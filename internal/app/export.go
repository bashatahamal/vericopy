package app

import (
	"context"
	"errors"
	"strings"

	"github.com/bashatahamal/vericopy/internal/verrors"
)

// PrintFailure renders a command error according to the parsed global flags.
func PrintFailure(globals *Globals, err error) error {
	return globals.printer().Failure(err)
}

// NormalizeCommandError classifies Cobra parsing errors and cancellation.
func NormalizeCommandError(err error) error {
	if err == nil {
		return nil
	}
	var diagnostic *verrors.Error
	if errors.As(err, &diagnostic) {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return verrors.Wrap(verrors.CodeTransferInterrupted, "the operation was canceled", err)
	}
	message := err.Error()
	for _, prefix := range []string{"unknown command", "unknown flag", "required flag", "requires ", "accepts "} {
		if strings.HasPrefix(message, prefix) || strings.Contains(message, prefix) {
			return verrors.Wrap(verrors.CodeInvalidArguments, message, err)
		}
	}
	return err
}
