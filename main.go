package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/user/claudevu/internal/tui"
)

var version = "dev"

func main() {
	var (
		showVersion bool
	)

	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Printf("claudevu %s\n", version)
		os.Exit(0)
	}

	// Determine Claude directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine home directory: %v\n", err)
		os.Exit(1)
	}
	claudeDir := filepath.Join(homeDir, ".claude")

	// Verify it exists
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: %s not found. Is Claude Code installed?\n", claudeDir)
		os.Exit(1)
	}

	model := tui.NewModel(claudeDir)

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseAllMotion())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// If user requested resume, exec claude after TUI exits
	if m, ok := finalModel.(tui.Model); ok && m.ResumeSessionID != "" {
		fmt.Printf("Resuming session %s...\n", m.ResumeSessionID)
		cmd := exec.Command("claude", "--resume", m.ResumeSessionID)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error resuming session: %v\n", err)
			os.Exit(1)
		}
	}
}
