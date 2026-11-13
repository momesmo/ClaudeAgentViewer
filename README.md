# claudevu

A blazing-fast terminal UI for browsing, searching, and monitoring your Claude Code sessions.

## Session View

```
╭─ Sessions (58) ──────────────────────╮╭─ Session 299a15d3... ──────────────────╮
│ ▼ C:\repo\claudeagentViewer (2)      ││                                        │
│   📌▼ 03/12 17:22 ... [1 agents]     ││  Model     claude-opus-4-6             │
│       ⚪ 🔍 Explore                   ││  Resume    claude --resume 299a15d3... │
│   ▶ 03/12 14:53 /exit                ││                                        │
│ ▶ C:\repo1 (18)                      ││  Tokens                                │
│ ▶ C:\Users\user (13)                 ││    Input:      629                     │
│ ▶ C:\repo (5)                        ││    Output:     111.5k                  │
│                                      ││    Cache Write: 2.1k                   │
│                                      ││    Cache Read:  63.0M                  │
│                                      ││    Total:       112.1k                 │
│                                      ││                                        │
│                                      ││  Activity                              │
│                                      ││    Messages: 777     Tools: 293 calls  │
│                                      ││                                        │
│                                      ││  Cost         $8.37                    │
│                                      ││                                        │
│                                      ││  Environment (estimated)               │
│                                      ││    Energy:     0.74 kWh                │
│                                      ││    CO2:        289.27 g                │
│                                      ││    Tree-days:  7.5                     │
│                                      ││    Tree-day = days 1 tree needs to     │
│                                      ││    absorb the CO2                      │
│                                      ││                                        │
│                                      ││  Press l to view full transcript       │
╰──────────────────────────────────────╯╰────────────────────────────────────────╯
 ↑↓←→:Navigate  l:Logs  f:Flag  d:Dash  /:Search  Tab/S-Tab:Panes  ?:Help  q:Quit
```

## Dashboard View (`d` key)

```
  Dashboard — All Sessions

  Summary
    Sessions:   58
    Tokens:     2.7M
    Cost:       $1,532.53
    Energy:     25.78 kWh
    CO2:        10,054.3 g
    Tree-days:  261.8

  By Project
    Project                        Sessions     Tokens       Cost      CO2
    C:\repo1                             18       2.1M   $845.68   8979g
    C:\repo\claudeagentViewer             2     163.1k   $312.73    587g
    ...

  By Model
    Model                        Sessions     Tokens       Cost
    claude-opus-4-6                    24     557.6k   $790.41
    claude-sonnet-4-6                  22       1.0M   $179.29
    ...

  Top Sessions by Cost
    1. $312.73  163.1k  Building claudevu TUI...
    2. $142.50  1.2M    Arlo loop agent...
    ...

  Press d or Esc to close  ↑↓ to scroll
```

## Features

