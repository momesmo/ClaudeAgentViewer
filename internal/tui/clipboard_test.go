package tui

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/user/claudevu/internal/data"
)

func TestClipboardCommand(t *testing.T) {
	cases := []struct {
		name  string
		goos  string
		env   map[string]string
		tools []string
		cmd   string
		args  []string
		err   bool
	}{
		{"windows", "windows", nil, nil, "clip", nil, false},
		{"darwin", "darwin", nil, nil, "pbcopy", nil, false},
		{"wayland with wl-copy", "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, []string{"wl-copy", "xclip"}, "wl-copy", nil, false},
		{"wayland without wl-copy falls to xclip", "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, []string{"xclip"}, "xclip", []string{"-selection", "clipboard"}, false},
		{"wl-copy present but no wayland", "linux", nil, []string{"wl-copy", "xclip"}, "xclip", []string{"-selection", "clipboard"}, false},
		{"x11 xclip", "linux", nil, []string{"xclip", "xsel"}, "xclip", []string{"-selection", "clipboard"}, false},
		{"x11 xsel only", "linux", nil, []string{"xsel"}, "xsel", []string{"--clipboard", "--input"}, false},
		{"wayland, only xsel", "freebsd", map[string]string{"WAYLAND_DISPLAY": "w"}, []string{"xsel"}, "xsel", []string{"--clipboard", "--input"}, false},
		{"neither", "linux", nil, nil, "", nil, true},
		{"unknown os none", "plan9", nil, nil, "", nil, true},
		{"unknown os xclip", "plan9", nil, []string{"xclip"}, "xclip", []string{"-selection", "clipboard"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			getenv := func(k string) string { return c.env[k] }
			lp := func(n string) (string, error) {
				for _, x := range c.tools {
					if x == n {
						return "/bin/" + n, nil
					}
				}
				return "", errors.New("not found")
			}
			cmd, args, err := clipboardCommand(c.goos, getenv, lp)
			if (err != nil) != c.err {
				t.Fatalf("err = %v, want err=%v", err, c.err)
			}
			if err != nil {
				if !strings.Contains(err.Error(), "no clipboard tool found") {
					t.Errorf("unexpected error text %q", err)
				}
				return
			}
			if cmd != c.cmd || !reflect.DeepEqual(args, c.args) {
				t.Errorf("got %q %v, want %q %v", cmd, args, c.cmd, c.args)
			}
		})
	}
}

// hostile is text a shell or an argument parser would treat specially.
const hostile = "'; rm -rf / # `$(touch /tmp/pwn)` && calc \"%PATH%\"\n"

// The real guarantee: the text reaches the tool ONLY on stdin. It is never part of
// the command name or arguments, and no shell sits in between.
func TestNewClipboardCmd_TextOnlyOnStdin(t *testing.T) {
	lp := func(n string) (string, error) { return "/bin/" + n, nil }
	shells := map[string]bool{"sh": true, "bash": true, "zsh": true, "cmd": true, "cmd.exe": true, "powershell": true, "pwsh": true}
	for _, goos := range []string{"windows", "darwin", "linux", "plan9"} {
		for _, wayland := range []string{"", "wayland-0"} {
			name, args, err := clipboardCommand(goos, func(k string) string {
				if k == "WAYLAND_DISPLAY" {
					return wayland
				}
				return ""
			}, lp)
			if err != nil {
				t.Fatal(err)
			}
			cmd := newClipboardCmd(context.Background(), name, args, hostile)
			if shells[filepath.Base(cmd.Args[0])] {
				t.Errorf("%s: the clipboard command must not go through a shell: %v", goos, cmd.Args)
			}
			for _, a := range cmd.Args {
				if strings.ContainsAny(a, "'`$;&|\n") || strings.Contains(a, "rm -rf") {
					t.Errorf("%s: hostile text leaked into the arguments: %q", goos, cmd.Args)
				}
			}
			got, err := io.ReadAll(cmd.Stdin)
			if err != nil || string(got) != hostile {
				t.Errorf("%s: stdin = %q, want exactly the text", goos, got)
			}
		}
	}
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sleep/sh/false to stand in for a clipboard tool (platform skip, not a data skip)")
	}
}

// A wedged clipboard tool must not hang the caller.
func TestRunClipboardCmd_TimesOut(t *testing.T) {
	skipOnWindows(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := runClipboardCmd(ctx, "sleep", []string{"30"}, "x")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a timeout", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %v: the timeout did not bound the call", d)
	}
}

func TestRunClipboardCmd_FailureIsShortAndSafe(t *testing.T) {
	skipOnWindows(t)
	err := runClipboardCmd(context.Background(), "false", nil, "x")
	if err == nil || err.Error() != "false: exit status 1" {
		t.Errorf("err = %v", err)
	}
	if err := runClipboardCmd(context.Background(), "definitely-not-a-real-tool", nil, "x"); err == nil || len(err.Error()) > 200 {
		t.Errorf("missing tool: %v", err)
	}
}

func TestRunClipboardCmd_DeliversTextOnStdin(t *testing.T) {
	skipOnWindows(t)
	out := filepath.Join(t.TempDir(), "got")
	if err := runClipboardCmd(context.Background(), "sh", []string{"-c", `cat > "$1"`, "sh", out}, hostile); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); string(b) != hostile {
		t.Errorf("tool received %q", b)
	}
}

// Pressing `c` must not run the tool on the UI goroutine: it returns a command,
// and the outcome arrives as a message.
func TestCopyKeyIsAsyncAndReportsOutcome(t *testing.T) {
	s := data.SessionInfo{SessionID: "abc-123"}
	m := Model{width: 120, height: 30, selectedInfo: &s}
	r, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if cmd == nil {
		t.Fatal("copy must return a command so it runs off the UI goroutine")
	}
	if got := r.(Model).statusMsg; got != "" {
		t.Errorf("no result is known yet, status = %q", got)
	}
	r, _ = r.Update(ClipboardCopiedMsg{What: "session ID"})
	if got := r.(Model).statusMsg; got != "Copied session ID" {
		t.Errorf("success status = %q", got)
	}
	r, _ = r.Update(ClipboardCopiedMsg{What: "session ID", Err: errors.New(strings.Repeat("boom ", 100))})
	got := r.(Model).statusMsg
	if !strings.HasPrefix(got, "Copy failed: boom") || len([]rune(got)) > 100 {
		t.Errorf("failure status must be short: %q", got)
	}
}
