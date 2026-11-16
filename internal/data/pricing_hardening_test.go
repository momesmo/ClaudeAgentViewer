package data

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- I1: the embedded table is the baseline, never the runtime book ----------

func TestFetchedBooksNeverInheritRuntimeOverrides(t *testing.T) {
	orig := DefaultBook()
	t.Cleanup(func() { SetDefaultBook(orig) })
	tweaked := *orig
	tweaked.Modifiers.FastMultiplier = 7 // as if a user override had been installed
	tweaked.Models = map[string]ModelPrice{"claude-opus-9": orig.Models["claude-opus-5"]}
	SetDefaultBook(&tweaked)

	b, err := ParsePricingMarkdown(fixture(t), "s", "2026-11-16")
	if err != nil {
		t.Fatal(err)
	}
	if b.Modifiers.FastMultiplier != EmbeddedBook().Modifiers.FastMultiplier {
		t.Errorf("a parsed page must take modifiers from the embedded table, got %v", b.Modifiers.FastMultiplier)
	}
	// The plausibility guard compares with the embedded model list, so a runtime
	// book (with override-only models) can neither block nor satisfy it.
	if err := checkPlausible(b); err != nil {
		t.Errorf("plausibility must use the embedded table: %v", err)
	}
}

// --- I2: a future timestamp must not block refreshing forever ----------------

func TestShouldAttemptIgnoresFutureTimestamps(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		last time.Time
		want bool
	}{
		"never":                 {time.Time{}, true},
		"recent":                {now.Add(-time.Hour), false},
		"old":                   {now.Add(-25 * time.Hour), true},
		"slightly ahead (skew)": {now.Add(time.Minute), false},
		"an hour ahead":         {now.Add(time.Hour), true},
		"year 9999":             {time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), true},
	}
	for name, c := range cases {
		if got := (PricingState{LastAttempt: c.last}).ShouldAttempt(now, 24*time.Hour); got != c.want {
			t.Errorf("%s: ShouldAttempt = %v, want %v", name, got, c.want)
		}
	}
}

// --- I3: defensive local file reads -------------------------------------------

func TestReadSmallFile(t *testing.T) {
	dir := t.TempDir()
	ok := dir + "/ok.json"
	writeFile(t, ok, `{"a":1}`)
	if b, err := readSmallFile(ok); err != nil || string(b) != `{"a":1}` {
		t.Errorf("small regular file: %q %v", b, err)
	}
	big := dir + "/big.json"
	writeFile(t, big, strings.Repeat("x", maxLocalFile+1))
	if _, err := readSmallFile(big); err == nil {
		t.Errorf("a file over the cap must be rejected")
	}
	if _, err := readSmallFile(dir); err == nil {
		t.Errorf("a directory must be rejected")
	}
	if _, err := readSmallFile(dir + "/missing.json"); err == nil {
		t.Errorf("a missing file must report an error")
	}
}

// --- I4 / vetting ---------------------------------------------------------------

func TestCheckPlausible_NewModelsMustLookLikeRealPrices(t *testing.T) {
	good, err := ParsePricingMarkdown(fixture(t), "s", "2026-11-16")
	if err != nil {
		t.Fatal(err)
	}
	add := func(r Rate) *PriceBook {
		b := *good
		b.Models = map[string]ModelPrice{}
		for k, v := range good.Models {
			b.Models[k] = v
		}
		b.Models["claude-opus-9-9"] = ModelPrice{Rate: r}
		return &b
	}
	if err := checkPlausible(add(Rate{Input: 4, CacheWrite5m: 5, CacheWrite1h: 8, CacheRead: 0.2, Output: 20})); err != nil {
		t.Errorf("a normal new model must pass: %v", err)
	}
	for name, r := range map[string]Rate{
		"absurdly cheap":  {Input: 0.0001, CacheWrite5m: 0.000125, CacheWrite1h: 0.0002, CacheRead: 0.00001, Output: 0.0005},
		"odd cache ratio": {Input: 4, CacheWrite5m: 5, CacheWrite1h: 8, CacheRead: 2, Output: 20},
		"free-ish output": {Input: 4, CacheWrite5m: 5, CacheWrite1h: 8, CacheRead: 0.2, Output: 4.5},
		"absurdly dear":   {Input: 500, CacheWrite5m: 625, CacheWrite1h: 1000, CacheRead: 25, Output: 2500},
	} {
		if err := checkPlausible(add(r)); err == nil {
			t.Errorf("%s: should be rejected", name)
		}
	}
}

