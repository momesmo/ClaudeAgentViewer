package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/user/claudevu/internal/data"
)

func restoreBook(t *testing.T) {
	t.Helper()
	orig := data.DefaultBook()
	t.Cleanup(func() { data.SetDefaultBook(orig) })
}

func exactStats() data.SessionStats {
	return data.SessionStats{ModelBreakdown: []data.ModelUsage{{Model: "claude-opus-5-5", Match: data.PriceExact}}}
}
func familyStats() data.SessionStats {
	return data.SessionStats{CostEstimated: true, ModelBreakdown: []data.ModelUsage{{Model: "claude-opus-9-9", Match: data.PriceFamily}}}
}
func unpricedStats() data.SessionStats {
	return data.SessionStats{UnpricedTokens: 10, UnpricedModels: []string{"mystery"}, ModelBreakdown: []data.ModelUsage{{Model: "mystery", Match: data.PriceNone}}}
}

func autoModel(t *testing.T) Model {
	m := Model{opts: Options{PricingAuto: true, PricingCacheDir: t.TempDir()}}
	return m
}

func TestNeedsPricing(t *testing.T) {
	if needsPricing(exactStats()) {
		t.Errorf("exact match needs no update")
	}
	if !needsPricing(familyStats()) || !needsPricing(unpricedStats()) {
		t.Errorf("family-estimated and unpriced models should trigger an update")
	}
}

func TestMaybeRefreshPricing_Conditions(t *testing.T) {
	now := time.Now()
	m := autoModel(t)
	if cmd := m.maybeRefreshPricing(exactStats()); cmd != nil || m.pricingBusy {
		t.Errorf("no unknown model: nothing to do")
	}
	m = autoModel(t)
	if cmd := m.maybeRefreshPricing(familyStats()); cmd == nil || !m.pricingBusy {
		t.Errorf("unknown model with auto on: should start a refresh")
	}
	if cmd := m.maybeRefreshPricing(unpricedStats()); cmd != nil {
		t.Errorf("a refresh is already running: must not start another")
	}
	m = autoModel(t)
	m.opts.PricingAuto = false
	if cmd := m.maybeRefreshPricing(unpricedStats()); cmd != nil {
		t.Errorf("auto off: the network must never be touched")
	}
	m = autoModel(t)
	m.opts.PricingCacheDir = ""
	if cmd := m.maybeRefreshPricing(unpricedStats()); cmd != nil {
		t.Errorf("no usable cache dir: cannot store a result, so do not fetch")
	}
	m = autoModel(t)
	m.pricingState = data.PricingState{LastAttempt: now.Add(-time.Hour)}
	if cmd := m.maybeRefreshPricing(unpricedStats()); cmd != nil {
		t.Errorf("within the 24h cooldown: no retry")
	}
	m = autoModel(t)
	m.pricingState = data.PricingState{LastAttempt: now.Add(-25 * time.Hour)}
	if cmd := m.maybeRefreshPricing(unpricedStats()); cmd == nil {
		t.Errorf("cooldown over: retry allowed")
	}
}

func TestStatsMessagesTriggerRefresh(t *testing.T) {
	m := autoModel(t)
	r, cmd := m.Update(QuickStatsLoadedMsg{Stats: unpricedStats()})
	if cmd == nil || !r.(Model).pricingBusy {
		t.Errorf("quick stats with an unpriced model should start a refresh")
	}
	m = autoModel(t)
	r, cmd = m.Update(DashboardLoadedMsg{Data: data.DashboardData{UnpricedTokens: 5, UnpricedModels: []string{"x"}}})
	if cmd == nil || !r.(Model).pricingBusy {
		t.Errorf("dashboard with unpriced tokens should start a refresh")
	}
	m = autoModel(t)
	if _, cmd = m.Update(QuickStatsLoadedMsg{Stats: exactStats()}); cmd != nil {
		t.Errorf("fully priced stats must not start a refresh")
	}
}

