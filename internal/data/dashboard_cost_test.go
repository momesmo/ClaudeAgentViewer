package data

import "testing"

func TestBuildDashboard_PerModelBreakdownAndCostFlags(t *testing.T) {
	groups := []ProjectGroup{{Project: "p", Sessions: []SessionInfo{{SessionID: "a"}, {SessionID: "b"}, {SessionID: "c"}}}}
	stats := map[string]SessionStats{
		"a": { // mixes two models: each must get its own share, not the whole session
			TotalInputTokens: 10, TotalOutputTokens: 290, EstimatedCostUSD: 4.0, Models: []string{"claude-opus-5-5", "claude-sonnet-5-5"},
			ModelBreakdown: []ModelUsage{
				{Model: "claude-opus-5-5", Messages: 2, Tokens: 200, CostUSD: 3.0, Match: PriceExact},
				{Model: "claude-sonnet-5-5", Messages: 5, Tokens: 100, CostUSD: 1.0, Match: PriceExact},
			},
			PricingAsOf: "2026-11-16",
		},
		"b": {
			TotalOutputTokens: 100, EstimatedCostUSD: 2.0, Models: []string{"claude-sonnet-5-5"}, CostEstimated: true,
			ModelBreakdown: []ModelUsage{{Model: "claude-sonnet-5-5", Messages: 1, Tokens: 100, CostUSD: 2.0, Match: PriceFamily}},
		},
		"c": {
			TotalOutputTokens: 50, Models: []string{"mystery"}, UnpricedModels: []string{"mystery"}, UnpricedTokens: 500,
			ModelBreakdown: []ModelUsage{{Model: "mystery", Messages: 1, Tokens: 50, Match: PriceNone}},
		},
	}
	d := BuildDashboard(groups, stats)
	if !approx(d.TotalCost, 6.0) {
		t.Errorf("total cost = %v, want 6", d.TotalCost)
	}
	byModel := map[string]ModelStats{}
	var sum float64
	for _, m := range d.ByModel {
		byModel[m.Model] = m
		sum += m.Cost
	}
	if !approx(sum, d.TotalCost) {
		t.Errorf("model rows (%v) must add up to the total (%v)", sum, d.TotalCost)
	}
	if o := byModel["claude-opus-5-5"]; !approx(o.Cost, 3.0) || o.Sessions != 1 || o.Tokens != 200 {
		t.Errorf("opus row = %+v", o)
	}
	if s := byModel["claude-sonnet-5-5"]; !approx(s.Cost, 3.0) || s.Sessions != 2 || s.Tokens != 200 || !s.Estimated {
		t.Errorf("sonnet row = %+v (one of its sessions was family-estimated)", s)
	}
	if m := byModel["mystery"]; !m.Unpriced || m.Cost != 0 {
		t.Errorf("unpriced row = %+v", m)
	}
	if !d.CostEstimated || d.UnpricedTokens != 500 || len(d.UnpricedModels) != 1 || d.PricingAsOf != "2026-11-16" {
		t.Errorf("dashboard flags wrong: est=%v unpriced=%d %v asOf=%q", d.CostEstimated, d.UnpricedTokens, d.UnpricedModels, d.PricingAsOf)
	}
}

func TestBuildDashboard_SessionWithoutBreakdownFallsBackToUnknown(t *testing.T) {
	groups := []ProjectGroup{{Project: "p", Sessions: []SessionInfo{{SessionID: "a"}}}}
	d := BuildDashboard(groups, map[string]SessionStats{"a": {TotalOutputTokens: 10, EstimatedCostUSD: 1}})
	if len(d.ByModel) != 1 || d.ByModel[0].Model != "unknown" || !approx(d.ByModel[0].Cost, 1) {
		t.Errorf("ByModel = %+v", d.ByModel)
	}
}
