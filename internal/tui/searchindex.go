package tui

import (
	"runtime"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/user/claudevu/internal/data"
)

// SearchIndexLoadedMsg carries changed or new index entries keyed by SessionID.
type SearchIndexLoadedMsg struct{ Entries map[string]data.IndexEntry }

// loadSearchIndexCmd builds content index entries in the background. prev is
// only read; the returned message contains only new or modified entries.
func loadSearchIndexCmd(sessions []data.SessionInfo, prev map[string]data.IndexEntry) tea.Cmd {
	// Snapshot inputs so nothing is shared with the model.
	work := make([]data.SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		if s.FilePath != "" {
			work = append(work, s)
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
		return SearchIndexLoadedMsg{Entries: out}
	}
}
