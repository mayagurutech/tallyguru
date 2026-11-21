package auth

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mayagurutech/tallyguru/internal/crypt"
)

func store(t *testing.T) Store {
	b, _ := crypt.New(bytes.Repeat([]byte{1}, 32))
	return Store{Dir: filepath.Join(t.TempDir(), "test-mcp__nogstin__live"), Box: b}
}

var nov26 = time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)

func TestFY(t *testing.T) {
	for in, want := range map[string]string{"2026-04-01": "2026-27", "2027-03-31": "2026-27", "2026-03-31": "2025-26", "2099-12-01": "2099-00"} {
		d, _ := time.Parse("2006-01-02", in)
		if got := FY(d); got != want {
			t.Errorf("FY(%s)=%s want %s", in, got, want)
		}
	}
}

func TestGateClosedUntilAuthorised(t *testing.T) {
	s := store(t)
	if _, err := s.Valid("TEST MCP", nov26); err == nil || !strings.Contains(err.Error(), "no authorisation") {
		t.Fatalf("expected closed gate, got %v", err)
	}
}

func TestLetterStoredEncrypted(t *testing.T) {
	s := store(t)
	pdf := []byte("%PDF-1.7 Client letter authorising Guru & Co, PAN ABCDE1234F")
	r := Record{Company: "TEST MCP", Source: "live", Kind: Letter, FileName: "letter.pdf", FY: FY(nov26), AcceptedAt: nov26}
	if err := s.Add(r, pdf); err != nil {
		t.Fatal(err)
	}
	got, err := s.Valid("TEST MCP", nov26)
	if err != nil || got.SHA256 == "" || got.Bytes != len(pdf) || got.NoticeSHA256 == "" {
		t.Fatalf("bad record %+v %v", got, err)
	}
	// Nothing readable on disk.
	files, _ := os.ReadDir(s.Dir)
	if len(files) != 2 {
		t.Fatalf("want record + evidence files, got %d", len(files))
	}
	for _, f := range files {
		b, _ := os.ReadFile(filepath.Join(s.Dir, f.Name()))
		if bytes.Contains(b, []byte("ABCDE1234F")) || bytes.Contains(b, []byte("TEST MCP")) {
			t.Fatalf("%s contains plain text", f.Name())
		}
	}
}

func TestSelfCertificationNeedsNoFile(t *testing.T) {
	s := store(t)
	if err := s.Add(Record{Company: "Pilot Client", Kind: Self, FY: FY(nov26), AcceptedAt: nov26}, nil); err != nil {
		t.Fatal(err)
	}
	r, err := s.Valid("Pilot Client", nov26)
	if err != nil || r.Kind != Self || r.SHA256 != "" {
		t.Fatalf("self-cert record: %+v %v", r, err)
	}
	files, _ := os.ReadDir(s.Dir)
	if len(files) != 1 {
		t.Fatalf("self-cert must store only the record, got %d files", len(files))
	}
}

func TestYearlyReconfirm(t *testing.T) {
	s := store(t)
	s.Add(Record{Company: "TEST MCP", Kind: Owner, FY: "2026-27", AcceptedAt: nov26}, nil)
	next := time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.Valid("TEST MCP", next); err == nil || !strings.Contains(err.Error(), "re-confirm for FY 2027-28") {
		t.Fatalf("new FY must ask to re-confirm, got %v", err)
	}
}

func TestEvidenceChecks(t *testing.T) {
	bad := map[string]struct {
		k    Kind
		data string
	}{
		"letter.pdf": {Letter, "just text renamed to pdf"},
		"empty.png":  {Letter, ""},
		"mail.eml":   {Email, "%PDF-1.4"},
		"mail.msg":   {Email, "From: x\nSubject: y"},
	}
	for name, c := range bad {
		if CheckEvidence(c.k, name, []byte(c.data)) == nil {
			t.Errorf("%s should be rejected", name)
		}
	}
	good := map[string]struct {
		k    Kind
		data []byte
	}{
		"a.pdf": {Letter, []byte("%PDF-1.4 ...")},
		"a.jpg": {Letter, []byte{0xFF, 0xD8, 0xFF, 0xE0}},
		"a.eml": {Email, []byte("From: owner@client.in\r\nDate: Fri, 13 Nov 2026\r\nSubject: Authorisation\r\n\r\nI authorise...")},
		"a.msg": {Email, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1}},
	}
	for name, c := range good {
		if err := CheckEvidence(c.k, name, c.data); err != nil {
			t.Errorf("%s should be accepted: %v", name, err)
		}
	}
}

func TestRecordsBoundToCompanyFolder(t *testing.T) {
	s := store(t)
	s.Add(Record{Company: "TEST MCP", Kind: Owner, FY: "2026-27"}, nil)
	other := Store{Dir: filepath.Join(filepath.Dir(s.Dir), "other__nogstin__live"), Box: s.Box}
	os.MkdirAll(other.Dir, 0o700)
	b, _ := os.ReadFile(filepath.Join(s.Dir, recFile))
	os.WriteFile(filepath.Join(other.Dir, recFile), b, 0o600)
	if _, err := other.Records(); err == nil {
		t.Fatal("authorisation copied to another company's folder must not be accepted")
	}
}

// The yearly declaration gives each company its own record (marked with its basis) at first
// use; without it, the usual error.
func TestCoverFromAnnual(t *testing.T) {
	box, _ := crypt.New(bytes.Repeat([]byte{7}, 32))
	root := t.TempDir()
	annual, co := Store{Dir: root, Box: box}, Store{Dir: root + "/co", Box: box}
	now := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	if _, err := CoverFromAnnual(annual, co, "ABC", "", "live", now); err == nil {
		t.Fatal("no declaration at all must refuse")
	}
	annual.Add(Record{Company: AllCompanies, Kind: Self, FY: FY(now), AcceptedAt: now}, nil)
	r, err := CoverFromAnnual(annual, co, "ABC", "27AAAAA0000A1Z5", "live", now)
	if err != nil || r.Company != "ABC" || !strings.Contains(r.Basis, "annual declaration of 21-11-2026") {
		t.Fatalf("record from yearly declaration: %+v %v", r, err)
	}
	if _, err := CoverFromAnnual(annual, co, "ABC", "", "live", now.AddDate(1, 0, 0)); err == nil {
		t.Fatal("next financial year must ask again")
	}
}
