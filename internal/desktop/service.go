// Package desktop exposes the desktop application's safe, UI-oriented service
// boundary. It deliberately shares the parsing, SSH, SFTP, transfer, and
// verification packages used by the CLI.
package desktop

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/bashatahamal/vericopy/internal/access"
	nativesftp "github.com/bashatahamal/vericopy/internal/backend/sftp"
	"github.com/bashatahamal/vericopy/internal/localpath"
	"github.com/bashatahamal/vericopy/internal/permissions"
	"github.com/bashatahamal/vericopy/internal/remote"
	"github.com/bashatahamal/vericopy/internal/sshclient"
	"github.com/bashatahamal/vericopy/internal/transfer"
	"github.com/bashatahamal/vericopy/internal/verrors"
	"github.com/bashatahamal/vericopy/internal/version"
)

// Service coordinates desktop requests without making the frontend responsible
// for any security-sensitive decision.
type Service struct {
	mu     sync.Mutex
	root   context.Context
	cancel context.CancelFunc
	active bool
}

// NewService creates the desktop service boundary.
func NewService() *Service {
	return &Service{root: context.Background()}
}

// SetContext connects the service lifetime to the native application lifetime.
func (s *Service) SetContext(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.root = ctx
}

// Dashboard provides non-secret local readiness information for the home view.
type Dashboard struct {
	Version             string `json:"version"`
	Platform            string `json:"platform"`
	KnownHostsPath      string `json:"known_hosts_path"`
	StrictHostKeysReady bool   `json:"strict_host_keys_ready"`
	SSHAgentAvailable   bool   `json:"ssh_agent_available"`
	TransferActive      bool   `json:"transfer_active"`
}

// GetDashboard returns current desktop readiness without opening a connection.
func (s *Service) GetDashboard() Dashboard {
	knownHosts := sshclient.DefaultKnownHosts()
	_, knownHostsErr := sshclient.NewHostKeyCallback(knownHosts)

	s.mu.Lock()
	active := s.active
	s.mu.Unlock()

	build := version.Current()
	return Dashboard{
		Version:             build.Version,
		Platform:            build.OS + "/" + build.Arch,
		KnownHostsPath:      knownHosts,
		StrictHostKeysReady: knownHostsErr == nil,
		SSHAgentAvailable:   os.Getenv("SSH_AUTH_SOCK") != "",
		TransferActive:      active,
	}
}

// TransferRequest is the desktop equivalent of an explicit copy command. It
// contains paths and policy references only; it never accepts a password.
type TransferRequest struct {
	Source       string `json:"source"`
	Destination  string `json:"destination"`
	Identity     string `json:"identity,omitempty"`
	KnownHosts   string `json:"known_hosts,omitempty"`
	Port         int    `json:"port"`
	Permissions  string `json:"permissions,omitempty"`
	Group        string `json:"group,omitempty"`
	ReadableBy   string `json:"readable_by,omitempty"`
	Recursive    bool   `json:"recursive"`
	Resume       bool   `json:"resume"`
	Overwrite    bool   `json:"overwrite"`
	PreserveTime bool   `json:"preserve_time"`
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
	Source       SourceSummary      `json:"source"`
	Destination  DestinationSummary `json:"destination"`
	Permissions  string             `json:"permissions"`
	KnownHosts   string             `json:"known_hosts"`
	Resume       bool               `json:"resume"`
	Overwrite    bool               `json:"overwrite"`
	PreserveTime bool               `json:"preserve_time"`
	ReadableBy   string             `json:"readable_by,omitempty"`
}

// TransferResult combines the machine-readable engine result with the concise
// completion text shown by the desktop application.
type TransferResult struct {
	Result  transfer.Result `json:"result"`
	Summary string          `json:"summary"`
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
	policy, err := permissions.Resolve(request.Permissions, "", "")
	if err != nil {
		return preparedTransfer{}, err
	}

	review := TransferReview{
		Source: SourceSummary{
			Path: source, Kind: string(localpath.KindRelative), Size: sourceInfo.Size(), IsDirectory: sourceInfo.IsDir(),
		},
		Destination: DestinationSummary{User: destination.User, Host: destination.Host, Path: destination.Path, Port: request.Port},
		Permissions: request.Permissions, KnownHosts: request.KnownHosts, Resume: request.Resume,
		Overwrite: request.Overwrite, PreserveTime: request.PreserveTime, ReadableBy: request.ReadableBy,
	}
	info, inspectErr := localpath.Inspect(request.Source, "")
	if inspectErr == nil {
		review.Source.Kind = string(info.Kind)
	}
	return preparedTransfer{request: request, source: source, destination: destination, policy: policy, review: review}, nil
}

// StartTransfer executes the previously reviewable operation through the same
// strict SSH and native SFTP implementation used by the CLI.
func (s *Service) StartTransfer(request TransferRequest) (transfer.Result, error) {
	prepared, err := prepare(request)
	if err != nil {
		return transfer.Result{}, err
	}
	ctx, finish, err := s.beginTransfer()
	if err != nil {
		return transfer.Result{}, err
	}
	defer finish()

	sshConnection, err := sshclient.Dial(ctx, sshclient.Options{
		User: prepared.destination.User, Host: prepared.destination.Host, Port: prepared.request.Port,
		KnownHosts: prepared.request.KnownHosts, Identity: prepared.request.Identity, Timeout: 15 * time.Second,
	})
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

	result, err := (transfer.Engine{Remote: remoteFS}).Copy(ctx, prepared.source, prepared.destination.Path, transfer.Options{
		Recursive: prepared.request.Recursive, Resume: prepared.request.Resume,
		Overwrite: prepared.request.Overwrite, PreserveTime: prepared.request.PreserveTime,
		Policy: prepared.policy, GID: gid,
	})
	if err != nil {
		return transfer.Result{}, err
	}
	if prepared.request.ReadableBy != "" {
		identity, resolveErr := resolver.User(ctx, prepared.request.ReadableBy)
		if resolveErr != nil {
			return transfer.Result{}, resolveErr
		}
		if _, checkErr := access.Check(ctx, remoteFS, result.Destination, identity); checkErr != nil {
			return transfer.Result{}, checkErr
		}
	}
	return result, nil
}

func (s *Service) beginTransfer() (context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active {
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
	_ = s.CancelTransfer()
}

// FormatResult is deliberately small so the UI does not have to reconstruct a
// human outcome from fields alone.
func FormatResult(result transfer.Result) string {
	return fmt.Sprintf("Verified %d file(s), %d bytes\nSHA-256: %s", result.Files, result.Bytes, result.SHA256)
}
