package data

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	benchSessionCount  = 1000
	benchBlobBytes     = 40 * 1024
	benchNoMatchFilter = "#nomatch#" // '#' never appears in the generated blobs, so nothing matches
	scanBudget         = 100 * time.Millisecond
)

// benchFixture builds deterministic pseudo-random lowercase blobs (no '#'), one per session.
func benchFixture() ([]SessionInfo, []string) {
	const alphabet = "abcdefghijklmnopqrstuvwxyz      "
	sessions := make([]SessionInfo, benchSessionCount)
	blobs := make([]string, benchSessionCount)
	var seed uint32 = 2463534242
	for i := range sessions {
		var b strings.Builder
		b.Grow(benchBlobBytes)
		for j := 0; j < benchBlobBytes; j++ {
			seed ^= seed << 13
			seed ^= seed >> 17
			seed ^= seed << 5
			b.WriteByte(alphabet[seed%uint32(len(alphabet))])
		}
		blobs[i] = b.String()
		s := SessionInfo{
			SessionID: fmt.Sprintf("session-%04d", i),
			Project:   fmt.Sprintf("/proj/p%03d", i%50),
			Display:   fmt.Sprintf("prompt number %d about something", i),
		}
		s.FillSearchMeta()
		sessions[i] = s
	}
	return sessions, blobs
}

// scanAll runs one full MatchSession pass and returns the number of non-matches seen.
func scanAll(sessions []SessionInfo, blobs []string, filter string) int {
	n := 0
	for i := range sessions {
		if MatchSession(sessions[i], blobs[i], filter) == MatchNone {
			n++
		}
	}
	return n
}

func BenchmarkMatchSession_1000x40KB(b *testing.B) {
	sessions, blobs := benchFixture()
	b.SetBytes(int64(benchSessionCount * benchBlobBytes))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if got := scanAll(sessions, blobs, benchNoMatchFilter); got != benchSessionCount {
			b.Fatalf("expected no matches, got %d non-matches", got)
		}
	}
}

func TestMatchSession_ScanBudget(t *testing.T) {
	sessions, blobs := benchFixture()
	start := time.Now()
	got := scanAll(sessions, blobs, benchNoMatchFilter)
	elapsed := time.Since(start)
	if got != benchSessionCount {
		t.Fatalf("expected no matches, got %d non-matches", got)
	}
	t.Logf("full scan of %d sessions x 40KB: %v", benchSessionCount, elapsed)
	if elapsed > scanBudget {
		t.Fatalf("scan took %v, budget %v", elapsed, scanBudget)
	}
}
