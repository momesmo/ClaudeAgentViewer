package data

import "strings"

// ModelPricing holds per-million-token rates in USD
type ModelPricing struct {
	InputPerM        float64
	OutputPerM       float64
	CacheWritePerM   float64 // cache_creation_input_tokens
	CacheReadPerM    float64 // cache_read_input_tokens
}

// Pricing table based on Anthropic's published rates (as of 2025)
// https://docs.anthropic.com/en/docs/about-claude/models
var modelPricing = map[string]ModelPricing{
	// Claude 4.5/4.6 Opus
	"opus": {
		InputPerM:      15.0,
		OutputPerM:     75.0,
		CacheWritePerM: 18.75,
		CacheReadPerM:  1.50,
	},
	// Claude 4.5/4.6 Sonnet
	"sonnet": {
		InputPerM:      3.0,
		OutputPerM:     15.0,
		CacheWritePerM: 3.75,
		CacheReadPerM:  0.30,
	},
	// Claude 4.5 Haiku
	"haiku": {
		InputPerM:      0.80,
		OutputPerM:     4.0,
		CacheWritePerM: 1.0,
		CacheReadPerM:  0.08,
	},
}

// GetPricing returns the pricing for a model string.
// Matches by substring: "claude-opus-4-6" -> opus, "claude-sonnet-4-6" -> sonnet, etc.
func GetPricing(model string) ModelPricing {
	lower := strings.ToLower(model)
	if strings.Contains(lower, "opus") {
		return modelPricing["opus"]
	}
	if strings.Contains(lower, "haiku") {
		return modelPricing["haiku"]
	}
	// Default to Sonnet (most common)
	return modelPricing["sonnet"]
}

// ComputeCost calculates the cost from token counts and model
func ComputeCost(inputTokens, outputTokens, cacheWriteTokens, cacheReadTokens int, model string) float64 {
	p := GetPricing(model)
	cost := float64(inputTokens) * p.InputPerM / 1_000_000
	cost += float64(outputTokens) * p.OutputPerM / 1_000_000
	cost += float64(cacheWriteTokens) * p.CacheWritePerM / 1_000_000
	cost += float64(cacheReadTokens) * p.CacheReadPerM / 1_000_000
	return cost
}
