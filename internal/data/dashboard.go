package data

import "sort"

// DashboardData holds aggregated stats across all sessions
type DashboardData struct {
	TotalSessions int
	TotalTokens   int
	TotalCost     float64
	TotalCO2g     float64
	TotalTreeDays float64
	TotalEnergyKWh float64

	ByProject []ProjectStats
	ByModel   []ModelStats
	TopCost   []SessionCostEntry
}

type ProjectStats struct {
	Project  string
	Sessions int
	Tokens   int
	Cost     float64
	CO2g     float64
}

type ModelStats struct {
	Model    string
	Sessions int
	Tokens   int
	Cost     float64
}

type SessionCostEntry struct {
	SessionID string
	Project   string
	Display   string
	Cost      float64
	Tokens    int
	Model     string
}

// BuildDashboard aggregates stats from a map of sessionID -> SessionStats
func BuildDashboard(groups []ProjectGroup, statsMap map[string]SessionStats) DashboardData {
	d := DashboardData{}

	projectMap := make(map[string]*ProjectStats)
	modelMap := make(map[string]*ModelStats)
	var allSessions []SessionCostEntry

	for _, g := range groups {
		for _, s := range g.Sessions {
			stats, ok := statsMap[s.SessionID]
			if !ok {
				continue
			}

			d.TotalSessions++
			tokens := stats.TotalInputTokens + stats.TotalOutputTokens
			d.TotalTokens += tokens
			d.TotalCost += stats.EstimatedCostUSD
			d.TotalCO2g += stats.CO2Kg * 1000
			d.TotalTreeDays += stats.TreeDays
			d.TotalEnergyKWh += stats.EnergyKWh

			// By project
			ps, ok := projectMap[g.Project]
			if !ok {
				ps = &ProjectStats{Project: g.Project}
				projectMap[g.Project] = ps
			}
			ps.Sessions++
			ps.Tokens += tokens
			ps.Cost += stats.EstimatedCostUSD
			ps.CO2g += stats.CO2Kg * 1000

			// By model
			model := "unknown"
			if len(stats.Models) > 0 {
				model = stats.Models[0]
			}
			ms, ok := modelMap[model]
			if !ok {
				ms = &ModelStats{Model: model}
				modelMap[model] = ms
			}
			ms.Sessions++
			ms.Tokens += tokens
			ms.Cost += stats.EstimatedCostUSD

			// For top cost
			display := s.Display
			if len(display) > 40 {
				display = display[:37] + "..."
			}
			allSessions = append(allSessions, SessionCostEntry{
				SessionID: s.SessionID,
				Project:   g.Project,
				Display:   display,
				Cost:      stats.EstimatedCostUSD,
				Tokens:    tokens,
				Model:     model,
			})
		}
	}

	// Sort by project cost descending
	for _, ps := range projectMap {
		d.ByProject = append(d.ByProject, *ps)
	}
	sort.Slice(d.ByProject, func(i, j int) bool {
		return d.ByProject[i].Cost > d.ByProject[j].Cost
	})

	// Sort by model cost descending
	for _, ms := range modelMap {
		d.ByModel = append(d.ByModel, *ms)
	}
	sort.Slice(d.ByModel, func(i, j int) bool {
		return d.ByModel[i].Cost > d.ByModel[j].Cost
	})

	// Top 5 most expensive sessions
	sort.Slice(allSessions, func(i, j int) bool {
		return allSessions[i].Cost > allSessions[j].Cost
	})
	if len(allSessions) > 5 {
		d.TopCost = allSessions[:5]
	} else {
		d.TopCost = allSessions
	}

	return d
}
