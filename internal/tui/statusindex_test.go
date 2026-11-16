package tui

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/user/claudevu/internal/data"
)

func barModel(width int) Model {
	s := data.SessionInfo{SessionID: "s1", Project: "p", Display: "hello", FilePath: "/x"}
	s.FillSearchMeta()
	m := Model{
		width: width, height: 30,
		groups:      []data.ProjectGroup{{Project: "p", Expanded: true, Sessions: []data.SessionInfo{s}}},
		searchIndex: map[string]data.IndexEntry{"s1": {Blob: "hello world"}},
	}
	m.buildListItems("")
	return m
}

func TestIndexBar_CacheOff(t *testing.T) {
	bar := barModel(120).renderIndexBar()
	if !strings.Contains(bar, "Cache off") || !strings.Contains(bar, "memory") {
		t.Errorf("want cache-off indicator, got %q", bar)
	}
	if strings.Contains(bar, "Save") {
		t.Errorf("save status must be hidden when cache is off: %q", bar)
	}
}

func TestIndexBar_CacheOnShowsPathAndSeeded(t *testing.T) {
	home := os.Getenv("HOME") // isolated by TestMain; only a path prefix here
	m := barModel(160)
	m.opts.IndexCache = true
	m.cacheDir = home + "/Library/Caches/claudevu"
	m.cacheSeeded = 3
	bar := m.renderIndexBar()
	for _, w := range []string{"Cache ON", "~/Library/Caches/claudevu", "loaded 3"} {
		if !strings.Contains(bar, w) {
			t.Errorf("bar missing %q: %q", w, bar)
		}
	}
}

func TestIndexBar_IndexingThenReady(t *testing.T) {
	m := barModel(160)
	m.indexJobs = 1
	if bar := m.renderIndexBar(); !strings.Contains(bar, "indexing") {
		t.Errorf("want indexing state: %q", bar)
	}
	r, _ := m.Update(SearchIndexLoadedMsg{Entries: map[string]data.IndexEntry{"s1": {Blob: "hello world"}}})
	m = r.(Model)
	if m.indexJobs != 0 {
		t.Errorf("indexJobs = %d, want 0", m.indexJobs)
	}
	bar := m.renderIndexBar()
	if !strings.Contains(bar, "ready") || !strings.Contains(bar, "1 session") {
		t.Errorf("want ready with session count: %q", bar)
	}
	// An extra, unmatched completion must not underflow.
	r, _ = m.Update(SearchIndexLoadedMsg{})
	if r.(Model).indexJobs != 0 {
		t.Errorf("indexJobs went negative")
	}
}

func TestIndexBar_HistoryReloadCountsAsIndexing(t *testing.T) {
	m := barModel(160)
	r, cmd := m.Update(historyChangedMsg{})
	if cmd == nil || r.(Model).indexJobs != 1 {
		t.Errorf("reindex should start a counted job; jobs=%d cmd nil=%v", r.(Model).indexJobs, cmd == nil)
	}
}

func TestIndexBar_SaveStates(t *testing.T) {
	m := barModel(200)
	m.opts.IndexCache = true
	m.cacheDir = "/tmp/c"
	if bar := m.renderIndexBar(); !strings.Contains(bar, "no save yet") {
		t.Errorf("initial: %q", bar)
	}
	m.saveInFlight = true
	if bar := m.renderIndexBar(); !strings.Contains(bar, "saving") {
		t.Errorf("in flight: %q", bar)
	}
	m.saveDirty = true
	if bar := m.renderIndexBar(); !strings.Contains(bar, "queued") {
		t.Errorf("dirty: %q", bar)
	}

	at := time.Date(2026, 11, 13, 14, 3, 41, 0, time.Local)
	r, _ := m.Update(IndexSaveDoneMsg{Size: 800 * 1024, At: at})
	m = r.(Model)
	m.saveInFlight, m.saveDirty = false, false // the dirty follow-up save is covered elsewhere
	bar := m.renderIndexBar()
	if !strings.Contains(bar, "saved 14:03:41") || !strings.Contains(bar, "800 KB") {
		t.Errorf("after save: %q", bar)
	}

	m.saveInFlight, m.saveDirty = false, false
	r, _ = m.Update(IndexSaveDoneMsg{Err: os.ErrPermission, At: at})
	m = r.(Model)
	if bar := m.renderIndexBar(); !strings.Contains(bar, "save failed") {
		t.Errorf("after failure: %q", bar)
	}
}

