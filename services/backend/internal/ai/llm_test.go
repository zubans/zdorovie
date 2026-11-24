package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	"zdorovie/backend/internal/config"
	"zdorovie/backend/internal/db"
)

func TestMaskPII(t *testing.T) {
	in := "Пациентка Иванова Мария звонила с +7 (999) 123-45-67, почта maria@example.ru, дата 15.05.1994, СНИЛС 123-456-789 00."
	out := MaskPII(in, "Иванова Мария")
	for _, forbidden := range []string{"Иванова", "Мария", "999", "maria@example.ru", "15.05.1994", "123-456-789"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("expected PII %q to be masked, got: %s", forbidden, out)
		}
	}
}

func TestRecommendTemplateAndConclusion(t *testing.T) {
	tpls := []db.SummaryTemplate{
		{Code: "first_trimester_adaptation", Title: "1 триместр", Keywords: "токсикоз,тошнота"},
		{Code: "anemia_ferritin", Title: "Ферритин", Keywords: "ферритин,железо,усталость", RecommendationsTpl: "Базовые шаги по железу"},
	}
	code, reason := RecommendTemplate(context.Background(), config.Config{LLMProvider: "mock"}, tpls, "Низкий ферритин 14 и сильная усталость", "22 нед.")
	if code != "anemia_ferritin" {
		t.Fatalf("expected anemia_ferritin, got %q (reason=%s)", code, reason)
	}

	concl, err := GenerateConclusionFromDescription(context.Background(), config.Config{LLMProvider: "mock"}, &tpls[1], "Назначен контроль ферритина", "", "Ферритин 14", "22 нед.", "Мария")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(concl.Recommendations, "контроль ферритина") {
		t.Fatalf("expected doctor notes in recommendations, got: %s", concl.Recommendations)
	}
}

