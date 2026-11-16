package data

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Shared test helpers; always compiled (no build tag) so every GOOS builds the tests.

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func parseFloatNoComma(s string) (float64, error) {
	var v float64
	_, err := fmt.Sscanf(s, "%g", &v)
	return v, err
}

func trimFloat(v float64) string { return fmt.Sprintf("%g", v) }

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	writeFile(t, p, content)
	return p
}
