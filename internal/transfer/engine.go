package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	nativesftp "github.com/bashatahamal/vericopy/internal/backend/sftp"
	"github.com/bashatahamal/vericopy/internal/checksum"
	"github.com/bashatahamal/vericopy/internal/permissions"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

// RemoteFS is the minimum native SFTP surface required by the transfer engine.
type RemoteFS interface {
	Open(path string) (io.ReadCloser, error)
	OpenFile(path string, flags int) (nativesftp.File, error)
	Stat(path string) (fs.FileInfo, error)
	Lstat(path string) (fs.FileInfo, error)
	Mkdir(path string) error
	MkdirAll(path string) error
	Chmod(path string, mode fs.FileMode) error
	Chown(path string, uid, gid int) error
	Chtimes(path string, atime, mtime time.Time) error
	Rename(oldPath, newPath string) error
	Remove(path string) error
}

// Options is the verified transfer policy.
type Options struct {
	Recursive    bool
	Resume       bool
	Overwrite    bool
	NoClobber    bool
	PreserveTime bool
	DryRun       bool
	Policy       permissions.Policy
	GID          *int
}

// Result summarizes verified transfer work.
type Result struct {
	Source       string `json:"source"`
	Destination  string `json:"destination"`
	Files        int    `json:"files"`
	Bytes        int64  `json:"bytes"`
	SHA256       string `json:"sha256,omitempty"`
	ResumedBytes int64  `json:"resumed_bytes,omitempty"`
	Verified     bool   `json:"verified"`
	DryRun       bool   `json:"dry_run"`
}

// Engine performs transfers without invoking a remote shell.
type Engine struct {
	Remote RemoteFS
}

