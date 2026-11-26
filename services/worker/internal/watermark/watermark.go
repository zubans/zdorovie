package watermark

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

//go:embed fonts/DejaVuSans.ttf
var dejaVuRegular []byte

//go:embed fonts/DejaVuSans-Bold.ttf
var dejaVuBold []byte

// Input defines personalization details stamped onto each PDF page and metadata.
type Input struct {
	FullName    string
	Email       string
	Phone       string
	OrderNumber string
	IssuedAt    time.Time
}

// MaskPhone masks middle digits of a Russian phone number while keeping traceability.
func MaskPhone(phone string) string {
	p := strings.TrimSpace(phone)
	if len(p) < 7 {
		return p
	}
	return p[:len(p)-4] + "**" + p[len(p)-2:]
}

// RenderOverlayPDF generates a 1-page transparent A4 PDF overlay with diagonal Cyrillic watermark and bottom footer.
func RenderOverlayPDF(in Input) ([]byte, error) {
	if in.IssuedAt.IsZero() {
		in.IssuedAt = time.Now().UTC()
	}
	doc := fpdf.New("P", "mm", "A4", "")
	doc.AddUTF8FontFromBytes("DejaVu", "", dejaVuRegular)
	doc.AddUTF8FontFromBytes("DejaVu", "B", dejaVuBold)
	doc.SetAutoPageBreak(false, 0)
	doc.AddPage()

	// Diagonal semi-transparent center watermark
	doc.SetAlpha(0.14, "Normal")
	doc.SetFont("DejaVu", "B", 18)
	doc.SetTextColor(45, 106, 79)
	doc.TransformBegin()
	doc.TransformRotate(38, 105, 148)
	doc.SetXY(15, 138)
	doc.CellFormat(180, 9, fmt.Sprintf("%s • %s", in.FullName, in.Email), "", 0, "C", false, 0, "")
	doc.TransformEnd()

	// Secondary subtle diagonal stamp in upper/lower area
	doc.SetAlpha(0.08, "Normal")
	doc.SetFont("DejaVu", "", 11)
	doc.TransformBegin()
	doc.TransformRotate(38, 105, 65)
	doc.SetXY(20, 62)
	doc.CellFormat(170, 6, fmt.Sprintf("Заказ %s • Экземпляр %s", in.OrderNumber, in.Email), "", 0, "C", false, 0, "")
	doc.TransformEnd()

	// Bottom protective footer
	doc.SetAlpha(0.88, "Normal")
	doc.SetY(282)
	doc.SetX(12)
	doc.SetFont("DejaVu", "", 7.5)
	doc.SetTextColor(95, 105, 115)
	footer := fmt.Sprintf(
		"Экземпляр подготовлен исключительно для: %s (%s, %s). Заказ %s от %s. Копирование и распространение запрещено.",
		in.FullName, in.Email, MaskPhone(in.Phone), in.OrderNumber, in.IssuedAt.Format("02.01.2006"),
	)
	doc.MultiCell(186, 3.6, footer, "", "C", false)

	var buf bytes.Buffer
	if err := doc.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// StampPDF applies the personalized Cyrillic watermark overlay to all pages of srcPDF
// and embeds recipient ownership properties into the PDF Info dictionary.
func StampPDF(srcPDF []byte, in Input) ([]byte, error) {
	overlayBytes, err := RenderOverlayPDF(in)
	if err != nil {
		return nil, fmt.Errorf("render watermark overlay: %w", err)
	}

	tmpOverlay, err := os.CreateTemp("", "zdorovie_wm_*.pdf")
	if err != nil {
		return nil, fmt.Errorf("create overlay temp file: %w", err)
	}
	overlayPath := tmpOverlay.Name()
	defer os.Remove(overlayPath)

	if _, err := tmpOverlay.Write(overlayBytes); err != nil {
		tmpOverlay.Close()
		return nil, err
	}
	if err := tmpOverlay.Close(); err != nil {
		return nil, err
	}

	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed

	wm, err := api.PDFWatermark(overlayPath+":1", "scale:1.0 rel, rot:0, opacity:1.0", true, false, types.POINTS)
	if err != nil {
		return nil, fmt.Errorf("build pdfcpu watermark config: %w", err)
	}

	var stamped bytes.Buffer
	if err := api.AddWatermarks(bytes.NewReader(srcPDF), &stamped, nil, wm, conf); err != nil {
		return nil, fmt.Errorf("apply pdfcpu watermarks: %w", err)
	}

	var finalBuf bytes.Buffer
	props := map[string]string{
		"Author":  "Больше чем здоровье • Беременность без тревоги",
		"Subject": fmt.Sprintf("Личный экземпляр: %s (%s) • Заказ %s", in.FullName, in.Email, in.OrderNumber),
		"Creator": "Zdorovie Watermark Worker",
	}
	if err := api.AddProperties(bytes.NewReader(stamped.Bytes()), &finalBuf, props, conf); err != nil {
		// Return watermarked bytes even if optional metadata property add fails
		return stamped.Bytes(), nil
	}
	return finalBuf.Bytes(), nil
}
