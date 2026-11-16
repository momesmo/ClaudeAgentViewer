package data

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestEmbeddedPriceBookLoadsAndHasKnownRates(t *testing.T) {
	b := DefaultBook()
	if len(b.Models) < 20 {
		t.Fatalf("embedded table has only %d models", len(b.Models))
	}
	cases := map[string]Rate{
		"claude-sonnet-5-5": {Input: 2, CacheWrite5m: 2.5, CacheWrite1h: 4, CacheRead: 0.10, Output: 10},
		"claude-opus-5-5":   {Input: 4, CacheWrite5m: 5, CacheWrite1h: 8, CacheRead: 0.20, Output: 20},
		"claude-opus-4-8":   {Input: 5, CacheWrite5m: 6.25, CacheWrite1h: 10, CacheRead: 0.50, Output: 25},
		"claude-fable-5-2":  {Input: 8, CacheWrite5m: 10, CacheWrite1h: 16, CacheRead: 0.20, Output: 40},
		"claude-opus-4-1":   {Input: 15, CacheWrite5m: 18.75, CacheWrite1h: 30, CacheRead: 1.5, Output: 75},
	}
	for id, want := range cases {
		got, ok := b.Models[id]
		if !ok || got.Rate != want {
			t.Errorf("%s = %+v (present=%v), want %+v", id, got.Rate, ok, want)
		}
	}
	h := b.Models["claude-haiku-5-5"]
	if h.LongContext == nil || h.LongContext.OverTokens != 100000 || h.LongContext.Input != 0.5 || h.Input != 0.1 {
		t.Errorf("haiku 5.5 tiers wrong: %+v", h)
	}
	if b.Source == "" || b.AsOf == "" {
		t.Errorf("embedded table must record its source and date: %q %q", b.Source, b.AsOf)
	}
}

func TestLookup_ExactAndNormalized(t *testing.T) {
	b := DefaultBook()
	exact := map[string]string{"claude-opus-5-5": "claude-opus-5-5"}
	for in, want := range exact {
		if _, id, k := b.Lookup(in); id != want || k != PriceExact {
			t.Errorf("Lookup(%q) = %s,%v want %s,exact", in, id, k, want)
		}
	}
	normalized := map[string]string{
		"anthropic.claude-opus-5-5":              "claude-opus-5-5",
		"us.anthropic.claude-sonnet-5-5-v1:0":    "claude-sonnet-5-5",
		"claude-opus-4-5@20251101":               "claude-opus-4-5",
		"claude-sonnet-4-5-20250929":             "claude-sonnet-4-5",
		"claude-opus-4-8[1m]":                    "claude-opus-4-8",
		"CLAUDE-OPUS-5-5":                        "claude-opus-5-5",
		"claude-opus-5-5-latest":                 "claude-opus-5-5",
		"global.anthropic.claude-haiku-4-5-v1:0": "claude-haiku-4-5",
	}
	for in, want := range normalized {
		if _, id, k := b.Lookup(in); id != want || k != PriceNormalized {
			t.Errorf("Lookup(%q) = %s,%v want %s,normalized", in, id, k, want)
		}
	}
}

func TestLookup_FamilyFallbackIsMarkedEstimated(t *testing.T) {
	b := DefaultBook()
	cases := map[string]string{
		"claude-opus-5-9":            "claude-opus-5-5",   // newer than any known opus -> latest known
		"claude-opus-4-9":            "claude-opus-4-8",   // between known versions -> highest not above it
		"claude-sonnet-9-0":          "claude-sonnet-5-5", // far future
		"opus":                       "claude-opus-5-5",   // bare aliases -> latest in family
		"sonnet":                     "claude-sonnet-5-5",
		"haiku":                      "claude-haiku-5-5",
		"claude-3-5-sonnet-20241022": "claude-sonnet-4", // legacy naming, older than any known sonnet -> lowest known
	}
	for in, want := range cases {
		if _, id, k := b.Lookup(in); id != want || k != PriceFamily {
			t.Errorf("Lookup(%q) = %s,%v want %s,family", in, id, k, want)
		}
	}
}

func TestLookup_UnknownModels(t *testing.T) {
	b := DefaultBook()
	for _, in := range []string{"", "<synthetic>", "gpt-4o", "gemini-2", "llama-3", "claude", "claude-unknownfamily-5-0"} {
		if _, id, k := b.Lookup(in); k != PriceNone || id != "" {
			t.Errorf("Lookup(%q) = %s,%v want none", in, id, k)
		}
	}
}

