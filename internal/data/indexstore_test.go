package data

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveLoadIndex_RoundTrip(t *testing.T) {
	dir, cd := t.TempDir(), "/a/.claude"
	now := time.Now()
	in := map[string]IndexEntry{"a": {ModTime: now, Blob: "hello"}, "b": {ModTime: now.Add(time.Hour), Blob: "world"}}
	if err := SaveIndex(dir, cd, in); err != nil {
		t.Fatal(err)
	}
	out := LoadIndex(dir, cd)
	if len(out) != 2 {
		t.Fatalf("len %d", len(out))
	}
	for k, v := range in {
		if out[k].Blob != v.Blob || !out[k].ModTime.Equal(v.ModTime) {
			t.Errorf("mismatch %s", k)
		}
	}
}

func TestLoadIndex_MissingCorruptVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	if m := LoadIndex(dir, "x"); m == nil || len(m) != 0 {
		t.Fatal("missing")
	}
	os.WriteFile(indexPath(dir), []byte("garbage\x00\x01"), 0o600)
	if m := LoadIndex(dir, "x"); m == nil || len(m) != 0 {
		t.Fatal("corrupt")
	}
	f, _ := os.Create(indexPath(dir))
	enc := gob.NewEncoder(f)
	enc.Encode(indexHeader{Version: IndexVersion + 1, ClaudeDir: "x"})
	enc.Encode(map[string]IndexEntry{"a": {Blob: "b"}})
	f.Close()
	if m := LoadIndex(dir, "x"); m == nil || len(m) != 0 {
		t.Fatal("version")
	}
}

func TestLoadIndex_DifferentClaudeDir(t *testing.T) {
	dir := t.TempDir()
	SaveIndex(dir, "/a/.claude", map[string]IndexEntry{"a": {Blob: "x"}})
	if m := LoadIndex(dir, "/b/.claude"); m == nil || len(m) != 0 {
		t.Fatal("expected empty")
	}
}

func TestSaveIndex_RedactsSecrets(t *testing.T) {
	dir := t.TempDir()
	aws := "akiaabcdefghijklmnop"
	jwt := "eyjhbgcioijiuzi1niisinr5cci.eyjzdwiioiixmjmondu2nzg5mcj9.sflkxwrjsmeki2ivbfa3wvrq"
	blob := strings.ToLower("key AKIAABCDEFGHIJKLMNOP end ") + jwt
	SaveIndex(dir, "c", map[string]IndexEntry{"a": {Blob: blob}})
	got := LoadIndex(dir, "c")["a"].Blob
	if strings.Contains(got, aws) || strings.Contains(got, "eyjhbg") || strings.Count(got, "[REDACTED]") != 2 {
		t.Fatalf("not redacted: %q", got)
	}
}

func TestSaveIndex_PermsAndAtomic(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub")
	if err := SaveIndex(dir, "c", map[string]IndexEntry{"a": {Blob: "x"}}); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(indexPath(dir))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file %v", fi.Mode())
	}
	di, _ := os.Stat(dir)
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir %v", di.Mode())
	}
	tmps, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(tmps) != 0 {
		t.Errorf("tmp left: %v", tmps)
	}
}

func TestClearIndex(t *testing.T) {
	dir := t.TempDir()
	SaveIndex(dir, "c", map[string]IndexEntry{"a": {Blob: "x"}})
	if r, err := ClearIndex(dir); !r || err != nil {
		t.Fatal(r, err)
	}
	if r, err := ClearIndex(dir); r || err != nil {
		t.Fatal(r, err)
	}
}
