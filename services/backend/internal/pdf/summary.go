package pdf

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

//go:embed fonts/DejaVuSans.ttf
var dejaVuRegular []byte

//go:embed fonts/DejaVuSans-Bold.ttf
var dejaVuBold []byte

// SummaryInput contains all fields rendered into an approved consultation PDF.
type SummaryInput struct {
	SummaryID       int64
	DoctorName      string
	PatientName     string
	PatientEmail    string
	PatientPhone    string
	PregnancyStatus string
	ConsultDate     time.Time
	ApprovedAt      time.Time
	Complaints      string
	Anamnesis       string
	Recommendations string
	DiagnosticsPlan string
	FollowUp        string
}

// MaskPhone hides middle digits of a Russian phone number for protective footers.
func MaskPhone(phone string) string {
	p := strings.TrimSpace(phone)
	if len(p) < 7 {
		return p
	}
	return p[:len(p)-4] + "**" + p[len(p)-2:]
}

// GenerateConsultationSummary renders a branded Cyrillic PDF with doctor stamp and patient watermark.
func GenerateConsultationSummary(in SummaryInput) ([]byte, error) {
	doc := fpdf.New("P", "mm", "A4", "")
	doc.AddUTF8FontFromBytes("DejaVu", "", dejaVuRegular)
	doc.AddUTF8FontFromBytes("DejaVu", "B", dejaVuBold)
	doc.SetMargins(18, 18, 18)
	doc.SetAutoPageBreak(true, 24)

	doc.SetTitle(fmt.Sprintf("Медицинские рекомендации №%d", in.SummaryID), true)
	doc.SetAuthor(in.DoctorName, true)
	doc.SetSubject(fmt.Sprintf("Подготовлено для %s (%s)", in.PatientName, in.PatientEmail), true)

	footerText := fmt.Sprintf(
		"Экземпляр подготовлен исключительно для: %s (%s, %s). Заключение №%d от %s. Распространение запрещено.",
		in.PatientName, in.PatientEmail, MaskPhone(in.PatientPhone), in.SummaryID, in.ApprovedAt.Format("02.01.2006"),
	)

	doc.SetFooterFunc(func() {
		// Diagonal translucent watermark in the center of the page
		doc.SetAlpha(0.14, "Normal")
		doc.SetFont("DejaVu", "B", 20)
		doc.SetTextColor(74, 144, 226)
		doc.TransformBegin()
		doc.TransformRotate(38, 105, 148)
		doc.SetXY(20, 140)
		doc.CellFormat(170, 10, fmt.Sprintf("%s • %s", in.PatientName, in.PatientEmail), "", 0, "C", false, 0, "")
		doc.TransformEnd()
		doc.SetAlpha(1.0, "Normal")

		// Bottom protective footer
		doc.SetY(-16)
		doc.SetFont("DejaVu", "", 7.5)
		doc.SetTextColor(110, 118, 129)
		doc.MultiCell(0, 4, footerText, "", "C", false)
	})

	doc.AddPage()

	// Header banner
	doc.SetFillColor(240, 247, 255)
	doc.Rect(18, 16, 174, 24, "F")
	doc.SetXY(22, 20)
	doc.SetFont("DejaVu", "B", 13)
	doc.SetTextColor(25, 74, 130)
	doc.CellFormat(0, 6, "Медицинские рекомендации по итогам консультации", "", 1, "L", false, 0, "")
	doc.SetX(22)
	doc.SetFont("DejaVu", "", 9.5)
	doc.SetTextColor(65, 85, 105)
	doc.CellFormat(0, 6, fmt.Sprintf("Авторский интенсив «Беременность без тревоги» • Врач: %s", in.DoctorName), "", 1, "L", false, 0, "")

	doc.Ln(8)

	// Metadata box
	doc.SetFont("DejaVu", "", 9.5)
	doc.SetTextColor(30, 41, 59)
	metaLines := []string{
		fmt.Sprintf("Пациентка: %s (%s)", in.PatientName, in.PatientEmail),
		fmt.Sprintf("Срок / статус: %s", fallbackDash(in.PregnancyStatus)),
		fmt.Sprintf("Дата консультации: %s   |   Утверждено врачом: %s", in.ConsultDate.Format("02.01.2006 15:04"), in.ApprovedAt.Format("02.01.2006 15:04")),
	}
	for _, line := range metaLines {
		doc.CellFormat(0, 5.5, line, "", 1, "L", false, 0, "")
	}
	doc.Ln(4)

	sections := []struct {
		title string
		body  string
	}{
		{"1. Основные жалобы и симптомы", in.Complaints},
		{"2. Анамнез и текущие показатели", in.Anamnesis},
		{"3. Рекомендации по образу жизни, питанию и нутритивной поддержке", in.Recommendations},
		{"4. План лабораторных исследований и чек-апов", in.DiagnosticsPlan},
		{"5. Контроль и дальнейшее наблюдение", in.FollowUp},
	}

	for _, sec := range sections {
		if strings.TrimSpace(sec.body) == "" {
			continue
		}
		doc.SetFont("DejaVu", "B", 10.5)
		doc.SetTextColor(25, 74, 130)
		doc.CellFormat(0, 7, sec.title, "", 1, "L", false, 0, "")
		doc.SetFont("DejaVu", "", 9.5)
		doc.SetTextColor(30, 41, 59)
		doc.MultiCell(0, 5.2, sec.body, "", "L", false)
		doc.Ln(3)
	}

	// Doctor verification stamp
	doc.Ln(4)
	y := doc.GetY()
	if y > 245 {
		doc.AddPage()
		y = doc.GetY()
	}
	doc.SetDrawColor(25, 74, 130)
	doc.SetLineWidth(0.5)
	doc.Rect(116, y, 76, 24, "D")
	doc.SetXY(120, y+4)
	doc.SetFont("DejaVu", "B", 8.5)
	doc.SetTextColor(25, 74, 130)
	doc.CellFormat(68, 5, "УТВЕРЖДЕНО ВРАЧОМ", "", 2, "C", false, 0, "")
	doc.SetFont("DejaVu", "", 8)
	doc.CellFormat(68, 5, in.DoctorName, "", 2, "C", false, 0, "")
	doc.CellFormat(68, 5, fmt.Sprintf("ID заключения: ZDOR-SUM-%d • %s", in.SummaryID, in.ApprovedAt.Format("02.01.2006")), "", 0, "C", false, 0, "")

	var out bytes.Buffer
	if err := doc.Output(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// GenerateStarterMasterPDF creates a clean starter educational PDF for a course module.
func GenerateStarterMasterPDF(moduleNum, lessonNum int, title, description, body string) ([]byte, error) {
	doc := fpdf.New("P", "mm", "A4", "")
	doc.AddUTF8FontFromBytes("DejaVu", "", dejaVuRegular)
	doc.AddUTF8FontFromBytes("DejaVu", "B", dejaVuBold)
	doc.SetMargins(18, 20, 18)
	doc.AddPage()

	doc.SetFont("DejaVu", "B", 14)
	doc.SetTextColor(25, 74, 130)
	doc.MultiCell(0, 7, fmt.Sprintf("Модуль %d. Урок %d: %s", moduleNum, lessonNum, title), "", "L", false)
	doc.Ln(4)
	doc.SetFont("DejaVu", "", 10)
	doc.SetTextColor(71, 85, 105)
	doc.MultiCell(0, 5.5, description, "", "L", false)
	doc.Ln(4)
	doc.SetTextColor(30, 41, 59)
	doc.MultiCell(0, 5.5, body, "", "L", false)

	var out bytes.Buffer
	if err := doc.Output(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func fallbackDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "не указано"
	}
	return s
}
