package data

import "sort"

// syntheticModel is the placeholder Claude Code records for locally generated
// assistant messages (no API call, no usage, no cost).
const syntheticModel = "<synthetic>"

// ModelUsage is one model's share of a session.
type ModelUsage struct {
	Model    string
	Messages int // API responses, each counted once
	Tokens   int // input + output
	CostUSD  float64
	Match    PriceMatch // PriceNone: no price known for this model
}

// UsageTally accumulates usage for one session.
//
// Claude Code writes one transcript line per content block (thinking, text,
// each tool call), and every line repeats the whole message's usage. Summing
// lines therefore counts a message several times; the tally counts each
// message ID once. Each message is priced with its own model, so sessions that
// mix models are costed correctly.
type UsageTally struct {
	book *PriceBook
	seen map[string]struct{}

	perModel map[string]*ModelUsage
	order    []string // first-appearance order (stable tie-break)

	input, output, cacheRead, cacheWrite int
	cost                                 float64
	estimated                            bool
	unpricedTokens                       int
	unpriced                             map[string]bool
}

// NewUsageTally returns a tally that prices with book (DefaultBook if nil).
func NewUsageTally(book *PriceBook) *UsageTally {
	if book == nil {
		book = DefaultBook()
	}
	return &UsageTally{book: book, seen: map[string]struct{}{}, perModel: map[string]*ModelUsage{}, unpriced: map[string]bool{}}
}

// Add records one transcript line's usage. msgID is the API message ID; lines
// sharing an ID are counted once. An empty ID cannot be deduplicated.
func (t *UsageTally) Add(msgID string, u Usage) {
	if u.Model == syntheticModel {
		return
	}
	if msgID != "" {
		if _, dup := t.seen[msgID]; dup {
			return
		}
		t.seen[msgID] = struct{}{}
	}
	writes := u.CacheWrite5m + u.CacheWrite1h
	t.input += u.Input
	t.output += u.Output
	t.cacheRead += u.CacheRead
	t.cacheWrite += writes

	mu := t.perModel[u.Model]
	if mu == nil {
		mu = &ModelUsage{Model: u.Model, Match: PriceExact}
		t.perModel[u.Model] = mu
		t.order = append(t.order, u.Model)
	}
	mu.Messages++
	mu.Tokens += u.Input + u.Output

	c := t.book.Cost(u)
	switch c.Match {
	case PriceNone:
		t.unpricedTokens += u.Input + u.Output + u.CacheRead + writes
		t.unpriced[u.Model] = true
		mu.Match = PriceNone
	case PriceFamily:
		t.estimated = true
		if mu.Match != PriceNone {
			mu.Match = PriceFamily
		}
	}
	t.cost += c.USD
	mu.CostUSD += c.USD
}

// Apply writes the totals into stats. It does not touch the energy fields;
// callers run ComputeEnergy afterwards.
func (t *UsageTally) Apply(s *SessionStats) {
	s.TotalInputTokens = t.input
	s.TotalOutputTokens = t.output
	s.CacheReadTokens = t.cacheRead
	s.CacheWriteTokens = t.cacheWrite
	s.EstimatedCostUSD = t.cost
	s.CostEstimated = t.estimated
	s.UnpricedTokens = t.unpricedTokens
	s.PricingAsOf = t.book.AsOf

	s.UnpricedModels = s.UnpricedModels[:0]
	for m := range t.unpriced {
		s.UnpricedModels = append(s.UnpricedModels, m)
	}
	sort.Strings(s.UnpricedModels)

	s.ModelBreakdown = s.ModelBreakdown[:0]
	for _, m := range t.order {
		s.ModelBreakdown = append(s.ModelBreakdown, *t.perModel[m])
	}
	sort.SliceStable(s.ModelBreakdown, func(i, j int) bool { // most expensive first; ties keep first-seen order
		a, b := s.ModelBreakdown[i], s.ModelBreakdown[j]
		if a.CostUSD != b.CostUSD {
			return a.CostUSD > b.CostUSD
		}
		return a.Messages > b.Messages
	})
	s.Models = s.Models[:0]
	for _, m := range s.ModelBreakdown {
		s.Models = append(s.Models, m.Model)
	}
}

// splitCacheWrites splits cache_creation_input_tokens into 5-minute and 1-hour
// writes using the per-TTL breakdown when the transcript has one. Tokens the
// breakdown does not account for (or all of them, in older transcripts with no
// breakdown) are 5-minute writes, the API's default TTL.
func splitCacheWrites(total, in5m, in1h int) (w5m, w1h int) {
	w5m, w1h = in5m, in1h
	if rest := total - in5m - in1h; rest > 0 {
		w5m += rest
	}
	return w5m, w1h
}
