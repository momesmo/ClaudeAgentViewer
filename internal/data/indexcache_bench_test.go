package data

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeSessions writes n synthetic session transcripts of roughly bytesEach
// bytes into a temp dir: indexed prompts/replies mixed with bulky tool_result
// lines (which are skipped by the indexer, as in real transcripts).
func makeSessions(tb testing.TB, n, bytesEach int) []string {
	tb.Helper()
	dir := tb.TempDir()
	bulk := strings.Repeat("x", 4000)
	var paths []string
	for i := 0; i < n; i++ {
		var sb strings.Builder
		for j := 0; sb.Len() < bytesEach; j++ {
			fmt.Fprintf(&sb, `{"type":"user","gitBranch":"feat/x","message":{"role":"user","content":"question %d about parsing tokens in module %d"}}`+"\n", j, i)
			fmt.Fprintf(&sb, `{"type":"assistant","message":{"role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"answer %d: look at the handler"},{"type":"tool_use","name":"Grep","input":{}}]}}`+"\n", j)
			fmt.Fprintf(&sb, `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"%s"}]}}`+"\n", bulk)
		}
		p := filepath.Join(dir, fmt.Sprintf("s%03d.jsonl", i))
		if err := os.WriteFile(p, []byte(sb.String()), 0o644); err != nil {
			tb.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

func buildAll(paths []string) map[string]IndexEntry {
	idx := make(map[string]IndexEntry, len(paths))
	for _, p := range paths {
		e, _ := BuildIndexEntry(p, nil)
		idx[p] = e
	}
	return idx
}

// BenchmarkIndex compares the three ways a launch can obtain the search index:
// rebuild from transcripts (no cache), load the on-disk cache, or reuse an
// already-loaded index and only stat the files (mtime check). Search itself
// (MemScan) is identical for all three because it scans the in-memory blobs.
func BenchmarkIndex(b *testing.B) {
	paths := makeSessions(b, 40, 1<<20) // 40 sessions, ~40 MB of transcripts
	idx := buildAll(paths)
	cacheDir := b.TempDir()
	if err := SaveIndex(cacheDir, "c", idx); err != nil {
		b.Fatal(err)
	}
	b.Run("ColdRebuild_NoCache", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buildAll(paths)
		}
	})
	b.Run("LoadCacheFile", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if len(LoadIndex(cacheDir, "c")) != len(paths) {
				b.Fatal("short load")
			}
		}
	})
	b.Run("SaveCacheFile_Uncached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			redactMu.Lock()
			redactCache = map[string]redactCacheEntry{} // force re-redaction
			redactMu.Unlock()
			if err := SaveIndex(cacheDir, "c", idx); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("SaveCacheFile_RedactionCached", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if err := SaveIndex(cacheDir, "c", idx); err != nil {
				b.Fatal(err)
			}
		}
	})
	loaded := LoadIndex(cacheDir, "c")
	b.Run("WarmReindex_MtimeCheck", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, p := range paths {
				prev := loaded[p]
				BuildIndexEntry(p, &prev)
			}
		}
	})
	b.Run("MemScan_Search", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, e := range idx {
				strings.Contains(e.Blob, "needle-not-present")
			}
		}
	})
}

// A cache hit (load + mtime check) must beat rebuilding from transcripts, or
// the opt-in disk cache has no reason to exist.
func TestIndex_CacheBeatsRebuild(t *testing.T) {
	paths := makeSessions(t, 20, 1<<20)
	t0 := time.Now()
	idx := buildAll(paths)
	cold := time.Since(t0)

	dir := t.TempDir()
	if err := SaveIndex(dir, "c", idx); err != nil {
		t.Fatal(err)
	}
	t1 := time.Now()
	loaded := LoadIndex(dir, "c")
	changed := 0
	for _, p := range paths {
		prev := loaded[p]
		if _, c := BuildIndexEntry(p, &prev); c {
			changed++
		}
	}
	warm := time.Since(t1)
	t.Logf("20 sessions x ~1MB: cold rebuild %v, cache load+mtime check %v (changed=%d)", cold, warm, changed)
	if changed != 0 {
		t.Errorf("warm check re-read %d unchanged files", changed)
	}
	if warm*2 > cold {
		t.Errorf("cache hit (%v) should be well under half of a cold rebuild (%v)", warm, cold)
	}
}
