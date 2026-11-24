package httpapi

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"zdorovie/backend/internal/ai"
	"zdorovie/backend/internal/db"
	"zdorovie/backend/internal/maxbot"
)

func (s *Server) handleCabinetDashboard(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	sub, err := s.store.GetLatestSubscription(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Не удалось загрузить подписку")
		return
	}

	subActive := sub != nil && sub.IsActive && sub.ExpiresAt.After(time.Now().UTC())
	var mats []db.CabinetMaterial
	if subActive || u.Role == "admin" || u.Role == "doctor" {
		mats, _ = s.store.ListCabinetMaterials(r.Context(), u.ID)
	}

	readyCount := 0
	completedCount := 0
	pdfCount := 0
	for _, m := range mats {
		if m.FileType == "pdf" {
			pdfCount++
			if m.ClientStatus == "ready" {
				readyCount++
			}
		}
		if m.Completed {
			completedCount++
		}
	}

	slots, _ := s.store.ListConsultationSlots(r.Context(), false, u.ID, "")
	var upcoming *db.ConsultationSlot
	now := time.Now().UTC()
	for i := range slots {
		if slots[i].ClientID != nil && *slots[i].ClientID == u.ID && slots[i].Status == "booked" && slots[i].SlotEnd.After(now) {
			upcoming = &slots[i]
			break
		}
	}

	docs, _ := s.store.ListPatientDocuments(r.Context(), u.ID, true)
	cCfg, _ := s.store.GetConsultationConfig(r.Context())

	var ownSlots []db.ConsultationSlot
	for _, sl := range slots {
		if sl.ClientID != nil && *sl.ClientID == u.ID {
			sl.DoctorNotes = ""
			ownSlots = append(ownSlots, sl)
		}
	}
	atts, _ := s.store.ListClientAttachments(r.Context(), u.ID)
	msgs, _ := s.store.ListChatMessages(r.Context(), u.ID)
	latestPatSum, _ := s.store.GetLatestPatientAISummary(r.Context(), u.ID, "patient")

	patProfile := &db.ClientProfile{
		User:            *u,
		Subscription:    sub,
		SubActive:       subActive,
		Materials:       mats,
		Consultations:   ownSlots,
		Attachments:     atts,
		Documents:       docs,
		Messages:        msgs,
		LatestAISummary: latestPatSum,
	}

	var completedSummary ai.PatientSummaryOutput
	unprocessedCount := 0
	if latestPatSum != nil {
		completedSummary = ai.PatientSummaryOutput{
			ID:                 latestPatSum.ID,
			GenerationMode:     latestPatSum.GenerationMode,
			TemplateCode:       latestPatSum.TemplateCode,
			TemplateTitle:      ai.DefaultPatientSummaryTemplateTitle,
			SummaryText:        latestPatSum.SummaryText,
			LLMModelName:       latestPatSum.LLMModelName,
			EventsCount:        latestPatSum.EventsCount,
			NewEventsCount:     latestPatSum.NewEventsCount,
			BaseSummaryID:      latestPatSum.BaseSummaryID,
			ProcessedEventKeys: latestPatSum.ProcessedEventKeys,
			ScopeLabel:         latestPatSum.ScopeLabel,
			CreatedAt:          &latestPatSum.CreatedAt,
		}
		unprocessedCount = ai.CountUnprocessedProfileEvents(patProfile, latestPatSum, true)
	} else {
		completedSummary, _, unprocessedCount, _ = s.generateAndPersistPatientSummary(
			r.Context(),
			patProfile,
			ai.SummaryFilterRequest{Mode: "full"},
			nil,
			true,
		)
	}
	chatUnread, _ := s.store.CountUnreadForClient(r.Context(), u.ID)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user":         u,
		"subscription": sub,
		"sub_active":   subActive,
		"stats": map[string]int{
			"total_materials":     len(mats),
			"completed_materials": completedCount,
			"pdf_total":           pdfCount,
			"pdf_ready":           readyCount,
			"documents_count":     len(docs),
		},
		"upcoming_consultation":    upcoming,
		"consultation_config":      cCfg,
		"completed_summary":        completedSummary,
		"unprocessed_events_count": unprocessedCount,
		"summary_template":         ai.GetPatientSummaryTemplateSpec(),
		"consultations":            ownSlots,
		"attachments":              atts,
		"documents":                docs,
		"chat_unread_count":        chatUnread,
	})
}

