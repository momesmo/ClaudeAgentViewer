package tui

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/user/claudevu/internal/data"
)

// statusSeg is one segment of the index status bar. plains lists the text
// from most to least detailed; the renderer picks the longest that fits.
type statusSeg struct {
	plains []string
	style  lipgloss.Style
}

const statusSep = " │ "

// renderIndexBar renders the index status line (above the key-hint bar): cache on/off and location,
// index progress, save state, and the active filter. Segments are listed in
// priority order; lower-priority ones are dropped first on narrow terminals,
// so the bar never wraps.
func (m Model) renderIndexBar() string {
	avail := m.width - 2 // statusBarStyle has 1 col of padding on each side
	if avail < 1 {
		avail = 1
	}
	segs := m.indexSegments(avail)

	// Fit to width: first shrink segments to shorter variants, lowest priority
	// first; only when everything is at its shortest drop trailing segments.
	cur := make([]int, len(segs))
	n := len(segs)
	for segsWidth(segs[:n], cur) > avail {
		shrunk := false
		for i := n - 1; i >= 0; i-- {
			if cur[i] < len(segs[i].plains)-1 {
				cur[i]++
				shrunk = true
				break
			}
		}
		if !shrunk {
			if n == 1 {
				break
			}
			n--
		}
	}
	var parts []string
	for i, s := range segs[:n] {
		text := s.plains[cur[i]]
		if n == 1 {
			text = truncateRunes(text, avail) // last resort for very narrow terminals
		}
		parts = append(parts, s.style.Render(text))
	}
	sep := mutedStyle.Render(statusSep)
	return statusBarStyle.Width(m.width).Render(strings.Join(parts, sep))
}

func segsWidth(segs []statusSeg, variant []int) int {
	w := 0
	for i, s := range segs {
		if i > 0 {
			w += lipgloss.Width(statusSep)
		}
		w += lipgloss.Width(s.plains[variant[i]])
	}
	return w
}

func (m Model) indexSegments(avail int) []statusSeg {
	home, _ := os.UserHomeDir()
	var segs []statusSeg

	// 1. Cache on/off and where it lives.
	if m.opts.IndexCache {
		pathMax := avail / 3
		if pathMax < 16 {
			pathMax = 16
		}
		path := shortenPath(m.cacheDir, home, pathMax)
		full := "Cache ON  " + path
		if m.cacheSeeded > 0 {
			full += fmt.Sprintf(" · loaded %d", m.cacheSeeded)
		}
		segs = append(segs, statusSeg{
			plains: []string{full, "Cache ON  " + path, "Cache ON  " + shortenPath(m.cacheDir, home, 16), "Cache ON"},
			style:  userLabelStyle,
		})
	} else {
		segs = append(segs, statusSeg{plains: []string{"Cache off (memory only)", "Cache off"}, style: mutedStyle})
	}

	// 2. Index progress and size.
	indexed, total, bytes := m.indexCounts()
	count := plural(indexed, "session")
	if indexed < total {
		count = fmt.Sprintf("%d of %s", indexed, plural(total, "session"))
	}
	if m.indexJobs > 0 {
		segs = append(segs, statusSeg{
			plains: []string{"Index: indexing… (" + count + " so far)", "Index: indexing…", "Indexing…"},
			style:  toolLabelStyle,
		})
	} else {
		segs = append(segs, statusSeg{
			plains: []string{"Index: ready · " + count + " · " + formatBytes(int64(bytes)), "Index: ready · " + count, "Index: ready"},
			style:  assistantLabelStyle,
		})
	}

	// 3. Cache save state (only meaningful when the cache is on).
	if m.opts.IndexCache {
		switch {
		case m.saveDirty:
			segs = append(segs, statusSeg{plains: []string{"Save: queued (changes pending)", "Save: queued"}, style: toolLabelStyle})
		case m.saveInFlight:
			segs = append(segs, statusSeg{plains: []string{"Save: saving…"}, style: toolLabelStyle})
		case m.lastSaveErr != "":
			segs = append(segs, statusSeg{
				plains: []string{"Cache save failed: " + truncateRunes(m.lastSaveErr, 60), "Cache save failed"},
				style:  lipgloss.NewStyle().Bold(true).Foreground(colorError),
			})
		case !m.lastSaveAt.IsZero():
			at := m.lastSaveAt.Format("15:04:05")
			segs = append(segs, statusSeg{
				plains: []string{"Save: saved " + at + " · " + formatBytes(m.lastSaveSize), "Save: saved " + at, "Saved " + at, "Saved"},
				style:  userLabelStyle,
			})
		default:
			segs = append(segs, statusSeg{plains: []string{"Save: no save yet", "No save yet"}, style: mutedStyle})
		}
	}

	// 4. Active search filter and how many sessions it shows.
	if q := m.searchInput.Value(); q != "" {
		shown := 0
		for _, it := range m.listItems {
			if it.kind == itemSession {
				shown++
			}
		}
		segs = append(segs, statusSeg{
			plains: []string{fmt.Sprintf("Filter %q: %s", truncateRunes(q, 20), plural(shown, "session")), fmt.Sprintf("Filter: %d", shown)},
			style:  helpKeyStyle,
		})
	}

	// 5. Which prices costs are computed from (and a refresh in progress).
	segs = append(segs, m.pricingSegment())
	return segs
}

