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
	SessionID   string
	Project     string
	Display     string
	Timestamp   time.Time
	FilePath    string
	AgentName   string
	CustomTitle string
	SearchMeta  string // lowercase metadata joined by \x00; see FillSearchMeta
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

// ComputeEnergy calculates energy/CO2/tree-day metrics from token counts.
//
// Methodology & sources:
//   - Energy per token: ~0.001 Wh (1 mWh) per token for inference on frontier models.
//     Based on benchmarks of Llama-3.3-70B on H100 (~0.39J/token) and GPT-4o query
//     estimates (~0.42 Wh per short query). See: arXiv:2505.09598, arXiv:2509.08867.
//     Cache reads use ~1% of full computation energy (ccwatt methodology).
//   - Carbon intensity: 0.390 kg CO2/kWh — IEA 2024 global average for data centers.
//     Individual providers vary (Google claims lower, coal-heavy grids are higher).
//     Source: IEA "Energy and AI" report (2025).
//   - Tree absorption: 14 kg CO2/year per mature tree = 38.4 g/day.
//     Source: European Environment Agency, US Forest Service estimates.
//   - Tree-days: number of days one mature tree needs to absorb the session's CO2.
//     e.g., 9.7 tree-days = 1 tree for ~10 days, or 10 trees for ~1 day.
//
// CAVEAT: These are rough estimates. Anthropic does not publish per-token energy
// data. Actual consumption depends on hardware, batch size, quantization, and
// data center PUE. Treat these as directional, not precise.
func (s *SessionStats) ComputeEnergy() {
	// Energy: ~0.001 Wh per token for full inference
	// Cache reads are ~1% of full compute cost (served from memory, no GPU inference)
	fullTokens := float64(s.TotalInputTokens + s.TotalOutputTokens)
	cacheTokens := float64(s.CacheReadTokens) * 0.01
	totalWh := (fullTokens + cacheTokens) * 0.001
	s.EnergyKWh = totalWh / 1000.0

	// CO2: 0.390 kg per kWh (IEA 2024 global average grid carbon intensity)
	s.CO2Kg = s.EnergyKWh * 0.390

	// Tree-days: mature tree absorbs ~38.4g CO2/day (14 kg/year)
	// Represents how many days of one tree's growth offsets this session's emissions
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
