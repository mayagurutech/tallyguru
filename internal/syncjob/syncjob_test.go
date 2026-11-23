package syncjob

import "testing"

func TestParseFrom(t *testing.T) {
	for in, want := range map[string]string{
		"all": "", "": "", "2025-04-01": "2025-04-01", "01-04-2025": "2025-04-01",
		"2025-26": "2025-04-01", "FY 25-26": "2025-04-01", "2025-2026": "2025-04-01", "fy2024-25": "2024-04-01",
	} {
		if got, err := ParseFrom(in); err != nil || got != want {
			t.Errorf("%q: got %q %v, want %q", in, got, err, want)
		}
	}
	if _, err := ParseFrom("last year"); err == nil {
		t.Error("nonsense must be refused")
	}
}
