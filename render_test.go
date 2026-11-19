package main

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/user/claudevu/internal/data"
	"github.com/user/claudevu/internal/testfixture"
	"github.com/user/claudevu/internal/tui"
)

// ---- shared helpers for the TUI tests ----

func upd(m tui.Model, msg tea.Msg) tui.Model {
	r, _ := m.Update(msg)
	return r.(tui.Model)
}

func runes(m tui.Model, s string) tui.Model {
	return upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

func press(m tui.Model, k tea.KeyType, n int) tui.Model {
	for i := 0; i < n; i++ {
		m = upd(m, tea.KeyMsg{Type: k})
	}
	return m
}

func newModel(t *testing.T, fx *testfixture.Fixture, w, h int) tui.Model {
	t.Helper()
	m := tui.NewModel(fx.ClaudeDir, tui.Options{})
	return upd(m, tea.WindowSizeMsg{Width: w, Height: h})
}

// show prints a screen only under `go test -v`.
func show(name string, m tui.Model) {
	if testing.Verbose() {
		fmt.Printf("\n=== %s ===\n%s\n", name, m.View())
	}
}

func lineCount(s string) int { return strings.Count(s, "\n") + 1 }

// checkFits fails if the view is taller than the terminal.
func checkFits(t *testing.T, step string, m tui.Model, w, h int) {
	t.Helper()
	if n := lineCount(m.View()); n > h {
		t.Errorf("%s at %dx%d: view has %d lines, terminal has %d", step, w, h, n, h)
	}
}

// rowOf returns the list-row index (0-based, across all expanded projects) of a
// project or session in a model where every project is expanded, sessions
// collapsed.
func rowOf(fx *testfixture.Fixture, key string) int {
	row := 0
	for _, p := range fx.Projects {
		row++
		for _, s := range p.Sessions {
			if s.Key == key {
				return row
			}
			row++
		}
	}
	return -1
}

// expandAllProjects opens every project (cursor ends on the last project row).
func expandAllProjects(m tui.Model, fx *testfixture.Fixture) tui.Model {
	for i, p := range fx.Projects {
		m = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
		if i < len(fx.Projects)-1 {
			m = press(m, tea.KeyDown, len(p.Sessions)+1)
		}
	}
	return m
}

// cursorTo moves the cursor to a flat row index.
func cursorTo(m tui.Model, row int) tui.Model {
	m = press(m, tea.KeyUp, 60)
	return press(m, tea.KeyDown, row)
}

func quickStats(t *testing.T, s *testfixture.SessionExp) data.SessionStats {
	t.Helper()
	st, err := data.QuickScanStats(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// ---- tests ----

func TestFullRender(t *testing.T) {
	fx := testfixture.BuildClaudeDir(t)
	m := newModel(t, fx, 120, 40)
	view := m.View()
	show("INITIAL", m)

	if want := fmt.Sprintf("Sessions (%d)", len(fx.Sessions)); !strings.Contains(view, want) {
		t.Errorf("initial view lacks %q", want)
	}
	for _, p := range fx.Projects {
		if want := fmt.Sprintf("%s (%d)", p.Path, len(p.Sessions)); !strings.Contains(view, want) {
			t.Errorf("initial view lacks project row %q", want)
		}
	}

	// select the most recent session and look at its detail
	m = upd(m, tea.KeyMsg{Type: tea.KeyEnter}) // expand first project
	m = upd(m, tea.KeyMsg{Type: tea.KeyDown})
	m = upd(m, tea.KeyMsg{Type: tea.KeyEnter}) // expand its first session
	rec := fx.Session(t, "recent")
	m = upd(m, tui.QuickStatsLoadedMsg{Stats: quickStats(t, rec), Path: rec.Path})
	show("AFTER SELECT", m)
	view = m.View()
	if !strings.Contains(view, "Session "+rec.ID) {
		t.Errorf("detail pane header should be %q:\n%s", "Session "+rec.ID, view)
	}
	if !strings.Contains(view, "Tools:") || !strings.Contains(view, "3 calls") {
		t.Errorf("detail should report the 3 tool calls (Read + 2 Agent) from the quick scan:\n%s", view)
	}
	if !strings.Contains(view, "Explore") || !strings.Contains(view, "Plan") {
		t.Errorf("session row should list its subagents:\n%s", view)
	}
}

// TestRenderSizesNeverPanicOrOverflow walks a flagged, live-stats session through
// many terminal sizes. The long live-stats badge in a narrow pane used to drive
// a negative width into truncate and panic.
func TestRenderSizesNeverPanicOrOverflow(t *testing.T) {
	fx := testfixture.BuildClaudeDir(t)
	stats := map[string]data.SessionStats{}
	for _, s := range fx.Sessions {
		if s.HasFile {
			stats[s.ID] = quickStats(t, s)
		}
	}
	sizes := [][2]int{{200, 60}, {120, 40}, {100, 30}, {80, 24}, {60, 20}, {60, 12}, {40, 15}, {30, 10}}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			m := newModel(t, fx, w, h)
			checkFits(t, "initial", m, w, h)
			m = expandAllProjects(m, fx)
			checkFits(t, "expanded", m, w, h)

			// flag the recent session (long badge) and a mystery one (unpriced "+")
			m = cursorTo(m, rowOf(fx, "recent"))
			m = runes(m, "f")
			m = cursorTo(m, rowOf(fx, "mystery"))
			m = runes(m, "f")
			m = upd(m, tui.FlaggedRefreshMsg{Stats: stats})
			checkFits(t, "flagged with live stats", m, w, h)
			if w >= 100 { // exact live-stats badges: tokens then cost, "unpriced" for the unknown model
				v := m.View()
				if !strings.Contains(v, "4.5k $0.03") {
					t.Errorf("flagged recent session badge should read \"4.5k $0.03\" at width %d:\n%s", w, v)
				}
				if !strings.Contains(v, "1.1k unpriced") {
					t.Errorf("flagged mystery session badge should read \"1.1k unpriced\" at width %d:\n%s", w, v)
				}
			}

			// open the recent session (subagents), details, logs, search, dashboard, help
			m = cursorTo(m, rowOf(fx, "recent"))
			m = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
			rec := fx.Session(t, "recent")
			m = upd(m, tui.QuickStatsLoadedMsg{Stats: stats[rec.ID], Path: rec.Path})
			checkFits(t, "session open", m, w, h)
			msgs, err := data.LoadTranscript(rec.Path)
			if err != nil {
				t.Fatal(err)
			}
			m = upd(m, tui.TranscriptLoadedMsg{Messages: msgs, Stats: data.ComputeStats(msgs), Path: rec.Path})
			checkFits(t, "logs", m, w, h)
			m = upd(m, tea.KeyMsg{Type: tea.KeyTab})
			m = press(m, tea.KeyDown, 5)
			m = press(m, tea.KeyPgDown, 2)
			checkFits(t, "logs scrolled", m, w, h)
			m = upd(m, tea.KeyMsg{Type: tea.KeyTab})
			m = runes(m, "?")
			if h >= 24 { // the help text is a fixed 22 rows; it is not clipped on shorter terminals
				checkFits(t, "help", m, w, h)
			}
			m = upd(m, tea.KeyMsg{Type: tea.KeyEsc})
			m = runes(m, "/")
			m = runes(m, "mystery")
			checkFits(t, "search", m, w, h)
		})
	}
}