func (s *Server) handleCabinetProfile(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	sub, _ := s.store.GetLatestSubscription(r.Context(), u.ID)
	orders, _ := s.store.ListOrders(r.Context(), u.ID, "")
	tariffs, _ := s.store.ListTariffs(r.Context(), true)

	subActive := sub != nil && sub.IsActive && sub.ExpiresAt.After(time.Now().UTC())
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"user":         u,
		"subscription": sub,
		"sub_active":   subActive,
		"orders":       orders,
		"tariffs":      tariffs,
	})
}

func (s *Server) handleCabinetRenew(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var req struct {
		TariffCode string `json:"tariff_code"`
	}
	if err := decodeJSON(r, &req, 64<<10); err != nil || req.TariffCode == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "Выберите тариф для продления")
		return
	}
	tariff, err := s.store.GetTariffByCode(r.Context(), req.TariffCode)
	if err != nil || !tariff.IsActive {
		writeError(w, http.StatusBadRequest, "invalid_tariff", "Тариф не найден")
		return
	}
	order, err := s.store.CreateOrder(r.Context(), u.ID, *tariff, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Ошибка создания заказа")
		return
	}
	payURL, err := s.yoo.CreatePaymentForOrder(r.Context(), *order, *u, *tariff)
	if err != nil {
		writeError(w, http.StatusBadGateway, "payment_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"order":       order,
		"payment_url": payURL,
	})
}

func (s *Server) handleCabinetMaterials(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	mats, err := s.store.ListCabinetMaterials(r.Context(), u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Не удалось загрузить материалы")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"materials": mats,
	})
}

func (s *Server) handleCabinetMaterialProgress(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	matID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID материала")
		return
	}
	var req struct {
		Completed bool `json:"completed"`
	}
	if err := decodeJSON(r, &req, 16<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	if err := s.store.SetMaterialCompleted(r.Context(), u.ID, int(matID), req.Completed); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Не удалось обновить прогресс")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true, "completed": req.Completed})
}

func (s *Server) handleCabinetMaterialDownload(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	matID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID материала")
		return
	}

	mat, err := s.store.GetMaterialByID(r.Context(), int(matID))
	if err != nil || !mat.IsPublished {
		writeError(w, http.StatusNotFound, "not_found", "Материал не найден")
		return
	}

	var bucket, key, filename string
	filename = fmt.Sprintf("module_%d_lesson_%d.pdf", mat.ModuleNumber, mat.LessonNumber)

	if u.Role == "admin" || u.Role == "doctor" {
		bucket = s.s3.BucketMaster
		key = mat.S3MasterKey
	} else {
		cm, err := s.store.GetClientMaterial(r.Context(), u.ID, int(matID))
		if err != nil {
			writeError(w, http.StatusConflict, "watermark_missing", "Персональный PDF ещё не сформирован. Обратитесь в поддержку или попробуйте позже.")
			return
		}
		if cm.Status != "ready" {
			writeError(w, http.StatusConflict, "watermark_pending", "Ваш персональный PDF с защитной маркировкой сейчас подготавливается. Пожалуйста, обновите страницу через пару секунд.")
			return
		}
		bucket = s.s3.BucketClient
		key = cm.S3ClientKey
	}

	if s.s3 == nil {
		writeError(w, http.StatusServiceUnavailable, "s3_unavailable", "Файловое хранилище недоступно")
		return
	}
	url, err := s.s3.PresignedGetURL(r.Context(), bucket, key, 600*time.Second, filename)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "s3_error", "Не удалось сформировать ссылку для скачивания")
		return
	}

	s.store.WriteAuditLog(r.Context(), &u.ID, u.Role, "material.download", "material", strconv.FormatInt(matID, 10), map[string]string{
		"s3_key": key,
	})

	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"url":             url,
			"expires_seconds": 600,
			"file_name":       filename,
		})
		return
	}
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (s *Server) handleCabinetConsultationSlots(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	sub, _ := s.store.GetLatestSubscription(r.Context(), u.ID)
	cCfg, _ := s.store.GetConsultationConfig(r.Context())

	subActive := sub != nil && sub.IsActive && sub.ExpiresAt.After(time.Now().UTC())
	hasFeedback := (sub != nil && sub.HasFeedback) || u.Role == "admin" || u.Role == "doctor"

	slots, err := s.store.ListConsultationSlots(r.Context(), false, u.ID, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Не удалось загрузить расписание")
		return
	}

	// Scrub sensitive summary/internal fields before returning to patient
	for i := range slots {
		slots[i].DoctorNotes = ""
		if slots[i].ClientID == nil || *slots[i].ClientID != u.ID {
			slots[i].ClientName = ""
			slots[i].ClientEmail = ""
			slots[i].ClientQuestion = ""
			slots[i].MeetingURL = ""
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"consultation_config": cCfg,
		"sub_active":          subActive,
		"has_feedback":        hasFeedback,
		"slots":               slots,
	})
}

