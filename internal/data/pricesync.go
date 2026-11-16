package data

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Parsing the published pricing page
// ---------------------------------------------------------------------------

var (
	supRE       = regexp.MustCompile(`(?is)<sup>.*?</sup>`)
	nameRE      = regexp.MustCompile(`(?i)^\s*claude\s+([a-z]+)\s+(\d+(?:\.\d+)*)\b`)
	overRE      = regexp.MustCompile(`(?i)\bover\s+([\d,]+)\s+tokens`)
	priceCellRE = regexp.MustCompile(`^\$\s*(\d[\d,]*(?:\.\d+)?|\.\d+)\s*(?:USD\s*)?/\s*MTok\b`)
	sepCellRE   = regexp.MustCompile(`^:?-+:?$`)
)

func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func isSeparatorRow(cells []string) bool {
	for _, c := range cells {
		if !sepCellRE.MatchString(c) {
			return false
		}
	}
	return len(cells) > 0
}

// ParsePricingMarkdown parses the model table of Anthropic's pricing page
// (markdown form) into a price book. Modifiers are copied from the embedded
// default because the page prose is not table-parsable.
func ParsePricingMarkdown(md []byte, source, asOf string) (*PriceBook, error) {
	lines := strings.Split(strings.ReplaceAll(string(md), "\r\n", "\n"), "\n")
	hdr := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "|") {
			continue
		}
		cells := splitRow(t)
		if len(cells) >= 6 && strings.EqualFold(cells[0], "Model") && strings.Contains(t, "Base input tokens") {
			hdr = i
			break
		}
	}
	if hdr < 0 {
		return nil, errors.New("pricing page: model table not found (layout changed?)")
	}
	std := map[string]Rate{}
	lc := map[string]LongContextRate{}
	for i := hdr + 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(t, "|") {
			break
		}
		cells := splitRow(t)
		if isSeparatorRow(cells) {
			continue
		}
		name := supRE.ReplaceAllString(cells[0], "")
		m := nameRE.FindStringSubmatch(name)
		if m == nil || !knownFamilies[strings.ToLower(m[1])] {
			continue
		}
		id := "claude-" + strings.ToLower(m[1]) + "-" + strings.ReplaceAll(m[2], ".", "-")
		if !modelKeyRE.MatchString(id) {
			continue
		}
		if len(cells) < 6 {
			return nil, fmt.Errorf("pricing page: row %q has %d cells, want 6", strings.TrimSpace(name), len(cells))
		}
		var p [5]float64
		for j := 0; j < 5; j++ {
			c := strings.TrimSpace(supRE.ReplaceAllString(cells[j+1], ""))
			pm := priceCellRE.FindStringSubmatch(c)
			if pm == nil {
				return nil, fmt.Errorf("pricing page: row %q column %d: cannot parse price %q", strings.TrimSpace(name), j+2, c)
			}
			v, err := strconv.ParseFloat(strings.ReplaceAll(pm[1], ",", ""), 64)
			if err != nil {
				return nil, fmt.Errorf("pricing page: row %q column %d: bad price %q", strings.TrimSpace(name), j+2, c)
			}
			p[j] = v
		}
		r := Rate{Input: p[0], CacheWrite5m: p[1], CacheWrite1h: p[2], CacheRead: p[3], Output: p[4]}
		if om := overRE.FindStringSubmatch(name); om != nil {
			n, err := strconv.Atoi(strings.ReplaceAll(om[1], ",", ""))
			if err != nil {
				return nil, fmt.Errorf("pricing page: row %q: bad token threshold %q", strings.TrimSpace(name), om[1])
			}
			if _, dup := lc[id]; dup {
				return nil, fmt.Errorf("pricing page: duplicate long-context row for %s", id)
			}
			lc[id] = LongContextRate{OverTokens: n, Rate: r}
			continue
		}
		if _, dup := std[id]; dup {
			return nil, fmt.Errorf("pricing page: duplicate row for %s", id)
		}
		std[id] = r
	}
	if len(std) == 0 {
		return nil, errors.New("pricing page: model table has no recognizable model rows")
	}
	models := make(map[string]ModelPrice, len(std))
	for id, r := range std {
		mp := ModelPrice{Rate: r}
		if l, ok := lc[id]; ok {
			l := l
			mp.LongContext = &l
		}
		models[id] = mp
	}
	for id := range lc {
		if _, ok := std[id]; !ok {
			return nil, fmt.Errorf("pricing page: long-context row for %s has no standard row", id)
		}
	}
	b := &PriceBook{Version: 1, Source: source, AsOf: asOf, Modifiers: DefaultBook().Modifiers, Models: models}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return b, nil
}