// Copy transfers a regular file or a directory tree.
func (e Engine) Copy(ctx context.Context, source, destination string, options Options) (Result, error) {
	if options.Overwrite && options.NoClobber {
		return Result{}, verrors.New(verrors.CodeInvalidArguments, "--overwrite and --no-clobber cannot be used together")
	}
	info, err := os.Lstat(source)
	if err != nil {
		return Result{}, verrors.Wrap(verrors.CodeInvalidLocalPath, "the source cannot be inspected", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Result{}, verrors.New(verrors.CodeUnsupportedFileType, "symbolic links are not followed by default")
	}
	if info.IsDir() {
		if !options.Recursive {
			return Result{}, verrors.New(verrors.CodeInvalidArguments, "the source is a directory; use --recursive")
		}
		return e.copyDirectory(ctx, source, destination, options)
	}
	if !info.Mode().IsRegular() {
		return Result{}, verrors.New(verrors.CodeUnsupportedFileType, "only regular files and directories are supported")
	}
	return e.copyFile(ctx, source, destination, options)
}

func (e Engine) copyDirectory(ctx context.Context, source, destination string, options Options) (Result, error) {
	result := Result{Source: source, Destination: destination, Verified: true, DryRun: options.DryRun}
	type directoryRecord struct {
		remote string
		info   fs.FileInfo
	}
	directories := make([]directoryRecord, 0)
	if err := e.validateRemoteParents(destination); err != nil {
		return Result{}, err
	}
	if existing, err := e.Remote.Lstat(destination); err == nil && existing.Mode()&fs.ModeSymlink != 0 {
		return Result{}, verrors.New(verrors.CodeUnsupportedFileType,
			fmt.Sprintf("remote symbolic link %q is not followed", destination))
	} else if err == nil && !options.Overwrite {
		return Result{}, verrors.New(verrors.CodeDestinationExists,
			fmt.Sprintf("destination directory %q already exists", destination)).WithHint(
			"Choose another destination or pass --overwrite to merge explicitly.")
	} else if err != nil && !isNotExist(err) {
		return Result{}, verrors.Wrap(verrors.CodeDestinationNotWritable, "the destination directory could not be inspected", err)
	}
	err := filepath.WalkDir(source, func(local string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return verrors.New(verrors.CodeUnsupportedFileType,
				fmt.Sprintf("symbolic link %q is not followed", local))
		}
		relative, err := filepath.Rel(source, local)
		if err != nil {
			return err
		}
		remotePath := destination
		if relative != "." {
			remotePath = path.Join(destination, filepath.ToSlash(relative))
		}
		if entry.IsDir() {
			directoryInfo, err := entry.Info()
			if err != nil {
				return err
			}
			directories = append(directories, directoryRecord{remote: remotePath, info: directoryInfo})
			if options.DryRun {
				return nil
			}
			if err := e.Remote.MkdirAll(remotePath); err != nil {
				return verrors.Wrap(verrors.CodeDestinationNotWritable, "could not create a destination directory", err)
			}
			if options.GID != nil {
				if err := e.Remote.Chown(remotePath, -1, *options.GID); err != nil {
					return verrors.Wrap(verrors.CodeGroupUnavailable, "could not apply the requested group", err)
				}
			}
			directoryMode := options.Policy.Directory
			if options.Policy.Preserve {
				directoryMode = directoryInfo.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
			}
			if err := e.Remote.Chmod(remotePath, directoryMode); err != nil {
				return verrors.Wrap(verrors.CodeInvalidPermission, "could not apply the directory permission policy", err)
			}
			return nil
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !fileInfo.Mode().IsRegular() {
			return verrors.New(verrors.CodeUnsupportedFileType,
				fmt.Sprintf("special file %q is not supported", local))
		}
		fileResult, err := e.copyFile(ctx, local, remotePath, options)
		if err != nil {
			return err
		}
		result.Files++
		result.Bytes += fileResult.Bytes
		result.ResumedBytes += fileResult.ResumedBytes
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	if options.PreserveTime && !options.DryRun {
		for index := len(directories) - 1; index >= 0; index-- {
			directory := directories[index]
			if err := e.Remote.Chtimes(directory.remote, directory.info.ModTime(), directory.info.ModTime()); err != nil {
				return Result{}, verrors.Wrap(verrors.CodeInvalidPermission, "could not preserve a directory modification time", err)
			}
		}
	}
	return result, nil
}

func (e Engine) copyFile(ctx context.Context, source, destination string, options Options) (Result, error) {
	result := Result{Source: source, Destination: destination, Files: 1, Verified: false, DryRun: options.DryRun}
	initial, err := os.Stat(source)
	if err != nil {
		return Result{}, verrors.Wrap(verrors.CodeInvalidLocalPath, "the source cannot be read", err)
	}
	if err := e.validateRemoteParents(destination); err != nil {
		return Result{}, err
	}
	if existing, statErr := e.Remote.Lstat(destination); statErr == nil {
		if existing.Mode()&fs.ModeSymlink != 0 {
			return Result{}, verrors.New(verrors.CodeUnsupportedFileType,
				fmt.Sprintf("remote symbolic link %q is not followed", destination))
		}
		if existing.IsDir() {
			destination = path.Join(destination, filepath.Base(source))
			result.Destination = destination
			existing, statErr = e.Remote.Lstat(destination)
			if statErr == nil && existing.Mode()&fs.ModeSymlink != 0 {
				return Result{}, verrors.New(verrors.CodeUnsupportedFileType,
					fmt.Sprintf("remote symbolic link %q is not followed", destination))
			}
		}
		if statErr == nil && !options.Overwrite {
			return Result{}, verrors.New(verrors.CodeDestinationExists,
				fmt.Sprintf("destination %q already exists", destination)).WithHint(
				"Choose another destination or pass --overwrite explicitly.")
		}
		if statErr != nil && !isNotExist(statErr) {
			return Result{}, verrors.Wrap(verrors.CodeDestinationNotWritable, "the destination could not be inspected", statErr)
		}
	} else if !isNotExist(statErr) {
		return Result{}, verrors.Wrap(verrors.CodeDestinationNotWritable, "the destination could not be inspected", statErr)
	}

	localFile, err := os.Open(source)
	if err != nil {
		return Result{}, verrors.Wrap(verrors.CodeInvalidLocalPath, "the source could not be opened", err)
	}
	prefixDigest, prefixBytes, err := checksum.PrefixSHA256(localFile, prefixLimit)
	_ = localFile.Close()
	if err != nil {
		return Result{}, verrors.Wrap(verrors.CodeTransferFailed, "could not fingerprint the source", err)
	}
	metadata := PartialMetadata{
		Schema: metadataSchema, SourceSize: initial.Size(), SourceMTime: initial.ModTime().UnixNano(),
		PrefixBytes: prefixBytes, PrefixSHA256: prefixDigest,
	}
	partialPath, sidecarPath := PartialPaths(destination, metadata)
	if options.DryRun {
		result.Bytes = initial.Size()
		return result, nil
	}
	if err := e.Remote.MkdirAll(path.Dir(destination)); err != nil {
		return Result{}, verrors.Wrap(verrors.CodeDestinationNotWritable, "could not create the destination parent", err)
	}

	offset, err := e.preparePartial(source, partialPath, sidecarPath, metadata, options.Resume)
	if err != nil {
		return Result{}, err
	}
	result.ResumedBytes = offset
	localDigest, localBytes, err := e.upload(ctx, source, partialPath, offset)
	if err != nil {
		return Result{}, err
	}

	finalSource, err := os.Stat(source)
	if err != nil || finalSource.Size() != initial.Size() || !finalSource.ModTime().Equal(initial.ModTime()) {
		return Result{}, verrors.New(verrors.CodeSourceChanged, "the source changed while it was being transferred").
			WithHint("Retry after the source is no longer being modified.")
	}
	remoteReader, err := e.Remote.Open(partialPath)
	if err != nil {
		return Result{}, verrors.Wrap(verrors.CodeVerificationFailed, "could not reopen the remote partial file", err)
	}
	remoteDigest, remoteBytes, err := checksum.SHA256(remoteReader)
	_ = remoteReader.Close()
	if err != nil {
		return Result{}, verrors.Wrap(verrors.CodeVerificationFailed, "could not calculate the remote SHA-256", err)
	}
	if localBytes != remoteBytes || localDigest != remoteDigest {
		return Result{}, verrors.New(verrors.CodeChecksumMismatch,
			"the remote partial file does not match the source").WithDetails(map[string]any{
			"local_size": localBytes, "remote_size": remoteBytes,
			"local_sha256": localDigest, "remote_sha256": remoteDigest,
		})
	}
	if err := e.applyFilePolicy(partialPath, initial, options); err != nil {
		return Result{}, err
	}
	if options.Overwrite {
		if _, statErr := e.Remote.Lstat(destination); statErr == nil {
			if err := e.Remote.Remove(destination); err != nil {
				return Result{}, verrors.Wrap(verrors.CodeDestinationNotWritable, "could not replace the existing destination", err)
			}
		}
	}
	if err := e.Remote.Rename(partialPath, destination); err != nil {
		return Result{}, verrors.Wrap(verrors.CodeTransferFailed,
			"verification succeeded, but finalizing the remote file failed", err).
			WithHint("The verified partial file was retained for inspection or retry.")
	}
	_ = e.Remote.Remove(sidecarPath)
	result.Bytes, result.SHA256, result.Verified = localBytes, localDigest, true
	return result, nil
}

func (e Engine) preparePartial(source, partialPath, sidecarPath string, expected PartialMetadata, resume bool) (int64, error) {
	partialInfo, partialErr := e.Remote.Lstat(partialPath)
	if partialErr == nil && partialInfo.Mode()&fs.ModeSymlink != 0 {
		return 0, verrors.New(verrors.CodeUnsupportedFileType,
			fmt.Sprintf("remote symbolic link %q is not followed", partialPath))
	}
	if partialErr == nil && resume {
		stored, err := e.readMetadata(sidecarPath)
		if err != nil || !ResumeCompatible(expected, stored, partialInfo.Size()) {
			return 0, verrors.New(verrors.CodeResumeIncompatible,
				"existing partial state does not match this source").WithHint(
				"Retry without --resume to replace Vericopy's stale partial state.")
		}
		validationBytes := min(expected.PrefixBytes, partialInfo.Size())
		partialReader, err := e.Remote.Open(partialPath)
		if err != nil {
			return 0, err
		}
		remotePrefix, _, hashErr := checksum.PrefixSHA256(partialReader, validationBytes)
		_ = partialReader.Close()
		if hashErr != nil {
			return 0, hashErr
		}
		localReader, err := os.Open(source)
		if err != nil {
			return 0, err
		}
		localPrefix, _, hashErr := checksum.PrefixSHA256(localReader, validationBytes)
		_ = localReader.Close()
		if hashErr != nil {
			return 0, hashErr
		}
		if remotePrefix != localPrefix {
			return 0, verrors.New(verrors.CodeResumeIncompatible, "the remote partial prefix does not match the source")
		}
		return partialInfo.Size(), nil
	}
	if partialErr != nil && !isNotExist(partialErr) {
		return 0, verrors.Wrap(verrors.CodeDestinationNotWritable, "partial state could not be inspected", partialErr)
	}
	if partialErr == nil {
		if err := e.Remote.Remove(partialPath); err != nil {
			return 0, verrors.Wrap(verrors.CodeDestinationNotWritable, "stale partial state could not be removed", err)
		}
		_ = e.Remote.Remove(sidecarPath)
	} else if sidecarInfo, sidecarErr := e.Remote.Lstat(sidecarPath); sidecarErr == nil {
		if sidecarInfo.Mode()&fs.ModeSymlink != 0 {
			return 0, verrors.New(verrors.CodeUnsupportedFileType,
				fmt.Sprintf("remote symbolic link %q is not followed", sidecarPath))
		}
		if err := e.Remote.Remove(sidecarPath); err != nil {
			return 0, verrors.Wrap(verrors.CodeDestinationNotWritable, "stale resume metadata could not be removed", err)
		}
	} else if !isNotExist(sidecarErr) {
		return 0, verrors.Wrap(verrors.CodeDestinationNotWritable, "resume metadata could not be inspected", sidecarErr)
	}
	if err := e.writeMetadata(sidecarPath, expected); err != nil {
		return 0, err
	}
	file, err := e.Remote.OpenFile(partialPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return 0, verrors.Wrap(verrors.CodeDestinationNotWritable, "could not create a restrictive partial file", err)
	}
	_ = file.Close()
	if err := e.Remote.Chmod(partialPath, 0o600); err != nil {
		return 0, verrors.Wrap(verrors.CodeInvalidPermission, "could not restrict the partial file", err)
	}
	return 0, nil
}

func (e Engine) upload(ctx context.Context, source, partialPath string, offset int64) (string, int64, error) {
	local, err := os.Open(source)
	if err != nil {
		return "", 0, verrors.Wrap(verrors.CodeInvalidLocalPath, "the source could not be reopened", err)
	}
	defer local.Close()
	hash := sha256.New()
	if offset > 0 {
		if _, err := io.CopyN(hash, &contextReader{ctx: ctx, reader: local}, offset); err != nil {
			return "", 0, verrors.Wrap(verrors.CodeTransferFailed, "could not hash the resumed source prefix", err)
		}
	}
	remote, err := e.Remote.OpenFile(partialPath, os.O_WRONLY)
	if err != nil {
		return "", 0, verrors.Wrap(verrors.CodeDestinationNotWritable, "could not open the remote partial file", err)
	}
	defer remote.Close()
	if _, err := remote.Seek(offset, io.SeekStart); err != nil {
		return "", 0, verrors.Wrap(verrors.CodeTransferFailed, "could not seek the remote partial file for resume", err)
	}
	written, err := io.Copy(io.MultiWriter(remote, hash), &contextReader{ctx: ctx, reader: local})
	if err != nil {
		if ctx.Err() != nil {
			return "", offset + written, verrors.Wrap(verrors.CodeTransferInterrupted,
				"the transfer was interrupted; compatible partial state was retained", ctx.Err())
		}
		return "", offset + written, verrors.Wrap(verrors.CodeTransferInterrupted,
			"the connection ended before upload completed; compatible partial state was retained", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), offset + written, nil
}

func (e Engine) applyFilePolicy(remotePath string, source fs.FileInfo, options Options) error {
	mode := options.Policy.File
	if options.Policy.Preserve {
		mode = source.Mode() & (fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky)
	}
	if options.GID != nil {
		if err := e.Remote.Chown(remotePath, -1, *options.GID); err != nil {
			return verrors.Wrap(verrors.CodeGroupUnavailable, "could not apply the requested group", err)
		}
	}
	if err := e.Remote.Chmod(remotePath, mode); err != nil {
		return verrors.Wrap(verrors.CodeInvalidPermission, "could not apply the file permission policy", err)
	}
	if options.PreserveTime {
		if err := e.Remote.Chtimes(remotePath, source.ModTime(), source.ModTime()); err != nil {
			return verrors.Wrap(verrors.CodeInvalidPermission, "could not preserve the source modification time", err)
		}
	}
	return nil
}

func (e Engine) writeMetadata(filename string, metadata PartialMetadata) error {
	file, err := e.Remote.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return verrors.Wrap(verrors.CodeDestinationNotWritable, "could not create resume metadata", err)
	}
	if err := e.Remote.Chmod(filename, 0o600); err != nil {
		_ = file.Close()
		return verrors.Wrap(verrors.CodeInvalidPermission, "could not restrict resume metadata", err)
	}
	encodeErr := json.NewEncoder(file).Encode(metadata)
	closeErr := file.Close()
	if encodeErr != nil {
		return verrors.Wrap(verrors.CodeDestinationNotWritable, "could not write resume metadata", encodeErr)
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func (e Engine) readMetadata(filename string) (PartialMetadata, error) {
	info, err := e.Remote.Lstat(filename)
	if err != nil {
		return PartialMetadata{}, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return PartialMetadata{}, verrors.New(verrors.CodeUnsupportedFileType,
			fmt.Sprintf("remote symbolic link %q is not followed", filename))
	}
	file, err := e.Remote.Open(filename)
	if err != nil {
		return PartialMetadata{}, err
	}
	defer file.Close()
	var metadata PartialMetadata
	if err := json.NewDecoder(io.LimitReader(file, 16*1024)).Decode(&metadata); err != nil {
		return PartialMetadata{}, err
	}
	return metadata, nil
}

func (e Engine) validateRemoteParents(destination string) error {
	parent := path.Dir(path.Clean(destination))
	if parent == "." {
		return nil
	}
	components := make([]string, 0)
	if path.IsAbs(parent) {
		components = append(components, "/")
	}
	current := ""
	for _, part := range strings.Split(strings.TrimPrefix(parent, "/"), "/") {
		if part == "" || part == "." {
			continue
		}
		if path.IsAbs(parent) {
			current = path.Join("/", current, part)
		} else {
			current = path.Join(current, part)
		}
		components = append(components, current)
	}
	for _, component := range components {
		info, err := e.Remote.Lstat(component)
		if isNotExist(err) {
			return nil
		}
		if err != nil {
			return verrors.Wrap(verrors.CodeDestinationNotWritable,
				fmt.Sprintf("remote parent %q could not be inspected", component), err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return verrors.New(verrors.CodeUnsupportedFileType,
				fmt.Sprintf("remote symbolic link %q is not followed", component))
		}
		if !info.IsDir() {
			return verrors.New(verrors.CodeDestinationNotWritable,
				fmt.Sprintf("remote parent %q is not a directory", component))
		}
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(buffer)
	}
}

func isNotExist(err error) bool {
	return err != nil && (errors.Is(err, fs.ErrNotExist) || strings.Contains(strings.ToLower(err.Error()), "no such file"))
}
