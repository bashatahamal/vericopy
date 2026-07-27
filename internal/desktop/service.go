// Package desktop exposes the desktop application's safe, UI-oriented service
// boundary. It deliberately shares the parsing, SSH, SFTP, transfer, and
// verification packages used by the supporting command adapter.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bashatahamal/vericopy/internal/access"
	nativesftp "github.com/bashatahamal/vericopy/internal/backend/sftp"
	"github.com/bashatahamal/vericopy/internal/credentialstore"
	"github.com/bashatahamal/vericopy/internal/localpath"
	"github.com/bashatahamal/vericopy/internal/permissions"
	"github.com/bashatahamal/vericopy/internal/remote"
	"github.com/bashatahamal/vericopy/internal/remotehash"
	"github.com/bashatahamal/vericopy/internal/sshclient"
	"github.com/bashatahamal/vericopy/internal/transfer"
	"github.com/bashatahamal/vericopy/internal/verrors"
	"github.com/bashatahamal/vericopy/internal/version"
	"github.com/bashatahamal/vericopy/internal/wakelock"
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
	wakeLock     wakelock.Release
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

// transferDirectionDownload marks a TransferRequest whose Source is remote
// and Destination is local. Any other value (including the empty default)
// is treated as an upload, for backward compatibility with saved sessions
// and history predating downloads.
const transferDirectionDownload = "download"

// TransferRequest is the desktop equivalent of an explicit copy command.
// Password is accepted only for a live password-authenticated transfer. It is
// excluded from reviews, saved sessions, history, progress, and diagnostics.
type TransferRequest struct {
	Source             string `json:"source"`
	Destination        string `json:"destination"`
	// Direction is "upload" (the default, for backward compatibility) or
	// "download". Upload requires Source to be a local path and Destination
	// to be a remote user@host:/path; download requires the reverse.
	Direction          string `json:"direction,omitempty"`
	Authentication     string `json:"authentication,omitempty"`
	Password           string `json:"password,omitempty"`
	Identity           string `json:"identity,omitempty"`
	KnownHosts         string `json:"known_hosts,omitempty"`
	Port               int    `json:"port"`
	Permissions        string `json:"permissions,omitempty"`
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
	Source             SourceSummary      `json:"source"`
	Destination        DestinationSummary `json:"destination"`
	Authentication     string             `json:"authentication"`
	Permissions        string             `json:"permissions"`
	KnownHosts         string             `json:"known_hosts"`
	Resume             bool               `json:"resume"`
	Overwrite          bool               `json:"overwrite"`
	NoClobber          bool               `json:"no_clobber"`
	PreserveTime       bool               `json:"preserve_time"`
	FixMediaNames      bool               `json:"fix_media_names"`
	GenerateThumbnails bool               `json:"generate_thumbnails"`
	ReadableBy         string             `json:"readable_by,omitempty"`
}

// TransferResult combines the machine-readable engine result with the concise
// completion text shown by the desktop application.
type TransferResult struct {
	Result  transfer.Result `json:"result"`
	Summary string          `json:"summary"`
}

// DestinationPreviewEntry is one remote directory entry shown during review.
type DestinationPreviewEntry struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// DestinationPreview reports what a real, authenticated connection found at
// the reviewed destination. Unlike ReviewTransfer, this dials SSH and reads
// remote metadata; it never writes anything.
type DestinationPreview struct {
	Path        string                    `json:"path"`
	Exists      bool                      `json:"exists"`
	IsDirectory bool                      `json:"is_directory"`
	WillCreate  bool                      `json:"will_create"`
	Entries     []DestinationPreviewEntry `json:"entries"`
}

