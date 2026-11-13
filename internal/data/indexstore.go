package data

import (
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
)

// IndexVersion must be bumped whenever blob contents or MaxBlobBytes change.
const IndexVersion = 1

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
	_ = os.Chmod(cacheDir, 0o700)
	red := make(map[string]IndexEntry, len(idx))
	for k, e := range idx {
		e.Blob = RedactSecretsFold(e.Blob)
		red[k] = e
	}
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
	err := os.Remove(indexPath(cacheDir))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
