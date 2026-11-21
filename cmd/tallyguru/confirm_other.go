//go:build !windows

package main

// confirmDialog is Windows-only; elsewhere the AI app's own form (MCP elicitation) is used.
var confirmDialog func(title, text string) (bool, error)
