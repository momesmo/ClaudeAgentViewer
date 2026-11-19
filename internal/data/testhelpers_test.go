package data

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// skipUnixPermBitsOnWindows is a platform skip, not a data skip: Windows has
// no 0600/0700 permission bits to assert.
func skipUnixPermBitsOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits (0600/0700) are not meaningful on Windows")
	}
}
