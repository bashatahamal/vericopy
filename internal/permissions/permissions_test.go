package permissions_test

import (
	"testing"

	"github.com/bashatahamal/vericopy/internal/permissions"
)

func TestPresets(t *testing.T) {
	policy, err := permissions.Resolve("service-readonly", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if permissions.Octal(policy.Directory) != "2750" || permissions.Octal(policy.File) != "0640" {
		t.Fatalf("unexpected service policy: dir=%s file=%s", permissions.Octal(policy.Directory), permissions.Octal(policy.File))
	}
}

func TestOverride(t *testing.T) {
	policy, err := permissions.Resolve("private", "664", "2775")
	if err != nil {
		t.Fatal(err)
	}
	if permissions.Octal(policy.Directory) != "2775" || permissions.Octal(policy.File) != "0664" {
		t.Fatalf("unexpected override: %#v", policy)
	}
}

func TestInvalidModes(t *testing.T) {
	for _, mode := range []string{"99", "0888", "10000", "4755"} {
		if _, err := permissions.Resolve("private", mode, ""); err == nil {
			t.Fatalf("expected rejection for %q", mode)
		}
	}
}
