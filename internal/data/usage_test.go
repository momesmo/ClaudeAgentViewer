package data

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTally_DedupesRepeatedMessageID(t *testing.T) {
	tl := NewUsageTally(DefaultBook())
	u := Usage{Model: "claude-sonnet-5-5", Input: 10, Output: 100, CacheRead: 1000, CacheWrite1h: 500}
	// Claude Code logs one JSONL line per content block, each repeating the message's usage.
	tl.Add("msg_1", u)
	tl.Add("msg_1", u)
	tl.Add("msg_1", u)
	tl.Add("msg_2", u)
	var s SessionStats
	tl.Apply(&s)
	if s.TotalOutputTokens != 200 || s.TotalInputTokens != 20 || s.CacheReadTokens != 2000 || s.CacheWriteTokens != 1000 {
		t.Errorf("tokens counted wrong: %+v", s)
	}
	want := 2 * DefaultBook().Cost(u).USD
	if !approx(s.EstimatedCostUSD, want) {
		t.Errorf("cost = %v, want %v", s.EstimatedCostUSD, want)
	}
}

func TestTally_EmptyIDIsNeverDeduped(t *testing.T) {
	tl := NewUsageTally(DefaultBook())
	u := Usage{Model: "claude-sonnet-5-5", Output: 100}
	tl.Add("", u)
	tl.Add("", u)
	var s SessionStats
	tl.Apply(&s)
	if s.TotalOutputTokens != 200 {
		t.Errorf("lines without an ID cannot be deduplicated, got %d", s.TotalOutputTokens)
	}
}

func TestTally_MixedModelsPricedPerMessageAndDeterministic(t *testing.T) {
	b := DefaultBook()
	sonnet := Usage{Model: "claude-sonnet-5-5", Input: 1000, Output: 1000}
	opus := Usage{Model: "claude-opus-5-5", Input: 1000, Output: 1000}
	for i := 0; i < 20; i++ { // map iteration order must not matter
		tl := NewUsageTally(b)
		tl.Add("a", sonnet)
		tl.Add("b", opus)
		tl.Add("c", sonnet)
		var s SessionStats
		tl.Apply(&s)
		want := 2*b.Cost(sonnet).USD + b.Cost(opus).USD
		if !approx(s.EstimatedCostUSD, want) {
			t.Fatalf("cost = %v, want %v", s.EstimatedCostUSD, want)
		}
		if len(s.Models) != 2 || s.Models[0] != "claude-sonnet-5-5" || s.Models[1] != "claude-opus-5-5" {
			t.Fatalf("models must be ordered by cost, got %v", s.Models)
		}
		if len(s.ModelBreakdown) != 2 || s.ModelBreakdown[0].Model != "claude-sonnet-5-5" || s.ModelBreakdown[0].Messages != 2 {
			t.Fatalf("breakdown wrong: %+v", s.ModelBreakdown)
		}
		if !approx(s.ModelBreakdown[0].CostUSD+s.ModelBreakdown[1].CostUSD, s.EstimatedCostUSD) {
			t.Fatalf("breakdown must add up to the total")
		}
	}
}

func TestTally_UnpricedAndEstimated(t *testing.T) {
	tl := NewUsageTally(DefaultBook())
	tl.Add("a", Usage{Model: "claude-sonnet-5-5", Input: 1_000_000})
	tl.Add("b", Usage{Model: "mystery-model-1", Input: 500, Output: 500, CacheRead: 1000})
	tl.Add("c", Usage{Model: "claude-opus-9-9", Input: 1_000_000}) // family fallback -> estimate
	tl.Add("d", Usage{Model: "<synthetic>"})                       // placeholder with no usage: ignored entirely
	var s SessionStats
	tl.Apply(&s)
	if !s.CostEstimated {
		t.Errorf("family-matched model must mark the cost as estimated")
	}
	if len(s.UnpricedModels) != 1 || s.UnpricedModels[0] != "mystery-model-1" || s.UnpricedTokens != 2000 {
		t.Errorf("unpriced = %v / %d tokens", s.UnpricedModels, s.UnpricedTokens)
	}
	for _, m := range s.Models {
		if m == "<synthetic>" {
			t.Errorf("<synthetic> must not be listed as a model")
		}
	}
	if !approx(s.EstimatedCostUSD, 2+4) { // sonnet 1M in = $2, opus-5-5 (fallback) 1M in = $4
		t.Errorf("cost = %v, want 6 (unpriced model contributes nothing)", s.EstimatedCostUSD)
	}
	if s.PricingAsOf == "" {
		t.Errorf("stats should record which price table was used")
	}
}

