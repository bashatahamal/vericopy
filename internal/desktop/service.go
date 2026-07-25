// Package desktop exposes the desktop application's safe, UI-oriented service
// boundary. It deliberately shares the parsing, SSH, SFTP, transfer, and
// verification packages used by the supporting command adapter.
package desktop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bashatahamal/vericopy/internal/access"
	nativesftp "github.com/bashatahamal/vericopy/internal/backend/sftp"
	"github.com/bashatahamal/vericopy/internal/localpath"
	"github.com/bashatahamal/vericopy/internal/permissions"
	"github.com/bashatahamal/vericopy/internal/remote"
	"github.com/bashatahamal/vericopy/internal/remotehash"
	"github.com/bashatahamal/vericopy/internal/sshclient"
	"github.com/bashatahamal/vericopy/internal/transfer"
	"github.com/bashatahamal/vericopy/internal/verrors"
	"github.com/bashatahamal/vericopy/internal/version"
)

// Service coordinates desktop requests without making the frontend responsible
// for any security-sensitive decision.
type Service struct {
	mu           sync.Mutex
	root         context.Context
	cancel       context.CancelFunc
	active       bool
	state        *StateStore
	progress     func(TransferProgress)
	lastProgress time.Time
	jobs         map[string]*runtimeJob
	jobOrder     []string
	runningJobs  int
	maxJobs      int
	jobProgress  map[string]time.Time
	jobWG        sync.WaitGroup
	executor     transferExecutor
	closed       bool
}

// NewService creates the desktop service boundary.
func NewService() *Service {
	statePath, err := DefaultStatePath()
	if err != nil {
		statePath = ""
	}
	return NewServiceWithStatePath(statePath)
}

// NewServiceWithStatePath creates a desktop service using an explicit local
// state path. It is useful for isolated desktop tests and does not change any
// transfer security behavior.
func NewServiceWithStatePath(statePath string) *Service {
	service := &Service{
		root: context.Background(), state: newStateStore(statePath), jobs: make(map[string]*runtimeJob),
		maxJobs: defaultConcurrentJobs, jobProgress: make(map[string]time.Time),
	}
	service.restoreJobs()
	return service
}

// SetContext connects the service lifetime to the native application lifetime.
func (s *Service) SetContext(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.root = ctx
}

// SetProgressHandler connects truthful engine progress to the native shell.
// The handler receives state only; it cannot alter a transfer.
func (s *Service) SetProgressHandler(handler func(TransferProgress)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.progress = handler
}

// Dashboard provides non-secret local readiness information for the home view.
type Dashboard struct {
	Version             string `json:"version"`
	Platform            string `json:"platform"`
	KnownHostsPath      string `json:"known_hosts_path"`
	StrictHostKeysReady bool   `json:"strict_host_keys_ready"`
	SSHAgentAvailable   bool   `json:"ssh_agent_available"`
	TransferActive      bool   `json:"transfer_active"`
	RunningTransfers    int    `json:"running_transfers"`
	QueuedTransfers     int    `json:"queued_transfers"`
}

// GetDashboard returns current desktop readiness without opening a connection.
func (s *Service) GetDashboard() Dashboard {
	knownHosts := sshclient.DefaultKnownHosts()
	_, knownHostsErr := sshclient.NewHostKeyCallback(knownHosts)

	s.mu.Lock()
	running, queued := s.runningJobs, s.queuedJobsLocked()
	active := s.active || running > 0 || queued > 0
	s.mu.Unlock()

	build := version.Current()
	return Dashboard{
		Version:             build.Version,
		Platform:            build.OS + "/" + build.Arch,
		KnownHostsPath:      knownHosts,
		StrictHostKeysReady: knownHostsErr == nil,
		SSHAgentAvailable:   os.Getenv("SSH_AUTH_SOCK") != "",
		TransferActive:      active,
		RunningTransfers:    running,
		QueuedTransfers:     queued,
	}
}