func TestCost_Basics(t *testing.T) {
	b := DefaultBook()
	// opus 5.5: (1000*4 + 2000*20 + 10000*0.2 + 5000*8) / 1e6
	c := b.Cost(Usage{Model: "claude-opus-5-5", Input: 1000, Output: 2000, CacheRead: 10000, CacheWrite1h: 5000})
	if !approx(c.USD, 0.086) || c.Match != PriceExact {
		t.Errorf("cost = %v (%v), want 0.086 exact", c.USD, c.Match)
	}
	// 5-minute and 1-hour writes use different rates
	five := b.Cost(Usage{Model: "claude-sonnet-5-5", CacheWrite5m: 1_000_000})
	hour := b.Cost(Usage{Model: "claude-sonnet-5-5", CacheWrite1h: 1_000_000})
	if !approx(five.USD, 2.5) || !approx(hour.USD, 4.0) {
		t.Errorf("5m=%v 1h=%v want 2.5 and 4.0", five.USD, hour.USD)
	}
	if c := b.Cost(Usage{Model: "gpt-4o", Input: 1_000_000}); c.USD != 0 || c.Match != PriceNone {
		t.Errorf("unpriced model must cost 0 and be flagged: %+v", c)
	}
	if c := b.Cost(Usage{Model: "claude-opus-5-9", Input: 1_000_000}); c.Match != PriceFamily || !approx(c.USD, 4) {
		t.Errorf("family fallback: %+v", c)
	}
}

func TestCost_HaikuLongContextBoundary(t *testing.T) {
	b := DefaultBook()
	// prompt length counts input + cache reads + cache writes
	at := b.Cost(Usage{Model: "claude-haiku-5-5", Input: 50_000, CacheRead: 30_000, CacheWrite1h: 20_000}) // exactly 100,000
	over := b.Cost(Usage{Model: "claude-haiku-5-5", Input: 50_000, CacheRead: 30_000, CacheWrite1h: 20_001})
	// at: 50000*0.1 + 30000*0.01 + 20000*0.2 = 5000+300+4000 = 9300 -> $0.0093
	if !approx(at.USD, 0.0093) {
		t.Errorf("at threshold = %v, want 0.0093 (standard rates)", at.USD)
	}
	// over: 50000*0.5 + 30000*0.05 + 20001*1.0 = 25000+1500+20001 = 46501 -> $0.046501
	if !approx(over.USD, 0.046501) {
		t.Errorf("over threshold = %v, want 0.046501 (long-context rates)", over.USD)
	}
}

func TestCost_ModifiersStack(t *testing.T) {
	b := DefaultBook()
	base := b.Cost(Usage{Model: "claude-opus-5-5", Input: 1_000_000, Output: 1_000_000}).USD // 24
	fast := b.Cost(Usage{Model: "claude-opus-5-5", Input: 1_000_000, Output: 1_000_000, Speed: "fast"}).USD
	us := b.Cost(Usage{Model: "claude-opus-5-5", Input: 1_000_000, Output: 1_000_000, Geo: "us"}).USD
	both := b.Cost(Usage{Model: "claude-opus-5-5", Input: 1_000_000, Output: 1_000_000, Speed: "fast", Geo: "us"}).USD
	if !approx(base, 24) || !approx(fast, 48) || !approx(us, 26.4) || !approx(both, 52.8) {
		t.Errorf("base=%v fast=%v us=%v both=%v", base, fast, us, both)
	}
	if g := b.Cost(Usage{Model: "claude-opus-5-5", Input: 1_000_000, Geo: "global", Speed: "standard"}).USD; !approx(g, 4) {
		t.Errorf("global/standard must not change the price: %v", g)
	}
	// data residency multiplier only applies to Claude 4.6 and later
	old := b.Cost(Usage{Model: "claude-opus-4-5", Input: 1_000_000, Geo: "us"}).USD
	if !approx(old, 5) {
		t.Errorf("opus 4.5 has no US-geo multiplier, got %v", old)
	}
	ws := b.Cost(Usage{Model: "claude-sonnet-5-5", WebSearches: 3}).USD
	if !approx(ws, 0.03) {
		t.Errorf("3 web searches = %v, want 0.03", ws)
	}
}

