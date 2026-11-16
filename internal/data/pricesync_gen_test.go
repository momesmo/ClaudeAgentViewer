package data

import (
	"encoding/json"
	"os"
	"testing"
)

// TestRegeneratePricingTable rewrites pricing_table.json from the fixture
// testdata/pricing_page_excerpt.md. It only writes when UPDATE_PRICING_TABLE is
// set; PRICING_AS_OF (YYYY-MM-DD) is required then. See docs/pricing.md.
func TestRegeneratePricingTable(t *testing.T) {
	if os.Getenv("UPDATE_PRICING_TABLE") != "1" {
		t.Skip("set UPDATE_PRICING_TABLE=1 to regenerate pricing_table.json")
	}
	asOf := os.Getenv("PRICING_AS_OF")
	if asOf == "" {
		t.Fatal("set PRICING_AS_OF=YYYY-MM-DD")
	}
	md, err := os.ReadFile("testdata/pricing_page_excerpt.md")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParsePricingMarkdown(md, DefaultPricingURL, asOf)
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.MarshalIndent(b, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("pricing_table.json", append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
