package data

import (
	"encoding/json"
	"os"
	"strings"
)

// QuickStats reads token usage from a session JSONL without full message parsing.
// Only scans for "usage" fields in assistant messages — much faster than LoadTranscript.
type quickMessage struct {
	ID    string   `json:"id"`
	Model string   `json:"model"`
	Usage rawUsage `json:"usage"`
}

type quickLine struct {
	Type    string       `json:"type"`
	Message quickMessage `json:"message"`
}

type metaLine struct {
	Type        string `json:"type"`
	AgentName   string `json:"agentName"`
	CustomTitle string `json:"customTitle"`
}

// ScanSessionMeta reads the first occurrence of agentName and customTitle from a session file.
func ScanSessionMeta(path string) (agentName, customTitle string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := newLineScanner(f, maxLineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if agentName != "" && customTitle != "" {
			break
		}
		if !strings.Contains(string(line), `"agent-name"`) && !strings.Contains(string(line), `"custom-title"`) {
			continue
		}
		var ml metaLine
		if err := json.Unmarshal(line, &ml); err != nil {
			continue
		}
		if ml.Type == "agent-name" && agentName == "" {
			agentName = ml.AgentName
		}
		if ml.Type == "custom-title" && customTitle == "" {
			customTitle = ml.CustomTitle
		}
	}
	return
}

// QuickScanStats reads just token counts and message count from a session file.
// This is ~10x faster than LoadTranscript because it skips content parsing.
func QuickScanStats(path string) (SessionStats, error) {
	f, err := os.Open(path)
	if err != nil {
		return SessionStats{}, err
	}
	defer f.Close()

	stats := SessionStats{ToolsUsed: make(map[string]int)}
	tally := NewUsageTally(DefaultBook())

	scanner := newLineScanner(f, maxStatsLineBytes)

	for scanner.Scan() {
		line := scanner.Bytes()

		// Fast pre-check: skip lines that aren't assistant messages
		if !strings.Contains(string(line), `"output_tokens"`) {
			// Count user messages
			if strings.Contains(string(line), `"type":"user"`) {
				stats.MessageCount++
			}
			// Count tool uses
			if strings.Contains(string(line), `"tool_use"`) {
				stats.ToolCallCount++
			}
			continue
		}

		var ql quickLine
		if err := json.Unmarshal(line, &ql); err != nil {
			continue
		}

		if ql.Type != "assistant" {
			continue
		}

		stats.MessageCount++
		// one content block per line, so this counts each tool call once
		stats.ToolCallCount += strings.Count(string(line), `"type":"tool_use"`)
		tally.Add(ql.Message.ID, ql.Message.Usage.usage(ql.Message.Model))
	}

	tally.Apply(&stats) // each API message counted once, priced per model

	// Energy metrics
	stats.ComputeEnergy()

	return stats, nil
}
