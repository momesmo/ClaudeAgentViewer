package data

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SubagentMeta is the parsed .meta.json for a subagent
type SubagentMeta struct {
	AgentType string `json:"agentType"`
}

// SubagentInfo holds metadata for a subagent. Transcript is loaded lazily.
type SubagentInfo struct {
	AgentID      string
	AgentType    string // "Explore", "Plan", "general-purpose"
	FilePath     string
	IsRunning    bool
	LastActivity time.Time
	Expanded     bool             // user toggled this subagent open
	Loaded       bool             // transcript has been loaded
	Messages     []SessionMessage // loaded on demand
	Stats        SessionStats     // computed after loading
}

const runningThreshold = 30 * time.Second

// findSubagentDir locates the subagents directory for a session
func findSubagentDir(claudeDir, sessionID string) string {
	projectsDir := filepath.Join(claudeDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(projectsDir, entry.Name(), sessionID, "subagents")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

// ScanSubagents does a lightweight scan: reads meta + checks running status.
// Does NOT load any transcripts.
func ScanSubagents(claudeDir, sessionID string) ([]SubagentInfo, error) {
	subagentDir := findSubagentDir(claudeDir, sessionID)
	if subagentDir == "" {
		return nil, nil
	}

	files, err := os.ReadDir(subagentDir)
	if err != nil {
		return nil, err
	}

	var subagents []SubagentInfo
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".meta.json") {
			continue
		}

		metaPath := filepath.Join(subagentDir, f.Name())
		metaData, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var meta SubagentMeta
		if err := json.Unmarshal(metaData, &meta); err != nil {
			continue
		}

		agentID := strings.TrimSuffix(f.Name(), ".meta.json")
		jsonlPath := filepath.Join(subagentDir, agentID+".jsonl")
		fi, err := os.Stat(jsonlPath)
		if err != nil {
			continue
		}

		sa := SubagentInfo{
			AgentID:      agentID,
			AgentType:    meta.AgentType,
			FilePath:     jsonlPath,
			LastActivity: fi.ModTime(),
			IsRunning:    detectRunning(jsonlPath, fi.ModTime()),
		}

		subagents = append(subagents, sa)
	}

	sortSubagents(subagents)
	return subagents, nil
}

// RefreshSubagentStatus updates only the running status and sorts.
// Preserves Expanded/Loaded/Messages/Stats from the old list.
func RefreshSubagentStatus(claudeDir, sessionID string, old []SubagentInfo) []SubagentInfo {
	subagentDir := findSubagentDir(claudeDir, sessionID)
	if subagentDir == "" {
		return old
	}

	files, err := os.ReadDir(subagentDir)
	if err != nil {
		return old
	}

	// Build lookup of existing subagents
	oldMap := make(map[string]*SubagentInfo, len(old))
	for i := range old {
		oldMap[old[i].AgentID] = &old[i]
	}

	var result []SubagentInfo
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".meta.json") {
			continue
		}

		agentID := strings.TrimSuffix(f.Name(), ".meta.json")
		jsonlPath := filepath.Join(subagentDir, agentID+".jsonl")
		fi, err := os.Stat(jsonlPath)
		if err != nil {
			continue
		}

		if existing, ok := oldMap[agentID]; ok {
			// Update running status, preserve everything else
			existing.IsRunning = detectRunning(jsonlPath, fi.ModTime())
			existing.LastActivity = fi.ModTime()
			result = append(result, *existing)
		} else {
			// New subagent appeared
			metaPath := filepath.Join(subagentDir, f.Name())
			metaData, err := os.ReadFile(metaPath)
			if err != nil {
				continue
			}
			var meta SubagentMeta
			if err := json.Unmarshal(metaData, &meta); err != nil {
				continue
			}
			result = append(result, SubagentInfo{
				AgentID:      agentID,
				AgentType:    meta.AgentType,
				FilePath:     jsonlPath,
				LastActivity: fi.ModTime(),
				IsRunning:    detectRunning(jsonlPath, fi.ModTime()),
			})
		}
	}

	sortSubagents(result)
	return result
}

func sortSubagents(subagents []SubagentInfo) {
	sort.Slice(subagents, func(i, j int) bool {
		if subagents[i].IsRunning != subagents[j].IsRunning {
			return subagents[i].IsRunning
		}
		return subagents[i].LastActivity.After(subagents[j].LastActivity)
	})
}

// detectRunning checks if a subagent is still running.
// Uses mtime as a fast pre-check before reading the file.
func detectRunning(jsonlPath string, mtime time.Time) bool {
	// Fast path: if file hasn't been modified recently, it's done
	if time.Since(mtime) >= runningThreshold {
		return false
	}
	// Slow path: check if last line has end_turn
	return !checkLastLineEndTurn(jsonlPath)
}

func checkLastLineEndTurn(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return false
	}
	size := fi.Size()

	readSize := int64(8192)
	if readSize > size {
		readSize = size
	}
	if _, err := f.Seek(size-readSize, 0); err != nil {
		return false
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	var lastLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			lastLine = line
		}
	}

	if lastLine == "" {
		return false
	}

	return strings.Contains(lastLine, `"stop_reason":"end_turn"`)
}

// LoadSubagentTranscript loads the transcript for a subagent (call on expand)
func LoadSubagentTranscript(info *SubagentInfo) error {
	messages, err := LoadTranscript(info.FilePath)
	if err != nil {
		return err
	}
	info.Messages = messages
	info.Stats = ComputeStats(messages)
	info.Loaded = true
	return nil
}

// CountRunningSubagents returns how many subagents are currently running
func CountRunningSubagents(subagents []SubagentInfo) int {
	count := 0
	for _, sa := range subagents {
		if sa.IsRunning {
			count++
		}
	}
	return count
}

// QuickSubagentCount does the cheapest possible check: just count .jsonl files
// and check mtime. Only used for session list badges.
func QuickSubagentCount(claudeDir, sessionID string) (total int, running int) {
	subagentDir := findSubagentDir(claudeDir, sessionID)
	if subagentDir == "" {
		return 0, 0
	}

	files, err := os.ReadDir(subagentDir)
	if err != nil {
		return 0, 0
	}

	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".jsonl") {
			continue
		}
		total++
		// Only check running for recently-modified files (fast path)
		fi, err := os.Stat(filepath.Join(subagentDir, f.Name()))
		if err != nil {
			continue
		}
		if time.Since(fi.ModTime()) < runningThreshold {
			if !checkLastLineEndTurn(filepath.Join(subagentDir, f.Name())) {
				running++
			}
		}
	}
	return total, running
}

// SubagentTypeEmoji returns the display emoji for a subagent type
func SubagentTypeEmoji(agentType string) string {
	switch agentType {
	case "Explore":
		return "🔍"
	case "Plan":
		return "📋"
	default:
		return "🤖"
	}
}

// SubagentStatusEmoji returns a status indicator
func SubagentStatusEmoji(running bool) string {
	if running {
		return "🟢"
	}
	return "⚪"
}
