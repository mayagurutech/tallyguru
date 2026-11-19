package mask

import "testing"

func TestText(t *testing.T) {
	cases := map[string]string{
		"PAN ABCDE1234F":           "PAN XXXXXX234F",
		"GSTIN 27ABCDE1234F1Z5 ok": "GSTIN 27ABCDE1234F1Z5 ok",
		"call 9876543210 now":      "call XXXXXX3210 now",
		"call +91 9876543210":      "call XXXXXX3210",
		"call 09876543210":         "call XXXXXX3210",
		"a/c 50100123456789 HDFC":  "a/c XXXXXXXXXX6789 HDFC",
		"Aadhaar 1234 5678 9012":   "Aadhaar XXXX-XXXX-9012",
		// Must NOT change: formatted amounts, dates, voucher numbers, short numbers.
		"Closing: 4,13,360.00 Dr":              "Closing: 4,13,360.00 Dr",
		"12,34,56,789.01 Cr":                   "12,34,56,789.01 Cr",
		"Apr 2026 | 6 | 30,000.00 | 79,440.00": "Apr 2026 | 6 | 30,000.00 | 79,440.00",
		"2026-04-01 to 2026-06-30":             "2026-04-01 to 2026-06-30",
		"Invoice TMCP/SA/001, PIN 452001":      "Invoice TMCP/SA/001, PIN 452001",
	}
	for in, want := range cases {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}
