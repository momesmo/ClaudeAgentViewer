package data

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// embeddedPricing is the price table shipped with the binary. It is generated
// from Anthropic's published pricing page (see docs/pricing.md) and records its
// source and date. A locally fetched copy or a user override file can layer on
// top of it (see LayeredBook in pricesync.go).
//
//go:embed pricing_table.json
var embeddedPricing []byte

// Rate is a set of prices in USD per million tokens.
type Rate struct {
	Input        float64 `json:"input"`
	CacheWrite5m float64 `json:"cacheWrite5m"`
	CacheWrite1h float64 `json:"cacheWrite1h"`
	CacheRead    float64 `json:"cacheRead"`
	Output       float64 `json:"output"`
}

// LongContextRate replaces the standard rate for a request whose prompt
// (input + cache reads + cache writes) is longer than OverTokens. Only some
// models (Claude Haiku 5.5) are priced this way.
type LongContextRate struct {
	OverTokens int `json:"overTokens"`
	Rate
}

// ModelPrice is the price of one model ID.
type ModelPrice struct {
	Rate
	LongContext *LongContextRate `json:"longContext,omitempty"`
}

// Modifiers are published multipliers that stack on top of a model's rates.
type Modifiers struct {
	FastMultiplier   float64 `json:"fastMultiplier"`   // usage.speed == "fast"
	USGeoMultiplier  float64 `json:"usGeoMultiplier"`  // usage.inference_geo == "us" (Claude 4.6 and later)
	WebSearchPer1000 float64 `json:"webSearchPer1000"` // USD per 1,000 web searches
}

// PriceBook maps model IDs to prices. It is immutable once published.
type PriceBook struct {
	Version   int                   `json:"version"`
	Source    string                `json:"source"`
	AsOf      string                `json:"asOf"`
	Modifiers Modifiers             `json:"modifiers"`
	Models    map[string]ModelPrice `json:"models"`
}

// PriceMatch says how a model string was matched to a price.
type PriceMatch int

const (
	PriceNone       PriceMatch = iota // no price: cost is unknown, not zero
	PriceExact                        // the model ID is in the table
	PriceNormalized                   // matched after stripping a date, provider prefix or suffix
	PriceFamily                       // unknown version: priced like the nearest known model in its family (an estimate)
)

func (k PriceMatch) String() string {
	return [...]string{"none", "exact", "normalized", "family"}[k]
}

var (
	defaultBook  atomic.Pointer[PriceBook]
	embeddedBook *PriceBook // the table compiled into the binary; never replaced at runtime
)

func init() {
	b, err := ParsePriceBook(embeddedPricing)
	if err != nil {
		panic("claudevu: embedded pricing_table.json is invalid: " + err.Error())
	}
	embeddedBook = b
	defaultBook.Store(b)
}

// EmbeddedBook returns the built-in table. It is the fixed baseline that
// fetched prices are checked against; unlike DefaultBook it never contains
// fetched prices or user overrides. Callers must not modify it.
func EmbeddedBook() *PriceBook { return embeddedBook }

// DefaultBook returns the price book used for all cost calculations.
func DefaultBook() *PriceBook { return defaultBook.Load() }

// SetDefaultBook replaces the price book (for example after a pricing update).
// It is safe to call while other goroutines are computing costs.
func SetDefaultBook(b *PriceBook) {
	if b != nil {
		defaultBook.Store(b)
	}
}

var modelKeyRE = regexp.MustCompile(`^claude-(fable|mythos|opus|sonnet|haiku)-\d+(-\d+)*$`)

const maxSanePrice = 1000.0 // USD per million tokens; anything above is treated as corrupt

