package diagnostics

import (
	"regexp"
	"strings"
)

var sensitiveAssignment = regexp.MustCompile(`(?i)\b(password|passphrase|token|secret|private[_-]?key)\s*[:=]\s*([^\s,;]+)`)

// Redact removes common secret-shaped values from diagnostic text.
func Redact(value string) string {
	redacted := sensitiveAssignment.ReplaceAllString(value, "$1=[REDACTED]")
	if strings.Contains(redacted, "-----BEGIN") && strings.Contains(redacted, "PRIVATE KEY-----") {
		return "[REDACTED PRIVATE KEY]"
	}
	return redacted
}