func TestPatientSummaryFullIncrementalAndPreConsultation(t *testing.T) {
	now := time.Now().UTC()
	profile := &db.ClientProfile{
		User: db.User{
			ID:              10,
			FullName:        "Смирнова Анна",
			PregnancyStatus: "24 недели",
		},
		Subscription: &db.Subscription{
			TariffName: "С обратной связью (6 мес.)",
			ExpiresAt:  now.Add(60 * 24 * time.Hour),
			DaysLeft:   60,
			IsActive:   true,
		},
		SubActive: true,
		Materials: []db.CabinetMaterial{
			{
				Material:  db.Material{ID: 1, ModuleNumber: 1, LessonNumber: 1, Title: "Адаптация и дефициты", Description: "База 1 триместра"},
				Completed: true,
			},
			{
				Material:  db.Material{ID: 2, ModuleNumber: 2, LessonNumber: 1, Title: "Ферритин и отёки", Description: "Второй триместр"},
				Completed: false,
			},
		},
		Consultations: []db.ConsultationSlot{
			{
				ID:             101,
				SlotStart:      now.Add(20 * time.Minute),
				SlotEnd:        now.Add(65 * time.Minute),
				Status:         "booked",
				ClientQuestion: "Разобрать ферритин 18",
				DoctorNotes:    "Обратить внимание на дозировку железа",
			},
		},
		Attachments: []db.ConsultationAttachment{
			{
				ID:        201,
				SlotID:    101,
				FileName:  "OAK_Ferritin_Nov.pdf",
				FileSize:  420 * 1024,
				CreatedAt: now.Add(-10 * time.Minute),
			},
		},
		Documents: []db.PatientDocument{
			{
				ID:              301,
				Title:           "Заключение от 10.11",
				Recommendations: "Мягкая форма железа через день",
				FollowUp:        "Контроль через 4 недели",
				IsPublished:     true,
				CreatedAt:       now.Add(-48 * time.Hour),
			},
		},
		Messages: []db.ChatMessage{
			{
				ID:         401,
				SenderRole: "client",
				Body:       "Сдала анализ, прикрепила к записи",
				CreatedAt:  now.Add(-5 * time.Minute),
			},
		},
		Questions: []db.Question{
			{
				ID:           501,
				QuestionText: "Можно ли пить железо с кофе?",
				AnswerText:   "Разносить минимум на 2 часа",
				Status:       "answered",
				CreatedAt:    now.Add(-72 * time.Hour),
			},
		},
	}

	// 1. Full build from scratch (ignores any previous summary)
	prevDummy := &db.PatientAISummary{
		ID:                 55,
		SummaryText:        "Старое саммари",
		ProcessedEventKeys: []string{"mat:1:done"},
		CreatedAt:          now.Add(-24 * time.Hour),
	}
	fullCtx, err := BuildPatientAIContext(profile, SummaryFilterRequest{Mode: "full"}, prevDummy, nil, false)
	if err != nil {
		t.Fatalf("BuildPatientAIContext full failed: %v", err)
	}
	if fullCtx.UsePreviousSummary {
		t.Fatalf("expected full mode NOT to use previous summary")
	}
	if len(fullCtx.Attachments) != 1 || len(fullCtx.ChatMessages) != 1 {
		t.Fatalf("expected attachments and chat messages collected, got atts=%d chat=%d", len(fullCtx.Attachments), len(fullCtx.ChatMessages))
	}

	fullOut, err := GeneratePatientSummary(context.Background(), config.Config{LLMProvider: "mock"}, fullCtx)
	if err != nil {
		t.Fatalf("GeneratePatientSummary full failed: %v", err)
	}
	for _, heading := range []string{
		"## 1. Паспорт срока и статус ведения",
		"## 2. Что нового с прошлого саммари (Динамика)",
		"## 3. Клиническая картина, анализы и назначения",
		"## 4. Коммуникация: чат с врачом и вопросы из MAX",
		"## 5. Освоение программы курса «Авокадо»",
		"## 6. Фокус внимания и план к следующей встрече",
		"OAK_Ferritin_Nov.pdf",
	} {
		if !strings.Contains(fullOut.SummaryText, heading) {
			t.Fatalf("expected section/content %q in templated summary, got:\n%s", heading, fullOut.SummaryText)
		}
	}

	// 2. Save baseline and add 1 new completed lesson -> incremental pre-consultation summary
	baseline := &db.PatientAISummary{
		ID:                 77,
		SummaryText:        fullOut.SummaryText,
		ProcessedEventKeys: fullOut.ProcessedEventKeys,
		CreatedAt:          now.Add(-15 * time.Minute),
	}
	if unproc := CountUnprocessedProfileEvents(profile, baseline, false); unproc != 0 {
		t.Fatalf("expected 0 unprocessed events immediately after full summary, got %d", unproc)
	}

	// Mark lesson 2 completed -> now 1 new unprocessed event!
	profile.Materials[1].Completed = true
	if unproc := CountUnprocessedProfileEvents(profile, baseline, false); unproc != 1 {
		t.Fatalf("expected 1 new unprocessed event after completing lesson 2, got %d", unproc)
	}

	preCtx, err := BuildPatientAIContext(
		profile,
		SummaryFilterRequest{Mode: "pre_consultation", SlotID: &profile.Consultations[0].ID},
		baseline,
		&profile.Consultations[0],
		false,
	)
	if err != nil {
		t.Fatalf("BuildPatientAIContext pre_consultation failed: %v", err)
	}
	if !preCtx.UsePreviousSummary || preCtx.NewEventsCount != 1 {
		t.Fatalf("expected incremental pre_consultation to use previous summary with 1 new event, got usePrev=%v new=%d",
			preCtx.UsePreviousSummary, preCtx.NewEventsCount)
	}
	preOut, err := GeneratePatientSummary(context.Background(), config.Config{LLMProvider: "mock"}, preCtx)
	if err != nil {
		t.Fatalf("GeneratePatientSummary pre_consultation failed: %v", err)
	}
	if !strings.Contains(preOut.SummaryText, "Обработано новых событий с прошлой сводки: 1") {
		t.Fatalf("expected delta section with 1 new event, got:\n%s", preOut.SummaryText)
	}
}
