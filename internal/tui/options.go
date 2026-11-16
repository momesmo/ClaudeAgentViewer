package tui

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/user/claudevu/internal/data"
)

// Options configures optional TUI behavior.
type Options struct {
	IndexCache bool
	CacheDir   string // "" means data.DefaultCacheDir()

	// Pricing. Prices come from the layered book (embedded < fetched cache <
	// override file), loaded by main before the TUI starts. The network is used
	// only when PricingAuto is set (opt-in) and a model has no exact price.
	PricingAuto      bool
	PricingCacheDir  string // where the fetched table and attempt state live; "" disables fetching
	PricingConfigDir string // where pricing-overrides.json lives
	PricingFetch     data.FetchOptions
	PricingInfo      data.LayerInfo // what the startup layering found (shown in the status bar)
}

// ResolveIndexCache decides whether the disk cache is enabled. An explicitly
// set flag wins over the environment value.
func ResolveIndexCache(flagSet, flagVal bool, env string) bool {
	if flagSet {
		return flagVal
	}
	e := strings.ToLower(strings.TrimSpace(env))
	return e == "1" || e == "true"
}

// PricingFetchOptions builds the fetch options for a pricing source. An empty
// source means the official page with its built-in host allowlist. A custom
// source must be https and is trusted only for its own host, because the user
// chose it explicitly.
func PricingFetchOptions(source string) (data.FetchOptions, error) {
	if source == "" {
		return data.FetchOptions{URL: data.DefaultPricingURL}, nil
	}
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return data.FetchOptions{}, fmt.Errorf("pricing source must be an https URL with a host and no credentials: %q", source)
	}
	allow := u.Hostname()
	if u.Port() != "" {
		allow = u.Host // an explicit port in the URL is trusted as given
	}
	return data.FetchOptions{URL: source, AllowHosts: []string{allow}}, nil
}
