package data

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Where a price comes from.
const (
	OriginBuiltIn  = "built-in"
	OriginCache    = "fetched cache"
	OriginOverride = "override"
)

// PricingCooldown is the minimum time between automatic price refresh attempts.
const PricingCooldown = 24 * time.Hour

// PricingPolicy describes how (and whether) prices may be refreshed.
type PricingPolicy struct {
	Auto       bool   // opt-in automatic refresh is enabled
	AutoVia    string // how it was enabled: "--pricing-auto" or "CLAUDEVU_PRICING_AUTO", "" if off
	URL        string
	AllowHosts []string // empty = the built-in default allowlist
}

// LayerFile describes one price layer.
type LayerFile struct {
	Layer   string
	Path    string
	Exists  bool
	InUse   bool
	Status  string
	Size    int64
	ModTime time.Time
	AsOf    string
	Models  int
}

// PricingProblem is something wrong with an optional layer, with how to fix it.
type PricingProblem struct {
	Layer   string
	Message string
	Fix     string
}

// ModelSource is one model's price and which layer supplies it.
type ModelSource struct {
	ID     string
	Price  ModelPrice
	Origin string
	Note   string // how it differs from the layer below, "" for built-in
}

// ModelUse is how a model that appears in the user's sessions is priced.
type ModelUse struct {
	Model      string
	Sessions   int
	Messages   int
	Tokens     int // input + output
	CostUSD    float64
	Match      PriceMatch
	ResolvedID string // the table entry used, "" when unpriced
}

// PricingReport is everything --pricing-info shows.
type PricingReport struct {
	CacheDir, ConfigDir string
	Files               []LayerFile
	Problems            []PricingProblem
	Book                *PriceBook
	Models              []ModelSource
	ModifierOrigins     map[string]string
	Policy              PricingPolicy
	State               PricingState
	NextAutoAttempt     time.Time // zero: an attempt is allowed now
	Usage               []ModelUse
}

