package tui

import (
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// historyChangedMsg signals that history.jsonl has been modified
type historyChangedMsg struct{}

// tickMsg is a periodic check for file changes (fallback for fsnotify issues)
type tickMsg time.Time

// watchInterval is how often we poll for changes
const watchInterval = 2 * time.Second

// watchHistoryCmd returns a command that polls history.jsonl for changes
func watchHistoryCmd(claudeDir string, lastModTime time.Time) tea.Cmd {
	return tea.Tick(watchInterval, func(t time.Time) tea.Msg {
		histPath := filepath.Join(claudeDir, "history.jsonl")
		info, err := os.Stat(histPath)
		if err != nil {
			return tickMsg(t)
		}
		if info.ModTime().After(lastModTime) {
			return historyChangedMsg{}
		}
		return tickMsg(t)
	})
}

// watchSessionCmd polls a specific session file for changes (live tailing)
type sessionChangedMsg struct {
	path string
}

func watchSessionCmd(path string, lastModTime time.Time) tea.Cmd {
	if path == "" {
		return nil
	}
	return tea.Tick(watchInterval, func(t time.Time) tea.Msg {
		info, err := os.Stat(path)
		if err != nil {
			return tickMsg(t)
		}
		if info.ModTime().After(lastModTime) {
			return sessionChangedMsg{path: path}
		}
		return tickMsg(t)
	})
}
