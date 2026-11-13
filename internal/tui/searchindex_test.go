package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/user/claudevu/internal/data"
)

func writeSess(t *testing.T, dir, name, text string) string {
	p := filepath.Join(dir, name)
	line := `{"type":"user","message":{"role":"user","content":"` + text + `"}}` + "\n"
	if err := os.WriteFile(p, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadSearchIndexCmd_ReturnsOnlyChanged(t *testing.T) {
	dir := t.TempDir()
	pa := writeSess(t, dir, "a.jsonl", "alpha")
	pb := writeSess(t, dir, "b.jsonl", "beta")
	sessions := []data.SessionInfo{{SessionID: "a", FilePath: pa}, {SessionID: "b", FilePath: pb}}
	cur, _ := data.BuildIndexEntry(pa, nil)
	prev := map[string]data.IndexEntry{"a": cur}
	msg := loadSearchIndexCmd(sessions, prev)().(SearchIndexLoadedMsg)
	if len(msg.Entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(msg.Entries))
	}
	if _, ok := msg.Entries["b"]; !ok {
		t.Fatal("expected b")
	}
}

func TestLoadSearchIndexCmd_SkipsEmptyFilePath(t *testing.T) {
	msg := loadSearchIndexCmd([]data.SessionInfo{{SessionID: "x"}}, nil)().(SearchIndexLoadedMsg)
	if len(msg.Entries) != 0 {
		t.Fatalf("got %v", msg.Entries)
	}
}

func TestUpdate_SearchIndexLoadedMsg_MergesAndRebuilds(t *testing.T) {
	ti := textinput.New()
	ti.SetValue("zzz")
	m := Model{searchInput: ti, searchIndex: map[string]data.IndexEntry{"old": {Blob: "o"}}}
	m.groups = []data.ProjectGroup{{Project: "p", Expanded: true, Sessions: []data.SessionInfo{{SessionID: "s1", Display: "hi"}}}}
	m.buildListItems("")
	before := len(m.listItems)
	nm, _ := m.Update(SearchIndexLoadedMsg{Entries: map[string]data.IndexEntry{"s1": {Blob: "zzz"}}})
	got := nm.(Model)
	if len(got.searchIndex) != 2 || got.searchIndex["s1"].Blob != "zzz" {
		t.Fatalf("not merged: %v", got.searchIndex)
	}
	if before == 0 {
		t.Fatal("setup")
	}
}
