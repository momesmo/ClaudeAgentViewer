package tui

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/user/claudevu/internal/data"
)

// SearchIndexLoadedMsg carries changed or new index entries keyed by SessionID,
// and the IDs of sessions whose transcript no longer exists (their entries must
// be dropped so deleted sessions stop matching).
type SearchIndexLoadedMsg struct {
	Entries map[string]data.IndexEntry
	Removed []string
}

// loadSearchIndexCmd builds content index entries in the background. prev is
// only read; the returned message contains only new or modified entries.
func loadSearchIndexCmd(sessions []data.SessionInfo, prev map[string]data.IndexEntry) tea.Cmd {
	// Snapshot inputs so nothing is shared with the model.
	work := make([]data.SessionInfo, 0, len(sessions))
	var removed []string
	for _, s := range sessions {
		if s.FilePath != "" {
			work = append(work, s)
		} else if _, had := prev[s.SessionID]; had {
			removed = append(removed, s.SessionID) // file can no longer be located
		}
	}
	prevCopy := make(map[string]data.IndexEntry, len(prev))
	for k, v := range prev {
		prevCopy[k] = v
	}
	return func() tea.Msg {
		out := make(map[string]data.IndexEntry)
		var mu sync.Mutex
		var wg sync.WaitGroup
		ch := make(chan data.SessionInfo)
		for i := 0; i < runtime.NumCPU(); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for s := range ch {
					var p *data.IndexEntry
					if e, ok := prevCopy[s.SessionID]; ok {
						p = &e
					}
					if _, err := os.Stat(s.FilePath); os.IsNotExist(err) {
						if p != nil { // transcript deleted since it was indexed
							mu.Lock()
							removed = append(removed, s.SessionID)
							mu.Unlock()
						}
						continue
					}
					e, changed := data.BuildIndexEntry(s.FilePath, p)
					if changed {
						mu.Lock()
						out[s.SessionID] = e
						mu.Unlock()
					}
				}
			}()
		}
		for _, s := range work {
			ch <- s
		}
		close(ch)
		wg.Wait()
		return SearchIndexLoadedMsg{Entries: out, Removed: removed}
	}
}

// renderSearchMatch explains why the selected session matches the active
// filter: which metadata fields matched and a few snippets from the conversation.
func (m Model) renderSearchMatch(width int) string {
	q := m.searchInput.Value()
	f := strings.ToLower(q)
	var sb strings.Builder
	fit := func(line string) string { return truncateRunes(line, width-2) } // never wider than the pane
	sb.WriteString(toolLabelStyle.Render(fit("  Search "+fmt.Sprintf("%q", truncateRunes(q, 24)))) + "\n")

	fields := data.MatchedFields(*m.selectedInfo, f)
	if len(fields) > 0 {
		sb.WriteString(fit("    Matched in: "+strings.Join(fields, ", ")) + "\n")
	}

	const indent = 6
	avail := width - indent - 2
	if avail < 6 {
		avail = 6
	}
	matchDisp := truncateRunes(f, avail)
	ctx := (avail-lipgloss.Width(matchDisp))/2 - 1
	if ctx < 0 {
		ctx = 0
	}
	blob := m.searchIndex[m.selectedInfo.SessionID].Blob
	snips, total := data.FindSnippets(blob, f, 3, ctx)
	switch {
	case total > 0:
		word := "matches"
		if total == 1 {
			word = "match"
		}
		more := ""
		if total >= 9999 {
			more = "+"
		}
		line := fmt.Sprintf("    Content: %d%s %s (shown lowercased)", total, more, word)
		if lipgloss.Width(line) > width-2 {
			line = fmt.Sprintf("    Content: %d%s %s", total, more, word)
		}
		sb.WriteString(fit(line) + "\n")
		for _, sn := range snips {
			before, after := sn.Before, sn.After
			for lipgloss.Width(before)+lipgloss.Width(matchDisp)+lipgloss.Width(after) > avail && (before != "" || after != "") {
				if lipgloss.Width(before) >= lipgloss.Width(after) {
					before = trimFirstRune(before)
				} else {
					after = trimLastRune(after)
				}
			}
			sb.WriteString("      " + mutedStyle.Render(before) + toolLabelStyle.Render(matchDisp) + mutedStyle.Render(after) + "\n")
		}
	case len(fields) == 0:
		note := "no match in the indexed text"
		if m.indexJobs > 0 {
			note = "index still building"
		}
		sb.WriteString(mutedStyle.Render(fit("    ("+note+")")) + "\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

func trimFirstRune(s string) string {
	_, n := utf8.DecodeRuneInString(s)
	return s[n:]
}

func trimLastRune(s string) string {
	_, n := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-n]
}
