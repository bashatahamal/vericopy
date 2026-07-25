// Wails uses a temporary "bindings" build tag while reflecting the exported
// bridge API. Production desktop builds use the "desktop" tag. Keeping both
// tags here lets Wails generate bindings without pulling the desktop shell into
// ordinary engine-focused Go test runs.
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

func (b *Bridge) PreviewDestination(request desktop.TransferRequest) (desktop.DestinationPreview, error) {
	return b.service.PreviewDestination(request)
}

func (b *Bridge) StartTransfer(request desktop.TransferRequest) (desktop.TransferResult, error) {
	result, err := b.service.StartTransfer(request)
	if err != nil {
		return desktop.TransferResult{}, err
	}
	return desktop.TransferResult{Result: result, Summary: desktop.FormatResult(result)}, nil
}

func (b *Bridge) EnqueueTransfer(request desktop.TransferRequest) (desktop.TransferJob, error) {
	return b.service.EnqueueTransfer(request)
}

func (b *Bridge) ListTransferJobs() desktop.TransferQueue {
	return b.service.ListTransferJobs()
}

func (b *Bridge) GetTransferJobRequest(id string) (desktop.TransferRequest, error) {
	return b.service.GetTransferJobRequest(id)
}

func (b *Bridge) RetryTransferJob(id, password string) (desktop.TransferJob, error) {
	return b.service.RetryTransferJob(id, password)
}

func (b *Bridge) CancelTransferJob(id string) bool {
	return b.service.CancelTransferJob(id)
}

func (b *Bridge) PauseTransferJob(id string) bool {
	return b.service.PauseTransferJob(id)
}

func (b *Bridge) RemoveTransferJob(id string) (bool, error) {
	return b.service.RemoveTransferJob(id)
}

func (b *Bridge) ClearFinishedTransferJobs() (int, error) {
	return b.service.ClearFinishedTransferJobs()
}

func (b *Bridge) CancelTransfer() bool {
	return b.service.CancelTransfer()
}

// Deprecated: retained for one-time migration to ListSessions.
func (b *Bridge) ListProfiles() ([]desktop.ConnectionProfile, error) {
	return b.service.ListProfiles()
}

// Deprecated: retained for one-time migration to SaveSession.
func (b *Bridge) SaveProfile(profile desktop.ConnectionProfile) (desktop.ConnectionProfile, error) {
	return b.service.SaveProfile(profile)
}

// Deprecated: retained for one-time migration to DeleteSession.
func (b *Bridge) DeleteProfile(id string) (bool, error) {
	return b.service.DeleteProfile(id)
}

func (b *Bridge) ListSessions() ([]desktop.SessionProfile, error) {
	return b.service.ListSessions()
}

func (b *Bridge) SaveSession(session desktop.SessionProfile) (desktop.SessionProfile, error) {
	return b.service.SaveSession(session)
}

func (b *Bridge) DeleteSession(name string) (bool, error) {
	return b.service.DeleteSession(name)
}

func (b *Bridge) ListTransferHistory() ([]desktop.TransferHistoryEntry, error) {
	return b.service.ListTransferHistory()
}

func (b *Bridge) ClearTransferHistory() error {
	return b.service.ClearTransferHistory()
}

func (b *Bridge) beforeClose(ctx context.Context) bool {
	if !b.service.HasActiveJobs() {
		return false
	}
	choice, err := runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type: runtime.QuestionDialog, Title: "Transfers are still active",
		Message: "Minimize Vericopy to keep queued and active transfers running. Quitting safely interrupts active work and pauses the queue.",
		Buttons: []string{"Keep running", "Quit and stop"}, DefaultButton: "Keep running", CancelButton: "Keep running",
	})
	if err != nil || choice != "Quit and stop" {
		runtime.WindowMinimise(ctx)
		return true
	}
	return false
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
		OnBeforeClose:    bridge.beforeClose,
		Bind:             []interface{}{bridge},
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
