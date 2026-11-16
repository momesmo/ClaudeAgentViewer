package testfixture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/user/claudevu/internal/data"
)

// BigLineBytes is the size of the oversized tool_result line in the "bigline" session.
const BigLineBytes = 320 * 1024

// ModelExp is the expected per-model share of a session.
type ModelExp struct {
	Messages  int // API messages (each counted once)
	Tokens    int // input + output
	CostUSD   float64
	Unpriced  bool
	Estimated bool // priced by family fallback
}

// SubExp describes a subagent.
type SubExp struct {
	ID       string
	Type     string
	Running  bool
	Lines    int // transcript lines (messages)
	Messages int // unique API messages
	Tokens   int // input + output
}

// SessionExp is one session plus what the app should report for it.
type SessionExp struct {
	Key        string // short stable name used by tests
	ID         string
	Project    string // project path as stored in history.jsonl
	ProjectDir string // directory name under projects/
	Display    string
	Time       time.Time
	HasFile    bool
	Path       string // transcript path ("" when !HasFile)

	CustomTitle string
	AgentName   string
	SearchWord  string // appears only inside the transcript, never in Display

	Lines        int // transcript lines of type user/assistant == MessageCount
	APIMessages  int // distinct assistant messages
	ToolUses     int // tool_use blocks
	Input        int
	Output       int
	CacheRead    int
	CacheWrite   int
	CostUSD      float64
	Models       []string // first-appearance order
	PerModel     map[string]ModelExp
	Unpriced     bool
	Estimated    bool
	UnpricedToks int
	Duration     time.Duration
	Subagents    []SubExp
}

// Tokens is input + output, the figure the dashboard sums.
func (s SessionExp) Tokens() int { return s.Input + s.Output }

// ProjectExp lists a project's sessions, most recent first.
type ProjectExp struct {
	Path     string
	Dir      string
	Sessions []*SessionExp
}

// Fixture is a built claude dir with its expectations.
type Fixture struct {
	ClaudeDir string
	Sessions  []*SessionExp // newest first
	Projects  []*ProjectExp // ordered by most recent session

	// Dashboard expectations (sessions that have a transcript).
	ScanSessions int
	TotalTokens  int
	TotalCost    float64
	ByModel      map[string]ModelExp
}

// Session returns the session with the given key; it fails the test if absent.
func (f *Fixture) Session(t testing.TB, key string) *SessionExp {
	t.Helper()
	for _, s := range f.Sessions {
		if s.Key == key {
			return s
		}
	}
	t.Fatalf("fixture has no session %q", key)
	return nil
}

type tok struct{ in, out, cr, cw5, cw1 int }

type builder struct {
	t       testing.TB
	book    *data.PriceBook
	exp     *SessionExp
	lines   []string
	ts      time.Time
	n       int
	seen    map[string]bool
	matches map[string]data.PriceMatch
}

func newBuilder(t testing.TB, exp *SessionExp, start time.Time) *builder {
	return &builder{t: t, book: data.DefaultBook(), exp: exp, ts: start,
		seen: map[string]bool{}, matches: map[string]data.PriceMatch{}}
}

func (b *builder) stamp() string {
	b.ts = b.ts.Add(time.Second)
	return b.ts.UTC().Format("2006-01-02T15:04:05.000Z")
}

func (b *builder) emit(v map[string]any) {
	v["sessionId"] = b.exp.ID
	b.lines = append(b.lines, mustJSON(b.t, v))
}

func (b *builder) meta(typ, key, val string) {
	b.lines = append(b.lines, mustJSON(b.t, map[string]any{"type": typ, key: val, "sessionId": b.exp.ID}))
}

func (b *builder) user(text string) {
	b.exp.Lines++
	b.emit(map[string]any{"type": "user", "timestamp": b.stamp(), "uuid": b.uuid(),
		"message": map[string]any{"role": "user", "content": text}})
}

// toolResult is a user line carrying a tool_result block.
func (b *builder) toolResult(toolID, content string) {
	b.exp.Lines++
	b.emit(map[string]any{"type": "user", "timestamp": b.stamp(), "uuid": b.uuid(),
		"message": map[string]any{"role": "user", "content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": toolID, "content": content}}}})
}

