package tui

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// The key-hint bar is wider than a narrow terminal; it used to wrap onto a
// second row and push the whole view past the terminal height. A transient
// status message must survive truncation, so it is rendered first.
func TestStatusBarNeverWrapsAndKeepsStatusMessage(t *testing.T) {
	for _, msg := range []string{"Privacy ON", "Index cache save failed"} {
		for _, w := range []int{40, 60, 80, 120} {
			m := Model{width: w, height: 30, privacyMode: true, liveWatching: true,
				statusMsg: msg, statusExpires: time.Now().Add(time.Minute),
				flaggedSessions: map[string]bool{"a": true}}
			bar := m.renderStatusBar()
			if n := len(strings.Split(bar, "\n")); n != 1 {
				t.Errorf("width %d: status bar is %d rows, want 1", w, n)
			}
			plain := strings.TrimSpace(ansiRE.ReplaceAllString(bar, ""))
			if lipgloss.Width(msg) <= w-2 {
				if !strings.HasPrefix(plain, msg) {
					t.Errorf("width %d: bar should start with %q, got %q", w, msg, plain)
				}
			} else if !strings.HasPrefix(plain, msg[:w-2-1]) {
				t.Errorf("width %d: bar should start with the message, got %q", w, plain)
			}
			if w == 120 && !strings.Contains(plain, "Navigate") {
				t.Errorf("width 120: key hints missing: %q", plain)
			}
		}
	}
}
