package tui

import (
	"errors"
	"reflect"
	"strings"
	"testing"
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

func TestClipboardCommandNeverContainsText(t *testing.T) {
	const secret = "SESSION-ID-9f8e7d"
	lp := func(n string) (string, error) { return "/bin/" + n, nil }
	for _, goos := range []string{"windows", "darwin", "linux", "plan9"} {
		cmd, args, err := clipboardCommand(goos, func(string) string { return "x" }, lp)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(cmd+" "+strings.Join(args, " "), secret) {
			t.Errorf("%s: text leaked into command", goos)
		}
	}
}