func TestSplitCacheWrites(t *testing.T) {
	cases := []struct{ total, f5, f1, w5, w1 int }{
		{1000, 0, 1000, 0, 1000}, // split present
		{1000, 400, 600, 400, 600},
		{1000, 0, 0, 1000, 0},      // no split recorded: the API's default TTL is 5 minutes
		{1000, 300, 200, 800, 200}, // split smaller than total: remainder is 5m
		{0, 0, 0, 0, 0},
	}
	for _, c := range cases {
		w5, w1 := splitCacheWrites(c.total, c.f5, c.f1)
		if w5 != c.w5 || w1 != c.w1 {
			t.Errorf("splitCacheWrites(%d,%d,%d) = %d,%d want %d,%d", c.total, c.f5, c.f1, w5, w1, c.w5, c.w1)
		}
	}
}

// writeSession writes a transcript in Claude Code's real shape: one line per
// content block, each repeating the message's usage.
func writeSession(t *testing.T) string {
	t.Helper()
	line := func(typ, id, model string, in, out, cr, w5, w1 int, speed, geo, block string) string {
		return fmt.Sprintf(`{"type":%q,"sessionId":"s","timestamp":"2026-11-13T10:00:00.000Z","message":{"id":%q,"role":"assistant","model":%q,"content":[%s],"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":%d,"cache_creation_input_tokens":%d,"cache_creation":{"ephemeral_5m_input_tokens":%d,"ephemeral_1h_input_tokens":%d},"speed":%q,"inference_geo":%q,"server_tool_use":{"web_search_requests":0}}}}`,
			typ, id, model, block, in, out, cr, w5+w1, w5, w1, speed, geo)
	}
	think := `{"type":"thinking","thinking":"hm"}`
	tool := `{"type":"tool_use","id":"t1","name":"Bash","input":{}}`
	text := `{"type":"text","text":"done"}`
	lines := []string{
		`{"type":"user","sessionId":"s","timestamp":"2026-11-13T09:59:00.000Z","message":{"role":"user","content":"hi"}}`,
		line("assistant", "m1", "claude-sonnet-5-5", 10, 500, 20000, 0, 3000, "standard", "global", think),
		line("assistant", "m1", "claude-sonnet-5-5", 10, 500, 20000, 0, 3000, "standard", "global", tool),
		line("assistant", "m1", "claude-sonnet-5-5", 10, 500, 20000, 0, 3000, "standard", "global", text),
		line("assistant", "m2", "claude-opus-5-5", 5, 800, 50000, 1000, 2000, "fast", "us", text),
		line("assistant", "m2", "claude-opus-5-5", 5, 800, 50000, 1000, 2000, "fast", "us", tool),
		line("assistant", "m3", "<synthetic>", 0, 0, 0, 0, 0, "standard", "global", text),
	}
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSessionStats_QuickAndFullAgreeAndMatchHandComputation(t *testing.T) {
	p := writeSession(t)
	quick, err := QuickScanStats(p)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := LoadTranscript(p)
	if err != nil {
		t.Fatal(err)
	}
	full := ComputeStats(msgs)

	b := DefaultBook()
	m1 := b.Cost(Usage{Model: "claude-sonnet-5-5", Input: 10, Output: 500, CacheRead: 20000, CacheWrite1h: 3000, Speed: "standard", Geo: "global"}).USD
	m2 := b.Cost(Usage{Model: "claude-opus-5-5", Input: 5, Output: 800, CacheRead: 50000, CacheWrite5m: 1000, CacheWrite1h: 2000, Speed: "fast", Geo: "us"}).USD
	want := m1 + m2
	if m2 < m1*0.5 { // sanity: fast + US on opus costs more than the standard sonnet message
		t.Fatalf("test fixture sanity: m1=%v m2=%v", m1, m2)
	}
	for name, s := range map[string]SessionStats{"quick": quick, "full": full} {
		if !approx(s.EstimatedCostUSD, want) {
			t.Errorf("%s cost = %v, want %v", name, s.EstimatedCostUSD, want)
		}
		if s.TotalOutputTokens != 1300 || s.TotalInputTokens != 15 || s.CacheReadTokens != 70000 || s.CacheWriteTokens != 6000 {
			t.Errorf("%s tokens wrong (each message must be counted once): %+v", name, s)
		}
		if len(s.Models) != 2 || s.UnpricedTokens != 0 {
			t.Errorf("%s models/unpriced wrong: %v %d", name, s.Models, s.UnpricedTokens)
		}
	}
	if !approx(quick.EstimatedCostUSD, full.EstimatedCostUSD) || quick.TotalOutputTokens != full.TotalOutputTokens {
		t.Errorf("quick and full scans disagree: %v vs %v", quick.EstimatedCostUSD, full.EstimatedCostUSD)
	}
}
