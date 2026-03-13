package data

import "regexp"

var secretPatterns []*regexp.Regexp

func init() {
	patterns := []string{
		`sk-proj-[A-Za-z0-9_-]{20,}`,
		`sk-ant-[A-Za-z0-9_-]{20,}`,
		`ghp_[A-Za-z0-9]{36,}`,
		`gho_[A-Za-z0-9]{36,}`,
		`github_pat_[A-Za-z0-9_]{20,}`,
		`AKIA[0-9A-Z]{16}`,
		`xoxb-[0-9A-Za-z-]+`,
		`xoxp-[0-9A-Za-z-]+`,
		`eyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}`,
	}
	for _, p := range patterns {
		secretPatterns = append(secretPatterns, regexp.MustCompile(p))
	}
}

// RedactSecrets replaces detected secrets with [REDACTED]
func RedactSecrets(text string) string {
	for _, re := range secretPatterns {
		text = re.ReplaceAllString(text, "[REDACTED]")
	}
	return text
}
