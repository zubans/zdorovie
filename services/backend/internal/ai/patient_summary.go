package ai

import (
	"context"
	"fmt"
	"strings"
	"time"

	"zdorovie/backend/internal/config"
	"zdorovie/backend/internal/db"
)

const DefaultPatientSummaryTemplateCode = "avocado_clinical_v1"
const DefaultPatientSummaryTemplateTitle = "Клинический паспорт беременности «Авокадо» (6 разделов)"

// SummaryTemplateSection describes one section of the unified patient summary template.
type SummaryTemplateSection struct {
	Number      int    `json:"number"`
	Heading     string `json:"heading"`
	Description string `json:"description"`
}

// PatientSummaryTemplateSpec describes the structured template used for all patient AI summaries.
type PatientSummaryTemplateSpec struct {
	Code        string                   `json:"code"`
	Title       string                   `json:"title"`
	Description string                   `json:"description"`
	Sections    []SummaryTemplateSection `json:"sections"`
}

// GetPatientSummaryTemplateSpec returns the canonical 6-section clinical summary template.
func GetPatientSummaryTemplateSpec() PatientSummaryTemplateSpec {
	return PatientSummaryTemplateSpec{
		Code:        DefaultPatientSummaryTemplateCode,
		Title:       DefaultPatientSummaryTemplateTitle,
		Description: "Единый клинический шаблон ИИ-саммари профиля беременной: объединяет срок, динамику новых событий, анализы, назначения, чат, вопросы MAX и прогресс обучения.",
		Sections: []SummaryTemplateSection{
			{
				Number:      1,
				Heading:     "1. Паспорт срока и статус ведения",
				Description: "Срок беременности / триместр, активный тариф, прогресс курса и режим расчёта сводки.",
			},
			{
				Number:      2,
				Heading:     "2. Что нового с прошлого саммари (Динамика)",
				Description: "Новые необработанные материалы, свежие сообщения в чате, загруженные анализы и запрос к предстоящей встрече.",
			},
			{
				Number:      3,
				Heading:     "3. Клиническая картина, анализы и назначения",
				Description: "Жалобы, заметки с приёмов, прикреплённые файлы анализов, утверждённые рекомендации и план контроля.",
			},
			{
				Number:      4,
				Heading:     "4. Коммуникация: чат с врачом и вопросы из MAX",
				Description: "Ключевые вопросы пациентки во внутреннем чате и боте MAX и данные врачом ответы.",
			},
			{
				Number:      5,
				Heading:     "5. Освоение программы курса «Авокадо»",
				Description: "Изученные модули и памятки, усвоенные темы по триместру и следующий урок.",
			},
			{
				Number:      6,
				Heading:     "6. Фокус внимания и план к следующей встрече",
				Description: "Главные акценты для врача на ближайшую консультацию и спокойный план действий для пациентки.",
			},
		},
	}
}

// SummaryFilterRequest defines manual or automated summary generation parameters.
type SummaryFilterRequest struct {
	Mode               string   `json:"mode"` // full | incremental | custom | pre_consultation
	UsePreviousSummary *bool    `json:"use_previous_summary,omitempty"`
	TemplateCode       string   `json:"template_code,omitempty"`
	SlotID             *int64   `json:"slot_id,omitempty"`
	DateFrom           string   `json:"date_from"`
	DateTo             string   `json:"date_to"`
	EventTypes         []string `json:"event_types"` // materials, consultations, attachments, documents, chat, questions
	MaterialIDs        []int    `json:"material_ids"`
	ConsultationIDs    []int64  `json:"consultation_ids"`
	AttachmentIDs      []int64  `json:"attachment_ids"`
	DocumentIDs        []int64  `json:"document_ids"`
	ChatMessageIDs     []int64  `json:"chat_message_ids"`
	QuestionIDs        []int64  `json:"question_ids"`
}

func parseMSKBounds(dateFromStr, dateToStr string) (*time.Time, *time.Time, error) {
	msk := time.FixedZone("MSK", 3*3600)
	var fromT, toT *time.Time
	if strings.TrimSpace(dateFromStr) != "" {
		t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dateFromStr), msk)
		if err != nil {
			return nil, nil, fmt.Errorf("date_from должен быть в формате ГГГГ-ММ-ДД")
		}
		fromT = &t
	}
	if strings.TrimSpace(dateToStr) != "" {
		t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(dateToStr), msk)
		if err != nil {
			return nil, nil, fmt.Errorf("date_to должен быть в формате ГГГГ-ММ-ДД")
		}
		endOfDay := t.Add(24*time.Hour - time.Nanosecond)
		toT = &endOfDay
	}
	return fromT, toT, nil
}