func TestIndexBar_FilterCount(t *testing.T) {
	m := barModel(200)
	m.searchInput.SetValue("hello")
	m.buildListItems("hello")
	bar := m.renderIndexBar()
	if !strings.Contains(bar, `Filter "hello"`) || !strings.Contains(bar, "1 session") {
		t.Errorf("want filter segment: %q", bar)
	}
	m.searchInput.SetValue("")
	if strings.Contains(m.renderIndexBar(), "Filter") {
		t.Errorf("filter segment must vanish when no filter")
	}
}

func TestIndexBar_NarrowWidthNeverWraps(t *testing.T) {
	home := os.Getenv("HOME") // isolated by TestMain; only a path prefix here
	for _, w := range []int{20, 40, 60, 80, 120} {
		m := barModel(w)
		m.opts.IndexCache = true
		m.cacheDir = home + "/a/very/long/cache/directory/path/that/keeps/going/claudevu"
		m.saveInFlight = true
		m.searchInput.SetValue("hello")
		bar := m.renderIndexBar()
		if strings.Contains(bar, "\n") || lipgloss.Width(bar) > w {
			t.Errorf("width %d: bar wraps or overflows (%d cols): %q", w, lipgloss.Width(bar), bar)
		}
		if w >= 40 && !strings.Contains(bar, "Cache ON") {
			t.Errorf("width %d: cache state (highest priority) was dropped: %q", w, bar)
		}
	}
}

func TestRenderMain_ShowsBothBarsAndFits(t *testing.T) {
	m := barModel(120)
	m.height = 30
	lines := strings.Split(m.View(), "\n")
	if len(lines) > m.height {
		t.Errorf("view is %d lines tall, terminal is %d", len(lines), m.height)
	}
	// Order, top to bottom: panes, index bar, keys bar (keys bar stays the last line).
	last := lines[len(lines)-1]
	prev := lines[len(lines)-2]
	if !strings.Contains(last, "Navigate") || !strings.Contains(prev, "Cache") {
		t.Errorf("want index bar above keys bar:\n%q\n%q", prev, last)
	}
}

func TestShortenPath(t *testing.T) {
	home := "/Users/a"
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"/Users/a/Library/Caches/claudevu", 80, "~/Library/Caches/claudevu"},
		{"/var/tmp/x", 80, "/var/tmp/x"},
		{"/Users/a/Library/Caches/claudevu", 12, "…/claudevu"},
		{"", 10, ""},
	}
	for _, c := range cases {
		got := shortenPath(c.in, home, c.max)
		if got != c.want {
			t.Errorf("shortenPath(%q,%d) = %q, want %q", c.in, c.max, got, c.want)
		}
		if lipgloss.Width(got) > c.max && c.max > 0 {
			t.Errorf("shortenPath(%q,%d) too wide: %q", c.in, c.max, got)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{0: "0 B", 512: "512 B", 800 * 1024: "800 KB", 3 * 1024 * 1024 / 2: "1.5 MB"}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

// The pane height and ensureListVisible must agree, or the cursor row can
// scroll out of view at the bottom of the list.
func TestListCursorStaysVisibleWithIndexBar(t *testing.T) {
	var sessions []data.SessionInfo
	for i := 0; i < 60; i++ {
		s := data.SessionInfo{SessionID: fmt.Sprintf("id%02d", i), Project: "p", Display: fmt.Sprintf("prompt-%02d", i), FilePath: "/x"}
		s.FillSearchMeta()
		sessions = append(sessions, s)
	}
	for _, h := range []int{12, 20, 30, 50} {
		m := Model{width: 120, height: h, focus: focusList,
			groups: []data.ProjectGroup{{Project: "p", Expanded: true, Sessions: sessions}}}
		m.buildListItems("")
		for _, cur := range []int{0, 1, 30, len(m.listItems) - 1} {
			m.listCursor = cur
			m.ensureListVisible()
			if lines := strings.Split(m.View(), "\n"); len(lines) > h {
				t.Errorf("height %d cursor %d: view has %d lines", h, cur, len(lines))
			}
			if cur > 0 && !strings.Contains(m.View(), fmt.Sprintf("prompt-%02d", cur-1)) {
				t.Errorf("height %d: cursor row prompt-%02d not visible", h, cur-1)
			}
		}
	}
}

func lipglossWidth(s string) int { return lipgloss.Width(s) }