func TestRefreshVetted_PageWideChangeIsSuspiciousEvenIfEachStepIsSmall(t *testing.T) {
	cache, cfg := t.TempDir(), t.TempDir()
	// Halve every price: each is within 3x, but changing most of the table at once is not normal.
	page := string(fixture(t))
	var out []string
	for _, l := range strings.Split(page, "\n") {
		if strings.HasPrefix(l, "| Claude ") && strings.Contains(l, "/ MTok") && !strings.Contains(l, "USD") && !strings.Contains(l, "x ") {
			cells := strings.Split(l, "|")
			for i := 2; i <= 6 && i < len(cells); i++ {
				c := strings.TrimSpace(cells[i])
				if v, ok := parsePriceCell(c); ok {
					cells[i] = " $" + trimFloat(v/2) + " / MTok "
				}
			}
			l = strings.Join(cells, "|")
		}
		out = append(out, l)
	}
	_, o := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		w.Write([]byte(strings.Join(out, "\n")))
	})
	res, err := RefreshPricingVetted(context.Background(), cache, cfg, o, time.Now(), false)
	if err == nil || res.Applied {
		t.Fatalf("a page-wide change must be refused: res=%+v err=%v", res, err)
	}
	if _, ok := LoadPricingCache(cache); ok {
		t.Errorf("nothing may be cached when refused")
	}
}

func parsePriceCell(c string) (float64, bool) {
	m := priceCellRE.FindStringSubmatch(c)
	if m == nil {
		return 0, false
	}
	v, err := parseFloatNoComma(m[1])
	return v, err == nil
}

// --- M1 / M2: parsing strictness ------------------------------------------------

func TestParse_RejectsLocaleCommaPrices(t *testing.T) {
	hdr := "| Model | Base input tokens | 5m cache writes | 1h cache writes | Cache hits and refreshes | Output tokens |\n| - | - | - | - | - | - |\n"
	for _, bad := range []string{"$0,5 / MTok", "$1,,,5 / MTok", "$4,0 / MTok"} {
		row := "| Claude Opus 5.5 | " + bad + " | $5 / MTok | $8 / MTok | $0.2 / MTok | $20 / MTok |\n"
		if _, err := ParsePricingMarkdown([]byte(hdr+row), "s", "2026-01-01"); err == nil {
			t.Errorf("%q must be rejected, not read as a different number", bad)
		}
	}
	// Genuine thousands separators still parse (checked at the cell level: such
	// prices would exceed the sanity ceiling as whole models).
	for cell, want := range map[string]string{"$1,000 / MTok": "1,000", "$12,345.50 / MTok": "12,345.50", "$0.20 / MTok": "0.20", "$4 / MTok": "4"} {
		if m := priceCellRE.FindStringSubmatch(cell); m == nil || m[1] != want {
			t.Errorf("%q should parse as %q, got %v", cell, want, m)
		}
	}
}

func TestParse_ReadsColumnsByHeaderNotPosition(t *testing.T) {
	// Same data, columns reordered: output first, then input, writes, hits.
	md := "| Model | Output tokens | Base input tokens | 1h cache writes | 5m cache writes | Cache hits and refreshes |\n| - | - | - | - | - | - |\n" +
		"| Claude Opus 5.5 | $20 / MTok | $4 / MTok | $8 / MTok | $5 / MTok | $0.2 / MTok |\n" +
		strings.Repeat("", 0)
	b, err := ParsePricingMarkdown([]byte(md), "s", "2026-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if r := b.Models["claude-opus-5-5"].Rate; r != (Rate{Input: 4, CacheWrite5m: 5, CacheWrite1h: 8, CacheRead: 0.2, Output: 20}) {
		t.Errorf("columns mapped by position: %+v", r)
	}
	missing := "| Model | Base input tokens | 5m cache writes | Cache hits and refreshes | Output tokens | Notes |\n| - | - | - | - | - | - |\n" +
		"| Claude Opus 5.5 | $4 / MTok | $5 / MTok | $0.2 / MTok | $20 / MTok | x |\n"
	if _, err := ParsePricingMarkdown([]byte(missing), "s", "2026-01-01"); err == nil {
		t.Errorf("a header without the 1h column must be rejected")
	}
}

// --- M3: untrusted text ---------------------------------------------------------

func TestCleanTextAndClip(t *testing.T) {
	got := cleanText("ok\x1b[31mred\x07\r\nline", 300)
	if strings.ContainsAny(got, "\x1b\x07\r\n") {
		t.Errorf("control characters survived: %q", got)
	}
	if long := cleanText(strings.Repeat("a", 5000), 300); len([]rune(long)) > 301 {
		t.Errorf("not truncated: %d", len([]rune(long)))
	}
	if c := clip(strings.Repeat("é", 500)); len([]rune(c)) > 81 {
		t.Errorf("clip too long: %d", len([]rune(c)))
	}
}

func TestRefreshStoresSanitizedError(t *testing.T) {
	cache, cfg := t.TempDir(), t.TempDir()
	_, o := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("\x1b[2J" + strings.Repeat("junk ", 100)))
	})
	if _, err := RefreshPricingVetted(context.Background(), cache, cfg, o, time.Now(), false); err == nil {
		t.Fatal("expected a parse error")
	}
	st := LoadPricingState(cache)
	if strings.ContainsAny(st.LastError, "\x1b\r\n") || len([]rune(st.LastError)) > 301 {
		t.Errorf("stored error is not safe to display: %q", st.LastError)
	}
}

