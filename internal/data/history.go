package data

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// LoadHistory reads ~/.claude/history.jsonl and returns session info grouped by project
func LoadHistory(claudeDir string) ([]ProjectGroup, error) {
	histPath := filepath.Join(claudeDir, "history.jsonl")
	f, err := os.Open(histPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Deduplicate: keep the latest entry per sessionID
	seen := make(map[string]*SessionInfo)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		var entry HistoryEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.SessionID == "" {
			continue
		}

		ts := time.UnixMilli(entry.Timestamp)
		info := &SessionInfo{
			SessionID: entry.SessionID,
			Project:   entry.Project,
			Display:   entry.Display,
			Timestamp: ts,
		}

		if existing, ok := seen[entry.SessionID]; ok {
			if ts.After(existing.Timestamp) {
				seen[entry.SessionID] = info
			}
		} else {
			seen[entry.SessionID] = info
		}
	}

	// Enrich sessions with agentName/customTitle from their JSONL files
	for id, info := range seen {
		path := FindSessionFile(claudeDir, info.Project, info.SessionID)
		if path != "" {
			info.FilePath = path
			info.AgentName, info.CustomTitle = ScanSessionMeta(path)
		}
		info.FillSearchMeta()
		seen[id] = info
	}

	// Group by project
	projectMap := make(map[string][]SessionInfo)
	for _, info := range seen {
		projectMap[info.Project] = append(projectMap[info.Project], *info)
	}

	// Sort sessions within each project by timestamp descending
	var groups []ProjectGroup
	for project, sessions := range projectMap {
		sort.Slice(sessions, func(i, j int) bool {
			return sessions[i].Timestamp.After(sessions[j].Timestamp)
		})
		groups = append(groups, ProjectGroup{
			Project:  project,
			Sessions: sessions,
			Expanded: false,
		})
	}

	// Sort projects by most recent session
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].Sessions) == 0 || len(groups[j].Sessions) == 0 {
			return len(groups[i].Sessions) > len(groups[j].Sessions)
		}
		return groups[i].Sessions[0].Timestamp.After(groups[j].Sessions[0].Timestamp)
	})

	return groups, nil
}
