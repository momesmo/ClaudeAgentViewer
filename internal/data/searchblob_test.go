package data

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFixture(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBuildIndexEntry_IncludesUserAssistantTools(t *testing.T) {
	p := writeFixture(t,
		`{"type":"user","gitBranch":"feat/x","message":{"role":"user","content":"Fix The Login Bug"}}`,
		`{"type":"assistant","message":{"role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Checking auth"},{"type":"tool_use","name":"Grep","input":{}}]}}`,
	)
	e, changed := BuildIndexEntry(p, nil)
	if !changed {
		t.Fatal("expected changed")
	}
	for _, w := range []string{"fix the login bug", "checking auth", "grep", "feat/x", "claude-opus-5-5"} {
		if !strings.Contains(e.Blob, w) {
			t.Errorf("blob missing %q: %q", w, e.Blob)
		}
	}
}

func TestBuildIndexEntry_ExcludesThinkingAndToolResult(t *testing.T) {
	p := writeFixture(t,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"secretthought"},{"type":"text","text":"ok"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"secretresult"}]}}`,
	)
	e, _ := BuildIndexEntry(p, nil)
	if strings.Contains(e.Blob, "secretthought") || strings.Contains(e.Blob, "secretresult") {
		t.Errorf("blob leaked: %q", e.Blob)
	}
	if !strings.Contains(e.Blob, "ok") {
		t.Error("expected text")
	}
}

func TestBuildIndexEntry_CapsAndLowercases(t *testing.T) {
	p := writeFixture(t, `{"type":"user","message":{"role":"user","content":"`+strings.Repeat("ABCé", 50*1024)+`"}}`)
	e, _ := BuildIndexEntry(p, nil)
	if len(e.Blob) > MaxBlobBytes || len(e.Blob) == 0 {
		t.Errorf("len=%d", len(e.Blob))
	}
	if e.Blob != strings.ToLower(e.Blob) {
		t.Error("not lowercase")
	}
}

func TestBuildIndexEntry_SkipsMalformedAndHugeLines(t *testing.T) {
	huge := `{"type":"user","message":{"role":"user","content":"` + strings.Repeat("x", 2*1024*1024) + `"}}`
	p := writeFixture(t, "garbage{{", huge, `{"type":"user","message":{"role":"user","content":"needle"}}`)
	e, _ := BuildIndexEntry(p, nil)
	if !strings.Contains(e.Blob, "needle") {
		t.Errorf("needle missing, len=%d", len(e.Blob))
	}
}

func TestBuildIndexEntry_CacheByModTime(t *testing.T) {
	p := writeFixture(t, `{"type":"user","message":{"role":"user","content":"first"}}`)
	e, _ := BuildIndexEntry(p, nil)
	e2, changed := BuildIndexEntry(p, &e)
	if changed || e2.Blob != e.Blob {
		t.Fatal("expected cached")
	}
	if err := os.WriteFile(p, []byte(`{"type":"user","message":{"role":"user","content":"second"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := e.ModTime.Add(time.Hour)
	os.Chtimes(p, later, later)
	e3, changed := BuildIndexEntry(p, &e)
	if !changed || !strings.Contains(e3.Blob, "second") {
		t.Fatalf("expected rebuild: %v %q", changed, e3.Blob)
	}
}

func TestBuildIndexEntry_MissingFile(t *testing.T) {
	e, changed := BuildIndexEntry(filepath.Join(t.TempDir(), "nope.jsonl"), nil)
	if changed || e.Blob != "" || !e.ModTime.IsZero() {
		t.Error("expected zero entry")
	}
}
