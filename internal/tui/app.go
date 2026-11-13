package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/user/claudevu/internal/data"
)

type focus int

const (
	focusList focus = iota
	focusDetail
)

type view int

const (
	viewMain view = iota
	viewHelp
	viewDashboard
)

// listItem is a row in the 3-level tree: project / session / subagent
type listItem struct {
	kind         itemKind
	projectIndex int
	sessionIndex int
	subagentIdx  int // index into sessionSubagents map
	matchKind    data.MatchKind
}

type itemKind int

const (
	itemProject  itemKind = iota
	itemSession
	itemSubagent
)

// sessionKey uniquely identifies a session for the subagent map
type sessionKey struct {
	pi, si int
}

type Model struct {
	groups    []data.ProjectGroup
	claudeDir string
	width     int
	height    int
	focus     focus
	currentView  view
	showThinking bool
	privacyMode  bool
	searching    bool
	searchInput  textinput.Model

	// List navigation
	listItems  []listItem
	listCursor int
	listOffset int

	// Session expansion (tracks which sessions show their subagents)
	expandedSessions map[sessionKey]bool
	// Subagents per session (loaded on expand)
	sessionSubagents map[sessionKey][]data.SubagentInfo

	// Detail pane
	transcript   []data.SessionMessage
	stats        data.SessionStats
	selectedInfo *data.SessionInfo    // set when viewing a session
	selectedSub  *data.SubagentInfo   // set when viewing a subagent
	detailScroll int
	loading      bool   // true while transcript loads async
	showLogs     bool   // false = metadata view, true = transcript view

	// Confirm resume
	confirmResume bool

	// Status message
	statusMsg     string
	statusExpires time.Time

	// Live refresh
	historyModTime  time.Time
	sessionFilePath string
	sessionModTime  time.Time
	liveWatching    bool

	// Flagged sessions for monitoring
	flaggedSessions map[string]bool      // sessionID -> true
	flaggedStats    map[string]data.SessionStats // sessionID -> latest stats

	// Dashboard
	dashboardData    *data.DashboardData
	dashboardLoading bool
	dashboardScroll  int

	// Search index (content blobs keyed by session ID)
	searchIndex map[string]data.IndexEntry

	// Resume
	ResumeSessionID  string
	ResumeProjectDir string

	err error
}

// Async messages (exported for testing)
type QuickStatsLoadedMsg struct {
	Stats data.SessionStats
	Path  string
}

type TranscriptLoadedMsg struct {
	Messages []data.SessionMessage
	Stats    data.SessionStats
	Path     string
}

type DashboardLoadedMsg struct {
	Data data.DashboardData
}

type FlaggedRefreshMsg struct {
	Stats map[string]data.SessionStats // sessionID -> latest stats
}

type SubagentTranscriptLoadedMsg struct {
	AgentID  string
	Messages []data.SessionMessage
	Stats    data.SessionStats
}

func NewModel(claudeDir string) Model {
	ti := textinput.New()
	ti.Placeholder = "Search sessions..."
	ti.CharLimit = 100

	m := Model{
		claudeDir:        claudeDir,
		searchIndex:      make(map[string]data.IndexEntry),
		showThinking:     false,
		privacyMode:      true,
		searchInput:      ti,
		expandedSessions: make(map[sessionKey]bool),
		sessionSubagents: make(map[sessionKey][]data.SubagentInfo),
		flaggedSessions:  make(map[string]bool),
		flaggedStats:     make(map[string]data.SessionStats),
	}

	groups, err := data.LoadHistory(claudeDir)
	if err != nil {
		m.err = err
		return m
	}
	m.groups = groups
	m.buildListItems("")

	histPath := filepath.Join(claudeDir, "history.jsonl")
	if info, err := os.Stat(histPath); err == nil {
		m.historyModTime = info.ModTime()
	}

	return m
}

func (m *Model) buildListItems(filter string) {
	filter = strings.ToLower(filter)
	m.listItems = nil
	for pi, g := range m.groups {
		kinds := make([]data.MatchKind, len(g.Sessions))
		hasMatch := filter == ""
		for si, s := range g.Sessions {
			kinds[si] = data.MatchSession(s, m.searchIndex[s.SessionID].Blob, filter)
			if kinds[si] != data.MatchNone {
				hasMatch = true
			}
		}
		if !hasMatch {
			continue
		}

		m.listItems = append(m.listItems, listItem{kind: itemProject, projectIndex: pi})
		if g.Expanded {
			for si := range g.Sessions {
				if kinds[si] == data.MatchNone {
					continue
				}
				m.listItems = append(m.listItems, listItem{kind: itemSession, projectIndex: pi, sessionIndex: si, matchKind: kinds[si]})

				// If session is expanded, show its subagents
				sk := sessionKey{pi, si}
				if m.expandedSessions[sk] {
					if subs, ok := m.sessionSubagents[sk]; ok {
						for sai := range subs {
							m.listItems = append(m.listItems, listItem{
								kind: itemSubagent, projectIndex: pi, sessionIndex: si, subagentIdx: sai,
							})
						}
					}
				}
			}
		}
	}
}

func (m *Model) setStatus(msg string) {
	m.statusMsg = msg
	m.statusExpires = time.Now().Add(3 * time.Second)
}

