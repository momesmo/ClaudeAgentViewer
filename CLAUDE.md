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

# Search/cache performance (rebuild vs cache load vs mtime check vs in-memory scan)
go test ./internal/data -run xxx -bench BenchmarkIndex
```

**CI and toolchain.** `.github/workflows/ci.yml` runs vet, build, `go test -race` and a gofmt check on Linux, macOS and Windows, cross-builds six OS/arch targets, tests the minimum Go version in `go.mod`, and runs a blocking `govulncheck`. `go.mod` pins `toolchain go1.27.4`; Go downloads it automatically, which keeps local builds, CI and releases on a patched standard library (older patch releases had reachable `net/http`/`crypto/tls` vulnerabilities that the price fetcher would hit). Actions are pinned to full commit SHAs; Dependabot keeps them current. Run `gofmt -l $(git ls-files '*.go')` before committing: CI fails on unformatted files.

**Tests never touch your real `~/.claude`.** Every package's `TestMain` calls `internal/testenv`, which points HOME, USERPROFILE, XDG_*, AppData and LocalAppData at an empty temp dir. Tests use `internal/testfixture` (`BuildClaudeDir`): a fake claude dir with the real on-disk shape (one JSONL line per content block with repeated message IDs and usage, subagents, an oversized line, unknown models, fast and US-geo messages) whose expected values are derived by hand, including literal USD amounts. A test must never skip because data is missing. To profile your own data: `CLAUDEVU_TEST_REAL_CLAUDE=1 go test -run PerfProfile .`

## Working agreements (read before committing or pushing)

Full detail and reasons: `docs/development.md`. The essentials:

- **Identity.** All commits and pushes are the personal identity `momesmo <6090451+momesmo@users.noreply.github.com>`; the work identity must never be committed or pushed. `origin` is the personal fork `momesmo/ClaudeAgentViewer`; `upstream` is the original project and its push is deliberately disabled. The local branch `local-dev-backup-work-identity` holds old work-identity commits: never push it. The git/gh configuration that makes this work lives **outside the repo** (`~/.gitconfig`, `~/.gitconfig-personal`, `~/.zshrc`); `docs/development.md` explains how to verify and recreate it.
- **Do not push, merge, delete branches or change GitHub settings unless the user asked.** Commit locally and report. `gh` commands against the fork need `GH_TOKEN=$(gh auth token -h github.com -u momesmo)` because the active gh account is the work one.
- **Never bypass the ggshield pre-commit hook.** If a test string looks like a secret, split the literal (`"xo" + "xb-..."`). Never commit real session data or tokens.
- **Backlog and decisions live in `docs/improvements.md`** (stable IDs, decision log). Update it, and the relevant docs, in the same change as the code.
- **How work is done:** implement in a git worktree (`.claude/worktrees/`, git-ignored), get an independent review, verify claims yourself, and prove tests catch bugs by mutating the code (a mutation no test catches is a missing test).
- **Before committing:** `go vet ./... && go test -race -count=1 ./...` and `gofmt -l $(git ls-files '*.go')` (empty). CI enforces both.

## Architecture

`claudevu` is a Bubble Tea TUI that reads Claude Code's local session data from `~/.claude/` — no writes (except the opt-in index cache under the user cache dir, never `~/.claude/`), no network except the opt-in price refresh (`--update-pricing`, `--pricing-auto`).

**Entry point** (`main.go`): Starts the Bubble Tea program, then after the TUI exits checks `m.ResumeSessionID` — if set, execs `claude --resume <id>`. Resume is the only side-effect the TUI triggers on the host, apart from the opt-in index cache (`--index-cache`, `--clear-cache`, `--cache-dir`, `CLAUDEVU_INDEX_CACHE`), which writes under the user cache dir.

**Data layer** (`internal/data/`):
- `history.go` — parses `~/.claude/history.jsonl` into `[]ProjectGroup`
- `session.go` — `LoadTranscript` / `ComputeStats` / `FindSessionFile` for a single `.jsonl` session file
- `subagent.go` — scans `~/.claude/projects/[proj]/[id]/subagents/` for child agents
- `types.go` — shared structs (`SessionInfo`, `SessionStats`, `SubagentInfo`, `ProjectGroup`)
- `dashboard.go` — `BuildDashboard` aggregates stats across all sessions for the `d` view
- `pricebook.go` — `PriceBook` (per-model rates + modifiers), `Lookup` (exact / normalized / family fallback / none), `Cost`, `Validate`, `Merge`; embedded `pricing_table.json` (dated). `EmbeddedBook()` is the fixed baseline; `DefaultBook()`/`SetDefaultBook()` is the layered book in use
- `usage.go` — `UsageTally`: counts each API message once (by `message.id`), prices per message by its own model, splits 5m/1h cache writes; fills `SessionStats` (cost, `CostEstimated`, `UnpricedModels`, `ModelBreakdown`). Both `ComputeStats` and `QuickScanStats` use it
- `linescan.go` — `lineScanner`: JSONL reader that skips oversized lines instead of aborting (bufio.Scanner stopped at the first line over its buffer and silently dropped the rest of a session)
- `pricinginfo.go` / `pricinginfo_text.go` — `ExplainPricing`/`ScanModelUsage`/`PricingReport.Text`: the read-only `--pricing-info` report (layers, problems with fixes, per-model provenance, modifier origins, refresh policy, how your sessions' models are priced)
- `pricesync.go` / `pricerefresh.go` — opt-in price refresh: markdown parser (reads columns by header), hardened fetcher (https, allowlist, size/time caps), cache/state/override files, `LayeredBook`, `RefreshPricingVetted` (refuses >3x or >25% changes unless accepted). See `docs/pricing.md`
- `privacy.go` — regex-based secret redaction (`RedactSecrets` for display in `message_render.go`; `RedactSecretsFold` for lowercased search blobs before they are written to the index cache). Best-effort; rules carry lowercase `hints` that skip regexes cheaply, and must not use uppercase regex escapes (`TestRedactRulesFoldSafe`)
- `searchblob.go` — `BuildIndexEntry`: per-session lowercase content blob (prompts, assistant text, tool names; branch/model only when they change; not thinking or tool results). Capped at `MaxBlobBytes` (256 KB) as head (first `HeadBytes`=64 KB, read forward) + tail (most recent text, read backwards from EOF in `tailChunkBytes` chunks). Lines over `maxLineBytes` (1 MiB) are cut and their message text salvaged (`salvageText`); oversize tool results are ignored. Skips re-reading a file whose mod-time is unchanged. Bump `IndexVersion` when this changes
- `searchmatch.go` — `MatchSession` / `FillSearchMeta`: metadata vs content matching
- `indexstore.go` — opt-in on-disk index cache (`LoadIndex`/`SaveIndex`/`ClearIndex`): gob file, atomic temp+rename, mode 0600, secrets redacted on save (redaction results cached per session/mod-time), `PathInside` guard keeps it out of `~/.claude`. Bump `IndexVersion` whenever blob contents or redaction rules change
- `quickstats.go` — `QuickScanStats`: lightweight scan that counts tokens, cost and tool calls without parsing full message content; `ScanSessionMeta`

**TUI layer** (`internal/tui/`):
- `app.go` — the entire Bubble Tea `Model` / `Update` / `View` cycle. The model is a 3-level tree (Project → Session → Subagent). All data loading is async via `tea.Cmd` returning typed messages (`QuickStatsLoadedMsg`, `TranscriptLoadedMsg`, etc.).
- `searchindex.go` — `loadSearchIndexCmd` builds the content index in the background (`SearchIndexLoadedMsg`)
- `statusindex.go` — `renderIndexBar`: status line shown above the key-hint bar (cache on/off + path, index progress, save state, filter count); segments shrink then drop by priority so it never wraps. `Model.indexJobs` counts started-but-unfinished reindexes (use `startReindex()`, balanced by `SearchIndexLoadedMsg`)
- `options.go` — `Options` (index cache and pricing settings), `ResolveIndexCache` (flag/env resolution, also used for `--pricing-auto`), `PricingFetchOptions`
- Clipboard (in `app.go`): `clipboardCommand` is a pure OS/env tool selector (pbcopy / clip / wl-copy / xclip / xsel); `copyToClipboardCmd` runs it in the background with a 3 s timeout and the text only on stdin, reporting via `ClipboardCopiedMsg`
- `keys.go` — centralized key bindings; all key handling in `app.go` uses `key.Matches()` against this map.
- `message_render.go` — renders individual JSONL messages (user/assistant/tool) into styled strings
- `styles.go` — all Lip Gloss styles in one place
- `watcher.go` — live-tail: polls session file mod-time on a ticker, emits `sessionChangedMsg`

**Key data flow**: `LoadHistory` → `buildListItems` builds the flat `[]listItem` slice the list renders from. Expanding a session calls `ScanSubagents` (sync/cheap) plus `loadQuickStatsCmd` (async). Pressing `l` triggers `loadTranscriptCmd` (async, heavier) and enables the live-tail watcher.

**Search flow**: `buildListItems` lowercases the filter once and calls `data.MatchSession` once per session (precomputed `SessionInfo.SearchMeta` first, then the content blob from `Model.searchIndex`, keyed by session ID). `Init`, Ctrl+R and live-tail changes run `loadSearchIndexCmd` (worker pool, returns only new/changed entries as `SearchIndexLoadedMsg{Entries, Removed}`; the model is only mutated in `Update`). `Update` applies `Removed` (deleted transcripts), prunes entries for sessions no longer in history, and rebuilds the list with `rebuildListKeepingCursor` so rows appearing above the cursor do not move the selection. `renderSearchMatch` (detail pane) uses `data.MatchedFields` and `data.FindSnippets` to show why the selected session matched. With the cache enabled, `NewModel` seeds `searchIndex` from `LoadIndex`, and each `SearchIndexLoadedMsg` triggers a `saveIndexCmd` on a copy of the index; saves are coalesced (`saveInFlight`/`saveDirty`, cleared by `IndexSaveDoneMsg`).

**Resume flow**: `keys.Resume` (`r`) sets `confirmResume = true`. A `y` keypress sets `m.ResumeSessionID` + `m.ResumeProjectDir` and calls `tea.Quit`. `main.go` then runs `claude --resume <id>` with `cmd.Dir` set to the session's original project directory — the TUI must fully exit before Claude CLI takes over the terminal.
