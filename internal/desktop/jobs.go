package desktop

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"time"

	"github.com/bashatahamal/vericopy/internal/sshclient"
	"github.com/bashatahamal/vericopy/internal/transfer"
	"github.com/bashatahamal/vericopy/internal/verrors"
	"github.com/bashatahamal/vericopy/internal/wakelock"
)

const (
	defaultConcurrentJobs = 2
	maxTransferJobs       = 100

	JobQueued        = "queued"
	JobRunning       = "running"
	JobCancelling    = "cancelling"
	JobPausing       = "pausing"
	JobPaused        = "paused"
	JobNeedsPassword = "needs_password"
	JobVerified      = "verified"
	JobInterrupted   = "interrupted"
	JobFailed        = "failed"
	JobCanceled      = "canceled"
)

type transferExecutor func(context.Context, preparedTransfer, string, func(transfer.Progress)) (transfer.Result, error)

// TransferJob is the non-secret job summary exposed to the desktop frontend.
// Complete paths, identity paths, known_hosts paths, and passwords are omitted.
type TransferJob struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
	Status      string    `json:"status"`
	Phase       string    `json:"phase,omitempty"`
	SourceName  string    `json:"source_name"`
	Destination string    `json:"destination"`
	// LocalPath is the on-disk path this transfer ultimately produced or
	// read: the resolved local file for a verified download, or the
	// original local source for a verified upload. It is set only once a
	// job finishes successfully, so the desktop UI can offer to reveal it
	// in the OS file manager.
	LocalPath        string       `json:"local_path,omitempty"`
	Authentication   string       `json:"authentication"`
	TransferredBytes int64        `json:"transferred_bytes,omitempty"`
	TotalBytes       int64        `json:"total_bytes,omitempty"`
	ResumedBytes     int64        `json:"resumed_bytes,omitempty"`
	CurrentFile      int          `json:"current_file,omitempty"`
	TotalFiles       int          `json:"total_files,omitempty"`
	Files            int          `json:"files,omitempty"`
	SkippedFiles     int          `json:"skipped_files,omitempty"`
	Bytes            int64        `json:"bytes,omitempty"`
	Verified         bool         `json:"verified"`
	DiagnosticCode   verrors.Code `json:"diagnostic_code,omitempty"`
	Message          string       `json:"message,omitempty"`
}

// TransferQueue is a complete point-in-time view of the local scheduler.
type TransferQueue struct {
	Jobs          []TransferJob `json:"jobs"`
	Running       int           `json:"running"`
	Queued        int           `json:"queued"`
	MaxConcurrent int           `json:"max_concurrent"`
}

// persistedTransferRequest deliberately has no password field. It is the only
// request shape allowed into desktop-state.json.
type persistedTransferRequest struct {
	Source             string `json:"source"`
	Destination        string `json:"destination"`
	Direction          string `json:"direction,omitempty"`
	Authentication     string `json:"authentication"`
	Identity           string `json:"identity,omitempty"`
	KnownHosts         string `json:"known_hosts,omitempty"`
	Port               int    `json:"port"`
	Permissions        string `json:"permissions"`
	Group              string `json:"group,omitempty"`
	ReadableBy         string `json:"readable_by,omitempty"`
	Recursive          bool   `json:"recursive"`
	Resume             bool   `json:"resume"`
	Overwrite          bool   `json:"overwrite"`
	NoClobber          bool   `json:"no_clobber"`
	PreserveTime       bool   `json:"preserve_time"`
	FixMediaNames      bool   `json:"fix_media_names"`
	GenerateThumbnails bool   `json:"generate_thumbnails"`
}

type persistedTransferJob struct {
	Job     TransferJob              `json:"job"`
	Request persistedTransferRequest `json:"request"`
}

type runtimeJob struct {
	record          persistedTransferJob
	password        string
	cancel          context.CancelFunc
	cancelRequested bool
	pauseRequested  bool
}