// TransferRequest is the desktop equivalent of an explicit copy command.
// Password is accepted only for a live password-authenticated transfer. It is
// excluded from reviews, saved sessions, history, progress, and diagnostics.
type TransferRequest struct {
	Source         string `json:"source"`
	Destination    string `json:"destination"`
	Authentication string `json:"authentication,omitempty"`
	Password       string `json:"password,omitempty"`
	Identity       string `json:"identity,omitempty"`
	KnownHosts     string `json:"known_hosts,omitempty"`
	Port           int    `json:"port"`
	Permissions    string `json:"permissions,omitempty"`
	Group          string `json:"group,omitempty"`
	ReadableBy     string `json:"readable_by,omitempty"`
	Recursive      bool   `json:"recursive"`
	Resume         bool   `json:"resume"`
	Overwrite      bool   `json:"overwrite"`
	PreserveTime   bool   `json:"preserve_time"`
}

// SourceSummary is the source information shown before any remote connection.
type SourceSummary struct {
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	Size        int64  `json:"size"`
	IsDirectory bool   `json:"is_directory"`
}

// DestinationSummary is the parsed remote target shown for review.
type DestinationSummary struct {
	User string `json:"user"`
	Host string `json:"host"`
	Path string `json:"path"`
	Port int    `json:"port"`
}

// TransferReview is a locally validated transfer plan. Review never dials SSH
// or writes a destination.
type TransferReview struct {
	Source         SourceSummary      `json:"source"`
	Destination    DestinationSummary `json:"destination"`
	Authentication string             `json:"authentication"`
	Permissions    string             `json:"permissions"`
	KnownHosts     string             `json:"known_hosts"`
	Resume         bool               `json:"resume"`
	Overwrite      bool               `json:"overwrite"`
	PreserveTime   bool               `json:"preserve_time"`
	ReadableBy     string             `json:"readable_by,omitempty"`
}

// TransferResult combines the machine-readable engine result with the concise
// completion text shown by the desktop application.
type TransferResult struct {
	Result  transfer.Result `json:"result"`
	Summary string          `json:"summary"`
}

// TransferProgress is a per-file update for the desktop UI. Directory
// transfers intentionally report the active file, not an invented total.
type TransferProgress struct {
	JobID            string `json:"job_id,omitempty"`
	Phase            string `json:"phase"`
	FileName         string `json:"file_name,omitempty"`
	TransferredBytes int64  `json:"transferred_bytes,omitempty"`
	TotalBytes       int64  `json:"total_bytes,omitempty"`
	ResumedBytes     int64  `json:"resumed_bytes,omitempty"`
	Message          string `json:"message,omitempty"`
}

type preparedTransfer struct {
	request     TransferRequest
	source      string
	destination remote.Destination
	policy      permissions.Policy
	review      TransferReview
}

// ReviewTransfer validates a request and returns the exact operation the user
// is about to start. The desktop UI cannot bypass this check.
func (s *Service) ReviewTransfer(request TransferRequest) (TransferReview, error) {
	prepared, err := prepare(request)
	if err != nil {
		return TransferReview{}, err
	}
	return prepared.review, nil
}

