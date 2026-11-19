package data

import (
	"regexp"
	"strings"
	"testing"
)

// Secret-shaped literals are split with "+" so secret scanners (ggshield) do
// not flag these fake test values in the source.
//
// secretSamples are fake but correctly shaped secrets. secret is the part that
// must not survive redaction.
var secretSamples = []struct{ name, text, secret string }{
	{"anthropic", "key sk-ant-api03-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789_-abcdEFGH end", "AbCdEfGhIjKlMnOpQrStUvWxYz"},
	{"openai project", "sk-proj-AbCdEfGhIjKlMnOpQrStUvWx0123456789", "AbCdEfGhIjKlMnOpQrStUvWx"},
	{"openai legacy", "OPENAI=sk-AbCdEfGhIjKlMnOpQrStUvWxYz0123456789ABCDEFGHIJKL", "AbCdEfGhIjKlMnOpQrStUvWxYz"},
	{"github ghp", "token " + "ghp_" + strings.Repeat("a1B2", 9), "a1B2a1B2a1B2"},
	{"github ghs", "ghs_" + strings.Repeat("a1B2", 9), "a1B2a1B2a1B2"},
	{"github ghu", "ghu_" + strings.Repeat("a1B2", 9), "a1B2a1B2a1B2"},
	{"github ghr", "ghr_" + strings.Repeat("a1B2", 9), "a1B2a1B2a1B2"},
	{"github fine-grained", "github_pat_11ABCDEFG0abcdefghij_klmnopqrstuvwxyz0123456789ABCDEFGHIJ", "klmnopqrstuvwxyz"},
	{"aws access", "id AK" + "IAIOSFODNN7EXAMPLE", "IOSFODNN7EXAMPLE"},
	{"aws temp", "id AS" + "IAIOSFODNN7EXAMPLE", "IOSFODNN7EXAMPLE"},
	{"aws secret key", "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "wJalrXUtnFEMI"},
	{"slack bot", "xo" + "xb-123456789012-1234567890123-AbCdEfGhIjKlMnOpQrStUvWx", "123456789012"},
	{"slack app", "xa" + "pp-1-A0123456789-1234567890-abcdef0123456789", "A0123456789"},
	{"slack webhook", "https://hooks.sl" + "ack.com/services/T00000000/B00000000/" + strings.Repeat("X", 24), "T00000000"},
	{"jwt", "ey" + "JhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." + "eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4ifQ." + "SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c", "SflKxwRJSMeKKF2QT4fw"},
	{"google api", "AI" + "zaSyA-1234567890abcdefghijklmnopqrstuv", "1234567890abcdefghij"},
	{"stripe", "sk_live_" + strings.Repeat("aB3dE5", 5), "aB3dE5aB3dE5"},
	{"sendgrid", "S" + "G.abcdefghijklmnopqrstuv." + "abcdefghijklmnopqrstuvwxyz" + "0123456789ABCDEFG", "abcdefghijklmnopqrstuv"},
	{"huggingface", "hf_" + strings.Repeat("aB3dE5", 6), "aB3dE5aB3dE5"},
	{"npm", "npm_" + strings.Repeat("aB3dE5", 6), "aB3dE5aB3dE5"},
	{"bearer", "Authorization: Bearer 3f9a8c7d6e5b4a392817060504030201", "3f9a8c7d6e5b4a39"},
	{"db url password", "postgres://admin:S3cretP4ss@db.example.com:5432/prod", "S3cretP4ss"},
	{"password assign", "password=hunter2hunter2", "hunter2hunter2"},
	{"json password", `{"password": "correct-horse-battery"}`, "correct-horse-battery"},
	{"api key assign", "API_KEY: abcd1234efgh5678", "abcd1234efgh5678"},
	{"private key block", "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA1234567890abcdef\n-----END RSA PRIVATE KEY-----", "MIIEowIBAAKCAQEA"},
	{"private key truncated", "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAABG5vbmU", "b3BlbnNzaC1rZXktdjE"},
}

func TestRedactSecrets_Table(t *testing.T) {
	for _, c := range secretSamples {
		got := RedactSecrets(c.text)
		if strings.Contains(got, c.secret) || !strings.Contains(got, "[REDACTED]") {
			t.Errorf("%s: not redacted: %q", c.name, got)
		}
	}
}

// Saved search blobs are lowercased before redaction, so every sample must
// also be caught in lowercase form.
func TestRedactSecretsFold_Table(t *testing.T) {
	for _, c := range secretSamples {
		lower := strings.ToLower(c.text)
		got := RedactSecretsFold(lower)
		if strings.Contains(got, strings.ToLower(c.secret)) || !strings.Contains(got, "[REDACTED]") {
			t.Errorf("%s: lowercased not redacted: %q", c.name, got)
		}
	}
}

func TestRedactSecrets_KeepsContext(t *testing.T) {
	cases := map[string]string{
		"Authorization: Bearer 3f9a8c7d6e5b4a392817060504030201 ok":    "Authorization: Bearer [REDACTED] ok",
		"postgres://admin:S3cretP4ss@db.example.com/prod":              "postgres://admin:[REDACTED]@db.example.com/prod",
		`password="correct-horse-battery" and more`:                    `password="[REDACTED]" and more`,
		"aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPL": "aws_secret_access_key = [REDACTED]",
	}
	for in, want := range cases {
		if got := RedactSecrets(in); got != want {
			t.Errorf("RedactSecrets(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRedactSecrets_NoFalsePositives(t *testing.T) {
	for _, s := range []string{
		"task-ant-farm-schedule-xxxxxxxxxxxxxxxxxxxx", // "sk-ant-" inside a word
		"risk-proj-planning-document-for-the-team-2026",
		"see xoxb- prefix docs",
		"the password field is required",
		"max_tokens=4096 and token_count: 12",
		"git push origin main",
		"https://example.com/path/to/page",
		"user@example.com wrote about keys and tokens",
		"ghost_writer_of_the_book_has_a_very_long_name_here",
		"the key=value pair",
	} {
		if got := RedactSecrets(s); got != s {
			t.Errorf("false positive: %q -> %q", s, got)
		}
		if got := RedactSecretsFold(strings.ToLower(s)); got != strings.ToLower(s) {
			t.Errorf("fold false positive: %q -> %q", s, got)
		}
	}
}

func TestRedactSecrets_MultipleAndIdempotent(t *testing.T) {
	in := "a AK" + "IAIOSFODNN7EXAMPLE b password=hunter2hunter2 c sk-proj-AbCdEfGhIjKlMnOpQrStUvWx0123456789"
	once := RedactSecrets(in)
	if strings.Count(once, "[REDACTED]") != 3 {
		t.Errorf("want 3 redactions: %q", once)
	}
	if RedactSecrets(once) != once {
		t.Errorf("not idempotent: %q", RedactSecrets(once))
	}
}

// Folding lowercases the pattern text, which would corrupt uppercase regex
// escapes (\S -> \s, \D -> \d, \W -> \w, \B -> \b ...).
func TestRedactRulesFoldSafe(t *testing.T) {
	bad := regexp.MustCompile(`\\[A-Z]`)
	for _, r := range redactRules {
		if bad.MatchString(r.pat) {
			t.Errorf("pattern uses an uppercase escape, unsafe to fold: %s", r.pat)
		}
		for _, h := range r.hints {
			if h != strings.ToLower(h) {
				t.Errorf("hint %q must be lowercase", h)
			}
		}
	}
}

// Every rule's hints must be necessary conditions: a sample the rule matches
// must contain one of its hints, or the prefilter would hide real secrets.
func TestRedactRuleHintsCoverSamples(t *testing.T) {
	for _, c := range secretSamples {
		lower := strings.ToLower(c.text)
		for _, r := range secretRules {
			if r.re.MatchString(c.text) {
				ok := false
				for _, h := range r.hints {
					ok = ok || strings.Contains(lower, h)
				}
				if !ok {
					t.Errorf("%s: rule %s matches but no hint present", c.name, r.re)
				}
			}
		}
	}
}