func persistedRequest(request TransferRequest) persistedTransferRequest {
	return persistedTransferRequest{
		Source: request.Source, Destination: request.Destination, Direction: request.Direction, Authentication: request.Authentication,
		Identity: request.Identity, KnownHosts: request.KnownHosts, Port: request.Port,
		Permissions: request.Permissions, Group: request.Group, ReadableBy: request.ReadableBy,
		Recursive: request.Recursive, Resume: request.Resume, Overwrite: request.Overwrite,
		NoClobber: request.NoClobber, PreserveTime: request.PreserveTime,
		FixMediaNames: request.FixMediaNames, GenerateThumbnails: request.GenerateThumbnails,
	}
}

func (request persistedTransferRequest) liveRequest() TransferRequest {
	return TransferRequest{
		Source: request.Source, Destination: request.Destination, Direction: request.Direction, Authentication: request.Authentication,
		Identity: request.Identity, KnownHosts: request.KnownHosts, Port: request.Port,
		Permissions: request.Permissions, Group: request.Group, ReadableBy: request.ReadableBy,
		Recursive: request.Recursive, Resume: request.Resume, Overwrite: request.Overwrite,
		NoClobber: request.NoClobber, PreserveTime: request.PreserveTime,
		FixMediaNames: request.FixMediaNames, GenerateThumbnails: request.GenerateThumbnails,
	}
}

// EnqueueTransfer validates and queues one transfer. Passwords are retained
// only by the in-memory runtime job until its SSH handshake starts.
func (s *Service) EnqueueTransfer(request TransferRequest) (TransferJob, error) {
	password := request.Password
	request.Password = ""
	prepared, err := prepare(request)
	if err != nil {
		return TransferJob{}, err
	}
	if prepared.request.Authentication == sshclient.AuthenticationPassword && password == "" {
		return TransferJob{}, verrors.New(verrors.CodeAuthenticationFailed,
			"enter the SSH password before adding this transfer")
	}
	if prepared.request.Direction == transferDirectionDownload {
		prepared.request.Source = canonicalDestination(prepared.remote)
		prepared.request.Destination = prepared.local
	} else {
		prepared.request.Source = prepared.local
		prepared.request.Destination = canonicalDestination(prepared.remote)
	}

	id, err := randomID()
	if err != nil {
		return TransferJob{}, err
	}
	sourceName := filepath.Base(prepared.local)
	if prepared.request.Direction == transferDirectionDownload {
		sourceName = path.Base(prepared.remote.Path)
	}
	if sourceName == "." || sourceName == string(filepath.Separator) || sourceName == "" {
		sourceName = "source"
	}
	record := persistedTransferJob{
		Job: TransferJob{
			ID: id, CreatedAt: time.Now().UTC(), Status: JobQueued, Phase: JobQueued,
			SourceName: sourceName, Destination: redactedDestination(prepared.remote),
			Authentication: prepared.request.Authentication, Message: "Waiting for an available transfer slot",
		},
		Request: persistedRequest(prepared.request),
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		password = ""
		return TransferJob{}, verrors.New(verrors.CodeTransferFailed, "the transfer manager is shutting down")
	}
	if len(s.jobs) >= maxTransferJobs {
		s.mu.Unlock()
		password = ""
		return TransferJob{}, verrors.New(verrors.CodeTransferFailed,
			"the transfer manager already contains 100 jobs").WithHint("Remove finished jobs before adding another transfer.")
	}
	s.jobs[id] = &runtimeJob{record: record, password: password}
	s.jobOrder = append(s.jobOrder, id)
	s.mu.Unlock()
	password = ""

	if err := s.state.saveTransferJob(record); err != nil {
		s.mu.Lock()
		delete(s.jobs, id)
		s.removeJobOrderLocked(id)
		s.mu.Unlock()
		return TransferJob{}, err
	}
	s.emitJobProgress(record.Job, "queued", record.Job.Message)
	go s.dispatchJobs()
	return record.Job, nil
}

