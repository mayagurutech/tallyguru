package main

import (
	"strings"
	"testing"

	"github.com/mayagurutech/tallyguru/internal/config"
)

func TestSetupHindiThenServerInstructions(t *testing.T) {
	root := t.TempDir()
	// hi, profile 4 (auditor), no toggles, privacy y, read-only default.
	if err := setup(root, strings.NewReader("hi\n4\n\ny\n\n")); err != nil {
		t.Fatal(err)
	}
	c, _ := config.Load(root)
	if c.Language != "hi" || c.Profile != "auditor" || !c.PrivacyMode || !c.ReadOnly {
		t.Fatalf("config: %+v", c)
	}
	s, err := buildServer(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Instructions, "Hindi") {
		t.Error("Hindi chosen but AI not told")
	}
	if s.Filter == nil {
		t.Error("masking must stay on by default")
	}
}

// English gets only the one-line connect hint (kept short: it is sent every session).
func TestSetupEnglishInstructionsShort(t *testing.T) {
	root := t.TempDir()
	if err := setup(root, strings.NewReader("en\n1\n\nn\n\n")); err != nil {
		t.Fatal(err)
	}
	s, _ := buildServer(root)
	if !strings.Contains(s.Instructions, "sync_now") || strings.Contains(s.Instructions, "Hindi") || len(s.Instructions) > 120 {
		t.Errorf("English instructions: %q", s.Instructions)
	}
}

func TestFlagsFirst(t *testing.T) {
	got := strings.Join(flagsFirst([]string{"sync", "-full", "Pilot Client", "-source", "backup"}), "|")
	if got != "-full|-source|backup|sync|Pilot Client" {
		t.Fatalf("got %s", got)
	}
}