// ExplainPricing gathers the facts about the price layers without touching the
// network or writing anything.
func ExplainPricing(cacheDir, configDir string, policy PricingPolicy, now time.Time) PricingReport {
	book, info := LayeredBook(cacheDir, configDir)
	emb := EmbeddedBook()
	r := PricingReport{CacheDir: cacheDir, ConfigDir: configDir, Book: book, Policy: policy,
		ModifierOrigins: map[string]string{}}

	// --- layer files -------------------------------------------------------
	r.Files = append(r.Files, LayerFile{Layer: OriginBuiltIn, Path: "(compiled into claudevu)", Exists: true, InUse: true,
		Status: "in use", AsOf: emb.AsOf, Models: len(emb.Models)})

	cachePath := ""
	if cacheDir != "" {
		cachePath = filepath.Join(cacheDir, cacheFileName)
	}
	var cacheBook *PriceBook
	cf := LayerFile{Layer: OriginCache, Path: cachePath}
	switch {
	case cacheDir == "":
		cf.Status = "unavailable (no usable cache directory)"
	default:
		if fi, err := os.Stat(cachePath); err == nil {
			cf.Exists, cf.Size, cf.ModTime = true, fi.Size(), fi.ModTime()
		}
		switch {
		case !cf.Exists:
			cf.Status = "not present (created by claudevu --update-pricing)"
		case info.CacheUsed:
			cacheBook, _ = LoadPricingCache(cacheDir)
			cf.InUse, cf.Status = true, "in use"
			if cacheBook != nil {
				cf.AsOf, cf.Models = cacheBook.AsOf, len(cacheBook.Models)
			}
		default:
			cf.Status = "ignored (see problems)"
		}
	}
	r.Files = append(r.Files, cf)

	ovPath := ""
	if configDir != "" {
		ovPath = filepath.Join(configDir, overridesFileName)
	}
	of := LayerFile{Layer: OriginOverride, Path: ovPath}
	var ovBook *PriceBook
	if configDir == "" {
		of.Status = "unavailable (no user config directory)"
	} else {
		if fi, err := os.Stat(ovPath); err == nil {
			of.Exists, of.Size, of.ModTime = true, fi.Size(), fi.ModTime()
		}
		ov, _ := LoadPricingOverrides(configDir)
		switch {
		case !of.Exists:
			of.Status = "not present (create it to set your own prices)"
		case ov != nil:
			ovBook = ov
			of.InUse, of.Status, of.Models = true, "in use", len(ov.Models)
		default:
			of.Status = "ignored as a whole; see problems below"
		}
	}
	r.Files = append(r.Files, of)

	// --- problems ----------------------------------------------------------
	for _, p := range info.Problems {
		if strings.HasPrefix(p, "pricing overrides") {
			continue // replaced by the detailed list below
		}
		fix := "Delete the file (claudevu --clear-cache removes it); claudevu --update-pricing writes a fresh one."
		if strings.Contains(p, "older than the built-in") || strings.Contains(p, "invalid date") {
			fix = "Run claudevu --update-pricing for current prices, or delete the file (claudevu --clear-cache)."
		}
		r.Problems = append(r.Problems, PricingProblem{Layer: OriginCache, Message: p, Fix: fix})
	}
	if of.Exists && ovBook == nil && ovPath != "" {
		r.Problems = append(r.Problems, overrideProblems(ovPath)...)
	}

	// --- per-model provenance ----------------------------------------------
	origin := map[string]string{}
	note := map[string]string{}
	lower := emb
	if cacheBook != nil {
		for id, cp := range cacheBook.Models {
			if n, changed := describeChange(emb, id, cp); changed {
				origin[id], note[id] = OriginCache, n
			}
		}
		lower = emb.Merge(cacheBook)
	}
	if ovBook != nil {
		for id := range ovBook.Models {
			if n, changed := describeChange(lower, id, book.Models[id]); changed {
				origin[id], note[id] = OriginOverride, n
			}
		}
	}
	for id, mp := range book.Models {
		o := origin[id]
		if o == "" {
			o = OriginBuiltIn
		}
		r.Models = append(r.Models, ModelSource{ID: id, Price: mp, Origin: o, Note: note[id]})
	}
	sort.Slice(r.Models, func(i, j int) bool { return modelLess(r.Models[i].ID, r.Models[j].ID) })

	// --- modifier provenance -------------------------------------------------
	cur := emb.Modifiers
	r.ModifierOrigins = map[string]string{"fastMultiplier": OriginBuiltIn, "usGeoMultiplier": OriginBuiltIn, "webSearchPer1000": OriginBuiltIn}
	apply := func(m Modifiers, layer string) {
		if m.FastMultiplier != 0 && m.FastMultiplier != cur.FastMultiplier {
			cur.FastMultiplier, r.ModifierOrigins["fastMultiplier"] = m.FastMultiplier, layer
		}
		if m.USGeoMultiplier != 0 && m.USGeoMultiplier != cur.USGeoMultiplier {
			cur.USGeoMultiplier, r.ModifierOrigins["usGeoMultiplier"] = m.USGeoMultiplier, layer
		}
		if m.WebSearchPer1000 != 0 && m.WebSearchPer1000 != cur.WebSearchPer1000 {
			cur.WebSearchPer1000, r.ModifierOrigins["webSearchPer1000"] = m.WebSearchPer1000, layer
		}
	}
	if cacheBook != nil {
		apply(cacheBook.Modifiers, OriginCache)
	}
	if ovBook != nil {
		apply(ovBook.Modifiers, OriginOverride)
	}

	// --- refresh state -------------------------------------------------------
	if cacheDir != "" {
		r.State = LoadPricingState(cacheDir)
		if !r.State.ShouldAttempt(now, PricingCooldown) {
			r.NextAutoAttempt = r.State.LastAttempt.Add(PricingCooldown)
		}
	}
	return r
}

// describeChange says how a model's price in the layer above differs from base.
func describeChange(base *PriceBook, id string, nw ModelPrice) (string, bool) {
	old, ok := base.Models[id]
	if !ok {
		return PriceChange{Model: id, Added: true, New: nw}.String()[len(id)+2:], true
	}
	if sameModelPrice(old, nw) {
		return "", false
	}
	return strings.TrimPrefix(PriceChange{Model: id, Old: old, New: nw}.String(), id+": "), true
}

// modelLess orders models by family, then newest version first.
func modelLess(a, b string) bool {
	fa, va, oka := familyAndVersion(a)
	fb, vb, okb := familyAndVersion(b)
	if !oka || !okb {
		return a < b
	}
	if fa != fb {
		return fa < fb
	}
	return cmpVersion(va, vb) > 0
}

