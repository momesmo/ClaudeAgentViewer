package testfixture

import (
	"math"
	"testing"
)

// The independently implemented formula must agree with the hand-derived literals.
func TestFixtureCostMatchesHandDerivedLiterals(t *testing.T) {
	fx := BuildClaudeDir(t)
	for key, want := range map[string]float64{"tiny": LiteralTinyUSD, "recent": LiteralRecentUSD, "speedgeo": LiteralSpeedGeoUSD} {
		if got := fx.Session(t, key).CostUSD; math.Abs(got-want) > 1e-9 {
			t.Errorf("%s: fixture cost %v, hand-derived %v", key, got, want)
		}
	}
}
