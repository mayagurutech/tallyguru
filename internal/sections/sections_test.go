package sections

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCite(t *testing.T) {
	if err := Load(""); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"40A(3)":  "s. 36(4) (40A(3) of 1961) (verify)",
		"269T":    "269T of 1961 (2025 section: verify)",
		"unknown": "unknown of 1961 (2025 section: verify)",
	} {
		if got := Cite(key); got != want {
			t.Errorf("%s: %q, want %q", key, got, want)
		}
	}
}

func TestUserOverride(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, FileName), []byte("\xef\xbb\xbfkey,s2025,s1961,subject,source,verified\n40A(3),36(4),40A(3),cash,Act,yes\n"), 0o600)
	if err := Load(root); err != nil {
		t.Fatal(err)
	}
	defer Load("")
	if got := Cite("40A(3)"); got != "s. 36(4) (40A(3) of 1961)" {
		t.Errorf("verified row: %q", got)
	}
}
