package gst

import "testing"

func TestProblem(t *testing.T) {
	// 27AAPFU0939F1ZV is widely published as a valid example GSTIN.
	for g, want := range map[string]string{
		"27AAPFU0939F1ZV":   "",
		"27aapfu0939f1zv":   "",
		"":                  "",
		"27AAPFU0939F1ZW":   "check digit does not match (typing mistake likely)",
		"27AAPFU0939F1Z":    "not 15 characters",
		"27AAPF10939F1ZV":   "not in the regular GSTIN format",
		"00AAPFU0939F1ZV":   "unknown state code 00",
		" 27AAPFU0939F1ZV ": "",
	} {
		if got := Problem(g); got != want {
			t.Errorf("%q: %q, want %q", g, got, want)
		}
	}
}

func TestStates(t *testing.T) {
	if StateOf("24ABCDE1234F1Z5") != "Gujarat" || StateOf("2") != "" {
		t.Error("StateOf")
	}
	for _, c := range [][2]string{{"Jammu & Kashmir", "Jammu and Kashmir"}, {"Orissa", "Odisha"}, {"GUJARAT", "Gujarat"}, {"Madhya Pradesh", "madhya pradesh"}} {
		if !SameState(c[0], c[1]) {
			t.Errorf("%q vs %q", c[0], c[1])
		}
	}
	if SameState("Gujarat", "Maharashtra") || SameState("", "") {
		t.Error("different or empty states must not match")
	}
}
