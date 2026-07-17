package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/bashatahamal/vericopy/internal/app"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	root, globals := app.NewRoot(os.Stdout, os.Stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		err = app.NormalizeCommandError(err)
		_ = globalsFailure(globals, err)
		os.Exit(verrors.ExitStatus(err))
	}
}

func globalsFailure(globals *app.Globals, err error) error {
	return app.PrintFailure(globals, err)
}
