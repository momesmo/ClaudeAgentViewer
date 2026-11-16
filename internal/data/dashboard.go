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

	// Cost trust: see SessionStats. TotalCost excludes unpriced models.
	CostEstimated  bool     // some cost was priced by model-family fallback
	UnpricedTokens int      // tokens from models with no known price
	UnpricedModels []string // those models
	PricingAsOf    string   // date of the price table used

	ByProject []ProjectStats
	ByModel   []ModelStats
	TopCost   []SessionCostEntry
}

type ProjectStats struct {
	Project   string
	Sessions  int
	Tokens    int
	Cost      float64
	CO2g      float64
	Estimated bool // includes family-estimated costs
	Unpriced  bool // includes tokens from unpriced models (Cost is a lower bound)
}

type ModelStats struct {
	Model     string
	Sessions  int
	Tokens    int
	Cost      float64
	Estimated bool
	Unpriced  bool
}

type SessionCostEntry struct {
	SessionID string
	Project   string
	Display   string
	Cost      float64
	Tokens    int
	Model     string
	Estimated bool
	Unpriced  bool
}

// BuildDashboard aggregates stats from a map of sessionID -> SessionStats
func BuildDashboard(groups []ProjectGroup, statsMap map[string]SessionStats) DashboardData {
	d := DashboardData{}

	projectMap := make(map[string]*ProjectStats)
	modelMap := make(map[string]*ModelStats)
	var allSessions []SessionCostEntry
	var unpricedSet []string

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
			d.CostEstimated = d.CostEstimated || stats.CostEstimated
			d.UnpricedTokens += stats.UnpricedTokens
			unpricedSet = addUnique(unpricedSet, stats.UnpricedModels)
			if d.PricingAsOf == "" || stats.PricingAsOf > d.PricingAsOf {
				d.PricingAsOf = stats.PricingAsOf
			}
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
			ps.Estimated = ps.Estimated || stats.CostEstimated
			ps.Unpriced = ps.Unpriced || stats.UnpricedTokens > 0
			ps.CO2g += stats.CO2Kg * 1000

			// By model: a session that mixed models contributes each model's own share.
			model := "unknown"
			if len(stats.Models) > 0 {
				model = stats.Models[0]
			}
			breakdown := stats.ModelBreakdown
			if len(breakdown) == 0 {
				breakdown = []ModelUsage{{Model: model, Tokens: tokens, CostUSD: stats.EstimatedCostUSD}}
			}
			for _, mu := range breakdown {
				ms, ok := modelMap[mu.Model]
				if !ok {
					ms = &ModelStats{Model: mu.Model}
					modelMap[mu.Model] = ms
				}
				ms.Sessions++
				ms.Tokens += mu.Tokens
				ms.Cost += mu.CostUSD
				ms.Estimated = ms.Estimated || mu.Match == PriceFamily
				ms.Unpriced = ms.Unpriced || mu.Match == PriceNone
			}

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
				Estimated: stats.CostEstimated,
				Unpriced:  stats.UnpricedTokens > 0,
			})
		}
	}

	sort.Strings(unpricedSet)
	d.UnpricedModels = unpricedSet

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

func addUnique(set, add []string) []string {
	for _, a := range add {
		found := false
		for _, s := range set {
			if s == a {
				found = true
				break
			}
		}
		if !found {
			set = append(set, a)
		}
	}
	return set
}
