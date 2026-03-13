# claudevu

A blazing-fast terminal UI for browsing, searching, and monitoring your Claude Code sessions.

```
╭─ Sessions (58) ──────────────────────╮╭─ Session 299a15d3... ──────────────────╮
│ ▼ C:\repo\claudeagentViewer (2)      ││                                        │
│   📌▼ 03/12 17:22 ... [1 agents]     ││  Model     claude-opus-4-6             │
│       ⚪ 🔍 Explore                   ││  Resume    claude --resume 299a15d3... │
│   ▶ 03/12 14:53 /exit                ││                                        │
│ ▶ C:\repo1 (18)                      ││  Tokens                                │
│ ▶ C:\Users\user (13)                 ││    Input:    629     Output:  111.5k    │
│ ▶ C:\repo (5)                        ││    Cached:   63.0M   Total:   112.1k   │
│                                      ││                                        │
│                                      ││  Activity                              │
│                                      ││    Messages: 777     Tools: 293 calls  │
│                                      ││                                        │
│                                      ││  Cost         $8.37                    │
│                                      ││                                        │
│                                      ││  Environment                           │
│                                      ││    Energy:   0.74 kWh                  │
│                                      ││    CO2:      370.86 g                  │
│                                      ││    Trees:    9.7 tree-days to offset   │
╰──────────────────────────────────────╯╰────────────────────────────────────────╯
 ↑↓←→:Navigate  l:Logs  f:Flag  /:Search  Tab/S-Tab:Panes  ?:Help  q:Quit
```

## Features

- **3-level tree view** — Project > Session > Subagent with arrow key navigation
- **Metadata dashboard** — tokens, cost, model, messages, tool calls per session
- **Environment metrics** — energy (kWh), CO2 (g), tree-days to offset (inspired by [ccwatt](https://github.com/nyanko3141592/ccwatt))
- **Subagent tracking** — see all subagents spawned by a session, with running/completed status
- **Session flagging** — pin sessions with `f` for live monitoring (stats auto-refresh every 3s)
- **Live log tailing** — press `l` to stream transcript in real-time for active sessions
- **Async everything** — transcript loading never blocks the UI
- **Privacy mode** — auto-redacts API keys, tokens, and secrets (on by default)
- **Fuzzy search** — filter sessions by prompt text, session ID, or project path
- **Session resume** — press `r` to launch `claude --resume <id>` directly

## Install

### From source

```bash
go install github.com/user/claudevu@latest
```

### Build locally

```bash
git clone https://github.com/HemantKumarMS/ClaudeAgentViewer.git
cd ClaudeAgentViewer
go build -o claudevu.exe .
./claudevu.exe
```

## Keybindings

| Key | Action |
|-----|--------|
| `↑` `↓` `j` `k` | Navigate tree / scroll transcript |
| `→` or `Enter` | Expand project or session |
| `←` | Collapse (or jump to parent) |
| `l` | View/hide full transcript (starts live tailing) |
| `f` | Flag/unflag session for live monitoring |
| `Tab` / `Shift+Tab` | Switch between tree and detail panes |
| `/` | Fuzzy search sessions |
| `c` | Copy session ID to clipboard |
| `r` | Resume session (`claude --resume`) |
| `t` | Toggle thinking block visibility |
| `p` | Toggle privacy mode |
| `Ctrl+R` | Force refresh (preserves UI state) |
| `PgUp` / `PgDn` | Scroll transcript by page |
| `?` | Help |
| `q` | Quit |

## How it works

claudevu reads Claude Code's local session data:

- `~/.claude/history.jsonl` — session index
- `~/.claude/projects/[project]/[sessionId].jsonl` — full transcripts
- `~/.claude/projects/[project]/[sessionId]/subagents/` — subagent data

No data leaves your machine. Everything is read-only.

## Tech Stack

| Layer | Tool |
|-------|------|
| Language | Go 1.22+ |
| TUI | [Bubble Tea](https://github.com/charmbracelet/bubbletea) (Charm ecosystem) |
| Styling | [Lip Gloss](https://github.com/charmbracelet/lipgloss) |

## License

MIT