// ListTransferJobs returns active and retained jobs without exposing stored
// request paths or credentials.
func (s *Service) ListTransferJobs() TransferQueue {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs := make([]TransferJob, 0, len(s.jobOrder))
	for _, id := range s.jobOrder {
		if job := s.jobs[id]; job != nil {
			jobs = append(jobs, job.record.Job)
		}
	}
	sort.SliceStable(jobs, func(left, right int) bool {
		return jobs[left].CreatedAt.After(jobs[right].CreatedAt)
	})
	return TransferQueue{Jobs: jobs, Running: s.runningJobs, Queued: s.queuedJobsLocked(), MaxConcurrent: s.maxJobs}
}

// GetTransferJobRequest returns the non-secret setup for a retry form.
func (s *Service) GetTransferJobRequest(id string) (TransferRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[id]
	if job == nil {
		return TransferRequest{}, verrors.New(verrors.CodeInvalidArguments, "the transfer job no longer exists")
	}
	return job.record.Request.liveRequest(), nil
}

// RetryTransferJob places an interrupted or failed job back in the queue.
func (s *Service) RetryTransferJob(id, password string) (TransferJob, error) {
	s.mu.Lock()
	job := s.jobs[id]
	if job == nil {
		s.mu.Unlock()
		return TransferJob{}, verrors.New(verrors.CodeInvalidArguments, "the transfer job no longer exists")
	}
	status := job.record.Job.Status
	if status != JobPaused && status != JobNeedsPassword && status != JobInterrupted && status != JobFailed && status != JobCanceled {
		s.mu.Unlock()
		return TransferJob{}, verrors.New(verrors.CodeInvalidArguments, fmt.Sprintf("a %s transfer cannot be retried", status))
	}
	if job.record.Job.Authentication == sshclient.AuthenticationPassword && password == "" {
		s.mu.Unlock()
		return TransferJob{}, verrors.New(verrors.CodeAuthenticationFailed, "enter the SSH password before retrying this transfer")
	}
	previous := job.record
	job.password = password
	job.cancelRequested = false
	job.record.Job.Status = JobQueued
	job.record.Job.Phase = JobQueued
	job.record.Job.StartedAt = time.Time{}
	job.record.Job.CompletedAt = time.Time{}
	job.record.Job.TransferredBytes = 0
	job.record.Job.TotalBytes = 0
	job.record.Job.ResumedBytes = 0
	job.record.Job.Files = 0
	job.record.Job.Bytes = 0
	job.record.Job.Verified = false
	job.record.Job.DiagnosticCode = ""
	job.record.Job.Message = "Waiting for an available transfer slot"
	record := job.record
	s.mu.Unlock()
	password = ""
	if err := s.state.saveTransferJob(record); err != nil {
		s.mu.Lock()
		if current := s.jobs[id]; current != nil {
			current.record = previous
			current.password = ""
		}
		s.mu.Unlock()
		return TransferJob{}, err
	}
	s.emitJobProgress(record.Job, JobQueued, record.Job.Message)
	go s.dispatchJobs()
	return record.Job, nil
}

// CancelTransferJob cancels a queued or running job. Compatible partial state
// remains available when the transfer engine reports an interruption.
func (s *Service) CancelTransferJob(id string) bool {
	s.mu.Lock()
	job := s.jobs[id]
	if job == nil {
		s.mu.Unlock()
		return false
	}
	if job.record.Job.Status == JobQueued || job.record.Job.Status == JobPaused || job.record.Job.Status == JobNeedsPassword {
		job.password = ""
		job.record.Job.Status = JobCanceled
		job.record.Job.Phase = JobCanceled
		job.record.Job.CompletedAt = time.Now().UTC()
		job.record.Job.Message = "Canceled before transfer"
		record := job.record
		s.mu.Unlock()
		_ = s.state.saveTransferJob(record)
		s.emitJobProgress(record.Job, JobCanceled, record.Job.Message)
		return true
	}
	if job.record.Job.Status != JobRunning && job.record.Job.Status != JobCancelling && job.record.Job.Status != JobPausing {
		s.mu.Unlock()
		return false
	}
	job.cancelRequested = true
	job.pauseRequested = false
	job.record.Job.Status = JobCancelling
	job.record.Job.Phase = JobCancelling
	job.record.Job.Message = "Cancellation requested; compatible partial state will be kept"
	cancel := job.cancel
	record := job.record
	s.mu.Unlock()
	s.emitJobProgress(record.Job, JobCancelling, record.Job.Message)
	if cancel != nil {
		cancel()
	}
	return true
}

