package sshclient

import (
	"testing"

	"github.com/bashatahamal/vericopy/internal/verrors"
)

func TestPasswordAuthenticationRequiresAOneTimePassword(t *testing.T) {
	methods, cleanup, err := authenticationMethods(AuthenticationPassword, "", "")
	cleanup()
	if err == nil || len(methods) != 0 || verrors.As(err).Code != verrors.CodeAuthenticationFailed {
		t.Fatalf("missing password was accepted: methods=%d err=%v", len(methods), err)
	}
}

func TestPasswordAuthenticationDoesNotLoadPublicKeys(t *testing.T) {
	methods, cleanup, err := authenticationMethods(AuthenticationPassword, "/path/that/must/not/be/read", "one-time-secret")
	cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) != 1 {
		t.Fatalf("password mode created %d authentication methods", len(methods))
	}
}

func TestAuthenticationRejectsUnknownMethod(t *testing.T) {
	methods, cleanup, err := authenticationMethods("magic", "", "")
	cleanup()
	if err == nil || len(methods) != 0 || verrors.As(err).Code != verrors.CodeInvalidArguments {
		t.Fatalf("unknown authentication was accepted: methods=%d err=%v", len(methods), err)
	}
}
