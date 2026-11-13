package data

import (
	"strings"
	"unicode/utf8"
)

// MatchKind describes where a search filter matched a session.
type MatchKind int

const (
	MatchNone    MatchKind = iota
	MatchMeta              // matched display / id / project / agent name / custom title
	MatchContent           // matched only in the content blob
)

// FillSearchMeta sets s.SearchMeta to the lowercased metadata fields joined by "\x00".
func (s *SessionInfo) FillSearchMeta() {
	s.SearchMeta = strings.ToLower(strings.Join(
		[]string{s.Display, s.SessionID, s.Project, s.AgentName, s.CustomTitle}, "\x00"))
}

// MatchSession reports where filterLower (already lowercase) matches. An empty filter returns MatchMeta.
func MatchSession(s SessionInfo, blob string, filterLower string) MatchKind {
	if filterLower == "" {
		return MatchMeta
	}
	meta := s.SearchMeta
	if meta == "" {
		s.FillSearchMeta()
		meta = s.SearchMeta
	}
	if strings.Contains(meta, filterLower) {
		return MatchMeta
	}
	if blob != "" && strings.Contains(blob, filterLower) {
		return MatchContent
	}
	return MatchNone
}

// Snippet is one occurrence of a search term with a little surrounding text
// from the same message.
type Snippet struct {
	Before, Match, After string
}

// FindSnippets returns up to max snippets of filterLower in blob (non-
// overlapping, in order) plus the total number of occurrences (counted up to a
// cap). Each snippet has at most ctx bytes of context on either side, never
// crosses a message boundary (a newline in the blob), never splits a rune, and
// ends in an ellipsis where it was clipped. filterLower must be lowercase; the
// blob is lowercase already, so snippets are lowercase too.
func FindSnippets(blob, filterLower string, max, ctx int) (snips []Snippet, total int) {
	const countCap = 9999
	if filterLower == "" {
		return nil, 0
	}
	pos := 0
	for pos <= len(blob) && total < countCap {
		i := strings.Index(blob[pos:], filterLower)
		if i < 0 {
			break
		}
		abs := pos + i
		total++
		if len(snips) < max {
			snips = append(snips, snippetAt(blob, abs, len(filterLower), ctx))
		}
		pos = abs + len(filterLower)
	}
	return snips, total
}

func snippetAt(blob string, abs, n, ctx int) Snippet {
	lineStart := strings.LastIndexByte(blob[:abs], '\n') + 1
	lineEnd := len(blob)
	if j := strings.IndexByte(blob[abs+n:], '\n'); j >= 0 {
		lineEnd = abs + n + j
	}
	s0, e0 := abs-ctx, abs+n+ctx
	if s0 < lineStart {
		s0 = lineStart
	}
	if e0 > lineEnd {
		e0 = lineEnd
	}
	for s0 < abs && !utf8.RuneStart(blob[s0]) {
		s0++
	}
	for e0 > abs+n && e0 < len(blob) && !utf8.RuneStart(blob[e0]) {
		e0--
	}
	sn := Snippet{Before: blob[s0:abs], Match: blob[abs : abs+n], After: blob[abs+n : e0]}
	if s0 > lineStart {
		sn.Before = "…" + sn.Before
	}
	if e0 < lineEnd {
		sn.After += "…"
	}
	return sn
}

// MatchedFields lists which session metadata fields contain filterLower (for
// example "prompt", "project"), in a fixed order. Content matches are not
// included; use FindSnippets for those.
func MatchedFields(s SessionInfo, filterLower string) []string {
	if filterLower == "" {
		return nil
	}
	var out []string
	check := func(label, v string) {
		if v != "" && strings.Contains(strings.ToLower(v), filterLower) {
			out = append(out, label)
		}
	}
	check("prompt", s.Display)
	check("session ID", s.SessionID)
	check("project", s.Project)
	check("agent name", s.AgentName)
	check("title", s.CustomTitle)
	return out
}