// ParsePriceBook decodes and validates a price book.
func ParsePriceBook(data []byte) (*PriceBook, error) {
	var b PriceBook
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&b); err != nil {
		return nil, fmt.Errorf("pricing: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("pricing: unexpected data after the JSON document")
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return &b, nil
}

// Validate rejects books that are structurally wrong or have implausible prices,
// so a corrupt or hostile file can never silently produce nonsense costs.
func (b *PriceBook) Validate() error {
	if b.Version != 1 {
		return fmt.Errorf("pricing: unsupported version %d", b.Version)
	}
	if b.AsOf != "" {
		if _, err := time.Parse("2006-01-02", b.AsOf); err != nil {
			return fmt.Errorf("pricing: asOf %q is not a YYYY-MM-DD date", b.AsOf)
		}
	}
	if len(b.Models) == 0 {
		return fmt.Errorf("pricing: no models")
	}
	if len(b.Models) > 500 {
		return fmt.Errorf("pricing: implausibly many models (%d)", len(b.Models))
	}
	m := b.Modifiers
	if !between(m.FastMultiplier, 1, 10) || !between(m.USGeoMultiplier, 1, 2) || !between(m.WebSearchPer1000, 0, 100) {
		return fmt.Errorf("pricing: implausible modifiers %+v", m)
	}
	for id, mp := range b.Models {
		if !modelKeyRE.MatchString(id) {
			return fmt.Errorf("pricing: bad model id %q", id)
		}
		if err := validateRate(id, mp.Rate); err != nil {
			return err
		}
		if lc := mp.LongContext; lc != nil {
			if lc.OverTokens <= 0 || lc.OverTokens > 10_000_000 {
				return fmt.Errorf("pricing: %s: bad long-context threshold %d", id, lc.OverTokens)
			}
			if err := validateRate(id+" (long context)", lc.Rate); err != nil {
				return err
			}
		}
	}
	return nil
}

func between(v, lo, hi float64) bool { return !math.IsNaN(v) && v >= lo && v <= hi }

func validateRate(what string, r Rate) error {
	for name, v := range map[string]float64{"input": r.Input, "cacheWrite5m": r.CacheWrite5m, "cacheWrite1h": r.CacheWrite1h, "cacheRead": r.CacheRead, "output": r.Output} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v > maxSanePrice {
			return fmt.Errorf("pricing: %s: %s price %v is not plausible", what, name, v)
		}
	}
	if r.CacheWrite1h < r.CacheWrite5m || r.CacheWrite5m < r.Input || r.CacheRead > r.Input {
		return fmt.Errorf("pricing: %s: cache prices are inconsistent with the input price (%+v)", what, r)
	}
	return nil
}

// Merge returns a new book with over's models and non-zero modifiers layered on
// top of b. Neither input is modified.
func (b *PriceBook) Merge(over *PriceBook) *PriceBook {
	if over == nil {
		over = &PriceBook{}
	}
	out := &PriceBook{Version: b.Version, Source: b.Source, AsOf: b.AsOf, Modifiers: b.Modifiers,
		Models: make(map[string]ModelPrice, len(b.Models)+len(over.Models))}
	for k, v := range b.Models {
		out.Models[k] = v
	}
	for k, v := range over.Models {
		// An override that leaves out the long-context tier keeps the base tier;
		// dropping it would silently under-price long prompts.
		if base, ok := b.Models[k]; ok && v.LongContext == nil && base.LongContext != nil {
			v.LongContext = base.LongContext
		}
		out.Models[k] = v
	}
	if over.Modifiers.FastMultiplier != 0 {
		out.Modifiers.FastMultiplier = over.Modifiers.FastMultiplier
	}
	if over.Modifiers.USGeoMultiplier != 0 {
		out.Modifiers.USGeoMultiplier = over.Modifiers.USGeoMultiplier
	}
	if over.Modifiers.WebSearchPer1000 != 0 {
		out.Modifiers.WebSearchPer1000 = over.Modifiers.WebSearchPer1000
	}
	if over.AsOf > out.AsOf {
		out.AsOf = over.AsOf
	}
	return out
}

// --- model matching ----------------------------------------------------------

var (
	regionPrefixRE = regexp.MustCompile(`^(us|eu|apac|jp|au|global)\.`)
	bedrockVerRE   = regexp.MustCompile(`-v\d+:\d+$`)
	dateSuffixRE   = regexp.MustCompile(`-\d{8}$`)
	bracketRE      = regexp.MustCompile(`\[[^\]]*\]$`)
	modernNameRE   = regexp.MustCompile(`^claude-([a-z]+)-(\d+(?:-\d+)*)$`)
	legacyNameRE   = regexp.MustCompile(`^claude-(\d+(?:-\d+)*)-([a-z]+)$`)
)

// normalizeModelID strips provider prefixes, date snapshots and other decoration.
func normalizeModelID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = bracketRE.ReplaceAllString(s, "")
	s = regionPrefixRE.ReplaceAllString(s, "")
	s = strings.TrimPrefix(s, "anthropic.")
	if i := strings.IndexByte(s, '@'); i >= 0 {
		s = s[:i]
	}
	s = bedrockVerRE.ReplaceAllString(s, "")
	s = strings.TrimSuffix(s, "-latest")
	s = dateSuffixRE.ReplaceAllString(s, "")
	return s
}