func prepare(request TransferRequest) (preparedTransfer, error) {
	if request.Port == 0 {
		request.Port = 22
	}
	if request.Port < 1 || request.Port > 65535 {
		return preparedTransfer{}, verrors.New(verrors.CodeInvalidArguments, "SSH port must be between 1 and 65535")
	}
	if request.KnownHosts == "" {
		request.KnownHosts = sshclient.DefaultKnownHosts()
	}
	if request.Permissions == "" {
		request.Permissions = "private"
	}
	if request.Authentication == "" {
		request.Authentication = sshclient.AuthenticationKey
	}
	switch request.Authentication {
	case sshclient.AuthenticationKey:
		request.Password = ""
	case sshclient.AuthenticationPassword:
		request.Identity = ""
		request.Password = ""
	default:
		return preparedTransfer{}, verrors.New(verrors.CodeInvalidArguments,
			fmt.Sprintf("unsupported SSH authentication method %q", request.Authentication))
	}

	source, err := localpath.ResolveForRuntime(request.Source)
	if err != nil {
		return preparedTransfer{}, err
	}
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return preparedTransfer{}, verrors.Wrap(verrors.CodeInvalidLocalPath, "the source cannot be inspected", err)
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return preparedTransfer{}, verrors.New(verrors.CodeUnsupportedFileType, "symbolic links are not followed by default")
	}
	if !sourceInfo.IsDir() && !sourceInfo.Mode().IsRegular() {
		return preparedTransfer{}, verrors.New(verrors.CodeUnsupportedFileType, "only regular files and directories are supported")
	}
	if sourceInfo.IsDir() && !request.Recursive {
		return preparedTransfer{}, verrors.New(verrors.CodeInvalidArguments, "the source is a directory; enable recursive transfer")
	}

	destination, err := remote.Parse(request.Destination)
	if err != nil {
		return preparedTransfer{}, err
	}
	if destination.User == "" {
		return preparedTransfer{}, verrors.New(verrors.CodeInvalidArguments,
			"the desktop app requires an explicit SSH user in the destination").WithHint(
			"Use user@host:/absolute/remote/path so the account is clear before transfer.")
	}
	if !strings.HasPrefix(destination.Path, "/") {
		return preparedTransfer{}, verrors.New(verrors.CodeInvalidArguments,
			"the desktop app requires an absolute remote path").WithHint(
			"Use user@host:/absolute/remote/path so the destination is unambiguous.")
	}
	policy, err := permissions.Resolve(request.Permissions, "", "")
	if err != nil {
		return preparedTransfer{}, err
	}

	review := TransferReview{
		Source: SourceSummary{
			Path: source, Kind: string(localpath.KindRelative), Size: sourceInfo.Size(), IsDirectory: sourceInfo.IsDir(),
		},
		Destination:    DestinationSummary{User: destination.User, Host: destination.Host, Path: destination.Path, Port: request.Port},
		Authentication: request.Authentication, Permissions: request.Permissions, KnownHosts: request.KnownHosts, Resume: request.Resume,
		Overwrite: request.Overwrite, PreserveTime: request.PreserveTime, ReadableBy: request.ReadableBy,
	}
	info, inspectErr := localpath.Inspect(request.Source, "")
	if inspectErr == nil {
		review.Source.Kind = string(info.Kind)
	}
	return preparedTransfer{request: request, source: source, destination: destination, policy: policy, review: review}, nil
}

// StartTransfer executes the previously reviewable operation through the same
// strict SSH and native SFTP implementation used by every local interface.
func (s *Service) StartTransfer(request TransferRequest) (transfer.Result, error) {
	password := request.Password
	request.Password = ""
	prepared, err := prepare(request)
	if err != nil {
		return transfer.Result{}, err
	}
	if prepared.request.Authentication == sshclient.AuthenticationPassword && password == "" {
		return transfer.Result{}, verrors.New(verrors.CodeAuthenticationFailed,
			"enter the SSH password before starting the transfer")
	}
	startedAt := time.Now().UTC()
	s.emitProgress(TransferProgress{Phase: "connecting", FileName: filepath.Base(prepared.source), Message: "Connecting with strict host verification"})
	ctx, finish, err := s.beginTransfer()
	if err != nil {
		return transfer.Result{}, err
	}
	defer finish()

	result, transferErr := s.execute(ctx, prepared, password, s.transferProgress)
	password = ""
	return s.completeTransfer(prepared, startedAt, result, transferErr)
}

