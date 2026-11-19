package main

import (
	"math"
	"os"
	"sort"
	"testing"

	"github.com/user/claudevu/internal/data"
	"github.com/user/claudevu/internal/testfixture"
)

const eps = 1e-9

func near(a, b float64) bool { return math.Abs(a-b) < eps }

func TestLoadHistory(t *testing.T) {
	fx := testfixture.BuildClaudeDir(t)
	groups, err := data.LoadHistory(fx.ClaudeDir)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(groups) != len(fx.Projects) {
		t.Fatalf("projects = %d, want %d", len(groups), len(fx.Projects))
	}
	total := 0
	for i, g := range groups {
		want := fx.Projects[i]
		if g.Project != want.Path || len(g.Sessions) != len(want.Sessions) {
			t.Fatalf("group %d = %s (%d sessions), want %s (%d)", i, g.Project, len(g.Sessions), want.Path, len(want.Sessions))
		}
		for j, s := range g.Sessions {
			ws := want.Sessions[j]
			total++
			if s.SessionID != ws.ID || s.Display != ws.Display {
				t.Errorf("%s: got %q %q, want %q %q (latest history entry wins, newest first)", ws.Key, s.SessionID, s.Display, ws.ID, ws.Display)
			}
			if (s.FilePath != "") != ws.HasFile {
				t.Errorf("%s: FilePath %q, HasFile=%v", ws.Key, s.FilePath, ws.HasFile)
			}
			if s.CustomTitle != ws.CustomTitle || s.AgentName != ws.AgentName {
				t.Errorf("%s: title/agent = %q/%q, want %q/%q", ws.Key, s.CustomTitle, s.AgentName, ws.CustomTitle, ws.AgentName)
			}
		}
	}
	if total != len(fx.Sessions) || total < 6 {
		t.Errorf("sessions = %d, want %d (>=6)", total, len(fx.Sessions))
	}
}

// scanBoth runs the quick scan and the full transcript pass and returns both.
func scanBoth(t *testing.T, s *testfixture.SessionExp) (quick, full data.SessionStats) {
	t.Helper()
	return scanBothLines(t, s, s.Lines)
}

// scanBothLines is scanBoth when some lines are expected to be skipped.
func scanBothLines(t *testing.T, s *testfixture.SessionExp, wantLines int) (quick, full data.SessionStats) {
	t.Helper()
	quick, err := data.QuickScanStats(s.Path)
	if err != nil {
		t.Fatalf("%s QuickScanStats: %v", s.Key, err)
	}
	msgs, err := data.LoadTranscript(s.Path)
	if err != nil {
		t.Fatalf("%s LoadTranscript: %v", s.Key, err)
	}
	if len(msgs) != wantLines {
		t.Errorf("%s: LoadTranscript returned %d messages, want %d", s.Key, len(msgs), wantLines)
	}
	return quick, data.ComputeStats(msgs)
}

func checkStats(t *testing.T, label string, got data.SessionStats, s *testfixture.SessionExp) {
	t.Helper()
	checkStatsLines(t, label, got, s, s.Lines)
}