func (b *builder) uuid() string { b.n++; return fmt.Sprintf("uuid-%s-%d", b.exp.Key, b.n) }

// assistant writes ONE LINE PER BLOCK, each repeating the same message.id and
// usage, exactly as Claude Code does, and records the expectation once.
func (b *builder) assistant(model string, u tok, stop string, blocks ...map[string]any) {
	b.n++
	id := fmt.Sprintf("msg_%s_%d", b.exp.Key, b.n)
	usage := map[string]any{
		"input_tokens": u.in, "output_tokens": u.out,
		"cache_read_input_tokens":     u.cr,
		"cache_creation_input_tokens": u.cw5 + u.cw1,
		"cache_creation":              map[string]any{"ephemeral_5m_input_tokens": u.cw5, "ephemeral_1h_input_tokens": u.cw1},
		"service_tier":                "standard", "speed": "standard", "inference_geo": "global",
	}
	for i, blk := range blocks {
		b.exp.Lines++
		if blk["type"] == "tool_use" {
			b.exp.ToolUses++
		}
		msg := map[string]any{"id": id, "role": "assistant", "model": model, "type": "message",
			"content": []any{blk}, "usage": usage}
		if i == len(blocks)-1 && stop != "" {
			msg["stop_reason"] = stop
		} else {
			msg["stop_reason"] = nil
		}
		b.emit(map[string]any{"type": "assistant", "timestamp": b.stamp(), "uuid": b.uuid(), "message": msg})
	}
	// expectations: once per message, never per line
	e := b.exp
	e.APIMessages++
	e.Input += u.in
	e.Output += u.out
	e.CacheRead += u.cr
	e.CacheWrite += u.cw5 + u.cw1
	c := b.book.Cost(data.Usage{Model: model, Input: u.in, Output: u.out, CacheRead: u.cr,
		CacheWrite5m: u.cw5, CacheWrite1h: u.cw1, Speed: "standard", Geo: "global"})
	e.CostUSD += c.USD
	pm, ok := e.PerModel[model]
	if !ok {
		e.Models = append(e.Models, model)
	}
	pm.Messages++
	pm.Tokens += u.in + u.out
	pm.CostUSD += c.USD
	switch c.Match {
	case data.PriceNone:
		pm.Unpriced, e.Unpriced = true, true
		e.UnpricedToks += u.in + u.out + u.cr + u.cw5 + u.cw1
	case data.PriceFamily:
		pm.Estimated, e.Estimated = true, true
	}
	e.PerModel[model] = pm
}

