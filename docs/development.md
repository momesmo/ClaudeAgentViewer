# Development guide and handoff

Read this before you commit or push. `CLAUDE.md` (loaded automatically) has the
short version; this has the detail and the reasons. State described here is as of
**2026-11-16**; run `git log --oneline -15` and `git status -sb` for what is true now.

## Where to look

| File | What it is |
|---|---|
| `README.md` | What claudevu does, flags, keybindings, pricing and cache behavior |
| `CLAUDE.md` | Architecture map, commands, test rules, working agreements |
| `docs/improvements.md` | **The backlog and decision log.** Open items have IDs (R-xx, C-xx); update it whenever you decide something |
| `docs/pricing.md` | Price layers, how costs are computed, fetching rules, `--pricing-info` |
| `docs/superpowers/plans/2026-11-13-deep-search.md` | Historical plan for the search and cache work. Fully implemented; do not re-execute |
| `.github/workflows/ci.yml` | CI (see below) |

## What exists

claudevu is a Bubble Tea TUI over Claude Code's local session data. Shipped so far:
the original browser/dashboard/live-tail/resume features, then on this branch:
content search with a background index and match snippets, an opt-in on-disk index
cache, a status bar for cache/index state, head+tail index blobs, broad secret
redaction, exact per-model pricing (each API message counted once and priced with
its own model), an opt-in vetted price refresh and `--pricing-info`, a fixture-based
test suite that never touches your real data, CI on three operating systems, and a
safe cross-platform clipboard.

## Branches and remotes

| Name | Meaning |
|---|---|
| `origin` | **momesmo/ClaudeAgentViewer**, the personal fork (public). All work is pushed here |
| `upstream` | HemantKumarMS/ClaudeAgentViewer, the original project. **Push is disabled** (`no_push_to_upstream`); never change that |
| `main` | Snapshot of upstream at `013c6d9`. Not modified by this work |
| `local-dev` | The working branch, pushed to `origin/local-dev` |
| `local-dev-backup-work-identity` | **Local only.** The same commits before the history rewrite, authored with the work identity. Never push it; delete it when no longer wanted (`git branch -D local-dev-backup-work-identity`) |

## Git and GitHub setup (lives OUTSIDE the repo)

Two GitHub identities are in use and must stay separate: the **work** account
(`anup-jasani`, the active `gh` account) and the **personal** account (`momesmo`).
Everything pushed here is personal. A commit carrying the work name or email must
never be pushed.

| Piece | Where | Effect |
|---|---|---|
| Identity rules | `~/.gitconfig` (3 `includeIf "hasconfig:remote.*.url:...momesmo/**"` blocks, last in the file) | Repos whose remote is under `github.com/momesmo` use `~/.gitconfig-personal` |
| Personal config | `~/.gitconfig-personal` | name `momesmo`, email `6090451+momesmo@users.noreply.github.com`, `commit.gpgsign=false`, credential helper that supplies momesmo's token |
| This repo's config | `.git/config` | Same identity and helper set locally too; `origin` URL has `momesmo@` in it |
| `gh` wrapper | `~/.zshrc` (`gh()` function) | In interactive zsh, `gh` inside a momesmo repo uses momesmo's token. **Not active in non-interactive shells**, so scripts and agents must set `GH_TOKEN` themselves (below) |
| Pre-commit hook | `core.hooksPath` -> ggshield | Scans staged changes for secrets on every commit |
| `gh` token scopes | gh keyring | momesmo has `repo`, `workflow`, `gist`, `read:org` |

Check it is intact: `git config user.email` must print the noreply address;
`git remote -v` must show `upstream` push = `no_push_to_upstream`; `git log -3 --format='%an <%ae>'`
must show only `momesmo <6090451+momesmo@users.noreply.github.com>`.

If the setup is missing (new machine), recreate it: add the three `includeIf` blocks
to the end of `~/.gitconfig` and a `~/.gitconfig-personal` with the identity above
and this credential helper (write it with `git config --file`, because git config
treats a bare `;` as a comment and strips bare quotes):

```
git config --file ~/.gitconfig-personal --add credential.https://github.com.helper ''
git config --file ~/.gitconfig-personal --add credential.https://github.com.helper \
  '!f() { test "$1" = get && echo username=momesmo && echo "password=$(gh auth token -h github.com -u momesmo)"; }; f'
```

### The secrets rules

- **Never bypass the ggshield hook** (`--no-verify`). If it flags a *fake* secret in a
  test, split the literal so no secret-shaped string appears in source:
  `"xo" + "xb-123..."`. GitHub's push protection rejects such strings too, and by then
  they are in history (which forces a rewrite).
- Never commit real session data, tokens, the work email, or anything from `~/.claude`.

## Pushing and watching CI

