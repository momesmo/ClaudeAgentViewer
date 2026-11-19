package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/user/claudevu/internal/data"
	"github.com/user/claudevu/internal/tui"
)

var version = "dev"

func main() {
	var (
		showVersion bool
		indexCache  bool
		clearCache  bool
		cacheDirArg string

		updatePricing bool
		pricingInfo   bool
		pricingAuto   bool
		pricingSource string
		acceptChanges bool
	)

	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.BoolVar(&indexCache, "index-cache", false, "Persist the search index to an on-disk cache, best-effort secret redaction, see README (also CLAUDEVU_INDEX_CACHE=1)")
	flag.BoolVar(&clearCache, "clear-cache", false, "Delete the on-disk index cache and exit")
	flag.StringVar(&cacheDirArg, "cache-dir", "", "Cache directory (default: user cache dir/claudevu)")
	flag.BoolVar(&updatePricing, "update-pricing", false, "Fetch the current model prices once (network), print what changed, and exit")
	flag.BoolVar(&pricingInfo, "pricing-info", false, "Explain where each price comes from, list any pricing problems, and show how the models in your sessions are priced (no network), then exit")
	flag.BoolVar(&pricingAuto, "pricing-auto", false, "Opt in to refreshing prices automatically (network, at most once a day) when a model has no exact price (also CLAUDEVU_PRICING_AUTO=1)")
	flag.StringVar(&pricingSource, "pricing-source", "", "Pricing page URL (https; default: Anthropic's published pricing page)")
	flag.BoolVar(&acceptChanges, "accept-price-changes", false, "With --update-pricing: apply price changes larger than 3x after you have reviewed them")
	flag.Parse()

	if showVersion {
		fmt.Printf("claudevu %s\n", version)
		os.Exit(0)
	}

	flagSet, pricingAutoSet := false, false
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "index-cache":
			flagSet = true
		case "pricing-auto":
			pricingAutoSet = true
		}
	})
	enableCache := tui.ResolveIndexCache(flagSet, indexCache, os.Getenv("CLAUDEVU_INDEX_CACHE"))

	cacheDir := cacheDirArg
	var pricingDir string
	if cacheDir == "" {
		d, err := data.DefaultCacheDir()
		if err != nil {
			if clearCache {
				fmt.Fprintf(os.Stderr, "Error: cannot determine cache directory: %v\n", err)
				os.Exit(1)
			}
			if enableCache {
				fmt.Fprintf(os.Stderr, "Warning: cannot determine cache directory (%v); index cache disabled\n", err)
				enableCache = false
			}
		} else {
			cacheDir = d
		}
	}
	pricingDir = cacheDir

	if cacheDir != "" {
		if home, herr := os.UserHomeDir(); herr == nil && data.PathInside(cacheDir, filepath.Join(home, ".claude")) {
			if clearCache {
				fmt.Fprintf(os.Stderr, "Error: refusing cache dir %s inside ~/.claude\n", cacheDir)
				os.Exit(1)
			}
			if enableCache {
				fmt.Fprintf(os.Stderr, "Warning: cache dir %s is inside ~/.claude; index cache disabled\n", cacheDir)
				enableCache = false
			}
			pricingDir = "" // never read or write pricing files under ~/.claude either
		}
	}

	// Price book: embedded table <- fetched cache <- user override file. Reading
	// these files needs no opt-in; fetching does (--update-pricing / --pricing-auto).
	configDir := ""
	if d, cerr := os.UserConfigDir(); cerr == nil {
		configDir = filepath.Join(d, "claudevu")
	}
	fetchOpts, ferr := tui.PricingFetchOptions(pricingSource)
	if ferr != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", ferr)
		os.Exit(1)
	}

	if clearCache {
		path := data.IndexPath(cacheDir)
		removed, err := data.ClearIndex(cacheDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		priceFiles, perr := data.ClearPricingCache(pricingDir)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", perr)
			os.Exit(1)
		}
		if removed {
			fmt.Printf("Removed %s\n", path)
		}
		for _, f := range priceFiles {
			fmt.Printf("Removed %s\n", f)
		}
		if !removed && len(priceFiles) == 0 {
			fmt.Printf("No cache found at %s\n", path)
		}
		os.Exit(0)
	}

	if updatePricing {
		os.Exit(runUpdatePricing(pricingDir, configDir, fetchOpts, acceptChanges))
	}

	book, layerInfo := data.LayeredBook(pricingDir, configDir)
	data.SetDefaultBook(book)
	autoPricing := tui.ResolveIndexCache(pricingAutoSet, pricingAuto, os.Getenv("CLAUDEVU_PRICING_AUTO"))

	if pricingInfo {
		policy := data.PricingPolicy{Auto: autoPricing && pricingDir != "", URL: fetchOpts.URL, AllowHosts: fetchOpts.AllowHosts}
		if policy.Auto {
			policy.AutoVia = "CLAUDEVU_PRICING_AUTO"
			if pricingAutoSet {
				policy.AutoVia = "--pricing-auto"
			}
		}
		os.Exit(runPricingInfo(pricingDir, configDir, policy))
	}

	// Determine Claude directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine home directory: %v\n", err)
		os.Exit(1)
	}
	claudeDir := filepath.Join(homeDir, ".claude")

	// Verify it exists
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: %s not found. Is Claude Code installed?\n", claudeDir)
		os.Exit(1)
	}

	model := tui.NewModel(claudeDir, tui.Options{
		IndexCache: enableCache, CacheDir: cacheDir,
		PricingAuto: autoPricing && pricingDir != "", PricingCacheDir: pricingDir, PricingConfigDir: configDir,
		PricingFetch: fetchOpts, PricingInfo: layerInfo,
	})

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseAllMotion())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// If user requested resume, exec claude after TUI exits
	if m, ok := finalModel.(tui.Model); ok && m.ResumeSessionID != "" {
		fmt.Printf("Resuming session %s...\n", m.ResumeSessionID)
		cmd := exec.Command("claude", "--resume", m.ResumeSessionID)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if m.ResumeProjectDir != "" {
			cmd.Dir = m.ResumeProjectDir
		}
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error resuming session: %v\n", err)
			os.Exit(1)
		}
	}
}