// refreshFlaggedCmd refreshes stats for all flagged sessions in one batch
func refreshFlaggedCmd(claudeDir string, flagged map[string]bool, groups []data.ProjectGroup) tea.Cmd {
	if len(flagged) == 0 {
		return nil
	}
	// Capture the session IDs and their projects
	type target struct {
		sessionID string
		project   string
	}
	var targets []target
	for _, g := range groups {
		for _, s := range g.Sessions {
			if flagged[s.SessionID] {
				targets = append(targets, target{s.SessionID, s.Project})
			}
		}
	}
	if len(targets) == 0 {
		return nil
	}

	return func() tea.Msg {
		result := make(map[string]data.SessionStats)
		for _, t := range targets {
			path := data.FindSessionFile(claudeDir, t.project, t.sessionID)
			if path != "" {
				stats, err := data.QuickScanStats(path)
				if err == nil {
					result[t.sessionID] = stats
				}
			}
		}
		return FlaggedRefreshMsg{Stats: result}
	}
}

// scheduleNextFlaggedRefresh returns a tick that triggers the next refresh
func scheduleNextFlaggedRefresh() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return flaggedTickMsg{}
	})
}

type flaggedTickMsg struct{}

// loadDashboardCmd scans all sessions for quick stats and builds the dashboard
func loadDashboardCmd(claudeDir string, groups []data.ProjectGroup) tea.Cmd {
	return func() tea.Msg {
		statsMap := make(map[string]data.SessionStats)
		for _, g := range groups {
			for _, s := range g.Sessions {
				path := data.FindSessionFile(claudeDir, s.Project, s.SessionID)
				if path == "" {
					continue
				}
				stats, err := data.QuickScanStats(path)
				if err == nil {
					statsMap[s.SessionID] = stats
				}
			}
		}
		d := data.BuildDashboard(groups, statsMap)
		return DashboardLoadedMsg{Data: d}
	}
}

// loadQuickStatsCmd loads just token counts (fast, no message parsing)
func loadQuickStatsCmd(claudeDir, project, sessionID string) tea.Cmd {
	return func() tea.Msg {
		path := data.FindSessionFile(claudeDir, project, sessionID)
		if path == "" {
			return QuickStatsLoadedMsg{}
		}
		stats, _ := data.QuickScanStats(path)
		return QuickStatsLoadedMsg{Stats: stats, Path: path}
	}
}

// loadTranscriptCmd loads a transcript asynchronously
func loadTranscriptCmd(claudeDir, project, sessionID string) tea.Cmd {
	return func() tea.Msg {
		path := data.FindSessionFile(claudeDir, project, sessionID)
		if path == "" {
			return TranscriptLoadedMsg{}
		}
		messages, err := data.LoadTranscript(path)
		if err != nil {
			return TranscriptLoadedMsg{Path: path}
		}
		stats := data.ComputeStats(messages)
		return TranscriptLoadedMsg{Messages: messages, Stats: stats, Path: path}
	}
}

// loadSubagentTranscriptCmd loads a subagent transcript asynchronously
func loadSubagentTranscriptCmd(sa data.SubagentInfo) tea.Cmd {
	return func() tea.Msg {
		messages, err := data.LoadTranscript(sa.FilePath)
		if err != nil {
			return SubagentTranscriptLoadedMsg{AgentID: sa.AgentID}
		}
		stats := data.ComputeStats(messages)
		return SubagentTranscriptLoadedMsg{AgentID: sa.AgentID, Messages: messages, Stats: stats}
	}
}

// reindexCmd indexes all known sessions, passing the current index as prev.
func (m Model) reindexCmd() tea.Cmd {
	var sessions []data.SessionInfo
	for _, g := range m.groups {
		sessions = append(sessions, g.Sessions...)
	}
	return loadSearchIndexCmd(sessions, m.searchIndex)
}

