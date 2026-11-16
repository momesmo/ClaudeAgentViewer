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
is not a problem. An override for a model that has a long-context tier (Claude
Haiku 5.5) keeps that tier unless you supply your own, so long prompts are not
silently under-priced.

## How costs are computed

- Each API message is counted **once**. Claude Code writes one transcript line
  per content block (thinking, text, each tool call) and every line repeats the
  whole message's usage, so lines sharing a `message.id` are deduplicated.
- Each message is priced with **its own model**, so a session that mixes models
  is costed correctly. Cache writes are priced at the 5-minute or 1-hour rate
  using the per-TTL breakdown in the transcript.
- Published modifiers stack on top: fast mode (`speed: "fast"`, x2), US-only
  inference (`inference_geo: "us"`, x1.1, Claude 4.6 and later), Claude Haiku
  5.5's higher rates above 100,000 prompt tokens, and web searches ($10 per
  1,000).
- Model matching: exact ID, then after stripping a date snapshot, `-latest`,
  `[1m]`, a provider prefix (`anthropic.`, `us.anthropic.`) or a Bedrock
  `-v1:0` suffix. An unknown version of a known family (for example
  `claude-opus-5-9`) is priced like the nearest known model and shown with a
  `~` as an estimate. A model with no match at all (a non-Claude model, say) is
  **unpriced**: it is not counted as free. Costs show a `+` when some tokens
  could not be priced, and the detail pane lists the models.

## Fetching (opt-in)

Nothing is fetched unless you opt in, in one of two ways:

- `claudevu --update-pricing` fetches once, prints what changed, and exits.
- `--pricing-auto` (or `CLAUDEVU_PRICING_AUTO=1`) lets the TUI refresh once in
  the background when it meets a model without an exact price, and no more than
  once every 24 hours (failures and refusals count as attempts).

`--pricing-source <https URL>` selects a different page; it is trusted only for
its own host. Nothing but the cache and attempt-state files is written, in the
cache directory (never `~/.claude`). `--clear-cache` removes them too.

`FetchPricing` reads the pricing page markdown
(`https://platform.claude.com/docs/en/about-claude/pricing.md`) under these
rules:

- HTTPS only; no credentials in the URL; ASCII host names only.
- The host must be in the allowlist (default: platform.claude.com only). A plain
  entry allows port 443; an entry like `host:8443` allows exactly that port.
- Redirects: at most 3, same host, https only.
- Response capped at 2 MiB, enforced on the decompressed body; 10 s timeout.
- Only the header `User-Agent: claudevu-pricing/1` is sent. No cookies, auth or
  anything derived from your sessions or model names.
- Status must be 200 and Content-Type `text/markdown`, `text/plain` or
  `application/(x-)markdown` (or absent).
- The table is read by **header name**, not column position, and price cells
  must be plain `$X / MTok` numbers (locale commas such as `$0,5` are rejected).
- Plausibility: at least 15 models and at least 80% of the embedded model IDs;
  any model not in the embedded table must have a believable price structure
  (cache write 1.1-1.5x input, 1h write 1.5-2.5x, cache read 0.01-0.25x, output
  2-10x, input between $0.02 and $200 per million).

### Vetting before anything is applied

`RefreshPricingVetted` compares the fetched table with the table currently in
use and **refuses** (counts it as an attempt, writes no cache, tells you how to
review) when:

- any existing model's price moves by more than **3x** in either direction, or
- more than **25%** of the models you already have change at all.

Review the changes with `claudevu --update-pricing`; if they are right, apply
them with `--update-pricing --accept-price-changes`.

### Local files are read defensively

The cache, state and override files must be regular files of at most 1 MiB;
FIFOs, devices and huge files are reported as problems and ignored, so a bad
file can never hang or crash startup. A cache dated in the future, or older than
the built-in table, is ignored.

## Regenerating the embedded table

1. Save the model table(s) of the pricing page to
   `internal/data/testdata/pricing_page_excerpt.md`.
2. Run:

   ```bash
   UPDATE_PRICING_TABLE=1 PRICING_AS_OF=2026-11-16 go test ./internal/data -run TestRegeneratePricingTable
   ```

3. Review the `git diff` of `pricing_table.json`, then run `go test ./...`.
