package pdf

import (
	"bytes"
	"testing"
	"time"
)

func TestGenerateConsultationSummary_Cyrillic(t *testing.T) {
	data, err := GenerateConsultationSummary(SummaryInput{
		SummaryID:       101,
		DoctorName:      "Елена Соколова, акушер-гинеколог",
		PatientName:     "Анна Кузнецова",
		PatientEmail:    "anna@example.com",
		PatientPhone:    "+79991234567",
		PregnancyStatus: "24 недели",
		ConsultDate:     time.Now(),
		ApprovedAt:      time.Now(),
		Complaints:      "Лёгкая усталость вечером",
		Anamnesis:       "Беременность первая, протекает физиологично",
		Recommendations: "Контроль ферритина, прогулки 30 минут, спокойный сон.",
		DiagnosticsPlan: "Общий анализ крови через 2 недели",
		FollowUp:        "Повторный приём через 14 дней",
	})
	if err != nil {
		t.Fatalf("GenerateConsultationSummary failed: %v", err)
	}
	if len(data) < 2000 || !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("expected valid non-trivial PDF output, got %d bytes", len(data))
	}
}

func TestGenerateStarterMasterPDF(t *testing.T) {
	data, err := GenerateStarterMasterPDF(1, 1, "Модуль 1. Первый триместр", "Базовый чек-лист", "Питание и спокойствие")
	if err != nil {
		t.Fatalf("GenerateStarterMasterPDF failed: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatalf("expected valid PDF header")
	}
	if got := MaskPhone("+79991234567"); got != "+7999123**67" {
		t.Errorf("unexpected masked phone: %s", got)
	}
}