func (m Model) Init() tea.Cmd {
	// No polling — Ctrl+R to refresh, l for live logs. Index content in background.
	return m.reindexCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case QuickStatsLoadedMsg:
		m.loading = false
		m.stats = msg.Stats
		if msg.Path != "" {
			m.sessionFilePath = msg.Path
		}
		return m, nil

	case TranscriptLoadedMsg:
		m.loading = false
		m.showLogs = true
		m.transcript = msg.Messages
		m.stats = msg.Stats
		m.detailScroll = 0
		if msg.Path != "" {
			m.sessionFilePath = msg.Path
			if fi, err := os.Stat(msg.Path); err == nil {
				m.sessionModTime = fi.ModTime()
			}
		}
		// If viewing logs, start live tailing
		if m.liveWatching {
			return m, watchSessionCmd(m.sessionFilePath, m.sessionModTime)
		}
		return m, nil

	case SubagentTranscriptLoadedMsg:
		m.loading = false
		m.showLogs = true
		m.transcript = msg.Messages
		m.stats = msg.Stats
		m.detailScroll = 0
		for sk, subs := range m.sessionSubagents {
			for i := range subs {
				if subs[i].AgentID == msg.AgentID {
					subs[i].Messages = msg.Messages
					subs[i].Stats = msg.Stats
					subs[i].Loaded = true
					m.sessionSubagents[sk] = subs
					break
				}
			}
		}
		return m, nil

	case SearchIndexLoadedMsg:
		if m.searchIndex == nil {
			m.searchIndex = make(map[string]data.IndexEntry)
		}
		for id, e := range msg.Entries {
			m.searchIndex[id] = e
		}
		m.buildListItems(m.searchInput.Value())
		return m, nil

	case historyChangedMsg:
		return m, m.reindexCmd()

	case sessionChangedMsg:
		// Live tailing: reload transcript when file changes
		if m.showLogs && m.selectedInfo != nil && m.sessionFilePath == msg.path {
			return m, tea.Batch(
				loadTranscriptCmd(m.claudeDir, m.selectedInfo.Project, m.selectedInfo.SessionID),
				loadSearchIndexCmd([]data.SessionInfo{*m.selectedInfo}, m.searchIndex),
			)
		}
		// Keep watching
		if m.liveWatching {
			return m, watchSessionCmd(m.sessionFilePath, m.sessionModTime)
		}
		return m, nil

	case tickMsg:
		// Only active during live watching
		if m.liveWatching {
			return m, watchSessionCmd(m.sessionFilePath, m.sessionModTime)
		}
		return m, nil

	case flaggedTickMsg:
		// Refresh all flagged sessions' stats
		if len(m.flaggedSessions) > 0 {
			return m, refreshFlaggedCmd(m.claudeDir, m.flaggedSessions, m.groups)
		}
		return m, nil

	case DashboardLoadedMsg:
		m.dashboardLoading = false
		d := msg.Data
		m.dashboardData = &d
		return m, nil

	case FlaggedRefreshMsg:
		// Update cached stats for flagged sessions
		for sid, stats := range msg.Stats {
			m.flaggedStats[sid] = stats
		}
		// Update the selected session's stats if it's flagged
		if m.selectedInfo != nil && m.flaggedSessions[m.selectedInfo.SessionID] {
			if stats, ok := msg.Stats[m.selectedInfo.SessionID]; ok {
				m.stats = stats
			}
		}
		// Refresh subagent statuses for expanded flagged sessions
		for sk := range m.expandedSessions {
			sid := m.groups[sk.pi].Sessions[sk.si].SessionID
			if m.flaggedSessions[sid] {
				if subs, ok := m.sessionSubagents[sk]; ok {
					m.sessionSubagents[sk] = data.RefreshSubagentStatus(m.claudeDir, sid, subs)
				}
			}
		}
		// Rebuild list to reflect updated stats/status
		m.buildListItems(m.searchInput.Value())
		// Schedule next refresh if we still have flagged sessions
		if len(m.flaggedSessions) > 0 {
			return m, scheduleNextFlaggedRefresh()
		}
		return m, nil

	case tea.KeyMsg:
		if m.searching {
			switch msg.String() {
			case "esc":
				m.searching = false
				m.searchInput.Blur()
				m.searchInput.SetValue("")
				m.buildListItems("")
				return m, nil
			case "enter":
				m.searching = false
				m.searchInput.Blur()
				return m, nil
			default:
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				m.buildListItems(m.searchInput.Value())
				m.listCursor = 0
				m.listOffset = 0
				return m, cmd
			}
		}

		if m.confirmResume {
			switch msg.String() {
			case "y", "Y":
				m.confirmResume = false
				if m.selectedInfo != nil {
					m.ResumeSessionID = m.selectedInfo.SessionID
					m.ResumeProjectDir = m.selectedInfo.Project
					return m, tea.Quit
				}
			default:
				m.confirmResume = false
			}
			return m, nil
		}

		if m.currentView == viewHelp {
			if msg.String() == "esc" || msg.String() == "?" || msg.String() == "q" {
				m.currentView = viewMain
			}
			return m, nil
		}

		if m.currentView == viewDashboard {
			switch {
			case msg.String() == "esc" || msg.String() == "d" || msg.String() == "q":
				m.currentView = viewMain
			case key.Matches(msg, keys.Up):
				if m.dashboardScroll > 0 {
					m.dashboardScroll--
				}
			case key.Matches(msg, keys.Down):
				m.dashboardScroll++
			case key.Matches(msg, keys.PageUp):
				m.dashboardScroll -= 20
				if m.dashboardScroll < 0 {
					m.dashboardScroll = 0
				}
			case key.Matches(msg, keys.PageDown):
				m.dashboardScroll += 20
			}
			return m, nil
		}

		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.Help):
			m.currentView = viewHelp
			return m, nil

		case key.Matches(msg, keys.Dashboard):
			m.currentView = viewDashboard
			m.dashboardScroll = 0
			if m.dashboardData == nil {
				m.dashboardLoading = true
				return m, loadDashboardCmd(m.claudeDir, m.groups)
			}
			return m, nil

		case key.Matches(msg, keys.Search):
			m.searching = true
			m.searchInput.Focus()
			return m, m.searchInput.Focus()

		case key.Matches(msg, keys.Tab):
			if m.focus == focusList {
				m.focus = focusDetail
			} else {
				m.focus = focusList
			}
			return m, nil

		case key.Matches(msg, keys.ShiftTab):
			if m.focus == focusDetail {
				m.focus = focusList
			} else {
				m.focus = focusDetail
			}
			return m, nil

		case key.Matches(msg, keys.Toggle):
			m.showThinking = !m.showThinking
			return m, nil

		case key.Matches(msg, keys.Privacy):
			m.privacyMode = !m.privacyMode
			if m.privacyMode {
				m.setStatus("Privacy ON")
			} else {
				m.setStatus("Privacy OFF")
			}
			return m, nil

		case key.Matches(msg, keys.Logs):
			if m.showLogs {
				// Hide logs, stop live watching
				m.showLogs = false
				m.liveWatching = false
				m.transcript = nil
				return m, nil
			}
			if m.selectedSub != nil {
				if m.selectedSub.Loaded {
					m.transcript = m.selectedSub.Messages
					m.stats = m.selectedSub.Stats
					m.showLogs = true
					m.liveWatching = false
					m.detailScroll = 0
				} else {
					m.loading = true
					return m, loadSubagentTranscriptCmd(*m.selectedSub)
				}
			} else if m.selectedInfo != nil {
				m.loading = true
				m.liveWatching = true // enable live tailing for this session
				m.setStatus("Live tailing enabled")
				return m, loadTranscriptCmd(m.claudeDir, m.selectedInfo.Project, m.selectedInfo.SessionID)
			}
			return m, nil

		case key.Matches(msg, keys.Flag):
			// Toggle flag on the currently selected session
			if m.listCursor < len(m.listItems) {
				item := m.listItems[m.listCursor]
				var sid string
				if item.kind == itemSession {
					sid = m.groups[item.projectIndex].Sessions[item.sessionIndex].SessionID
				} else if item.kind == itemSubagent {
					// Flag the parent session
					sid = m.groups[item.projectIndex].Sessions[item.sessionIndex].SessionID
				}
				if sid != "" {
					if m.flaggedSessions[sid] {
						delete(m.flaggedSessions, sid)
						delete(m.flaggedStats, sid)
						m.setStatus("Unflagged session")
						// Stop polling if no more flagged
					} else {
						m.flaggedSessions[sid] = true
						m.setStatus("Flagged for monitoring")
						// Start the refresh cycle
						return m, scheduleNextFlaggedRefresh()
					}
				}
			}
			return m, nil

		case key.Matches(msg, keys.Refresh):
			// Save UI state
			savedCursor := m.listCursor
			savedOffset := m.listOffset
			oldExpanded := make(map[string]bool)
			for _, g := range m.groups {
				if g.Expanded {
					oldExpanded[g.Project] = true
				}
			}

			groups, err := data.LoadHistory(m.claudeDir)
			if err == nil {
				// Restore project expansion state
				for i := range groups {
					if oldExpanded[groups[i].Project] {
						groups[i].Expanded = true
					}
				}
				m.groups = groups
				m.buildListItems(m.searchInput.Value())

				// Restore cursor (clamp to new list length)
				if savedCursor < len(m.listItems) {
					m.listCursor = savedCursor
				} else if len(m.listItems) > 0 {
					m.listCursor = len(m.listItems) - 1
				}
				m.listOffset = savedOffset
				m.ensureListVisible()
			}
			m.setStatus("Refreshed")
			// Also refresh selected session stats async
			if m.selectedInfo != nil {
				return m, tea.Batch(loadQuickStatsCmd(m.claudeDir, m.selectedInfo.Project, m.selectedInfo.SessionID), m.reindexCmd())
			}
			return m, m.reindexCmd()

		case key.Matches(msg, keys.Copy):
			if m.selectedInfo != nil {
				copyToClipboard(m.selectedInfo.SessionID)
				m.setStatus("Copied session ID")
			}
			return m, nil

		case key.Matches(msg, keys.Resume):
			if m.selectedInfo != nil {
				m.confirmResume = true
			}
			return m, nil

		case key.Matches(msg, keys.Up):
			if m.focus == focusList {
				if m.listCursor > 0 {
					m.listCursor--
					m.ensureListVisible()
				}
			} else {
				if m.detailScroll > 0 {
					m.detailScroll--
				}
			}
			return m, nil

		case key.Matches(msg, keys.Down):
			if m.focus == focusList {
				if m.listCursor < len(m.listItems)-1 {
					m.listCursor++
					m.ensureListVisible()
				}
			} else {
				m.detailScroll++
			}
			return m, nil

		case key.Matches(msg, keys.Right):
			// Expand current item in list
			if m.focus == focusList && m.listCursor < len(m.listItems) {
				item := m.listItems[m.listCursor]
				switch item.kind {
				case itemProject:
					if !m.groups[item.projectIndex].Expanded {
						m.groups[item.projectIndex].Expanded = true
						m.buildListItems(m.searchInput.Value())
					}
				case itemSession:
					sk := sessionKey{item.projectIndex, item.sessionIndex}
					if !m.expandedSessions[sk] {
						info := &m.groups[item.projectIndex].Sessions[item.sessionIndex]
						m.selectedInfo = info
						m.selectedSub = nil
						m.showLogs = false
						m.liveWatching = false
						m.transcript = nil
						m.loading = true
						m.expandedSessions[sk] = true
						subs, _ := data.ScanSubagents(m.claudeDir, info.SessionID)
						m.sessionSubagents[sk] = subs
						m.buildListItems(m.searchInput.Value())
						return m, loadQuickStatsCmd(m.claudeDir, info.Project, info.SessionID)
					}
				}
			}
			return m, nil

		case key.Matches(msg, keys.Left):
			// Collapse current item in list, or go to parent
			if m.focus == focusList && m.listCursor < len(m.listItems) {
				item := m.listItems[m.listCursor]
				switch item.kind {
				case itemProject:
					if m.groups[item.projectIndex].Expanded {
						m.groups[item.projectIndex].Expanded = false
						m.buildListItems(m.searchInput.Value())
					}
				case itemSession:
					sk := sessionKey{item.projectIndex, item.sessionIndex}
					if m.expandedSessions[sk] {
						// Collapse session (hide subagents)
						m.expandedSessions[sk] = false
						m.buildListItems(m.searchInput.Value())
					} else {
						// Already collapsed — jump to parent project
						for i := m.listCursor - 1; i >= 0; i-- {
							if m.listItems[i].kind == itemProject && m.listItems[i].projectIndex == item.projectIndex {
								m.listCursor = i
								m.ensureListVisible()
								break
							}
						}
					}
				case itemSubagent:
					// Jump to parent session
					for i := m.listCursor - 1; i >= 0; i-- {
						if m.listItems[i].kind == itemSession &&
							m.listItems[i].projectIndex == item.projectIndex &&
							m.listItems[i].sessionIndex == item.sessionIndex {
							m.listCursor = i
							m.ensureListVisible()
							break
						}
					}
				}
			}
			return m, nil

		case key.Matches(msg, keys.PageUp):
			if m.focus == focusDetail {
				m.detailScroll -= 20
				if m.detailScroll < 0 {
					m.detailScroll = 0
				}
			}
			return m, nil

		case key.Matches(msg, keys.PageDown):
			if m.focus == focusDetail {
				m.detailScroll += 20
			}
			return m, nil

		case key.Matches(msg, keys.Enter):
			if m.focus != focusList || m.listCursor >= len(m.listItems) {
				return m, nil
			}
			item := m.listItems[m.listCursor]
			switch item.kind {
			case itemProject:
				m.groups[item.projectIndex].Expanded = !m.groups[item.projectIndex].Expanded
				m.buildListItems(m.searchInput.Value())

			case itemSession:
				sk := sessionKey{item.projectIndex, item.sessionIndex}
				if m.expandedSessions[sk] {
					// Collapse: hide subagents
					m.expandedSessions[sk] = false
					m.buildListItems(m.searchInput.Value())
				} else {
					// Expand: scan subagents + load quick stats (no transcript!)
					info := &m.groups[item.projectIndex].Sessions[item.sessionIndex]
					m.selectedInfo = info
					m.selectedSub = nil
					m.showLogs = false
					m.liveWatching = false
					m.transcript = nil
					m.loading = true
					m.expandedSessions[sk] = true

					// Scan subagents (lightweight)
					subs, _ := data.ScanSubagents(m.claudeDir, info.SessionID)
					m.sessionSubagents[sk] = subs
					m.buildListItems(m.searchInput.Value())

					// Load quick stats only (fast!)
					return m, loadQuickStatsCmd(m.claudeDir, info.Project, info.SessionID)
				}

			case itemSubagent:
				sk := sessionKey{item.projectIndex, item.sessionIndex}
				subs := m.sessionSubagents[sk]
				if item.subagentIdx < len(subs) {
					sa := &subs[item.subagentIdx]
					m.selectedSub = sa
					m.liveWatching = false
					m.showLogs = false
					info := &m.groups[item.projectIndex].Sessions[item.sessionIndex]
					m.selectedInfo = info

					// Load quick stats for subagent
					if sa.Loaded {
						m.transcript = nil
						m.stats = sa.Stats
						m.detailScroll = 0
						m.loading = false
					} else {
						// Not loaded yet — load quick stats
						m.loading = true
						m.transcript = nil
						return m, func() tea.Cmd {
							saPath := sa.FilePath
							return func() tea.Msg {
								stats, _ := data.QuickScanStats(saPath)
								return QuickStatsLoadedMsg{Stats: stats}
							}
						}()
					}
				}
			}
			return m, nil
		}
	}

	return m, nil
}

