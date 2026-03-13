# claudevu — Browse, Search & Analyze Your Claude Code Sessions

> A blazing-fast terminal UI for exploring all your Claude Code sessions across every project. Search transcripts, track token spend, resume sessions, and understand your AI coding patterns — all from a single portable binary.

**This plan was synthesized from reviews by GPT-5.1-Codex, Claude Sonnet 4, and Gemini 3 Pro. The consensus items are highest priority; unique high-value ideas are called out by source.**

---

## ✨ What Makes This Special

| Pain Point | How claudevu Solves It |
|---|---|
| "I had a great session last week but can't find it" | Fuzzy search across all sessions by prompt text, tools used, or session ID |
| "How much am I spending on Claude?" | Token usage dashboard with cost estimates, sparklines, and per-project breakdowns |
| "I need to resume that session" | One-key resume (`r`) launches `claude --resume <id>` directly |
| "Raw JSONL is unreadable" | Syntax-highlighted transcripts with markdown rendering, collapsible tool calls |
| "Is my API key visible?" | Privacy mode auto-redacts secrets (sk-proj-*, ghp_*, AWS keys) by default |

---

## Tech Stack

| Layer | Tool | Why |
|---|---|---|
| Language | **Go 1.22+** | Single binary, zero dependencies, cross-platform |
| TUI Framework | **Bubble Tea** | Best-in-class Go TUI framework (Charm ecosystem) |
| Styling | **Lip Gloss** | Flexible terminal styling with adaptive color support |
| Components | **Bubbles** | Reusable TUI components (list, viewport, textinput) |
| Markdown | **Glamour** | Beautiful terminal markdown rendering |
| Syntax Highlighting | **Chroma** | Code block highlighting in transcripts |
| Cache/Search | **SQLite** (modernc.org/sqlite) | CGO-free, fast full-text search index |
| Distribution | **GoReleaser** | Automated cross-platform builds + Homebrew/Scoop |
| Demo | **VHS** | Scripted terminal GIF recordings for README |

---

## Data Sources

- **Global history**: `~/.claude/history.jsonl` — session index with display text, timestamp, project, sessionId
- **Session transcripts**: `~/.claude/projects/[project-name]/[sessionId].jsonl` — full message log (user, assistant, tool_use, progress, file-history-snapshot)
- **Subagent metadata**: `[sessionId]/subagents/agent-[id].meta.json`

### Data Format Details

#### `history.jsonl` (each line)
```json
{
  "display": "User prompt text",
  "pastedContents": {},
  "timestamp": 1772497287200,
  "project": "C:\\repo",
  "sessionId": "c722559e-06aa-42dd-952c-6639758239d9"
}
```

#### Session JSONL message types

**User message:**
```json
{
  "parentUuid": null,
  "isSidechain": false,
  "userType": "external",
  "cwd": "C:\\repo",
  "sessionId": "uuid",
  "version": "2.1.74",
  "gitBranch": "HEAD",
  "type": "user",
  "message": { "role": "user", "content": "message text" },
  "uuid": "unique-message-id",
  "timestamp": "ISO-8601"
}
```

**Assistant message:**
```json
{
  "parentUuid": "uuid",
  "type": "assistant",
  "sessionId": "uuid",
  "message": {
    "model": "claude-sonnet-4-6",
    "role": "assistant",
    "content": [
      {"type": "thinking", "thinking": "..."},
      {"type": "tool_use", "id": "toolu_xxx", "name": "ToolName", "input": {}},
      {"type": "text", "text": "..."}
    ],
    "usage": { "input_tokens": 123, "output_tokens": 456 }
  },
  "uuid": "unique-message-id",
  "timestamp": "ISO-8601"
}
```

**Other types (skip during rendering):** `progress`, `file-history-snapshot`, `system`

### File Locations
- Projects directory: `~/.claude/projects/`
- Project dirs named: `C--repo`, `C--repo-claudeagentViewer`, etc. (colons → dashes, separators normalized)
- Session files: `[project-dir]/[sessionId].jsonl` (UUID format)
- Subagent files: `[sessionId]/subagents/agent-[id].jsonl` + `.meta.json`
- ~1,469 total session files across ~12 projects
- ~414 history entries