// ---------------------------------------------------------------------------
// Fetching
// ---------------------------------------------------------------------------

const (
	// DefaultPricingURL is the published pricing page (markdown form).
	DefaultPricingURL = "https://platform.claude.com/docs/en/about-claude/pricing.md"

	defaultMaxBytes = 2 << 20
	defaultTimeout  = 10 * time.Second
	defaultUA       = "claudevu-pricing/1"
	maxRedirects    = 3
)

var defaultAllowHosts = []string{"platform.claude.com", "docs.anthropic.com", "claude.com", "www.anthropic.com"}

// FetchOptions controls FetchPricing. Zero values select safe defaults.
type FetchOptions struct {
	URL        string
	Client     *http.Client // optional; copied, its Jar is dropped and redirects are restricted
	MaxBytes   int64
	Timeout    time.Duration
	AllowHosts []string
	UserAgent  string
}

func hostAllowed(host string, allow []string) bool {
	for _, a := range allow {
		if strings.EqualFold(a, host) {
			return true
		}
	}
	return false
}

func textContentType(ct string) bool {
	if ct == "" {
		return true
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	mt = strings.ToLower(mt)
	return strings.HasPrefix(mt, "text/") || mt == "application/markdown" || mt == "application/x-markdown"
}

// FetchPricing downloads and parses the pricing page under strict safety rules.
func FetchPricing(ctx context.Context, o FetchOptions) (*PriceBook, error) {
	if o.URL == "" {
		o.URL = DefaultPricingURL
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = defaultMaxBytes
	}
	if o.Timeout <= 0 {
		o.Timeout = defaultTimeout
	}
	if len(o.AllowHosts) == 0 {
		o.AllowHosts = defaultAllowHosts
	}
	if o.UserAgent == "" {
		o.UserAgent = defaultUA
	}
	u, err := url.Parse(o.URL)
	if err != nil {
		return nil, fmt.Errorf("pricing fetch: bad URL: %w", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("pricing fetch: only https is allowed (got %q)", u.Scheme)
	}
	if u.User != nil {
		return nil, errors.New("pricing fetch: URL must not contain credentials")
	}
	if !hostAllowed(u.Hostname(), o.AllowHosts) {
		return nil, fmt.Errorf("pricing fetch: host %q is not in the allowlist", u.Hostname())
	}
	var client http.Client
	if o.Client != nil {
		client = *o.Client
	}
	client.Jar = nil
	client.Timeout = o.Timeout
	origHost := u.Host
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects {
			return fmt.Errorf("pricing fetch: more than %d redirects", maxRedirects)
		}
		if req.URL.Scheme != "https" || req.URL.Host != origHost {
			return fmt.Errorf("pricing fetch: refusing redirect to %s", req.URL.Redacted())
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("pricing fetch: %w", err)
	}
	req.Header.Set("User-Agent", o.UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pricing fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pricing fetch: unexpected status %s", resp.Status)
	}
	if !textContentType(resp.Header.Get("Content-Type")) {
		return nil, fmt.Errorf("pricing fetch: unexpected content type %q", resp.Header.Get("Content-Type"))
	}
	if resp.ContentLength > o.MaxBytes {
		return nil, fmt.Errorf("pricing fetch: response larger than %d bytes", o.MaxBytes)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, o.MaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("pricing fetch: reading body: %w", err)
	}
	if int64(len(body)) > o.MaxBytes {
		return nil, fmt.Errorf("pricing fetch: response larger than %d bytes", o.MaxBytes)
	}
	b, err := ParsePricingMarkdown(body, o.URL, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	if err := checkPlausible(b); err != nil {
		return nil, err
	}
	return b, nil
}

func checkPlausible(b *PriceBook) error {
	if len(b.Models) < 15 {
		return fmt.Errorf("pricing fetch: only %d models found; page layout probably changed", len(b.Models))
	}
	known := DefaultBook().Models
	have := 0
	for id := range known {
		if _, ok := b.Models[id]; ok {
			have++
		}
	}
	if len(known) > 0 && float64(have) < 0.8*float64(len(known)) {
		return fmt.Errorf("pricing fetch: only %d of %d known models found; page layout probably changed", have, len(known))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

const (
	cacheFileName     = "pricing-cache.v1.json"
	stateFileName     = "pricing-state.json"
	overridesFileName = "pricing-overrides.json"
)

func atomicWrite(dir, name string, data []byte) error {
	if dir == "" {
		return errors.New("pricing: empty directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, name+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return err
	}
	ok = true
	return nil
}

// SavePricingCache validates and atomically writes the fetched price book.
func SavePricingCache(cacheDir string, b *PriceBook) error {
	if b == nil {
		return errors.New("pricing: nil book")
	}
	if err := b.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", " ")
	if err != nil {
		return err
	}
	return atomicWrite(cacheDir, cacheFileName, append(data, '\n'))
}

func readCache(cacheDir string) (*PriceBook, string) {
	data, err := os.ReadFile(filepath.Join(cacheDir, cacheFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ""
		}
		return nil, "pricing cache unreadable: " + err.Error()
	}
	b, err := ParsePriceBook(data)
	if err != nil {
		return nil, "pricing cache ignored: " + err.Error()
	}
	return b, ""
}

// LoadPricingCache returns the cached book if it exists and validates.
func LoadPricingCache(cacheDir string) (*PriceBook, bool) {
	b, _ := readCache(cacheDir)
	return b, b != nil
}

// PricingState records fetch attempts so failures are not retried every run.
type PricingState struct {
	LastAttempt time.Time `json:"lastAttempt"`
	LastSuccess time.Time `json:"lastSuccess"`
	LastError   string    `json:"lastError,omitempty"`
	Attempts    int       `json:"attempts"`
}

// LoadPricingState returns the stored state, or the zero state if absent or corrupt.
func LoadPricingState(cacheDir string) PricingState {
	var s PricingState
	data, err := os.ReadFile(filepath.Join(cacheDir, stateFileName))
	if err != nil || json.Unmarshal(data, &s) != nil {
		return PricingState{}
	}
	return s
}

func SavePricingState(cacheDir string, s PricingState) error {
	data, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}
	return atomicWrite(cacheDir, stateFileName, append(data, '\n'))
}

// ShouldAttempt is false within cooldown of the last attempt.
func (s PricingState) ShouldAttempt(now time.Time, cooldown time.Duration) bool {
	if s.LastAttempt.IsZero() {
		return true
	}
	return now.Sub(s.LastAttempt) >= cooldown
}

// ---------------------------------------------------------------------------
// Overrides and layering
// ---------------------------------------------------------------------------

// LoadPricingOverrides reads pricing-overrides.json from configDir. A missing
// file returns (nil, ""); any other problem returns (nil, reason).
func LoadPricingOverrides(configDir string) (*PriceBook, string) {
	data, err := os.ReadFile(filepath.Join(configDir, overridesFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ""
		}
		return nil, "pricing overrides unreadable: " + err.Error()
	}
	var b PriceBook
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&b); err != nil {
		return nil, "pricing overrides ignored: " + err.Error()
	}
	if err := validateOverride(&b); err != nil {
		return nil, "pricing overrides ignored: " + err.Error()
	}
	return &b, ""
}

func validateOverride(b *PriceBook) error {
	if b.Version != 1 {
		return fmt.Errorf("version must be 1 (got %d)", b.Version)
	}
	if len(b.Models) > 500 {
		return errors.New("implausibly many models")
	}
	m := b.Modifiers
	if (m.FastMultiplier != 0 && !between(m.FastMultiplier, 1, 10)) ||
		(m.USGeoMultiplier != 0 && !between(m.USGeoMultiplier, 1, 2)) ||
		!between(m.WebSearchPer1000, 0, 100) {
		return fmt.Errorf("implausible modifiers %+v", m)
	}
	for id, mp := range b.Models {
		if !modelKeyRE.MatchString(id) {
			return fmt.Errorf("bad model id %q", id)
		}
		if err := validateRate(id, mp.Rate); err != nil {
			return err
		}
		if lc := mp.LongContext; lc != nil {
			if lc.OverTokens <= 0 || lc.OverTokens > 10_000_000 {
				return fmt.Errorf("%s: bad long-context threshold %d", id, lc.OverTokens)
			}
			if err := validateRate(id+" (long context)", lc.Rate); err != nil {
				return err
			}
		}
	}
	return nil
}

// LayerInfo describes how LayeredBook was assembled.
type LayerInfo struct {
	EmbeddedAsOf, CacheAsOf string
	CacheUsed               bool
	OverrideModels          int
	Problems                []string
}

// LayeredBook returns embedded <- fetched cache <- overrides. It never fails;
// problems with the optional layers are reported in LayerInfo.Problems.
func LayeredBook(cacheDir, configDir string) (*PriceBook, LayerInfo) {
	var info LayerInfo
	base, err := ParsePriceBook(embeddedPricing)
	if err != nil { // cannot happen: init() already validated it
		base = DefaultBook()
	}
	info.EmbeddedAsOf = base.AsOf
	out := base
	if cacheDir != "" {
		c, reason := readCache(cacheDir)
		switch {
		case reason != "":
			info.Problems = append(info.Problems, reason)
		case c != nil:
			info.CacheAsOf = c.AsOf
			if c.AsOf < base.AsOf {
				info.Problems = append(info.Problems, fmt.Sprintf("pricing cache (%s) is older than the built-in table (%s); ignored", c.AsOf, base.AsOf))
			} else {
				out = out.Merge(c)
				info.CacheUsed = true
			}
		}
	}
	if configDir != "" {
		ov, reason := LoadPricingOverrides(configDir)
		if reason != "" {
			info.Problems = append(info.Problems, reason)
		} else if ov != nil {
			out = out.Merge(ov)
			info.OverrideModels = len(ov.Models)
		}
	}
	return out, info
}

// RefreshPricing fetches the pricing page, stores it in the cache, and returns
// the layered book. On failure nothing but the state file is written.
func RefreshPricing(ctx context.Context, cacheDir, configDir string, o FetchOptions, now time.Time) (*PriceBook, PricingState, error) {
	st := LoadPricingState(cacheDir)
	fail := func(err error) (*PriceBook, PricingState, error) {
		st.LastAttempt = now
		st.LastError = err.Error()
		st.Attempts++
		_ = SavePricingState(cacheDir, st)
		return nil, st, err
	}
	fetched, err := FetchPricing(ctx, o)
	if err != nil {
		return fail(err)
	}
	if err := SavePricingCache(cacheDir, fetched); err != nil {
		return fail(fmt.Errorf("pricing: saving cache: %w", err))
	}
	st = PricingState{LastAttempt: now, LastSuccess: now}
	if err := SavePricingState(cacheDir, st); err != nil {
		return nil, st, fmt.Errorf("pricing: saving state: %w", err)
	}
	b, _ := LayeredBook(cacheDir, configDir)
	return b, st, nil
}
