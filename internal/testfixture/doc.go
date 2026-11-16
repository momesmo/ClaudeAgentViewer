// Package testfixture builds a deterministic, self-contained fake ~/.claude
// directory under t.TempDir() for tests, together with the values the app is
// expected to report for it.
//
// The on-disk shape mirrors Claude Code: history.jsonl, one transcript per
// session under projects/<dir>/<id>.jsonl with ONE LINE PER CONTENT BLOCK
// (every line of a message repeats its message.id and usage), subagent
// transcripts plus .meta.json under projects/<dir>/<id>/subagents/, and
// agent-name / custom-title metadata lines.
//
// Expected values are computed while the fixture is written (summing the usage
// that was written, pricing it with data.DefaultBook), never by running the
// scanners under test.
package testfixture
