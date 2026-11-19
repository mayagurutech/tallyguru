package mapping

import "testing"

func TestDecide(t *testing.T) {
	lakh := int64(100000 * 100)
	cases := []struct {
		name string
		s    Suggestion
		e    *Evidence
		tag  string
		ask  bool
	}{
		{"unused", Suggestion{TDS, "none", Low, ""}, nil, "none", false},
		{"flags never asked", Suggestion{Flag, "creditor", Medium, ""}, &Evidence{Entries: 9}, "creditor", false},
		{"tax by sales use", Suggestion{Tax, "igst output", Low, ""}, &Evidence{Entries: 9, WithSales: 8, WithCost: 1}, "igst output", false},
		{"tax by purchase use", Suggestion{Tax, "cgst output", Low, ""}, &Evidence{Entries: 9, WithCost: 8}, "cgst input", false},
		{"tds deducted confirms rule", Suggestion{TDS, "194J(b) professional", High, ""}, &Evidence{Entries: 3, MaxYear: lakh, TDSShare: 1}, "194J(b) professional", false},
		{"tds deducted, section unclear", Suggestion{TDS, "none", Low, ""}, &Evidence{Entries: 3, MaxYear: lakh, TDSShare: 0.9}, "none", false},
		{"small unknown expense", Suggestion{TDS, "none", Low, ""}, &Evidence{Entries: 3, MaxYear: 10000 * 100}, "none", false},
		{"big unknown expense", Suggestion{TDS, "none", Low, "no rule"}, &Evidence{Entries: 3, MaxYear: lakh}, "none", false},
		{"rent without TDS is an audit finding", Suggestion{TDS, "194I(b) building rent", High, ""}, &Evidence{Entries: 12, MaxYear: 5 * lakh}, "194I(b) building rent", false},
		{"gst booked", Suggestion{GST, "eligible", Low, ""}, &Evidence{Entries: 3, MaxYear: lakh, GSTShare: 1}, "eligible", false},
		{"no gst booked", Suggestion{GST, "eligible", Low, ""}, &Evidence{Entries: 3, MaxYear: lakh}, "exempt", false},
		{"credit on blocked item", Suggestion{GST, "blocked 17(5)", Medium, ""}, &Evidence{Entries: 3, MaxYear: lakh, GSTShare: 1}, "blocked 17(5)", true},
	}
	for _, c := range cases {
		d := Decide(c.s, c.e)
		if d.Tag != c.tag || d.Ask != c.ask {
			t.Errorf("%s: got %q ask=%v, want %q ask=%v (%s)", c.name, d.Tag, d.Ask, c.tag, c.ask, d.Reason)
		}
	}
}