func (s *Server) handleCabinetBookSlot(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	sub, _ := s.store.GetLatestSubscription(r.Context(), u.ID)
	if u.Role == "client" && (sub == nil || !sub.HasFeedback) {
		writeError(w, http.StatusForbidden, "tariff_no_feedback", "Запись на индивидуальные консультации доступна на тарифе «С обратной связью».")
		return
	}

	slotID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID слота")
		return
	}
	var req struct {
		Question string `json:"question"`
	}
	_ = decodeJSON(r, &req, 64<<10)

	var orderID int64
	if sub != nil {
		orderID = sub.OrderID
	}
	sl, err := s.store.BookConsultationSlot(r.Context(), slotID, u.ID, orderID, strings.TrimSpace(req.Question))
	if err != nil {
		writeError(w, http.StatusConflict, "slot_unavailable", "Выбранный слот уже занят или недоступен")
		return
	}

	msk := time.FixedZone("MSK", 3*3600)
	dateStr := sl.SlotStart.In(msk).Format("02.01.2006 в 15:04 (МСК)")
	if s.yoo != nil {
		s.yoo.DeliverEmail(u.Email, "Запись на консультацию подтверждена", fmt.Sprintf(
			"Здравствуйте, %s!\n\nВы записаны на индивидуальную консультацию %s.\nСсылка на видеовстречу появится в личном кабинете.", u.FullName, dateStr,
		))
	}
	if u.MaxUserID != nil && *u.MaxUserID != "" && s.bot != nil {
		_ = s.bot.SendMessage(r.Context(), *u.MaxUserID, maxbot.OutgoingMessage{
			Text: fmt.Sprintf("📅 *Запись подтверждена*\n\nЖдём вас на консультации *%s*. Вы можете прикрепить анализы в личном кабинете.", dateStr),
		})
	}
	s.store.WriteAuditLog(r.Context(), &u.ID, u.Role, "consultation.booked", "consultation_slot", strconv.FormatInt(slotID, 10), nil)
	writeJSON(w, http.StatusOK, map[string]interface{}{"slot": sl})
}

func (s *Server) handleCabinetCancelSlot(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	slotID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID слота")
		return
	}
	if err := s.store.CancelConsultationSlot(r.Context(), slotID, u.ID, false); err != nil {
		writeError(w, http.StatusConflict, "cancel_failed", "Не удалось отменить запись")
		return
	}
	s.store.WriteAuditLog(r.Context(), &u.ID, u.Role, "consultation.canceled", "consultation_slot", strconv.FormatInt(slotID, 10), nil)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCabinetUpdateSlotQuestion(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	slotID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID слота")
		return
	}
	var req struct {
		Question string `json:"question"`
	}
	if err := decodeJSON(r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Некорректный JSON")
		return
	}
	if err := s.store.UpdateSlotQuestion(r.Context(), slotID, u.ID, strings.TrimSpace(req.Question)); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Запись не найдена")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleCabinetUploadAttachment(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	slotID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID слота")
		return
	}
	sl, err := s.store.GetConsultationSlot(r.Context(), slotID, false)
	if err != nil || sl.ClientID == nil || *sl.ClientID != u.ID {
		writeError(w, http.StatusForbidden, "forbidden", "Можно прикреплять файлы только к своей записи")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 25<<20)
	if err := r.ParseMultipartForm(25 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "file_too_large", "Максимальный размер файла анализа — 20 МБ")
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing_file", "Не передан файл")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	switch ext {
	case ".pdf", ".jpg", ".jpeg", ".png", ".webp":
	default:
		writeError(w, http.StatusBadRequest, "unsupported_type", "Допустимы файлы PDF, JPEG, PNG, WEBP")
		return
	}
	ct := hdr.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}

	safeName := filepath.Base(hdr.Filename)
	s3Key := fmt.Sprintf("attachments/slot_%d/user_%d_%d_%s", slotID, u.ID, time.Now().Unix(), safeName)
	if err := s.s3.PutStream(r.Context(), s.s3.BucketConsult, s3Key, file, hdr.Size, ct); err != nil {
		writeError(w, http.StatusInternalServerError, "s3_error", "Ошибка сохранения файла")
		return
	}
	att, err := s.store.AddConsultationAttachment(r.Context(), slotID, u.ID, safeName, ct, hdr.Size, s3Key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Ошибка записи в БД")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"attachment": att})
}