// PreviewDestination connects using the reviewed request's authentication
// and lists what is actually at the destination, or its parent directory
// when the destination itself does not exist yet. A successful call is
// itself proof the authentication and connection details work, ahead of
// queuing the transfer. It never writes to the destination.
func (s *Service) PreviewDestination(request TransferRequest) (DestinationPreview, error) {
	password := request.Password
	request.Password = ""
	prepared, err := prepare(request)
	if err != nil {
		return DestinationPreview{}, err
	}
	if prepared.request.Authentication == sshclient.AuthenticationPassword && password == "" {
		return DestinationPreview{}, verrors.New(verrors.CodeAuthenticationFailed,
			"enter the SSH password before previewing the destination")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sshConnection, err := sshclient.Dial(ctx, sshclient.Options{
		User: prepared.remote.User, Host: prepared.remote.Host, Port: prepared.request.Port,
		KnownHosts: prepared.request.KnownHosts, Identity: prepared.request.Identity,
		Authentication: prepared.request.Authentication, Password: password, Timeout: 15 * time.Second,
	})
	password = ""
	if err != nil {
		return DestinationPreview{}, err
	}
	defer sshConnection.Close()
	remoteFS, err := nativesftp.New(sshConnection)
	if err != nil {
		return DestinationPreview{}, verrors.Wrap(verrors.CodeConnectionFailed, "the server did not accept the SFTP subsystem", err)
	}
	defer remoteFS.Close()

	destinationPath := prepared.remote.Path
	preview := DestinationPreview{}
	listPath := destinationPath
	if info, statErr := remoteFS.Lstat(destinationPath); statErr == nil {
		preview.Exists = true
		preview.IsDirectory = info.IsDir()
		if !info.IsDir() {
			listPath = path.Dir(destinationPath)
		}
	} else {
		preview.WillCreate = true
		listPath = path.Dir(destinationPath)
	}

	entries, err := remoteFS.ReadDir(listPath)
	if err != nil {
		return DestinationPreview{}, verrors.Wrap(verrors.CodeDestinationNotReadable,
			fmt.Sprintf("could not list %q", listPath), err)
	}
	preview.Path = listPath
	for _, entry := range entries {
		preview.Entries = append(preview.Entries, DestinationPreviewEntry{
			Name: entry.Name(), IsDir: entry.IsDir(), Size: entry.Size(), ModTime: entry.ModTime(),
		})
	}
	sort.Slice(preview.Entries, func(i, j int) bool {
		if preview.Entries[i].IsDir != preview.Entries[j].IsDir {
			return preview.Entries[i].IsDir
		}
		return preview.Entries[i].Name < preview.Entries[j].Name
	})
	return preview, nil
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

// preparedTransfer is direction-agnostic: local is the local-side path
// (the source for an upload, the destination for a download) and remote is
// the parsed remote endpoint (the destination for an upload, the source for
// a download).
type preparedTransfer struct {
	request TransferRequest
	local   string
	remote  remote.Destination
	policy  permissions.Policy
	review  TransferReview
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
	if request.Overwrite && request.NoClobber {
		return preparedTransfer{}, verrors.New(verrors.CodeInvalidArguments, "overwrite and no-clobber cannot be used together")
	}
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
	policy, err := permissions.Resolve(request.Permissions, "", "")
	if err != nil {
		return preparedTransfer{}, err
	}

	if request.Direction == transferDirectionDownload {
		return prepareDownload(request, policy)
	}
	return prepareUpload(request, policy)
}

func prepareUpload(request TransferRequest, policy permissions.Policy) (preparedTransfer, error) {
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

	review := TransferReview{
		Source: SourceSummary{
			Path: source, Kind: string(localpath.KindRelative), Size: sourceInfo.Size(), IsDirectory: sourceInfo.IsDir(),
		},
		Destination:    DestinationSummary{User: destination.User, Host: destination.Host, Path: destination.Path, Port: request.Port},
		Authentication: request.Authentication, Permissions: request.Permissions, KnownHosts: request.KnownHosts, Resume: request.Resume,
		Overwrite: request.Overwrite, NoClobber: request.NoClobber, PreserveTime: request.PreserveTime, ReadableBy: request.ReadableBy,
		FixMediaNames: request.FixMediaNames, GenerateThumbnails: request.GenerateThumbnails,
	}
	info, inspectErr := localpath.Inspect(request.Source, "")
	if inspectErr == nil {
		review.Source.Kind = string(info.Kind)
	}
	return preparedTransfer{request: request, local: source, remote: destination, policy: policy, review: review}, nil
}

// prepareDownload mirrors prepareUpload with Source and Destination
// reversed. Unlike an upload's remote destination, the remote source cannot
// be inspected here: doing so would require dialing SSH during local
// validation, which prepare intentionally never does. Callers that already
// know the source is a directory (Browse lists remote entries before
// offering to download them) pass that through Recursive explicitly.
func prepareDownload(request TransferRequest, policy permissions.Policy) (preparedTransfer, error) {
	source, err := remote.Parse(request.Source)
	if err != nil {
		return preparedTransfer{}, err
	}
	if source.User == "" {
		return preparedTransfer{}, verrors.New(verrors.CodeInvalidArguments,
			"the desktop app requires an explicit SSH user in the source").WithHint(
			"Use user@host:/absolute/remote/path so the account is clear before transfer.")
	}
	if !strings.HasPrefix(source.Path, "/") {
		return preparedTransfer{}, verrors.New(verrors.CodeInvalidArguments,
			"the desktop app requires an absolute remote path").WithHint(
			"Use user@host:/absolute/remote/path so the source is unambiguous.")
	}

	destination, err := localpath.ResolveForRuntime(request.Destination)
	if err != nil {
		return preparedTransfer{}, err
	}
	if destinationInfo, statErr := os.Lstat(destination); statErr == nil {
		if destinationInfo.Mode()&os.ModeSymlink != 0 {
			return preparedTransfer{}, verrors.New(verrors.CodeUnsupportedFileType, "symbolic links are not followed by default")
		}
		if !destinationInfo.IsDir() && !destinationInfo.Mode().IsRegular() {
			return preparedTransfer{}, verrors.New(verrors.CodeUnsupportedFileType, "only regular files and directories are supported")
		}
	} else if !os.IsNotExist(statErr) {
		return preparedTransfer{}, verrors.Wrap(verrors.CodeInvalidLocalPath, "the local destination cannot be inspected", statErr)
	}

	review := TransferReview{
		Source:         SourceSummary{Path: source.Path, Kind: "remote", IsDirectory: request.Recursive},
		Destination:    DestinationSummary{Path: destination, Port: request.Port},
		Authentication: request.Authentication, Permissions: request.Permissions, KnownHosts: request.KnownHosts, Resume: request.Resume,
		Overwrite: request.Overwrite, NoClobber: request.NoClobber, PreserveTime: request.PreserveTime,
		FixMediaNames: request.FixMediaNames, GenerateThumbnails: request.GenerateThumbnails,
	}
	return preparedTransfer{request: request, local: destination, remote: source, policy: policy, review: review}, nil
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
	s.emitProgress(TransferProgress{Phase: "connecting", FileName: filepath.Base(prepared.local), Message: "Connecting with strict host verification"})
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
		User: prepared.remote.User, Host: prepared.remote.Host, Port: prepared.request.Port,
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

	engine := transfer.Engine{Remote: remoteFS, Hasher: remotehash.Hasher{Runner: access.SSHRunner{Client: sshConnection.Client}}}
	options := transfer.Options{
		Recursive: prepared.request.Recursive, Resume: prepared.request.Resume,
		Overwrite: prepared.request.Overwrite, NoClobber: prepared.request.NoClobber, PreserveTime: prepared.request.PreserveTime,
		FixMediaNames: prepared.request.FixMediaNames, GenerateThumbnails: prepared.request.GenerateThumbnails,
		Policy: prepared.policy, Progress: progress,
	}

	if prepared.request.Direction == transferDirectionDownload {
		return engine.Download(ctx, prepared.remote.Path, prepared.local, options)
	}

	resolver := access.Resolver{Runner: access.SSHRunner{Client: sshConnection.Client}}
	if prepared.request.Group != "" {
		resolved, resolveErr := resolver.Group(ctx, prepared.request.Group)
		if resolveErr != nil {
			return transfer.Result{}, resolveErr
		}
		options.GID = &resolved
	}
	result, err := engine.Copy(ctx, prepared.local, prepared.remote.Path, options)
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
// Password is never written to the state file: when RememberPassword is set
// and a password was provided, it is stored in the OS credential store
// instead, keyed by the session name. Clearing RememberPassword removes any
// previously stored password for this session.
func (s *Service) SaveSession(session SessionProfile) (SessionProfile, error) {
	password := session.Password
	session.Password = ""
	saved, err := s.state.SaveSession(session)
	if err != nil {
		return SessionProfile{}, err
	}
	if session.RememberPassword && password != "" {
		if err := credentialstore.Save(saved.Name, password); err != nil {
			return SessionProfile{}, verrors.Wrap(verrors.CodeInvalidArguments, "could not store the password securely", err)
		}
	} else if !session.RememberPassword {
		_ = credentialstore.Delete(saved.Name)
	}
	return saved, nil
}

// LoadSessionPassword returns a previously stored password for a saved
// session, for pre-filling the password field when RememberPassword was set.
func (s *Service) LoadSessionPassword(name string) (string, error) {
	password, err := credentialstore.Load(name)
	if err != nil {
		if errors.Is(err, credentialstore.ErrNotFound) {
			return "", nil
		}
		return "", verrors.Wrap(verrors.CodeInvalidArguments, "could not read the stored password", err)
	}
	return password, nil
}

// DeleteSession removes one local transfer session by its unique name, and
// any password stored for it.
func (s *Service) DeleteSession(name string) (bool, error) {
	_ = credentialstore.Delete(name)
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
		StartedAt: startedAt, CompletedAt: time.Now().UTC(), SourceName: filepath.Base(prepared.local),
		Destination: redactedDestination(prepared.remote), Files: result.Files, Bytes: result.Bytes,
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
	summary := fmt.Sprintf("Verified %d file(s), %d bytes\nSHA-256: %s", result.Files, result.Bytes, result.SHA256)
	if result.SkippedFiles > 0 {
		summary += fmt.Sprintf("\nSkipped %d file(s) that already existed", result.SkippedFiles)
	}
	return summary
}
