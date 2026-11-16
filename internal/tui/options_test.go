package tui

import (
	"testing"

	"github.com/user/claudevu/internal/data"
)

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

func TestPricingFetchOptions(t *testing.T) {
	o, err := PricingFetchOptions("")
	if err != nil || o.URL != data.DefaultPricingURL || len(o.AllowHosts) != 0 {
		t.Errorf("default source: %+v %v", o, err)
	}
	o, err = PricingFetchOptions("https://prices.example.org/pricing.md")
	if err != nil || o.URL != "https://prices.example.org/pricing.md" || len(o.AllowHosts) != 1 || o.AllowHosts[0] != "prices.example.org" {
		t.Errorf("custom source must allow exactly its own host: %+v %v", o, err)
	}
	for _, bad := range []string{"http://prices.example.org/p.md", "ftp://x/y", "file:///etc/passwd", "not a url", "https://", "//host/p"} {
		if _, err := PricingFetchOptions(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