// pricingServer serves the real pricing page excerpt plus one extra model.
func pricingServer(t *testing.T, extraModel bool, handler http.HandlerFunc) data.FetchOptions {
	t.Helper()
	raw, err := os.ReadFile("../data/testdata/pricing_page_excerpt.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	if extraModel {
		var out []string
		done := false
		for _, l := range strings.Split(page, "\n") {
			out = append(out, l)
			if !done && strings.HasPrefix(l, "| Claude Opus 5.5 ") && strings.Contains(l, "Base") == false && !strings.Contains(l, "USD") && !strings.Contains(l, "x ") {
				out = append(out, strings.Replace(l, "Claude Opus 5.5", "Claude Opus 5.6", 1))
				done = true
			}
		}
		page = strings.Join(out, "\n")
	}
	if handler == nil {
		handler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/markdown")
			w.Write([]byte(page))
		}
	}
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	return data.FetchOptions{URL: srv.URL + "/p.md", Client: srv.Client(), AllowHosts: []string{u.Host}}
}

func TestRefreshCmdAndMessage_EndToEnd(t *testing.T) {
	restoreBook(t)
	fo := pricingServer(t, true, nil)
	m := Model{opts: Options{PricingAuto: true, PricingCacheDir: t.TempDir(), PricingConfigDir: t.TempDir(), PricingFetch: fo}}
	m.selectedInfo = &data.SessionInfo{SessionID: "s", Project: "p"}
	cmd := m.maybeRefreshPricing(unpricedStats())
	if cmd == nil {
		t.Fatal("expected a refresh command")
	}
	msg := cmd().(PricingRefreshedMsg)
	if msg.Err != nil || !msg.Res.Applied {
		t.Fatalf("refresh failed: %+v", msg)
	}
	m.dashboardData = &data.DashboardData{}
	r, next := m.Update(msg)
	m = r.(Model)
	if _, ok := data.DefaultBook().Models["claude-opus-5-6"]; !ok {
		t.Errorf("the new price book must become the default")
	}
	if m.pricingBusy || m.dashboardData != nil || next == nil {
		t.Errorf("busy=%v dashboard cleared=%v recompute cmd nil=%v", m.pricingBusy, m.dashboardData == nil, next == nil)
	}
	if !strings.Contains(m.pricingNote, "updated") {
		t.Errorf("note = %q", m.pricingNote)
	}
	if m.pricingState.LastSuccess.IsZero() {
		t.Errorf("state must record the success so the cooldown applies")
	}
}

func TestPricingRefreshedMsg_Failures(t *testing.T) {
	restoreBook(t)
	before := data.DefaultBook()
	m := Model{pricingBusy: true}
	r, _ := m.Update(PricingRefreshedMsg{Err: context.DeadlineExceeded})
	m = r.(Model)
	if m.pricingBusy || !strings.Contains(m.pricingNote, "failed") || data.DefaultBook() != before {
		t.Errorf("failure must leave prices untouched: busy=%v note=%q", m.pricingBusy, m.pricingNote)
	}
	m = Model{pricingBusy: true}
	r, _ = m.Update(PricingRefreshedMsg{Err: data.ErrSuspiciousPrices})
	if n := r.(Model).pricingNote; !strings.Contains(n, "--update-pricing") {
		t.Errorf("suspicious changes must point the user at the review command: %q", n)
	}
	if data.DefaultBook() != before {
		t.Errorf("suspicious changes must not be applied")
	}
}

func TestIndexBar_PricingSegment(t *testing.T) {
	restoreBook(t)
	m := barModel(200)
	if bar := m.renderIndexBar(); !strings.Contains(bar, "Prices: "+data.DefaultBook().AsOf) {
		t.Errorf("the bar should say which price table is in use: %q", bar)
	}
	m.pricingBusy = true
	if bar := m.renderIndexBar(); !strings.Contains(bar, "Prices: updating") {
		t.Errorf("busy: %q", bar)
	}
	m.pricingBusy = false
	m.pricingNote, m.pricingNoteAt = "prices updated (2 changed)", time.Now()
	if bar := m.renderIndexBar(); !strings.Contains(bar, "prices updated (2 changed)") {
		t.Errorf("note: %q", bar)
	}
	m.pricingNoteAt = time.Now().Add(-time.Hour)
	if bar := m.renderIndexBar(); strings.Contains(bar, "prices updated") {
		t.Errorf("old notes must expire: %q", bar)
	}
	m.opts.PricingInfo = data.LayerInfo{CacheUsed: true, OverrideModels: 2, Problems: []string{"bad override"}}
	bar := m.renderIndexBar()
	for _, want := range []string{"fetched", "2 overrides", "1 problem"} {
		if !strings.Contains(bar, want) {
			t.Errorf("layer info %q missing: %q", want, bar)
		}
	}
}

var _ = tea.Quit
