package httpapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"zdorovie/backend/internal/ai"
	"zdorovie/backend/internal/db"
	"zdorovie/backend/internal/maxbot"
	"zdorovie/backend/internal/queue"
)

func (s *Server) handleAdminAnalytics(w http.ResponseWriter, r *http.Request) {
	analytics, err := s.store.GetAnalytics(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Не удалось загрузить аналитику")
		return
	}
	var qStats queue.QueueStats
	if s.queue != nil {
		qStats, _ = s.queue.Stats(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"analytics": analytics,
		"queues":    qStats,
	})
}

func (s *Server) handleAdminQueues(w http.ResponseWriter, r *http.Request) {
	if s.queue == nil {
		writeJSON(w, http.StatusOK, queue.QueueStats{})
		return
	}
	stats, err := s.queue.Stats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "queue_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleAdminClients(w http.ResponseWriter, r *http.Request) {
	tariffCode := r.URL.Query().Get("tariff")
	statusFilter := r.URL.Query().Get("status")
	clients, err := s.store.ListClients(r.Context(), tariffCode, statusFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Не удалось загрузить клиентов")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"clients": clients})
}

func (s *Server) handleAdminUpdateClientSub(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	clientID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID клиента")
		return
	}
	var req struct {
		TariffID  int    `json:"tariff_id"`
		ExpiresAt string `json:"expires_at"`
	}
	if err := decodeJSON(r, &req, 32<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	exp, err := time.Parse(time.RFC3339, req.ExpiresAt)
	if err != nil {
		exp, err = time.Parse("2006-01-02", req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_date", "Некорректный формат даты expires_at (YYYY-MM-DD или RFC3339)")
			return
		}
		exp = exp.Add(23*time.Hour + 59*time.Minute)
	}
	if err := s.store.UpdateClientSubscription(r.Context(), clientID, req.TariffID, exp); err != nil {
		writeError(w, http.StatusBadRequest, "update_failed", err.Error())
		return
	}
	s.store.WriteAuditLog(r.Context(), &actor.ID, actor.Role, "admin.subscription_updated", "user", strconv.FormatInt(clientID, 10), req)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAdminResendMagicLink(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	clientID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID клиента")
		return
	}
	u, err := s.store.GetUserByID(r.Context(), clientID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Пользователь не найден")
		return
	}
	magicURL, err := s.yoo.IssueMagicLink(r.Context(), *u)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_error", err.Error())
		return
	}

	s.yoo.DeliverEmail(u.Email, "Ссылка для входа в кабинет «Больше чем здоровье»", fmt.Sprintf(
		"Здравствуйте, %s!\n\nАдминистратор направил вам новую одноразовую ссылку для входа в личный кабинет:\n%s\n", u.FullName, magicURL,
	))
	if u.MaxUserID != nil && *u.MaxUserID != "" && s.bot != nil {
		_ = s.bot.SendMessage(r.Context(), *u.MaxUserID, maxbot.OutgoingMessage{
			Text: "🔐 *Новая ссылка для входа в личный кабинет*\n\nНажмите кнопку ниже для авторизации:",
			Attachments: maxbot.InlineKeyboard([]maxbot.Button{{Type: "link", Text: "🎓 Войти в Кабинет", URL: magicURL}}),
		})
	}
	s.store.WriteAuditLog(r.Context(), &actor.ID, actor.Role, "admin.resend_magic_link", "user", strconv.FormatInt(clientID, 10), nil)

	resp := map[string]interface{}{"sent": true}
	if s.cfg.AppEnv != "production" && s.cfg.AppDebug {
		resp["magic_url"] = magicURL
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAdminClientMaterials(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID клиента")
		return
	}
	cms, err := s.store.ListClientMaterialsForUser(r.Context(), clientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"client_materials": cms})
}