func TestSubagentTreeView(t *testing.T) {
	fx := testfixture.BuildClaudeDir(t)
	rec := fx.Session(t, "recent")
	if len(rec.Subagents) != 2 {
		t.Fatalf("fixture session has %d subagents", len(rec.Subagents))
	}
	m := newModel(t, fx, 120, 50)
	m = expandAllProjects(m, fx)
	m = cursorTo(m, rowOf(fx, "recent"))
	m = upd(m, tea.KeyMsg{Type: tea.KeyEnter}) // expand session -> subagent rows
	m = upd(m, tui.QuickStatsLoadedMsg{Stats: quickStats(t, rec), Path: rec.Path})
	show("SESSION WITH SUBAGENTS", m)

	view := m.View()
	if !strings.Contains(view, "[⚡1/2]") {
		t.Errorf("session row should show 1 of 2 subagents running:\n%s", view)
	}
	var runRow, doneRow string
	for _, ln := range strings.Split(view, "\n") {
		switch {
		case strings.Contains(ln, "Explore"):
			runRow = ln
		case strings.Contains(ln, "Plan"):
			doneRow = ln
		}
	}
	if !strings.Contains(runRow, "running") || !strings.Contains(runRow, data.SubagentStatusEmoji(true)) {
		t.Errorf("running subagent row = %q", runRow)
	}
	if doneRow == "" || strings.Contains(doneRow, "running") || !strings.Contains(doneRow, data.SubagentStatusEmoji(false)) {
		t.Errorf("completed subagent row = %q", doneRow)
	}

	// 's' toggles the subagent tree in the detail pane; the pane stays within the screen
	m = runes(m, "s")
	show("AFTER s", m)
	checkFits(t, "subagent tree", m, 120, 50)
	m = upd(m, tea.KeyMsg{Type: tea.KeyTab})
	m = runes(m, "1")
	checkFits(t, "expand first subagent", m, 120, 50)

	m = runes(m, "?")
	show("HELP", m)
	if v := m.View(); !strings.Contains(strings.ToLower(v), "subagent") {
		t.Errorf("help screen should mention subagents:\n%s", v)
	}
}

func quickMsg(t *testing.T, s *testfixture.SessionExp) tea.Msg {
	return tui.QuickStatsLoadedMsg{Stats: quickStats(t, s), Path: s.Path}
}

func flaggedMsg(stats map[string]data.SessionStats) tea.Msg {
	return tui.FlaggedRefreshMsg{Stats: stats}
}

func transcriptMsg(msgs []data.SessionMessage, path string) tea.Msg {
	return tui.TranscriptLoadedMsg{Messages: msgs, Stats: data.ComputeStats(msgs), Path: path}
}

// listed reports whether a session's row (display text, possibly truncated by
// the pane width) is visible in view.
func listed(view string, s *testfixture.SessionExp) bool {
	d := s.Display
	if len(d) > 8 {
		d = d[:8]
	}
	return strings.Contains(view, d)
}
