package main

import (
	"testing"

	"github.com/user/claudevu/internal/testenv"
)

// TestMain points HOME and the user config/cache dirs at an empty temp dir so no
// test can read the developer's real ~/.claude; see internal/testenv.
func TestMain(m *testing.M) { testenv.Run(m) }
