package data

import (
	"strings"
	"testing"
)

func scanLines(in string, limit int) []string {
	s := newLineScanner(strings.NewReader(in), limit)
	var out []string
	for s.Scan() {
		out = append(out, string(s.Bytes()))
	}
	return out
}

func TestLineScanner(t *testing.T) {
	big := strings.Repeat("x", 500)
	cases := []struct {
		name, in string
		want     []string
	}{
		{"plain", "a\nb\nc\n", []string{"a", "b", "c"}},
		{"oversize skipped, later lines kept", "a\n" + big + "\nb\nc\n", []string{"a", "b", "c"}},
		{"final line without newline", "a\nb", []string{"a", "b"}},
		{"oversize final line without newline skipped", "a\n" + big, []string{"a"}},
		{"oversize final line with newline skipped", "a\n" + big + "\n", []string{"a"}},
		{"CRLF", "a\r\nb\r\n", []string{"a", "b"}},
		{"CRLF around oversize", "a\r\n" + big + "\r\nb\r\n", []string{"a", "b"}},
		{"empty lines skipped", "a\n\n\nb\n", []string{"a", "b"}},
		{"line within the limit (newline counts toward it) kept", "a\n" + strings.Repeat("y", 99) + "\nb\n", []string{"a", strings.Repeat("y", 99), "b"}},
		{"two oversize lines in a row", big + "\n" + big + "\nz\n", []string{"z"}},
	}
	for _, c := range cases {
		got := scanLines(c.in, 100)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s: got %d lines %q, want %q", c.name, len(got), got, c.want)
		}
	}
}

// An oversized line must not drop the messages after it, in either scan.
func TestStatsScansSkipOversizedLineAndKeepLaterMessages(t *testing.T) {
	defer SetStatsLineLimitForTest(2048)()
	asst := func(id string) string {
		return `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"id":"` + id + `","role":"assistant","model":"claude-sonnet-5-5","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"
	}
	huge := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"` + strings.Repeat("z", 10000) + `"}]}}` + "\n"
	p := writeTemp(t, asst("m1")+huge+asst("m2")+asst("m3"))
	q, err := QuickScanStats(p)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := LoadTranscript(p)
	if err != nil {
		t.Fatal(err)
	}
	f := ComputeStats(msgs)
	for name, st := range map[string]SessionStats{"quick": q, "full": f} {
		if st.MessageCount != 3 || st.TotalInputTokens != 30 || st.TotalOutputTokens != 15 {
			t.Errorf("%s: msgs=%d in=%d out=%d, want 3/30/15 (oversize user line skipped, later messages kept)",
				name, st.MessageCount, st.TotalInputTokens, st.TotalOutputTokens)
		}
	}
}
