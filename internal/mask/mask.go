// Package mask hides personal identifiers in text before it leaves the server for the AI.
// It is applied to every tool answer and error, so new tools are covered automatically.
// GSTINs are left whole (needed for GST work); a PAN inside a GSTIN is not masked.
package mask

import (
	"regexp"
	"strings"
)

var re = regexp.MustCompile(
	`\b(\d{2}[A-Z]{5}\d{4}[A-Z][1-9A-Z]Z[0-9A-Z])\b` + // 1 GSTIN: keep
		`|\b([A-Z]{5}\d{4}[A-Z])\b` + // 2 PAN
		`|\b(\d{4}[ -]\d{4}[ -]\d{4})\b` + // 3 Aadhaar (grouped)
		`|(?:\+91[ -]?|\b0|\b)([6-9]\d{9})\b` + // 4 Indian mobile (+91 / 0 prefix optional)
		`|\b(\d{9,18})\b`) // 5 bank account / other long numbers

// keep shows only the last n characters.
func keep(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.Repeat("X", len(s)-n) + s[len(s)-n:]
}

// Text masks identifiers in s.
func Text(s string) string {
	return re.ReplaceAllStringFunc(s, func(m string) string {
		g := re.FindStringSubmatch(m)
		switch {
		case g[1] != "":
			return m
		case g[2] != "":
			return keep(g[2], 4) // XXXXXX234F
		case g[3] != "":
			d := strings.NewReplacer(" ", "", "-", "").Replace(g[3])
			return "XXXX-XXXX-" + d[8:]
		case g[4] != "":
			return "XXXXXX" + g[4][6:] // mobile: last 4
		default:
			return keep(g[5], 4) // account no.: last 4
		}
	})
}
