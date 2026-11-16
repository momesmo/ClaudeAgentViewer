package data

import (
	"context"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/pricing_page_excerpt.md")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParsePricingMarkdown_MatchesEmbedded(t *testing.T) {
	b, err := ParsePricingMarkdown(fixture(t), "src", "2026-11-16")
	if err != nil {
		t.Fatal(err)
	}
	if b.Version != 1 || b.Source != "src" || b.AsOf != "2026-11-16" {
		t.Fatalf("meta: %+v", b)
	}
	if b.Modifiers != DefaultBook().Modifiers {
		t.Fatalf("modifiers %+v", b.Modifiers)
	}
	for id, want := range DefaultBook().Models {
		got, ok := b.Models[id]
		if !ok {
			t.Errorf("missing %s", id)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v want %+v", id, got, want)
		}
	}
	if len(b.Models) != len(DefaultBook().Models) {
		t.Errorf("model count %d vs %d", len(b.Models), len(DefaultBook().Models))
	}
}

func TestParsePricingMarkdown_Padding(t *testing.T) {
	md := "intro\n|Model|Base input tokens|5m cache writes|1h cache writes|Cache hits|Output|\n|---|---|---|---|---|---|\n" +
		"|   Claude   Opus 5.5 ([x](http://a/b_(c)))   |$4/MTok<sup>1</sup>|  $5 / MTok|$8 / MTok|$0.20 / MTok |$20 / MTok|\n" +
		"| Claude Instant 1 | $1 / MTok | $1 / MTok | $1 / MTok | $1 / MTok | $1 / MTok |\n\nafter\n"
	b, err := ParsePricingMarkdown([]byte(md), "s", "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Models) != 1 || b.Models["claude-opus-5-5"].Input != 4 || b.Models["claude-opus-5-5"].CacheRead != 0.2 {
		t.Fatalf("%+v", b.Models)
	}
}

func TestParsePricingMarkdown_Errors(t *testing.T) {
	hdr := "| Model | Base input tokens | 5m cache writes | 1h cache writes | Cache hits and refreshes | Output tokens |\n| - | - | - | - | - | - |\n"
	row := "| Claude Opus 5.5 | $4 / MTok | $5 / MTok | $8 / MTok | $0.2 / MTok | $20 / MTok |\n"
	cases := map[string]string{
		"bad price":      hdr + strings.Replace(row, "$5 / MTok", "five dollars", 1),
		"missing table":  "# nothing here\n",
		"duplicate":      hdr + row + row,
		"html":           "<!doctype html><html><body><table><tr><td>Model</td></tr></table></body></html>",
		"empty":          "",
		"short row":      hdr + "| Claude Opus 5.5 | $4 / MTok |\n",
		"lc without std": hdr + "| Claude Haiku 5.5 (for prompts over 100,000 tokens) | $0.5 / MTok | $0.625 / MTok | $1 / MTok | $0.05 / MTok | $2.5 / MTok |\n",
		"invalid rates":  hdr + strings.Replace(row, "$8 / MTok", "$1 / MTok", 1),
	}
	for name, md := range cases {
		if b, err := ParsePricingMarkdown([]byte(md), "s", "2026-01-01"); err == nil {
			t.Errorf("%s: expected error, got %+v", name, b)
		}
	}
	_, err := ParsePricingMarkdown([]byte(hdr+strings.Replace(row, "$5 / MTok", "five", 1)), "s", "2026-01-01")
	if err == nil || !strings.Contains(err.Error(), "Opus 5.5") {
		t.Errorf("error should name the row: %v", err)
	}
}

func TestParsePricingMarkdown_TruncationNeverPanics(t *testing.T) {
	fx := fixture(t)
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 500; i++ {
		n := rng.Intn(len(fx) + 1)
		cut := fx[:n]
		if i%3 == 0 { // also chop the front / splice garbage
			cut = append(append([]byte{}, fx[rng.Intn(n+1):n]...), byte(rng.Intn(256)))
		}
		_, _ = ParsePricingMarkdown(cut, "s", "2026-01-01")
	}
}

// --- fetch -----------------------------------------------------------------

func tlsServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, FetchOptions) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return srv, FetchOptions{URL: srv.URL + "/p.md", Client: srv.Client(), AllowHosts: []string{u.Host}}
}

func serveMD(t *testing.T) http.HandlerFunc {
	fx := fixture(t)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Write(fx)
	}
}

