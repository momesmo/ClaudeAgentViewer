package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/user/claudevu/internal/data"
	"github.com/user/claudevu/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func upd(m tui.Model, msg tea.Msg) tui.Model {
	result, _ := m.Update(msg)
	return result.(tui.Model)
}

func show(name string, m tui.Model) {
	fmt.Printf("\n=== %s ===\n%s\n", name, m.View())
}

func TestTreeView(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	claudeDir := filepath.Join(homeDir, ".claude")

	t0 := time.Now()
	m := tui.NewModel(claudeDir)
	m = upd(m, tea.WindowSizeMsg{Width: 120, Height: 35})
	fmt.Printf("Init: %v\n", time.Since(t0))

	show("1_INITIAL", m)

	// Expand project, navigate to session, expand it
	m = upd(m, tea.KeyMsg{Type: tea.KeyEnter})
	m = upd(m, tea.KeyMsg{Type: tea.KeyDown})
	m = upd(m, tea.KeyMsg{Type: tea.KeyEnter})

	// Simulate quick stats
	groups, _ := data.LoadHistory(claudeDir)
	if len(groups) > 0 && len(groups[0].Sessions) > 0 {
		s := groups[0].Sessions[0]
		path := data.FindSessionFile(claudeDir, s.Project, s.SessionID)
		if path != "" {
			stats, _ := data.QuickScanStats(path)
			m = upd(m, tui.QuickStatsLoadedMsg{Stats: stats, Path: path})
		}
	}
	show("2_SESSION_METADATA", m)

	// Flag this session with 'f'
	// First go back to list focus
	m = upd(m, tea.KeyMsg{Type: tea.KeyTab})
	m = upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	show("3_SESSION_FLAGGED", m)

	// Navigate to another session and flag it too
	m = upd(m, tea.KeyMsg{Type: tea.KeyDown}) // next session
	m = upd(m, tea.KeyMsg{Type: tea.KeyDown}) // skip subagent
	m = upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})

	// Simulate flagged refresh
	if len(groups) > 0 {
		refreshStats := make(map[string]data.SessionStats)
		for _, g := range groups {
			for _, s := range g.Sessions {
				path := data.FindSessionFile(claudeDir, s.Project, s.SessionID)
				if path != "" {
					stats, _ := data.QuickScanStats(path)
					refreshStats[s.SessionID] = stats
				}
			}
		}
		m = upd(m, tui.FlaggedRefreshMsg{Stats: refreshStats})
	}
	show("4_TWO_FLAGGED (live stats in tree)", m)

	// Unflag one
	m = upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	show("5_ONE_UNFLAGGED", m)

	// Press l to view logs with live tailing
	m = upd(m, tea.KeyMsg{Type: tea.KeyUp})
	m = upd(m, tea.KeyMsg{Type: tea.KeyUp})
	m = upd(m, tea.KeyMsg{Type: tea.KeyEnter}) // expand session
	if len(groups) > 0 && len(groups[0].Sessions) > 0 {
		s := groups[0].Sessions[0]
		path := data.FindSessionFile(claudeDir, s.Project, s.SessionID)
		if path != "" {
			stats, _ := data.QuickScanStats(path)
			m = upd(m, tui.QuickStatsLoadedMsg{Stats: stats, Path: path})
		}
	}
	m = upd(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if len(groups) > 0 && len(groups[0].Sessions) > 0 {
		s := groups[0].Sessions[0]
		path := data.FindSessionFile(claudeDir, s.Project, s.SessionID)
		if path != "" {
			msgs, _ := data.LoadTranscript(path)
			stats := data.ComputeStats(msgs)
			m = upd(m, tui.TranscriptLoadedMsg{Messages: msgs, Stats: stats, Path: path})
		}
	}
	show("6_LIVE_TAILING (l pressed)", m)
}
