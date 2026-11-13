package tui

import (
	"github.com/charmbracelet/lipgloss"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/user/claudevu/internal/data"
)

func TestNewModel_CacheOff_DoesNotTouchDisk(t *testing.T) {
	t.Setenv("CLAUDEVU_INDEX_CACHE", "")
	dir, cd := t.TempDir(), t.TempDir()
	data.SaveIndex(dir, cd, map[string]data.IndexEntry{"a": {Blob: "x"}})
	before, _ := os.ReadDir(dir)
	m := NewModel(cd, Options{IndexCache: false, CacheDir: dir})
	if len(m.searchIndex) != 0 {
		t.Fatal("index should be empty")
	}
	after, _ := os.ReadDir(dir)
	if len(before) != len(after) {
		t.Fatal("dir changed")
	}
}

func TestNewModel_CacheOn_SeedsIndex(t *testing.T) {
	t.Setenv("CLAUDEVU_INDEX_CACHE", "")
	dir, cd := t.TempDir(), t.TempDir()
	data.SaveIndex(dir, cd, map[string]data.IndexEntry{"a": {ModTime: time.Now(), Blob: "x"}})
	m := NewModel(cd, Options{IndexCache: true, CacheDir: dir})
	if m.searchIndex["a"].Blob != "x" {
		t.Fatal("not seeded")
	}
}

func TestUpdate_IndexLoaded_SavesWhenEnabled(t *testing.T) {
	t.Setenv("CLAUDEVU_INDEX_CACHE", "")
	msg := SearchIndexLoadedMsg{Entries: map[string]data.IndexEntry{"a": {Blob: "x"}}}
	on := NewModel(t.TempDir(), Options{IndexCache: true, CacheDir: t.TempDir()})
	if _, cmd := on.Update(msg); cmd == nil {
		t.Fatal("expected save cmd")
	}
	off := NewModel(t.TempDir(), Options{CacheDir: t.TempDir()})
	if _, cmd := off.Update(msg); cmd != nil {
		t.Fatal("expected nil cmd")
	}
}

func TestUpdate_IndexLoaded_CoalescesSaves(t *testing.T) {
	t.Setenv("CLAUDEVU_INDEX_CACHE", "")
	msg := SearchIndexLoadedMsg{Entries: map[string]data.IndexEntry{"a": {Blob: "x"}}}
	m := NewModel(t.TempDir(), Options{IndexCache: true, CacheDir: t.TempDir()})
	r, cmd := m.Update(msg)
	if cmd == nil {
		t.Fatal("first save expected")
	}
	m = r.(Model)
	for i := 0; i < 2; i++ {
		r, cmd = m.Update(msg)
		if cmd != nil {
			t.Fatal("save must be skipped while in flight")
		}
		m = r.(Model)
	}
	r, cmd = m.Update(IndexSaveDoneMsg{})
	if cmd == nil {
		t.Fatal("expected exactly one follow-up save")
	}
	m = r.(Model)
	if _, cmd = m.Update(IndexSaveDoneMsg{}); cmd != nil {
		t.Fatal("no further save expected")
	}
}

func TestRenderList_ContentRowSingleLine(t *testing.T) {
	long := strings.Repeat("very long prompt text ", 20)
	s := data.SessionInfo{SessionID: "s1", Display: long}
	s.FillSearchMeta()
	m := Model{
		groups:      []data.ProjectGroup{{Project: "p", Expanded: true, Sessions: []data.SessionInfo{s}}},
		searchIndex: map[string]data.IndexEntry{"s1": {Blob: "zzz needle zzz"}},
	}
	m.buildListItems("needle")
	for _, cur := range []int{1, 0} {
		m.listCursor = cur
		m.focus = focusList
		out := m.renderList(50, 10)
		for i, line := range strings.Split(out, "\n") {
			if lipgloss.Width(line) > 50 {
				t.Errorf("cursor %d line %d too wide: %d", cur, i, lipgloss.Width(line))
			}
		}
		if n := strings.Count(out, "\n"); n > 4 {
			t.Errorf("cursor %d: expected few lines, got %d", cur, n+1)
		}
	}
}