// PauseTransferJob pauses a queued or running job. Compatible partial state
// remains available for a later Retry, exactly like a canceled job, but the
// job is marked Paused rather than Canceled so its intent stays clear:
// this is expected to be picked back up, not abandoned.
func (s *Service) PauseTransferJob(id string) bool {
	s.mu.Lock()
	job := s.jobs[id]
	if job == nil {
		s.mu.Unlock()
		return false
	}
	if job.record.Job.Status == JobQueued || job.record.Job.Status == JobNeedsPassword {
		job.password = ""
		job.record.Job.Status = JobPaused
		job.record.Job.Phase = JobPaused
		job.record.Job.CompletedAt = time.Now().UTC()
		job.record.Job.Message = "Paused before transfer; resume when ready"
		record := job.record
		s.mu.Unlock()
		_ = s.state.saveTransferJob(record)
		s.emitJobProgress(record.Job, JobPaused, record.Job.Message)
		return true
	}
	if job.record.Job.Status != JobRunning && job.record.Job.Status != JobPausing {
		s.mu.Unlock()
		return false
	}
	job.pauseRequested = true
	job.cancelRequested = false
	job.record.Job.Status = JobPausing
	job.record.Job.Phase = JobPausing
	job.record.Job.Message = "Pausing; compatible partial state will be kept"
	cancel := job.cancel
	record := job.record
	s.mu.Unlock()
	s.emitJobProgress(record.Job, JobPausing, record.Job.Message)
	if cancel != nil {
		cancel()
	}
	return true
}

// RemoveTransferJob removes one retained terminal job from the manager.
func (s *Service) RemoveTransferJob(id string) (bool, error) {
	s.mu.Lock()
	job := s.jobs[id]
	if job == nil {
		s.mu.Unlock()
		return false, nil
	}
	if !terminalJobStatus(job.record.Job.Status) {
		s.mu.Unlock()
		return false, verrors.New(verrors.CodeInvalidArguments, "an active transfer job cannot be removed")
	}
	delete(s.jobs, id)
	s.removeJobOrderLocked(id)
	delete(s.jobProgress, id)
	s.mu.Unlock()
	if err := s.state.deleteTransferJob(id); err != nil {
		return false, err
	}
	return true, nil
}

