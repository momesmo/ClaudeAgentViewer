package data

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
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

func TestFindSnippets(t *testing.T) {
	blob := "first line about parsing\nthe needle is here and the needle again\nlast line\nneedle at start"
	snips, total := FindSnippets(blob, "needle", 3, 10)
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if len(snips) != 3 {
		t.Fatalf("snips = %d, want 3", len(snips))
	}
	for _, s := range snips {
		if s.Match != "needle" {
			t.Errorf("match = %q", s.Match)
		}
		if strings.Contains(s.Before+s.After, "\n") {
			t.Errorf("snippet crosses a message boundary: %+v", s)
		}
	}
	if snips[0].Before != "the " || !strings.HasPrefix(snips[0].After, " is here") {
		t.Errorf("first snippet = %+v", snips[0])
	}
	if snips[2].Before != "" {
		t.Errorf("match at line start must have empty Before, got %q", snips[2].Before)
	}
}

func TestFindSnippets_LimitsAndCounts(t *testing.T) {
	blob := strings.Repeat("xx needle yy\n", 50)
	snips, total := FindSnippets(blob, "needle", 3, 8)
	if len(snips) != 3 || total != 50 {
		t.Errorf("len=%d total=%d, want 3 and 50", len(snips), total)
	}
	if s, n := FindSnippets(blob, "", 3, 8); s != nil || n != 0 {
		t.Errorf("empty filter must find nothing")
	}
	if s, n := FindSnippets(blob, "absent", 3, 8); len(s) != 0 || n != 0 {
		t.Errorf("no match must return nothing")
	}
}

func TestFindSnippets_ContextEllipsisAndRunes(t *testing.T) {
	blob := "ééééééééééééééééneedleééééééééééééééé"
	snips, _ := FindSnippets(blob, "needle", 1, 5) // 5 BYTES of context cuts mid-rune unless handled
	s := snips[0]
	if !utf8.ValidString(s.Before) || !utf8.ValidString(s.After) {
		t.Fatalf("snippet split a rune: %q / %q", s.Before, s.After)
	}
	if !strings.HasPrefix(s.Before, "…") || !strings.HasSuffix(s.After, "…") {
		t.Errorf("clipped context should end in an ellipsis: %+v", s)
	}
}

func TestMatchedFields(t *testing.T) {
	s := SessionInfo{SessionID: "abc-123", Project: "/work/needle-proj", Display: "fix the login", AgentName: "Planner", CustomTitle: "Login rewrite"}
	cases := map[string][]string{
		"login":   {"prompt", "title"},
		"needle":  {"project"},
		"abc":     {"session ID"},
		"planner": {"agent name"},
		"zzz":     nil,
	}
	for q, want := range cases {
		got := MatchedFields(s, q)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("MatchedFields(%q) = %v, want %v", q, got, want)
		}
	}
	if got := MatchedFields(s, ""); got != nil {
		t.Errorf("empty filter: %v", got)
	}
}