---

## UI Layout

### Main View (Two-Pane)
```
┌─ Sessions (60) ─────────── 🔍 _________ ┬─ Session c722559e-06aa-42dd-952c ──────┐
│                                          │  claude --resume c722559e...           │
│ ▼ C:\repo (7 sessions)                   │  Model: claude-sonnet-4-6              │
│   03/03 00:21 │ clone the repo htt...    │  Branch: main  Tokens: 12.4k  ~$0.08  │
│ ● 03/03 01:15 │ Do the option 1          ├───────────────────────────────────────│
│   ...                                    │                                        │
│ ▼ C:\repo1 (18 sessions)                 │  👤 USER                               │
│   03/06 10:30 │ Build the new feat...    │  clone the repo https://github.com/... │
│   ...                                    │                                        │
│                                          │  🤖 ASSISTANT                          │
│                                          │  I'll clone that repo for you.         │
│                                          │                                        │
│                                          │  🔧 TOOL: Bash                         │
│                                          │  ┌─────────────────────────────────┐   │
│                                          │  │ git clone https://github.com/...│   │
│                                          │  │ > Cloning into 'outlook-mcp'... │   │
│                                          │  └─────────────────────────────────┘   │
│                                          │                                        │
│                                          │  💭 Thinking (collapsed) ▶             │
├──────────────────────────────────────────┴────────────────────────────────────────┤
│ ↑↓:Navigate  Enter:Select  /:Search  c:Copy ID  r:Resume  t:Thinking  Tab:Focus │
│ d:Dashboard  p:Privacy  ?:Help  q:Quit                                           │
└──────────────────────────────────────────────────────────────────────────────────┘
```

### Analytics Dashboard (`d` key)
```
┌─ Token Usage (30 days) ──────────────────────────────────────────────────────────┐
│                                                                                   │
│  Total: 2.4M tokens  │  Est. Cost: $47.20  │  Sessions: 142  │  Avg: 16.9k/sess │
│                                                                                   │
│  Daily Usage          ▁▃▅▇██▇▅▃▁▂▄▆█▇▅▃▁▁▃▅▇██▇▅▃▁▂▄                           │
│                       ─────────────────────────────────                            │
│                       Feb 10                    Mar 12                             │
│                                                                                   │
│  Top Projects                    │  Top Tools                                     │
│  ├─ C:\repo      842k (35%)     │  ├─ edit         342 calls                     │
│  ├─ C:\repo1     621k (26%)     │  ├─ Bash         287 calls                     │
│  └─ C:\app       419k (17%)     │  ├─ Read         198 calls                     │
│                                  │  └─ Search       124 calls                     │
│  Models Used                     │                                                │
│  ├─ sonnet-4-6   78%            │  Cost Trend: ↓ 12% vs last month               │
│  └─ opus-4       22%            │                                                │
└──────────────────────────────────────────────────────────────────────────────────┘
```

---

## File Structure

```
claudevu/
  go.mod
  go.sum
  main.go                          # Entry point + CLI flags
  demo.tape                        # VHS script for README GIF
  .goreleaser.yml                  # Cross-platform release config
  LICENSE                          # MIT
  README.md                        # Story-driven with demo GIF
  CONTRIBUTING.md
  internal/
    config/
      config.go                    # User config (~/.config/claudevu/config.yaml)
      themes.go                    # Built-in color schemes
    data/
      history.go                   # Parse history.jsonl, return session index
      session.go                   # Parse session JSONL transcript (lazy, streaming)
      scanner.go                   # Scan ~/.claude/projects/ for session files
      types.go                     # All data types
      cache.go                     # SQLite index for fast full-text search
      analytics.go                 # Token usage aggregation, cost calculation
      privacy.go                   # Secret detection & redaction
    tui/
      app.go                       # Root Bubble Tea model, layout management
      session_list.go              # Left pane: grouped session list with filtering
      session_detail.go            # Right pane: transcript viewer with scrolling
      message_render.go            # Render messages with markdown + syntax highlighting
      dashboard.go                 # Analytics dashboard view
      styles.go                    # Lip Gloss style definitions (theme-aware)
      keys.go                      # Key bindings
      resume.go                    # Launch `claude --resume <id>`
      help.go                      # Help overlay
```

