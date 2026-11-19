package main

import (
	"os"
	"strings"
	"testing"

	"github.com/mayagurutech/tallyguru/internal/store"
)

func TestForget(t *testing.T) {
	root := t.TempDir()
	keep := store.Label{Company: "Other Co", Source: store.Live}
	gone := store.Label{Company: "TEST MCP", Source: store.Live}
	for _, l := range []store.Label{keep, gone} {
		db, err := store.Open(root, l)
		if err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
	os.WriteFile(gone.Dir(root)+"/authorisation.enc", []byte("x"), 0o600)
	os.WriteFile(root+"/key.dpapi", []byte("k"), 0o600)

	// Wrong confirmation: nothing deleted.
	if err := runForget(root, "TEST MCP", store.Live, strings.NewReader("test mcp\n")); err == nil {
		t.Fatal("case-different confirmation must be refused")
	}
	if _, err := os.Stat(gone.Dir(root)); err != nil {
		t.Fatal("deleted despite wrong confirmation")
	}
	// Wrong source: nothing found.
	if err := runForget(root, "TEST MCP", store.Backup, strings.NewReader("TEST MCP\n")); err == nil {
		t.Fatal("backup source has no data; must report that")
	}
	// Correct confirmation.
	if err := runForget(root, "TEST MCP", store.Live, strings.NewReader("TEST MCP\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(gone.Dir(root)); !os.IsNotExist(err) {
		t.Fatal("company folder still exists")
	}
	for _, p := range []string{keep.Dir(root), root + "/key.dpapi"} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s must be kept: %v", p, err)
		}
	}
}