```bash
git push origin local-dev          # uses momesmo's token through the repo-local helper

# CI and other gh commands need momesmo's token explicitly (the active gh account is the work one):
export GH_TOKEN=$(gh auth token -h github.com -u momesmo)
gh run list -R momesmo/ClaudeAgentViewer --limit 5
gh run view <id> -R momesmo/ClaudeAgentViewer --json status,conclusion,jobs
gh run rerun <id> -R momesmo/ClaudeAgentViewer --failed
```

- `gh auth refresh` only refreshes the *active* account. To add scopes to momesmo:
  `gh auth switch -u momesmo; gh auth refresh -h github.com -s workflow; gh auth switch -u anup-jasani`.
- Pushing anything under `.github/workflows` needs momesmo's `workflow` scope (it has it).
- Do **not** push, merge, delete branches, change repo settings, or enable Actions unless
  the user asked. Commit locally and report.

## CI (`.github/workflows/ci.yml`)

Jobs: `test` (ubuntu, macos, windows: vet, build, `go test -race`, gofmt on ubuntu),
`cross-build` (linux/darwin/windows x amd64/arm64, `CGO_ENABLED=0`), `min-go`
(builds with the `go` line from `go.mod`, `GOTOOLCHAIN=local`), `vulncheck`
(blocking `govulncheck`). Actions are pinned to full commit SHAs with a version
comment; Dependabot updates them. `go.mod` pins `toolchain go1.27.4` (older patch
releases had reachable `net/http`/`crypto/tls` vulnerabilities).

First real run (2026-11-16, run 43946192515): everything passed except
`test (macos-latest)`, which never got a runner (GitHub's macOS arm64 capacity). A
re-run was queued. See R-43 in `docs/improvements.md`. Known wrinkle: push plus
pull_request runs twice on a PR branch (R-42).

## How work was done (keep doing it this way)

1. **Plan and decide** in `docs/improvements.md` (stable IDs, decision log).
2. **Implement in a git worktree** under `.claude/worktrees/<name>` (git-ignored), on a
   branch off `local-dev`. Remove the worktree and delete the branch after merging.
3. **Independent review** by a fresh reviewer who did not write the code, with a concrete
   brief (what must hold, what to try to break). Reviews found real defects every time:
   vacuous tests, an ignored vulnerability, a UI freeze, silently dropped data.
4. **Fix wave**, then **verify it yourself**; do not accept "all tests pass" from the author.
5. **Mutation testing** for test suites: deliberately break the code (remove a dedupe,
   change a rate, drop a guard) and confirm a test fails. A mutation nothing catches is a
   missing test.
6. Verify claims with evidence (run the command, read the file). Report faithfully:
   failures, skips and unverified items go in the report.
7. Update the docs in the same change: `CLAUDE.md` for architecture, `docs/improvements.md`
   for status, `docs/pricing.md`/README for user-visible behavior.

## Test and code rules

- `go vet ./... && go test -race -count=1 ./...`, and `gofmt -l $(git ls-files '*.go')` must be empty (CI enforces it).
- Tests use `internal/testfixture` and are isolated by `internal/testenv`; they must not
  read `~/.claude`, the user cache or config dirs, and must **never skip for missing data**.
  Platform-gated skips (POSIX modes, FIFOs) need a reason string.
- Cost expectations in tests are hand-derived (including literal USD amounts), not computed
  by the code under test. Changing the price table means updating those literals.
- Bump `IndexVersion` (`internal/data/indexstore.go`) whenever blob contents, the cap or the
  redaction rules change.
- Regenerate the embedded price table with the steps in `docs/pricing.md`; commit the dated
  `pricing_table.json` and the excerpt in `internal/data/testdata`.
- Product constraints: read-only against `~/.claude`; the only writes are the opt-in index
  cache and pricing cache/state (cache dir, never `~/.claude`); the only network use is the
  opt-in price refresh, and nothing about the user or their sessions is ever sent.

## Environment gotchas (learned the hard way)

- **zsh does not word-split unquoted variables.** `for spec in "a b c"; do set -- $spec` gives
  one argument. Use explicit variables or arrays.
- macOS has no `timeout` command. Foreground `sleep` is blocked in the agent harness; use
  background tasks and wait for the notification.
- Inside this module `go` auto-switches to the pinned toolchain (`go version` prints 1.27.4,
  not your installed version). `GOTOOLCHAIN=local` disables that (the min-go CI job uses it).
- `.claude/` holds worktrees and local settings and is git-ignored; `gofmt -l .` would
  descend into worktrees, so scope it to `git ls-files`.
- `gh api ... ` against a non-existent repo returns 404, which also happens when a shell
  variable was mangled; check the URL being sent before trusting a 404.
- A background poll loop that exceeds its tool timeout is moved to the background; read its
  output file when notified instead of starting a second copy.

## Open work

See `docs/improvements.md`. Notable: R-43 finish verifying CI (macOS job), R-42 duplicate CI
runs, R-04 whole-conversation search, R-03 auto-refresh of the session list, R-01 mark
sessions with no transcript, R-34 "Messages" counts transcript lines not API messages.
