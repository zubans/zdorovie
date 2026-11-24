package watermark

import (
	"bytes"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func TestStampPDF_CyrillicAndPageCountPreserved(t *testing.T) {
	// Generate a 2-page Cyrillic master PDF via fpdf
	master := fpdf.New("P", "mm", "A4", "")
	master.AddUTF8FontFromBytes("DejaVu", "", dejaVuRegular)
	master.SetFont("DejaVu", "", 14)
	master.AddPage()
	master.Cell(0, 10, "Модуль 1. Первый триместр — базовая методичка")
	master.AddPage()
	master.Cell(0, 10, "Страница 2. Контрольные анализы")

	var srcBuf bytes.Buffer
	if err := master.Output(&srcBuf); err != nil {
		t.Fatalf("create source pdf: %v", err)
	}

	stamped, err := StampPDF(srcBuf.Bytes(), Input{
		FullName:    "Смирнова Елена Викторовна",
		Email:       "elena@example.ru",
		Phone:       "+79991234567",
		OrderNumber: "ZDOR-1042",
		IssuedAt:    time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("StampPDF failed: %v", err)
	}
	if !bytes.HasPrefix(stamped, []byte("%PDF-")) || len(stamped) <= srcBuf.Len() {
		t.Fatalf("expected valid stamped PDF larger than source, got %d bytes", len(stamped))
	}

	count, err := api.PageCount(bytes.NewReader(stamped), nil)
	if err != nil {
		t.Fatalf("read stamped pdf page count: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 pages preserved, got %d", count)
	}
}
