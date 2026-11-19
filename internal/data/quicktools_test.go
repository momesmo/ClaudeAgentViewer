package data

import (
	"os"
	"path/filepath"
	"testing"
)

// QuickScanStats reported 0 tool calls: tool_use blocks live on assistant
// lines, which the scan hands to the usage path without counting them.
func TestQuickScanStatsCountsToolCalls(t *testing.T) {
	line := func(typ, id, block string) string {
		return `{"type":"` + typ + `","message":{"id":"` + id + `","model":"claude-sonnet-5-5","content":[` + block + `],"usage":{"input_tokens":1,"output_tokens":1}}}` + "\n"
	}
	body := line("assistant", "m1", `{"type":"thinking","thinking":"x"}`) +
		line("assistant", "m1", `{"type":"tool_use","id":"t1","name":"Read","input":{}}`) +
		line("assistant", "m2", `{"type":"tool_use","id":"t2","name":"Bash","input":{}}`) +
		line("assistant", "m2", `{"type":"text","text":"mentions \"type\":\"tool_use\" in prose"}`)
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := QuickScanStats(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.ToolCallCount != 2 {
		t.Errorf("ToolCallCount = %d, want 2", st.ToolCallCount)
	}
	msgs, _ := LoadTranscript(p)
	if full := ComputeStats(msgs); full.ToolCallCount != st.ToolCallCount {
		t.Errorf("quick %d vs full %d", st.ToolCallCount, full.ToolCallCount)
	}
}

// A user line that carries the literal "tool_use" (for example a structured
// tool result) is not a tool call; the full scan does not count it either.
func TestQuickScanStatsIgnoresUserLinesQuotingToolUse(t *testing.T) {
	body := `{"type":"user","message":{"role":"user","content":"hi"},"toolUseResult":{"kind":"tool_use","k2":"tool_use","x":"\"tool_use\""}}` + "\n" +
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"a \"type\":\"tool_use\" quote"}]},"note":"tool_use","raw":"tool_use"}` + "\n" +
		`{"type":"user","message":{"role":"user","content":"x"},"a":"tool_use"}` + "\n"
	p := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := QuickScanStats(p)
	if err != nil || st.ToolCallCount != 0 || st.MessageCount != 3 {
		t.Errorf("tools=%d msgs=%d err=%v, want 0 tools 3 msgs", st.ToolCallCount, st.MessageCount, err)
	}
}
