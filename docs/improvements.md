# claudevu improvements checklist

A living list of improvement ideas. Edit it as decisions are made.

**Status markers**

- `[ ]` open (not started)
- `[~]` deferred: say why and when to revisit, in the item's note
- `[>]` in progress
- `[x]` done: add the commit or date in the note
- `[-]` won't do: add the reason in the note

**Rules of thumb**

- IDs are stable. Never renumber; append new items at the end of a section.
- When you change a status, add one line to the **Decision log** at the bottom.
- Effort: **S** (under half a day), **M** (a day or two), **L** (more).
- Priority: **H** / **M** / **L**.

_Last updated: 2026-11-13_

---

## 1. Trust and accuracy

- [ ] **R-01 (H, S) Mark sessions with no transcript.**
  History lists 213 sessions but only 27 have a transcript file; the other 186 show in the tree with nothing behind them, and the dashboard counts 27. Dim them, label "transcript missing", add a toggle to hide them. Check that resume is handled for them.
- [ ] **R-02 (H, M) Per-version pricing with a visible fallback.**
  `pricing.go` matches only the substring "opus"/"sonnet"/"haiku" and prices unknown models as Sonnet. Use an exact model table; show "price unknown" instead of guessing; allow an override file. Re-verify the README pricing table against current published rates.
- [ ] **R-16 (H, M) Tests that don't depend on the real `~/.claude`.**
  The root-package tests read live data, which hid the `truncate` panic and makes results vary by machine. Move them to a fixture directory.
- [ ] **R-17 (M, S) `gofmt` the repo and split `app.go`** (about 1,600 lines) into model / update / render / commands. Do the formatting pass in its own commit so diffs stay readable.
- [ ] **R-18 (M, M) CI and cross-platform checks.** A workflow running `go vet`, `go test -race` and a build on macOS, Linux and Windows. Add Wayland (`wl-copy`) to the clipboard code.

## 2. Search

- [ ] **R-04 (H, M) Search the whole conversation.**
  The 256 KB index drops the middle of very long sessions. Options: an on-demand "search this session's full transcript" (`n`/`N` through matches in the log view), or a compact term index.
- [ ] **R-05 (M, M) Search operators:** `project:`, `agent:`, `after:`, `cost>`, quoted phrases, and a case-sensitive toggle.
- [ ] **R-07 (M, M) In-transcript search and navigation:** `/` inside the log view, jump to next/previous message, collapse tool calls.
- [ ] **R-24 (M, S) Show the matching text in list rows**, not only the `[content]` tag (highlight the match in the displayed prompt).
- [ ] **R-25 (L, M) Say what kind of message matched** ("in a prompt" / "in a reply") in the snippets. Needs the index to record message kind.
- [ ] **R-26 (M, M) Make subagent transcripts searchable.** They are not indexed today.
- [ ] **R-27 (M, M) Snippets in original case.** The index is lowercase, so snippets are lowercase; showing original text means re-reading the transcript for the selected session only.

## 3. Navigation and workflow

- [ ] **R-03 (H, S) Auto-refresh the session list.** Start `watchHistoryCmd` (or poll the projects folder) so new sessions appear without `Ctrl+R`. This also gives the currently dead `historyChangedMsg` handler a purpose. If wired up, the handler must also reload history, not only re-index.
- [ ] **R-06 (M, S) Sort and group options** (date, cost, tokens, name) and a project-level cost roll-up.
- [ ] **R-08 (M, M) Export** a session or search result to Markdown or JSON, with privacy-mode redaction applied.
- [ ] **R-09 (L, S) Copy more than the session ID:** the resume command, project path, or selected message.
- [ ] **R-10 (L, S) Open in editor / file manager**; show git branch and working directory per session.
- [ ] **R-11 (L, M) Remember flags and expanded folders** between launches (opt-in, stored next to the cache).

## 4. Insight

- [ ] **R-12 (M, M) Cost over time:** daily/weekly charts in the dashboard and a week-over-week line.
- [ ] **R-13 (L, S) Cache efficiency:** the share of input served from cache, per session.
- [ ] **R-14 (L, M) Tool-usage breakdown:** most-used tools, error rates, sessions with many retries.
- [ ] **R-15 (L, S) Budget alerts:** configurable daily or per-session cost threshold in the status bar.

## 5. Index and cache robustness