func (m *Model) ensureListVisible() {
	listHeight := m.height - 6
	if listHeight < 1 {
		listHeight = 1
	}
	if m.listCursor < m.listOffset {
		m.listOffset = m.listCursor
	}
	if m.listCursor >= m.listOffset+listHeight {
		m.listOffset = m.listCursor - listHeight + 1
	}
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err)
	}
	switch m.currentView {
	case viewHelp:
		return m.renderHelp()
	case viewDashboard:
		return m.renderDashboard()
	default:
		return m.renderMain()
	}
}

func (m Model) renderMain() string {
	listWidth := m.width * 40 / 100
	if listWidth < 30 {
		listWidth = 30
	}
	detailWidth := m.width - listWidth - 3

	contentHeight := m.height - 2
	innerHeight := contentHeight - 2

	listContent := m.renderList(listWidth, innerHeight)
	detailContent := m.renderDetail(detailWidth, innerHeight)

	listBorder := lipgloss.NewStyle().
		Width(listWidth).Height(contentHeight).
		Border(lipgloss.RoundedBorder(), true, true, true, true).
		BorderForeground(colorBorder)

	detailBorder := lipgloss.NewStyle().
		Width(detailWidth).Height(contentHeight).
		MaxHeight(contentHeight + 2).
		Border(lipgloss.RoundedBorder(), true, true, true, true).
		BorderForeground(colorBorder)

	if m.focus == focusList {
		listBorder = listBorder.BorderForeground(colorPrimary)
	} else {
		detailBorder = detailBorder.BorderForeground(colorPrimary)
	}

	main := lipgloss.JoinHorizontal(lipgloss.Top,
		listBorder.Render(listContent),
		detailBorder.Render(detailContent))

	return lipgloss.JoinVertical(lipgloss.Left, main, m.renderStatusBar())
}