// ClearFinishedTransferJobs removes retained terminal jobs but keeps queued,
// running, paused, and credential-waiting work.
func (s *Service) ClearFinishedTransferJobs() (int, error) {
	s.mu.Lock()
	ids := make([]string, 0)
	for id, job := range s.jobs {
		if terminalJobStatus(job.record.Job.Status) {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	removed := 0
	for _, id := range ids {
		ok, err := s.RemoveTransferJob(id)
		if err != nil {
			return removed, err
		}
		if ok {
			removed++
		}
	}
	return removed, nil
}

// HasActiveJobs reports whether closing the process would interrupt work.
func (s *Service) HasActiveJobs() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runningJobs > 0 || s.queuedJobsLocked() > 0
}

func (s *Service) restoreJobs() {
	records, err := s.state.listTransferJobs()
	if err != nil {
		return
	}
	for _, record := range records {
		switch record.Job.Status {
		case JobQueued, JobRunning, JobCancelling, JobPausing:
			if record.Job.Authentication == sshclient.AuthenticationPassword {
				record.Job.Status = JobNeedsPassword
				record.Job.Phase = JobNeedsPassword
				record.Job.Message = "Enter the SSH password to resume after restart"
			} else {
				record.Job.Status = JobPaused
				record.Job.Phase = JobPaused
				record.Job.Message = "Paused when Vericopy last exited; retry when ready"
			}
			record.Job.CompletedAt = time.Time{}
			_ = s.state.saveTransferJob(record)
		}
		s.jobs[record.Job.ID] = &runtimeJob{record: record}
		s.jobOrder = append(s.jobOrder, record.Job.ID)
	}
}

func (s *Service) dispatchJobs() {
	for {
		s.mu.Lock()
		if s.closed || s.active || s.runningJobs >= s.maxJobs {
			s.mu.Unlock()
			return
		}
		var selected *runtimeJob
		for _, id := range s.jobOrder {
			job := s.jobs[id]
			if job != nil && job.record.Job.Status == JobQueued {
				selected = job
				break
			}
		}
		if selected == nil {
			s.mu.Unlock()
			return
		}
		root := s.root
		if root == nil {
			root = context.Background()
		}
		ctx, cancel := context.WithCancel(root)
		selected.cancel = cancel
		selected.cancelRequested = false
		selected.record.Job.Status = JobRunning
		selected.record.Job.Phase = "connecting"
		selected.record.Job.StartedAt = time.Now().UTC()
		selected.record.Job.Message = "Connecting with strict host verification"
		record := selected.record
		password := selected.password
		selected.password = ""
		s.runningJobs++
		if s.runningJobs == 1 {
			s.wakeLock = wakelock.Acquire("Vericopy transfer in progress")
		}
		s.jobWG.Add(1)
		s.mu.Unlock()

		_ = s.state.saveTransferJob(record)
		s.emitJobProgress(record.Job, "connecting", record.Job.Message)
		go s.runJob(ctx, record.Job.ID, password)
	}
}

func (s *Service) runJob(ctx context.Context, id, password string) {
	defer s.jobWG.Done()
	s.mu.Lock()
	job := s.jobs[id]
	if job == nil {
		s.runningJobs--
		s.mu.Unlock()
		return
	}
	request := job.record.Request.liveRequest()
	s.mu.Unlock()
	prepared, err := prepare(request)
	if err != nil {
		password = ""
		s.finishJob(id, preparedTransfer{}, transfer.Result{}, err)
		return
	}
	result, transferErr := s.execute(ctx, prepared, password, func(update transfer.Progress) {
		s.updateJobProgress(id, update)
	})
	password = ""
	s.finishJob(id, prepared, result, transferErr)
}

// releaseWakeLockIfIdleLocked releases the sleep-prevention lock once no job
// is running. Callers must hold s.mu.
func (s *Service) releaseWakeLockIfIdleLocked() {
	if s.runningJobs == 0 && s.wakeLock != nil {
		s.wakeLock()
		s.wakeLock = nil
	}
}

func (s *Service) finishJob(id string, prepared preparedTransfer, result transfer.Result, transferErr error) {
	now := time.Now().UTC()
	s.mu.Lock()
	job := s.jobs[id]
	if job == nil {
		if s.runningJobs > 0 {
			s.runningJobs--
		}
		s.releaseWakeLockIfIdleLocked()
		s.mu.Unlock()
		go s.dispatchJobs()
		return
	}
	job.cancel = nil
	job.password = ""
	job.record.Job.CompletedAt = now
	job.record.Job.Files = result.Files
	job.record.Job.SkippedFiles = result.SkippedFiles
	job.record.Job.Bytes = result.Bytes
	job.record.Job.ResumedBytes = result.ResumedBytes
	job.record.Job.Verified = result.Verified
	job.record.Job.TransferredBytes = result.Bytes
	if result.Bytes > 0 {
		job.record.Job.TotalBytes = result.Bytes
	}
	if transferErr == nil {
		job.record.Job.Status = JobVerified
		job.record.Job.Phase = "completed"
		job.record.Job.Message = "Transfer verified"
		if prepared.request.Direction == transferDirectionDownload {
			job.record.Job.LocalPath = result.Destination
		} else {
			job.record.Job.LocalPath = prepared.local
		}
	} else {
		diagnostic := verrors.As(transferErr)
		job.record.Job.DiagnosticCode = diagnostic.Code
		job.record.Job.Status = JobFailed
		job.record.Job.Phase = JobFailed
		job.record.Job.Message = diagnostic.Message
		if job.pauseRequested {
			job.record.Job.Status = JobPaused
			job.record.Job.Phase = JobPaused
			job.record.Job.Message = "Paused; resume when ready"
		} else if job.cancelRequested {
			job.record.Job.Status = JobCanceled
			job.record.Job.Phase = JobCanceled
			job.record.Job.Message = "Transfer canceled; compatible partial state kept"
		} else if diagnostic.Code == verrors.CodeTransferInterrupted || ctxCanceled(transferErr) {
			job.record.Job.Status = JobInterrupted
			job.record.Job.Phase = JobInterrupted
		}
	}
	job.cancelRequested = false
	record := job.record
	if s.runningJobs > 0 {
		s.runningJobs--
	}
	s.releaseWakeLockIfIdleLocked()
	s.mu.Unlock()

	_ = s.state.saveTransferJob(record)
	entry := TransferHistoryEntry{
		StartedAt: record.Job.StartedAt, CompletedAt: record.Job.CompletedAt, Status: record.Job.Status,
		SourceName: record.Job.SourceName, Destination: record.Job.Destination, Files: result.Files,
		Bytes: result.Bytes, ResumedBytes: result.ResumedBytes, Verified: result.Verified,
		DiagnosticCode: record.Job.DiagnosticCode,
	}
	_ = s.state.recordHistory(entry)
	s.emitJobProgress(record.Job, record.Job.Phase, record.Job.Message)
	go s.dispatchJobs()
}

func (s *Service) updateJobProgress(id string, update transfer.Progress) {
	s.mu.Lock()
	job := s.jobs[id]
	if job == nil {
		s.mu.Unlock()
		return
	}
	job.record.Job.Phase = update.Phase
	job.record.Job.TransferredBytes = update.TransferredBytes
	job.record.Job.TotalBytes = update.TotalBytes
	job.record.Job.ResumedBytes = update.ResumedBytes
	job.record.Job.CurrentFile = update.CurrentFile
	job.record.Job.TotalFiles = update.TotalFiles
	if name := filepath.Base(update.Source); name != "." && name != string(filepath.Separator) && name != "" {
		job.record.Job.SourceName = name
	}
	record := job.record.Job
	handler := s.progress
	now := time.Now()
	last := s.jobProgress[id]
	if update.Phase == "uploading" && update.TotalBytes > 0 && update.TransferredBytes < update.TotalBytes && now.Sub(last) < 100*time.Millisecond {
		s.mu.Unlock()
		return
	}
	s.jobProgress[id] = now
	s.mu.Unlock()
	if handler != nil {
		handler(TransferProgress{
			JobID: id, Phase: update.Phase, FileName: record.SourceName,
			TransferredBytes: update.TransferredBytes, TotalBytes: update.TotalBytes,
			ResumedBytes: update.ResumedBytes,
		})
	}
}

func (s *Service) emitJobProgress(job TransferJob, phase, message string) {
	s.mu.Lock()
	handler := s.progress
	s.mu.Unlock()
	if handler != nil {
		handler(TransferProgress{
			JobID: job.ID, Phase: phase, FileName: job.SourceName,
			TransferredBytes: job.TransferredBytes, TotalBytes: job.TotalBytes,
			ResumedBytes: job.ResumedBytes, Message: message,
		})
	}
}

func (s *Service) queuedJobsLocked() int {
	queued := 0
	for _, job := range s.jobs {
		if job.record.Job.Status == JobQueued {
			queued++
		}
	}
	return queued
}

func (s *Service) removeJobOrderLocked(id string) {
	order := s.jobOrder[:0]
	for _, candidate := range s.jobOrder {
		if candidate != id {
			order = append(order, candidate)
		}
	}
	s.jobOrder = order
}

func terminalJobStatus(status string) bool {
	return status == JobVerified || status == JobInterrupted || status == JobFailed || status == JobCanceled
}

func ctxCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
