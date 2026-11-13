package data

import "strings"

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
