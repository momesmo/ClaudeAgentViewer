package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/claudevu/internal/data"
)

func mkSession(id, display, project, path string) data.SessionInfo {
	s := data.SessionInfo{SessionID: id, Display: display, Project: project, FilePath: path}
	s.FillSearchMeta()
	return s
}

// --- 1. stale blobs ---------------------------------------------------------

func TestLoadSearchIndexCmd_ReportsRemovedSessions(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present.jsonl")
	if err := os.WriteFile(present, []byte(`{"type":"user","message":{"role":"user","content":"hi there"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prev := map[string]data.IndexEntry{
		"gone":    {Blob: "old text"},
		"nopath":  {Blob: "old text"},
		"present": {},
	}
	sessions := []data.SessionInfo{
		mkSession("gone", "g", "p", filepath.Join(dir, "deleted.jsonl")), // file deleted
		mkSession("nopath", "n", "p", ""),                                // file can no longer be located
		mkSession("present", "x", "p", present),
		mkSession("never-indexed", "z", "p", filepath.Join(dir, "also-missing.jsonl")), // nothing to remove
	}
	msg := loadSearchIndexCmd(sessions, prev)().(SearchIndexLoadedMsg)
	got := map[string]bool{}
	for _, id := range msg.Removed {
		got[id] = true
	}
	if !got["gone"] || !got["nopath"] || got["present"] || got["never-indexed"] || len(msg.Removed) != 2 {
		t.Errorf("Removed = %v, want exactly [gone nopath]", msg.Removed)
	}
	if _, ok := msg.Entries["present"]; !ok {
		t.Errorf("present session should still be indexed")
	}
}

func TestUpdate_IndexLoaded_RemovesStaleAndUnknownEntries(t *testing.T) {
	m := Model{
		groups: []data.ProjectGroup{{Project: "p", Expanded: true, Sessions: []data.SessionInfo{mkSession("a", "alpha", "p", "/x")}}},
		searchIndex: map[string]data.IndexEntry{
			"a":           {Blob: "keep"},
			"deleted":     {Blob: "needle in a deleted session"},
			"not-in-hist": {Blob: "needle from a session no longer in history"},
		},
	}
	m.buildListItems("needle")
	r, _ := m.Update(SearchIndexLoadedMsg{Removed: []string{"deleted"}})
	m = r.(Model)
	if _, ok := m.searchIndex["deleted"]; ok {
		t.Errorf("explicitly removed entry still in the index")
	}
	if _, ok := m.searchIndex["not-in-hist"]; ok {
		t.Errorf("entry for a session that is no longer in history should be pruned")
	}
	if _, ok := m.searchIndex["a"]; !ok {
		t.Errorf("live entry was dropped")
	}
}

func TestUpdate_IndexLoaded_RemovalAloneTriggersCacheSave(t *testing.T) {
	m := Model{
		opts:        Options{IndexCache: true},
		cacheDir:    t.TempDir(),
		groups:      []data.ProjectGroup{{Project: "p", Sessions: []data.SessionInfo{mkSession("a", "alpha", "p", "/x")}}},
		searchIndex: map[string]data.IndexEntry{"a": {Blob: "x"}},
	}
	r, cmd := m.Update(SearchIndexLoadedMsg{Removed: []string{"a"}})
	if cmd == nil || !r.(Model).saveInFlight {
		t.Errorf("dropping an entry must rewrite the cache (cmd nil=%v)", cmd == nil)
	}
}

// --- 3. cursor stability ----------------------------------------------------

func cursorModel() Model {
	// Group order: B first, A second. a1 matches "needle" by its prompt; b1 only
	// matches once its content blob arrives, and it lists *above* a1.
	m := Model{
		groups: []data.ProjectGroup{
			{Project: "B", Expanded: true, Sessions: []data.SessionInfo{mkSession("b1", "unrelated", "B", "/b")}},
			{Project: "A", Expanded: true, Sessions: []data.SessionInfo{mkSession("a1", "has the needle", "A", "/a")}},
		},
		searchIndex: map[string]data.IndexEntry{},
	}
	m.searchInput.SetValue("needle")
	m.buildListItems("needle")
	return m
}

func selectedSessionID(m Model) string {
	if m.listCursor < 0 || m.listCursor >= len(m.listItems) {
		return "<out of range>"
	}
	it := m.listItems[m.listCursor]
	if it.kind != itemSession {
		return "<not a session>"
	}
	return m.groups[it.projectIndex].Sessions[it.sessionIndex].SessionID
}

func TestUpdate_IndexLoaded_KeepsCursorOnSameItem(t *testing.T) {
	m := cursorModel()
	m.listCursor = 1 // a1 (list is: A project, a1)
	if selectedSessionID(m) != "a1" {
		t.Fatalf("setup: selected %s", selectedSessionID(m))
	}
	before := len(m.listItems)
	r, _ := m.Update(SearchIndexLoadedMsg{Entries: map[string]data.IndexEntry{"b1": {Blob: "a needle appears here"}}})
	m = r.(Model)
	if len(m.listItems) <= before {
		t.Fatalf("setup: expected new rows, got %d -> %d", before, len(m.listItems))
	}
	if got := selectedSessionID(m); got != "a1" {
		t.Errorf("cursor moved to %s after rows were inserted above it, want a1", got)
	}
}

func TestUpdate_IndexLoaded_CursorClampedWhenItemDisappears(t *testing.T) {
	m := cursorModel()
	// b1 matches by content first, cursor on it...
	m.searchIndex["b1"] = data.IndexEntry{Blob: "needle"}
	m.buildListItems("needle")
	for i, it := range m.listItems {
		if it.kind == itemSession && m.groups[it.projectIndex].Sessions[it.sessionIndex].SessionID == "b1" {
			m.listCursor = i
		}
	}
	if selectedSessionID(m) != "b1" {
		t.Fatalf("setup: selected %s", selectedSessionID(m))
	}
	// ...then its file is deleted, so it drops out of the filtered list.
	r, _ := m.Update(SearchIndexLoadedMsg{Removed: []string{"b1"}})
	m = r.(Model)
	if m.listCursor < 0 || m.listCursor >= len(m.listItems) {
		t.Errorf("cursor %d out of range (len %d)", m.listCursor, len(m.listItems))
	}
}

// --- 17. where did it match -------------------------------------------------

func detailModel(filter, blob string) Model {
	s := mkSession("abc-123", "fix the login bug", "/work/proj", "/x")
	m := Model{
		width: 120, height: 30,
		groups:      []data.ProjectGroup{{Project: "/work/proj", Expanded: true, Sessions: []data.SessionInfo{s}}},
		searchIndex: map[string]data.IndexEntry{"abc-123": {Blob: blob}},
	}
	m.selectedInfo = &m.groups[0].Sessions[0]
	m.searchInput.SetValue(filter)
	m.buildListItems(filter)
	return m
}

func TestDetail_ShowsSearchMatchDetails(t *testing.T) {
	m := detailModel("login", "intro\nthe login page crashes\nand the login form too\nend")
	out := m.renderDetail(80, 30)
	for _, want := range []string{`Search "login"`, "prompt", "2 matches", "login page crashes"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q:\n%s", want, out)
		}
	}
}

func TestDetail_NoSearchSectionWithoutFilterOrForSubagent(t *testing.T) {
	m := detailModel("", "the login page")
	if strings.Contains(m.renderDetail(80, 30), "Search ") {
		t.Errorf("no filter: search section must be hidden")
	}
	m = detailModel("login", "the login page")
	m.selectedSub = &data.SubagentInfo{}
	if strings.Contains(m.renderDetail(80, 30), `Search "login"`) {
		t.Errorf("subagent selected: search section must be hidden")
	}
}

func TestDetail_SearchSectionContentOnlyAndNarrow(t *testing.T) {
	m := detailModel("needle", "xx needle yy")
	out := m.renderDetail(30, 30) // very narrow pane
	if !strings.Contains(out, "1 match") || strings.Contains(out, "1 matches") {
		t.Errorf("want singular '1 match':\n%s", out)
	}
	if strings.Contains(out, "Matched in") {
		t.Errorf("content-only match must not claim a metadata field:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if w := lipglossWidth(line); w > 30 {
			t.Errorf("line wider than pane (%d): %q", w, line)
		}
	}
}