---

## Data Types (`internal/data/types.go`)

```go
type HistoryEntry struct {
    Display   string `json:"display"`
    Timestamp int64  `json:"timestamp"`
    Project   string `json:"project"`
    SessionID string `json:"sessionId"`
}

type SessionMessage struct {
    Type      string          // "user", "assistant", "progress", "file-history-snapshot"
    SessionID string
    Timestamp string
    GitBranch string
    Version   string
    Model     string          // assistant only
    Content   string          // user: plain text
    Blocks    []ContentBlock  // assistant: text/thinking/tool_use blocks
    Usage     TokenUsage      // assistant only
}

type ContentBlock struct {
    Type      string // "text", "thinking", "tool_use", "tool_result"
    Text      string
    ToolName  string
    ToolInput string // JSON string, truncated for display
}

type TokenUsage struct {
    InputTokens       int `json:"input_tokens"`
    OutputTokens      int `json:"output_tokens"`
    CacheCreation     int `json:"cache_creation_input_tokens"`
    CacheRead         int `json:"cache_read_input_tokens"`
}

type SessionStats struct {
    TotalInputTokens  int
    TotalOutputTokens int
    EstimatedCostUSD  float64
    MessageCount      int
    ToolCallCount     int
    Duration          time.Duration
    Models            []string
    ToolsUsed         map[string]int
}
```

---

## Data Loading Strategy

