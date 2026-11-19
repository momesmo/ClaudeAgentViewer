package data

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func userLine(text, branch, model string) string {
	return fmt.Sprintf(`{"type":"user","gitBranch":%q,"message":{"role":"user","model":%q,"content":%q}}`, branch, model, text)
}

func assistantLine(text, branch, model string) string {
	return fmt.Sprintf(`{"type":"assistant","gitBranch":%q,"message":{"role":"assistant","model":%q,"content":[{"type":"text","text":%q},{"type":"tool_use","name":"Grep","input":{}}]}}`, branch, model, text)
}

func toolResultLine(bulk string) string {
	return fmt.Sprintf(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":%q}]}}`, bulk)
}

// Branch and model repeat on every message; they should only be written when
// they change, so they don't eat the budget.
func TestBuildIndexEntry_DedupesRepeatedMetadata(t *testing.T) {
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, userLine(fmt.Sprintf("question %d", i), "feat/same", "claude-opus-5-5"))
	}
	lines = append(lines, userLine("last", "feat/other", "claude-opus-5-5"))
	e, _ := BuildIndexEntry(writeFixture(t, lines...), nil)
	if n := strings.Count(e.Blob, "feat/same"); n != 1 {
		t.Errorf("feat/same appears %d times, want 1", n)
	}
	if n := strings.Count(e.Blob, "claude-opus-5-5"); n != 1 {
		t.Errorf("model appears %d times, want 1 (unchanged across the session)", n)
	}
	if !strings.Contains(e.Blob, "feat/other") {
		t.Errorf("changed branch must be recorded: %q", e.Blob)
	}
}

// Long sessions keep the beginning AND the end, not just the beginning.
func TestBuildIndexEntry_KeepsHeadAndTailOfLongSession(t *testing.T) {
	var lines []string
	for i := 0; i < 6000; i++ {
		lines = append(lines, userLine(fmt.Sprintf("message-%04d %s", i, strings.Repeat("filler ", 15)), "b", "m"))
		if i%50 == 0 {
			lines = append(lines, toolResultLine(strings.Repeat("bulk", 20000))) // unindexed bulk between messages
		}
	}
	e, _ := BuildIndexEntry(writeFixture(t, lines...), nil)
	if len(e.Blob) > MaxBlobBytes {
		t.Fatalf("blob %d bytes exceeds cap %d", len(e.Blob), MaxBlobBytes)
	}
	for _, want := range []string{"message-0000", "message-0001", "message-5999", "message-5998"} {
		if !strings.Contains(e.Blob, want) {
			t.Errorf("blob missing %s", want)
		}
	}
	if strings.Contains(e.Blob, "message-3000 ") {
		t.Errorf("middle of a long session should be dropped")
	}
	if strings.Contains(e.Blob, "bulkbulk") {
		t.Errorf("tool_result content must not be indexed")
	}
	// Chronological order: head first, tail after, tail in ascending order.
	if !(strings.Index(e.Blob, "message-0000") < strings.Index(e.Blob, "message-5990") &&
		strings.Index(e.Blob, "message-5990") < strings.Index(e.Blob, "message-5999")) {
		t.Errorf("blob is not in chronological order")
	}
}

// A session that fits within the cap is indexed completely, with no duplicates.
func TestBuildIndexEntry_SmallSessionIsComplete(t *testing.T) {
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, userLine(fmt.Sprintf("only-once-%02d", i), "b", "m"))
	}
	e, _ := BuildIndexEntry(writeFixture(t, lines...), nil)
	for i := 0; i < 50; i++ {
		if n := strings.Count(e.Blob, fmt.Sprintf("only-once-%02d", i)); n != 1 {
			t.Errorf("only-once-%02d appears %d times", i, n)
		}
	}
}

// A session just over the head budget but under the cap must lose nothing
// (the head/tail regions must neither overlap nor leave a gap).
func TestBuildIndexEntry_MidSizedSessionLosesNothing(t *testing.T) {
	var lines []string
	n := 0
	for total := 0; total < 150*1024; n++ { // > HeadBytes, < MaxBlobBytes
		s := fmt.Sprintf("item-%05d %s", n, strings.Repeat("pad ", 40))
		total += len(s) + 1
		lines = append(lines, userLine(s, "b", "m"))
	}
	e, _ := BuildIndexEntry(writeFixture(t, lines...), nil)
	for i := 0; i < n; i++ {
		if c := strings.Count(e.Blob, fmt.Sprintf("item-%05d ", i)); c != 1 {
			t.Fatalf("item-%05d appears %d times, want 1", i, c)
		}
	}
}