func parseVersion(s string) []int {
	var v []int
	for _, p := range strings.Split(s, "-") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		v = append(v, n)
	}
	return v
}

func cmpVersion(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// familyAndVersion splits a model ID into its family and version.
func familyAndVersion(id string) (family string, ver []int, ok bool) {
	if m := modernNameRE.FindStringSubmatch(id); m != nil {
		if v := parseVersion(m[2]); v != nil {
			return m[1], v, true
		}
	}
	if m := legacyNameRE.FindStringSubmatch(id); m != nil { // claude-3-5-sonnet
		if v := parseVersion(m[1]); v != nil {
			return m[2], v, true
		}
	}
	return "", nil, false
}

var knownFamilies = map[string]bool{"fable": true, "mythos": true, "opus": true, "sonnet": true, "haiku": true}

// Lookup finds the price for a model string. The returned ID is the table key
// that was used; it is empty when kind is PriceNone.
func (b *PriceBook) Lookup(model string) (ModelPrice, string, PriceMatch) {
	n := normalizeModelID(model)
	if n == "" {
		return ModelPrice{}, "", PriceNone
	}
	if mp, ok := b.Models[n]; ok {
		if n == model {
			return mp, n, PriceExact
		}
		return mp, n, PriceNormalized
	}
	// Unknown ID: estimate from the nearest known model in the same family.
	family, ver, ok := familyAndVersion(n)
	if !ok && knownFamilies[n] { // bare alias such as "opus"
		family, ver, ok = n, []int{1 << 30}, true
	}
	if !ok {
		return ModelPrice{}, "", PriceNone
	}
	type cand struct {
		id  string
		ver []int
	}
	var cands []cand
	for id := range b.Models {
		if f, v, ok := familyAndVersion(id); ok && f == family {
			cands = append(cands, cand{id, v})
		}
	}
	if len(cands) == 0 {
		return ModelPrice{}, "", PriceNone
	}
	sort.Slice(cands, func(i, j int) bool { return cmpVersion(cands[i].ver, cands[j].ver) < 0 })
	pick := cands[0] // older than anything known -> the oldest known model
	for _, c := range cands {
		if cmpVersion(c.ver, ver) <= 0 {
			pick = c // highest known version not above the requested one
		}
	}
	return b.Models[pick.id], pick.id, PriceFamily
}

// supportsGeoMultiplier reports whether the US data-residency multiplier applies
// ("Claude 4.6 and later models").
func supportsGeoMultiplier(id string) bool {
	_, ver, ok := familyAndVersion(id)
	return ok && cmpVersion(ver, []int{4, 6}) >= 0
}

// --- cost --------------------------------------------------------------------

// Usage is the billable usage of one API response.
type Usage struct {
	Model        string
	Input        int
	Output       int // includes thinking tokens
	CacheRead    int
	CacheWrite5m int
	CacheWrite1h int
	WebSearches  int
	Speed        string // "standard" or "fast"
	Geo          string // "global" or "us"
}

// PromptTokens is the length of the request's prompt: input plus cache reads and writes.
func (u Usage) PromptTokens() int { return u.Input + u.CacheRead + u.CacheWrite5m + u.CacheWrite1h }

// CostResult is a computed cost and how trustworthy it is.
type CostResult struct {
	USD   float64
	Match PriceMatch // PriceNone means USD is 0 because the model has no price
}

// Cost prices one response. A model with no price yields USD 0 and PriceNone;
// callers must treat that as "unknown", not "free".
func (b *PriceBook) Cost(u Usage) CostResult {
	mp, id, kind := b.Lookup(u.Model)
	if kind == PriceNone {
		return CostResult{Match: PriceNone}
	}
	r := mp.Rate
	if lc := mp.LongContext; lc != nil && u.PromptTokens() > lc.OverTokens {
		r = lc.Rate
	}
	usd := (float64(u.Input)*r.Input + float64(u.Output)*r.Output + float64(u.CacheRead)*r.CacheRead +
		float64(u.CacheWrite5m)*r.CacheWrite5m + float64(u.CacheWrite1h)*r.CacheWrite1h) / 1e6
	if u.Speed == "fast" {
		usd *= b.Modifiers.FastMultiplier
	}
	if u.Geo == "us" && supportsGeoMultiplier(id) {
		usd *= b.Modifiers.USGeoMultiplier
	}
	usd += float64(u.WebSearches) * b.Modifiers.WebSearchPer1000 / 1000
	return CostResult{USD: usd, Match: kind}
}