func (m Model) renderList(width, height int) string {
	var sb strings.Builder

	sessionCount := 0
	for _, g := range m.groups {
		sessionCount += len(g.Sessions)
	}

	title := fmt.Sprintf(" Sessions (%d)", sessionCount)
	if m.searching {
		title = " " + m.searchInput.View()
	}
	sb.WriteString(titleStyle.Width(width - 2).Render(title))
	sb.WriteString("\n")

	visibleHeight := height - 2
	if visibleHeight < 1 {
		visibleHeight = 1
	}

	end := m.listOffset + visibleHeight
	if end > len(m.listItems) {
		end = len(m.listItems)
	}

	for i := m.listOffset; i < end; i++ {
		item := m.listItems[i]
		selected := i == m.listCursor && m.focus == focusList

		switch item.kind {
		case itemProject:
			g := m.groups[item.projectIndex]
			arrow := "▶"
			if g.Expanded {
				arrow = "▼"
			}
			label := fmt.Sprintf("%s %s (%d)", arrow, g.Project, len(g.Sessions))
			if selected {
				sb.WriteString(selectedStyle.Width(width - 2).Render(label))
			} else {
				sb.WriteString(projectHeaderStyle.Width(width - 2).Render(label))
			}

		case itemSession:
			s := m.groups[item.projectIndex].Sessions[item.sessionIndex]
			sk := sessionKey{item.projectIndex, item.sessionIndex}
			expanded := m.expandedSessions[sk]

			arrow := "▶"
			if expanded {
				arrow = "▼"
			}

			ts := s.Timestamp.Format("01/02 15:04")

			// Flag indicator
			flag := " "
			if m.flaggedSessions[s.SessionID] {
				flag = "📌"
			}

			// Subagent count badge
			badge := ""
			if subs, ok := m.sessionSubagents[sk]; ok && len(subs) > 0 {
				running := data.CountRunningSubagents(subs)
				if running > 0 {
					badge = fmt.Sprintf(" [⚡%d/%d]", running, len(subs))
				} else {
					badge = fmt.Sprintf(" [%d agents]", len(subs))
				}
			}

			// Show live stats for flagged sessions
			if m.flaggedSessions[s.SessionID] {
				if fstats, ok := m.flaggedStats[s.SessionID]; ok {
					badge += fmt.Sprintf(" %s $%.2f",
						formatTokens(fstats.TotalInputTokens+fstats.TotalOutputTokens),
						fstats.EstimatedCostUSD)
				}
			}

			display := truncate(s.Display, width-24-len(badge))
			line := fmt.Sprintf(" %s%s %s %s%s", flag, arrow, ts, display, badge)
			if item.matchKind == data.MatchContent {
				if selected {
					line += " [content]"
				} else {
					line += " " + matchHintStyle.Render("[content]")
				}
			}

			if selected {
				sb.WriteString(selectedStyle.Width(width - 2).Render(line))
			} else {
				sb.WriteString(normalItemStyle.Width(width - 2).Render(line))
			}

		case itemSubagent:
			sk := sessionKey{item.projectIndex, item.sessionIndex}
			subs := m.sessionSubagents[sk]
			if item.subagentIdx < len(subs) {
				sa := subs[item.subagentIdx]
				status := data.SubagentStatusEmoji(sa.IsRunning)
				typeEmoji := data.SubagentTypeEmoji(sa.AgentType)

				label := sa.AgentType
				runLabel := ""
				if sa.IsRunning {
					runLabel = " running"
				}

				statsStr := ""
				if sa.Loaded {
					statsStr = fmt.Sprintf(" %dm/%dt",
						sa.Stats.MessageCount, sa.Stats.ToolCallCount)
				}

				line := fmt.Sprintf("      %s %s %s%s%s", status, typeEmoji, label, runLabel, statsStr)
				if selected {
					sb.WriteString(selectedStyle.Width(width - 2).Render(line))
				} else {
					sb.WriteString(mutedStyle.Width(width - 2).Render(line))
				}
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func (m Model) renderDetail(width, height int) string {
	if m.loading {
		return mutedStyle.Render("\n  Loading...")
	}

	if m.selectedInfo == nil {
		return mutedStyle.Render("\n  Select a session to view details\n\n  ↑↓ Navigate  Enter Expand  l View logs")
	}

	var sb strings.Builder

	// Header
	if m.selectedSub != nil {
		sa := m.selectedSub
		emoji := data.SubagentTypeEmoji(sa.AgentType)
		status := "completed"
		if sa.IsRunning {
			status = "running"
		}
		sb.WriteString(headerStyle.Width(width - 2).Render(
			fmt.Sprintf("%s Subagent [%s] — %s", emoji, sa.AgentType, status)))
	} else {
		sb.WriteString(headerStyle.Width(width - 2).Render(
			fmt.Sprintf("Session %s", truncate(m.selectedInfo.SessionID, 36))))
	}
	sb.WriteString("\n\n")

	if m.confirmResume {
		sb.WriteString(toolLabelStyle.Render("  Resume this session? (y/n)"))
		sb.WriteString("\n\n")
	}

	// Metadata dashboard (always shown)
	if m.stats.MessageCount > 0 || m.stats.TotalOutputTokens > 0 {
		s := m.stats

		// Model
		model := "unknown"
		if len(s.Models) > 0 {
			model = s.Models[0]
		}
		sb.WriteString(assistantLabelStyle.Render("  Model     "))
		sb.WriteString(model + "\n")

		// Resume command
		if m.selectedSub == nil {
			sb.WriteString(assistantLabelStyle.Render("  Resume    "))
			sb.WriteString(fmt.Sprintf("claude --resume %s", truncate(m.selectedInfo.SessionID, 36)) + "\n")
		}

		sb.WriteString("\n")

		// Token stats
		sb.WriteString(toolLabelStyle.Render("  Tokens") + "\n")
		sb.WriteString(fmt.Sprintf("    Input:      %s\n", formatTokens(s.TotalInputTokens)))
		sb.WriteString(fmt.Sprintf("    Output:     %s\n", formatTokens(s.TotalOutputTokens)))
		if s.CacheWriteTokens > 0 {
			sb.WriteString(fmt.Sprintf("    Cache Write:%s\n", formatTokens(s.CacheWriteTokens)))
		}
		if s.CacheReadTokens > 0 {
			sb.WriteString(fmt.Sprintf("    Cache Read: %s\n", formatTokens(s.CacheReadTokens)))
		}
		sb.WriteString(fmt.Sprintf("    Total:      %s\n", formatTokens(s.TotalInputTokens+s.TotalOutputTokens)))

		sb.WriteString("\n")

		// Activity
		sb.WriteString(toolLabelStyle.Render("  Activity") + "\n")
		sb.WriteString(fmt.Sprintf("    Messages: %d\n", s.MessageCount))
		sb.WriteString(fmt.Sprintf("    Tools:    %d calls\n", s.ToolCallCount))

		sb.WriteString("\n")

		// Cost
		sb.WriteString(toolLabelStyle.Render("  Cost") + "\n")
		sb.WriteString(fmt.Sprintf("    Estimated: $%.2f\n", s.EstimatedCostUSD))

		sb.WriteString("\n")

		// Energy / Environment (ccwatt-style)
		sb.WriteString(thinkingLabelStyle.Render("  Environment (estimated)") + "\n")
		sb.WriteString(fmt.Sprintf("    Energy:     %.4f kWh\n", s.EnergyKWh))
		sb.WriteString(fmt.Sprintf("    CO2:        %.2f g\n", s.CO2Kg*1000))
		if s.TreeDays >= 1 {
			sb.WriteString(fmt.Sprintf("    Tree-days:  %.1f\n", s.TreeDays))
		} else if s.TreeDays > 0 {
			sb.WriteString(fmt.Sprintf("    Tree-days:  %.2f\n", s.TreeDays))
		} else {
			sb.WriteString("    Tree-days:  negligible\n")
		}
		sb.WriteString(mutedStyle.Render("    Tree-day = days 1 tree needs to absorb the CO2") + "\n")
		sb.WriteString(mutedStyle.Render("    ~0.001 Wh/tok, IEA 0.39 kgCO2/kWh, 38.4 gCO2/tree/day") + "\n")

		sb.WriteString("\n")
		if m.showLogs {
			sb.WriteString(mutedStyle.Render("  Press l to hide logs"))
		} else {
			sb.WriteString(mutedStyle.Render("  Press l to view full transcript"))
		}
		sb.WriteString("\n")
	}

	// Transcript (only when l is pressed)
	if m.showLogs && len(m.transcript) > 0 {
		sb.WriteString("\n")
		sb.WriteString(headerStyle.Width(width - 2).Render("Transcript"))
		sb.WriteString("\n\n")

		msgsNeeded := (height + m.detailScroll) / 3
		if msgsNeeded < 20 {
			msgsNeeded = 20
		}
		if msgsNeeded > len(m.transcript) {
			msgsNeeded = len(m.transcript)
		}

		for i := 0; i < msgsNeeded; i++ {
			rendered := renderMessage(m.transcript[i], width-4, m.showThinking, m.privacyMode)
			if rendered != "" {
				sb.WriteString(rendered)
				sb.WriteString("\n")
			}
		}
		if msgsNeeded < len(m.transcript) {
			sb.WriteString(mutedStyle.Render(
				fmt.Sprintf("\n  ... %d more messages (scroll down)", len(m.transcript)-msgsNeeded)))
			sb.WriteString("\n")
		}
	}

	return m.applyScroll(sb.String(), height)
}

func (m Model) applyScroll(content string, height int) string {
	lines := strings.Split(content, "\n")
	maxScroll := len(lines) - height
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := m.detailScroll
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}
	end := scroll + height
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[scroll:end], "\n")
}

func (m Model) renderStatusBar() string {
	var parts []string
	bindings := []struct{ key, desc string }{
		{"↑↓←→", "Navigate"},
		{"l", "Logs"},
		{"f", "Flag"},
		{"d", "Dash"},
		{"/", "Search"},
		{"c", "Copy"},
		{"r", "Resume"},
		{"Tab/S-Tab", "Panes"},
		{"?", "Help"},
		{"q", "Quit"},
	}
	for _, b := range bindings {
		parts = append(parts, helpKeyStyle.Render(b.key)+":"+helpDescStyle.Render(b.desc))
	}
	bar := strings.Join(parts, "  ")

	if m.liveWatching {
		bar += "  " + assistantLabelStyle.Render("[LIVE]")
	}
	if len(m.flaggedSessions) > 0 {
		bar += "  " + toolLabelStyle.Render(fmt.Sprintf("[📌%d]", len(m.flaggedSessions)))
	}
	if m.privacyMode {
		bar += "  " + toolLabelStyle.Render("[PRIVACY]")
	}
	if m.statusMsg != "" && time.Now().Before(m.statusExpires) {
		bar += "  " + assistantLabelStyle.Render(m.statusMsg)
	}

	return statusBarStyle.Width(m.width).Render(bar)
}

func (m Model) renderHelp() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("claudevu - Help"))
	sb.WriteString("\n\n")

	helpItems := []struct{ key, desc string }{
		{"↑/↓ or j/k", "Navigate tree or scroll transcript"},
		{"→ or Enter", "Expand project/session"},
		{"←", "Collapse (or jump to parent)"},
		{"l", "View/hide full transcript (starts live tailing)"},
		{"f", "Flag/unflag session for monitoring (auto-refresh)"},
		{"Tab", "Switch focus to next pane"},
		{"Shift+Tab", "Switch focus to previous pane"},
		{"/", "Start fuzzy search"},
		{"Esc", "Cancel search / Close help"},
		{"c", "Copy session ID to clipboard"},
		{"r", "Resume session (launches claude --resume)"},
		{"t", "Toggle thinking block visibility"},
		{"p", "Toggle privacy mode (secret redaction)"},
		{"d", "Usage dashboard (all sessions aggregated)"},
		{"Ctrl+R", "Force refresh (preserves UI state)"},
		{"PgUp/PgDn", "Scroll transcript by page"},
		{"?", "Toggle this help screen"},
		{"q / Ctrl+C", "Quit"},
	}

	for _, h := range helpItems {
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			helpKeyStyle.Width(16).Render(h.key),
			helpDescStyle.Render(h.desc)))
	}
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("  Press ? or Esc to close"))
	return sb.String()
}

