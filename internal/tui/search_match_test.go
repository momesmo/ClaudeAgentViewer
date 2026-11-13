package tui

import (
	"testing"

	"github.com/user/claudevu/internal/data"
)

func matchModel(idx map[string]data.IndexEntry) Model {
	s1 := data.SessionInfo{SessionID: "sess-1", Display: "hello world"}
	s1.FillSearchMeta()
	s2 := data.SessionInfo{SessionID: "sess-2", Display: "other"}
	s2.FillSearchMeta()
	m := Model{
		groups:      []data.ProjectGroup{{Project: "projA", Expanded: true, Sessions: []data.SessionInfo{s1, s2}}},
		searchIndex: idx,
	}
	return m
}

func TestBuildListItems_ContentMatchShowsSession(t *testing.T) {
	m := matchModel(map[string]data.IndexEntry{"sess-1": {Blob: "some needle here"}})
	m.buildListItems("NEEDLE")
	if len(m.listItems) != 2 || m.listItems[0].kind != itemProject || m.listItems[1].kind != itemSession {
		t.Fatalf("items = %+v", m.listItems)
	}
	if m.listItems[1].matchKind != data.MatchContent || m.listItems[1].sessionIndex != 0 {
		t.Fatalf("got %+v", m.listItems[1])
	}
}

func TestBuildListItems_NoMatchHidesProject(t *testing.T) {
	m := matchModel(map[string]data.IndexEntry{"sess-1": {Blob: "nothing"}})
	m.buildListItems("needle")
	if len(m.listItems) != 0 {
		t.Fatalf("items = %+v", m.listItems)
	}
}

func TestBuildListItems_MetaMatchPreferred(t *testing.T) {
	m := matchModel(map[string]data.IndexEntry{"sess-1": {Blob: "hello again"}})
	m.buildListItems("hello")
	if len(m.listItems) != 2 || m.listItems[1].matchKind != data.MatchMeta {
		t.Fatalf("items = %+v", m.listItems)
	}
}

func TestBuildListItems_EmptyFilterUnchanged(t *testing.T) {
	m := matchModel(map[string]data.IndexEntry{})
	m.buildListItems("")
	if len(m.listItems) != 3 {
		t.Fatalf("items = %+v", m.listItems)
	}
}

func TestBuildListItems_MissingIndexEntryStillMatchesMeta(t *testing.T) {
	m := matchModel(map[string]data.IndexEntry{})
	m.buildListItems("other")
	if len(m.listItems) != 2 || m.listItems[1].sessionIndex != 1 {
		t.Fatalf("items = %+v", m.listItems)
	}
}
