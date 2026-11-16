package data

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var infoNow = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func writeCacheFile(t *testing.T, dir string, b *PriceBook) {
	t.Helper()
	if err := SavePricingCache(dir, b); err != nil {
		t.Fatal(err)
	}
}

func today() string { return time.Now().UTC().Format("2006-01-02") }

func TestExplain_DefaultsAreBuiltInOnly(t *testing.T) {
	cache, cfg := t.TempDir(), t.TempDir()
	r := ExplainPricing(cache, cfg, PricingPolicy{}, infoNow)
	if len(r.Models) != len(EmbeddedBook().Models) || len(r.Problems) != 0 {
		t.Fatalf("models=%d problems=%v", len(r.Models), r.Problems)
	}
	for _, m := range r.Models {
		if m.Origin != OriginBuiltIn || m.Note != "" {
			t.Fatalf("%s: origin=%s note=%q, want built-in", m.ID, m.Origin, m.Note)
		}
	}
	byLayer := map[string]LayerFile{}
	for _, f := range r.Files {
		byLayer[f.Layer] = f
	}
	if !byLayer[OriginBuiltIn].InUse || byLayer[OriginCache].Exists || byLayer[OriginOverride].Exists {
		t.Errorf("files = %+v", r.Files)
	}
	if !strings.HasSuffix(byLayer[OriginCache].Path, "pricing-cache.v1.json") || !strings.HasSuffix(byLayer[OriginOverride].Path, "pricing-overrides.json") {
		t.Errorf("paths wrong: %+v", r.Files)
	}
}

func TestExplain_CacheAndOverrideProvenance(t *testing.T) {
	cache, cfg := t.TempDir(), t.TempDir()
	c := *EmbeddedBook()
	c.AsOf = today()
	c.Models = map[string]ModelPrice{}
	for k, v := range EmbeddedBook().Models {
		c.Models[k] = v
	}
	opus := c.Models["claude-opus-5-5"]
	opus.Input, opus.CacheWrite5m, opus.CacheWrite1h, opus.CacheRead, opus.Output = 5, 6.25, 10, 0.25, 25
	c.Models["claude-opus-5-5"] = opus
	c.Models["claude-opus-5-6"] = ModelPrice{Rate: Rate{Input: 5, CacheWrite5m: 6.25, CacheWrite1h: 10, CacheRead: 0.5, Output: 25}}
	writeCacheFile(t, cache, &c)
	writeOverride(t, cfg, `{"version":1,"models":{"claude-sonnet-5-5":{"input":1.5,"cacheWrite5m":1.875,"cacheWrite1h":3,"cacheRead":0.075,"output":7.5}},"modifiers":{"fastMultiplier":3}}`)

	r := ExplainPricing(cache, cfg, PricingPolicy{}, infoNow)
	got := map[string]ModelSource{}
	for _, m := range r.Models {
		got[m.ID] = m
	}
	if m := got["claude-opus-5-5"]; m.Origin != OriginCache || !strings.Contains(m.Note, "$4") || !strings.Contains(m.Note, "$5") {
		t.Errorf("changed by the cache: %+v", m)
	}
	if m := got["claude-opus-5-6"]; m.Origin != OriginCache || !strings.Contains(m.Note, "new") {
		t.Errorf("new in the cache: %+v", m)
	}
	if m := got["claude-sonnet-5-5"]; m.Origin != OriginOverride || !strings.Contains(m.Note, "$1.5") {
		t.Errorf("overridden: %+v", m)
	}
	if m := got["claude-haiku-5-5"]; m.Origin != OriginBuiltIn {
		t.Errorf("a model the cache re-lists unchanged stays built-in: %+v", m)
	}
	if r.ModifierOrigins["fastMultiplier"] != OriginOverride || r.ModifierOrigins["usGeoMultiplier"] != OriginBuiltIn {
		t.Errorf("modifier origins: %+v", r.ModifierOrigins)
	}
	if r.Book.Modifiers.FastMultiplier != 3 {
		t.Errorf("book should carry the overridden modifier")
	}
}

func TestExplain_ProblemsAreSpecificAndActionable(t *testing.T) {
	cache, cfg := t.TempDir(), t.TempDir()
	// an override with a JSON syntax error on line 3
	os.WriteFile(filepath.Join(cfg, "pricing-overrides.json"), []byte("{\n  \"version\": 1,\n  \"models\": oops\n}\n"), 0o600)
	r := ExplainPricing(cache, cfg, PricingPolicy{}, infoNow)
	if len(r.Problems) != 1 || r.Problems[0].Layer != OriginOverride || !strings.Contains(r.Problems[0].Message, "line 3") || r.Problems[0].Fix == "" {
		t.Fatalf("syntax error problem: %+v", r.Problems)
	}
	// valid JSON but several bad entries: every one is listed
	writeOverride(t, cfg, `{"version":1,"models":{
	  "not-a-model":{"input":1,"cacheWrite5m":1,"cacheWrite1h":1,"cacheRead":1,"output":1},
	  "claude-opus-5-5":{"input":4,"cacheWrite5m":5,"cacheWrite1h":2,"cacheRead":0.2,"output":20},
	  "claude-sonnet-5-5":{"input":2,"cacheWrite5m":2.5,"cacheWrite1h":4,"cacheRead":0.1,"output":10}}}`)
	r = ExplainPricing(cache, cfg, PricingPolicy{}, infoNow)
	var msgs []string
	for _, p := range r.Problems {
		msgs = append(msgs, p.Message)
	}
	joined := strings.Join(msgs, "\n")
	if !strings.Contains(joined, "not-a-model") || !strings.Contains(joined, "claude-opus-5-5") || strings.Contains(joined, "claude-sonnet-5-5") {
		t.Errorf("each bad entry (and only those) should be listed:\n%s", joined)
	}
	for _, f := range r.Files {
		if f.Layer == OriginOverride && (f.InUse || !strings.Contains(f.Status, "ignored")) {
			t.Errorf("an invalid override file is ignored as a whole: %+v", f)
		}
	}
	for _, m := range r.Models {
		if m.Origin == OriginOverride {
			t.Errorf("nothing may be attributed to an ignored file: %+v", m)
		}
	}
}

