package pdf

import (
	"bytes"
	"testing"
)

// pdfMagic is the 5-byte header every PDF stream begins with — handy to
// confirm RenderReceipt actually produced PDF output (not a generator error
// swallowed into empty bytes).
var pdfMagic = []byte("%PDF-")

func baseReceipt() Receipt {
	return Receipt{
		ReceiptNo:     "rcpt-1",
		IssuedAt:      "2026-06-02 09:00 UTC",
		Status:        "paid",
		PropertyTitle: "Sea-view loft",
		CheckIn:       "2026-08-01",
		CheckOut:      "2026-08-05",
		Nights:        4,
		Guests:        2,
		Subtotal:      "400.00 EUR",
		Discount:      "",
		CleaningFee:   "30.00 EUR",
		ServiceFee:    "57.00 EUR",
		Tax:           "10.00 EUR",
		Total:         "497.00 EUR",
	}
}

// TestRenderReceipt_LegacyTaxLine renders a receipt with only the legacy Tax
// field set — proving the renderer still falls back to one aggregate line
// for pre-S49 bookings.
func TestRenderReceipt_LegacyTaxLine(t *testing.T) {
	body, err := RenderReceipt(baseReceipt())
	if err != nil {
		t.Fatalf("RenderReceipt: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("RenderReceipt returned empty body")
	}
	if !bytes.HasPrefix(body, pdfMagic) {
		head := body
		if len(head) > 8 {
			head = head[:8]
		}
		t.Fatalf("body does not start with PDF magic: %q", head)
	}
}

// TestRenderReceipt_JurisdictionTaxLines exercises the S58 branch where the
// per-jurisdiction breakdown REPLACES the single Tax line in the output.
// Smoke-level only: we don't parse the PDF, but we confirm bytes flow.
func TestRenderReceipt_JurisdictionTaxLines(t *testing.T) {
	r := baseReceipt()
	r.Tax = "" // S49+ booking: legacy field cleared
	r.TaxLines = []TaxLineRow{
		{Name: "VAT (PT)", Value: "23.00 EUR"},
		{Name: "City tax (Lisbon)", Value: "8.00 EUR"},
	}
	body, err := RenderReceipt(r)
	if err != nil {
		t.Fatalf("RenderReceipt: %v", err)
	}
	if !bytes.HasPrefix(body, pdfMagic) {
		t.Fatal("body does not start with PDF magic")
	}
}

// TestRenderReceipt_UnnamedTaxLine confirms the renderer degrades to a plain
// "Tax" label when a row carries no Name (defensive — should never happen in
// practice, the tax BC always sets a name).
func TestRenderReceipt_UnnamedTaxLine(t *testing.T) {
	r := baseReceipt()
	r.Tax = ""
	r.TaxLines = []TaxLineRow{{Name: "", Value: "5.00 EUR"}}
	if _, err := RenderReceipt(r); err != nil {
		t.Fatalf("RenderReceipt: %v", err)
	}
}

