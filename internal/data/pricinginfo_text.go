package data

import (
	"fmt"
	"os"
	"strings"
)

func tilde(p string) string {
	if h, err := os.UserHomeDir(); err == nil && h != "" && strings.HasPrefix(p, h) {
		return "~" + strings.TrimPrefix(p, h)
	}
	return p
}

func rateCell(v float64) string { return fmt.Sprintf("%8s", money(v)) }

// Text renders the report as plain text for a terminal.
func (r PricingReport) Text() string {
	var sb strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&sb, format+"\n", a...) }
	h := func(title string) { w("\n%s\n%s", title, strings.Repeat("-", len(title))) }

	w("claudevu pricing information")
	w("Prices in use: %d models, table dated %s. Costs are estimates from published rates.", len(r.Book.Models), r.Book.AsOf)

	h("Price layers (a later layer overrides an earlier one, model by model)")
	for i, f := range r.Files {
		name := map[string]string{OriginBuiltIn: "built-in table", OriginCache: "fetched cache", OriginOverride: "your overrides"}[f.Layer]
		w("  %d. %-15s %s", i+1, name, strings.ToUpper(map[bool]string{true: "in use", false: "not used"}[f.InUse]))
		w("     file:   %s", tilde(f.Path))
		w("     status: %s", f.Status)
		if f.Exists && f.Layer != OriginBuiltIn {
			w("     file is %d bytes, modified %s", f.Size, f.ModTime.Local().Format("2006-01-02 15:04"))
		}
		if f.InUse {
			w("     dated %s, %d models", f.AsOf, f.Models)
		}
	}

	h("Problems")
	if len(r.Problems) == 0 {
		w("  none")
	}
	for i, p := range r.Problems {
		w("  %d. [%s] %s", i+1, p.Layer, p.Message)
		w("     fix: %s", p.Fix)
	}

	h("Refresh policy")
	if r.Policy.Auto {
		w("  automatic refresh: ON (%s)", r.Policy.AutoVia)
	} else {
		w("  automatic refresh: OFF. claudevu never uses the network unless you ask.")
		w("    enable with --pricing-auto or CLAUDEVU_PRICING_AUTO=1, or refresh once with --update-pricing")
	}
	if r.Policy.URL != "" {
		w("  source: %s", r.Policy.URL)
		hosts := "platform.claude.com (default)"
		if len(r.Policy.AllowHosts) > 0 {
			hosts = strings.Join(r.Policy.AllowHosts, ", ")
		}
		w("  allowed hosts: %s; https only, 2 MiB cap, no cookies or credentials, nothing about you is sent", hosts)
	}
	switch {
	case r.State.LastAttempt.IsZero():
		w("  last attempt: never")
	default:
		w("  last attempt: %s (%d failed or refused attempts since the last success)", r.State.LastAttempt.Local().Format("2006-01-02 15:04"), r.State.Attempts)
		if !r.State.LastSuccess.IsZero() {
			w("  last success: %s", r.State.LastSuccess.Local().Format("2006-01-02 15:04"))
		}
		if r.State.LastError != "" {
			w("  last error:   %s", r.State.LastError)
		}
	}
	if r.Policy.Auto {
		if r.NextAutoAttempt.IsZero() {
			w("  next automatic attempt: allowed now (only when a model has no exact price)")
		} else {
			w("  next automatic attempt: not before %s", r.NextAutoAttempt.Local().Format("2006-01-02 15:04"))
		}
	}
	w("  safety: a refresh that moves any price more than 3x, or changes more than 25%% of the models, is refused until you")
	w("          review it with --update-pricing and apply it with --accept-price-changes")

	h("How prices are applied")
	m := r.Book.Modifiers
	w("  - Every API message is counted once (Claude Code logs it as several lines) and priced with its own model.")
	w("  - Cache writes use the 5-minute or 1-hour rate, from the split recorded in the transcript.")
	w("  - fast mode (speed=fast): x%g  [%s]", m.FastMultiplier, r.ModifierOrigins["fastMultiplier"])
	w("  - US-only inference (inference_geo=us): x%g on Claude 4.6 and later  [%s]", m.USGeoMultiplier, r.ModifierOrigins["usGeoMultiplier"])
	w("  - web search: $%g per 1,000 searches  [%s]", m.WebSearchPer1000, r.ModifierOrigins["webSearchPer1000"])
	w("  - Models with a long-context tier (Claude Haiku 5.5) use it when one request's prompt exceeds the threshold.")
	w("  - An unknown version of a known family is priced like the nearest known version and shown with ~ (estimate).")
	w("  - A model with no match is NOT priced: its tokens are left out of the cost and the total shows +.")

	h("Model prices ($ per million tokens)")
	w("  %-20s %8s %8s %8s %8s %8s  %s", "model", "input", "5m write", "1h write", "cache rd", "output", "source")
	for _, ms := range r.Models {
		p := ms.Price
		w("  %-20s %s %s %s %s %s  %s", ms.ID, rateCell(p.Input), rateCell(p.CacheWrite5m), rateCell(p.CacheWrite1h), rateCell(p.CacheRead), rateCell(p.Output), ms.Origin)
		if ms.Note != "" {
			w("  %-20s   -> %s", "", ms.Note)
		}
		if lc := p.LongContext; lc != nil {
			w("  %-20s %s %s %s %s %s  (prompts over %s tokens)", "  long-context", rateCell(lc.Input), rateCell(lc.CacheWrite5m), rateCell(lc.CacheWrite1h), rateCell(lc.CacheRead), rateCell(lc.Output), commas(lc.OverTokens))
		}
	}

	h("Models in your sessions")
	if len(r.Usage) == 0 {
		w("  (not scanned, or no usage found)")
	} else {
		w("  %-24s %9s %10s  %s", "model", "messages", "cost", "priced as")
		src := map[string]string{}
		for _, ms := range r.Models {
			src[ms.ID] = ms.Origin
		}
		var advice []string
		for _, u := range r.Usage {
			cost, how := "", ""
			switch u.Match {
			case PriceExact, PriceNormalized:
				cost = fmt.Sprintf("$%.2f", u.CostUSD)
				how = fmt.Sprintf("%s (%s)", u.ResolvedID, src[u.ResolvedID])
				if u.Match == PriceNormalized {
					how += ", after removing a date or provider prefix"
				}
			case PriceFamily:
				cost = fmt.Sprintf("~$%.2f", u.CostUSD)
				how = fmt.Sprintf("ESTIMATE from the nearest known model, %s", u.ResolvedID)
				advice = append(advice, fmt.Sprintf("%s has no price of its own; add it to your overrides or run --update-pricing once it is published.", u.Model))
			default:
				cost = "not priced"
				how = "no price known: its tokens are left out of every cost"
				if modelKeyRE.MatchString(u.Model) {
					advice = append(advice, fmt.Sprintf("%s has no price: add it to your overrides (see docs/pricing.md) or run --update-pricing.", u.Model))
				} else {
					advice = append(advice, fmt.Sprintf("%s is not a Claude model id, so it cannot be priced here; it is deliberately left out of costs.", u.Model))
				}
			}
			w("  %-24s %9d %10s  %s", clip(u.Model), u.Messages, cost, how)
		}
		for _, a := range advice {
			w("  note: %s", a)
		}
	}
	w("")
	return sb.String()
}

func commas(n int) string {
	s := fmt.Sprint(n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
