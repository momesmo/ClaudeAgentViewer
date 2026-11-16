package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/user/claudevu/internal/data"
	"github.com/user/claudevu/internal/testenv"
	"github.com/user/claudevu/internal/testfixture"
)

// TestPerfSmokeFixture times the whole data path on the fixture and asserts it
// stays far below any realistic budget (a regression guard, not a benchmark).
func TestPerfSmokeFixture(t *testing.T) {
	fx := testfixture.BuildClaudeDir(t)
	t0 := time.Now()
	groups, err := data.LoadHistory(fx.ClaudeDir)
	if err != nil || len(groups) != len(fx.Projects) {
		t.Fatalf("LoadHistory: %v (%d groups)", err, len(groups))
	}
	n := 0
	for _, g := range groups {
		for _, s := range g.Sessions {
			if s.FilePath == "" {
				continue
			}
			if _, err := data.QuickScanStats(s.FilePath); err != nil {
				t.Fatal(err)
			}
			msgs, err := data.LoadTranscript(s.FilePath)
			if err != nil {
				t.Fatal(err)
			}
			data.ComputeStats(msgs)
			data.ScanSubagents(fx.ClaudeDir, s.SessionID)
			n++
		}
	}
	if n != fx.ScanSessions {
		t.Errorf("scanned %d sessions, want %d", n, fx.ScanSessions)
	}
	if d := time.Since(t0); d > 10*time.Second {
		t.Errorf("fixture data path took %v", d)
	}
	if testing.Verbose() {
		fmt.Printf("fixture data path: %v for %d sessions\n", time.Since(t0), n)
	}
}

// TestPerfProfileRealData profiles the developer's own ~/.claude. It is opt-in
// (CLAUDEVU_TEST_REAL_CLAUDE=1) because it reads real, private data.
func TestPerfProfileRealData(t *testing.T) {
	home, ok := testenv.RealHome()
	if !ok {
		t.Skip("opt-in real-data profile: set CLAUDEVU_TEST_REAL_CLAUDE=1 to profile your own ~/.claude")
	}
	t.Setenv("HOME", home) // the suite isolates HOME; this test deliberately uses the real one
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	claudeDir := filepath.Join(userHome, ".claude")

	t0 := time.Now()
	groups, err := data.LoadHistory(claudeDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("LoadHistory: %v (%d groups)", time.Since(t0), len(groups))

	totalSize, sessions := int64(0), 0
	for _, g := range groups {
		for _, s := range g.Sessions {
			sessions++
			if s.FilePath == "" {
				continue
			}
			if fi, err := os.Stat(s.FilePath); err == nil {
				totalSize += fi.Size()
			}
		}
	}
	t.Logf("Total session data: %.1f MB across %d sessions", float64(totalSize)/1024/1024, sessions)
	if len(groups) > 0 && len(groups[0].Sessions) > 0 {
		s := groups[0].Sessions[0]
		t1 := time.Now()
		msgs, _ := data.LoadTranscript(s.FilePath)
		t.Logf("LoadTranscript(latest): %v (%d messages)", time.Since(t1), len(msgs))
		t2 := time.Now()
		st := data.ComputeStats(msgs)
		t.Logf("ComputeStats: %v (tokens %d)", time.Since(t2), st.TotalInputTokens+st.TotalOutputTokens)
	}
}
