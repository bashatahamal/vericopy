package verrors_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/bashatahamal/vericopy/internal/verrors"
)

func TestErrorSerializationOmitsCause(t *testing.T) {
	err := verrors.Wrap(verrors.CodeAuthenticationFailed, "authentication failed", errors.New("secret passphrase"))
	data, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(data), "passphrase") {
		t.Fatalf("serialized internal cause: %s", data)
	}
	if !strings.Contains(string(data), `"code":"AUTHENTICATION_FAILED"`) {
		t.Fatalf("missing stable code: %s", data)
	}
}
