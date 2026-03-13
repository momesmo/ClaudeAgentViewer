package data

import "time"

// HistoryEntry represents one line from history.jsonl
type HistoryEntry struct {
	Display   string `json:"display"`
	Timestamp int64  `json:"timestamp"`
	Project   string `json:"project"`
	SessionID string `json:"sessionId"`
}

// SessionInfo is the metadata for a session (no full transcript loaded)
type SessionInfo struct {
	SessionID string
	Project   string
	Display   string
	Timestamp time.Time
	FilePath  string
}


// SessionMessage represents a parsed message from a session JSONL file
type SessionMessage struct {
	Type      string
	SessionID string
	Timestamp string
	GitBranch string
	Version   string
	Model     string
	Content   string         // user: plain text
	Blocks    []ContentBlock // assistant: structured blocks
	Usage     TokenUsage
}

// ContentBlock is a piece of assistant message content
type ContentBlock struct {
	Type             string // "text", "thinking", "tool_use", "tool_result"
	Text             string
	ToolName         string
	ToolInput        string
	ToolID           string
	AgentDescription string // for Agent tool_use: the "description" field
	AgentType        string // for Agent tool_use: the "subagent_type" field
}

// TokenUsage tracks token counts from a single assistant message
type TokenUsage struct {
	InputTokens          int `json:"input_tokens"`
	OutputTokens         int `json:"output_tokens"`
	CacheCreationTokens  int `json:"cache_creation_input_tokens"`
	CacheReadTokens      int `json:"cache_read_input_tokens"`
}

// SessionStats aggregates stats across an entire session transcript
type SessionStats struct {
	TotalInputTokens  int
	TotalOutputTokens int
	CacheWriteTokens  int
	CacheReadTokens   int
	EstimatedCostUSD  float64
	MessageCount      int
	ToolCallCount     int
	Duration          time.Duration
	Models            []string
	ToolsUsed         map[string]int
	// Energy metrics (ccwatt-style)
	EnergyKWh   float64 // estimated energy consumption
	CO2Kg       float64 // estimated CO2 emissions
	TreeDays    float64 // days of tree growth to offset
}

// ComputeEnergy calculates energy/CO2/tree metrics from token counts
// Based on ccwatt methodology: ~0.001 Wh/token, cached reads at 1%
func (s *SessionStats) ComputeEnergy() {
	// Energy: 0.001 Wh per token, cache reads at 1% cost
	fullTokens := float64(s.TotalInputTokens + s.TotalOutputTokens)
	cacheTokens := float64(s.CacheReadTokens) * 0.01
	totalWh := (fullTokens + cacheTokens) * 0.001
	s.EnergyKWh = totalWh / 1000.0

	// CO2: 0.5 kg per kWh (global average grid intensity)
	s.CO2Kg = s.EnergyKWh * 0.5

	// Tree-days: mature tree absorbs ~38.4g CO2/day (14 kg/year)
	if s.CO2Kg > 0 {
		s.TreeDays = (s.CO2Kg * 1000) / 38.4
	}
}

// ProjectGroup groups sessions under a project path
type ProjectGroup struct {
	Project  string
	Sessions []SessionInfo
	Expanded bool
}