// runUpdatePricing is the explicit, one-shot price refresh (--update-pricing).
// It returns the process exit code.
func runUpdatePricing(cacheDir, configDir string, o data.FetchOptions, accept bool) int {
	if cacheDir == "" {
		fmt.Fprintln(os.Stderr, "Error: no usable cache directory to store prices (use --cache-dir outside ~/.claude)")
		return 1
	}
	fmt.Printf("Fetching %s ...\n", o.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := data.RefreshPricingVetted(ctx, cacheDir, configDir, o, time.Now(), accept)
	if err != nil {
		if len(res.Suspicious) > 0 {
			fmt.Fprintln(os.Stderr, "Refusing to apply large price changes (more than 3x in either direction):")
			for _, c := range res.Suspicious {
				fmt.Fprintf(os.Stderr, "  %s\n", c)
			}
			fmt.Fprintln(os.Stderr, "If these are expected, review them and re-run with --accept-price-changes.")
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		return 1
	}
	if len(res.Changes) == 0 {
		fmt.Printf("Prices are up to date (%d models, as of %s).\n", len(res.Book.Models), res.Book.AsOf)
		return 0
	}
	fmt.Printf("Updated prices (%d models, as of %s). %d changed or new:\n", len(res.Book.Models), res.Book.AsOf, len(res.Changes))
	for _, c := range res.Changes {
		fmt.Printf("  %s\n", c)
	}
	return 0
}

// runPricingInfo prints the pricing report (--pricing-info). It never uses the
// network and writes nothing. It returns the process exit code.
func runPricingInfo(cacheDir, configDir string, policy data.PricingPolicy) int {
	report := data.ExplainPricing(cacheDir, configDir, policy, time.Now())
	data.SetDefaultBook(report.Book) // so scanned sessions are costed with the layered prices

	// How the models in your own sessions are priced (skipped if there are none).
	if home, err := os.UserHomeDir(); err == nil {
		claudeDir := filepath.Join(home, ".claude")
		if groups, herr := data.LoadHistory(claudeDir); herr == nil {
			var paths []string
			for _, g := range groups {
				for _, s := range g.Sessions {
					if s.FilePath != "" {
						paths = append(paths, s.FilePath)
					}
				}
			}
			report.Usage = data.ScanModelUsage(paths, report.Book)
		}
	}
	fmt.Print(report.Text())
	if len(report.Problems) > 0 {
		return 2 // distinguish "problems found" for scripts
	}
	return 0
}