1. **Startup (<50ms target)**: Read `history.jsonl` (~414 lines) into memory. Group by project, sort by timestamp desc.
2. **Background indexing**: Spawn goroutine to build/update SQLite full-text search index at `~/.config/claudevu/cache.db`. Index session prompts, tool names, and metadata for instant search.
3. **Fallback scan**: Scan `~/.claude/projects/` dirs for orphan `.jsonl` files not in history. Read only first 2 lines for metadata.
4. **On-demand transcript loading**: When user selects a session, stream-parse its `.jsonl`. Filter out `progress` and `file-history-snapshot` lines (~70%). Use sliding window for viewport (don't load 10k+ messages into memory).
5. **LRU cache**: Keep last 10 parsed transcripts in memory.

---

## 🔒 Privacy Mode (All 3 Reviewers Agreed: CRITICAL)

Enabled by default. Auto-redacts patterns in transcript view:
- `sk-proj-*`, `sk-ant-*` (API keys)
- `ghp_*`, `gho_*`, `github_pat_*` (GitHub tokens)
- `AKIA*` (AWS access keys)
- Generic high-entropy strings matching token patterns
- Toggle with `Ctrl+P` or `p` key
- Configurable in `config.yaml`

---

## Milestones

### v0.1 — MVP (Ship It) 🚀
**Goal**: A beautiful, fast session browser that solves real pain points.

- [ ] Two-pane layout with session list + transcript viewer
- [ ] Project grouping with collapsible headers
- [ ] Fuzzy search/filter across sessions
- [ ] Syntax-highlighted transcripts (markdown + code blocks via Glamour/Chroma)
- [ ] Collapsible thinking blocks + tool call formatting
- [ ] Copy session ID to clipboard (`c`)
- [ ] Resume session (`r`) with confirmation prompt
- [ ] Privacy mode with secret redaction (on by default)
- [ ] Token count display per session with cost estimate
- [ ] Multiple themes (Dark, Light, Solarized) with auto-detection
- [ ] Help overlay (`?`)
- [ ] Graceful error handling (corrupt JSONL, missing files)
- [ ] GoReleaser + GitHub Actions for cross-platform binaries
- [ ] Homebrew tap + Scoop bucket
- [ ] VHS-scripted demo GIF in README
- [ ] CLI flags: `claudevu [session-id]`, `claudevu --search "query"`, `claudevu --project /path`

### v0.1.1 — Live Refresh & Subagents 🔄
**Goal**: Real-time awareness of new sessions and full subagent visibility.

#### Real-Time Refresh
- [ ] File watcher on `~/.claude/history.jsonl` using `fsnotify` — detect new entries
- [ ] Periodic polling fallback (every 2s) for platforms where fsnotify is unreliable
- [ ] On change: re-read history, merge new sessions into existing groups, preserve selection
- [ ] Auto-refresh transcript view if the selected session's JSONL changes (live tailing)
- [ ] Visual indicator in status bar: "Live" with a blinking dot when watching
- [ ] `Ctrl+R` to force manual refresh

#### Subagent Support
- [x] Detect subagent directories: `[sessionId]/subagents/agent-[id].jsonl` + `.meta.json`
- [x] Parse `.meta.json` for agent type (Explore, Plan, general-purpose)
- [x] In transcript view: when a `tool_use` block has `name: "Agent"`, render inline subagent header
- [x] Subagent messages rendered indented with colored left border (distinct per agent type)
- [x] Collapsible subagent transcripts (`s` key to toggle all)
- [x] Subagent label shows: type badge (🔍 Explore, 📋 Plan, 🤖 General), description from input, message/tool count
- [x] Subagent token usage rolled into parent session stats

#### Live Subagent Tracking (Running vs Completed)
- [ ] Detect running subagents: heuristic using two signals:
  1. **No `stop_reason: "end_turn"`** in the last assistant message of the subagent JSONL → still running
  2. **File mtime within last 30s** → actively being written to
- [ ] Running subagent indicator in session list: e.g., "⚡3 running" next to session entry
- [ ] Running subagent indicator in detail header: "Subagents: 77 (3 running)"
- [ ] Subagent list shows per-agent status badge: 🟢 running / ⚪ completed
- [ ] Running subagents sorted to top in the subagent section
- [ ] Auto-refresh subagent state every 2s when a session with running subagents is selected
- [ ] Live message count update for running subagents (tail the JSONL)
- [ ] Optional notification when a subagent completes (status bar flash)

### v0.2 — Analytics & Power Features 📊
- [ ] Analytics dashboard with token usage trends (sparklines)
- [ ] Cost breakdown by project, model, and time period
- [ ] Tool usage heatmap (most-used tools, tool combinations)
- [ ] SQLite full-text search index for instant cross-session search
- [ ] Session bookmarks/favorites
- [ ] Export session as Markdown/HTML
- [ ] Persistent state (remember last session, filters, layout)
- [ ] Config file (`~/.config/claudevu/config.yaml`)

### v1.0 — Ecosystem 🌐
- [ ] SessionProvider adapter interface (support Cursor, Aider, Windsurf sessions)
- [ ] Plugin system for custom renderers and exporters
- [ ] Session diff/comparison view
- [ ] Live tailing of in-progress sessions
- [ ] Session chain visualization (resumed session trees)
- [ ] Shell completions (Bash/Zsh/Fish)

---

## Implementation Steps

### Step 1: Scaffold & Dependencies
```bash
go mod init github.com/user/claudevu
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/lipgloss
go get github.com/charmbracelet/bubbles
go get github.com/charmbracelet/glamour
go get github.com/alecthomas/chroma/v2
go get modernc.org/sqlite
```
- Set up file structure, `main.go` with CLI flag parsing
- Add `.goreleaser.yml`, `LICENSE` (MIT), `CONTRIBUTING.md`

### Step 2: Data Layer (`internal/data/`)
- `types.go`: All structs including `TokenUsage` with cache fields and `SessionStats`
- `history.go`: `LoadHistory(claudeDir string) ([]HistoryEntry, error)`
- `scanner.go`: `ScanProjects(claudeDir string) (map[string][]SessionInfo, error)`
- `session.go`: `LoadTranscript(path string) ([]SessionMessage, error)` — streaming parser, filter noise
- `analytics.go`: `ComputeStats(messages []SessionMessage) SessionStats` — token totals, cost estimates, tool counts
- `privacy.go`: `RedactSecrets(text string) string` — regex-based secret masking

### Step 3: TUI Skeleton (`internal/tui/`)
- `styles.go`: Theme-aware color definitions with adaptive terminal detection (24-bit / 256 / basic)
- `keys.go`: Full key map (arrows, enter, /, q, c, r, t, p, d, ?, tab)
- `app.go`: Root model with child models, focus management, view switching (main ↔ dashboard ↔ help)

### Step 4: Session List (`session_list.go`)
- Grouped by project (collapsible ▼/▶ headers)
- Each item: formatted timestamp + truncated first prompt (50 chars) + token indicator
- Fuzzy filtering with highlighted matches
- Arrow keys to navigate, Enter to select

### Step 5: Transcript Viewer (`session_detail.go` + `message_render.go`)
- Header: session ID, resume command, model, branch, token totals with cost
- `message_render.go`:
  - User: `👤 USER` prefix (green) + content
  - Assistant: `🤖 ASSISTANT` prefix (blue) + Glamour-rendered markdown
  - Tool calls: `🔧 TOOL: Name` (yellow) + boxed input with syntax highlighting
  - Thinking: `💭 Thinking` collapsed by default, `t` to expand
  - Large outputs: auto-collapsed with "▶ Show N more lines"

### Step 6: Privacy Mode (`internal/data/privacy.go`)
- Compile regex patterns at init time for performance
- Apply redaction in the render layer (not data layer) so raw data is preserved
- Toggle state stored in app model, `p` or `Ctrl+P` to toggle
- Visual indicator in status bar when privacy mode is active/inactive

### Step 7: Resume & Clipboard (`resume.go`)
- `r` key: confirmation prompt → clean exit → exec `claude --resume <id>`
- `c` key: copy session ID to clipboard (platform-aware: clip.exe / pbcopy / xclip / xsel)
- `Shift+C`: copy `claude --resume <id>` command to clipboard

### Step 8: Themes (`internal/config/themes.go`)
- Built-in: Dark (default), Light, Solarized, Monokai, High Contrast
- Auto-detect terminal background color where possible
- `T` key to cycle themes, persisted in config

### Step 9: CLI Interface & Deep Linking
```
claudevu                          # Launch TUI
claudevu <session-id>             # Open specific session directly
claudevu --search "database"      # Open with search pre-filled
claudevu --project /path/to/repo  # Filter to specific project
claudevu --version                # Print version
```

### Step 10: Live Refresh (`internal/tui/watcher.go`)
- Use `fsnotify` to watch `~/.claude/history.jsonl` for writes
- On file change: send `historyChangedMsg` to Bubble Tea program via `p.Send()`
- In `Update()`: re-load history, diff with existing groups, merge new sessions
- Preserve current cursor position and selected session across refreshes
- Also watch the selected session's JSONL for live transcript tailing
- Periodic fallback: send `tickMsg` every 2s, stat `history.jsonl` for mtime change
- Status bar shows live indicator when watcher is active

### Step 11: Subagent Support (`internal/data/subagent.go` + render changes)
- `subagent.go`: `LoadSubagents(sessionDir string) ([]SubagentInfo, error)`
  - Scan `[sessionId]/subagents/` for `agent-*.meta.json` + `.jsonl` pairs
  - Parse meta for type, parse JSONL lazily on demand
- New types: `SubagentInfo { AgentID, AgentType, Messages []SessionMessage, Stats SessionStats }`
- In `session.go`: when parsing `tool_use` with `name=="Agent"`, extract `input.description` and `input.subagent_type`
- In `message_render.go`: render Agent tool_use blocks with subagent header + indented transcript
- Indented rendering: 4-char left margin with colored border (🔍 cyan for Explore, 📋 yellow for Plan, 🤖 blue for general)
- `s` key to toggle subagent expansion globally
- Subagent stats rolled into parent session totals

### Step 12: Subagent Tree View + Live Tracking (PERFORMANCE-OPTIMIZED)

**Architecture: Lazy tree with lightweight polling**

Data layer (`internal/data/subagent.go`):
- `ScanSubagents()` — metadata-only scan (reads .meta.json + stat, NO transcript loading)
- `RefreshSubagentStatus()` — updates only running status, preserves all cached data
- `LoadSubagentTranscript()` — called ONLY when user expands a specific subagent
- `QuickSubagentCount()` — cheapest possible: count .jsonl files + mtime check
- Detection heuristic: mtime < 30s (fast path) + no `stop_reason:"end_turn"` in last line

Tree view UX (`internal/tui/app.go`):
- `s` key: toggles subagent tree section visibility
- Tree shows one line per subagent: `[key] [▶/▼] [🟢/⚪] [type emoji] [type] [running?] [stats if loaded]`
- Keys `1-9`: expand/collapse individual subagents (loads transcript lazily on first expand)
- Running subagents sorted to top
- Expanded subagent shows transcript with colored left border (cyan/yellow/blue by type)

Performance guarantees:
- Startup: NO subagent scanning (only on session select)
- Session select: scan metadata only (~10ms for 80 subagents)
- Tick (2s): only refresh status for selected session's subagents, no transcript loading
- Transcript: loaded ONCE per subagent, only on user expand, cached after
- Running subagent transcripts: re-loaded on tick only if expanded

### Step 13: Build, Package & Ship
- GoReleaser config for Windows/macOS(Intel+ARM)/Linux builds
- GitHub Actions CI: lint (golangci-lint) + test + build on every PR
- GitHub Actions Release: goreleaser on tag push
- Homebrew tap: `brew install user/tap/claudevu`
- Scoop bucket: `scoop install claudevu`
- README with VHS-generated demo GIF

---

## Distribution Strategy

```
Primary:     go install github.com/user/claudevu@latest
Homebrew:    brew install user/tap/claudevu
Scoop:       scoop bucket add claudevu https://github.com/user/scoop-claudevu && scoop install claudevu
Binary:      Download from GitHub Releases (Windows/macOS/Linux)
```

---

## Community Launch Checklist

### Pre-Launch
- [ ] README with compelling demo GIF (VHS), feature matrix, installation instructions
- [ ] Architecture diagram in docs/
- [ ] Sample session data for contributors to test with
- [ ] Issue templates (bug report, feature request)
- [ ] `make demo` target to regenerate GIF

### Launch Day
- [ ] Post to Hacker News: "Show HN: claudevu — a TUI for browsing your Claude Code sessions"
- [ ] Post to r/commandline, r/golang, r/ClaudeAI
- [ ] Tweet/post with demo GIF
- [ ] Announce in Claude Discord / community channels

### Post-Launch
- [ ] Triage issues within 24 hours
- [ ] Tag "good first issue" on beginner-friendly tasks
- [ ] Release v0.1.1 with community-reported fixes within 1 week

---

## Verification (MVP)
1. `go build .` compiles without errors
2. `./claudevu` shows session list from `~/.claude/history.jsonl` in <200ms
3. Navigate with arrow keys — sessions grouped by project, collapsible
4. Enter selects session — transcript renders with syntax highlighting
5. `c` copies session ID, `Shift+C` copies resume command
6. `/` + type → fuzzy filters sessions
7. `t` toggles thinking blocks
8. `r` shows confirmation → launches `claude --resume <id>`
9. `p` toggles privacy mode — secrets redacted in transcript
10. Cost estimate shown in session header
11. `?` shows help overlay with all keybindings
12. `q` exits cleanly
13. Works on Windows, macOS, and Linux

---

## Reviewer Consensus Summary

### All 3 Models Agreed On (Highest Priority):
1. **SQLite cache/index** for instant search across 1,500+ sessions
2. **Secret redaction / privacy mode** — non-negotiable for a tool reading session data
3. **Syntax highlighting + markdown rendering** in transcripts (Glamour + Chroma)
4. **GoReleaser + Homebrew/Scoop** for frictionless installation
5. **Demo GIF** (VHS) — the README demo is make-or-break for adoption
6. **Token usage analytics with cost estimates** — the killer differentiator

### Unique High-Value Ideas (One Reviewer Each):
- **GPT**: Live tailing of in-progress sessions, session diff/comparison, command palette
- **Claude**: Tool usage heatmaps, session chain visualization, plugin architecture
- **Gemini**: SessionProvider adapter for multi-tool support, deep linking CLI, VHS specifically