func (s *Server) handleAdminRewatermark(w http.ResponseWriter, r *http.Request) {
	clientID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID клиента")
		return
	}
	matID, err := pathID(r, "materialId")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID материала")
		return
	}
	u, err := s.store.GetUserByID(r.Context(), clientID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Клиент не найден")
		return
	}
	mat, err := s.store.GetMaterialByID(r.Context(), int(matID))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Материал не найден")
		return
	}
	orders, _ := s.store.ListOrders(r.Context(), clientID, "paid")
	var ordID int64
	ordNum := fmt.Sprintf("REPRINT-%d", time.Now().Unix())
	if len(orders) > 0 {
		ordID = orders[0].ID
		ordNum = orders[0].OrderNumber
	}
	cm, err := s.store.EnsureClientMaterialForUser(r.Context(), u.ID, ordID, ordNum, u.FullName, u.Email, u.Phone, mat.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	if s.queue != nil {
		if err := s.queue.EnqueueWatermark(queue.WatermarkPayload{
			ClientMaterialID: cm.ID,
			UserID:           u.ID,
			OrderID:          ordID,
			OrderNumber:      ordNum,
			MaterialID:       mat.ID,
			FullName:         u.FullName,
			Email:            u.Email,
			Phone:            u.Phone,
			MasterKey:        mat.S3MasterKey,
			ClientKey:        cm.S3ClientKey,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "queue_error", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"client_material": cm})
}

func (s *Server) handleAdminOrders(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	orders, err := s.store.ListOrders(r.Context(), 0, statusFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"orders": orders})
}

func (s *Server) handleAdminApproveOrder(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	orderID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID заказа")
		return
	}
	if err := s.store.ApproveOrderChannel(r.Context(), orderID); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Заказ не найден")
		return
	}
	ord, err := s.store.GetOrderByID(r.Context(), orderID)
	if err == nil && ord.UserMaxID != nil && *ord.UserMaxID != "" && s.bot != nil {
		cCfg, _ := s.store.GetConsultationConfig(r.Context())
		var btns [][]maxbot.Button
		if cCfg.MaxChannelInviteURL != "" {
			btns = append(btns, []maxbot.Button{{Type: "link", Text: "🔒 Перейти в закрытый канал курса", URL: cCfg.MaxChannelInviteURL}})
		}
		_ = s.bot.SendMessage(r.Context(), *ord.UserMaxID, maxbot.OutgoingMessage{
			Text: fmt.Sprintf("✅ *Доступ к закрытому каналу подтверждён*\n\nАдминистратор подтвердил ваш заказ *%s*. Добро пожаловать в комьюнити курса!", ord.OrderNumber),
			Attachments: maxbot.InlineKeyboard(btns...),
		})
	}
	s.store.WriteAuditLog(r.Context(), &actor.ID, actor.Role, "admin.order_channel_approved", "order", strconv.FormatInt(orderID, 10), nil)
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "order": ord})
}

func (s *Server) handleAdminListTariffs(w http.ResponseWriter, r *http.Request) {
	tariffs, err := s.store.ListTariffs(r.Context(), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"tariffs": tariffs})
}

func (s *Server) handleAdminUpdateTariff(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID тарифа")
		return
	}
	var req struct {
		Title        string `json:"title"`
		Description  string `json:"description"`
		PriceKopecks int64  `json:"price_kopecks"`
		DurationDays int    `json:"duration_days"`
		HasFeedback  bool   `json:"has_feedback"`
		IsActive     bool   `json:"is_active"`
	}
	if err := decodeJSON(r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	updated, err := s.store.UpdateTariff(r.Context(), int(id), req.Title, req.Description, req.PriceKopecks, req.DurationDays, req.HasFeedback, req.IsActive)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"tariff": updated})
}

func (s *Server) handleAdminListMaterials(w http.ResponseWriter, r *http.Request) {
	mats, err := s.store.ListMaterials(r.Context(), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"materials": mats})
}

func (s *Server) handleAdminCreateMaterial(w http.ResponseWriter, r *http.Request) {
	s.upsertMaterialFromRequest(w, r, 0)
}