func withinWindow(ts time.Time, fromT, toT *time.Time) bool {
	if fromT != nil && ts.Before(*fromT) {
		return false
	}
	if toT != nil && ts.After(*toT) {
		return false
	}
	return true
}

func makeStringSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		k := strings.ToLower(strings.TrimSpace(it))
		if k != "" {
			m[k] = true
		}
	}
	return m
}

func makeIntSet(items []int) map[int]bool {
	m := make(map[int]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}

func makeInt64Set(items []int64) map[int64]bool {
	m := make(map[int64]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}

// CountUnprocessedProfileEvents returns how many events in the patient's profile are not yet recorded in prev.
func CountUnprocessedProfileEvents(profile *db.ClientProfile, prev *db.PatientAISummary, patientSafe bool) int {
	if profile == nil {
		return 0
	}
	ctx, err := BuildPatientAIContext(profile, SummaryFilterRequest{Mode: "incremental"}, prev, nil, patientSafe)
	if err != nil {
		return 0
	}
	return ctx.NewEventsCount
}

// BuildPatientAIContext collects all data from a patient's profile and prepares it for full, incremental,
// pre-consultation (30 min before slot), or custom modal summarization.
func BuildPatientAIContext(
	profile *db.ClientProfile,
	req SummaryFilterRequest,
	prev *db.PatientAISummary,
	targetSlot *db.ConsultationSlot,
	patientSafe bool,
) (PatientContextForAI, error) {
	fromT, toT, err := parseMSKBounds(req.DateFrom, req.DateTo)
	if err != nil {
		return PatientContextForAI{}, err
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		hasFilters := req.DateFrom != "" || req.DateTo != "" || len(req.EventTypes) > 0 ||
			len(req.MaterialIDs) > 0 || len(req.ConsultationIDs) > 0 || len(req.AttachmentIDs) > 0 ||
			len(req.DocumentIDs) > 0 || len(req.ChatMessageIDs) > 0 || len(req.QuestionIDs) > 0
		if hasFilters {
			mode = "custom"
		} else {
			mode = "full"
		}
	}

	usePrev := false
	switch mode {
	case "incremental", "pre_consultation":
		usePrev = prev != nil && strings.TrimSpace(prev.SummaryText) != ""
	case "full":
		// Explicit requirement: full mode over all patient materials NEVER uses previous summary!
		usePrev = false
	case "custom":
		if req.UsePreviousSummary != nil && *req.UsePreviousSummary && prev != nil && strings.TrimSpace(prev.SummaryText) != "" {
			usePrev = true
		}
	}

	prevKeys := make(map[string]bool)
	if usePrev && prev != nil {
		for _, k := range prev.ProcessedEventKeys {
			prevKeys[k] = true
		}
	}

	typesSet := makeStringSet(req.EventTypes)
	includeType := func(name string) bool {
		if mode == "full" || mode == "incremental" || mode == "pre_consultation" || len(typesSet) == 0 {
			return true
		}
		return typesSet[name]
	}

	matFilter := makeIntSet(req.MaterialIDs)
	slotFilter := makeInt64Set(req.ConsultationIDs)
	attFilter := makeInt64Set(req.AttachmentIDs)
	docFilter := makeInt64Set(req.DocumentIDs)
	chatFilter := makeInt64Set(req.ChatMessageIDs)
	qFilter := makeInt64Set(req.QuestionIDs)

	hasExplicitIDs := mode == "custom" && (len(matFilter) > 0 || len(slotFilter) > 0 || len(attFilter) > 0 ||
		len(docFilter) > 0 || len(chatFilter) > 0 || len(qFilter) > 0)

	msk := time.FixedZone("MSK", 3*3600)
	tariffTitle := ""
	subInfo := "Не активна"
	if profile.Subscription != nil {
		tariffTitle = profile.Subscription.TariffName
		if profile.SubActive {
			subInfo = fmt.Sprintf("Активна до %s (осталось %d дн.)", profile.Subscription.ExpiresAt.In(msk).Format("02.01.2006"), profile.Subscription.DaysLeft)
		} else {
			subInfo = fmt.Sprintf("Истекла %s", profile.Subscription.ExpiresAt.In(msk).Format("02.01.2006"))
		}
	}

	tplCode := strings.TrimSpace(req.TemplateCode)
	if tplCode == "" {
		tplCode = DefaultPatientSummaryTemplateCode
	}

	out := PatientContextForAI{
		FullName:           profile.User.FullName,
		PregnancyStatus:    profile.User.PregnancyStatus,
		TariffTitle:        tariffTitle,
		SubActive:          profile.SubActive,
		SubscriptionInfo:   subInfo,
		GenerationMode:     mode,
		TemplateCode:       tplCode,
		UsePreviousSummary: usePrev,
	}

	if usePrev && prev != nil {
		out.BaseSummaryID = &prev.ID
		out.PreviousSummaryText = prev.SummaryText
		out.PreviousSummaryAt = prev.CreatedAt.In(msk).Format("02.01.2006 15:04")
	}

	if targetSlot != nil {
		slotStr := targetSlot.SlotStart.In(msk).Format("02.01.2006 в 15:04 (МСК)")
		qText := nonEmpty(targetSlot.ClientQuestion, "не указан")
		out.UpcomingSlotInfo = fmt.Sprintf("Запланирована консультация #%d на %s. Запрос пациентки: %s", targetSlot.ID, slotStr, qText)
	} else {
		now := time.Now().UTC()
		for _, sl := range profile.Consultations {
			if sl.Status == "booked" && sl.SlotEnd.After(now) {
				slotStr := sl.SlotStart.In(msk).Format("02.01.2006 в 15:04 (МСК)")
				out.UpcomingSlotInfo = fmt.Sprintf("Ближайшая запись #%d на %s (запрос: %s)", sl.ID, slotStr, nonEmpty(sl.ClientQuestion, "без комментария"))
				break
			}
		}
	}

	seenKeys := make(map[string]bool)
	var allKeys []string
	recordEvent := func(key, deltaDesc string) {
		if !seenKeys[key] {
			seenKeys[key] = true
			allKeys = append(allKeys, key)
		}
		out.EventsCount++
		if !prevKeys[key] {
			out.NewEventsCount++
			if deltaDesc != "" {
				out.NewDeltaItems = append(out.NewDeltaItems, deltaDesc)
			}
		}
	}

	// 1. Course Materials (both completed AND in-progress lessons)
	if includeType("materials") && (!hasExplicitIDs || len(matFilter) > 0) {
		for _, m := range profile.Materials {
			if len(matFilter) > 0 && !matFilter[m.ID] {
				continue
			}
			if m.Completed {
				ts := m.CreatedAt
				if m.CompletedAt != nil {
					ts = *m.CompletedAt
				}
				if !withinWindow(ts, fromT, toT) {
					continue
				}
				dateLabel := ts.In(msk).Format("02.01.2006")
				line := fmt.Sprintf("[%s] Модуль %d.%d «%s» — %s", dateLabel, m.ModuleNumber, m.LessonNumber, m.Title, m.Description)
				out.CompletedLessons = append(out.CompletedLessons, line)
				key := fmt.Sprintf("mat:%d:done", m.ID)
				recordEvent(key, fmt.Sprintf("Изучен урок %d.%d «%s» (%s)", m.ModuleNumber, m.LessonNumber, m.Title, dateLabel))
			} else if len(matFilter) > 0 || (fromT == nil && toT == nil) {
				out.InProgressLessons = append(out.InProgressLessons, fmt.Sprintf("Модуль %d.%d «%s» (в плане изучения)", m.ModuleNumber, m.LessonNumber, m.Title))
			}
		}
	}

	// Map slot start times for attachment labels
	slotTimeByID := make(map[int64]string, len(profile.Consultations))
	for _, c := range profile.Consultations {
		slotTimeByID[c.ID] = c.SlotStart.In(msk).Format("02.01.2006 15:04")
	}

	// 2. Consultations (appointments, patient questions, doctor notes)
	if includeType("consultations") && (!hasExplicitIDs || len(slotFilter) > 0) {
		for _, c := range profile.Consultations {
			if len(slotFilter) > 0 && !slotFilter[c.ID] {
				continue
			}
			if !withinWindow(c.SlotStart, fromT, toT) {
				continue
			}
			dateLabel := c.SlotStart.In(msk).Format("02.01.2006 15:04")
			statusLabel := c.Status
			switch c.Status {
			case "booked":
				statusLabel = "запланирована"
			case "completed":
				statusLabel = "завершена"
			}
			line := fmt.Sprintf("[%s] Консультация #%d (%s)", dateLabel, c.ID, statusLabel)
			if strings.TrimSpace(c.ClientQuestion) != "" {
				line += fmt.Sprintf(" — запрос пациентки: %s", strings.TrimSpace(c.ClientQuestion))
			}
			if !patientSafe && strings.TrimSpace(c.DoctorNotes) != "" {
				line += fmt.Sprintf(" — заметки врача: %s", strings.TrimSpace(c.DoctorNotes))
			}
			out.Consultations = append(out.Consultations, line)
			key := fmt.Sprintf("slot:%d:%s:%s", c.ID, c.Status, strings.TrimSpace(c.ClientQuestion))
			if !patientSafe {
				key += ":" + strings.TrimSpace(c.DoctorNotes)
			}
			recordEvent(key, fmt.Sprintf("Консультация %s (%s): %s", dateLabel, statusLabel, nonEmpty(c.ClientQuestion, "без уточнения запроса")))
		}
	}

	// 3. Attached Lab / Analysis Files
	if (includeType("attachments") || includeType("consultations")) && (!hasExplicitIDs || len(attFilter) > 0) {
		for _, a := range profile.Attachments {
			if len(attFilter) > 0 && !attFilter[a.ID] {
				continue
			}
			if !withinWindow(a.CreatedAt, fromT, toT) {
				continue
			}
			dateLabel := a.CreatedAt.In(msk).Format("02.01.2006 15:04")
			slotRef := slotTimeByID[a.SlotID]
			if slotRef == "" {
				slotRef = fmt.Sprintf("слот #%d", a.SlotID)
			}
			line := fmt.Sprintf("[%s] Файл анализа «%s» (%d КБ, ко встрече %s)", dateLabel, a.FileName, a.FileSize/1024+1, slotRef)
			out.Attachments = append(out.Attachments, line)
			key := fmt.Sprintf("att:%d", a.ID)
			recordEvent(key, fmt.Sprintf("Загружен файл анализов «%s» (%s)", a.FileName, dateLabel))
		}
	}

	// 4. Published Medical Documents / Prescriptions
	if includeType("documents") && (!hasExplicitIDs || len(docFilter) > 0) {
		for _, d := range profile.Documents {
			if patientSafe && !d.IsPublished {
				continue
			}
			if len(docFilter) > 0 && !docFilter[d.ID] {
				continue
			}
			ts := d.CreatedAt
			if d.PublishedAt != nil {
				ts = *d.PublishedAt
			}
			if !withinWindow(ts, fromT, toT) {
				continue
			}
			dateLabel := ts.In(msk).Format("02.01.2006")
			line := fmt.Sprintf("[%s] %s", dateLabel, d.Title)
			if strings.TrimSpace(d.Complaints) != "" {
				line += fmt.Sprintf(" | Жалобы: %s", strings.TrimSpace(d.Complaints))
			}
			if strings.TrimSpace(d.Anamnesis) != "" {
				line += fmt.Sprintf(" | Анамнез: %s", strings.TrimSpace(d.Anamnesis))
			}
			if strings.TrimSpace(d.Recommendations) != "" {
				line += fmt.Sprintf(" | Рекомендации: %s", strings.TrimSpace(d.Recommendations))
			}
			if strings.TrimSpace(d.DiagnosticsPlan) != "" {
				line += fmt.Sprintf(" | Обследования: %s", strings.TrimSpace(d.DiagnosticsPlan))
			}
			if strings.TrimSpace(d.FollowUp) != "" {
				line += fmt.Sprintf(" | Контроль: %s", strings.TrimSpace(d.FollowUp))
			}
			out.Prescriptions = append(out.Prescriptions, line)
			key := fmt.Sprintf("doc:%d", d.ID)
			recordEvent(key, fmt.Sprintf("Опубликовано заключение «%s» от %s", d.Title, dateLabel))
		}
	}

	// 5. Internal Cabinet Chat Messages (patient <-> doctor)
	if includeType("chat") && (!hasExplicitIDs || len(chatFilter) > 0) {
		for _, msg := range profile.Messages {
			if len(chatFilter) > 0 && !chatFilter[msg.ID] {
				continue
			}
			if !withinWindow(msg.CreatedAt, fromT, toT) {
				continue
			}
			dateLabel := msg.CreatedAt.In(msk).Format("02.01.2006 15:04")
			roleLabel := "Пациентка"
			if msg.SenderRole != "client" {
				roleLabel = "Врач"
			}
			line := fmt.Sprintf("[%s] %s: %s", dateLabel, roleLabel, strings.TrimSpace(msg.Body))
			out.ChatMessages = append(out.ChatMessages, line)
			key := fmt.Sprintf("chat:%d", msg.ID)
			recordEvent(key, fmt.Sprintf("Сообщение в чате (%s, %s): %s", roleLabel, dateLabel, limitRunes(strings.TrimSpace(msg.Body), 110)))
		}
	}

	// 6. MAX Bot Questions & Replies
	if !patientSafe && includeType("questions") && (!hasExplicitIDs || len(qFilter) > 0) {
		for _, q := range profile.Questions {
			if len(qFilter) > 0 && !qFilter[q.ID] {
				continue
			}
			if !withinWindow(q.CreatedAt, fromT, toT) {
				continue
			}
			dateLabel := q.CreatedAt.In(msk).Format("02.01.2006 15:04")
			line := fmt.Sprintf("[%s] Вопрос в MAX: %s", dateLabel, strings.TrimSpace(q.QuestionText))
			if strings.TrimSpace(q.AnswerText) != "" {
				line += fmt.Sprintf(" (Ответ врача: %s)", strings.TrimSpace(q.AnswerText))
			}
			out.RecentQuestions = append(out.RecentQuestions, line)
			key := fmt.Sprintf("q:%d:%s", q.ID, q.Status)
			recordEvent(key, fmt.Sprintf("Вопрос в боте MAX (%s): %s", dateLabel, limitRunes(strings.TrimSpace(q.QuestionText), 100)))
		}
	}

	// Merge processed event keys if building incrementally on previous summary
	if usePrev && prev != nil {
		merged := make([]string, 0, len(prev.ProcessedEventKeys)+len(allKeys))
		added := make(map[string]bool)
		for _, k := range prev.ProcessedEventKeys {
			if !added[k] {
				added[k] = true
				merged = append(merged, k)
			}
		}
		for _, k := range allKeys {
			if !added[k] {
				added[k] = true
				merged = append(merged, k)
			}
		}
		out.ProcessedEventKeys = merged
	} else {
		out.ProcessedEventKeys = allKeys
	}

	// Build clear ScopeLabel
	out.ScopeLabel = buildScopeLabel(mode, usePrev, out.PreviousSummaryAt, out.NewEventsCount, targetSlot, fromT, toT, typesSet, hasExplicitIDs, msk)
	return out, nil
}

func buildScopeLabel(
	mode string,
	usePrev bool,
	prevAt string,
	newCount int,
	targetSlot *db.ConsultationSlot,
	fromT, toT *time.Time,
	typesSet map[string]bool,
	hasExplicitIDs bool,
	msk *time.Location,
) string {
	switch mode {
	case "pre_consultation":
		slotLbl := ""
		if targetSlot != nil {
			slotLbl = " к приёму " + targetSlot.SlotStart.In(msk).Format("02.01 15:04")
		}
		if usePrev {
			return fmt.Sprintf("Предконсультационное саммари%s (база от %s + новых событий: %d)", slotLbl, prevAt, newCount)
		}
		return fmt.Sprintf("Предконсультационное саммари%s (полный сбор профиля)", slotLbl)

	case "incremental":
		if usePrev {
			return fmt.Sprintf("Инкрементальное обновление (база от %s + новых материалов: %d)", prevAt, newCount)
		}
		return "Первичное базовое саммари по всем материалам профиля"

	case "full":
		return "Полный пересчёт по всем материалам профиля (с нуля, без предыдущего саммари)"
	}

	var parts []string
	if usePrev {
		parts = append(parts, fmt.Sprintf("на основе саммари от %s (+%d новых)", prevAt, newCount))
	} else {
		parts = append(parts, "пересчёт без предыдущего саммари")
	}
	if fromT != nil || toT != nil {
		fromLbl := "начала"
		toLbl := "сегодня"
		if fromT != nil {
			fromLbl = fromT.In(msk).Format("02.01.2006")
		}
		if toT != nil {
			toLbl = toT.In(msk).Format("02.01.2006")
		}
		parts = append(parts, fmt.Sprintf("период %s – %s", fromLbl, toLbl))
	}
	if len(typesSet) > 0 {
		labelMap := map[string]string{
			"materials":     "уроки",
			"consultations": "встречи",
			"attachments":   "анализы",
			"documents":     "назначения",
			"chat":          "чат",
			"questions":     "вопросы MAX",
		}
		var names []string
		for _, k := range []string{"materials", "consultations", "attachments", "documents", "chat", "questions"} {
			if typesSet[k] {
				names = append(names, labelMap[k])
			}
		}
		if len(names) > 0 {
			parts = append(parts, "разделы: "+strings.Join(names, ", "))
		}
	}
	if hasExplicitIDs {
		parts = append(parts, "точечный выбор записей")
	}
	return "Выборочное саммари (" + strings.Join(parts, "; ") + ")"
}

func generateTemplatedPatientSummary(ctx context.Context, cfg config.Config, input PatientContextForAI) (PatientSummaryOutput, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.LLMProvider))
	scope := nonEmpty(input.ScopeLabel, "Все материалы и сведения профиля")
	mode := nonEmpty(input.GenerationMode, "full")
	tplCode := nonEmpty(input.TemplateCode, DefaultPatientSummaryTemplateCode)

	maskedLessons := MaskPII(strings.Join(input.CompletedLessons, "\n"), input.FullName)
	maskedInProg := MaskPII(strings.Join(input.InProgressLessons, "\n"), input.FullName)
	maskedConsults := MaskPII(strings.Join(input.Consultations, "\n"), input.FullName)
	maskedAtts := MaskPII(strings.Join(input.Attachments, "\n"), input.FullName)
	maskedDocs := MaskPII(strings.Join(input.Prescriptions, "\n"), input.FullName)
	maskedChat := MaskPII(strings.Join(input.ChatMessages, "\n"), input.FullName)
	maskedQuestions := MaskPII(strings.Join(input.RecentQuestions, "\n"), input.FullName)
	maskedDelta := MaskPII(strings.Join(input.NewDeltaItems, "\n"), input.FullName)
	maskedPrev := MaskPII(input.PreviousSummaryText, input.FullName)

	if provider == "yandexgpt" || provider == "gigachat" {
		sysPrompt := `Ты медицинский ИИ-ассистент проекта сопровождения беременности «Авокадо».
Сформируй структурированное клиническое саммари профиля пациентки СТРОГО по следующему шаблону из 6 разделов (сохраняй заголовки ## 1. ... — ## 6. ...):
## 1. Паспорт срока и статус ведения
## 2. Что нового с прошлого саммари (Динамика)
## 3. Клиническая картина, анализы и назначения
## 4. Коммуникация: чат с врачом и вопросы из MAX
## 5. Освоение программы курса «Авокадо»
## 6. Фокус внимания и план к следующей встрече

Правила:
- Если передан блок «Предыдущее саммари», возьми его за основу и актуализируй новыми необработанными данными, явно выделив изменения в разделе 2.
- Если режим «full» (полный пересчёт), не опирайся на старое саммари, а синтезируй картину заново по всем переданным материалам.
- Не выдумывай диагнозы и не включай персональные данные (152-ФЗ).`

		var userPrompt strings.Builder
		fmt.Fprintf(&userPrompt, "Режим: %s (%s)\nСрок/статус: %s\nТариф: %s (%s)\n",
			mode, scope, nonEmpty(input.PregnancyStatus, "не указан"), nonEmpty(input.TariffTitle, "не выбран"), input.SubscriptionInfo)
		if input.UpcomingSlotInfo != "" {
			fmt.Fprintf(&userPrompt, "Предстоящая консультация: %s\n", MaskPII(input.UpcomingSlotInfo, input.FullName))
		}
		if input.UsePreviousSummary && maskedPrev != "" {
			fmt.Fprintf(&userPrompt, "\n=== ПРЕДЫДУЩЕЕ САММАРИ (от %s) ===\n%s\n", input.PreviousSummaryAt, limitRunes(maskedPrev, 2500))
			fmt.Fprintf(&userPrompt, "\n=== НОВЫЕ НЕОБРАБОТАННЫЕ СОБЫТИЯ (%d) ===\n%s\n", input.NewEventsCount, nonEmpty(maskedDelta, "Новых событий нет"))
		}
		fmt.Fprintf(&userPrompt, "\n=== МАТЕРИАЛЫ ПРОФИЛЯ ===\nПройденные уроки:\n%s\nВ плане изучения:\n%s\nКонсультации:\n%s\nФайлы анализов:\n%s\nНазначения и выписки:\n%s\nСообщения чата:\n%s\nВопросы MAX:\n%s\n",
			nonEmpty(maskedLessons, "нет"), nonEmpty(maskedInProg, "нет"), nonEmpty(maskedConsults, "нет"),
			nonEmpty(maskedAtts, "нет"), nonEmpty(maskedDocs, "нет"), nonEmpty(maskedChat, "нет"), nonEmpty(maskedQuestions, "нет"))

		if raw, modelName, err := callConfiguredLLM(ctx, cfg, sysPrompt, userPrompt.String()); err == nil && strings.TrimSpace(raw) != "" {
			return PatientSummaryOutput{
				GenerationMode:     mode,
				TemplateCode:       tplCode,
				TemplateTitle:      DefaultPatientSummaryTemplateTitle,
				SummaryText:        strings.TrimSpace(raw),
				LLMModelName:       modelName,
				EventsCount:        input.EventsCount,
				NewEventsCount:     input.NewEventsCount,
				BaseSummaryID:      input.BaseSummaryID,
				ProcessedEventKeys: input.ProcessedEventKeys,
				ScopeLabel:         scope,
			}, nil
		}
	}

	// Deterministic 6-section clinical template synthesis
	var sb strings.Builder

	// Section 1: Паспорт срока и статус ведения
	sb.WriteString("## 1. Паспорт срока и статус ведения\n")
	sb.WriteString(fmt.Sprintf("• Срок / статус беременности: %s.\n", nonEmpty(input.PregnancyStatus, "не указан")))
	sb.WriteString(fmt.Sprintf("• Программа сопровождения: %s (%s).\n", nonEmpty(input.TariffTitle, "тариф не выбран"), nonEmpty(input.SubscriptionInfo, "без подписки")))
	sb.WriteString(fmt.Sprintf("• Прогресс обучения: пройдено уроков — %d, в плане изучения — %d.\n", len(input.CompletedLessons), len(input.InProgressLessons)))
	sb.WriteString(fmt.Sprintf("• Режим формирования сводки: %s (всего учтено единиц данных: %d).\n", scope, input.EventsCount))
	if input.UpcomingSlotInfo != "" {
		sb.WriteString("• " + MaskPII(input.UpcomingSlotInfo, input.FullName) + ".\n")
	}

	// Section 2: Что нового с прошлого саммари (Динамика)
	sb.WriteString("\n## 2. Что нового с прошлого саммари (Динамика)\n")
	if input.UsePreviousSummary && input.PreviousSummaryText != "" {
		sb.WriteString(fmt.Sprintf("• Опорное предыдущее саммари от %s сохранено за основу.\n", nonEmpty(input.PreviousSummaryAt, "предыдущего расчёта")))
		if len(input.NewDeltaItems) == 0 {
			sb.WriteString("• Новых необработанных событий с момента предыдущей сводки не поступало — клиническая и учебная картина стабильна.\n")
		} else {
			sb.WriteString(fmt.Sprintf("• Обработано новых событий с прошлой сводки: %d:\n", len(input.NewDeltaItems)))
			for _, d := range input.NewDeltaItems {
				sb.WriteString("• " + MaskPII(d, input.FullName) + "\n")
			}
		}
	} else if mode == "full" {
		sb.WriteString("• Выполнен полный пересчёт с нуля по всем материалам профиля (предыдущее саммари не использовалось).\n")
		if len(input.NewDeltaItems) > 0 {
			limit := len(input.NewDeltaItems)
			if limit > 4 {
				limit = 4
			}
			sb.WriteString("• Последние ключевые события профиля:\n")
			for _, d := range input.NewDeltaItems[:limit] {
				sb.WriteString("• " + MaskPII(d, input.FullName) + "\n")
			}
		}
	} else {
		sb.WriteString(fmt.Sprintf("• Сформирована целевая выборка (%s), обработано событий: %d.\n", scope, input.EventsCount))
	}

	// Section 3: Клиническая картина, анализы и назначения
	sb.WriteString("\n## 3. Клиническая картина, анализы и назначения\n")
	if len(input.Consultations) == 0 && len(input.Attachments) == 0 && len(input.Prescriptions) == 0 {
		sb.WriteString("• Записей консультаций, загруженных файлов анализов и опубликованных выписок в данной выборке пока нет.\n")
	} else {
		for _, c := range input.Consultations {
			sb.WriteString("• " + MaskPII(c, input.FullName) + "\n")
		}
		for _, a := range input.Attachments {
			sb.WriteString("• " + MaskPII(a, input.FullName) + "\n")
		}
		for _, p := range input.Prescriptions {
			sb.WriteString("• " + MaskPII(p, input.FullName) + "\n")
		}
	}

	// Section 4: Коммуникация: чат с врачом и вопросы из MAX
	sb.WriteString("\n## 4. Коммуникация: чат с врачом и вопросы из MAX\n")
	if len(input.ChatMessages) == 0 && len(input.RecentQuestions) == 0 {
		sb.WriteString("• Обращений во внутреннем чате кабинета и вопросов из бота MAX за выбранный срез не зафиксировано.\n")
	} else {
		if len(input.ChatMessages) > 0 {
			startIdx := 0
			if len(input.ChatMessages) > 5 {
				startIdx = len(input.ChatMessages) - 5
			}
			for _, msg := range input.ChatMessages[startIdx:] {
				sb.WriteString("• Чат кабинета: " + MaskPII(msg, input.FullName) + "\n")
			}
		}
		for _, q := range input.RecentQuestions {
			sb.WriteString("• Бот MAX: " + MaskPII(q, input.FullName) + "\n")
		}
	}

	// Section 5: Освоение программы курса «Авокадо»
	sb.WriteString("\n## 5. Освоение программы курса «Авокадо»\n")
	if len(input.CompletedLessons) == 0 {
		sb.WriteString("• Отмеченных пройденных уроков в выборке пока нет.\n")
	} else {
		for _, l := range input.CompletedLessons {
			sb.WriteString("• Изучено: " + MaskPII(l, input.FullName) + "\n")
		}
	}
	if len(input.InProgressLessons) > 0 {
		sb.WriteString("• Следующий шаг по программе: " + input.InProgressLessons[0] + ".\n")
	}

	// Section 6: Фокус внимания и план к следующей встрече
	sb.WriteString("\n## 6. Фокус внимания и план к следующей встрече\n")
	if input.UpcomingSlotInfo != "" {
		sb.WriteString("• Перед предстоящей консультацией: сверить запрос пациентки и динамику анализов (" + MaskPII(input.UpcomingSlotInfo, input.FullName) + ").\n")
	}
	if len(input.Attachments) > 0 {
		sb.WriteString("• Проверить прикреплённые лабораторные файлы (" + fmt.Sprintf("%d шт.", len(input.Attachments)) + ") и сопоставить с текущими назначениями.\n")
	}
	if len(input.Prescriptions) > 0 {
		sb.WriteString("• Проконтролировать переносимость и выполнение рекомендаций из последней утверждённой выписки.\n")
	} else {
		sb.WriteString("• Поддерживать спокойный темп прохождения уроков по текущему триместру и фиксировать вопросы к врачу в чате кабинета.\n")
	}

	return PatientSummaryOutput{
		GenerationMode:     mode,
		TemplateCode:       tplCode,
		TemplateTitle:      DefaultPatientSummaryTemplateTitle,
		SummaryText:        strings.TrimSpace(sb.String()),
		LLMModelName:       "avocado-clinical-summarizer-v2",
		EventsCount:        input.EventsCount,
		NewEventsCount:     input.NewEventsCount,
		BaseSummaryID:      input.BaseSummaryID,
		ProcessedEventKeys: input.ProcessedEventKeys,
		ScopeLabel:         scope,
	}, nil
}
