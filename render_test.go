package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/user/claudevu/internal/data"
	"github.com/user/claudevu/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

// update helper that handles the Model type assertion
func update(m tui.Model, msg tea.Msg) tui.Model {
	result, _ := m.Update(msg)
	return result.(tui.Model)
}

func TestFullRender(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	claudeDir := filepath.Join(homeDir, ".claude")

	m := tui.NewModel(claudeDir)
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	view := m.View()
	fmt.Println("=== INITIAL VIEW (first 25 lines) ===")
	for i, line := range strings.Split(view, "\n") {
		if i > 25 {
			break
		}
		fmt.Println(line)
	}

	if !strings.Contains(view, "Sessions") {
		t.Error("Expected 'Sessions' in initial view")
	}

	// Navigate down and select first session
	m = update(m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(m, tea.KeyMsg{Type: tea.KeyEnter})

	view = m.View()
	fmt.Println("\n=== AFTER SELECT (first 30 lines) ===")
	for i, line := range strings.Split(view, "\n") {
		if i > 30 {
			break
		}
		fmt.Println(line)
	}

	if !strings.Contains(view, "Session") {
		t.Error("Expected session detail in view")
	}
}

func TestSubagentTreeView(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	claudeDir := filepath.Join(homeDir, ".claude")

	groups, err := data.LoadHistory(claudeDir)
	if err != nil {
		t.Skip("No history")
	}

	// Find session with subagents and its list position
	var targetSessionID string
	for _, g := range groups {
		for _, s := range g.Sessions {
			subagents, _ := data.ScanSubagents(claudeDir, s.SessionID)
			if len(subagents) > 0 {
				targetSessionID = s.SessionID
				running := data.CountRunningSubagents(subagents)
				fmt.Printf("Target session: %s (%d subagents, %d running)\n",
					s.SessionID, len(subagents), running)
				for i, sa := range subagents {
					if i >= 5 {
						fmt.Printf("  ... and %d more\n", len(subagents)-5)
						break
					}
					fmt.Printf("  %s %s %s [%s] running=%v\n",
						data.SubagentStatusEmoji(sa.IsRunning),
						data.SubagentTypeEmoji(sa.AgentType),
						sa.AgentID, sa.AgentType, sa.IsRunning)
				}
				break
			}
		}
		if targetSessionID != "" {
			break
		}
	}

	if targetSessionID == "" {
		t.Skip("No session with subagents")
	}

	// Build model and simulate navigating to the session
	m := tui.NewModel(claudeDir)
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 50})

	// Navigate through list to find the target session
	for i := 0; i < 100; i++ {
		m = update(m, tea.KeyMsg{Type: tea.KeyDown})
		// Try selecting
		mCopy := update(m, tea.KeyMsg{Type: tea.KeyEnter})
		view := mCopy.View()
		if strings.Contains(view, targetSessionID[:8]) {
			m = mCopy
			fmt.Printf("Found target at position %d\n", i)
			break
		}
	}

	view := m.View()
	fmt.Println("\n=== SESSION WITH SUBAGENTS ===")
	for i, line := range strings.Split(view, "\n") {
		if i > 15 {
			break
		}
		fmt.Println(line)
	}

	// Check if subagent count shows
	if strings.Contains(view, "Subagent") {
		fmt.Println("✓ Subagent section visible in detail")
	} else {
		fmt.Println("✗ Subagent section NOT visible (might need to scroll)")
	}

	// Press 's' to toggle subagent tree
	m = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})

	view = m.View()
	fmt.Println("\n=== AFTER 's' (SUBAGENT TREE) ===")
	for i, line := range strings.Split(view, "\n") {
		if i > 35 {
			break
		}
		fmt.Println(line)
	}

	// Switch to detail pane and expand first subagent
	m = update(m, tea.KeyMsg{Type: tea.KeyTab})
	m = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})

	view = m.View()
	fmt.Println("\n=== AFTER '1' (EXPAND FIRST SUBAGENT) ===")
	allLines := strings.Split(view, "\n")
	for i, line := range allLines {
		if i > 40 {
			fmt.Printf("... (%d more lines)\n", len(allLines)-40)
			break
		}
		fmt.Println(line)
	}

	// Help test
	m = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	view = m.View()
	fmt.Println("\n=== HELP SCREEN ===")
	fmt.Println(view)

	if !strings.Contains(view, "subagent") && !strings.Contains(view, "Subagent") {
		t.Error("Help screen should mention subagents")
	}
}
