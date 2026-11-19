package statuspage

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mayagurutech/tallyguru/internal/config"
)

func TestAgreeAndApply(t *testing.T) {
	root := t.TempDir()
	s, err := LoadSettings(root)
	if err != nil || !s.NeedAgree {
		t.Fatalf("new install must ask to agree: %+v %v", s.NeedAgree, err)
	}
	if err := Agree(root, time.Now()); err != nil {
		t.Fatal(err)
	}
	if s, _ := LoadSettings(root); s.NeedAgree {
		t.Error("still asks after agreeing")
	}
	p := "auditor"
	c, err := Apply(root, Change{Profile: &p})
	if err != nil || !c.Modules["audit"] || !c.ReadOnly {
		t.Fatalf("auditor preset: %+v %v", c, err)
	}
	c, _ = Apply(root, Change{Modules: map[string]bool{"bank": true}})
	if !c.Modules["bank"] || !strings.HasSuffix(c.Profile, "*") {
		t.Errorf("module switch: %+v", c)
	}
	if _, err := Apply(root, Change{Modules: map[string]bool{"hack": true}}); err == nil {
		t.Error("unknown module accepted")
	}
	bad := 70000
	if _, err := Apply(root, Change{Port: &bad}); err == nil {
		t.Error("bad port accepted")
	}
	if c, _ := config.Load(root); c.Agreed == "" {
		t.Error("agreement lost after saving settings")
	}
}

func TestPanelNeedsKeyAndPost(t *testing.T) {
	root := t.TempDir()
	h := Handler(root, "test", config.Default(), nil, "k", make(chan struct{}), func(string) {})
	for _, c := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/settings", 403}, {"GET", "/settings?t=k", 200},
		{"GET", "/save?t=k", 403}, {"POST", "/save", 403}, {"POST", "/agree?t=x", 403},
		{"POST", "/agree?t=k", 200},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(c.method, c.path, strings.NewReader("{}")))
		if w.Code != c.want {
			t.Errorf("%s %s: %d, want %d", c.method, c.path, w.Code, c.want)
		}
	}
	_ = http.MethodPost
}

func TestLiveWritesSwitch(t *testing.T) {
	root := t.TempDir()
	h := Handler(root, "test", config.Default(), nil, "k", make(chan struct{}), func(string) {})
	do := func(path, body string) int {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		return w.Code
	}
	if c := do("/livewrites?t=k", `{"Company":"Pilot Client","On":true,"Typed":"yes"}`); c != 400 {
		t.Errorf("wrong word accepted: %d", c)
	}
	if c := do("/livewrites?t=k", `{"Company":"TEST MCP","On":true,"Typed":"ALLOW WRITES"}`); c != 400 {
		t.Errorf("TEST MCP switch accepted: %d", c)
	}
	if c := do("/livewrites?t=k", `{"Company":"Pilot Client","On":true,"Typed":"ALLOW WRITES"}`); c != 200 {
		t.Fatalf("switch on failed: %d", c)
	}
	if c, _ := config.Load(root); len(c.LiveWrites) != 1 {
		t.Errorf("not saved: %v", c.LiveWrites)
	}
	do("/livewrites?t=k", `{"Company":"Pilot Client","On":false}`)
	if c, _ := config.Load(root); len(c.LiveWrites) != 0 {
		t.Errorf("not switched off: %v", c.LiveWrites)
	}
	if c := do("/act?t=k", `{"Company":"TEST MCP","Tool":"draft_voucher"}`); c != 400 {
		t.Errorf("panel reached a tool outside its list: %d", c)
	}
}