// pricingSegment describes the price table in use: its date, where it came from,
// any problems loading optional layers, and refresh activity.
func (m Model) pricingSegment() statusSeg {
	asOf := data.DefaultBook().AsOf
	info := m.opts.PricingInfo
	if m.pricingBusy {
		return statusSeg{plains: []string{"Prices: updating…", "Prices…"}, style: toolLabelStyle}
	}
	if m.pricingNote != "" && time.Since(m.pricingNoteAt) < 30*time.Second {
		return statusSeg{plains: []string{"Prices: " + m.pricingNote, m.pricingNote}, style: toolLabelStyle}
	}
	full := "Prices: " + asOf
	if info.CacheUsed {
		full += " (fetched)"
	}
	if info.OverrideModels > 0 {
		full += fmt.Sprintf(" +%s", plural(info.OverrideModels, "override"))
	}
	style := mutedStyle
	plains := []string{full, "Prices: " + asOf}
	if n := len(info.Problems); n > 0 {
		full += " ⚠ " + plural(n, "problem")
		plains = []string{full, "Prices ⚠"}
		style = toolLabelStyle
	}
	return statusSeg{plains: plains, style: style}
}

// indexCounts returns how many known sessions have an index entry, how many
// have a transcript file at all, and the total blob bytes held for them.
func (m Model) indexCounts() (indexed, total, bytes int) {
	for _, g := range m.groups {
		for _, s := range g.Sessions {
			if s.FilePath == "" {
				continue
			}
			total++
			if e, ok := m.searchIndex[s.SessionID]; ok {
				indexed++
				bytes += len(e.Blob)
			}
		}
	}
	return
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func formatBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%d KB", n/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

// shortenPath replaces a leading home dir with ~ and, if still wider than
// max columns, keeps only the trailing path elements that fit, prefixed "…/".
func shortenPath(p, home string, max int) string {
	if p == "" {
		return ""
	}
	if home != "" && (p == home || strings.HasPrefix(p, home+"/")) {
		p = "~" + strings.TrimPrefix(p, home)
	}
	if lipgloss.Width(p) <= max {
		return p
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	best := ""
	for k := 1; k <= len(parts); k++ {
		cand := "…/" + strings.Join(parts[len(parts)-k:], "/")
		if lipgloss.Width(cand) > max {
			break
		}
		best = cand
	}
	if best == "" {
		return truncateRunes(p, max)
	}
	return best
}

// truncateRunes cuts s to at most max display columns, ending in "…" if cut.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > max {
		r = r[:len(r)-1]
	}
	if len(r) == 0 && utf8.RuneCountInString(s) > 0 {
		return "…"
	}
	return string(r) + "…"
}

// saveDoneInfo records the outcome of a finished cache save.
func (m *Model) recordSave(err error, size int64, at time.Time) {
	m.lastSaveAt = at
	if err != nil {
		m.lastSaveErr = err.Error()
		return
	}
	m.lastSaveErr = ""
	m.lastSaveSize = size
}