func text(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
func thinking(s string) map[string]any {
	return map[string]any{"type": "thinking", "thinking": s, "signature": "sig"}
}
func tool(id, name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
}

func mustJSON(t testing.TB, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func write(t testing.TB, path string, lines []string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !mtime.IsZero() {
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
}

func dirName(project string) string { return strings.ReplaceAll(project, "/", "-") }

// BuildClaudeDir writes the fixture under t.TempDir() and returns it.
func BuildClaudeDir(t testing.TB) *Fixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), ".claude")
	now := time.Now()
	f := &Fixture{ClaudeDir: root, ByModel: map[string]ModelExp{}}
	var hist []string

	newSession := func(key, project, display string, age time.Duration, hasFile bool) (*SessionExp, *builder) {
		s := &SessionExp{Key: key, ID: fmt.Sprintf("%s-0000-4000-8000-%012d", key[:min(8, len(key))], len(f.Sessions)+1),
			Project: project, ProjectDir: dirName(project), Display: display,
			Time: now.Add(-age).Truncate(time.Millisecond), HasFile: hasFile, PerModel: map[string]ModelExp{}}
		f.Sessions = append(f.Sessions, s)
		if hasFile {
			s.Path = filepath.Join(root, "projects", s.ProjectDir, s.ID+".jsonl")
		}
		hist = append(hist, mustJSON(t, map[string]any{"display": display, "timestamp": s.Time.UnixMilli(),
			"project": project, "sessionId": s.ID}))
		return s, newBuilder(t, s, s.Time.Add(-time.Hour))
	}
	finish := func(b *builder, extra ...string) {
		s := b.exp
		s.Duration = time.Duration(s.Lines-1) * time.Second // one stamp per message line, 1s apart
		write(t, s.Path, append(b.lines, extra...), time.Time{})
	}

	// ---- project alpha ----
	rec, b := newSession("recent", "/work/alpha", "implement the recent feature", 2*time.Minute, true)
	b.user("implement the recent feature")
	b.assistant("claude-sonnet-5-5", tok{1200, 300, 5000, 800, 0}, "",
		thinking("plan it"), text("Let me look around."), tool("toolu_a1", "Read", map[string]any{"file_path": "/work/alpha/main.go"}))
	b.toolResult("toolu_a1", "package main")
	b.assistant("claude-sonnet-5-5", tok{1500, 450, 7000, 0, 1200}, "",
		tool("toolu_a2", "Agent", map[string]any{"description": "explore the repo", "subagent_type": "Explore", "prompt": "look"}),
		tool("toolu_a3", "Agent", map[string]any{"description": "plan the work", "subagent_type": "Plan", "prompt": "plan"}))
	b.toolResult("toolu_a2", "done exploring")
	b.assistant("claude-sonnet-5-5", tok{900, 200, 9000, 0, 0}, "end_turn", text("All done."))
	finish(b)
	// a duplicate, older history entry for the same session must be ignored
	hist = append(hist, mustJSON(t, map[string]any{"display": "stale older prompt", "timestamp": rec.Time.Add(-time.Hour).UnixMilli(),
		"project": rec.Project, "sessionId": rec.ID}))
	f.subagents(t, rec, now)

	titled, b := newSession("titled", "/work/alpha", "fix the parser", 90*24*time.Hour, true)
	titled.CustomTitle, titled.AgentName = "Refactor the parser", "parser-agent"
	b.meta("agent-name", "agentName", titled.AgentName)
	b.meta("custom-title", "customTitle", titled.CustomTitle)
	b.user("fix the parser")
	b.assistant("claude-opus-5-5", tok{2000, 600, 10000, 1500, 0}, "",
		thinking("hmm"), text("Parser fixed."), tool("toolu_t1", "Edit", map[string]any{"file_path": "p.go"}))
	b.toolResult("toolu_t1", "ok")
	b.assistant("claude-opus-5-5", tok{2100, 400, 12000, 0, 0}, "end_turn", text("Verified."))
	finish(b)

	// ---- project beta ----
	mixed, b := newSession("mixed", "/work/beta", "compare two models", 30*24*time.Hour, true)
	mixed.SearchWord = "zebracrossing"
	b.user("compare two models")
	b.assistant("claude-sonnet-5-5", tok{800, 250, 3000, 500, 0}, "", text("First with sonnet."), tool("toolu_m1", "Bash", map[string]any{"command": "ls"}))
	b.toolResult("toolu_m1", "a b c")
	b.assistant("claude-opus-4-8", tok{1000, 700, 4000, 0, 600}, "", thinking("deeper"), text("Now opus notes the "+mixed.SearchWord+" detail."))
	b.user("thanks")
	b.assistant("claude-sonnet-5-5", tok{700, 150, 6000, 0, 0}, "end_turn", text("Welcome."))
	finish(b)

	_, b = newSession("bigline", "/work/beta", "read the huge file", 60*24*time.Hour, true)
	b.user("read the huge file")
	b.assistant("claude-opus-4-8", tok{500, 100, 0, 2000, 0}, "", tool("toolu_b1", "Read", map[string]any{"file_path": "huge.bin"}))
	b.toolResult("toolu_b1", strings.Repeat("x", BigLineBytes)) // > 300 KB single line
	b.assistant("claude-opus-4-8", tok{600, 900, 2000, 0, 0}, "", text("It is large."))
	b.assistant("claude-opus-4-8", tok{650, 300, 3000, 0, 0}, "end_turn", text("Summary done."))
	finish(b)

	// ---- project gamma ----
	_, b = newSession("mystery", "/work/gamma", "try the new model", 45*24*time.Hour, true)
	b.user("try the new model")
	b.assistant("mystery-model-1", tok{400, 120, 1000, 0, 0}, "", text("Hello from nowhere."))
	b.assistant("mystery-model-1", tok{450, 80, 1500, 0, 0}, "end_turn", text("Bye."))
	finish(b)

	_, b = newSession("tiny", "/work/gamma", "say hi to tiny", 200*24*time.Hour, true)
	b.user("say hi to tiny")
	b.assistant("claude-sonnet-5-5", tok{10, 5, 0, 0, 0}, "end_turn", text("Hello!"))
	finish(b)

	newSession("notranscript", "/work/gamma", "history entry without a transcript", 20*24*time.Hour, false)

	// noise the loader must tolerate
	hist = append(hist, "{not json", mustJSON(t, map[string]any{"display": "no session id", "timestamp": now.UnixMilli(), "project": "/work/alpha"}))
	write(t, filepath.Join(root, "history.jsonl"), hist, time.Time{})

	// newest first; recent history display wins (latest entry per session)
	sortSessions(f.Sessions)
	byProj := map[string]*ProjectExp{}
	for _, s := range f.Sessions {
		p := byProj[s.Project]
		if p == nil {
			p = &ProjectExp{Path: s.Project, Dir: s.ProjectDir}
			byProj[s.Project] = p
			f.Projects = append(f.Projects, p)
		}
		p.Sessions = append(p.Sessions, s)
		if !s.HasFile {
			continue
		}
		f.ScanSessions++
		f.TotalTokens += s.Tokens()
		f.TotalCost += s.CostUSD
		for m, pm := range s.PerModel {
			g := f.ByModel[m]
			g.Messages += pm.Messages
			g.Tokens += pm.Tokens
			g.CostUSD += pm.CostUSD
			g.Unpriced = g.Unpriced || pm.Unpriced
			g.Estimated = g.Estimated || pm.Estimated
			f.ByModel[m] = g
		}
	}
	return f
}