func (s *Server) handleCabinetListAttachments(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	slotID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID слота")
		return
	}
	sl, err := s.store.GetConsultationSlot(r.Context(), slotID, false)
	if err != nil || sl.ClientID == nil || *sl.ClientID != u.ID {
		writeError(w, http.StatusForbidden, "forbidden", "Нет доступа к слоту")
		return
	}
	atts, err := s.store.ListConsultationAttachments(r.Context(), slotID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Ошибка загрузки вложений")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"attachments": atts})
}

func (s *Server) handleCabinetDocuments(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	// Never exposes raw audio recordings or unapproved Whisper transcripts!
	docs, err := s.store.ListPatientDocuments(r.Context(), u.ID, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", "Не удалось загрузить документы")
		return
	}
	if docs == nil {
		docs = []db.PatientDocument{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"documents": docs})
}

func (s *Server) handleCabinetDocumentDownload(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	docID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "Неверный ID документа")
		return
	}
	doc, err := s.store.GetPatientDocument(r.Context(), docID)
	if err != nil || !doc.IsPublished || (doc.ClientID != u.ID && u.Role == "client") {
		writeError(w, http.StatusNotFound, "not_found", "Документ не найден или не опубликован")
		return
	}
	filename := fmt.Sprintf("recommendations_%d.pdf", doc.ID)
	url, err := s.s3.PresignedGetURL(r.Context(), s.s3.BucketConsult, doc.S3PDFKey, 600*time.Second, filename)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "s3_error", "Ошибка генерации ссылки")
		return
	}
	s.store.WriteAuditLog(r.Context(), &u.ID, u.Role, "document.download", "patient_document", strconv.FormatInt(doc.ID, 10), nil)
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"url":             url,
			"expires_seconds": 600,
			"file_name":       filename,
		})
		return
	}
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (s *Server) handleCabinetSummary(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var req summaryFilterRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req, 64<<10); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "Некорректный формат фильтров")
			return
		}
	}
	// Patient cabinet never queries CRM question history or raw doctor notes.
	req.QuestionIDs = nil

	sub, _ := s.store.GetLatestSubscription(r.Context(), u.ID)
	subActive := sub != nil && sub.IsActive && sub.ExpiresAt.After(time.Now().UTC())

	var mats []db.CabinetMaterial
	if subActive || u.Role == "admin" || u.Role == "doctor" {
		mats, _ = s.store.ListCabinetMaterials(r.Context(), u.ID)
	}
	slots, _ := s.store.ListConsultationSlots(r.Context(), false, u.ID, "")
	var ownSlots []db.ConsultationSlot
	for _, sl := range slots {
		if sl.ClientID != nil && *sl.ClientID == u.ID {
			sl.DoctorNotes = ""
			ownSlots = append(ownSlots, sl)
		}
	}
	docs, _ := s.store.ListPatientDocuments(r.Context(), u.ID, true)
	atts, _ := s.store.ListClientAttachments(r.Context(), u.ID)
	msgs, _ := s.store.ListChatMessages(r.Context(), u.ID)

	patProfile := &db.ClientProfile{
		User:          *u,
		Subscription:  sub,
		SubActive:     subActive,
		Materials:     mats,
		Consultations: ownSlots,
		Attachments:   atts,
		Documents:     docs,
		Messages:      msgs,
	}

	out, saved, unprocessedCount, err := s.generateAndPersistPatientSummary(r.Context(), patProfile, req, nil, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "summary_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"summary":                  out,
		"saved_summary":            saved,
		"unprocessed_events_count": unprocessedCount,
		"summary_template":         ai.GetPatientSummaryTemplateSpec(),
	})
}