func (s *Server) handleAdminUpdateMaterial(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID материала")
		return
	}
	s.upsertMaterialFromRequest(w, r, int(id))
}

func (s *Server) upsertMaterialFromRequest(w http.ResponseWriter, r *http.Request, id int) {
	var mat db.Material
	mat.ID = id
	mat.FileType = "pdf"
	mat.IsPublished = true

	if strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, 100<<20)
		if err := r.ParseMultipartForm(100 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "Ошибка чтения multipart данных")
			return
		}
		mat.ModuleNumber, _ = strconv.Atoi(r.FormValue("module_number"))
		mat.LessonNumber, _ = strconv.Atoi(r.FormValue("lesson_number"))
		mat.Title = strings.TrimSpace(r.FormValue("title"))
		mat.Description = strings.TrimSpace(r.FormValue("description"))
		mat.ContentMarkdown = r.FormValue("content_markdown")
		mat.VideoURL = strings.TrimSpace(r.FormValue("video_url"))
		mat.DurationMinutes, _ = strconv.Atoi(r.FormValue("duration_minutes"))
		if ft := strings.TrimSpace(r.FormValue("file_type")); ft != "" {
			mat.FileType = ft
		}
		if pub := r.FormValue("is_published"); pub != "" {
			mat.IsPublished = pub == "true" || pub == "1"
		}
		file, hdr, err := r.FormFile("master_pdf")
		if err == nil {
			defer file.Close()
			data, _ := io.ReadAll(io.LimitReader(file, 100<<20))
			key := fmt.Sprintf("masters/module-%d-lesson-%d-%d-%s", mat.ModuleNumber, mat.LessonNumber, time.Now().Unix(), hdr.Filename)
			if err := s.s3.PutBytes(r.Context(), s.s3.BucketMaster, key, data, "application/pdf"); err != nil {
				writeError(w, http.StatusInternalServerError, "s3_error", "Не удалось загрузить мастер-PDF в S3")
				return
			}
			mat.S3MasterKey = key
		}
	} else {
		if err := decodeJSON(r, &mat, 2<<20); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
			return
		}
		mat.ID = id
	}

	if mat.ModuleNumber <= 0 {
		mat.ModuleNumber = 1
	}
	if mat.LessonNumber <= 0 {
		mat.LessonNumber = 1
	}

	var saved *db.Material
	var err error
	if id == 0 {
		if mat.S3MasterKey == "" {
			mat.S3MasterKey = fmt.Sprintf("masters/module-%d-lesson-%d.pdf", mat.ModuleNumber, mat.LessonNumber)
		}
		saved, err = s.store.CreateMaterial(r.Context(), mat)
	} else {
		saved, err = s.store.UpdateMaterial(r.Context(), mat)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"material": saved})
}

func (s *Server) handleAdminQuestions(w http.ResponseWriter, r *http.Request) {
	qs, err := s.store.ListQuestions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"questions": qs})
}

func (s *Server) handleAdminReplyQuestion(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID вопроса")
		return
	}
	var req struct {
		Answer string `json:"answer"`
	}
	if err := decodeJSON(r, &req, 32<<10); err != nil || strings.TrimSpace(req.Answer) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Введите текст ответа")
		return
	}
	q, err := s.store.AnswerQuestion(r.Context(), id, strings.TrimSpace(req.Answer))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Вопрос не найден")
		return
	}
	if q.MaxUserID != nil && *q.MaxUserID != "" && s.bot != nil {
		_ = s.bot.SendMessage(r.Context(), *q.MaxUserID, maxbot.OutgoingMessage{
			Text: fmt.Sprintf("💬 *Ответ от врача и куратора:*\n\n%s", maxbot.EscapeMarkdown(q.AnswerText)),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"question": q})
}

