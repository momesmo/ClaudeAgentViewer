package data

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxBlobBytes caps the per-session search blob (lowercase bytes).
	MaxBlobBytes = 256 * 1024

	// HeadBytes is how much text is read from the start of a session before
	// the reader jumps to the end of the file. The blob is that head plus the
	// most recent text that still fits under MaxBlobBytes, so long sessions
	// keep both their beginning and their end; only the middle is dropped.
	HeadBytes = 64 * 1024

	// maxLineBytes bounds how much of one transcript line is held in memory.
	// A longer line that is a user/assistant message is salvaged (its first
	// maxLineBytes are scanned for the message text); other long lines, such
	// as tool results, are ignored.
	maxLineBytes = 1 << 20

	// maxItemBytes bounds a single prompt or reply so that one huge paste
	// cannot consume the whole budget: the head keeps its start, the tail its end.
	maxItemBytes = HeadBytes
)

// tailChunkBytes is the read size when walking the file backwards for the
// tail. A variable so tests can exercise chunk boundaries with a small value.
var tailChunkBytes = 1 << 20

// IndexEntry is the searchable content of one session.
type IndexEntry struct {
	ModTime time.Time
	Blob    string // lowercase, <= MaxBlobBytes, "" if unreadable
}

// lineGroup is the searchable content of one transcript line.
type lineGroup struct {
	branch, model string
	texts         []string // lowercase prompts, replies and tool names

	// Branch and model are written only when they differ from the previous
	// message's, so a long session does not repeat them on every line.
	emitBranch, emitModel bool
}

func (g lineGroup) metaSize() int {
	n := 0
	if g.emitBranch {
		n += len(g.branch) + 1
	}
	if g.emitModel {
		n += len(g.model) + 1
	}
	return n
}

func (g lineGroup) textSize() int {
	n := 0
	for _, t := range g.texts {
		n += len(t) + 1
	}
	return n
}

// BuildIndexEntry returns prev unchanged (changed=false) if the file mod-time
// equals prev.ModTime. A missing or unreadable file yields IndexEntry{}, false.
//
// The blob is built from the first HeadBytes of searchable text plus the most
// recent text that fits under MaxBlobBytes. Branch and model names are written
// only when they change from one message to the next.
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

	// Head: read forward until HeadBytes of text have been collected.
	r := bufio.NewReaderSize(f, 64*1024)
	var head []lineGroup
	headCount := 0
	lastBranch, lastModel := "", ""
	var offset int64
	eof := false
	for headCount < HeadBytes {
		line, trunc, n, rerr := readLineN(r)
		offset += n
		if g, ok := extractLine(line, trunc); ok {
			for i := range g.texts {
				g.texts[i] = cutFront(g.texts[i], maxItemBytes)
			}
			if g.branch != "" && g.branch != lastBranch {
				g.emitBranch, lastBranch = true, g.branch
			}
			if g.model != "" && g.model != lastModel {
				g.emitModel, lastModel = true, g.model
			}
			headCount += g.metaSize()
			head = append(head, g)
			headCount += g.textSize()
		}
		if rerr != nil {
			eof = true
			break
		}
	}
	headStr := cutFront(joinGroups(head), MaxBlobBytes)

	// Tail: walk the rest of the file backwards, newest first.
	tailStr := ""
	if budget := MaxBlobBytes - len(headStr) - 1; !eof && budget > 0 && offset < fi.Size() {
		tail := collectTail(f, offset, fi.Size(), budget)
		for i, j := 0, len(tail)-1; i < j; i, j = i+1, j-1 { // oldest first
			tail[i], tail[j] = tail[j], tail[i]
		}
		tailStr = cutBack(joinGroups(tail), budget)
	}

	blob := headStr
	if tailStr != "" {
		if blob != "" {
			blob += "\n"
		}
		blob += tailStr
	}
	return IndexEntry{ModTime: fi.ModTime(), Blob: blob}, true
}

