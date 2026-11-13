package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/user/claudevu/internal/data"
	"github.com/user/claudevu/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDashboard(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	claudeDir := filepath.Join(homeDir, ".claude")

	m := tui.NewModel(claudeDir, tui.Options{})
	m = func(m tui.Model, msg tea.Msg) tui.Model {
		r, _ := m.Update(msg); return r.(tui.Model)
	}(m, tea.WindowSizeMsg{Width: 100, Height: 40})

	// Press 'd' to open dashboard
	m = func(m tui.Model, msg tea.Msg) tui.Model {
		r, _ := m.Update(msg); return r.(tui.Model)
	}(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})

	// Simulate dashboard load
	groups, _ := data.LoadHistory(claudeDir)
	statsMap := make(map[string]data.SessionStats)
	for _, g := range groups {
		for _, s := range g.Sessions {
			path := data.FindSessionFile(claudeDir, s.Project, s.SessionID)
			if path != "" {
				stats, _ := data.QuickScanStats(path)
				statsMap[s.SessionID] = stats
			}
		}
	}
	d := data.BuildDashboard(groups, statsMap)

	m = func(m tui.Model, msg tea.Msg) tui.Model {
		r, _ := m.Update(msg); return r.(tui.Model)
	}(m, tui.DashboardLoadedMsg{Data: d})

	fmt.Println(m.View())
}
