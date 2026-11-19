// Package crypt encrypts sensitive local files and fields with AES-256-GCM.
// The 32-byte key is generated once per PC user and stored protected by Windows DPAPI,
// so it is never on disk in plain text and only this Windows user can unlock it.
package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Box encrypts and decrypts with one key.
type Box struct{ aead cipher.AEAD }

const magic = "TGE1" // file/field format version

// New builds a Box from a raw 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be 32 bytes")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	return &Box{g}, nil
}

// Seal encrypts plain; label binds the ciphertext to its purpose (e.g. company folder),
// so a file copied to another company fails to open.
func (b *Box) Seal(plain []byte, label string) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte(magic), nonce...)
	return b.aead.Seal(out, nonce, plain, []byte(label)), nil
}

// Open decrypts data sealed with the same label.
func (b *Box) Open(data []byte, label string) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(data) < len(magic)+n || string(data[:len(magic)]) != magic {
		return nil, errors.New("not an encrypted Tally MCP file")
	}
	p, err := b.aead.Open(nil, data[len(magic):len(magic)+n], data[len(magic)+n:], []byte(label))
	if err != nil {
		return nil, errors.New("cannot decrypt: wrong key, wrong company, or file damaged")
	}
	return p, nil
}

// LoadOrCreate returns the Box for this PC user, creating a protected key under root on
// first use.
func LoadOrCreate(root string) (*Box, error) {
	p := filepath.Join(root, "key.dpapi")
	if prot, err := os.ReadFile(p); err == nil {
		key, err := unprotect(prot)
		if err != nil {
			return nil, fmt.Errorf("unlocking key %s (was it made by another Windows user?): %w", p, err)
		}
		return New(key)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	prot, err := protect(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	// O_EXCL: never overwrite an existing key (that would lose access to all data).
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(prot); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return New(key)
}
