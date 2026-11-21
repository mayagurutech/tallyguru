package i18n

import (
	"strings"
	"testing"

	"github.com/mayagurutech/tallyguru/internal/auth"
)

func TestEveryKeyHasEnglishAndHindi(t *testing.T) {
	for k, m := range msgs {
		if k == "ai_instructions" {
			continue // English needs no instruction
		}
		if m[EN] == "" || m[HI] == "" {
			t.Errorf("%s: missing a language", k)
		}
		if strings.Count(m[EN], "%") != strings.Count(m[HI], "%") {
			t.Errorf("%s: placeholders differ between languages", k)
		}
	}
}

func TestEnglishLegalTextUnchanged(t *testing.T) {
	// The English legal texts are the reference; they must equal the auth package's.
	if T(EN, "self_declaration") != auth.SelfDeclaration {
		t.Fatal("English self-declaration drifted from auth package")
	}
	if T(EN, "notice") != auth.Notice || T(EN, "disclaimer") != auth.Disclaimer || T(EN, "owner_declaration") != auth.OwnerDeclaration {
		t.Fatal("English legal text drifted from auth package")
	}
}

func TestFallback(t *testing.T) {
	if T(Lang("xx"), "forget_done", "d") != "Deleted d." {
		t.Error("unknown language must fall back to English")
	}
	en, hi := T(EN, "ai_instructions"), T(HI, "ai_instructions")
	if !strings.Contains(en, "sync_now") || !strings.Contains(hi, "sync_now") || !strings.Contains(hi, "Hindi") || strings.Contains(en, "Hindi") {
		t.Errorf("AI instructions: connect hint in both, Hindi only for Hindi: %q / %q", en, hi)
	}
}
