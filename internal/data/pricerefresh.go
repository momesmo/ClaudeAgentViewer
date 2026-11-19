package data

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrSuspiciousPrices is returned when a refresh would move an existing price
// by more than suspiciousFactor in either direction. A hostile or broken page
// could otherwise silently make costs look tiny; the user must review and pass
// the explicit accept option instead.
var ErrSuspiciousPrices = errors.New("pricing: refusing suspicious price changes")

const suspiciousFactor = 3.0

// PriceChange is one model's difference between two price books.
type PriceChange struct {
	Model    string
	Added    bool
	Old, New ModelPrice
}

func rateFields(r Rate) [5]float64 {
	return [5]float64{r.Input, r.CacheWrite5m, r.CacheWrite1h, r.CacheRead, r.Output}
}

// Suspicious reports whether an existing model's price moved by more than
// suspiciousFactor (up or down) in any category.
func (c PriceChange) Suspicious() bool {
	if c.Added {
		return false
	}
	o, n := rateFields(c.Old.Rate), rateFields(c.New.Rate)
	for i := range o {
		if o[i] <= 0 || n[i] <= 0 {
			return true
		}
		if r := n[i] / o[i]; r > suspiciousFactor || r < 1/suspiciousFactor {
			return true
		}
	}
	return false
}

func money(v float64) string {
	return "$" + strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", v), "0"), ".")
}

func (c PriceChange) String() string {
	if c.Added {
		return fmt.Sprintf("%s: new model (input %s, output %s)", c.Model, money(c.New.Input), money(c.New.Output))
	}
	var parts []string
	add := func(name string, o, n float64) {
		if o != n {
			parts = append(parts, fmt.Sprintf("%s %s→%s", name, money(o), money(n)))
		}
	}
	add("input", c.Old.Input, c.New.Input)
	add("5m write", c.Old.CacheWrite5m, c.New.CacheWrite5m)
	add("1h write", c.Old.CacheWrite1h, c.New.CacheWrite1h)
	add("cache read", c.Old.CacheRead, c.New.CacheRead)
	add("output", c.Old.Output, c.New.Output)
	if (c.Old.LongContext == nil) != (c.New.LongContext == nil) || (c.Old.LongContext != nil && *c.Old.LongContext != *c.New.LongContext) {
		parts = append(parts, "long-context tier changed")
	}
	return c.Model + ": " + strings.Join(parts, ", ")
}

func sameModelPrice(a, b ModelPrice) bool {
	if a.Rate != b.Rate {
		return false
	}
	if (a.LongContext == nil) != (b.LongContext == nil) {
		return false
	}
	return a.LongContext == nil || *a.LongContext == *b.LongContext
}

// DiffBooks lists models that are new in nb or whose price differs from old.
// Models only in old are not reported (a refresh never removes prices).
func DiffBooks(old, nb *PriceBook) []PriceChange {
	var out []PriceChange
	for id, n := range nb.Models {
		o, ok := old.Models[id]
		switch {
		case !ok:
			out = append(out, PriceChange{Model: id, Added: true, New: n})
		case !sameModelPrice(o, n):
			out = append(out, PriceChange{Model: id, Old: o, New: n})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}

// SuspiciousChanges filters changes down to those RefreshPricingVetted refuses.
func SuspiciousChanges(changes []PriceChange) []PriceChange {
	var out []PriceChange
	for _, c := range changes {
		if c.Suspicious() {
			out = append(out, c)
		}
	}
	return out
}

// RefreshResult is the outcome of RefreshPricingVetted.
type RefreshResult struct {
	Applied    bool
	Book       *PriceBook // layered book to use, when Applied
	Changes    []PriceChange
	Suspicious []PriceChange
	State      PricingState
}

// RefreshPricingVetted fetches the pricing page and, unless it would make a
// suspicious change (see ErrSuspiciousPrices) or fails to validate, stores it in
// the cache and returns the new layered book. acceptSuspicious applies it
// anyway; use it only after the user has reviewed the changes. Every outcome
// except success updates the attempt state, so failures and refusals are not
// retried until the cooldown passes.
func RefreshPricingVetted(ctx context.Context, cacheDir, configDir string, o FetchOptions, now time.Time, acceptSuspicious bool) (RefreshResult, error) {
	// Make sure the result can be stored (and the attempt recorded) BEFORE using
	// the network; otherwise every run would fetch again and discard the page.
	if err := checkWritable(cacheDir); err != nil {
		return RefreshResult{}, fmt.Errorf("pricing: no writable cache directory: %w", err)
	}
	st := LoadPricingState(cacheDir)
	record := func(err error) {
		st.LastAttempt, st.LastError = now, cleanText(err.Error(), 300)
		st.Attempts++
		_ = SavePricingState(cacheDir, st)
	}
	fetched, err := FetchPricing(ctx, o)
	if err != nil {
		record(err)
		return RefreshResult{State: st}, err
	}
	current, _ := LayeredBook(cacheDir, configDir)
	res := RefreshResult{Changes: DiffBooks(current, fetched)}
	res.Suspicious = SuspiciousChanges(res.Changes)
	if n, shared := changedShare(current, fetched); shared > 0 && float64(n) > 0.25*float64(shared) {
		// Real price changes are rare and per-model; a page-wide change is far
		// more likely to be a broken or hostile page. Treat it as suspicious.
		res.Suspicious = append(res.Suspicious, res.Changes...)
	}
	if len(res.Suspicious) > 0 && !acceptSuspicious {
		err := fmt.Errorf("%w: %s", ErrSuspiciousPrices, res.Suspicious[0])
		record(err)
		res.State = st
		return res, err
	}
	if err := SavePricingCache(cacheDir, fetched); err != nil {
		err = fmt.Errorf("pricing: saving cache: %w", err)
		record(err)
		res.State = st
		return res, err
	}
	st = PricingState{LastAttempt: now, LastSuccess: now}
	if err := SavePricingState(cacheDir, st); err != nil {
		res.State = st
		return res, fmt.Errorf("pricing: saving state: %w", err)
	}
	res.Applied, res.State = true, st
	res.Book, _ = LayeredBook(cacheDir, configDir)
	return res, nil
}

// ClearPricingCache deletes the fetched price table and the attempt state from
// dir (override files are the user's and are never touched). It returns the
// files it removed. An empty dir is a no-op.
func ClearPricingCache(dir string) ([]string, error) {
	if dir == "" {
		return nil, nil
	}
	var removed []string
	for _, name := range []string{cacheFileName, stateFileName} {
		p := filepath.Join(dir, name)
		err := os.Remove(p)
		switch {
		case err == nil:
			removed = append(removed, p)
		case os.IsNotExist(err):
		default:
			return removed, err
		}
	}
	return removed, nil
}

// changedShare counts how many models present in both books have different
// prices, and how many models are shared.
func changedShare(old, nb *PriceBook) (changed, shared int) {
	for id, o := range old.Models {
		n, ok := nb.Models[id]
		if !ok {
			continue
		}
		shared++
		if !sameModelPrice(o, n) {
			changed++
		}
	}
	return
}

// checkWritable verifies dir can be created and written to.
func checkWritable(dir string) error {
	if dir == "" {
		return errors.New("no cache directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".write-probe-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}
