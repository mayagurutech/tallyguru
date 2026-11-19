package crypt

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSealOpen(t *testing.T) {
	b, _ := New(bytes.Repeat([]byte{7}, 32))
	c, err := b.Seal([]byte("PAN ABCDE1234F"), "test-mcp")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(c, []byte("ABCDE1234F")) {
		t.Fatal("plain text visible in ciphertext")
	}
	p, err := b.Open(c, "test-mcp")
	if err != nil || string(p) != "PAN ABCDE1234F" {
		t.Fatalf("round trip failed: %q %v", p, err)
	}
	if _, err := b.Open(c, "other-company"); err == nil {
		t.Fatal("must not open under another company's label")
	}
	c[len(c)-1] ^= 1
	if _, err := b.Open(c, "test-mcp"); err == nil {
		t.Fatal("tampered data must fail")
	}
}

func TestKeyFileProtected(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("DPAPI is Windows-only")
	}
	root := t.TempDir()
	b1, err := LoadOrCreate(root)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := b1.Seal([]byte("secret"), "x")
	b2, err := LoadOrCreate(root) // reload from disk
	if err != nil {
		t.Fatal(err)
	}
	if p, err := b2.Open(c, "x"); err != nil || string(p) != "secret" {
		t.Fatal("reloaded key cannot decrypt")
	}
	raw, _ := os.ReadFile(filepath.Join(root, "key.dpapi"))
	if len(raw) <= 32 {
		t.Fatal("key file looks unprotected")
	}
}