// Walking the file backwards in small chunks must give the same tail as a
// straightforward forward read (lines straddling chunk boundaries, CRLF, big
// unindexed lines).
func TestBuildIndexEntry_TailMatchesForwardRead_AcrossChunkBoundaries(t *testing.T) {
	old := tailChunkBytes
	tailChunkBytes = 4096
	defer func() { tailChunkBytes = old }()

	var lines []string
	var want []string
	for i := 0; i < 1200; i++ {
		text := fmt.Sprintf("m%04d %s", i, strings.Repeat("abcdefghij", i%37))
		lines = append(lines, userLine(text, "b", "m"))
		want = append(want, strings.ToLower(text))
		if i%7 == 0 {
			lines = append(lines, toolResultLine(strings.Repeat("z", 1000+i*13)))
		}
	}
	e, _ := BuildIndexEntry(writeFixture(t, lines...), nil)
	// The last 20 messages must all be present, intact, in order.
	last := want[len(want)-20:]
	pos := 0
	for _, w := range last {
		i := strings.Index(e.Blob[pos:], w)
		if i < 0 {
			t.Fatalf("tail message missing or corrupted: %.40q", w)
		}
		pos += i + len(w)
	}
	if strings.Contains(e.Blob, "zzzz") {
		t.Errorf("tool_result leaked into blob")
	}
}

func TestBuildIndexEntry_CRLFLines(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	var sb strings.Builder
	for i := 0; i < 400; i++ {
		sb.WriteString(userLine(fmt.Sprintf("crlf-%03d %s", i, strings.Repeat("x ", 300)), "b", "m") + "\r\n")
	}
	if err := os.WriteFile(p, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	e, _ := BuildIndexEntry(p, nil)
	for _, w := range []string{"crlf-000", "crlf-399"} {
		if !strings.Contains(e.Blob, w) {
			t.Errorf("missing %s", w)
		}
	}
}

// A prompt larger than maxLineBytes (a big paste) is truncated, not skipped.
func TestBuildIndexEntry_OversizePromptIsTruncatedNotSkipped(t *testing.T) {
	big := "needle-start " + strings.Repeat("filler ", 400*1024) + " needle-end" // ~2.8 MB
	p := writeFixture(t, userLine("hello first", "b", "m"), userLine(big, "b", "m"), userLine("after the big one", "b", "m"))
	e, _ := BuildIndexEntry(p, nil)
	for _, w := range []string{"hello first", "needle-start", "after the big one"} {
		if !strings.Contains(e.Blob, w) {
			t.Errorf("missing %q", w)
		}
	}
	if len(e.Blob) > MaxBlobBytes {
		t.Errorf("blob %d exceeds cap", len(e.Blob))
	}
}

// Same, but the big line sits in the tail region (found by the backward walk).
func TestBuildIndexEntry_OversizePromptInTailRegion(t *testing.T) {
	old := tailChunkBytes
	tailChunkBytes = 64 * 1024
	defer func() { tailChunkBytes = old }()

	var lines []string
	for i := 0; i < 2500; i++ {
		lines = append(lines, userLine(fmt.Sprintf("early-%04d %s", i, strings.Repeat("e", 100)), "b", "m"))
	}
	big := "tail-needle " + strings.Repeat("filler ", 400*1024)
	lines = append(lines, userLine(big, "b", "m"), userLine("very-last-message", "b", "m"))
	e, _ := BuildIndexEntry(writeFixture(t, lines...), nil)
	for _, w := range []string{"early-0000", "tail-needle", "very-last-message"} {
		if !strings.Contains(e.Blob, w) {
			t.Errorf("missing %q (len=%d)", w, len(e.Blob))
		}
	}
	if len(e.Blob) > MaxBlobBytes {
		t.Errorf("blob %d exceeds cap", len(e.Blob))
	}
}

// Oversize lines that are not message text (tool results) stay skipped.
func TestBuildIndexEntry_OversizeToolResultIgnored(t *testing.T) {
	p := writeFixture(t, userLine("before", "b", "m"), toolResultLine(strings.Repeat("secret-bulk ", 300*1024)), userLine("after", "b", "m"))
	e, _ := BuildIndexEntry(p, nil)
	if strings.Contains(e.Blob, "secret-bulk") || !strings.Contains(e.Blob, "before") || !strings.Contains(e.Blob, "after") {
		t.Errorf("unexpected blob: %.80q", e.Blob)
	}
}

func TestSalvageText(t *testing.T) {
	cases := []struct {
		name, line, want string
	}{
		{"string content, cut mid-escape", `{"type":"user","message":{"role":"user","content":"abc é def \`, "abc é def "},
		{"string content, cut mid-unicode-escape", `{"type":"user","message":{"role":"user","content":"abc \u00`, "abc "},
		{"escaped newline and quote", `{"type":"user","message":{"role":"user","content":"line1\nsay \"hi\" ok`, "line1\nsay \"hi\" ok"},
		{"block text", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"reply text here`, "reply text here"},
		{"tool_result is not text", `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"x`, ""},
		{"other line type", `{"type":"summary","summary":"abc`, ""},
	}
	for _, c := range cases {
		got, _ := salvageText([]byte(c.line))
		if got != c.want {
			t.Errorf("%s: salvageText = %q, want %q", c.name, got, c.want)
		}
	}
}
