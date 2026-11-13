package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/user/claudevu/internal/data"
	"github.com/user/claudevu/internal/tui"
)

var version = "dev"

func main() {
	var (
		showVersion bool
		indexCache  bool
		clearCache  bool
		cacheDirArg string
	)

	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.BoolVar(&indexCache, "index-cache", false, "Persist the search index to an on-disk cache, best-effort secret redaction, see README (also CLAUDEVU_INDEX_CACHE=1)")
	flag.BoolVar(&clearCache, "clear-cache", false, "Delete the on-disk index cache and exit")
	flag.StringVar(&cacheDirArg, "cache-dir", "", "Cache directory (default: user cache dir/claudevu)")
	flag.Parse()

	if showVersion {
		fmt.Printf("claudevu %s\n", version)
		os.Exit(0)
	}

	flagSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "index-cache" {
			flagSet = true
		}
	})
	enableCache := tui.ResolveIndexCache(flagSet, indexCache, os.Getenv("CLAUDEVU_INDEX_CACHE"))

	cacheDir := cacheDirArg
	if cacheDir == "" {
		d, err := data.DefaultCacheDir()
		if err != nil {
			if clearCache {
				fmt.Fprintf(os.Stderr, "Error: cannot determine cache directory: %v\n", err)
				os.Exit(1)
			}
			if enableCache {
				fmt.Fprintf(os.Stderr, "Warning: cannot determine cache directory (%v); index cache disabled\n", err)
				enableCache = false
			}
		} else {
			cacheDir = d
		}
	}

	if cacheDir != "" {
		if home, herr := os.UserHomeDir(); herr == nil && data.PathInside(cacheDir, filepath.Join(home, ".claude")) {
			if clearCache {
				fmt.Fprintf(os.Stderr, "Error: refusing cache dir %s inside ~/.claude\n", cacheDir)
				os.Exit(1)
			}
			if enableCache {
				fmt.Fprintf(os.Stderr, "Warning: cache dir %s is inside ~/.claude; index cache disabled\n", cacheDir)
				enableCache = false
			}
		}
	}

	if clearCache {
		path := data.IndexPath(cacheDir)
		removed, err := data.ClearIndex(cacheDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if removed {
			fmt.Printf("Removed %s\n", path)
		} else {
			fmt.Printf("No cache found at %s\n", path)
		}
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

	model := tui.NewModel(claudeDir, tui.Options{IndexCache: enableCache, CacheDir: cacheDir})

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
		if m.ResumeProjectDir != "" {
			cmd.Dir = m.ResumeProjectDir
		}
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error resuming session: %v\n", err)
			os.Exit(1)
		}
	}
}
