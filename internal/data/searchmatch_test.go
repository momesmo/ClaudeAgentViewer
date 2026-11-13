package data

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sess(display, project string) SessionInfo {
	s := SessionInfo{SessionID: "ABC-123", Display: display, Project: project}
	s.FillSearchMeta()
	return s
}

func TestMatchSession_Meta(t *testing.T) {
	s := sess("Fix Login", "/p/Proj")
	s.AgentName, s.CustomTitle = "Robo", "Big Title"
	s.FillSearchMeta()
	for _, f := range []string{"login", "abc-123", "/p/proj", "robo", "big title"} {
		if got := MatchSession(s, "", f); got != MatchMeta {
			t.Errorf("%q: got %v", f, got)
		}
	}
}

func TestMatchSession_ContentOnly(t *testing.T) {
	if got := MatchSession(sess("x", "y"), "...needle...", "needle"); got != MatchContent {
		t.Fatalf("got %v", got)
	}
}

func TestMatchSession_None(t *testing.T) {
	if got := MatchSession(sess("x", "y"), "blob", "zzz"); got != MatchNone {
		t.Fatalf("got %v", got)
	}
}

func TestMatchSession_EmptyFilter(t *testing.T) {
	if got := MatchSession(sess("x", "y"), "", ""); got != MatchMeta {
		t.Fatalf("got %v", got)
	}
}

func TestMatchSession_NoCrossFieldMatch(t *testing.T) {
	s := SessionInfo{Display: "ab", Project: "cd"}
	s.FillSearchMeta()
	if got := MatchSession(s, "", "bc"); got != MatchNone {
		t.Fatalf("got %v", got)
	}
}

func TestLoadHistory_FillsSearchMeta(t *testing.T) {
	dir := t.TempDir()
	line := `{"display":"Hello World","timestamp":1700000000000,"project":"/Some/Proj","sessionId":"SID-1"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "history.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	groups, err := LoadHistory(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range groups {
		for _, s := range g.Sessions {
			n++
			if s.SearchMeta == "" || s.SearchMeta != strings.ToLower(s.SearchMeta) {
				t.Errorf("bad SearchMeta %q", s.SearchMeta)
			}
		}
	}
	if n == 0 {
		t.Fatal("no sessions")
	}
}
