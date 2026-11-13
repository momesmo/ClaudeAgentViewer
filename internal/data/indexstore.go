package data

import (
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// IndexVersion must be bumped whenever blob contents or MaxBlobBytes change.
const IndexVersion = 3

type indexHeader struct {
	Version   int
	ClaudeDir string
}

// DefaultCacheDir returns os.UserCacheDir()/claudevu.
func DefaultCacheDir() (string, error) {
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "claudevu"), nil
}

func indexPath(cacheDir string) string {
	return filepath.Join(cacheDir, fmt.Sprintf("search-index.v%d.gob", IndexVersion))
}

// IndexPath returns the cache file path within cacheDir.
func IndexPath(cacheDir string) string { return indexPath(cacheDir) }

// LoadIndex returns an empty map on any problem.
func LoadIndex(cacheDir, claudeDir string) map[string]IndexEntry {
	out := make(map[string]IndexEntry)
	f, err := os.Open(indexPath(cacheDir))
	if err != nil {
		return out
	}
	defer f.Close()
	dec := gob.NewDecoder(f)
	var h indexHeader
	if err := dec.Decode(&h); err != nil || h.Version != IndexVersion || h.ClaudeDir != claudeDir {
		return out
	}
	var m map[string]IndexEntry
	if err := dec.Decode(&m); err != nil || m == nil {
		return out
	}
	return m
}

// SaveIndex atomically writes the index with secrets redacted.
func SaveIndex(cacheDir, claudeDir string, idx map[string]IndexEntry) error {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return err
	}
	sweepTmp(cacheDir)
	red := make(map[string]IndexEntry, len(idx))
	for k, e := range idx {
		e.Blob = redactBlobCached(k, e)
		red[k] = e
	}
	pruneRedactCache(idx)
	tmp, err := os.CreateTemp(cacheDir, "search-index-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { tmp.Close(); os.Remove(tmpName) }
	if err := os.Chmod(tmpName, 0o600); err != nil {
		cleanup()
		return err
	}
	enc := gob.NewEncoder(tmp)
	if err := enc.Encode(indexHeader{Version: IndexVersion, ClaudeDir: claudeDir}); err != nil {
		cleanup()
		return err
	}
	if err := enc.Encode(red); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, indexPath(cacheDir)); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// ClearIndex deletes the cache file; a missing file is not an error.
func ClearIndex(cacheDir string) (bool, error) {
	removed := sweepTmp(cacheDir)
	err := os.Remove(indexPath(cacheDir))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return removed, nil
	}
	return removed, err
}

// sweepTmp removes stale search-index-*.tmp files left by interrupted saves.
func sweepTmp(cacheDir string) bool {
	matches, _ := filepath.Glob(filepath.Join(cacheDir, "search-index-*.tmp"))
	any := false
	for _, f := range matches {
		if os.Remove(f) == nil {
			any = true
		}
	}
	return any
}

// PathInside reports whether path is dir itself or lies beneath it
// (symlinks resolved where possible).
func PathInside(path, dir string) bool {
	clean := func(p string) string {
		a, err := filepath.Abs(p)
		if err != nil {
			a = filepath.Clean(p)
		}
		// resolve the longest existing prefix
		cur, rest := a, ""
		for {
			if r, err := filepath.EvalSymlinks(cur); err == nil {
				return filepath.Join(r, rest)
			}
			parent := filepath.Dir(cur)
			if parent == cur {
				return a
			}
			rest = filepath.Join(filepath.Base(cur), rest)
			cur = parent
		}
	}
	rel, err := filepath.Rel(clean(dir), clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// redactCache remembers the redacted form of each session's blob so a save
// only re-runs the (comparatively slow) regexes for sessions that changed
// since the previous save. An entry is current while its mod-time and blob
// length are unchanged; blobs are rebuilt whenever the session file changes.
type redactCacheEntry struct {
	modTime time.Time
	blobLen int
	out     string
}

var (
	redactMu    sync.Mutex
	redactCache = map[string]redactCacheEntry{}
)

func redactBlobCached(key string, e IndexEntry) string {
	redactMu.Lock()
	c, ok := redactCache[key]
	redactMu.Unlock()
	if ok && c.modTime.Equal(e.ModTime) && c.blobLen == len(e.Blob) {
		return c.out
	}
	out := RedactSecretsFold(e.Blob)
	redactMu.Lock()
	redactCache[key] = redactCacheEntry{e.ModTime, len(e.Blob), out}
	redactMu.Unlock()
	return out
}

// pruneRedactCache drops cache entries for sessions no longer in the index.
func pruneRedactCache(idx map[string]IndexEntry) {
	redactMu.Lock()
	defer redactMu.Unlock()
	for k := range redactCache {
		if _, ok := idx[k]; !ok {
			delete(redactCache, k)
		}
	}
}
