package data

import (
	"bufio"
	"io"
)

// maxStatsLineBytes bounds a single transcript line when computing statistics.
// A message can carry a very large tool result or file write, so this is far
// above bufio.Scanner's usual limits; a line over it is skipped, never fatal.
const maxStatsLineBytes = 32 << 20

// lineScanner reads JSONL lines like bufio.Scanner, but a line longer than the
// limit is skipped instead of aborting the scan. (bufio.Scanner stops at the
// first oversized line with "token too long", which silently dropped every
// later message from a session's totals.)
type lineScanner struct {
	r     *bufio.Reader
	limit int
	line  []byte
	err   error
	done  bool
}

func newLineScanner(r io.Reader, limit int) *lineScanner {
	return &lineScanner{r: bufio.NewReaderSize(r, 256*1024), limit: limit}
}

// Scan advances to the next non-empty line that fits the limit.
func (s *lineScanner) Scan() bool {
	for !s.done {
		line, truncated, _, err := readLineLimit(s.r, s.limit)
		if err != nil {
			s.done = true
			if err != io.EOF {
				s.err = err
			}
		}
		if truncated || len(line) == 0 {
			continue
		}
		s.line = line
		return true
	}
	return false
}

// Bytes returns the current line (valid until the next call to Scan).
func (s *lineScanner) Bytes() []byte { return s.line }

// Err returns the first non-EOF read error.
func (s *lineScanner) Err() error { return s.err }
