// Package pdf renders documents (currently payment receipts) to PDF bytes using
// a pure-Go generator, so it works without external services.
package pdf

import (
	"bytes"

	"github.com/go-pdf/fpdf"
)

// Receipt is the plain-data input to the receipt renderer. The interface layer
// maps the application read-model onto it, keeping this package free of any
// domain/application dependency.
type Receipt struct {
	ReceiptNo     string
	IssuedAt      string
	Status        string
	PropertyTitle string
	CheckIn       string
	CheckOut      string
	Nights        int
	Guests        int
	Subtotal      string
	CleaningFee   string
	ServiceFee    string
	Total         string
}

// RenderReceipt produces an A4 PDF receipt and returns its bytes.
func RenderReceipt(r Receipt) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(20, 20, 20)
	pdf.AddPage()

	// Header.
	pdf.SetFont("Helvetica", "B", 22)
	pdf.SetTextColor(255, 56, 92) // AirHost pink
	pdf.CellFormat(0, 12, "AirHost", "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 11)
	pdf.SetTextColor(90, 90, 90)
	pdf.CellFormat(0, 7, "Payment receipt", "", 1, "L", false, 0, "")
	pdf.Ln(4)

	// Meta.
	pdf.SetTextColor(40, 40, 40)
	pdf.SetFont("Helvetica", "", 10)
	meta := func(label, value string) {
		pdf.SetFont("Helvetica", "B", 10)
		pdf.CellFormat(40, 6, label, "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 10)
		pdf.CellFormat(0, 6, value, "", 1, "L", false, 0, "")
	}
	meta("Receipt no.", r.ReceiptNo)
	meta("Issued", r.IssuedAt)
	meta("Status", r.Status)
	pdf.Ln(4)

	// Stay summary.
	pdf.SetFont("Helvetica", "B", 12)
	pdf.CellFormat(0, 8, r.PropertyTitle, "B", 1, "L", false, 0, "")
	pdf.Ln(2)
	meta("Check-in", r.CheckIn)
	meta("Check-out", r.CheckOut)
	meta("Nights", itoa(r.Nights))
	meta("Guests", itoa(r.Guests))
	pdf.Ln(6)

	// Price breakdown table.
	line := func(label, value string, bold bool) {
		style := ""
		if bold {
			style = "B"
		}
		pdf.SetFont("Helvetica", style, 11)
		pdf.CellFormat(120, 8, label, "", 0, "L", false, 0, "")
		pdf.CellFormat(0, 8, value, "", 1, "R", false, 0, "")
	}
	line("Subtotal", r.Subtotal, false)
	line("Cleaning fee", r.CleaningFee, false)
	line("Service fee", r.ServiceFee, false)
	pdf.SetDrawColor(200, 200, 200)
	pdf.Line(20, pdf.GetY(), 190, pdf.GetY())
	pdf.Ln(1)
	line("Total", r.Total, true)

	pdf.Ln(12)
	pdf.SetFont("Helvetica", "I", 9)
	pdf.SetTextColor(140, 140, 140)
	pdf.MultiCell(0, 5, "Thank you for booking with AirHost. This receipt was generated automatically; no signature is required.", "", "L", false)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
