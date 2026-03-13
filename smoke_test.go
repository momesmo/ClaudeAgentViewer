package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/user/claudevu/internal/data"
)

func TestLoadHistory(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	claudeDir := filepath.Join(homeDir, ".claude")

	groups, err := data.LoadHistory(claudeDir)
	if err != nil {
		t.Fatalf("LoadHistory failed: %v", err)
	}

	if len(groups) == 0 {
		t.Fatal("Expected at least one project group")
	}

	totalSessions := 0
	for _, g := range groups {
		totalSessions += len(g.Sessions)
		fmt.Printf("Project: %s (%d sessions)\n", g.Project, len(g.Sessions))
		for i, s := range g.Sessions {
			if i >= 3 {
				fmt.Printf("  ... and %d more\n", len(g.Sessions)-3)
				break
			}
			fmt.Printf("  %s | %s\n", s.Timestamp.Format("01/02 15:04"), truncateStr(s.Display, 60))
		}
	}
	fmt.Printf("\nTotal: %d projects, %d sessions\n", len(groups), totalSessions)
}

func TestLoadTranscript(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	claudeDir := filepath.Join(homeDir, ".claude")

	groups, err := data.LoadHistory(claudeDir)
	if err != nil || len(groups) == 0 {
		t.Skip("No history available")
	}

	// Load the first session's transcript
	info := groups[0].Sessions[0]
	path := data.FindSessionFile(claudeDir, info.Project, info.SessionID)
	if path == "" {
		t.Skip("Session file not found")
	}

	messages, err := data.LoadTranscript(path)
	if err != nil {
		t.Fatalf("LoadTranscript failed: %v", err)
	}

	stats := data.ComputeStats(messages)
	fmt.Printf("Session: %s\n", info.SessionID)
	fmt.Printf("Messages: %d, Tools: %d, Input: %s, Output: %s, Cost: $%.2f\n",
		stats.MessageCount, stats.ToolCallCount,
		formatTok(stats.TotalInputTokens), formatTok(stats.TotalOutputTokens),
		stats.EstimatedCostUSD)
	fmt.Printf("Models: %v\n", stats.Models)

	for i, msg := range messages {
		if i >= 5 {
			fmt.Printf("... and %d more messages\n", len(messages)-5)
			break
		}
		switch msg.Type {
		case "user":
			fmt.Printf("[USER] %s\n", truncateStr(msg.Content, 80))
		case "assistant":
			for _, b := range msg.Blocks {
				switch b.Type {
				case "text":
					fmt.Printf("[ASST] %s\n", truncateStr(b.Text, 80))
				case "tool_use":
					fmt.Printf("[TOOL] %s\n", b.ToolName)
				case "thinking":
					fmt.Printf("[THINK] %s\n", truncateStr(b.Text, 60))
				}
			}
		}
	}
}

func TestSubagents(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	claudeDir := filepath.Join(homeDir, ".claude")

	// Find a session with subagents
	groups, err := data.LoadHistory(claudeDir)
	if err != nil {
		t.Skip("No history")
	}

	found := false
	for _, g := range groups {
		for _, s := range g.Sessions {
			subagents, err := data.ScanSubagents(claudeDir, s.SessionID)
			if err != nil || len(subagents) == 0 {
				continue
			}
			found = true
			running := data.CountRunningSubagents(subagents)
			fmt.Printf("Session %s has %d subagents (%d running):\n", s.SessionID, len(subagents), running)
			// Only load first 3 transcripts to keep test fast
			for i, sa := range subagents {
				status := data.SubagentStatusEmoji(sa.IsRunning)
				if i < 3 {
					_ = data.LoadSubagentTranscript(&subagents[i])
					fmt.Printf("  %s %s %s [%s] — %d msgs, %d tools, %s tokens\n",
						status,
						data.SubagentTypeEmoji(sa.AgentType),
						sa.AgentID, sa.AgentType,
						subagents[i].Stats.MessageCount, subagents[i].Stats.ToolCallCount,
						formatTok(subagents[i].Stats.TotalInputTokens+subagents[i].Stats.TotalOutputTokens))
				} else if i == 3 {
					fmt.Printf("  ... and %d more\n", len(subagents)-3)
				}
			}
			// Test QuickSubagentCount
			qTotal, qRunning := data.QuickSubagentCount(claudeDir, s.SessionID)
			fmt.Printf("  QuickCount: %d total, %d running\n", qTotal, qRunning)
			// Test RefreshSubagentStatus
			refreshed := data.RefreshSubagentStatus(claudeDir, s.SessionID, subagents)
			fmt.Printf("  After refresh: %d subagents, %d running\n",
				len(refreshed), data.CountRunningSubagents(refreshed))
			break
		}
		if found {
			break
		}
	}

	if !found {
		t.Log("No sessions with subagents found (not a failure)")
	}
}

func TestPrivacy(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"key: sk-proj-abc123def456ghi789jkl012", "key: [REDACTED]"},
		{"token: ghp_abcdefghijklmnopqrstuvwxyz1234567890", "token: [REDACTED]"},
		{"no secrets here", "no secrets here"},
	}
	for _, tt := range tests {
		result := data.RedactSecrets(tt.input)
		if result != tt.expected {
			t.Errorf("RedactSecrets(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func formatTok(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}
