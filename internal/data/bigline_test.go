package data

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func asstLine(id string, out int, textLen int) string {
	text := strings.Repeat("x", textLen)
	return fmt.Sprintf(`{"type":"assistant","sessionId":"s","timestamp":"2026-11-13T10:00:00.000Z","message":{"id":%q,"role":"assistant","model":"claude-sonnet-5-5","content":[{"type":"text","text":%q}],"usage":{"input_tokens":1,"output_tokens":%d,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}`, id, text, out)
}

func bigToolResult(n int) string {
	return fmt.Sprintf(`{"type":"user","sessionId":"s","timestamp":"2026-11-13T10:00:01.000Z","message":{"role":"user","content":[{"type":"tool_result","content":%q}]}}`, strings.Repeat("r", n))
}

func writeLines(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// A single huge line (a large tool result) used to stop the scan, silently
// dropping every message after it from the totals.
func TestStats_OversizedLinesDoNotTruncateTheScan(t *testing.T) {
	for _, size := range []int{300 * 1024, 5 * 1024 * 1024} { // over the old 256 KB and 4 MB limits
		p := writeLines(t,
			asstLine("m1", 100, 10),
			bigToolResult(size),
			asstLine("m2", 200, 10),
			bigToolResult(size),
			asstLine("m3", 300, 10),
		)
		quick, err := QuickScanStats(p)
		if err != nil {
			t.Fatal(err)
		}
		msgs, err := LoadTranscript(p)
		if err != nil {
			t.Fatal(err)
		}
		full := ComputeStats(msgs)
		for name, s := range map[string]SessionStats{"quick": quick, "full": full} {
			if s.TotalOutputTokens != 600 {
				t.Errorf("%dKB %s: output tokens = %d, want 600 (messages after a big line were dropped)", size/1024, name, s.TotalOutputTokens)
			}
		}
		if len(msgs) != 5 {
			t.Errorf("%dKB: transcript has %d messages, want 5", size/1024, len(msgs))
		}
	}
}

// An assistant message that is itself huge (e.g. a Write of a big file) still carries usage.
func TestStats_HugeAssistantLineStillCounted(t *testing.T) {
	p := writeLines(t, asstLine("m1", 100, 3*1024*1024), asstLine("m2", 50, 10))
	quick, err := QuickScanStats(p)
	if err != nil {
		t.Fatal(err)
	}
	if quick.TotalOutputTokens != 150 {
		t.Errorf("output tokens = %d, want 150", quick.TotalOutputTokens)
	}
}

func TestScanSessionMeta_FindsNamesAfterBigLine(t *testing.T) {
	p := writeLines(t, bigToolResult(400*1024),
		`{"type":"agent-name","agentName":"Planner"}`,
		`{"type":"custom-title","customTitle":"My title"}`)
	a, c := ScanSessionMeta(p)
	if a != "Planner" || c != "My title" {
		t.Errorf("meta = %q, %q", a, c)
	}
}
