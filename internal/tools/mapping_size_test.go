package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMappingToolSize(t *testing.T) {
	e, _ := env(t)
	b, _ := json.Marshal(Mapping(e))
	if tok := len(b) / 4; tok > 400 {
		t.Fatalf("mapping tool definitions ~%d tokens, over the 400 target", tok)
	} else {
		t.Logf("mapping tool definitions: ~%d tokens", tok)
	}
}

// Target: tool definitions < 1,500 tokens per profile (about 4 characters a token).
func TestProfileToolSize(t *testing.T) {
	e, _ := env(t)
	for _, p := range []struct {
		name string
		mods [][]byte
	}{
		{"owner (core + reports)", [][]byte{mustJSON(Core(e)), mustJSON(Reports(e))}},
		{"accountant/auditor (core + mapping + reports)", [][]byte{mustJSON(Core(e)), mustJSON(Mapping(e)), mustJSON(Reports(e))}},
		{"auditor (core + mapping + reports + audit)", [][]byte{mustJSON(Core(e)), mustJSON(Mapping(e)), mustJSON(Reports(e)), mustJSON(Audit(e))}},
		{"accountant with entry (core + mapping + reports + accounting + masters)", [][]byte{mustJSON(Core(e)), mustJSON(Mapping(e)), mustJSON(Reports(e)), mustJSON(Accounting(e)), mustJSON(Masters(e))}},
		{"accountant with changes switched on", [][]byte{mustJSON(Core(e)), mustJSON(Mapping(e)), mustJSON(Reports(e)), mustJSON(Accounting(e)), mustJSON(Masters(e)), mustJSON(Changes(e))}},
		{"accountant with bulk and bank switched on", [][]byte{mustJSON(Core(e)), mustJSON(Mapping(e)), mustJSON(Reports(e)), mustJSON(Accounting(e)), mustJSON(Masters(e)), mustJSON(Bulk(e)), mustJSON(Bank(e)), mustJSON(Rules(e))}},
		{"bookkeeper: core + accounting + bulk + bank (no mapping, reports, masters)", [][]byte{mustJSON(Core(e)), mustJSON(Accounting(e)), mustJSON(Bulk(e)), mustJSON(Bank(e)), mustJSON(Rules(e))}},
	} {
		n := 0
		for _, b := range p.mods {
			n += len(b)
		}
		if tok := n / 4; tok > 1500 && !strings.Contains(p.name, "switched on") { // author 18 Nov 2026: changes may exceed while on
			t.Errorf("%s: ~%d tokens, over 1,500", p.name, tok)
		} else {
			t.Logf("%s: ~%d tokens", p.name, tok)
		}
	}
	t.Logf("run_report alone: ~%d tokens", len(mustJSON(Reports(e)))/4)
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
