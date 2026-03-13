package data

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// QuickStats reads token usage from a session JSONL without full message parsing.
// Only scans for "usage" fields in assistant messages — much faster than LoadTranscript.
type quickUsage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheReadTokens     int `json:"cache_read_input_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens"`
}

type quickMessage struct {
	Model string     `json:"model"`
	Usage quickUsage `json:"usage"`
}

type quickLine struct {
	Type    string       `json:"type"`
	Message quickMessage `json:"message"`
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
	modelSet := make(map[string]bool)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

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
		stats.TotalInputTokens += ql.Message.Usage.InputTokens
		stats.TotalOutputTokens += ql.Message.Usage.OutputTokens
		stats.CacheReadTokens += ql.Message.Usage.CacheReadTokens

		if ql.Message.Model != "" {
			modelSet[ql.Message.Model] = true
		}
	}

	for model := range modelSet {
		stats.Models = append(stats.Models, model)
	}

	// Cost estimation
	inputRate := 3.0 / 1_000_000
	outputRate := 15.0 / 1_000_000
	for _, model := range stats.Models {
		if strings.Contains(model, "opus") {
			inputRate = 15.0 / 1_000_000
			outputRate = 75.0 / 1_000_000
			break
		}
	}
	stats.EstimatedCostUSD = float64(stats.TotalInputTokens)*inputRate + float64(stats.TotalOutputTokens)*outputRate

	// Energy metrics
	stats.ComputeEnergy()

	return stats, nil
}
