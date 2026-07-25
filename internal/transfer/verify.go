package transfer

import (
	"context"
	"io/fs"
	"os"

	"github.com/bashatahamal/vericopy/internal/checksum"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

// Verify compares a local regular file with a remote file through SFTP.
func (e Engine) Verify(ctx context.Context, source, destination string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	localInfo, err := os.Lstat(source)
	if err != nil {
		return Result{}, verrors.Wrap(verrors.CodeInvalidLocalPath, "could not inspect the local source", err)
	}
	if localInfo.Mode()&fs.ModeSymlink != 0 || !localInfo.Mode().IsRegular() {
		return Result{}, verrors.New(verrors.CodeUnsupportedFileType,
			"local verification requires a regular file and does not follow symbolic links")
	}
	remoteInfo, err := e.Remote.Lstat(destination)
	if err != nil {
		return Result{}, verrors.Wrap(verrors.CodeVerificationFailed, "could not inspect the remote destination", err)
	}
	if remoteInfo.Mode()&fs.ModeSymlink != 0 || !remoteInfo.Mode().IsRegular() {
		return Result{}, verrors.New(verrors.CodeUnsupportedFileType,
			"remote verification requires a regular file and does not follow symbolic links")
	}
	local, err := os.Open(source)
	if err != nil {
		return Result{}, verrors.Wrap(verrors.CodeInvalidLocalPath, "could not open the local source", err)
	}
	localDigest, localBytes, err := checksum.SHA256(&contextReader{ctx: ctx, reader: local})
	_ = local.Close()
	if err != nil {
		return Result{}, err
	}
	if !e.verifyRemoteFast(ctx, destination, localDigest, localBytes) {
		remote, err := e.Remote.Open(destination)
		if err != nil {
			return Result{}, verrors.Wrap(verrors.CodeVerificationFailed, "could not open the remote destination", err)
		}
		remoteDigest, remoteBytes, err := checksum.SHA256(&contextReader{ctx: ctx, reader: remote})
		_ = remote.Close()
		if err != nil {
			return Result{}, err
		}
		if localBytes != remoteBytes || localDigest != remoteDigest {
			return Result{}, verrors.New(verrors.CodeChecksumMismatch, "the source and destination do not match").
				WithDetails(map[string]any{"local_size": localBytes, "remote_size": remoteBytes, "local_sha256": localDigest, "remote_sha256": remoteDigest})
		}
	}
	return Result{Source: source, Destination: destination, Files: 1, Bytes: localBytes, SHA256: localDigest, Verified: true}, nil
}
