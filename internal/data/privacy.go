package data

import (
	"regexp"
	"strings"
)

// redactRule is one secret detector. pat is written for original-case text; a
// leading (?i) marks a case-insensitive rule. hints are lowercase substrings,
// at least one of which must appear (case-insensitively) for the rule to be
// able to match; they let us skip most regexes on most text.
//
// The folded variant of each rule (used on already-lowercased search blobs)
// is derived by lowercasing the pattern and dropping (?i), so patterns must
// not use uppercase regex escapes such as \S, \D or \W (use [^\s] etc.);
// TestRedactRulesFoldSafe enforces this.
type redactRule struct {
	pat   string
	repl  string
	hints []string
}

const redacted = "[REDACTED]"

var redactRules = []redactRule{
	// Provider API keys and tokens (prefix-anchored; \b stops matches inside
	// longer words, e.g. "task-ant-farm" must not look like "sk-ant-").
	{`\bsk-proj-[A-Za-z0-9_-]{20,}`, redacted, []string{"sk-"}},
	{`\bsk-ant-[A-Za-z0-9_-]{20,}`, redacted, []string{"sk-"}},
	{`\bsk-[A-Za-z0-9]{32,}`, redacted, []string{"sk-"}}, // legacy OpenAI
	{`\bgh[pousr]_[A-Za-z0-9]{36,}`, redacted, []string{"gh"}},
	{`\bgithub_pat_[A-Za-z0-9_]{20,}`, redacted, []string{"github_pat_"}},
	{`\b(?:AKIA|ASIA)[0-9A-Z]{16}`, redacted, []string{"akia", "asia"}},
	{`\bAIza[0-9A-Za-z_-]{35,}`, redacted, []string{"aiza"}},
	{`\b[sr]k_(?:live|test)_[A-Za-z0-9]{16,}`, redacted, []string{"k_live_", "k_test_"}},
	{`\bSG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}`, redacted, []string{"sg."}},
	{`\bhf_[A-Za-z0-9]{30,}`, redacted, []string{"hf_"}},
	{`\bnpm_[A-Za-z0-9]{30,}`, redacted, []string{"npm_"}},
	{`\bxox[abprs]-[0-9A-Za-z-]{10,}`, redacted, []string{"xox"}},
	{`\bxapp-[0-9A-Za-z-]{10,}`, redacted, []string{"xapp-"}},
	{`https://hooks\.slack\.com/services/[A-Za-z0-9/]+`, redacted, []string{"hooks.slack.com"}},
	{`\beyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}`, redacted, []string{"eyj"}},

	// PEM private keys: full block, then a fallback for truncated blocks
	// (e.g. cut at the blob size cap) that redacts the header and first line.
	{`-----BEGIN [A-Z ]*PRIVATE KEY-----[^-]*-----END [A-Z ]*PRIVATE KEY-----`, redacted, []string{"private key"}},
	{`-----BEGIN [A-Z ]*PRIVATE KEY-----\s*[A-Za-z0-9+/=]{16,}`, redacted, []string{"private key"}},

	// Contextual secrets: keep the label, redact the value.
	{`(?i)\b(bearer)\s+[A-Za-z0-9._~+/=-]{16,}`, "${1} " + redacted, []string{"bearer"}},
	{`(?i)\b([a-z][a-z0-9+.-]*://[^\s:/@]+):[^\s/@]+@`, "${1}:" + redacted + "@", []string{"://"}},
	{`(?i)((?:password|passwd|pwd|secret|api[_-]?key|access[_-]?key|auth[_-]?token|token)["']?\s*[:=]\s*["']?)[^\s"',;]{8,}`,
		"${1}" + redacted, []string{"password", "passwd", "pwd", "secret", "key", "token"}},
}

type compiledRule struct {
	re    *regexp.Regexp
	repl  string
	hints []string
}

var (
	secretRules     []compiledRule // for original-case text
	secretRulesFold []compiledRule // for already-lowercased text
)

func init() {
	for _, r := range redactRules {
		secretRules = append(secretRules, compiledRule{regexp.MustCompile(r.pat), r.repl, r.hints})
		secretRulesFold = append(secretRulesFold, compiledRule{regexp.MustCompile(foldPattern(r.pat)), r.repl, r.hints})
	}
}

// foldPattern lowercases a pattern's literals and character classes and drops
// (?i), so it matches already-lowercased text while keeping Go's literal-prefix
// fast path (which (?i) would disable).
func foldPattern(p string) string { return strings.ToLower(strings.TrimPrefix(p, "(?i)")) }

func applyRules(text string, rules []compiledRule) string {
	lower := strings.ToLower(text)
	for _, r := range rules {
		found := false
		for _, h := range r.hints {
			if strings.Contains(lower, h) {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		if text2 := r.re.ReplaceAllString(text, r.repl); text2 != text {
			text = text2
			lower = strings.ToLower(text)
		}
	}
	return text
}

// RedactSecrets replaces detected secrets with [REDACTED]. Detection is
// pattern-based and best-effort: unknown token formats and short or unlabeled
// passwords are not caught.
func RedactSecrets(text string) string { return applyRules(text, secretRules) }

// RedactSecretsFold is like RedactSecrets but for text that has already been
// lowercased (e.g. search blobs).
func RedactSecretsFold(text string) string { return applyRules(text, secretRulesFold) }