// overrideProblems lists every reason an override file cannot be used, each
// with a fix: a JSON syntax error with its line and column, or every invalid
// entry (not just the first).
func overrideProblems(path string) []PricingProblem {
	prob := func(msg, fix string) PricingProblem {
		return PricingProblem{Layer: OriginOverride, Message: msg, Fix: fix}
	}
	data, err := readSmallFile(path)
	if err != nil {
		return []PricingProblem{prob("cannot read the file: "+clip(err.Error()), "Make it a regular JSON file smaller than 1 MiB, or delete it.")}
	}
	var b PriceBook
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&b); err != nil {
		return []PricingProblem{prob("not valid JSON: "+jsonErrorWhere(data, err), "Fix the syntax at that position (a JSON validator helps), or delete the file.")}
	}
	issues := overrideIssues(&b)
	if len(issues) == 0 {
		return []PricingProblem{prob("the file was rejected for an unknown reason", "Compare it with the example in docs/pricing.md, or delete it.")}
	}
	out := make([]PricingProblem, 0, len(issues))
	for _, is := range issues {
		out = append(out, prob(is, "Correct this entry or remove it. The whole file is ignored until every entry is valid."))
	}
	return out
}

// overrideIssues returns every validation failure in an override file.
func overrideIssues(b *PriceBook) []string {
	var out []string
	if b.Version != 1 {
		out = append(out, fmt.Sprintf(`"version" must be 1 (got %d)`, b.Version))
	}
	m := b.Modifiers
	if (m.FastMultiplier != 0 && !between(m.FastMultiplier, 1, 10)) || (m.USGeoMultiplier != 0 && !between(m.USGeoMultiplier, 1, 2)) || !between(m.WebSearchPer1000, 0, 100) {
		out = append(out, fmt.Sprintf("modifiers out of range: fastMultiplier 1-10, usGeoMultiplier 1-2, webSearchPer1000 0-100 (got %+v)", m))
	}
	if len(b.Models) > 500 {
		out = append(out, "implausibly many models")
	}
	ids := make([]string, 0, len(b.Models))
	for id := range b.Models {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		mp := b.Models[id]
		if !modelKeyRE.MatchString(id) {
			out = append(out, fmt.Sprintf("%q is not a valid model id (expected claude-<family>-<version>, like claude-opus-5-5)", clip(id)))
			continue
		}
		if err := validateRate(id, mp.Rate); err != nil {
			out = append(out, strings.TrimPrefix(err.Error(), "pricing: "))
		}
		if lc := mp.LongContext; lc != nil {
			if lc.OverTokens <= 0 || lc.OverTokens > 10_000_000 {
				out = append(out, fmt.Sprintf("%s: bad long-context threshold %d", id, lc.OverTokens))
			} else if err := validateRate(id+" (long context)", lc.Rate); err != nil {
				out = append(out, strings.TrimPrefix(err.Error(), "pricing: "))
			}
		}
	}
	return out
}

// jsonErrorWhere describes a JSON error with its line and column.
func jsonErrorWhere(data []byte, err error) string {
	var off int64 = -1
	var se *json.SyntaxError
	var te *json.UnmarshalTypeError
	switch {
	case errors.As(err, &se):
		off = se.Offset
	case errors.As(err, &te):
		off = te.Offset
	}
	if off < 0 || off > int64(len(data)) {
		return clip(err.Error())
	}
	line := 1 + bytes.Count(data[:off], []byte("\n"))
	col := int(off) - (bytes.LastIndexByte(data[:off], '\n') + 1)
	if col < 1 {
		col = 1
	}
	return fmt.Sprintf("line %d, column %d: %s", line, col, clip(err.Error()))
}

// ScanModelUsage reports how every model found in the given transcripts is
// priced (costs use the default price book). Unreadable files are skipped.
func ScanModelUsage(paths []string, book *PriceBook) []ModelUse {
	agg := map[string]*ModelUse{}
	for _, p := range paths {
		st, err := QuickScanStats(p)
		if err != nil {
			continue
		}
		for _, mu := range st.ModelBreakdown {
			u := agg[mu.Model]
			if u == nil {
				u = &ModelUse{Model: mu.Model, Match: PriceExact}
				_, u.ResolvedID, _ = book.Lookup(mu.Model)
				agg[mu.Model] = u
			}
			u.Sessions++
			u.Messages += mu.Messages
			u.Tokens += mu.Tokens
			u.CostUSD += mu.CostUSD
		}
	}
	out := make([]ModelUse, 0, len(agg))
	for _, u := range agg {
		_, id, kind := book.Lookup(u.Model)
		u.ResolvedID, u.Match = id, kind
		out = append(out, *u)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CostUSD != out[j].CostUSD {
			return out[i].CostUSD > out[j].CostUSD
		}
		return out[i].Model < out[j].Model
	})
	return out
}