func TestValidateRejectsBadBooks(t *testing.T) {
	good := func() *PriceBook {
		b := *DefaultBook()
		b.Models = map[string]ModelPrice{"claude-opus-5-5": DefaultBook().Models["claude-opus-5-5"]}
		return &b
	}
	if err := good().Validate(); err != nil {
		t.Fatalf("baseline must validate: %v", err)
	}
	mutate := map[string]func(*PriceBook){
		"no models":       func(b *PriceBook) { b.Models = map[string]ModelPrice{} },
		"wrong version":   func(b *PriceBook) { b.Version = 2 },
		"bad key":         func(b *PriceBook) { b.Models["not-a-model"] = b.Models["claude-opus-5-5"] },
		"zero input":      func(b *PriceBook) { m := b.Models["claude-opus-5-5"]; m.Input = 0; b.Models["claude-opus-5-5"] = m },
		"negative output": func(b *PriceBook) { m := b.Models["claude-opus-5-5"]; m.Output = -1; b.Models["claude-opus-5-5"] = m },
		"absurd price":    func(b *PriceBook) { m := b.Models["claude-opus-5-5"]; m.Output = 1e6; b.Models["claude-opus-5-5"] = m },
		"1h below 5m write": func(b *PriceBook) {
			m := b.Models["claude-opus-5-5"]
			m.CacheWrite1h = 1
			b.Models["claude-opus-5-5"] = m
		},
		"read above input": func(b *PriceBook) {
			m := b.Models["claude-opus-5-5"]
			m.CacheRead = 99
			b.Models["claude-opus-5-5"] = m
		},
		"nan": func(b *PriceBook) {
			m := b.Models["claude-opus-5-5"]
			m.Input = math.NaN()
			b.Models["claude-opus-5-5"] = m
		},
		"bad fast mult": func(b *PriceBook) { b.Modifiers.FastMultiplier = 50 },
		"bad long ctx": func(b *PriceBook) {
			m := b.Models["claude-opus-5-5"]
			m.LongContext = &LongContextRate{OverTokens: 0, Rate: m.Rate}
			b.Models["claude-opus-5-5"] = m
		},
	}
	for name, f := range mutate {
		b := good()
		f(b)
		if err := b.Validate(); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}

func TestParsePriceBook_RejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "not json", "{}", `{"version":1,"models":{}}`, strings.Repeat("{", 100)} {
		if _, err := ParsePriceBook([]byte(s)); err == nil {
			t.Errorf("ParsePriceBook(%.20q) should fail", s)
		}
	}
	// round trip of the embedded document
	b, err := ParsePriceBook(embeddedPricing)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(b)
	if _, err := ParsePriceBook(raw); err != nil {
		t.Errorf("round trip failed: %v", err)
	}
}

func TestMerge_OverrideWinsPerModel(t *testing.T) {
	base := DefaultBook()
	over := &PriceBook{Version: 1, Models: map[string]ModelPrice{
		"claude-opus-5-5": {Rate: Rate{Input: 1, CacheWrite5m: 1.25, CacheWrite1h: 2, CacheRead: 0.1, Output: 5}},
		"claude-opus-9-9": {Rate: Rate{Input: 9, CacheWrite5m: 11, CacheWrite1h: 18, CacheRead: 0.9, Output: 45}},
	}, Modifiers: Modifiers{FastMultiplier: 3}, AsOf: "2099-01-01"}
	m := base.Merge(over)
	if m.Models["claude-opus-5-5"].Input != 1 || m.Models["claude-sonnet-5-5"].Input != 2 {
		t.Errorf("per-model override wrong: %+v / %+v", m.Models["claude-opus-5-5"], m.Models["claude-sonnet-5-5"])
	}
	if _, ok := m.Models["claude-opus-9-9"]; !ok {
		t.Errorf("new model from override missing")
	}
	if m.Modifiers.FastMultiplier != 3 || m.Modifiers.USGeoMultiplier != base.Modifiers.USGeoMultiplier {
		t.Errorf("zero modifiers must not override: %+v", m.Modifiers)
	}
	if base.Models["claude-opus-5-5"].Input != 4 {
		t.Errorf("Merge must not mutate the base book")
	}
	// the override can price a model the base only knew by family fallback
	if _, _, k := m.Lookup("claude-opus-9-9"); k != PriceExact {
		t.Errorf("merged model should be an exact match, got %v", k)
	}
}
