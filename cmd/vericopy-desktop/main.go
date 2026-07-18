// Wails uses a temporary "bindings" build tag while reflecting the exported
// bridge API. Production desktop builds use the "desktop" tag. Keeping both
// tags here lets Wails generate bindings without pulling the desktop shell into
// ordinary CLI-focused Go test runs.
//go:build desktop || bindings

package main

import (
	"context"
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/bashatahamal/vericopy/internal/desktop"
)

//go:embed all:frontend/dist
var assets embed.FS

// Bridge is the Wails-facing adapter. It contains no transfer policy: all
// validation and execution stay inside the shared desktop service.
type Bridge struct {
	service *desktop.Service
	ctx     context.Context
}

func newBridge() *Bridge {
	return &Bridge{service: desktop.NewService()}
}

func (b *Bridge) startup(ctx context.Context) {
	b.ctx = ctx
	b.service.SetContext(ctx)
	b.service.SetProgressHandler(func(progress desktop.TransferProgress) {
		runtime.EventsEmit(ctx, "transfer:progress", progress)
	})
}

func (b *Bridge) shutdown(_ context.Context) {
	b.service.Close()
}

func (b *Bridge) GetDashboard() desktop.Dashboard {
	return b.service.GetDashboard()
}

func (b *Bridge) ReviewTransfer(request desktop.TransferRequest) (desktop.TransferReview, error) {
	return b.service.ReviewTransfer(request)
}

func (b *Bridge) StartTransfer(request desktop.TransferRequest) (desktop.TransferResult, error) {
	result, err := b.service.StartTransfer(request)
	if err != nil {
		return desktop.TransferResult{}, err
	}
	return desktop.TransferResult{Result: result, Summary: desktop.FormatResult(result)}, nil
}

func (b *Bridge) CancelTransfer() bool {
	return b.service.CancelTransfer()
}

func (b *Bridge) ListProfiles() ([]desktop.ConnectionProfile, error) {
	return b.service.ListProfiles()
}

func (b *Bridge) SaveProfile(profile desktop.ConnectionProfile) (desktop.ConnectionProfile, error) {
	return b.service.SaveProfile(profile)
}

func (b *Bridge) DeleteProfile(id string) (bool, error) {
	return b.service.DeleteProfile(id)
}

func (b *Bridge) ListTransferHistory() ([]desktop.TransferHistoryEntry, error) {
	return b.service.ListTransferHistory()
}

func (b *Bridge) ClearTransferHistory() error {
	return b.service.ClearTransferHistory()
}

func (b *Bridge) SelectSourceFile() (string, error) {
	return runtime.OpenFileDialog(b.ctx, runtime.OpenDialogOptions{Title: "Choose a source file"})
}

func (b *Bridge) SelectSourceDirectory() (string, error) {
	return runtime.OpenDirectoryDialog(b.ctx, runtime.OpenDialogOptions{Title: "Choose a source folder"})
}

func (b *Bridge) SelectIdentityFile() (string, error) {
	return runtime.OpenFileDialog(b.ctx, runtime.OpenDialogOptions{Title: "Choose an SSH private key"})
}

func main() {
	bridge := newBridge()
	if err := wails.Run(&options.App{
		Title:     "Vericopy",
		Width:     1280,
		Height:    820,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: options.NewRGB(250, 249, 247),
		OnStartup:        bridge.startup,
		OnShutdown:       bridge.shutdown,
		Bind:             []interface{}{bridge},
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
