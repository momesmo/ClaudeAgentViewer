# Pricing data

claudevu prices sessions from a price book (USD per million tokens, plus
published modifiers such as fast mode and US data residency).

## Layers

Later layers win, per model:

1. **Embedded** (`internal/data/pricing_table.json`, compiled into the binary).
   Records its `source` and `asOf`.
2. **Fetched cache** (`pricing-cache.v1.json`, in the cache directory), written
   only after an opted-in refresh. It is ignored if it fails validation or its
   `asOf` is older than the embedded table.
3. **Override file** (`pricing-overrides.json`, in the config directory),
   written by you. Always wins.

Corrupt or missing optional layers never cause an error: claudevu falls back to
the layer below and reports the reason (`LayerInfo.Problems`).

Other state: `pricing-state.json` (cache directory) records the last attempt,
last success, last error and attempt count so a failing fetch is not retried
on every run (cooldown via `PricingState.ShouldAttempt`).

Files are written atomically (temp file + rename), mode 0600, directories 0700.

## Override file

Same shape as `pricing_table.json`. `version` (must be 1) is required; `models`
and `modifiers` are optional, and a zero modifier means "not overridden". Each
model needs all five rates and obeys the same sanity rules as the embedded book
(positive, at most 1000 USD/MTok, cache write 1h >= 5m >= input, cache read <=
input). Model IDs look like `claude-opus-5-5`.

```json
{
  "version": 1,
  "modifiers": { "fastMultiplier": 2.0 },
  "models": {
    "claude-opus-5-5": {
      "input": 4, "cacheWrite5m": 5, "cacheWrite1h": 8,
      "cacheRead": 0.2, "output": 20
    },
    "claude-haiku-5-5": {
      "input": 0.1, "cacheWrite5m": 0.125, "cacheWrite1h": 0.2,
      "cacheRead": 0.01, "output": 0.5,
      "longContext": {
        "overTokens": 100000, "input": 0.5, "cacheWrite5m": 0.625,
        "cacheWrite1h": 1, "cacheRead": 0.05, "output": 2.5
      }
    }
  }
}
```

An invalid override file is ignored as a whole, with a warning. A missing file
is not a problem.

## Fetching

Nothing is fetched unless the user opts in. When they do, `FetchPricing` reads
the pricing page markdown
(`https://platform.claude.com/docs/en/about-claude/pricing.md`) under these
rules:

- HTTPS only; no credentials in the URL.
- The host must be in `AllowHosts` (default: platform.claude.com,
  docs.anthropic.com, claude.com, www.anthropic.com). A custom URL works only if
  the caller lists its host explicitly.
- Redirects: at most 3, same host, https only.
- Response capped at 2 MiB (error if exceeded); 10 s timeout.
- Only the header `User-Agent: claudevu-pricing/1` is sent. No cookies, auth or
  anything derived from your sessions or model names.
- Status must be 200 and Content-Type text (or markdown).
- Plausibility guard: the page must yield at least 15 models and at least 80%
  of the embedded model IDs, or the result is rejected (never a partial book).
- Failures write only the state file; the cache is never partially written.

## Regenerating the embedded table

1. Save the model table(s) of the pricing page to
   `internal/data/testdata/pricing_page_excerpt.md`.
2. Run:

   ```bash
   UPDATE_PRICING_TABLE=1 PRICING_AS_OF=2026-11-16 go test ./internal/data -run TestRegeneratePricingTable
   ```

3. Review the `git diff` of `pricing_table.json`, then run `go test ./...`.
