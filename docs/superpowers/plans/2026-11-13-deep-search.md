# Deep Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/` search match session content (all user prompts, assistant text, tool names, git branch, model) without making typing slower.

**Architecture:** Each session gets a lowercase "search blob" built once, in the background, and held in an in-memory map on the `Model` (keyed by session ID, with the file mod-time). `buildListItems` does one `strings.Contains` per session on cheap pre-lowercased metadata, then on the blob. Indexing runs as a `tea.Cmd` and reports back through a message, like `QuickStatsLoadedMsg`.

**Tech Stack:** Go, Bubble Tea, stdlib `testing`.

**Spec:** the conversation above (the user's "include more context into search without affecting speed" question and the 4-point approach: pre-computed blob, background indexing, content chosen by cost, cap + mod-time cache). No separate spec file.

## Global Constraints

- CLAUDE.md: claudevu is read-only against `~/.claude/`, with no writes and no network. **The in-memory index (Tasks A–E) is the default. The only permitted write is the opt-in disk cache (Task F), under `os.UserCacheDir()/claudevu/`, never under `~/.claude/`.** The cache is off unless `--index-cache` or `CLAUDEVU_INDEX_CACHE=1` is set.
- Blob content (cheap to moderate tier only): user prompts, assistant `text` blocks, `tool_use` tool names, git branch, model. **Exclude** `thinking` blocks and `tool_result` content.
- Blob cap: `data.MaxBlobBytes = 40 * 1024` per session, truncated after lowercasing.
- All search text is lowercase. The filter is lowercased once per keystroke.
- Never mutate `m.groups` or `SessionInfo` from a goroutine. Background results arrive only as `tea.Msg` values and are applied in `Update`.
- Run `go test -race ./...` before each commit.

## Review Focus

- **Empty or missing session file:** blob is `""`, no panic, session still matches on metadata.
- **Very long lines (>1 MB, e.g. big tool results):** indexing must not abort the whole session. Skip the line or use a large scanner buffer.
- **Malformed JSONL line:** skipped, and the rest of the file is still indexed.
- **Search typed before the index finishes:** metadata matches work immediately, and content matches appear once `SearchIndexLoadedMsg` arrives with the filter still active (the list is rebuilt).
- **Session file grows (live tail) or history reloads:** a stale blob is replaced when the mod-time changes, and the `groups` rebuild doesn't lose the index (it is keyed by session ID, not group index).

---

## Parallelization Map

| Wave | Task | Owns (only these files) | Depends on |
|------|------|-------------------------|-----------|
| 1 | A: blob builder | `internal/data/searchblob.go`, `internal/data/searchblob_test.go` | none |
| 1 | B: matcher + metadata | `internal/data/searchmatch.go`, `internal/data/searchmatch_test.go`, `internal/data/types.go`, `internal/data/history.go` | none |
| 2 | C: TUI index loading | `internal/tui/searchindex.go`, `internal/tui/searchindex_test.go`, `Update`/`Model` hunks in `internal/tui/app.go` | A, B |
| 2 | D: TUI matching + hint | `buildListItems` and list-row render hunks in `internal/tui/app.go` | B (and C's `Model.searchIndex` field, declared in the Interfaces block below) |
| 3 | E: perf guard | `internal/data/searchmatch_bench_test.go` | A, B |
| 3 | F: opt-in disk cache and flags | `internal/data/indexstore.go`, `internal/data/indexstore_test.go`, `internal/tui/options.go`, `main.go`, plus small `NewModel`/`Update` hunks in `internal/tui/app.go` | A, C |

- Wave 1: run A and B in parallel, each in its own git worktree (`isolation: "worktree"`). They touch disjoint files.
- Wave 2: C and D both edit `app.go`, but in different functions. Run them in parallel worktrees and merge C first, then D. Resolve any hunk conflicts by keeping both sides. Or run them sequentially if conflicts are a concern.
- Wave 3: E is tiny and can run alongside the wave-2 review. F needs C merged. Its `indexstore.go` half can start in wave 1, since it only needs the `IndexEntry` type from Shared Interfaces, but wiring waits for C.
- Merge order: A, B, C, D, E, F. Run `go build ./... && go test -race ./...` after each merge.
- The working tree already has uncommitted changes in `history.go`, `types.go`, `quickstats.go`, `app.go` and `main.go` (AgentName/CustomTitle/ResumeProjectDir). **Commit those first as a baseline** so the worktrees branch from them.

## Shared Interfaces (all tasks code against these exactly)

```go
// internal/data/searchblob.go  (Task A)
const MaxBlobBytes = 40 * 1024

type IndexEntry struct {
    ModTime time.Time
    Blob    string // lowercase, <= MaxBlobBytes, "" if unreadable
}

// BuildIndexEntry returns prev unchanged (changed=false) if the file's mod-time equals prev.ModTime
// and prev != nil. Otherwise it re-reads the file. A missing or unreadable file yields
// IndexEntry{} and changed=false.
func BuildIndexEntry(path string, prev *IndexEntry) (entry IndexEntry, changed bool)

// internal/data/searchmatch.go  (Task B)
type MatchKind int
const (
    MatchNone MatchKind = iota
    MatchMeta    // matched display / id / project / agent name / custom title
    MatchContent // matched only in the content blob
)

// FillSearchMeta sets s.SearchMeta = lowercase(Display, SessionID, Project, AgentName, CustomTitle)
// joined by "\x00". filterLower must be lowercase.
func (s *SessionInfo) FillSearchMeta()
func MatchSession(s SessionInfo, blob string, filterLower string) MatchKind
// An empty filter returns MatchMeta.
```

`SessionInfo` gains `SearchMeta string`.

```go
// internal/data/indexstore.go  (Task F)
const IndexVersion = 1 // bump whenever blob contents or MaxBlobBytes change

func DefaultCacheDir() (string, error) // os.UserCacheDir()/claudevu
// LoadIndex returns an empty map (never an error) if the file is missing, corrupt, from another
// IndexVersion, or built for a different claudeDir.
func LoadIndex(cacheDir, claudeDir string) map[string]IndexEntry
// SaveIndex writes atomically (temp file + rename), mode 0600 file / 0700 dir, and applies
// RedactSecrets to each blob before writing.
func SaveIndex(cacheDir, claudeDir string, idx map[string]IndexEntry) error
// ClearIndex deletes the cache file. A missing file is not an error (removed=false).
func ClearIndex(cacheDir string) (removed bool, err error)

// internal/tui/options.go  (Task F)
type Options struct {
    IndexCache bool
    CacheDir   string // "" means data.DefaultCacheDir()
}
// tui.NewModel changes to: NewModel(claudeDir string, opts Options) Model
```

```go
// internal/tui/searchindex.go  (Task C)
type SearchIndexLoadedMsg struct{ Entries map[string]data.IndexEntry } // keyed by SessionID; only changed or new entries
func loadSearchIndexCmd(sessions []data.SessionInfo, prev map[string]data.IndexEntry) tea.Cmd
// Model gains: searchIndex map[string]data.IndexEntry
```

---

### Task A: Search blob builder

**Files:**
- Create: `internal/data/searchblob.go`
- Test: `internal/data/searchblob_test.go`

**Interfaces:**
- Consumes: the `rawMessage` / `rawContentBlock` JSON structs in `internal/data/session.go` (same package; reuse them rather than redefining).
- Produces: `MaxBlobBytes`, `IndexEntry`, `BuildIndexEntry` (see Shared Interfaces).

- [ ] **Step 1: Write failing tests in `searchblob_test.go`.** Write small JSONL fixtures to `t.TempDir()`:
  - `TestBuildIndexEntry_IncludesUserAssistantTools`: a user prompt "Fix The Login Bug", an assistant text block "Checking auth", a `tool_use` named "Grep", and a message with `gitBranch` "feat/x" and model "claude-opus-5-5". Assert that `entry.Blob` contains `"fix the login bug"`, `"checking auth"`, `"grep"`, `"feat/x"` and `"claude-opus-5-5"`.
  - `TestBuildIndexEntry_ExcludesThinkingAndToolResult`: a `thinking` block "secretthought" and a `tool_result` "secretresult" are absent from the blob.
  - `TestBuildIndexEntry_CapsAndLowercases`: a 200 KB prompt gives `len(Blob) <= MaxBlobBytes` and `Blob == strings.ToLower(Blob)`.
  - `TestBuildIndexEntry_SkipsMalformedAndHugeLines`: a garbage line plus a 2 MB line, followed by a valid prompt "needle". "needle" is still in the blob.
  - `TestBuildIndexEntry_CacheByModTime`: a second call with `&entry` returns `changed=false`. After rewriting the file with a later mod-time (`os.Chtimes`), it returns `changed=true` with new content.
  - `TestBuildIndexEntry_MissingFile`: returns `IndexEntry{}, false`.
- [ ] **Step 2:** Run `go test ./internal/data/ -run BuildIndexEntry -v`. Expected: FAIL (undefined).
- [ ] **Step 3: Implement `BuildIndexEntry(path string, prev *IndexEntry) (IndexEntry, bool)` in `searchblob.go`.** Stat first for the mod-time check. Stream with `bufio.Reader.ReadLine`/`ReadBytes` rather than `Scanner`, so oversized lines are skipped and don't abort the scan. Join the parts with `\n`, stop appending once the running size reaches the cap, then truncate to `MaxBlobBytes` (on a rune boundary) after `strings.ToLower`.
- [ ] **Step 4:** Run `go test -race ./internal/data/ -run BuildIndexEntry -v`. Expected: PASS.
- [ ] **Step 5: Commit.** `git add internal/data/searchblob*.go && git commit -m "feat(search): build capped lowercase content blob per session"`

### Task B: Matcher and pre-lowercased metadata

**Files:**
- Create: `internal/data/searchmatch.go`
- Test: `internal/data/searchmatch_test.go`
- Modify: `internal/data/types.go` (add `SearchMeta string` to `SessionInfo`), `internal/data/history.go` (call `FillSearchMeta()` on each session after the `ScanSessionMeta` enrichment, before grouping)

**Interfaces:**
- Consumes: `SessionInfo`.
- Produces: `MatchKind`, `FillSearchMeta`, `MatchSession` (see Shared Interfaces).

- [ ] **Step 1: Write failing tests.**
  - `TestMatchSession_Meta`: a session with Display "Fix Login", filter "login" gives `MatchMeta`. Also check matches on the session ID, project, agent name and custom title.
  - `TestMatchSession_ContentOnly`: filter "needle" with blob "...needle..." and no metadata match gives `MatchContent`.
  - `TestMatchSession_None`: no match anywhere gives `MatchNone`.
  - `TestMatchSession_EmptyFilter`: returns `MatchMeta`.
  - `TestMatchSession_NoCrossFieldMatch`: Display "ab" and Project "cd", filter "bc" gives `MatchNone` (the `\x00` separator prevents false hits across fields).
  - `TestLoadHistory_FillsSearchMeta`: a temp `history.jsonl`, then `LoadHistory`, and every session has a non-empty lowercase `SearchMeta`.
- [ ] **Step 2:** Run `go test ./internal/data/ -run 'MatchSession|SearchMeta' -v`. Expected: FAIL.
- [ ] **Step 3: Implement `FillSearchMeta` and `MatchSession`** in `searchmatch.go`. Add the field to `types.go`. In `history.go`, call `info.FillSearchMeta()` inside the existing enrichment loop. It must run for every session, including those where `FindSessionFile` returns `""`, so restructure the loop slightly.
- [ ] **Step 4:** Run `go test -race ./internal/data/ -v`. Expected: PASS.
- [ ] **Step 5: Commit.** `git add internal/data && git commit -m "feat(search): precomputed lowercase metadata and MatchSession"`

### Task C: Background indexing in the TUI

**Files:**
- Create: `internal/tui/searchindex.go`
- Test: `internal/tui/searchindex_test.go`
- Modify: `internal/tui/app.go` (`Model` struct, `NewModel`/init, `Update`)

**Interfaces:**
- Consumes: `data.BuildIndexEntry`, `data.IndexEntry`, `data.SessionInfo.FilePath`.
- Produces: `SearchIndexLoadedMsg`, `loadSearchIndexCmd`, and the `Model.searchIndex map[string]data.IndexEntry` field (initialized to an empty map in the constructor).

- [ ] **Step 1: Write failing tests.**
  - `TestLoadSearchIndexCmd_ReturnsOnlyChanged`: temp session files and a `prev` map in which one entry is current. Run the cmd (`msg := cmd().(SearchIndexLoadedMsg)`). Assert that `Entries` has only the new or modified sessions.
  - `TestLoadSearchIndexCmd_SkipsEmptyFilePath`: a session with `FilePath == ""` is not in the result.
  - `TestUpdate_SearchIndexLoadedMsg_MergesAndRebuilds`: send the message to a `Model` with an active filter. The entries are merged into `m.searchIndex`, and `m.listItems` is rebuilt with `m.searchInput.Value()`.
- [ ] **Step 2:** Run `go test ./internal/tui/ -run SearchIndex -v`. Expected: FAIL.
- [ ] **Step 3: Implement `loadSearchIndexCmd`** in `searchindex.go`. It should flatten the sessions and build entries with a small worker pool (`runtime.NumCPU()` goroutines, results collected under a mutex into a plain map, with no shared state escaping). In `app.go`:
  - add the `searchIndex` field and initialize it;
  - return `loadSearchIndexCmd` from `Init` (batched with existing cmds);
  - handle `SearchIndexLoadedMsg` in `Update`: merge the entries, then `m.buildListItems(m.searchInput.Value())`;
  - re-issue the cmd after history reload (`historyChangedMsg` handling) and when `sessionChangedMsg` fires for the open session, passing the current `m.searchIndex` as `prev`.
- [ ] **Step 4:** Run `go test -race ./internal/tui/ -v`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(search): index session content in the background"` (plus the new files).

### Task D: Use the index in matching, with a match hint

**Files:**
- Modify: `internal/tui/app.go` (`buildListItems` at ~line 173; the session-row render function)
- Test: `internal/tui/search_match_test.go`

**Interfaces:**
- Consumes: `data.MatchSession`, `data.MatchKind`, `Model.searchIndex` (declared by Task C; if C isn't merged yet, add only the field declaration and let the merge dedupe it).
- Produces: `listItem` gains `matchKind data.MatchKind`, which the row renderer uses to append a dim `[content]` hint when the kind is `MatchContent`.

- [ ] **Step 1: Write failing tests** (build a `Model` with hand-made `groups` and `searchIndex`):
  - `TestBuildListItems_ContentMatchShowsSession`: filter "needle", where the session's blob contains it. The project and session items appear, with `matchKind == MatchContent`.
  - `TestBuildListItems_NoMatchHidesProject`: no meta or blob match leaves the project absent.
  - `TestBuildListItems_MetaMatchPreferred`: a meta match gives `MatchMeta`, not `MatchContent`.
  - `TestBuildListItems_EmptyFilterUnchanged`: with no filter, the output equals the previous behavior (all projects, and sessions when expanded).
  - `TestBuildListItems_MissingIndexEntryStillMatchesMeta`: a session with no index entry yet still matches on metadata.
- [ ] **Step 2:** Run `go test ./internal/tui/ -run BuildListItems -v`. Expected: FAIL.
- [ ] **Step 3: Rewrite the matching in `buildListItems`.** Lowercase the filter once. For each session call `data.MatchSession(s, m.searchIndex[s.SessionID].Blob, filter)` once, and reuse the result for both the project-level `hasMatch` check and the per-session filter. This replaces the duplicated `strings.Contains(strings.ToLower(...))` chains. Store the kind on the `listItem`. In the session-row renderer, append a muted `[content]` suffix (style in `styles.go`) when `matchKind == MatchContent`.
- [ ] **Step 4:** Run `go test -race ./internal/tui/ -v`. Expected: PASS.
- [ ] **Step 5: Commit.** `git commit -am "feat(search): match on content blob and show hint"`

### Task E: Performance guard

**Files:**
- Create: `internal/data/searchmatch_bench_test.go`

**Interfaces:**
- Consumes: `MatchSession`, `SessionInfo.FillSearchMeta`.

- [ ] **Step 1: Write `BenchmarkMatchSession_1000x40KB`** (1000 sessions, 40 KB random-ish lowercase blobs, a filter that matches none) and **`TestMatchSession_ScanBudget`**, which runs one full pass over those 1000 sessions and fails if it takes over 100 ms.
- [ ] **Step 2:** Run `go test ./internal/data/ -run ScanBudget -bench MatchSession -benchmem`. Expected: PASS, with the pass in low single-digit milliseconds. If it fails, report the number rather than raising the budget.
- [ ] **Step 3: Commit.** `git add internal/data/searchmatch_bench_test.go && git commit -m "test(search): guard keystroke scan latency"`

---

### Task F: Opt-in disk cache, `--index-cache`, `--clear-cache`, `CLAUDEVU_INDEX_CACHE`

**Files:**
- Create: `internal/data/indexstore.go`, `internal/data/indexstore_test.go`, `internal/tui/options.go`, `internal/tui/options_test.go`
- Modify: `main.go`, `internal/tui/app.go` (`NewModel` signature, `Model.opts`, `Init`, the `SearchIndexLoadedMsg` case), README (new flags and the privacy note), `CLAUDE.md` (replace "no writes" with "no writes except the opt-in index cache")

**Interfaces:**
- Consumes: `data.IndexEntry` (A), `Model.searchIndex` and `SearchIndexLoadedMsg` (C), `data.RedactSecrets(text string) string` (`privacy.go`).
- Produces: `LoadIndex`, `SaveIndex`, `ClearIndex`, `DefaultCacheDir`, `IndexVersion`, `tui.Options`, and the new `NewModel(claudeDir, opts)` signature.

**CLI behavior (decided, not open):**
- `--index-cache` (bool, default false) enables the cache.
- `CLAUDEVU_INDEX_CACHE` enables it when set to `1` or `true` (case-insensitive). Any other value, or unset, leaves it off. The explicit flag wins: `--index-cache=false` disables it even if the env var is set (detect with `flag.Visit`).
- `--cache-dir <path>` overrides the location (default `data.DefaultCacheDir()`).
- `--clear-cache` deletes the cache file in the resolved cache dir, prints `Removed <path>` or `No cache found at <path>`, and exits 0 **without starting the TUI**. It works whether or not `--index-cache` is set.
- If `os.UserCacheDir()` fails and no `--cache-dir` is given, print a warning to stderr and run with the cache disabled. This is never fatal.

- [ ] **Step 1: Write failing tests for the store** in `indexstore_test.go`, all with `t.TempDir()` for both dirs:
  - `TestSaveLoadIndex_RoundTrip`: save two entries, load them, and they are equal (compare `ModTime` with `Equal`).
  - `TestLoadIndex_MissingCorruptVersionMismatch`: no file, a file of garbage bytes, and a file saved with a different `IndexVersion` each give an empty non-nil map and no panic.
  - `TestLoadIndex_DifferentClaudeDir`: saved for `/a/.claude`, loaded for `/b/.claude`, gives an empty map.
  - `TestSaveIndex_RedactsSecrets`: a blob containing a string `RedactSecrets` rewrites (copy a known-redacted sample from the existing privacy tests, or from the patterns in `privacy.go`) is stored redacted. Assert on the loaded blob.
  - `TestSaveIndex_PermsAndAtomic`: the file mode is `0600`, the dir is `0700`, and no `*.tmp` is left behind after success.
  - `TestClearIndex`: returns `(true, nil)` after a save, then `(false, nil)` on a second call.
- [ ] **Step 2: Write failing tests for option resolution** in `options_test.go`. Put the flag/env resolution in a pure function in `internal/tui/options.go`: `ResolveIndexCache(flagSet, flagVal bool, env string) bool`. Cases: flag unset + env `1` is true; flag unset + env `TRUE` is true; flag unset + env `0` or `""` is false; flag set true + env `""` is true; flag explicitly false + env `1` is false.
- [ ] **Step 3:** Run `go test ./internal/data/ ./internal/tui/ -run 'Index|ResolveIndexCache' -v`. Expected: FAIL.
- [ ] **Step 4: Implement `indexstore.go`.** The file is `search-index.v1.gob` (the `v1` follows `IndexVersion`), with a gob header `{Version int; ClaudeDir string}` followed by the map. Write to `os.CreateTemp(cacheDir, "search-index-*.tmp")`, chmod `0600`, then `os.Rename`. Implement `ResolveIndexCache` in `options.go`.
- [ ] **Step 5: Wire `main.go`.** Define the three flags, call `flag.Visit` to see whether `index-cache` was set, and resolve the cache dir. Handle `--clear-cache` before the `~/.claude` existence check (it doesn't need Claude installed). Then call `tui.NewModel(claudeDir, tui.Options{...})`.
- [ ] **Step 6: Wire `app.go`.**
  - In `NewModel`, store `opts`. When `opts.IndexCache` is on, seed `m.searchIndex = data.LoadIndex(cacheDir, claudeDir)` before the first render (a synchronous load is fine: one file read).
  - Add a `saveIndexCmd(cacheDir, claudeDir string, idx map[string]data.IndexEntry) tea.Cmd`. It takes a **copy** of the map, so the goroutine never reads the live `m.searchIndex`.
  - In the `SearchIndexLoadedMsg` case, after merging, return `saveIndexCmd` when `opts.IndexCache` is on and `len(msg.Entries) > 0`. A save error is shown with `m.setStatus`, not fatal.
  - **Drop entries for sessions that no longer exist** from the saved copy (sessions absent from `m.groups`).
  - When the cache is off, nothing in `app.go` touches the disk.
- [ ] **Step 7: Add integration tests.**
  - `TestNewModel_CacheOff_DoesNotTouchDisk`: with `Options{IndexCache:false, CacheDir: dir}` and a populated `dir`, `m.searchIndex` is empty and `dir` is unchanged.
  - `TestNewModel_CacheOn_SeedsIndex`: with a saved index in `dir`, `NewModel` seeds `m.searchIndex`.
  - `TestUpdate_IndexLoaded_SavesWhenEnabled`: sending `SearchIndexLoadedMsg` with entries returns a non-nil cmd when the cache is on and a nil save cmd when it is off.
- [ ] **Step 8:** Run `go build ./... && go test -race ./...`. Expected: PASS. Manually check:
  - `go run . --index-cache`, quit, and the file exists in the cache dir with mode `-rw-------`.
  - `go run . --clear-cache` prints `Removed ...` and the file is gone.
  - `go run . --clear-cache` again prints `No cache found at ...`.
  - `CLAUDEVU_INDEX_CACHE=1 go run .` writes the cache.
  - `CLAUDEVU_INDEX_CACHE=1 go run . --index-cache=false` does not.
- [ ] **Step 9: Commit.** `git add -A internal main.go README.md CLAUDE.md && git commit -m "feat(search): opt-in on-disk index cache with --index-cache/--clear-cache"`

---

## Final verification (after all merges)

- [ ] `go build ./... && go test -race ./...` is green.
- [ ] Manual: `go run .`, press `/`, and type a phrase from the middle of an old conversation. Confirm the session appears with `[content]`, and that typing stays responsive.
- [ ] Run `go run . --clear-cache` to leave the machine clean after manual testing.
- [ ] Update the README search section and the `/` help text in `app.go` (~line 1247) to say search covers content.

## Self-review notes

- Coverage: the four points map to B/D (blob and fast matching), C (background), A (content tiers), and A/C (cap and mod-time cache). Debounce was dropped because Task E shows it isn't needed.
- Decision flagged: the disk cache is opt-in (Task F), because CLAUDE.md says claudevu is read-only. Without the opt-in, each launch re-indexes in parallel in the background. With it, the cache holds a second copy of conversation text, redacted with `RedactSecrets` and stored with `0600` permissions. Searching for a redacted secret therefore matches only after a fresh re-index in the same run.
- Types match across tasks: `IndexEntry`, `MatchKind`, `SearchIndexLoadedMsg` and `searchIndex` are used with identical names and signatures.