func TestExplain_OldAndFutureCachesAreExplained(t *testing.T) {
	cfg := t.TempDir()
	for name, asOf := range map[string]string{"older than built-in": "2020-01-01", "dated in the future": "2999-01-01"} {
		cache := t.TempDir()
		c := *EmbeddedBook()
		c.AsOf = asOf
		writeCacheFile(t, cache, &c)
		r := ExplainPricing(cache, cfg, PricingPolicy{}, infoNow)
		if len(r.Problems) != 1 || r.Problems[0].Layer != OriginCache || r.Problems[0].Fix == "" {
			t.Errorf("%s: %+v", name, r.Problems)
		}
	}
}

func TestExplain_RefreshPolicyAndState(t *testing.T) {
	cache, cfg := t.TempDir(), t.TempDir()
	r := ExplainPricing(cache, cfg, PricingPolicy{Auto: false, URL: DefaultPricingURL}, infoNow)
	if r.Policy.Auto || !r.NextAutoAttempt.IsZero() && r.NextAutoAttempt.After(infoNow) {
		t.Errorf("never attempted: an attempt is allowed now: %+v", r)
	}
	if err := SavePricingState(cache, PricingState{LastAttempt: infoNow.Add(-2 * time.Hour), LastError: "pricing fetch: boom", Attempts: 3}); err != nil {
		t.Fatal(err)
	}
	r = ExplainPricing(cache, cfg, PricingPolicy{Auto: true, URL: DefaultPricingURL}, infoNow)
	want := infoNow.Add(-2 * time.Hour).Add(PricingCooldown)
	if !r.NextAutoAttempt.Equal(want) || r.State.Attempts != 3 || r.State.LastError == "" {
		t.Errorf("next=%v want %v state=%+v", r.NextAutoAttempt, want, r.State)
	}
}

func TestScanModelUsage(t *testing.T) {
	p1 := writeSession(t) // sonnet-5-5 + opus-5-5 messages, plus a <synthetic> placeholder
	uses := ScanModelUsage([]string{p1, "/definitely/missing.jsonl"}, DefaultBook())
	got := map[string]ModelUse{}
	for _, u := range uses {
		got[u.Model] = u
	}
	if len(got) != 2 || got["claude-sonnet-5-5"].Messages != 1 || got["claude-opus-5-5"].Messages != 1 {
		t.Fatalf("uses = %+v", uses)
	}
	if u := got["claude-opus-5-5"]; u.Match != PriceExact || u.ResolvedID != "claude-opus-5-5" || u.CostUSD <= 0 {
		t.Errorf("opus: %+v", u)
	}
	if uses[0].CostUSD < uses[len(uses)-1].CostUSD {
		t.Errorf("must be sorted by cost, most expensive first")
	}
	// models with no exact price are explained
	p2 := writeLines(t, asstLine("a", 10, 5), strings.Replace(asstLine("b", 10, 5), "claude-sonnet-5-5", "claude-sonnet-9-9", 1), strings.Replace(asstLine("c", 10, 5), "claude-sonnet-5-5", "mystery-1", 1))
	got = map[string]ModelUse{}
	for _, u := range ScanModelUsage([]string{p2}, DefaultBook()) {
		got[u.Model] = u
	}
	if u := got["claude-sonnet-9-9"]; u.Match != PriceFamily || u.ResolvedID != "claude-sonnet-5-5" {
		t.Errorf("family estimate: %+v", u)
	}
	if u := got["mystery-1"]; u.Match != PriceNone || u.ResolvedID != "" || u.CostUSD != 0 {
		t.Errorf("unpriced: %+v", u)
	}
}

func TestReportText(t *testing.T) {
	cache, cfg := t.TempDir(), t.TempDir()
	writeOverride(t, cfg, `{"version":1,"models":{"claude-sonnet-5-5":{"input":1.5,"cacheWrite5m":1.875,"cacheWrite1h":3,"cacheRead":0.075,"output":7.5}}}`)
	r := ExplainPricing(cache, cfg, PricingPolicy{Auto: true, URL: DefaultPricingURL, AllowHosts: []string{"platform.claude.com"}}, infoNow)
	r.Usage = ScanModelUsage([]string{writeLines(t, asstLine("a", 10, 5), strings.Replace(asstLine("b", 10, 5), "claude-sonnet-5-5", "mystery-1", 1))}, r.Book)
	txt := r.Text()
	for _, want := range []string{
		"Price layers", "built-in table", "fetched cache", "your overrides", "pricing-overrides.json",
		"Model prices", "claude-opus-5-5", "claude-haiku-5-5", "over 100,000", "override",
		"How prices are applied", "fast mode", "US-only", "web search",
		"Refresh policy", "automatic refresh: ON", "platform.claude.com",
		"Models in your sessions", "mystery-1", "no price known",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("report missing %q", want)
		}
	}
	for _, r := range txt {
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f {
			t.Fatalf("report contains a control character %q", r)
		}
	}
}