// execute performs one prepared transfer without owning queue or history state.
// The queue and the compatibility StartTransfer method both use this boundary.
func (s *Service) execute(ctx context.Context, prepared preparedTransfer, password string, progress func(transfer.Progress)) (transfer.Result, error) {
	s.mu.Lock()
	executor := s.executor
	s.mu.Unlock()
	if executor != nil {
		return executor(ctx, prepared, password, progress)
	}
	return s.executePrepared(ctx, prepared, password, progress)
}

func (s *Service) executePrepared(ctx context.Context, prepared preparedTransfer, password string, progress func(transfer.Progress)) (transfer.Result, error) {
	sshConnection, err := sshclient.Dial(ctx, sshclient.Options{
		User: prepared.destination.User, Host: prepared.destination.Host, Port: prepared.request.Port,
		KnownHosts: prepared.request.KnownHosts, Identity: prepared.request.Identity,
		Authentication: prepared.request.Authentication, Password: password, Timeout: 15 * time.Second,
	})
	password = ""
	if err != nil {
		return transfer.Result{}, err
	}
	defer sshConnection.Close()
	remoteFS, err := nativesftp.New(sshConnection)
	if err != nil {
		return transfer.Result{}, verrors.Wrap(verrors.CodeConnectionFailed, "the server did not accept the SFTP subsystem", err)
	}
	defer remoteFS.Close()

	resolver := access.Resolver{Runner: access.SSHRunner{Client: sshConnection.Client}}
	var gid *int
	if prepared.request.Group != "" {
		resolved, resolveErr := resolver.Group(ctx, prepared.request.Group)
		if resolveErr != nil {
			return transfer.Result{}, resolveErr
		}
		gid = &resolved
	}

	engine := transfer.Engine{Remote: remoteFS, Hasher: remotehash.Hasher{Runner: access.SSHRunner{Client: sshConnection.Client}}}
	result, err := engine.Copy(ctx, prepared.source, prepared.destination.Path, transfer.Options{
		Recursive: prepared.request.Recursive, Resume: prepared.request.Resume,
		Overwrite: prepared.request.Overwrite, PreserveTime: prepared.request.PreserveTime,
		Policy: prepared.policy, GID: gid, Progress: progress,
	})
	if err != nil {
		return result, err
	}
	if prepared.request.ReadableBy != "" {
		identity, resolveErr := resolver.User(ctx, prepared.request.ReadableBy)
		if resolveErr != nil {
			return result, resolveErr
		}
		if _, checkErr := access.Check(ctx, remoteFS, result.Destination, identity); checkErr != nil {
			return result, checkErr
		}
	}
	return result, nil
}

// ListProfiles returns saved non-secret connection references.
//
// Deprecated: retained for one-time migration to ListSessions.
func (s *Service) ListProfiles() ([]ConnectionProfile, error) {
	return s.state.ListProfiles()
}

// SaveProfile creates or updates a saved non-secret connection reference.
//
// Deprecated: retained for one-time migration to SaveSession.
func (s *Service) SaveProfile(profile ConnectionProfile) (ConnectionProfile, error) {
	return s.state.SaveProfile(profile)
}

// DeleteProfile removes one saved connection reference.
//
// Deprecated: retained for one-time migration to DeleteSession.
func (s *Service) DeleteProfile(id string) (bool, error) {
	return s.state.DeleteProfile(id)
}

// ListSessions returns complete local transfer sessions from the Go state store.
func (s *Service) ListSessions() ([]SessionProfile, error) {
	return s.state.ListSessions()
}

// SaveSession creates or replaces one complete local transfer session.
func (s *Service) SaveSession(session SessionProfile) (SessionProfile, error) {
	return s.state.SaveSession(session)
}

// DeleteSession removes one local transfer session by its unique name.
func (s *Service) DeleteSession(name string) (bool, error) {
	return s.state.DeleteSession(name)
}