func TestFetchPricing_Success_RequestIsMinimal(t *testing.T) {
	var hdr http.Header
	_, o := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		hdr = r.Header.Clone()
		serveMD(t)(w, r)
	})
	o.Client.Jar = nil
	b, err := FetchPricing(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if b.Source != o.URL || b.AsOf != time.Now().UTC().Format("2006-01-02") {
		t.Errorf("meta %q %q", b.Source, b.AsOf)
	}
	if hdr.Get("User-Agent") != "claudevu-pricing/1" {
		t.Errorf("UA %q", hdr.Get("User-Agent"))
	}
	for _, k := range []string{"Cookie", "Authorization", "Referer", "X-Api-Key"} {
		if hdr.Get(k) != "" {
			t.Errorf("unexpected header %s", k)
		}
	}
}

func TestFetchPricing_RejectsSchemesAndHosts(t *testing.T) {
	ctx := context.Background()
	if _, err := FetchPricing(ctx, FetchOptions{URL: "http://platform.claude.com/x.md"}); err == nil {
		t.Error("http accepted")
	}
	if _, err := FetchPricing(ctx, FetchOptions{URL: "file:///etc/passwd"}); err == nil {
		t.Error("file accepted")
	}
	if _, err := FetchPricing(ctx, FetchOptions{URL: "https://evil.example/x.md"}); err == nil {
		t.Error("host outside default allowlist accepted")
	}
	srv, o := tlsServer(t, serveMD(t))
	_ = srv
	o.AllowHosts = []string{"platform.claude.com"}
	if _, err := FetchPricing(ctx, o); err == nil {
		t.Error("custom URL host not in AllowHosts accepted")
	}
}

func TestFetchPricing_Redirects(t *testing.T) {
	var n int32
	var srv *httptest.Server
	srv, o := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/p.md":
			http.Redirect(w, r, "/ok.md", http.StatusFound)
		case "/ok.md":
			serveMD(t)(w, r)
		case "/other":
			http.Redirect(w, r, "http://"+r.Host+"/ok.md", http.StatusFound)
		case "/loop":
			atomic.AddInt32(&n, 1)
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/cross":
			http.Redirect(w, r, "https://localhost:"+strings.Split(srv.URL, ":")[2]+"/ok.md", http.StatusFound)
		}
	})
	if _, err := FetchPricing(context.Background(), o); err != nil {
		t.Fatalf("same-host redirect: %v", err)
	}
	for _, p := range []string{"/other", "/loop", "/cross"} {
		oo := o
		oo.URL = srv.URL + p
		oo.AllowHosts = []string{"127.0.0.1", "localhost"}
		if _, err := FetchPricing(context.Background(), oo); err == nil {
			t.Errorf("%s: expected error", p)
		}
	}
	if n > 4 {
		t.Errorf("followed %d redirects", n)
	}
}

func TestFetchPricing_ServerMisbehaviour(t *testing.T) {
	fx := fixture(t)
	cases := map[string]http.HandlerFunc{
		"500": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", 500) },
		"404": func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) },
		"octet": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(fx)
		},
		"image": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Write(fx)
		},
		"oversize": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.Write(fx)
			w.(http.Flusher).Flush()
			w.Write([]byte(strings.Repeat("x", 4000)))
		},
		"bad layout": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/markdown")
			var keep []string
			for i, l := range strings.Split(string(fx), "\n") {
				if i < 12 || !strings.HasPrefix(l, "|") {
					keep = append(keep, l)
				}
			}
			w.Write([]byte(strings.Join(keep, "\n")))
		},
		"html": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html><body>moved</body></html>"))
		},
	}
	for name, h := range cases {
		_, o := tlsServer(t, h)
		if name == "oversize" {
			o.MaxBytes = int64(len(fx)) + 100
		}
		if b, err := FetchPricing(context.Background(), o); err == nil {
			t.Errorf("%s: expected error, got book with %d models", name, len(b.Models))
		}
	}
}

func TestFetchPricing_Timeout(t *testing.T) {
	_, o := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(500 * time.Millisecond) })
	o.Timeout = 50 * time.Millisecond
	if _, err := FetchPricing(context.Background(), o); err == nil {
		t.Error("expected timeout")
	}
}

func TestFetchPricing_CustomUserAgent(t *testing.T) {
	var ua string
	_, o := tlsServer(t, func(w http.ResponseWriter, r *http.Request) { ua = r.UserAgent(); serveMD(t)(w, r) })
	o.UserAgent = "custom/9"
	if _, err := FetchPricing(context.Background(), o); err != nil || ua != "custom/9" {
		t.Fatalf("%v %q", err, ua)
	}
}

