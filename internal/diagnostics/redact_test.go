package diagnostics_test

import (
	"strings"
	"testing"

	"github.com/bashatahamal/vericopy/internal/diagnostics"
)

func TestRedact(t *testing.T) {
	got := diagnostics.Redact("token=abc123 password:letmein safe=value")
	if strings.Contains(got, "abc123") || strings.Contains(got, "letmein") {
		t.Fatalf("secret remained in %q", got)
	}
	if !strings.Contains(got, "safe=value") {
		t.Fatalf("safe context was removed: %q", got)
	}
}

func TestRedactPrivateKey(t *testing.T) {
	input := "-----BEGIN OPENSSH " + "PRIVATE KEY-----\ntest fixture\n-----END OPENSSH " + "PRIVATE KEY-----"
	if got := diagnostics.Redact(input); got != "[REDACTED PRIVATE KEY]" {
		t.Fatalf("unexpected redaction: %q", got)
	}
}