func checkStatsLines(t *testing.T, label string, got data.SessionStats, s *testfixture.SessionExp, wantLines int) {
	t.Helper()
	if got.MessageCount != wantLines {
		t.Errorf("%s %s: MessageCount = %d, want %d", s.Key, label, got.MessageCount, wantLines)
	}
	// duplicated block lines must not multiply usage
	if got.TotalInputTokens != s.Input || got.TotalOutputTokens != s.Output ||
		got.CacheReadTokens != s.CacheRead || got.CacheWriteTokens != s.CacheWrite {
		t.Errorf("%s %s: tokens in/out/cr/cw = %d/%d/%d/%d, want %d/%d/%d/%d", s.Key, label,
			got.TotalInputTokens, got.TotalOutputTokens, got.CacheReadTokens, got.CacheWriteTokens,
			s.Input, s.Output, s.CacheRead, s.CacheWrite)
	}
	if !near(got.EstimatedCostUSD, s.CostUSD) {
		t.Errorf("%s %s: cost = %v, want %v", s.Key, label, got.EstimatedCostUSD, s.CostUSD)
	}
	if len(got.ModelBreakdown) != len(s.PerModel) {
		t.Errorf("%s %s: %d model rows, want %d", s.Key, label, len(got.ModelBreakdown), len(s.PerModel))
	}
	sum := 0.0
	for _, mu := range got.ModelBreakdown {
		w, ok := s.PerModel[mu.Model]
		if !ok {
			t.Errorf("%s %s: unexpected model %q", s.Key, label, mu.Model)
			continue
		}
		sum += mu.CostUSD
		if mu.Messages != w.Messages || mu.Tokens != w.Tokens || !near(mu.CostUSD, w.CostUSD) {
			t.Errorf("%s %s %s: msgs/tokens/cost = %d/%d/%v, want %d/%d/%v", s.Key, label, mu.Model,
				mu.Messages, mu.Tokens, mu.CostUSD, w.Messages, w.Tokens, w.CostUSD)
		}
		if (mu.Match == data.PriceNone) != w.Unpriced || (mu.Match == data.PriceFamily) != w.Estimated {
			t.Errorf("%s %s %s: price match %v, want unpriced=%v estimated=%v", s.Key, label, mu.Model, mu.Match, w.Unpriced, w.Estimated)
		}
	}
	if !near(sum, got.EstimatedCostUSD) {
		t.Errorf("%s %s: per-model costs sum to %v, total %v", s.Key, label, sum, got.EstimatedCostUSD)
	}
	if (len(got.UnpricedModels) > 0) != s.Unpriced || got.UnpricedTokens != s.UnpricedToks || got.CostEstimated != s.Estimated {
		t.Errorf("%s %s: unpriced=%v/%d estimated=%v, want %v/%d %v", s.Key, label,
			got.UnpricedModels, got.UnpricedTokens, got.CostEstimated, s.Unpriced, s.UnpricedToks, s.Estimated)
	}
	gm := append([]string(nil), got.Models...)
	wm := append([]string(nil), s.Models...)
	sort.Strings(gm)
	sort.Strings(wm)
	if len(gm) != len(wm) {
		t.Fatalf("%s %s: models %v, want %v", s.Key, label, gm, wm)
	}
	for i := range gm {
		if gm[i] != wm[i] {
			t.Errorf("%s %s: models %v, want %v", s.Key, label, gm, wm)
		}
	}
}

func TestStatsMatchFixtureForEverySession(t *testing.T) {
	fx := testfixture.BuildClaudeDir(t)
	n := 0
	for _, s := range fx.Sessions {
		if !s.HasFile {
			continue
		}
		n++
		quick, full := scanBoth(t, s)
		checkStats(t, "quick", quick, s)
		checkStats(t, "full", full, s)
		if full.ToolCallCount != s.ToolUses {
			t.Errorf("%s: ToolCallCount = %d, want %d", s.Key, full.ToolCallCount, s.ToolUses)
		}
		if quick.ToolCallCount != s.ToolUses || full.ToolCallCount != quick.ToolCallCount {
			t.Errorf("%s: tool calls quick=%d full=%d, want %d", s.Key, quick.ToolCallCount, full.ToolCallCount, s.ToolUses)
		}
		if full.Duration != s.Duration {
			t.Errorf("%s: Duration = %v, want %v", s.Key, full.Duration, s.Duration)
		}
		// quick and full scans agree on everything they both report
		if quick.MessageCount != full.MessageCount || quick.TotalInputTokens != full.TotalInputTokens ||
			quick.TotalOutputTokens != full.TotalOutputTokens || !near(quick.EstimatedCostUSD, full.EstimatedCostUSD) {
			t.Errorf("%s: quick %+v disagrees with full %+v", s.Key, quick, full)
		}
	}
	if n != fx.ScanSessions {
		t.Errorf("scanned %d sessions, fixture says %d", n, fx.ScanSessions)
	}
}