// --- storage -----------------------------------------------------------------

func TestCacheRoundTripAndPerms(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	if _, ok := LoadPricingCache(dir); ok {
		t.Fatal("empty dir should miss")
	}
	b := DefaultBook()
	if err := SavePricingCache(dir, b); err != nil {
		t.Fatal(err)
	}
	got, ok := LoadPricingCache(dir)
	if !ok || !reflect.DeepEqual(got, b) {
		t.Fatalf("round trip: %v", ok)
	}
	fi, _ := os.Stat(filepath.Join(dir, "pricing-cache.v1.json"))
	di, _ := os.Stat(dir)
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Errorf("perms %v %v", fi.Mode().Perm(), di.Mode().Perm())
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Errorf("leftover files: %v", ents)
	}
	os.WriteFile(filepath.Join(dir, "pricing-cache.v1.json"), []byte("{corrupt"), 0o600)
	if _, ok := LoadPricingCache(dir); ok {
		t.Error("corrupt cache accepted")
	}
	if err := SavePricingCache(dir, &PriceBook{Version: 1}); err == nil {
		t.Error("invalid book saved")
	}
}

func TestPricingState(t *testing.T) {
	dir := t.TempDir()
	if s := LoadPricingState(dir); !s.ShouldAttempt(time.Now(), time.Hour) {
		t.Error("never attempted should attempt")
	}
	now := time.Now()
	s := PricingState{LastAttempt: now, LastError: "x", Attempts: 2}
	if err := SavePricingState(dir, s); err != nil {
		t.Fatal(err)
	}
	g := LoadPricingState(dir)
	if g.Attempts != 2 || g.LastError != "x" || !g.LastAttempt.Equal(now) {
		t.Fatalf("%+v", g)
	}
	if g.ShouldAttempt(now.Add(time.Minute), time.Hour) || !g.ShouldAttempt(now.Add(2*time.Hour), time.Hour) {
		t.Error("cooldown wrong")
	}
	os.WriteFile(filepath.Join(dir, "pricing-state.json"), []byte("zzz"), 0o600)
	if g := LoadPricingState(dir); g.Attempts != 0 {
		t.Error("corrupt state should be zero")
	}
}

func writeOverride(t *testing.T, dir, s string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "pricing-overrides.json"), []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPricingOverrides(t *testing.T) {
	dir := t.TempDir()
	if b, why := LoadPricingOverrides(dir); b != nil || why != "" {
		t.Fatal("missing file should be silent")
	}
	writeOverride(t, dir, `{"version":1,"modifiers":{"fastMultiplier":3},"models":{"claude-opus-5-5":{"input":1,"cacheWrite5m":1.25,"cacheWrite1h":2,"cacheRead":0.1,"output":5}}}`)
	b, why := LoadPricingOverrides(dir)
	if b == nil || why != "" || b.Models["claude-opus-5-5"].Input != 1 || b.Modifiers.FastMultiplier != 3 {
		t.Fatalf("%+v %q", b, why)
	}
	bad := []string{
		`{"models":{}}`,
		`{"version":2}`,
		`not json`,
		`{"version":1,"models":{"gpt-4":{"input":1,"cacheWrite5m":1,"cacheWrite1h":1,"cacheRead":1,"output":1}}}`,
		`{"version":1,"models":{"claude-opus-5-5":{"input":-1,"cacheWrite5m":1,"cacheWrite1h":1,"cacheRead":1,"output":1}}}`,
		`{"version":1,"modifiers":{"fastMultiplier":99}}`,
	}
	for _, s := range bad {
		writeOverride(t, dir, s)
		if b, why := LoadPricingOverrides(dir); b != nil || why == "" {
			t.Errorf("%s: accepted", s)
		}
	}
}

