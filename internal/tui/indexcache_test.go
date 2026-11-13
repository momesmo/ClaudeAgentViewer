package tui

import (
	"os"
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