func (s *Server) handleAdminGetConsultSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.GetConsultationConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleAdminSaveConsultSettings(w http.ResponseWriter, r *http.Request) {
	var cfg db.ConsultationConfig
	if err := decodeJSON(r, &cfg, 32<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	switch cfg.Mode {
	case "max_chat":
		cfg.Mode = "direct_chat"
	case "external_link":
		cfg.Mode = "external_widget"
	}
	switch cfg.Mode {
	case "builtin_slots", "external_widget", "direct_chat":
	default:
		writeError(w, http.StatusBadRequest, "invalid_mode", "Допустимые режимы: builtin_slots, direct_chat, external_widget")
		return
	}
	if err := s.store.SaveConsultationConfig(r.Context(), cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleAdminListSlots(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	slots, err := s.store.ListConsultationSlots(r.Context(), true, 0, statusFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"slots": slots})
}

func (s *Server) handleAdminCreateSlot(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	var req struct {
		SlotStart  string `json:"slot_start"`
		SlotEnd    string `json:"slot_end"`
		MeetingURL string `json:"meeting_url"`
	}
	if err := decodeJSON(r, &req, 32<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	start, err := time.Parse(time.RFC3339, req.SlotStart)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_date", "slot_start должен быть в формате RFC3339")
		return
	}
	end, err := time.Parse(time.RFC3339, req.SlotEnd)
	if err != nil {
		end = start.Add(45 * time.Minute)
	}
	sl, err := s.store.CreateConsultationSlot(r.Context(), actor.ID, start, end, strings.TrimSpace(req.MeetingURL))
	if err != nil {
		if errors.Is(err, db.ErrSlotOverlap) {
			writeError(w, http.StatusConflict, "slot_overlap", "Время пересекается с существующим слотом расписания: "+err.Error())
			return
		}
		if errors.Is(err, db.ErrInvalidSlotDuration) {
			writeError(w, http.StatusBadRequest, "bad_slot_duration", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"slot": sl})
}

func (s *Server) handleAdminUpdateSlot(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID слота")
		return
	}
	var req struct {
		SlotStart   *string `json:"slot_start"`
		SlotEnd     *string `json:"slot_end"`
		MeetingURL  *string `json:"meeting_url"`
		Status      *string `json:"status"`
		DoctorNotes *string `json:"doctor_notes"`
	}
	if err := decodeJSON(r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	var startPtr, endPtr *time.Time
	if req.SlotStart != nil && strings.TrimSpace(*req.SlotStart) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.SlotStart))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_date", "slot_start должен быть в формате RFC3339")
			return
		}
		startPtr = &t
	}
	if req.SlotEnd != nil && strings.TrimSpace(*req.SlotEnd) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*req.SlotEnd))
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_date", "slot_end должен быть в формате RFC3339")
			return
		}
		endPtr = &t
	}
	if err := s.store.UpdateSlotAdmin(r.Context(), id, startPtr, endPtr, req.MeetingURL, req.Status, req.DoctorNotes); err != nil {
		if errors.Is(err, db.ErrSlotOverlap) {
			writeError(w, http.StatusConflict, "slot_overlap", "Время пересекается с существующим слотом расписания: "+err.Error())
			return
		}
		if errors.Is(err, db.ErrInvalidSlotDuration) {
			writeError(w, http.StatusBadRequest, "bad_slot_duration", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	sl, _ := s.store.GetConsultationSlot(r.Context(), id, true)
	writeJSON(w, http.StatusOK, map[string]interface{}{"slot": sl})
}

func (s *Server) handleAdminDeleteSlot(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID слота")
		return
	}
	if err := s.store.CancelConsultationSlot(r.Context(), id, 0, true); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAdminDownloadAttachment(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID вложения")
		return
	}
	att, err := s.store.GetConsultationAttachment(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Вложение не найдено")
		return
	}
	url, err := s.s3.PresignedGetURL(r.Context(), s.s3.BucketConsult, att.S3Key, 300*time.Second, att.FileName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "s3_error", err.Error())
		return
	}
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]interface{}{"url": url, "file_name": att.FileName})
		return
	}
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (s *Server) handleAdminClientProfile(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID клиента")
		return
	}
	profile, err := s.store.GetClientProfile(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Пациентка не найдена")
		return
	}

	preSlotSummary := s.ensureDuePreConsultationSummary(r.Context(), profile)

	var aiSum ai.PatientSummaryOutput
	unprocessedCount := 0
	if profile.LatestAISummary != nil {
		ls := profile.LatestAISummary
		aiSum = ai.PatientSummaryOutput{
			ID:                 ls.ID,
			SlotID:             ls.SlotID,
			GenerationMode:     ls.GenerationMode,
			TemplateCode:       ls.TemplateCode,
			TemplateTitle:      ai.DefaultPatientSummaryTemplateTitle,
			SummaryText:        ls.SummaryText,
			LLMModelName:       ls.LLMModelName,
			EventsCount:        ls.EventsCount,
			NewEventsCount:     ls.NewEventsCount,
			BaseSummaryID:      ls.BaseSummaryID,
			ProcessedEventKeys: ls.ProcessedEventKeys,
			ScopeLabel:         ls.ScopeLabel,
			CreatedAt:          &ls.CreatedAt,
		}
		unprocessedCount = ai.CountUnprocessedProfileEvents(profile, ls, false)
	} else {
		var saved *db.PatientAISummary
		aiSum, saved, unprocessedCount, _ = s.generateAndPersistPatientSummary(
			r.Context(),
			profile,
			ai.SummaryFilterRequest{Mode: "full"},
			nil,
			false,
		)
		if saved != nil {
			profile.LatestAISummary = saved
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"profile":                  profile,
		"ai_summary":               aiSum,
		"unprocessed_events_count": unprocessedCount,
		"pre_consultation_summary": preSlotSummary,
		"summary_template":         ai.GetPatientSummaryTemplateSpec(),
	})
}

