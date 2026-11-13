package data

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxBlobBytes caps the per-session search blob.
const MaxBlobBytes = 40 * 1024

// maxLineBytes: lines larger than this are skipped rather than parsed.
const maxLineBytes = 1 << 20

// IndexEntry is the searchable content of one session.
type IndexEntry struct {
	ModTime time.Time
	Blob    string // lowercase, <= MaxBlobBytes, "" if unreadable
}

// BuildIndexEntry returns prev unchanged (changed=false) if the file mod-time
// equals prev.ModTime. A missing or unreadable file yields IndexEntry{}, false.
func BuildIndexEntry(path string, prev *IndexEntry) (IndexEntry, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return IndexEntry{}, false
	}
	if prev != nil && fi.ModTime().Equal(prev.ModTime) {
		return *prev, false
	}
	f, err := os.Open(path)
	if err != nil {
		return IndexEntry{}, false
	}
	defer f.Close()

	var sb strings.Builder
	add := func(s string) {
		if s == "" || sb.Len() >= MaxBlobBytes {
			return
		}
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(s)
	}

	r := bufio.NewReaderSize(f, 64*1024)
	for sb.Len() < MaxBlobBytes {
		line, tooBig, rerr := readLine(r)
		if !tooBig && len(line) > 0 {
			indexLine(line, add)
		}
		if rerr != nil {
			break
		}
	}

	blob := strings.ToLower(sb.String())
	if len(blob) > MaxBlobBytes {
		blob = blob[:MaxBlobBytes]
		// Drop a trailing partial rune.
		for len(blob) > 0 {
			rn, size := utf8.DecodeLastRuneInString(blob)
			if rn == utf8.RuneError && size <= 1 {
				blob = blob[:len(blob)-1]
				continue
			}
			break
		}
	}
	return IndexEntry{ModTime: fi.ModTime(), Blob: blob}, true
}

// readLine reads one line, discarding content beyond maxLineBytes (tooBig=true).
func readLine(r *bufio.Reader) (line []byte, tooBig bool, err error) {
	for {
		chunk, isPrefix, e := r.ReadLine()
		if e != nil {
			return line, tooBig, e
		}
		if !tooBig {
			if len(line)+len(chunk) > maxLineBytes {
				tooBig = true
				line = nil
			} else {
				line = append(line, chunk...)
			}
		}
		if !isPrefix {
			return line, tooBig, nil
		}
	}
}

func indexLine(line []byte, add func(string)) {
	var raw rawMessage
	if json.Unmarshal(line, &raw) != nil {
		return
	}
	if raw.Type != "user" && raw.Type != "assistant" {
		return
	}
	add(raw.GitBranch)
	if raw.Message == nil {
		return
	}
	var msg rawMessageContent
	if json.Unmarshal(raw.Message, &msg) != nil {
		return
	}
	add(msg.Model)
	var s string
	if json.Unmarshal(msg.Content, &s) == nil {
		add(s)
		return
	}
	var blocks []rawContentBlock
	if json.Unmarshal(msg.Content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			add(b.Text)
		case "tool_use":
			add(b.Name)
		}
	}
}
