//go:build !windows

package data

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A FIFO or device in place of a cache/override file must never hang startup.
func TestLocalFilesThatAreNotRegularNeverHang(t *testing.T) {
	cache, cfg := t.TempDir(), t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(cache, cacheFileName), 0o600); err != nil {
		t.Skip("mkfifo unavailable:", err)
	}
	if err := syscall.Mkfifo(filepath.Join(cfg, overridesFileName), 0o600); err != nil {
		t.Skip("mkfifo unavailable:", err)
	}
	if err := os.Symlink("/dev/zero", filepath.Join(cache, stateFileName)); err != nil {
		t.Fatal(err)
	}
	done := make(chan LayerInfo, 1)
	go func() {
		_, info := LayeredBook(cache, cfg)
		_ = LoadPricingState(cache)
		done <- info
	}()
	select {
	case info := <-done:
		if info.CacheUsed || len(info.Problems) < 2 {
			t.Errorf("both bad layers should be reported: %+v", info)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("startup layering hung on a non-regular file")
	}
}

var _ = syscall.O_NONBLOCK