func (s *Server) handleAdminClientSummary(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID клиента")
		return
	}
	var req summaryFilterRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req, 64<<10); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "Некорректный формат фильтров")
			return
		}
	}
	profile, err := s.store.GetClientProfile(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Пациентка не найдена")
		return
	}
	aiSum, saved, unprocessedCount, err := s.generateAndPersistPatientSummary(r.Context(), profile, req, nil, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "summary_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"summary":                  aiSum,
		"saved_summary":            saved,
		"unprocessed_events_count": unprocessedCount,
		"summary_template":         ai.GetPatientSummaryTemplateSpec(),
	})
}

func (s *Server) handleAdminQuestionSuggestReplies(w http.ResponseWriter, r *http.Request) {
	actor := currentUser(r)
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID вопроса")
		return
	}
	q, err := s.store.GetQuestionByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Вопрос не найден")
		return
	}

	var pCtx ai.PatientContextForAI
	if q.MatchedUserID != nil && *q.MatchedUserID > 0 {
		if profile, pErr := s.store.GetClientProfile(r.Context(), *q.MatchedUserID); pErr == nil {
			pCtx, _ = buildPatientAIContext(profile, summaryFilterRequest{}, false)
		}
	} else {
		pCtx = ai.PatientContextForAI{
			FullName:        q.ClientName,
			PregnancyStatus: q.ClientPregStatus,
			TariffTitle:     q.TariffTitle,
			SubActive:       q.SubActive,
		}
	}

	variants, modelName, err := ai.SuggestQuestionReplies(r.Context(), s.cfg, q.QuestionText, pCtx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ai_error", "Не удалось сформировать варианты ответа")
		return
	}
	s.store.WriteAuditLog(r.Context(), &actor.ID, actor.Role, "question.ai_suggest", "question", strconv.FormatInt(q.ID, 10), map[string]interface{}{"model": modelName, "variants": len(variants)})
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"question":   q,
		"variants":   variants,
		"model_name": modelName,
	})
}
