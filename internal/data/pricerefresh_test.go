package data

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDiffBooks(t *testing.T) {
	old := DefaultBook()
	if got := DiffBooks(old, old); len(got) != 0 {
		t.Fatalf("identical books must have no changes, got %v", got)
	}
	nb := old.Merge(&PriceBook{Version: 1, Models: map[string]ModelPrice{
		"claude-opus-5-5": {Rate: Rate{Input: 5, CacheWrite5m: 6.25, CacheWrite1h: 10, CacheRead: 0.25, Output: 25}}, // +25%: ordinary change
		"claude-opus-5-6": {Rate: Rate{Input: 4, CacheWrite5m: 5, CacheWrite1h: 8, CacheRead: 0.2, Output: 20}},      // new model
	}})
	changes := DiffBooks(old, nb)
	if len(changes) != 2 {
		t.Fatalf("want 2 changes, got %v", changes)
	}
	byModel := map[string]PriceChange{}
	for _, c := range changes {
		byModel[c.Model] = c
	}
	if c := byModel["claude-opus-5-6"]; !c.Added || c.Suspicious() {
		t.Errorf("new model: %+v", c)
	}
	c := byModel["claude-opus-5-5"]
	if c.Added || c.Suspicious() || !strings.Contains(c.String(), "$4") || !strings.Contains(c.String(), "$5") {
		t.Errorf("ordinary change: %+v %q", c, c.String())
	}
	cheap := old.Merge(&PriceBook{Version: 1, Models: map[string]ModelPrice{
		"claude-opus-5-5": {Rate: Rate{Input: 0.04, CacheWrite5m: 0.05, CacheWrite1h: 0.08, CacheRead: 0.002, Output: 20}}, // input 100x cheaper
	}})
	if s := SuspiciousChanges(DiffBooks(old, cheap)); len(s) != 1 || s[0].Model != "claude-opus-5-5" {
		t.Errorf("a 100x price drop must be suspicious: %v", s)
	}
	dear := old.Merge(&PriceBook{Version: 1, Models: map[string]ModelPrice{
		"claude-sonnet-5-5": {Rate: Rate{Input: 20, CacheWrite5m: 25, CacheWrite1h: 40, CacheRead: 1, Output: 100}},
	}})
	if s := SuspiciousChanges(DiffBooks(old, dear)); len(s) != 1 {
		t.Errorf("a 10x price rise must be suspicious: %v", s)
	}
}

// pageWith returns the fixture page with the Opus 5.5 row replaced.
func pageWith(t *testing.T, row string) []byte {
	t.Helper()
	var out []string
	replaced := false
	for _, l := range strings.Split(string(fixture(t)), "\n") {
		if !replaced && strings.HasPrefix(l, "| Claude Opus 5.5 ") && strings.Contains(l, "Base") == false && strings.Contains(l, "/ MTok") && strings.Count(l, "|") >= 6 && !strings.Contains(l, "USD") {
			l, replaced = row, true
		}
		out = append(out, l)
	}
	if !replaced {
		t.Fatal("fixture row not found")
	}
	return []byte(strings.Join(out, "\n"))
}

func TestRefreshVetted(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	t.Run("unchanged page is applied", func(t *testing.T) {
		cache, cfg := t.TempDir(), t.TempDir()
		_, o := tlsServer(t, serveMD(t))
		res, err := RefreshPricingVetted(context.Background(), cache, cfg, o, now, false)
		if err != nil || !res.Applied || len(res.Changes) != 0 {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		if _, ok := LoadPricingCache(cache); !ok {
			t.Errorf("cache must be written")
		}
	})
	t.Run("ordinary change and new model are applied and reported", func(t *testing.T) {
		cache, cfg := t.TempDir(), t.TempDir()
		page := pageWith(t, "| Claude Opus 5.5 | $5 / MTok | $6.25 / MTok | $10 / MTok | $0.25 / MTok | $25 / MTok |")
		_, o := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/markdown")
			w.Write(page)
		})
		res, err := RefreshPricingVetted(context.Background(), cache, cfg, o, now, false)
		if err != nil || !res.Applied || len(res.Changes) != 1 || res.Changes[0].Model != "claude-opus-5-5" {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		if res.Book.Models["claude-opus-5-5"].Input != 5 {
			t.Errorf("returned book must carry the new price")
		}
	})
	t.Run("suspicious change is refused, counted against the cooldown, and not cached", func(t *testing.T) {
		cache, cfg := t.TempDir(), t.TempDir()
		page := pageWith(t, "| Claude Opus 5.5 | $0.04 / MTok | $0.05 / MTok | $0.08 / MTok | $0.002 / MTok | $20 / MTok |")
		_, o := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/markdown")
			w.Write(page)
		})
		res, err := RefreshPricingVetted(context.Background(), cache, cfg, o, now, false)
		if !errors.Is(err, ErrSuspiciousPrices) || res.Applied || len(res.Suspicious) != 1 {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		if _, ok := LoadPricingCache(cache); ok {
			t.Errorf("a refused refresh must not write the cache")
		}
		st := LoadPricingState(cache)
		if st.Attempts != 1 || st.LastError == "" || st.ShouldAttempt(now.Add(time.Hour), 24*time.Hour) {
			t.Errorf("refusal must count as an attempt and start the cooldown: %+v", st)
		}
		// explicit acceptance applies it
		res, err = RefreshPricingVetted(context.Background(), cache, cfg, o, now.Add(time.Minute), true)
		if err != nil || !res.Applied || res.Book.Models["claude-opus-5-5"].Input != 0.04 {
			t.Fatalf("accepted refresh: res=%+v err=%v", res, err)
		}
	})
	t.Run("fetch failure is recorded", func(t *testing.T) {
		cache, cfg := t.TempDir(), t.TempDir()
		_, o := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 500) })
		if _, err := RefreshPricingVetted(context.Background(), cache, cfg, o, now, false); err == nil {
			t.Fatal("expected error")
		}
		if st := LoadPricingState(cache); st.Attempts != 1 || st.LastError == "" {
			t.Errorf("state = %+v", st)
		}
	})
}

func TestClearPricingCache(t *testing.T) {
	dir := t.TempDir()
	if removed, err := ClearPricingCache(dir); err != nil || len(removed) != 0 {
		t.Fatalf("empty dir: %v %v", removed, err)
	}
	if err := SavePricingCache(dir, DefaultBook()); err != nil {
		t.Fatal(err)
	}
	if err := SavePricingState(dir, PricingState{Attempts: 2}); err != nil {
		t.Fatal(err)
	}
	removed, err := ClearPricingCache(dir)
	if err != nil || len(removed) != 2 {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	if _, ok := LoadPricingCache(dir); ok {
		t.Errorf("cache still present")
	}
	if st := LoadPricingState(dir); st.Attempts != 0 {
		t.Errorf("state still present: %+v", st)
	}
	if removed, _ := ClearPricingCache(""); len(removed) != 0 {
		t.Errorf("an empty directory name must be a no-op, never the current directory")
	}
}
