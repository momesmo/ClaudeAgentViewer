package tui

import (
	"strings"
	"testing"
	"time"
)

// The key-hint bar is wider than a narrow terminal; it used to wrap onto a
// second row and push the whole view past the terminal height.
func TestStatusBarNeverWraps(t *testing.T) {
	for _, w := range []int{30, 40, 60, 80, 120} {
		m := Model{width: w, height: 30, privacyMode: true, liveWatching: true,
			statusMsg: "Flagged for monitoring", statusExpires: time.Now().Add(time.Minute),
			flaggedSessions: map[string]bool{"a": true}}
		if n := len(strings.Split(m.renderStatusBar(), "\n")); n != 1 {
			t.Errorf("width %d: status bar is %d rows, want 1", w, n)
		}
	}
}
