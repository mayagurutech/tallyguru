// Package auth is the mandatory authorisation gate (DPDP Act, 2023) that must be passed
// before the first sync of any company, and again in every financial year.
//
// The tool cannot verify that a letter is genuine; it only records what the user supplied.
package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mayagurutech/tallyguru/internal/crypt"
)

// Notice is shown before any authorisation is recorded (wording from the project brief).
const Notice = `The Digital Personal Data Protection Act, 2023 applies to personal data in these books.
Data the AI reads is sent to your AI service provider for processing. You must have written
authorisation from the business before continuing.`

// Disclaimer is shown with the notice.
const Disclaimer = `This tool cannot verify that an authorisation letter or email is genuine. It only stores
what you provide, encrypted on this PC. Legal responsibility for obtaining and holding valid
authorisation stays with you.`

// OwnerDeclaration is accepted instead of a letter when owners use their own books.
const OwnerDeclaration = `I declare that I am the owner of this business, or a partner/director authorised by it,
that these are the business's own books, and that I understand data the AI reads is sent to
my AI service provider for processing.`

// ConfirmWord must be typed to accept.
const ConfirmWord = "I AGREE"

// Kind of authorisation.
type Kind string

const (
	Letter Kind = "letter" // signed letter: PDF or image
	Email  Kind = "email"  // saved email: .eml or .msg
	Owner  Kind = "owner"  // owner's declaration
	Self   Kind = "self"   // professional's self-certification (default; no file needed)
)

// SelfDeclaration is typed-accepted by accountants/auditors instead of uploading a letter.
const SelfDeclaration = `I confirm that I hold written authorisation from this business to access and process its
accounting data with an AI assistant for the purpose agreed with them, and that I will produce
it if asked. I understand data the AI reads is sent to my AI service provider for processing.`

// Record is one accepted authorisation.
type Record struct {
	Company, GSTIN, Source string
	Kind                   Kind
	FileName               string `json:",omitempty"`
	SHA256                 string `json:",omitempty"` // fingerprint of the uploaded file
	Bytes                  int    `json:",omitempty"`
	FY                     string // e.g. "2026-27"
	AcceptedAt             time.Time
	NoticeSHA256           string // fingerprint of the notice text the user accepted
	NoticeLang             string `json:",omitempty"` // language the notice was shown in
}

// Sum returns the SHA-256 fingerprint (hex) of b.
func Sum(b []byte) string { return sum(b) }

// FY returns the Indian financial year (April–March) containing t, e.g. "2026-27".
func FY(t time.Time) string {
	y := t.Year()
	if t.Month() < time.April {
		y--
	}
	return fmt.Sprintf("%d-%02d", y, (y+1)%100)
}

const maxFile = 20 << 20

// CheckEvidence validates an uploaded file's type by content, not just its name.
func CheckEvidence(k Kind, name string, data []byte) error {
	if len(data) == 0 {
		return errors.New("file is empty")
	}
	if len(data) > maxFile {
		return errors.New("file is larger than 20 MB")
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch k {
	case Letter:
		switch {
		case bytes.HasPrefix(data, []byte("%PDF-")):
		case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G'}):
		case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		default:
			return fmt.Errorf("%s is not a PDF, PNG or JPEG file", name)
		}
	case Email:
		switch {
		case ext == ".msg" && bytes.HasPrefix(data, []byte{0xD0, 0xCF, 0x11, 0xE0}):
		case ext == ".eml" && hasMailHeaders(data):
		default:
			return fmt.Errorf("%s is not a saved email (.eml or Outlook .msg)", name)
		}
	default:
		return fmt.Errorf("no file expected for %s", k)
	}
	return nil
}

func hasMailHeaders(b []byte) bool {
	head := strings.ToLower(string(b[:min(len(b), 8192)]))
	return strings.Contains(head, "from:") && (strings.Contains(head, "date:") || strings.Contains(head, "subject:"))
}

func sum(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

const recFile, evFile = "authorisation.enc", "evidence-%s.enc"

// Store keeps authorisation records for one company folder, encrypted.
type Store struct {
	Dir string
	Box *crypt.Box
}

func (s Store) label() string { return "auth:" + filepath.Base(s.Dir) }

// Records returns all stored records (oldest first).
func (s Store) Records() ([]Record, error) {
	b, err := os.ReadFile(filepath.Join(s.Dir, recFile))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p, err := s.Box.Open(b, s.label())
	if err != nil {
		return nil, err
	}
	var rs []Record
	return rs, json.Unmarshal(p, &rs)
}

// Add stores a new record and, for letters/emails, the encrypted file itself.
func (s Store) Add(r Record, evidence []byte) error {
	if r.Kind.HasFile() {
		if err := CheckEvidence(r.Kind, r.FileName, evidence); err != nil {
			return err
		}
		r.SHA256, r.Bytes = sum(evidence), len(evidence)
	}
	if r.NoticeSHA256 == "" { // caller sets it when the notice was shown in another language
		r.NoticeSHA256 = sum([]byte(Notice))
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	if r.Kind.HasFile() {
		c, err := s.Box.Seal(evidence, s.label()+":"+r.SHA256)
		if err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(s.Dir, fmt.Sprintf(evFile, r.SHA256[:16])), c); err != nil {
			return err
		}
	}
	rs, err := s.Records()
	if err != nil {
		return err
	}
	j, _ := json.Marshal(append(rs, r))
	c, err := s.Box.Seal(j, s.label())
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.Dir, recFile), c)
}

func writeAtomic(p string, b []byte) error {
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Valid returns the record covering the financial year of now, or an error explaining
// what the user must do. Sync and every tool call must pass this.
func (s Store) Valid(company string, now time.Time) (*Record, error) {
	rs, err := s.Records()
	if err != nil {
		return nil, fmt.Errorf("authorisation records unreadable: %w", err)
	}
	fy := FY(now)
	for i := len(rs) - 1; i >= 0; i-- {
		if rs[i].FY == fy && rs[i].Company == company {
			return &rs[i], nil
		}
	}
	if len(rs) > 0 {
		return nil, fmt.Errorf("authorisation for %q was for FY %s; re-confirm for FY %s with: tallyguru authorise %q",
			company, rs[len(rs)-1].FY, fy, company)
	}
	return nil, fmt.Errorf("no authorisation recorded for %q; run: tallyguru authorise %q", company, company)
}

// HasFile reports whether this kind carries an uploaded file.
func (k Kind) HasFile() bool { return k == Letter || k == Email }
