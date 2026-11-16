package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/user/claudevu/internal/data"
	"github.com/user/claudevu/internal/testfixture"
	"github.com/user/claudevu/internal/tui"
)

func TestDashboard(t *testing.T) {
	fx := testfixture.BuildClaudeDir(t)
	m := newModel(t, fx, 120, 60)

	// 'd' starts the real scan; run its command and feed the result back.
	r, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m = r.(tui.Model)
	if !strings.Contains(m.View(), "Scanning") {
		t.Errorf("dashboard should show a scanning state while loading:\n%s", m.View())
	}
	if cmd == nil {
		t.Fatal("dashboard key returned no load command")
	}
	msg, ok := cmd().(tui.DashboardLoadedMsg)
	if !ok {
		t.Fatalf("load command returned %T", cmd())
	}
	d := msg.Data

	// totals equal the sums the fixture was built from
	if d.TotalSessions != fx.ScanSessions {
		t.Errorf("TotalSessions = %d, want %d (the session without a transcript is not scanned)", d.TotalSessions, fx.ScanSessions)
	}
	if d.TotalTokens != fx.TotalTokens {
		t.Errorf("TotalTokens = %d, want %d", d.TotalTokens, fx.TotalTokens)
	}
	if !near(d.TotalCost, fx.TotalCost) {
		t.Errorf("TotalCost = %v, want %v", d.TotalCost, fx.TotalCost)
	}
	if len(d.ByProject) != len(fx.Projects) {
		t.Errorf("ByProject rows = %d, want %d", len(d.ByProject), len(fx.Projects))
	}
	projSessions := 0
	for _, p := range d.ByProject {
		projSessions += p.Sessions
	}
	if projSessions != fx.ScanSessions {
		t.Errorf("project rows count %d sessions, want %d", projSessions, fx.ScanSessions)
	}

	// per-model rows add up to the total and match the fixture per model
	if len(d.ByModel) != len(fx.ByModel) {
		t.Errorf("ByModel rows = %d, want %d", len(d.ByModel), len(fx.ByModel))
	}
	var modelCost float64
	var modelTokens int
	for _, ms := range d.ByModel {
		w, ok := fx.ByModel[ms.Model]
		if !ok {
			t.Errorf("unexpected model row %q", ms.Model)
			continue
		}
		modelCost += ms.Cost
		modelTokens += ms.Tokens
		if ms.Tokens != w.Tokens || !near(ms.Cost, w.CostUSD) || ms.Unpriced != w.Unpriced || ms.Estimated != w.Estimated {
			t.Errorf("model %s = tokens %d cost %v unpriced %v est %v, want %d %v %v %v",
				ms.Model, ms.Tokens, ms.Cost, ms.Unpriced, ms.Estimated, w.Tokens, w.CostUSD, w.Unpriced, w.Estimated)
		}
	}
	if !near(modelCost, d.TotalCost) || modelTokens != d.TotalTokens {
		t.Errorf("model rows sum to %d tokens / %v, total %d / %v", modelTokens, modelCost, d.TotalTokens, d.TotalCost)
	}

	// the unknown model is flagged, not silently costed at zero
	myst := fx.Session(t, "mystery")
	if d.UnpricedTokens != myst.UnpricedToks || len(d.UnpricedModels) != 1 || d.UnpricedModels[0] != "mystery-model-1" {
		t.Errorf("unpriced = %d %v, want %d [mystery-model-1]", d.UnpricedTokens, d.UnpricedModels, myst.UnpricedToks)
	}
	if !ms(d, "mystery-model-1").Unpriced {
		t.Error("mystery-model-1 row must be marked unpriced")
	}
	for _, p := range d.ByProject {
		if p.Project == myst.Project && !p.Unpriced {
			t.Errorf("project %s includes an unpriced model but is not flagged", p.Project)
		}
		if p.Project == "/work/alpha" && p.Unpriced {
			t.Error("alpha has only priced models")
		}
	}
	if len(d.TopCost) == 0 || len(d.TopCost) > 5 {
		t.Errorf("TopCost has %d entries", len(d.TopCost))
	}
	for i := 1; i < len(d.TopCost); i++ {
		if d.TopCost[i].Cost > d.TopCost[i-1].Cost {
			t.Error("TopCost is not sorted by cost")
		}
	}

	// rendered
	m = upd(m, msg)
	show("DASHBOARD", m)
	view := m.View()
	for _, want := range []string{"Dashboard", "By Project", "By Model", "mystery-model-1", "claude-sonnet-5-5", "claude-opus-5-5", "claude-opus-4-8", "no price"} {
		if !strings.Contains(view, want) {
			t.Errorf("dashboard view lacks %q", want)
		}
	}
	checkFits(t, "dashboard", m, 120, 60)
	for _, sz := range [][2]int{{60, 20}, {40, 15}} {
		checkFits(t, "dashboard narrow", upd(m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]}), sz[0], sz[1])
	}
}

func ms(d data.DashboardData, model string) data.ModelStats {
	for _, x := range d.ByModel {
		if x.Model == model {
			return x
		}
	}
	return data.ModelStats{}
}