- **3-level tree view** — Project > Session > Subagent with arrow key expand/collapse
- **Metadata dashboard** — tokens (input/output/cache write/cache read), cost, model, messages, tool calls
- **Usage dashboard** (`d`) — aggregated stats across all sessions by project, model, and top cost
- **Accurate pricing** — per-model rates for Opus/Sonnet/Haiku including cache token costs
- **Environment metrics** — energy (kWh), CO2 (g), tree-days (inspired by [ccwatt](https://github.com/nyanko3141592/ccwatt))
- **Subagent tracking** — see all subagents as children in the tree, with 🟢 running / ⚪ completed status
- **Session flagging** (`f`) — pin sessions for live monitoring (stats auto-refresh every 3s)
- **Live log tailing** (`l`) — stream transcript in real-time for active sessions
- **Async everything** — all loading happens in background, UI never blocks
- **Privacy mode** — auto-redacts API keys, tokens, and secrets (on by default)
- **Search** (`/`) — case-insensitive substring filter over prompt text, session ID, project path, agent name/title, and conversation content (prompts, assistant replies, tool names, git branch, model; matches found only in content are tagged `[content]`). Content is indexed in the background, so metadata matches work instantly and content matches appear as indexing finishes.
- **Index status bar** — a line just above the key hints shows whether the disk cache is on (and where it lives, how many entries were loaded from it), index progress (`indexing…` / `ready`, sessions indexed, index size), the last cache save (time, file size, or failure), and the active search filter with its result count. On narrow terminals it shortens itself instead of wrapping.
- **Session resume** — press `r` to launch `claude --resume <id>` directly
- **State preservation** — `Ctrl+R` refresh keeps your expanded folders, cursor, and flags

## Install

```bash
git clone https://github.com/HemantKumarMS/ClaudeAgentViewer.git
cd ClaudeAgentViewer
go build -o claudevu.exe .
./claudevu.exe
```

Requires Go 1.26+ (see `go.mod`).

## Index cache (opt-in)

By default the content search index is built in memory only. To persist it between runs:

- `--index-cache` (or `CLAUDEVU_INDEX_CACHE=1`/`true`) enables the cache; `--index-cache=false` overrides the env var.
- `--cache-dir <path>` overrides the location (default: `<user cache dir>/claudevu`).
- `--clear-cache` deletes the cache file and exits.

### Privacy: what the cache stores and what redaction does (and doesn't) do

- The cache stores **lowercased text from your prompts and assistant replies** (plus tool names, git branch and model), capped at 256 KB per session (the first 64 KB of a session plus its most recent text; only the middle of a very long session is dropped). Branch and model names are written only when they change. A pasted prompt larger than 1 MiB is kept partially (its first part) rather than skipped. It does not store tool inputs, tool results, thinking blocks or subagent transcripts.
- The file is written with mode 0600 and never under `~/.claude/`. `--clear-cache` removes it, including leftover temp files.
- **Secrets are redacted before anything is written, but redaction is best-effort pattern matching, not a guarantee.** It covers common formats: Anthropic/OpenAI/GitHub/AWS/Google/Stripe/SendGrid/Hugging Face/npm/Slack tokens, JWTs, PEM private keys, `Authorization: Bearer ...`, passwords in URLs, and `password=`/`token:`/`api_key=`-style assignments (values of 8+ characters). It does **not** catch unknown token formats, short or unlabeled passwords, or secrets split across a truncation point. Don't treat the cache as safe to share; delete it with `--clear-cache` if in doubt.
- The same rules power Privacy mode (`p`) in the transcript view, so the same limits apply there.
- Redaction applies to the saved copy only. In the live session you can still search for a secret you pasted. After a restart with the cache enabled, that secret is `[REDACTED]` in the loaded cache and won't match until its session file changes (or you run `--clear-cache`).

## Keybindings

| Key | Action |
|-----|--------|
| `↑` `↓` `j` `k` | Navigate tree / scroll transcript |
| `→` or `Enter` | Expand project or session |
| `←` | Collapse (or jump to parent) |
| `l` | View/hide full transcript (enables live tailing) |
| `f` | Flag/unflag session for live monitoring |
| `d` | Usage dashboard (aggregated stats across all sessions) |
| `Tab` / `Shift+Tab` | Switch between tree and detail panes |
| `/` | Search sessions (prompt text, ID, project, agent name/title, and conversation content) |
| `c` | Copy session ID to clipboard |
| `r` | Resume session (`claude --resume`) |
| `t` | Toggle thinking block visibility |
| `p` | Toggle privacy mode |
| `Ctrl+R` | Force refresh (preserves UI state) |
| `PgUp` / `PgDn` | Scroll transcript by page |
| `?` | Help overlay |
| `q` | Quit |

## How it works

claudevu reads Claude Code's local session data:

- `~/.claude/history.jsonl` — session index
- `~/.claude/projects/[project]/[sessionId].jsonl` — full transcripts
- `~/.claude/projects/[project]/[sessionId]/subagents/` — subagent data

No data leaves your machine. Everything is read-only, except the optional index cache (see "Index cache (opt-in)" above).

### Cost Calculation

Costs are calculated using Anthropic's published per-model rates:

| Model | Input/1M | Output/1M | Cache Write/1M | Cache Read/1M |
|-------|----------|-----------|-----------------|---------------|
| Opus 4.6 | $15.00 | $75.00 | $18.75 | $1.50 |
| Sonnet 4.6 | $3.00 | $15.00 | $3.75 | $0.30 |
| Haiku 4.5 | $0.80 | $4.00 | $1.00 | $0.08 |

### Environment Metrics

Energy and CO2 estimates use the following methodology:

| Metric | Value | Source |
|--------|-------|--------|
| Energy per token | ~0.001 Wh | arXiv:2505.09598, arXiv:2509.08867 |
| Cache read energy | ~1% of full token | ccwatt methodology |
| Grid carbon intensity | 0.390 kg CO2/kWh | IEA 2024 global average |
| Tree CO2 absorption | 38.4 g/day (14 kg/year) | European Environment Agency |

**Tree-day** = how many days one mature tree needs to grow to absorb the CO2 from your session. For example, 9.7 tree-days means one tree for ~10 days, or 10 trees for 1 day.

> **Note:** These are rough estimates. Anthropic does not publish per-token energy data. Actual consumption depends on hardware, batch size, quantization, and data center efficiency. Treat as directional, not precise.

## Tech Stack

| Layer | Tool |
|-------|------|
| Language | Go 1.26+ |
| TUI | [Bubble Tea](https://github.com/charmbracelet/bubbletea) (Charm ecosystem) |
| Styling | [Lip Gloss](https://github.com/charmbracelet/lipgloss) |

## License

MIT
