package tui

import "testing"

func TestResolveIndexCache(t *testing.T) {
	cases := []struct {
		set, val bool
		env      string
		want     bool
	}{
		{false, false, "1", true},
		{false, false, "TRUE", true},
		{false, false, "0", false},
		{false, false, "", false},
		{true, true, "", true},
		{true, false, "1", false},
	}
	for _, c := range cases {
		if got := ResolveIndexCache(c.set, c.val, c.env); got != c.want {
			t.Errorf("%+v got %v", c, got)
		}
	}
}