// The fixture's big line is 320 KB, below the real 32 MiB limit; lowering the
// limit below it makes the scanners hit the oversize path. The oversize line
// must be skipped, every later message still counted, with no error.
func TestBigLineDoesNotDropLaterMessages(t *testing.T) {
	fx := testfixture.BuildClaudeDir(t)
	s := fx.Session(t, "bigline")
	fi, err := os.Stat(s.Path)
	if err != nil || fi.Size() < testfixture.BigLineBytes {
		t.Fatalf("fixture transcript missing or too small: %v %v", fi, err)
	}

	// default limit: the big line is a normal user message
	quick, full := scanBoth(t, s)
	checkStats(t, "quick", quick, s)
	checkStats(t, "full", full, s)

	// lowered limit: the big line is skipped, the three messages after it survive
	defer data.SetStatsLineLimitForTest(64 << 10)()
	want := s.Lines - s.BigLines
	quick, full = scanBothLines(t, s, want)
	checkStatsLines(t, "quick(limit 64KB)", quick, s, want)
	checkStatsLines(t, "full(limit 64KB)", full, s, want)
	if quick.TotalOutputTokens != 100+900+300 || full.TotalOutputTokens != 100+900+300 {
		t.Errorf("later messages lost: out quick=%d full=%d, want 1300", quick.TotalOutputTokens, full.TotalOutputTokens)
	}
}

// Hand-derived per-message sums (see testfixture for the arithmetic), checked
// against what the scanners report: mixed models, fast, US geo, 5m and 1h writes.
func TestCostsMatchHandDerivedLiterals(t *testing.T) {
	fx := testfixture.BuildClaudeDir(t)
	lit := map[string]float64{
		"tiny":     testfixture.LiteralTinyUSD,
		"recent":   testfixture.LiteralRecentUSD,
		"speedgeo": testfixture.LiteralSpeedGeoUSD,
		"mixed":    0.00915 + 0.0305, // sonnet 5650+3500 micro-USD, opus-4-8 30500
	}
	for key, want := range lit {
		quick, full := scanBoth(t, fx.Session(t, key))
		if !near(quick.EstimatedCostUSD, want) || !near(full.EstimatedCostUSD, want) {
			t.Errorf("%s: cost quick=%v full=%v, want %v", key, quick.EstimatedCostUSD, full.EstimatedCostUSD, want)
		}
	}
}

func TestMixedModelSessionPricedPerModel(t *testing.T) {
	fx := testfixture.BuildClaudeDir(t)
	quick, full := scanBoth(t, fx.Session(t, "mixed"))
	for name, st := range map[string]data.SessionStats{"quick": quick, "full": full} {
		rows := map[string]data.ModelUsage{}
		for _, r := range st.ModelBreakdown {
			rows[r.Model] = r
		}
		if len(rows) != 2 {
			t.Fatalf("%s: model rows = %d, want 2", name, len(rows))
		}
		son, opus := rows["claude-sonnet-5-5"], rows["claude-opus-4-8"]
		if son.Messages != 2 || opus.Messages != 1 {
			t.Errorf("%s: messages sonnet=%d opus=%d, want 2 and 1", name, son.Messages, opus.Messages)
		}
		if !near(son.CostUSD, 0.00915) || !near(opus.CostUSD, 0.0305) {
			t.Errorf("%s: per-model cost sonnet=%v opus=%v, want 0.00915 and 0.0305", name, son.CostUSD, opus.CostUSD)
		}
	}
}

// Locally generated <synthetic> lines and truncated JSON lines must be ignored.
func TestSyntheticAndGarbageLinesIgnored(t *testing.T) {
	fx := testfixture.BuildClaudeDir(t)
	quick, full := scanBoth(t, fx.Session(t, "recent"))
	for name, st := range map[string]data.SessionStats{"quick": quick, "full": full} {
		for _, m := range st.Models {
			if m == "<synthetic>" {
				t.Errorf("%s: <synthetic> reported as a model", name)
			}
		}
		if len(st.ModelBreakdown) != 1 || st.TotalInputTokens >= 99999 {
			t.Errorf("%s: breakdown=%d input=%d (garbage line or synthetic usage leaked)", name, len(st.ModelBreakdown), st.TotalInputTokens)
		}
	}
}

