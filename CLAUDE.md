# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Run directly (no build step)
go run .

# Build
go build -o claudevu .

# Run all tests
go test ./...

# Run a single test file
go test -run TestName ./internal/tui/

# Run with race detector
go test -race ./...
```

## Architecture

`claudevu` is a Bubble Tea TUI that reads Claude Code's local session data from `~/.claude/` — no writes, no network.

**Entry point** (`main.go`): Starts the Bubble Tea program, then after the TUI exits checks `m.ResumeSessionID` — if set, execs `claude --resume <id>`. Resume is the only side-effect the TUI triggers on the host.

**Data layer** (`internal/data/`):
- `history.go` — parses `~/.claude/history.jsonl` into `[]ProjectGroup`
- `session.go` — `LoadTranscript` / `QuickScanStats` for a single `.jsonl` session file
- `subagent.go` — scans `~/.claude/projects/[proj]/[id]/subagents/` for child agents
- `types.go` — shared structs (`SessionInfo`, `SessionStats`, `SubagentInfo`, `ProjectGroup`)
- `pricing.go` — per-model token rates; `ComputeStats` applies them to produce cost + energy metrics
- `dashboard.go` — `BuildDashboard` aggregates stats across all sessions for the `d` view
- `privacy.go` — regex-based redaction applied in `message_render.go`
- `quickstats.go` — lightweight scan that counts tokens without parsing full message content

**TUI layer** (`internal/tui/`):
- `app.go` — the entire Bubble Tea `Model` / `Update` / `View` cycle. The model is a 3-level tree (Project → Session → Subagent). All data loading is async via `tea.Cmd` returning typed messages (`QuickStatsLoadedMsg`, `TranscriptLoadedMsg`, etc.).
- `keys.go` — centralized key bindings; all key handling in `app.go` uses `key.Matches()` against this map.
- `message_render.go` — renders individual JSONL messages (user/assistant/tool) into styled strings
- `styles.go` — all Lip Gloss styles in one place
- `watcher.go` — live-tail: polls session file mod-time on a ticker, emits `sessionChangedMsg`

**Key data flow**: `LoadHistory` → `buildListItems` builds the flat `[]listItem` slice the list renders from. Expanding a session calls `ScanSubagents` (sync/cheap) plus `loadQuickStatsCmd` (async). Pressing `l` triggers `loadTranscriptCmd` (async, heavier) and enables the live-tail watcher.

**Resume flow**: `keys.Resume` (`r`) sets `confirmResume = true`. A `y` keypress sets `m.ResumeSessionID` + `m.ResumeProjectDir` and calls `tea.Quit`. `main.go` then runs `claude --resume <id>` with `cmd.Dir` set to the session's original project directory — the TUI must fully exit before Claude CLI takes over the terminal.
