package tui

import "testing"

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello..."},
		{"hello", 2, "he"},
		{"hello", 0, ""},
		{"hello", -5, ""}, // negative width must not panic
		{"", -1, ""},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.max); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}