// collectTail reads [lo, hi) backwards in chunks and returns the searchable
// line groups, newest first, until about budget bytes of text are collected.
func collectTail(f *os.File, lo, hi int64, budget int) []lineGroup {
	var groups []lineGroup
	counted := 0

	// add handles one complete (or truncated) line; it reports whether to stop.
	add := func(line []byte, trunc bool) bool {
		line = trimEOL(line)
		if len(line) > maxLineBytes {
			line, trunc = line[:maxLineBytes], true
		}
		g, ok := extractLine(line, trunc)
		if !ok {
			return false
		}
		for i := range g.texts {
			if trunc { // a salvaged line only has its front; keep that
				g.texts[i] = cutFront(g.texts[i], maxItemBytes)
			} else {
				g.texts[i] = cutBack(g.texts[i], maxItemBytes)
			}
		}
		// The newer neighbour's branch/model are only needed if they differ from ours.
		if n := len(groups); n > 0 {
			newer := &groups[n-1]
			before := newer.metaSize()
			newer.emitBranch = newer.branch != "" && newer.branch != g.branch
			newer.emitModel = newer.model != "" && newer.model != g.model
			counted += newer.metaSize() - before
		}
		groups = append(groups, g)
		counted += g.textSize()
		return counted >= budget
	}

	pos := hi
	var carry []byte // bytes of a line whose start lies in an earlier chunk
	carryTrunc := false
	stop := false
	for pos > lo && !stop {
		readLen := int64(tailChunkBytes)
		if pos-lo < readLen {
			readLen = pos - lo
		}
		pos -= readLen
		chunk := make([]byte, readLen)
		if _, err := f.ReadAt(chunk, pos); err != nil && err != io.EOF {
			break
		}
		data := append(chunk, carry...)
		end := len(data)
		first := true
		for {
			i := bytes.LastIndexByte(data[:end], '\n')
			if i < 0 {
				break
			}
			trunc := false
			if first {
				trunc, carryTrunc = carryTrunc, false
			}
			first = false
			if add(data[i+1:end], trunc) {
				stop = true
				break
			}
			end = i
		}
		if stop {
			break
		}
		rest := data[:end]
		if pos == lo { // rest starts at a line boundary, so it is a complete line
			trunc := false
			if first {
				trunc = carryTrunc
			}
			add(rest, trunc)
			carry = nil
			break
		}
		if len(rest) > maxLineBytes { // keep the front of the line; drop its end
			rest = rest[:maxLineBytes]
			carryTrunc = true
		}
		carry = append([]byte(nil), rest...)
	}
	if n := len(groups); n > 0 { // the oldest collected message always states its metadata
		groups[n-1].emitBranch = groups[n-1].branch != ""
		groups[n-1].emitModel = groups[n-1].model != ""
	}
	return groups
}

// joinGroups renders groups (oldest first) as newline-separated text.
func joinGroups(groups []lineGroup) string {
	var parts []string
	for _, g := range groups {
		if g.emitBranch {
			parts = append(parts, g.branch)
		}
		if g.emitModel {
			parts = append(parts, g.model)
		}
		parts = append(parts, g.texts...)
	}
	return strings.Join(parts, "\n")
}

// readLineN reads one line, keeping at most maxLineBytes of it (truncated=true
// if more was discarded). n is the exact number of bytes consumed, including
// the line terminator, so callers can track the file offset.
func readLineN(r *bufio.Reader) (line []byte, truncated bool, n int64, err error) {
	for {
		chunk, e := r.ReadSlice('\n')
		n += int64(len(chunk))
		if !truncated {
			take := chunk
			if len(line)+len(take) > maxLineBytes {
				take = take[:maxLineBytes-len(line)]
				truncated = true
			}
			line = append(line, take...)
		}
		if e == bufio.ErrBufferFull {
			continue
		}
		return trimEOL(line), truncated, n, e
	}
}