func TestLayeredBook(t *testing.T) {
	cache, cfg := t.TempDir(), t.TempDir()
	emb, _ := ParsePriceBook(embeddedPricing)
	// Even if the default book was swapped at runtime, layering starts from the embedded table.
	orig := DefaultBook()
	t.Cleanup(func() { SetDefaultBook(orig) })
	SetDefaultBook(&PriceBook{Version: 1, Modifiers: orig.Modifiers, Models: map[string]ModelPrice{"claude-opus-9": orig.Models["claude-opus-5"]}})

	b, info := LayeredBook(cache, cfg)
	if len(b.Models) != len(emb.Models) || info.CacheUsed || info.EmbeddedAsOf != emb.AsOf {
		t.Fatalf("base: %d %+v", len(b.Models), info)
	}
	SetDefaultBook(orig)

	// Newer cache wins over embedded.
	c := *emb
	c.AsOf = time.Now().UTC().Format("2006-01-02")
	c.Models = map[string]ModelPrice{}
	for k, v := range emb.Models {
		c.Models[k] = v
	}
	mp := c.Models["claude-opus-5"]
	mp.Input, mp.CacheWrite5m, mp.CacheWrite1h, mp.CacheRead, mp.Output = 6, 7, 9, 0.6, 30
	c.Models["claude-opus-5"] = mp
	if err := SavePricingCache(cache, &c); err != nil {
		t.Fatal(err)
	}
	b, info = LayeredBook(cache, cfg)
	if !info.CacheUsed || b.Models["claude-opus-5"].Input != 6 || info.CacheAsOf != c.AsOf {
		t.Fatalf("cache: %+v", info)
	}
	// Override wins over cache.
	writeOverride(t, cfg, `{"version":1,"models":{"claude-opus-5":{"input":2,"cacheWrite5m":2.5,"cacheWrite1h":4,"cacheRead":0.2,"output":9}}}`)
	b, info = LayeredBook(cache, cfg)
	if b.Models["claude-opus-5"].Input != 2 || info.OverrideModels != 1 {
		t.Fatalf("override: %+v", info)
	}
	// Older cache must not override embedded.
	c.AsOf = "2000-01-01"
	SavePricingCache(cache, &c)
	os.Remove(filepath.Join(cfg, "pricing-overrides.json"))
	b, info = LayeredBook(cache, cfg)
	if info.CacheUsed || b.Models["claude-opus-5"].Input != emb.Models["claude-opus-5"].Input || len(info.Problems) != 1 {
		t.Fatalf("stale: %+v", info)
	}
	// Corrupt layers are reported, never fatal.
	os.WriteFile(filepath.Join(cache, "pricing-cache.v1.json"), []byte("{"), 0o600)
	writeOverride(t, cfg, "{")
	b, info = LayeredBook(cache, cfg)
	if b == nil || len(info.Problems) != 2 {
		t.Fatalf("corrupt: %+v", info)
	}
}

func TestRefreshPricing(t *testing.T) {
	cache, cfg := t.TempDir(), t.TempDir()
	fx := fixture(t)
	mode := "ok"
	_, o := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "ok":
			serveMD(t)(w, r)
		case "500":
			http.Error(w, "x", 500)
		case "big":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(strings.Repeat("y", len(fx)+500)))
		case "layout":
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte("# Pricing\nNo tables today.\n"))
		}
	})
	o.MaxBytes = int64(len(fx)) + 100
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	writeOverride(t, cfg, `{"version":1,"models":{"claude-opus-5":{"input":2,"cacheWrite5m":2.5,"cacheWrite1h":4,"cacheRead":0.2,"output":9}}}`)

	for i, m := range []string{"500", "big", "layout"} {
		mode = m
		b, st, err := RefreshPricing(context.Background(), cache, cfg, o, now)
		if err == nil || b != nil {
			t.Fatalf("%s: expected failure", m)
		}
		if st.Attempts != i+1 || st.LastError == "" || !st.LastAttempt.Equal(now) || !st.LastSuccess.IsZero() {
			t.Fatalf("%s: state %+v", m, st)
		}
		if _, ok := LoadPricingCache(cache); ok {
			t.Fatalf("%s: cache written on failure", m)
		}
		if got := LoadPricingState(cache); got.Attempts != i+1 {
			t.Fatalf("%s: state not persisted", m)
		}
	}
	mode = "ok"
	b, st, err := RefreshPricing(context.Background(), cache, cfg, o, now)
	if err != nil {
		t.Fatal(err)
	}
	if st.Attempts != 0 || st.LastError != "" || !st.LastSuccess.Equal(now) {
		t.Fatalf("state %+v", st)
	}
	if _, ok := LoadPricingCache(cache); !ok {
		t.Fatal("cache not written")
	}
	if b.Models["claude-opus-5"].Input != 2 {
		t.Error("override must win after refresh")
	}
	if b.Models["claude-opus-5-5"].Input != 4 {
		t.Error("fetched rates missing")
	}
	if g := LoadPricingState(cache); !g.LastSuccess.Equal(now) {
		t.Error("state not persisted")
	}
}
