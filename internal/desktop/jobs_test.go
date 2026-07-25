package desktop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bashatahamal/vericopy/internal/transfer"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

func TestTransferQueueBoundsConcurrencyAndStartsWaitingJobs(t *testing.T) {
	service, source := queueTestService(t)
	release := make(chan struct{})
	started := make(chan struct{}, 3)
	var running int32
	var maximum int32
	service.executor = func(ctx context.Context, _ preparedTransfer, _ string, progress func(transfer.Progress)) (transfer.Result, error) {
		current := atomic.AddInt32(&running, 1)
		for {
			observed := atomic.LoadInt32(&maximum)
			if current <= observed || atomic.CompareAndSwapInt32(&maximum, observed, current) {
				break
			}
		}
		started <- struct{}{}
		progress(transfer.Progress{Phase: "uploading", Source: source, TransferredBytes: 4, TotalBytes: 8})
		select {
		case <-release:
		case <-ctx.Done():
			atomic.AddInt32(&running, -1)
			return transfer.Result{}, ctx.Err()
		}
		atomic.AddInt32(&running, -1)
		return transfer.Result{Files: 1, Bytes: 8, Verified: true}, nil
	}

	for index := 0; index < 3; index++ {
		if _, err := service.EnqueueTransfer(TransferRequest{
			Source: source, Destination: "transfer@example.com:/srv/report-" + string(rune('a'+index)) + ".pdf",
			Authentication: "key", Resume: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < 2; index++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("two transfer slots did not start")
		}
	}
	select {
	case <-started:
		t.Fatal("a third transfer exceeded the concurrency limit")
	case <-time.After(150 * time.Millisecond):
	}
	queue := service.ListTransferJobs()
	if queue.Running != 2 || queue.Queued != 1 || queue.MaxConcurrent != 2 {
		t.Fatalf("unexpected queue counts: %#v", queue)
	}

	close(release)
	waitForJobs(t, service, func(queue TransferQueue) bool {
		if len(queue.Jobs) != 3 {
			return false
		}
		for _, job := range queue.Jobs {
			if job.Status != JobVerified {
				return false
			}
		}
		return true
	})
	if got := atomic.LoadInt32(&maximum); got != 2 {
		t.Fatalf("maximum concurrency = %d, want 2", got)
	}
}

func TestQueuedPasswordNeverPersistsAndRequiresPasswordAfterRestart(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "desktop-state.json")
	service := NewServiceWithStatePath(statePath)
	service.maxJobs = 0 // Keep both records queued so restart behavior is deterministic.
	source := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(source, []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	keyJob, err := service.EnqueueTransfer(TransferRequest{
		Source: source, Destination: "transfer@example.com:/srv/key-report.pdf", Authentication: "key",
	})
	if err != nil {
		t.Fatal(err)
	}
	passwordJob, err := service.EnqueueTransfer(TransferRequest{
		Source: source, Destination: "transfer@example.com:/srv/password-report.pdf",
		Authentication: "password", Password: "must-never-reach-disk",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "must-never-reach-disk") || strings.Contains(string(data), `"password":`) {
		t.Fatalf("password material was persisted: %s", data)
	}

	restarted := NewServiceWithStatePath(statePath)
	restarted.maxJobs = 0
	queue := restarted.ListTransferJobs()
	statuses := make(map[string]string)
	for _, job := range queue.Jobs {
		statuses[job.ID] = job.Status
	}
	if statuses[keyJob.ID] != JobPaused {
		t.Fatalf("key job restart status = %q, want %q", statuses[keyJob.ID], JobPaused)
	}
	if statuses[passwordJob.ID] != JobNeedsPassword {
		t.Fatalf("password job restart status = %q, want %q", statuses[passwordJob.ID], JobNeedsPassword)
	}
	if _, err := restarted.RetryTransferJob(passwordJob.ID, ""); err == nil || verrors.As(err).Code != verrors.CodeAuthenticationFailed {
		t.Fatalf("password job retried without a password: %v", err)
	}
}

func TestCancelQueuedTransferDoesNotRunIt(t *testing.T) {
	service, source := queueTestService(t)
	service.maxJobs = 0
	job, err := service.EnqueueTransfer(TransferRequest{
		Source: source, Destination: "transfer@example.com:/srv/report.pdf", Authentication: "key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !service.CancelTransferJob(job.ID) {
		t.Fatal("queued job was not canceled")
	}
	queue := service.ListTransferJobs()
	if len(queue.Jobs) != 1 || queue.Jobs[0].Status != JobCanceled {
		t.Fatalf("unexpected canceled queue: %#v", queue)
	}
	removed, err := service.RemoveTransferJob(job.ID)
	if err != nil || !removed || len(service.ListTransferJobs().Jobs) != 0 {
		t.Fatalf("terminal job was not removed: removed=%t err=%v", removed, err)
	}
}

func TestPauseQueuedTransferDoesNotRunIt(t *testing.T) {
	service, source := queueTestService(t)
	service.maxJobs = 0
	job, err := service.EnqueueTransfer(TransferRequest{
		Source: source, Destination: "transfer@example.com:/srv/report.pdf", Authentication: "key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !service.PauseTransferJob(job.ID) {
		t.Fatal("queued job was not paused")
	}
	queue := service.ListTransferJobs()
	if len(queue.Jobs) != 1 || queue.Jobs[0].Status != JobPaused {
		t.Fatalf("unexpected paused queue: %#v", queue)
	}
}

// TestPauseRunningTransferSurvivesRetry is the scenario a user reaches for a
// dedicated Pause button over Cancel: a job is deliberately stopped mid
// transfer, ends up Paused rather than Canceled, and Retry picks it back up
// using the partial bytes already on the server instead of starting over.
func TestPauseRunningTransferSurvivesRetry(t *testing.T) {
	service, source := queueTestService(t)
	started := make(chan struct{})
	var startedOnce sync.Once
	release := make(chan struct{})
	service.executor = func(ctx context.Context, _ preparedTransfer, _ string, progress func(transfer.Progress)) (transfer.Result, error) {
		progress(transfer.Progress{Phase: "uploading", Source: source, TransferredBytes: 4, TotalBytes: 8})
		startedOnce.Do(func() { close(started) })
		select {
		case <-release:
			return transfer.Result{Files: 1, Bytes: 8, Verified: true}, nil
		case <-ctx.Done():
			return transfer.Result{}, ctx.Err()
		}
	}
	job, err := service.EnqueueTransfer(TransferRequest{
		Source: source, Destination: "transfer@example.com:/srv/report.pdf", Authentication: "key", Resume: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("transfer did not start")
	}
	if !service.PauseTransferJob(job.ID) {
		t.Fatal("running job was not paused")
	}
	waitForJobs(t, service, func(queue TransferQueue) bool {
		return len(queue.Jobs) == 1 && queue.Jobs[0].Status == JobPaused
	})
	close(release)

	retried, err := service.RetryTransferJob(job.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != JobQueued {
		t.Fatalf("retried job status = %q, want %q", retried.Status, JobQueued)
	}
	waitForJobs(t, service, func(queue TransferQueue) bool {
		return len(queue.Jobs) == 1 && queue.Jobs[0].Status == JobVerified
	})
}

func queueTestService(t *testing.T) (*Service, string) {
	t.Helper()
	service := NewServiceWithStatePath(filepath.Join(t.TempDir(), "desktop-state.json"))
	source := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(source, []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return service, source
}

func waitForJobs(t *testing.T, service *Service, ready func(TransferQueue) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready(service.ListTransferJobs()) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("transfer jobs did not reach the expected state: %#v", service.ListTransferJobs())
}
