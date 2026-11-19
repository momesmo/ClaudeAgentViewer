// Package testenv isolates a test binary from the developer's real home.
//
// Every package's TestMain calls Run, so a test that accidentally resolves
// ~/.claude, the user cache dir or the user config dir lands in an empty temp
// directory and fails loudly instead of silently reading real data.
package testenv

import (
	"os"
	"runtime"
	"testing"
)

// optInVar enables the real-data checks (see RealHome).
const optInVar = "CLAUDEVU_TEST_REAL_CLAUDE"

var origHome, origUserProfile string

var isolatedVars = []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "AppData", "LocalAppData"}

// Run points the home/config/cache environment at a fresh temp dir, runs the
// tests, cleans up and exits.
func Run(m *testing.M) {
	origHome, origUserProfile = os.Getenv("HOME"), os.Getenv("USERPROFILE")
	tmp, err := os.MkdirTemp("", "claudevu-test-home-")
	if err != nil {
		panic(err)
	}
	for _, v := range isolatedVars {
		os.Setenv(v, tmp)
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

// RealHome returns the developer's real home directory (HOME, or USERPROFILE
// on Windows), captured before isolation, and false unless the opt-in
// environment variable CLAUDEVU_TEST_REAL_CLAUDE=1 is set. The opt-in
// real-data test in perf_test.go is the only place allowed to touch a real
// path; it uses this value directly and never re-reads the environment.
func RealHome() (string, bool) {
	if os.Getenv(optInVar) != "1" {
		return "", false
	}
	h := origHome
	if runtime.GOOS == "windows" && origUserProfile != "" {
		h = origUserProfile
	}
	return h, h != ""
}