func TestUnknownModelIsUnpricedNotZeroCost(t *testing.T) {
	fx := testfixture.BuildClaudeDir(t)
	quick, full := scanBoth(t, fx.Session(t, "mystery"))
	// 400+120+1000 and 450+80+1500 tokens, none of them costed
	for name, st := range map[string]data.SessionStats{"quick": quick, "full": full} {
		if len(st.UnpricedModels) != 1 || st.UnpricedModels[0] != "mystery-model-1" || st.UnpricedTokens != 3550 || st.EstimatedCostUSD != 0 {
			t.Errorf("%s: unpriced %v tokens %d cost %v, want [mystery-model-1] 3550 0", name, st.UnpricedModels, st.UnpricedTokens, st.EstimatedCostUSD)
		}
	}
}

func TestSubagents(t *testing.T) {
	fx := testfixture.BuildClaudeDir(t)
	s := fx.Session(t, "recent")
	subs, err := data.ScanSubagents(fx.ClaudeDir, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != len(s.Subagents) || len(subs) != 2 {
		t.Fatalf("subagents = %d, want %d", len(subs), len(s.Subagents))
	}
	want := map[string]testfixture.SubExp{}
	for _, w := range s.Subagents {
		want[w.ID] = w
	}
	for i := range subs {
		w, ok := want[subs[i].AgentID]
		if !ok {
			t.Fatalf("unexpected subagent %q", subs[i].AgentID)
		}
		if subs[i].AgentType != w.Type || subs[i].IsRunning != w.Running {
			t.Errorf("%s: type=%q running=%v, want %q %v", w.ID, subs[i].AgentType, subs[i].IsRunning, w.Type, w.Running)
		}
		if err := data.LoadSubagentTranscript(&subs[i]); err != nil {
			t.Fatal(err)
		}
		if subs[i].Stats.MessageCount != w.Lines || subs[i].Stats.TotalInputTokens+subs[i].Stats.TotalOutputTokens != w.Tokens {
			t.Errorf("%s: msgs=%d tokens=%d, want %d/%d", w.ID, subs[i].Stats.MessageCount,
				subs[i].Stats.TotalInputTokens+subs[i].Stats.TotalOutputTokens, w.Lines, w.Tokens)
		}
	}
	if !subs[0].IsRunning {
		t.Errorf("running subagents must sort first: %+v", subs[0])
	}
	if got := data.CountRunningSubagents(subs); got != 1 {
		t.Errorf("running = %d, want 1", got)
	}
	total, running := data.QuickSubagentCount(fx.ClaudeDir, s.ID)
	if total != 2 || running != 1 {
		t.Errorf("QuickSubagentCount = %d/%d, want 2/1", total, running)
	}
	if r := data.RefreshSubagentStatus(fx.ClaudeDir, s.ID, subs); len(r) != 2 || data.CountRunningSubagents(r) != 1 {
		t.Errorf("RefreshSubagentStatus lost subagents: %d", len(r))
	}
	// sessions without subagents report none
	if other, _ := data.ScanSubagents(fx.ClaudeDir, fx.Session(t, "tiny").ID); len(other) != 0 {
		t.Errorf("tiny session has %d subagents", len(other))
	}
}

func TestPrivacy(t *testing.T) {
	// Secret-shaped strings are assembled at run time so no literal looks like a credential.
	tests := []struct{ input, expected string }{
		{"key: sk-proj-" + "abc123def456ghi789jkl012", "key: [REDACTED]"},
		{"token: ghp_" + "abcdefghijklmnopqrstuvwxyz1234567890", "token: [REDACTED]"},
		{"no secrets here", "no secrets here"},
	}
	for _, tt := range tests {
		if got := data.RedactSecrets(tt.input); got != tt.expected {
			t.Errorf("RedactSecrets(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
