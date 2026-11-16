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
- **Search** (`/`) — case-insensitive substring filter over prompt text, session ID, project path, agent name/title, and conversation content (prompts, assistant replies, tool names, git branch, model; matches found only in content are tagged `[content]`). Content is indexed in the background, so metadata matches work instantly and content matches appear as indexing finishes. While a filter is active, the detail pane shows why the selected session matched: which fields (prompt, session ID, project, agent name, title), how many content matches, and up to three snippets with the match highlighted (snippets are lowercased). Sessions whose transcript is deleted stop matching on their old text, and the cursor stays on the same row when matches arrive.
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

No data leaves your machine, and claudevu never sends anything about your sessions. Everything is read-only, with two opt-in exceptions: the search index cache (see "Index cache (opt-in)" above) and a price refresh (see "Pricing" below), which is the only thing that uses the network and only when you ask for it.

### Pricing

Costs come from a price table built from Anthropic's published rates (embedded in the binary, dated in the status bar and on the dashboard). The table is applied per API message, so mixed-model sessions are costed correctly. Cache writes are split into 5-minute and 1-hour rates, fast mode and US-only inference multipliers are applied, and each message is counted once even though Claude Code logs it as several lines.

- A `~` before a cost means some model was priced by family because its exact version isn't in the table; a `+` means some tokens belong to a model with **no known price** and are not included (the detail pane names it).
- `claudevu --update-pricing` fetches the current prices once (the only network use), shows what changed, and exits. Large price swings are refused until you review them and pass `--accept-price-changes`.
- `--pricing-auto` / `CLAUDEVU_PRICING_AUTO=1` opts in to a background refresh, at most once a day, when a model without an exact price shows up.
- You can override or add prices in `<user config dir>/claudevu/pricing-overrides.json` (on macOS: `~/Library/Application Support/claudevu/`).
- `claudevu --pricing-info` explains where every price comes from, lists any problems (with fixes), shows the refresh policy and how each model in your sessions is priced. It is read-only and offline.

Details, safety rules and file formats: [docs/pricing.md](docs/pricing.md).

### Cost Calculation

Rates are USD per million tokens, as published by Anthropic (see `internal/data/pricing_table.json` for the full table and its date). Examples of the current rates:

| Model | Input/1M | Output/1M | Cache Write 5m/1M | Cache Write 1h/1M | Cache Read/1M |
|-------|----------|-----------|-------------------|-------------------|---------------|
| Opus 5.5 | $4.00 | $20.00 | $5.00 | $8.00 | $0.20 |
| Opus 4.8 | $5.00 | $25.00 | $6.25 | $10.00 | $0.50 |
| Sonnet 5.5 | $2.00 | $10.00 | $2.50 | $4.00 | $0.10 |
| Haiku 5.5 (prompts up to 100K tokens) | $0.10 | $0.50 | $0.125 | $0.20 | $0.01 |

Cache-read prices are not a flat share of input: they range from 0.025x to 0.1x depending on the model.

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