func sortSessions(s []*SessionExp) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].Time.After(s[j-1].Time); j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// subagents writes one running and one completed subagent for s.
func (f *Fixture) subagents(t testing.TB, s *SessionExp, now time.Time) {
	dir := filepath.Join(f.ClaudeDir, "projects", s.ProjectDir, s.ID, "subagents")
	mk := func(id, typ string, running bool) {
		sub := &SessionExp{Key: id, ID: id, PerModel: map[string]ModelExp{}}
		b := newBuilder(t, sub, now.Add(-time.Hour))
		b.user("do the subtask")
		if running { // last line has no end_turn and the file is fresh: still running
			b.assistant("claude-sonnet-5-5", tok{300, 80, 1000, 0, 0}, "", tool("toolu_s1", "Grep", map[string]any{"pattern": "x"}))
		} else {
			b.assistant("claude-sonnet-5-5", tok{300, 80, 1000, 0, 0}, "", tool("toolu_s1", "Grep", map[string]any{"pattern": "x"}))
			b.toolResult("toolu_s1", "match")
			b.assistant("claude-sonnet-5-5", tok{350, 120, 1500, 0, 0}, "end_turn", text("Found it."))
		}
		mtime := now // fresh mtime for the running one
		if !running {
			mtime = now.Add(-10 * time.Minute) // completed: stale and end_turn
		}
		write(t, filepath.Join(dir, id+".jsonl"), b.lines, mtime)
		write(t, filepath.Join(dir, id+".meta.json"), []string{mustJSON(t, map[string]any{"agentType": typ})}, mtime)
		s.Subagents = append(s.Subagents, SubExp{ID: id, Type: typ, Running: running, Lines: sub.Lines,
			Messages: sub.APIMessages, Tokens: sub.Tokens()})
	}
	mk("agent-run0001", "Explore", true)
	mk("agent-done0001", "Plan", false)
}