// --- M4: dates -----------------------------------------------------------------

func TestValidateAndLayeringRejectBadDates(t *testing.T) {
	b := *DefaultBook()
	b.AsOf = "zzz"
	if err := b.Validate(); err == nil {
		t.Errorf("a non-date asOf must be rejected")
	}
	cache, cfg := t.TempDir(), t.TempDir()
	for name, asOf := range map[string]string{"far future": "2999-01-01", "empty": ""} {
		c := *EmbeddedBook()
		c.AsOf = asOf
		if err := SavePricingCache(cache, &c); err != nil {
			t.Fatal(err)
		}
		_, info := LayeredBook(cache, cfg)
		if info.CacheUsed || len(info.Problems) == 0 {
			t.Errorf("%s: a cache dated %q must be ignored with a problem noted: %+v", name, asOf, info)
		}
	}
}

// --- M5 / M6 / M7: fetch policy --------------------------------------------------

func TestFetchPricing_HostPortAndAllowlistPolicy(t *testing.T) {
	ctx := context.Background()
	reject := map[string]FetchOptions{
		"non-ascii host":         {URL: "https://docſ.anthropic.com/x", AllowHosts: []string{"docs.anthropic.com"}},
		"port without allowlist": {URL: "https://platform.claude.com:8443/x"},
		"marketing host default": {URL: "https://docs.anthropic.com/x"},
		"claude.com default":     {URL: "https://claude.com/x"},
		"other host":             {URL: "https://example.org/x"},
	}
	for name, o := range reject {
		if _, err := FetchPricing(ctx, o); err == nil || !strings.Contains(err.Error(), "pricing fetch") {
			t.Errorf("%s: expected an immediate policy error, got %v", name, err)
		}
	}
}

func TestFetchPricing_RejectsHTMLContentType(t *testing.T) {
	_, o := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(fixture(t))
	})
	if _, err := FetchPricing(context.Background(), o); err == nil {
		t.Errorf("text/html must be rejected")
	}
	for _, ct := range []string{"text/markdown; charset=utf-8", "text/plain", ""} {
		ct := ct
		_, o := tlsServer(t, func(w http.ResponseWriter, r *http.Request) {
			if ct != "" {
				w.Header().Set("Content-Type", ct)
			} else {
				w.Header()["Content-Type"] = nil
			}
			w.Write(fixture(t))
		})
		if _, err := FetchPricing(context.Background(), o); err != nil {
			t.Errorf("content type %q should be accepted: %v", ct, err)
		}
	}
}

// --- M8: no network when the result cannot be stored ---------------------------

func TestRefreshVetted_NoNetworkWithoutWritableCacheDir(t *testing.T) {
	hits := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	t.Cleanup(srv.Close)
	o := FetchOptions{URL: srv.URL + "/p.md", Client: srv.Client(), AllowHosts: []string{strings.TrimPrefix(srv.URL, "https://")}}
	if _, err := RefreshPricingVetted(context.Background(), "", t.TempDir(), o, time.Now(), false); err == nil {
		t.Errorf("an empty cache dir must be an error")
	}
	blocker := t.TempDir() + "/file"
	writeFile(t, blocker, "x") // a FILE where the directory should be
	if _, err := RefreshPricingVetted(context.Background(), blocker+"/sub", t.TempDir(), o, time.Now(), false); err == nil {
		t.Errorf("an unusable cache dir must be an error")
	}
	if hits != 0 {
		t.Errorf("the network was used %d times although nothing could be stored", hits)
	}
}

// --- M11: price book edge cases -------------------------------------------------

func TestMergeEdgeCases(t *testing.T) {
	base := DefaultBook()
	if m := base.Merge(nil); len(m.Models) != len(base.Models) {
		t.Errorf("Merge(nil) must return a copy of the base")
	}
	// Overriding Haiku 5.5 without a long-context tier must not drop the tier.
	over := &PriceBook{Version: 1, Models: map[string]ModelPrice{
		"claude-haiku-5-5": {Rate: Rate{Input: 0.2, CacheWrite5m: 0.25, CacheWrite1h: 0.4, CacheRead: 0.02, Output: 1}},
	}}
	m := base.Merge(over)
	h := m.Models["claude-haiku-5-5"]
	if h.Input != 0.2 || h.LongContext == nil || h.LongContext.OverTokens != 100000 {
		t.Errorf("override lost the long-context tier: %+v", h)
	}
}

func TestParsePriceBook_RejectsTrailingData(t *testing.T) {
	good := string(embeddedPricing)
	if _, err := ParsePriceBook([]byte(good + " {\"extra\":1}")); err == nil {
		t.Errorf("trailing JSON must be rejected")
	}
	if _, err := ParsePriceBook([]byte(good + "\n\n")); err != nil {
		t.Errorf("trailing whitespace is fine: %v", err)
	}
}
