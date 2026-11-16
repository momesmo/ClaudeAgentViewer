package data

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// rawMessage is the JSON structure of a session JSONL line
type rawMessage struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	Timestamp string          `json:"timestamp"`
	GitBranch string          `json:"gitBranch"`
	Version   string          `json:"version"`
	Message   json.RawMessage `json:"message"`
	UUID      string          `json:"uuid"`
}

type rawMessageContent struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Model   string          `json:"model"`
	Usage   rawUsage        `json:"usage"`
}

type rawUsage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens"`
	CacheReadTokens     int `json:"cache_read_input_tokens"`
	CacheCreation       struct {
		Ephemeral5m int `json:"ephemeral_5m_input_tokens"`
		Ephemeral1h int `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	ServerToolUse struct {
		WebSearchRequests int `json:"web_search_requests"`
	} `json:"server_tool_use"`
	Speed        string `json:"speed"`
	InferenceGeo string `json:"inference_geo"`
}

// usage converts the raw fields into a billable Usage for model.
func (u rawUsage) usage(model string) Usage {
	w5, w1 := splitCacheWrites(u.CacheCreationTokens, u.CacheCreation.Ephemeral5m, u.CacheCreation.Ephemeral1h)
	return Usage{
		Model: model, Input: u.InputTokens, Output: u.OutputTokens, CacheRead: u.CacheReadTokens,
		CacheWrite5m: w5, CacheWrite1h: w1, WebSearches: u.ServerToolUse.WebSearchRequests,
		Speed: u.Speed, Geo: u.InferenceGeo,
	}
}

// tokenUsage converts the raw fields into the exported per-line TokenUsage.
func (u rawUsage) tokenUsage() TokenUsage {
	w5, w1 := splitCacheWrites(u.CacheCreationTokens, u.CacheCreation.Ephemeral5m, u.CacheCreation.Ephemeral1h)
	return TokenUsage{
		InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, CacheReadTokens: u.CacheReadTokens,
		CacheCreationTokens: w5 + w1, CacheWrite5m: w5, CacheWrite1h: w1,
		WebSearches: u.ServerToolUse.WebSearchRequests, Speed: u.Speed, Geo: u.InferenceGeo,
	}
}

type rawContentBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	Name     string          `json:"name"`
	ID       string          `json:"id"`
	Input    json.RawMessage `json:"input"`
	Content  json.RawMessage `json:"content"` // tool_result content
}

// FindSessionFile locates the JSONL file for a session
func FindSessionFile(claudeDir, project, sessionID string) string {
	// Convert project path to directory name (C:\repo -> C--repo)
	projectsDir := filepath.Join(claudeDir, "projects")

	// Try scanning project dirs
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(projectsDir, entry.Name(), sessionID+".jsonl")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// LoadTranscript parses a session JSONL file into messages
func LoadTranscript(path string) ([]SessionMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var messages []SessionMessage
	scanner := newLineScanner(f, maxStatsLineBytes)

	for scanner.Scan() {
		line := scanner.Bytes()
		var raw rawMessage
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}

		// Skip non-renderable types
		switch raw.Type {
		case "user", "assistant":
			// process these
		default:
			continue
		}

		msg := SessionMessage{
			Type:      raw.Type,
			SessionID: raw.SessionID,
			Timestamp: raw.Timestamp,
			GitBranch: raw.GitBranch,
			Version:   raw.Version,
		}

		if raw.Message == nil {
			continue
		}

		var content rawMessageContent
		if err := json.Unmarshal(raw.Message, &content); err != nil {
			continue
		}

		msg.Model = content.Model
		msg.MessageID = content.ID
		msg.Usage = content.Usage.tokenUsage()

		if raw.Type == "user" {
			// User content is either a string or array
			var plainText string
			if err := json.Unmarshal(content.Content, &plainText); err == nil {
				msg.Content = plainText
			} else {
				msg.Content = string(content.Content)
			}
		} else if raw.Type == "assistant" {
			// Parse content blocks
			var blocks []rawContentBlock
			if err := json.Unmarshal(content.Content, &blocks); err == nil {
				for _, b := range blocks {
					cb := ContentBlock{Type: b.Type}
					switch b.Type {
					case "text":
						cb.Text = b.Text
					case "thinking":
						cb.Text = b.Thinking
					case "tool_use":
						cb.ToolName = b.Name
						cb.ToolID = b.ID
						if b.Input != nil {
							inputStr := string(b.Input)
							// Extract Agent-specific fields
							if b.Name == "Agent" {
								var agentInput struct {
									Description  string `json:"description"`
									SubagentType string `json:"subagent_type"`
								}
								if json.Unmarshal(b.Input, &agentInput) == nil {
									cb.AgentDescription = agentInput.Description
									cb.AgentType = agentInput.SubagentType
								}
							}
							if len(inputStr) > 500 {
								inputStr = inputStr[:500] + "..."
							}
							cb.ToolInput = inputStr
						}
					case "tool_result":
						if b.Content != nil {
							cb.Text = string(b.Content)
							if len(cb.Text) > 500 {
								cb.Text = cb.Text[:500] + "..."
							}
						}
					}
					msg.Blocks = append(msg.Blocks, cb)
				}
			}
		}

		messages = append(messages, msg)
	}

	return messages, scanner.Err()
}

// ComputeStats computes aggregate statistics for a session transcript
func ComputeStats(messages []SessionMessage) SessionStats {
	stats := SessionStats{
		ToolsUsed: make(map[string]int),
	}

	tally := NewUsageTally(DefaultBook())
	var firstTime, lastTime time.Time

	for _, msg := range messages {
		stats.MessageCount++

		if msg.Timestamp != "" {
			t, err := time.Parse(time.RFC3339, msg.Timestamp)
			if err == nil {
				t, _ = time.Parse("2006-01-02T15:04:05.000Z", msg.Timestamp)
			}
			if !t.IsZero() {
				if firstTime.IsZero() || t.Before(firstTime) {
					firstTime = t
				}
				if lastTime.IsZero() || t.After(lastTime) {
					lastTime = t
				}
			}
		}

		if msg.Type == "assistant" {
			tally.Add(msg.MessageID, Usage{
				Model: msg.Model, Input: msg.Usage.InputTokens, Output: msg.Usage.OutputTokens,
				CacheRead: msg.Usage.CacheReadTokens, CacheWrite5m: msg.Usage.CacheWrite5m,
				CacheWrite1h: msg.Usage.CacheWrite1h, WebSearches: msg.Usage.WebSearches,
				Speed: msg.Usage.Speed, Geo: msg.Usage.Geo,
			})

			for _, block := range msg.Blocks {
				if block.Type == "tool_use" {
					stats.ToolCallCount++
					stats.ToolsUsed[block.ToolName]++
				}
			}
		}
	}

	if !firstTime.IsZero() && !lastTime.IsZero() {
		stats.Duration = lastTime.Sub(firstTime)
	}

	tally.Apply(&stats) // tokens, cost and models: each API message counted once, priced per model
	stats.ComputeEnergy()

	return stats
}