- [ ] **R-19 (M, M) Config file** (TOML under the user config dir) for cache settings, pricing overrides, default sort and key bindings. Flags and env vars still win.
- [ ] **R-20 (M, M) Smarter reindexing:** reindex only the changed session on `Ctrl+R`; cancel stale reindexes (repeated `Ctrl+R` starts overlapping jobs, and an older one can finish last and overwrite a newer entry); optionally `--cache-status` and a manual "rebuild index" key.
- [ ] **R-28 (L, S) Per-session indexing progress** in the index bar (needs the index job to report as it goes; today it shows `indexing…`).
- [ ] **R-29 (L, S) Size ceiling on `LoadIndex`:** stat the cache file and ignore it above a limit so a huge or hostile file cannot slow startup.
- [ ] **R-30 (L, S) Another instance's temp file:** the temp-file sweep can delete a second running instance's in-progress save (costs one save). Use a per-process temp name prefix.
- [ ] **R-31 (L, S) `fsync` before the cache rename** (after a crash an empty file is currently treated as "rebuild").
- [ ] **R-32 (L, S) Load history lazily:** `LoadHistory` reads each session file's metadata before the first paint (82 ms for 213 sessions today, growing with history). Move the per-file metadata scan off the startup path.

## 6. Privacy and security

- [ ] **R-21 (M, M) Redaction coverage:** entropy-based detection for unknown token formats, short or unlabeled passwords, and an editable allow/deny list. Known gaps: secrets split at a truncation point; the generic `token:`/`password=` rule can over-redact code.
- [ ] **R-22 (M, S) Privacy mode for the index bar and snippets.** Search snippets and the cache path are shown unredacted.
- [ ] **R-23 (L, L) Hash-only cache option:** store word hashes so the cache holds nothing readable (exact-phrase search would be lost).
- [ ] **R-33 (L, S) Make `RedactSecretsFold` harder to misuse.** It silently assumes lowercase input; add a guard or fold internally.

## 7. Small deferred cleanups (from earlier reviews)

- [ ] **C-01 (L, S)** Unchecked `os.Chtimes` error in `TestBuildIndexEntry_CacheByModTime`; other tests ignore `SaveIndex`/`os.Stat` errors; one `t.Setenv` is a no-op.
- [ ] **C-02 (L, S)** `MatchSession` silently fills `SearchMeta` when empty, which hides a missed `FillSearchMeta` call.
- [ ] **C-03 (L, S)** A filter containing `\x00` could match across metadata fields.
- [ ] **C-04 (L, S)** Benchmark uses a best-case no-match filter (`#nomatch#`); add a realistic-miss filter. The scan-budget test uses 40 KB blobs, not the 256 KB cap. Single wall-clock timing samples could flake under `-race` (use best-of-3).
- [ ] **C-05 (L, S)** No test that the `[content]` hint renders; no test for stale-entry dropping in `liveIndexCopy` or a save error reaching the status line; no symlink case in the `PathInside` test.
- [ ] **C-06 (L, S)** Stale "cache save failed" status can linger after a later successful save; redundant `IndexCache` check in the save-done handler; a pending dirty save is lost on quit.
- [ ] **C-07 (L, S)** `IndexPath` and `indexPath` duplicate each other; `--clear-cache` exits 1 when no cache directory can be determined.

## 8. Won't do / rejected

_(none yet)_

---

## Completed

- [x] **Opt-in on-disk index cache** with `--index-cache`, `--clear-cache`, `--cache-dir`, `CLAUDEVU_INDEX_CACHE`; redaction on save; `--cache-dir` refused inside `~/.claude`. _(deep-search work, 2026-11-13)_
- [x] **Content search with a background index** and a `[content]` hint. _(2026-11-13)_
- [x] **Index status bar** (cache on/off and path, index progress, save state, filter count), shown above the key hints; pane height overflow fixed. _(`4c2aedd`, reordered in `9fa13da`)_
- [x] **Broader secret redaction** (word boundaries, more token formats, contextual rules) with per-session redaction caching; README privacy section. _(`d66fe9a`)_
- [x] **Head + tail index blobs** (256 KB cap), branch/model written only on change, oversized prompts salvaged instead of skipped. _(`bf11b35`)_
- [x] **Stale index entries dropped** (deleted transcripts and sessions gone from history), **cursor stays on the same row** when index updates arrive, **detail pane explains a match** (fields, count, snippets). _(`cfa5044`)_
- [x] **Fixed:** `truncate` panic on negative width; `TestSubagentTreeView` (help text); the row-wrap test that never failed.

---

## Decision log

Add one line per change: date, item ID, what changed, why.

- 2026-11-13: List created from a code review of the app.