func trimEOL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// extractLine returns the searchable content of one transcript line, lowercased.
// ok is false for lines that are not user/assistant messages with text. A
// truncated line (cut at maxLineBytes) cannot be parsed as JSON, so its message
// text is salvaged from the raw bytes instead.
func extractLine(line []byte, truncated bool) (lineGroup, bool) {
	if len(line) == 0 {
		return lineGroup{}, false
	}
	if truncated {
		if s, ok := salvageText(line); ok {
			return lineGroup{texts: []string{strings.ToLower(s)}}, true
		}
		return lineGroup{}, false
	}
	var raw rawMessage
	if json.Unmarshal(line, &raw) != nil {
		return lineGroup{}, false
	}
	if raw.Type != "user" && raw.Type != "assistant" || raw.Message == nil {
		return lineGroup{}, false
	}
	var msg rawMessageContent
	if json.Unmarshal(raw.Message, &msg) != nil {
		return lineGroup{}, false
	}
	g := lineGroup{branch: strings.ToLower(raw.GitBranch), model: strings.ToLower(msg.Model)}
	addText := func(s string) {
		if s != "" {
			g.texts = append(g.texts, strings.ToLower(s))
		}
	}
	var s string
	if json.Unmarshal(msg.Content, &s) == nil {
		addText(s)
	} else {
		var blocks []rawContentBlock
		if json.Unmarshal(msg.Content, &blocks) == nil {
			for _, b := range blocks {
				switch b.Type {
				case "text":
					addText(b.Text)
				case "tool_use":
					addText(b.Name)
				}
			}
		}
	}
	return g, len(g.texts) > 0
}

// salvageText extracts the message text from a user/assistant line that was
// cut at maxLineBytes (so it is not valid JSON): the first string "content",
// or the first text block of an array content. Tool results are ignored.
func salvageText(line []byte) (string, bool) {
	prefix := line
	if len(prefix) > 4096 {
		prefix = prefix[:4096]
	}
	mi := bytes.Index(prefix, []byte(`"message"`))
	if mi < 0 {
		return "", false
	}
	pre := prefix[:mi]
	if !bytes.Contains(pre, []byte(`"type":"user"`)) && !bytes.Contains(pre, []byte(`"type":"assistant"`)) {
		return "", false
	}
	msg := line[mi:]
	ci := bytes.Index(msg, []byte(`"content":`))
	if ci < 0 {
		return "", false
	}
	rest := bytes.TrimLeft(msg[ci+len(`"content":`):], " ")
	if len(rest) == 0 {
		return "", false
	}
	switch rest[0] {
	case '"':
		return decodeJSONStringPrefix(rest[1:])
	case '[':
		probe := rest
		if len(probe) > 300 {
			probe = probe[:300]
		}
		if bytes.Contains(probe, []byte(`"tool_result"`)) {
			return "", false
		}
		ti := bytes.Index(rest, []byte(`"text":"`))
		if ti < 0 {
			return "", false
		}
		return decodeJSONStringPrefix(rest[ti+len(`"text":"`):])
	}
	return "", false
}

// decodeJSONStringPrefix decodes a JSON string body that may be cut off before
// its closing quote, dropping an incomplete trailing escape sequence.
func decodeJSONStringPrefix(b []byte) (string, bool) {
	end := len(b)
	closed := false
	for i := 0; i < len(b); i++ {
		if b[i] == '\\' {
			i++
			continue
		}
		if b[i] == '"' {
			end, closed = i, true
			break
		}
	}
	body := b[:end]
	if !closed {
		if i := bytes.LastIndexByte(body, '\\'); i >= 0 {
			slashes := 0
			for j := i; j >= 0 && body[j] == '\\'; j-- {
				slashes++
			}
			if slashes%2 == 1 && (len(body)-i == 1 || (body[i+1] == 'u' && len(body)-i < 6)) {
				body = body[:i]
			}
		}
	}
	var s string
	if json.Unmarshal(append(append([]byte{'"'}, body...), '"'), &s) != nil || s == "" {
		return "", false
	}
	return s, true
}

// cutFront keeps the first n bytes of s, without splitting a rune.
func cutFront(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if r == utf8.RuneError && size <= 1 {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}

// cutBack keeps the last n bytes of s, without splitting a rune.
func cutBack(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return s
}
