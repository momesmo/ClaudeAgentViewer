package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/user/claudevu/internal/data"
	"github.com/user/claudevu/internal/testfixture"
	"github.com/user/claudevu/internal/tui"
)

// TestTreeView walks the project -> session -> subagent tree: expand/collapse,
// flagging with live stats, and live-tail logs.
func TestTreeView(t *testing.T) {
	fx := testfixture.BuildClaudeDir(t)
	m := newModel(t, fx, 120, 40)
	show("1_INITIAL", m)

	// collapsed: only project rows
	view := m.View()
	for _, s := range fx.Sessions {
		if listed(view, s) {
			t.Errorf("session %q visible while every project is collapsed", s.Key)
		}
	}

	// expand the first project: its sessions appear
	m = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	view = m.View()
	first := fx.Projects[0]
	for _, s := range first.Sessions {
		if !listed(view, s) {
			t.Errorf("expanded project %s: session %q (%q) not listed", first.Path, s.Key, s.Display)
		}
	}
	other := fx.Projects[1].Sessions[0]
	if listed(view, other) {
		t.Errorf("session of collapsed project %s is visible", fx.Projects[1].Path)
	}

	// collapse it again
	m = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	if listed(m.View(), first.Sessions[0]) {
		t.Error("collapsing the project should hide its sessions")
	}

	// expand everything: every session of every project is listed
	m = expandAllProjects(m, fx)
	view = m.View()
	for _, s := range fx.Sessions {
		if !listed(view, s) {
			t.Errorf("session %q (%q) missing from fully expanded tree", s.Key, s.Display)
		}
	}
	if len(first.Sessions) > 0 && strings.Contains(view, "stale older prompt") {
		t.Error("older duplicate history entry leaked into the tree")
	}

	// session detail shows stats and the per-model breakdown for a mixed session
	mixed := fx.Session(t, "mixed")
	m = cursorTo(m, rowOf(fx, "mixed"))
	m = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = upd(m, quickMsg(t, mixed))
	show("2_SESSION_METADATA", m)
	view = m.View()
	for model := range mixed.PerModel {
		if !strings.Contains(view, model) {
			t.Errorf("mixed session detail lacks model %q:\n%s", model, view)
		}
	}

	// flag two sessions; live stats show in the tree
	stats := map[string]data.SessionStats{}
	for _, k := range []string{"mixed", "mystery"} {
		s := fx.Session(t, k)
		stats[s.ID] = quickStats(t, s)
		m = cursorTo(m, rowOf(fx, k))
		m = runes(m, "f")
	}
	m = upd(m, flaggedMsg(stats))
	show("3_TWO_FLAGGED", m)
	view = m.View()
	if !strings.Contains(view, "unpriced") { // mystery model: cost is not guessed
		t.Errorf("unpriced flagged session should say so:\n%s", view)
	}
	checkFits(t, "flagged", m, 120, 40)

	// unflag one, still renders
	m = runes(m, "f")
	checkFits(t, "unflagged", m, 120, 40)

	// logs with live tailing
	m = cursorTo(m, rowOf(fx, "mixed"))
	msgs, err := data.LoadTranscript(mixed.Path)
	if err != nil {
		t.Fatal(err)
	}
	m = upd(m, transcriptMsg(msgs, mixed.Path))
	m = upd(m, tea.KeyMsg{Type: tea.KeyTab}) // focus the detail pane, scroll to the transcript
	m = press(m, tea.KeyPgDown, 2)
	show("4_LOGS", m)
	view = m.View()
	if !strings.Contains(view, "First with sonnet") && !strings.Contains(view, "compare two models") {
		t.Errorf("logs view shows none of the transcript text:\n%s", view)
	}
}

// TestSearchFindsContentOnlyMatch searches for a word that appears only inside a
// transcript: the session must be found and marked as a content match.
func TestSearchFindsContentOnlyMatch(t *testing.T) {
	fx := testfixture.BuildClaudeDir(t)
	mixed := fx.Session(t, "mixed")
	if strings.Contains(strings.ToLower(mixed.Display), mixed.SearchWord) {
		t.Fatal("fixture search word must not be in the display text")
	}
	m := newModel(t, fx, 120, 40)

	// run the real background index (a single command, no timers) and feed it back
	idx, ok := m.Init()().(tui.SearchIndexLoadedMsg)
	if !ok {
		t.Fatal("Init did not return an index message")
	}
	if len(idx.Entries) != fx.ScanSessions {
		t.Errorf("indexed %d sessions, want %d (every session with a transcript)", len(idx.Entries), fx.ScanSessions)
	}
	m = upd(m, idx)

	m = runes(m, "/")
	m = runes(m, mixed.SearchWord)
	m = upd(m, tea.KeyMsg{Type: tea.KeyEnter}) // accept the filter
	m = upd(m, tea.KeyMsg{Type: tea.KeyEnter}) // expand the matching project
	show("SEARCH", m)
	view := m.View()
	if !strings.Contains(view, "[content]") {
		t.Errorf("content-only match should be marked [content]:\n%s", view)
	}
	if !listed(view, mixed) {
		t.Errorf("matching session %q not listed:\n%s", mixed.Display, view)
	}
	for _, s := range fx.Sessions {
		if s.Key != "mixed" && listed(view, s) {
			t.Errorf("non-matching session %q is listed", s.Key)
		}
	}

	// a display-text match is not a content match
	m = newModel(t, fx, 120, 40)
	m = upd(m, idx)
	m = runes(m, "/")
	m = runes(m, "parser")
	m = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	if v := m.View(); strings.Contains(v, "[content]") || !listed(v, fx.Session(t, "titled")) {
		t.Errorf("display match for 'parser' should list the session without [content]:\n%s", v)
	}
}
