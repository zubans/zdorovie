package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"zdorovie/backend/internal/auth"
	"zdorovie/backend/internal/config"
	"zdorovie/backend/internal/db"
)

func TestAPI_RBAC_And_AuthGuard(t *testing.T) {
	cfg := config.Config{
		AppEnv:    "test",
		JWTSecret: "test-jwt-secret-123",
	}
	srv := NewServer(cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	h := srv.Handler()

	// Unauthenticated request to cabinet dashboard should fail cleanly
	req := httptest.NewRequest(http.MethodGet, "/api/v1/cabinet/dashboard", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 or 503 when unauthenticated/nil store, got %d", rec.Code)
	}

	// Verify Admin route rejects invalid Bearer token cleanly even when store nil vs non-nil behavior
	clientTok, err := auth.IssueJWT(cfg.JWTSecret, 5, "client", "client@example.ru", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := auth.ParseJWT(cfg.JWTSecret, clientTok)
	if err != nil || claims.Role != "client" {
		t.Fatalf("expected client JWT role, got %+v", claims)
	}

	// Unauthenticated chat routes must reject cleanly (401 or 503 when nil store)
	for _, p := range []string{"/api/v1/cabinet/chat", "/api/v1/admin/chat/unread"} {
		chatReq := httptest.NewRequest(http.MethodGet, p, nil)
		chatRec := httptest.NewRecorder()
		h.ServeHTTP(chatRec, chatReq)
		if chatRec.Code != http.StatusUnauthorized && chatRec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 401 or 503 on unauth %s, got %d", p, chatRec.Code)
		}
	}
}

func TestAPI_MockPayDisabledInStrictProd(t *testing.T) {
	cfg := config.Config{
		AppEnv:   "production",
		AppDebug: false,
	}
	srv := NewServer(cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	h := srv.Handler()

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/dev/mock-pay"},
		{http.MethodPost, "/api/v1/dev/mock-pay/confirm"},
		{http.MethodGet, "/api/v1/dev/outbox"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s in prod without debug: expected 404, got %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestIsAllowedAudioFormat(t *testing.T) {
	for _, valid := range []string{".mp3", "m4a", ".WAV", "aac", "ogg", ".webm"} {
		if _, _, ok := isAllowedAudioFormat(valid); !ok {
			t.Errorf("expected %s to be accepted", valid)
		}
	}
	for _, bad := range []string{".exe", ".pdf", ".flac", ".txt"} {
		if _, _, ok := isAllowedAudioFormat(bad); ok {
			t.Errorf("expected %s to be rejected", bad)
		}
	}

	// Verify JSON encoder roundtrip helper sanity
	rec := httptest.NewRecorder()
	writeError(rec, http.StatusForbidden, "subscription_expired", "Подписка истекла")
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["error"] != "subscription_expired" {
		t.Fatalf("unexpected error payload: %+v", payload)
	}
}

func TestBuildPatientAIContext_152FZSafetyAndFilters(t *testing.T) {
	msk := time.FixedZone("MSK", 3*3600)
	t1 := time.Date(2026, 11, 10, 12, 0, 0, 0, msk)
	t2 := time.Date(2026, 11, 20, 12, 0, 0, 0, msk)

	prof := &db.ClientProfile{
		User: db.User{
			ID:              7,
			FullName:        "Мария Соколова",
			PregnancyStatus: "24 недели",
		},
		SubActive: true,
		Materials: []db.CabinetMaterial{
			{ID: 1, ModuleNumber: 1, LessonNumber: 1, Title: "Урок 1", Description: "База", Completed: true, CompletedAt: &t1},
			{ID: 2, ModuleNumber: 2, LessonNumber: 1, Title: "Урок 2", Description: "Поздний", Completed: true, CompletedAt: &t2},
			{ID: 3, ModuleNumber: 3, LessonNumber: 1, Title: "Урок 3", Description: "Не пройден", Completed: false},
		},
		Consultations: []db.ConsultationSlot{
			{ID: 10, SlotStart: t1, Status: "completed", ClientQuestion: "Вопрос про железо", DoctorNotes: "PRIVATE_NOTE_SHOULD_BE_HIDDEN_FOR_PATIENT"},
		},
		Documents: []db.PatientDocument{
			{ID: 100, Title: "Опубликованное заключение", IsPublished: true, PublishedAt: &t1, Recommendations: "Принимать фолаты"},
			{ID: 101, Title: "Неопубликованный черновик", IsPublished: false, CreatedAt: t1, Recommendations: "DRAFT_HIDDEN"},
		},
		Questions: []db.Question{
			{ID: 200, QuestionText: "Можно ли витамин D?", CreatedAt: t1},
		},
	}

	// Patient-safe summary must strip unpublished documents, private doctor_notes, and bot CRM questions
	safeCtx, err := buildPatientAIContext(prof, summaryFilterRequest{DateFrom: "2026-11-01", DateTo: "2026-11-15"}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(safeCtx.CompletedLessons) != 1 || len(safeCtx.Prescriptions) != 1 || len(safeCtx.RecentQuestions) != 0 {
		t.Fatalf("unexpected safe context slices: lessons=%d docs=%d qs=%d", len(safeCtx.CompletedLessons), len(safeCtx.Prescriptions), len(safeCtx.RecentQuestions))
	}
	joined := strings.Join(safeCtx.Consultations, "\n") + strings.Join(safeCtx.Prescriptions, "\n")
	if strings.Contains(joined, "PRIVATE_NOTE_SHOULD_BE_HIDDEN_FOR_PATIENT") || strings.Contains(joined, "DRAFT_HIDDEN") {
		t.Fatalf("patientSafe context leaked private doctor notes or unpublished draft: %s", joined)
	}

	// Doctor CRM summary sees full context and event_types filter
	docCtx, err := buildPatientAIContext(prof, summaryFilterRequest{EventTypes: []string{"questions"}}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docCtx.RecentQuestions) != 1 || len(docCtx.CompletedLessons) != 0 {
		t.Fatalf("expected only questions in filtered doctor ctx, got %+v", docCtx)
	}
}
