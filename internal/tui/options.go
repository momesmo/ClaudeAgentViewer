package tui

import "strings"

// Options configures optional TUI behavior.
type Options struct {
	IndexCache bool
	CacheDir   string // "" means data.DefaultCacheDir()
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
