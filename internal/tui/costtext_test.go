package tui

import (
	"strings"
	"testing"

	"github.com/user/claudevu/internal/data"
)

func TestCostText(t *testing.T) {
	cases := []struct {
		usd       float64
		est, unpr bool
		want      string
	}{
		{12.4, false, false, "$12.40"},
		{12.4, true, false, "~$12.40"},
		{12.4, false, true, "$12.40+"},
		{12.4, true, true, "~$12.40+"},
		{0, false, true, "unpriced"},
		{0, false, false, "$0.00"},
	}
	for _, c := range cases {
		if got := costText(c.usd, c.est, c.unpr); got != c.want {
			t.Errorf("costText(%v,%v,%v) = %q, want %q", c.usd, c.est, c.unpr, got, c.want)
		}
	}
}

func TestDashboard_ShowsCostCaveats(t *testing.T) {
	m := Model{width: 120, height: 60, currentView: viewDashboard, dashboardData: &data.DashboardData{
		TotalSessions: 3, TotalCost: 6, CostEstimated: true, UnpricedTokens: 1_500_000, UnpricedModels: []string{"mystery-1"}, PricingAsOf: "2026-11-16",
		ByModel: []data.ModelStats{
			{Model: "claude-sonnet-5-5", Sessions: 2, Tokens: 100, Cost: 3, Estimated: true},
			{Model: "mystery-1", Sessions: 1, Tokens: 50, Unpriced: true},
		},
		ByProject: []data.ProjectStats{{Project: "p", Sessions: 3, Cost: 6, Estimated: true, Unpriced: true}},
		TopCost:   []data.SessionCostEntry{{SessionID: "abc", Display: "x", Cost: 2, Estimated: true, Model: "claude-sonnet-5-5"}},
	}}
	out := m.renderDashboard()
	for _, want := range []string{"~$6.00+", "~$3.00", "unpriced", "mystery-1", "1.5M", "2026-11-16"} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard missing %q:\n%s", want, out)
		}
	}
}

func TestDetail_CostSectionShowsBreakdownAndUnpriced(t *testing.T) {
	s := data.SessionInfo{SessionID: "abc-123", Display: "x"}
	m := Model{width: 120, height: 40, selectedInfo: &s, stats: data.SessionStats{
		MessageCount: 5, TotalOutputTokens: 100, EstimatedCostUSD: 4, PricingAsOf: "2026-11-16",
		UnpricedModels: []string{"mystery-1"}, UnpricedTokens: 900, CostEstimated: true,
		ModelBreakdown: []data.ModelUsage{
			{Model: "claude-opus-5-5", Messages: 2, CostUSD: 3, Match: data.PriceExact},
			{Model: "claude-opus-9-9", Messages: 1, CostUSD: 1, Match: data.PriceFamily},
			{Model: "mystery-1", Messages: 1, Match: data.PriceNone},
		},
	}}
	out := m.renderDetail(100, 60)
	for _, want := range []string{"~$4.00+", "claude-opus-5-5", "claude-opus-9-9", "mystery-1", "900", "2026-11-16"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q:\n%s", want, out)
		}
	}
}
