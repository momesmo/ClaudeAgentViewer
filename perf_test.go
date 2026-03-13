package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/user/claudevu/internal/data"
)

func TestPerfProfile(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	claudeDir := filepath.Join(homeDir, ".claude")

	t0 := time.Now()
	groups, err := data.LoadHistory(claudeDir)
	t1 := time.Now()
	fmt.Printf("LoadHistory: %v (%d groups)\n", t1.Sub(t0), len(groups))
	if err != nil {
		t.Fatal(err)
	}

	// Find the current session (largest, most recent)
	if len(groups) > 0 && len(groups[0].Sessions) > 0 {
		s := groups[0].Sessions[0]
		fmt.Printf("Most recent session: %s in %s\n", s.SessionID, s.Project)

		path := data.FindSessionFile(claudeDir, s.Project, s.SessionID)
		fmt.Printf("Session file: %s\n", path)

		if path != "" {
			fi, _ := os.Stat(path)
			fmt.Printf("File size: %d bytes\n", fi.Size())

			t2 := time.Now()
			msgs, _ := data.LoadTranscript(path)
			t3 := time.Now()
			fmt.Printf("LoadTranscript: %v (%d messages)\n", t3.Sub(t2), len(msgs))

			t4 := time.Now()
			stats := data.ComputeStats(msgs)
			t5 := time.Now()
			fmt.Printf("ComputeStats: %v (tokens: %d)\n", t5.Sub(t4), stats.TotalInputTokens+stats.TotalOutputTokens)

			t6 := time.Now()
			subs, _ := data.ScanSubagents(claudeDir, s.SessionID)
			t7 := time.Now()
			fmt.Printf("ScanSubagents: %v (%d found)\n", t7.Sub(t6), len(subs))
		}
	}

	// Also check FindSessionFile perf
	if len(groups) > 0 && len(groups[0].Sessions) > 0 {
		s := groups[0].Sessions[0]
		t8 := time.Now()
		for i := 0; i < 10; i++ {
			data.FindSessionFile(claudeDir, s.Project, s.SessionID)
		}
		t9 := time.Now()
		fmt.Printf("FindSessionFile x10: %v\n", t9.Sub(t8))
	}

	// Check all session file sizes
	totalSize := int64(0)
	for _, g := range groups {
		for _, s := range g.Sessions {
			path := data.FindSessionFile(claudeDir, s.Project, s.SessionID)
			if path != "" {
				if fi, err := os.Stat(path); err == nil {
					totalSize += fi.Size()
				}
			}
		}
	}
	fmt.Printf("Total session data: %.1f MB across %d sessions\n",
		float64(totalSize)/1024/1024, countSessions(groups))
}

func countSessions(groups []data.ProjectGroup) int {
	n := 0
	for _, g := range groups {
		n += len(g.Sessions)
	}
	return n
}