// ListTransferHistory returns redacted local transfer records.
func (s *Service) ListTransferHistory() ([]TransferHistoryEntry, error) {
	return s.state.ListTransferHistory()
}

// ClearTransferHistory removes all local redacted transfer records.
func (s *Service) ClearTransferHistory() error {
	return s.state.ClearTransferHistory()
}

func (s *Service) completeTransfer(prepared preparedTransfer, startedAt time.Time, result transfer.Result, transferErr error) (transfer.Result, error) {
	entry := TransferHistoryEntry{
		StartedAt: startedAt, CompletedAt: time.Now().UTC(), SourceName: filepath.Base(prepared.source),
		Destination: redactedDestination(prepared.destination), Files: result.Files, Bytes: result.Bytes,
		ResumedBytes: result.ResumedBytes, Verified: result.Verified,
	}
	if entry.SourceName == "." || entry.SourceName == string(filepath.Separator) {
		entry.SourceName = "source"
	}
	if transferErr == nil {
		entry.Status = "verified"
		s.emitProgress(TransferProgress{Phase: "completed", FileName: entry.SourceName, TransferredBytes: result.Bytes, TotalBytes: result.Bytes, Message: "Transfer verified"})
	} else {
		diagnostic := verrors.As(transferErr)
		entry.DiagnosticCode = diagnostic.Code
		entry.Status = "failed"
		if diagnostic.Code == verrors.CodeTransferInterrupted {
			entry.Status = "interrupted"
		}
		s.emitProgress(TransferProgress{Phase: entry.Status, FileName: entry.SourceName, Message: diagnostic.Message})
	}
	_ = s.state.recordHistory(entry)
	return result, transferErr
}

func (s *Service) transferProgress(update transfer.Progress) {
	s.emitProgress(TransferProgress{
		Phase: update.Phase, FileName: filepath.Base(update.Source), TransferredBytes: update.TransferredBytes,
		TotalBytes: update.TotalBytes, ResumedBytes: update.ResumedBytes,
	})
}

func (s *Service) emitProgress(update TransferProgress) {
	s.mu.Lock()
	handler := s.progress
	if handler == nil {
		s.mu.Unlock()
		return
	}
	now := time.Now()
	if update.Phase == "uploading" && update.TotalBytes > 0 && update.TransferredBytes < update.TotalBytes && now.Sub(s.lastProgress) < 100*time.Millisecond {
		s.mu.Unlock()
		return
	}
	s.lastProgress = now
	s.mu.Unlock()
	handler(update)
}

func (s *Service) beginTransfer() (context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active || s.runningJobs > 0 {
		return nil, nil, verrors.New(verrors.CodeTransferFailed, "another transfer is already active")
	}
	root := s.root
	if root == nil {
		root = context.Background()
	}
	ctx, cancel := context.WithCancel(root)
	s.active, s.cancel = true, cancel
	return ctx, func() {
		cancel()
		s.mu.Lock()
		s.active, s.cancel = false, nil
		s.mu.Unlock()
	}, nil
}

// CancelTransfer requests interruption of the current transfer. Compatible
// partial state remains available for a later resume.
func (s *Service) CancelTransfer() bool {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// Close releases an active transfer when the desktop application shuts down.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	legacyCancel := s.cancel
	cancels := make([]context.CancelFunc, 0, len(s.jobs))
	for _, job := range s.jobs {
		job.password = ""
		if job.cancel != nil {
			cancels = append(cancels, job.cancel)
		}
	}
	s.mu.Unlock()
	if legacyCancel != nil {
		legacyCancel()
	}
	for _, cancel := range cancels {
		cancel()
	}
	s.jobWG.Wait()
}

// FormatResult is deliberately small so the UI does not have to reconstruct a
// human outcome from fields alone.
func FormatResult(result transfer.Result) string {
	return fmt.Sprintf("Verified %d file(s), %d bytes\nSHA-256: %s", result.Files, result.Bytes, result.SHA256)
}