func (m Model) renderDashboard() string {
	if m.dashboardLoading {
		return titleStyle.Render("  Dashboard") + "\n\n" + mutedStyle.Render("  Scanning all sessions...")
	}

	if m.dashboardData == nil {
		return titleStyle.Render("  Dashboard") + "\n\n" + mutedStyle.Render("  No data")
	}

	d := m.dashboardData
	var sb strings.Builder

	sb.WriteString(titleStyle.Render("  Dashboard — All Sessions"))
	sb.WriteString("\n\n")

	// Summary
	sb.WriteString(headerStyle.Width(m.width - 4).Render("  Summary"))
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("    Sessions:   %d\n", d.TotalSessions))
	sb.WriteString(fmt.Sprintf("    Tokens:     %s\n", formatTokens(d.TotalTokens)))
	sb.WriteString(fmt.Sprintf("    Cost:       $%.2f\n", d.TotalCost))
	sb.WriteString(fmt.Sprintf("    Energy:     %.4f kWh\n", d.TotalEnergyKWh))
	sb.WriteString(fmt.Sprintf("    CO2:        %.1f g\n", d.TotalCO2g))
	sb.WriteString(fmt.Sprintf("    Tree-days:  %.1f\n", d.TotalTreeDays))
	sb.WriteString("\n")

	// By Project
	sb.WriteString(headerStyle.Width(m.width - 4).Render("  By Project"))
	sb.WriteString("\n\n")
	sb.WriteString(assistantLabelStyle.Render(fmt.Sprintf("    %-30s %8s %10s %10s %8s", "Project", "Sessions", "Tokens", "Cost", "CO2")))
	sb.WriteString("\n")
	for _, p := range d.ByProject {
		project := p.Project
		if len(project) > 30 {
			project = "..." + project[len(project)-27:]
		}
		sb.WriteString(fmt.Sprintf("    %-30s %8d %10s %9s %7.0fg\n",
			project, p.Sessions, formatTokens(p.Tokens),
			fmt.Sprintf("$%.2f", p.Cost), p.CO2g))
	}
	sb.WriteString("\n")

	// By Model
	sb.WriteString(headerStyle.Width(m.width - 4).Render("  By Model"))
	sb.WriteString("\n\n")
	sb.WriteString(assistantLabelStyle.Render(fmt.Sprintf("    %-28s %8s %10s %10s", "Model", "Sessions", "Tokens", "Cost")))
	sb.WriteString("\n")
	for _, ms := range d.ByModel {
		sb.WriteString(fmt.Sprintf("    %-28s %8d %10s %9s\n",
			ms.Model, ms.Sessions, formatTokens(ms.Tokens),
			fmt.Sprintf("$%.2f", ms.Cost)))
	}
	sb.WriteString("\n")

	// Top expensive sessions
	sb.WriteString(headerStyle.Width(m.width - 4).Render("  Top Sessions by Cost"))
	sb.WriteString("\n\n")
	for i, s := range d.TopCost {
		sb.WriteString(fmt.Sprintf("    %d. $%.2f  %s  %s\n",
			i+1, s.Cost, formatTokens(s.Tokens), s.Display))
		sb.WriteString(mutedStyle.Render(fmt.Sprintf("       %s  %s", s.Model, truncate(s.SessionID, 36))))
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(mutedStyle.Render("  Press d or Esc to close  ↑↓ to scroll"))

	// Apply scroll
	lines := strings.Split(sb.String(), "\n")
	height := m.height - 1
	maxScroll := len(lines) - height
	if maxScroll < 0 {
		maxScroll = 0
	}
	scroll := m.dashboardScroll
	if scroll > maxScroll {
		scroll = maxScroll
	}
	end := scroll + height
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[scroll:end], "\n")
}

// Utilities

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max < 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func formatTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func copyToClipboard(text string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "echo|set /p="+text+"|clip")
	case "darwin":
		cmd = exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
	default:
		cmd = exec.Command("xclip", "-selection", "clipboard")
		cmd.Stdin = strings.NewReader(text)
	}
	cmd.Env = os.Environ()
	_ = cmd.Run()
}
