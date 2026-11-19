//go:build !windows

package crypt

import "errors"

// Non-Windows builds have no DPAPI; refuse rather than store the key in plain text.
func protect([]byte) ([]byte, error)   { return nil, errors.New("key protection needs Windows") }
func unprotect([]byte) ([]byte, error) { return nil, errors.New("key protection needs Windows") }
