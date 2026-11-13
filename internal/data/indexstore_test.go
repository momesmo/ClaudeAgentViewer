package data

import (
	"encoding/gob"
	"fmt"
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

func TestSaveIndex_DoesNotChmodExistingDir(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0o755)
	SaveIndex(dir, "c", map[string]IndexEntry{"a": {Blob: "x"}})
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o755 {
		t.Errorf("existing dir mode changed: %v", fi.Mode())
	}
	nd := filepath.Join(t.TempDir(), "new")
	SaveIndex(nd, "c", map[string]IndexEntry{"a": {Blob: "x"}})
	if fi, _ := os.Stat(nd); fi.Mode().Perm() != 0o700 {
		t.Errorf("new dir mode: %v", fi.Mode())
	}
}

func TestTmpSweep(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "search-index-123.tmp")
	os.WriteFile(stale, []byte("x"), 0o600)
	if r, err := ClearIndex(dir); !r || err != nil {
		t.Fatal(r, err)
	}
	os.WriteFile(stale, []byte("x"), 0o600)
	SaveIndex(dir, "c", map[string]IndexEntry{"a": {Blob: "x"}})
	if _, err := os.Stat(stale); err == nil {
		t.Error("stale tmp not swept by SaveIndex")
	}
}

func TestPathInside(t *testing.T) {
	base := t.TempDir()
	cd := filepath.Join(base, ".claude")
	cases := []struct {
		p    string
		want bool
	}{
		{cd, true},
		{filepath.Join(cd, "cache", "x"), true},
		{filepath.Join(base, ".claude-foo"), false},
		{filepath.Join(base, "other"), false},
		{base, false},
	}
	for _, c := range cases {
		if got := PathInside(c.p, cd); got != c.want {
			t.Errorf("%s: got %v", c.p, got)
		}
	}
}

// Text without secret-like hints must skip the regexes almost entirely.
func TestRedactSecretsFold_PlainTextFast(t *testing.T) {
	blob := strings.Repeat("lorem ipsum dolor sit amet ", 1600)[:40*1024]
	start := time.Now()
	for i := 0; i < 1000; i++ {
		RedactSecretsFold(blob)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("plain-text redaction too slow: %v", d)
	}
}

// Dense code-like text trips the hint prefilters ("key", "token", "://", "sk-")
// so the contextual regexes really run. The first save pays that cost; later
// saves of unchanged sessions must reuse the cached redaction.
func TestSaveIndex_RedactionCachedAcrossSaves(t *testing.T) {
	unit := "func parseToken(key string) { u := \"https://example.com/a\"; risk-free task-ant lorem ipsum token: none } "
	mod := time.Now()
	idx := map[string]IndexEntry{}
	for i := 0; i < 20; i++ {
		// distinct keys + tweak so entries differ; same modTime reused on 2nd save
		idx[fmt.Sprint("dense", i)] = IndexEntry{ModTime: mod, Blob: strings.Repeat(unit, 500)[:40*1024-i]}
	}
	dir := t.TempDir()
	t0 := time.Now()
	if err := SaveIndex(dir, "c", idx); err != nil {
		t.Fatal(err)
	}
	first := time.Since(t0)
	t1 := time.Now()
	if err := SaveIndex(dir, "c", idx); err != nil {
		t.Fatal(err)
	}
	second := time.Since(t1)
	t.Logf("20 dense sessions: first save %v, cached save %v", first, second)
	if second*3 > first {
		t.Errorf("second save (%v) should be much faster than first (%v)", second, first)
	}
	// A changed session (new mod-time) is re-redacted.
	e := idx["dense0"]
	e.ModTime = mod.Add(time.Second)
	e.Blob = "password=hunter2hunter2 " + e.Blob
	idx["dense0"] = e
	if err := SaveIndex(dir, "c", idx); err != nil {
		t.Fatal(err)
	}
	if got := LoadIndex(dir, "c")["dense0"].Blob; strings.Contains(got, "hunter2hunter2") {
		t.Errorf("changed session not re-redacted: %.60q", got)
	}
}
