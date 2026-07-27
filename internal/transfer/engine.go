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
	"sort"
	"strings"
	"time"

	nativesftp "github.com/bashatahamal/vericopy/internal/backend/sftp"
	"github.com/bashatahamal/vericopy/internal/checksum"
	"github.com/bashatahamal/vericopy/internal/medianame"
	"github.com/bashatahamal/vericopy/internal/permissions"
	"github.com/bashatahamal/vericopy/internal/thumbnail"
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
	ReadDir(path string) ([]fs.FileInfo, error)
}

// Options is the verified transfer policy.
type Options struct {
	Recursive          bool
	Resume             bool
	Overwrite          bool
	NoClobber          bool
	PreserveTime       bool
	DryRun             bool
	FixMediaNames      bool
	GenerateThumbnails bool
	Policy             permissions.Policy
	GID                *int
	Progress           func(Progress)
}

// Progress is an honest per-file transfer update. Directory transfers emit an
// update for the current file instead of inventing an aggregate percentage.
// CurrentFile and TotalFiles locate that file within a directory transfer;
// both are 1 for a single-file transfer. TotalFiles is 0 when the source
// tree could not be counted in advance, in which case CurrentFile still
// increments but has no known denominator.
type Progress struct {
	Phase            string
	Source           string
	Destination      string
	TransferredBytes int64
	TotalBytes       int64
	ResumedBytes     int64
	CurrentFile      int
	TotalFiles       int
}

// Result summarizes verified transfer work.
type Result struct {
	Source       string `json:"source"`
	Destination  string `json:"destination"`
	Files        int    `json:"files"`
	SkippedFiles int    `json:"skipped_files,omitempty"`
	Bytes        int64  `json:"bytes"`
	SHA256       string `json:"sha256,omitempty"`
	ResumedBytes int64  `json:"resumed_bytes,omitempty"`
	Verified     bool   `json:"verified"`
	DryRun       bool   `json:"dry_run"`
}

// RemoteHasher optionally computes a remote file's SHA-256 digest without
// reading it back over SFTP. It is advisory: any error, unavailability, or
// digest mismatch falls back to the authoritative byte-for-byte read-back
// verification, so a bug or an unsupported remote never weakens the
// transfer guarantee, only its speed.
type RemoteHasher interface {
	HashSHA256(ctx context.Context, path string) (digest string, ok bool, err error)
}

// Engine performs transfers without invoking a remote shell for the file
// transfer itself. Hasher, if set, may use a remote shell command purely to
// accelerate verification; see RemoteHasher.
type Engine struct {
	Remote RemoteFS
	Hasher RemoteHasher
}

func (e Engine) report(options Options, progress Progress) {
	if options.Progress != nil {
		options.Progress(progress)
	}
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
	return e.copyFile(ctx, source, destination, options, 1, 1)
}

func (e Engine) copyDirectory(ctx context.Context, source, destination string, options Options) (Result, error) {
	result := Result{Source: source, Destination: destination, Verified: true, DryRun: options.DryRun}
	type directoryRecord struct {
		remote string
		info   fs.FileInfo
	}
	directories := make([]directoryRecord, 0)
	// Best-effort: a source tree that cannot be pre-counted (e.g. permission
	// error on a subdirectory) still transfers normally, just without a known
	// "file N of M" denominator in progress updates.
	totalFiles, _ := countRegularFiles(source)
	fileIndex := 0
	if err := e.validateRemoteParents(destination); err != nil {
		return Result{}, err
	}
	if existing, err := e.Remote.Lstat(destination); err == nil && existing.Mode()&fs.ModeSymlink != 0 {
		return Result{}, verrors.New(verrors.CodeUnsupportedFileType,
			fmt.Sprintf("remote symbolic link %q is not followed", destination))
	} else if err == nil && !options.Overwrite && !options.NoClobber {
		return Result{}, verrors.New(verrors.CodeDestinationExists,
			fmt.Sprintf("destination directory %q already exists", destination)).WithHint(
			"Choose another destination, pass --overwrite to merge explicitly, or pass --no-clobber to add only what is missing.")
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
		fileIndex++
		fileResult, err := e.copyFile(ctx, local, remotePath, options, fileIndex, totalFiles)
		if err != nil {
			return err
		}
		result.Files += fileResult.Files
		result.SkippedFiles += fileResult.SkippedFiles
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

func (e Engine) copyFile(ctx context.Context, source, destination string, options Options, fileIndex, totalFiles int) (Result, error) {
	result := Result{Source: source, Destination: destination, Files: 1, Verified: false, DryRun: options.DryRun}
	initial, err := os.Stat(source)
	if err != nil {
		return Result{}, verrors.Wrap(verrors.CodeInvalidLocalPath, "the source cannot be read", err)
	}
	e.report(options, Progress{
		Phase: "preparing", Source: source, Destination: destination, TotalBytes: initial.Size(),
		CurrentFile: fileIndex, TotalFiles: totalFiles,
	})
	if err := e.validateRemoteParents(destination); err != nil {
		return Result{}, err
	}
	destination, skip, err := e.resolveDestination(source, destination, options)
	if err != nil {
		return Result{}, err
	}
	result.Destination = destination
	if skip {
		e.report(options, Progress{
			Phase: "skipped", Source: source, Destination: destination, TotalBytes: initial.Size(),
			CurrentFile: fileIndex, TotalFiles: totalFiles,
		})
		return Result{Source: source, Destination: destination, SkippedFiles: 1, DryRun: options.DryRun}, nil
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
	e.report(options, Progress{
		Phase: "uploading", Source: source, Destination: destination,
		TransferredBytes: offset, TotalBytes: initial.Size(), ResumedBytes: offset,
		CurrentFile: fileIndex, TotalFiles: totalFiles,
	})
	localDigest, localBytes, err := e.upload(ctx, source, partialPath, offset, func(transferred int64) {
		e.report(options, Progress{
			Phase: "uploading", Source: source, Destination: destination,
			TransferredBytes: transferred, TotalBytes: initial.Size(), ResumedBytes: offset,
			CurrentFile: fileIndex, TotalFiles: totalFiles,
		})
	})
	if err != nil {
		return Result{}, err
	}

	finalSource, err := os.Stat(source)
	if err != nil || finalSource.Size() != initial.Size() || !finalSource.ModTime().Equal(initial.ModTime()) {
		return Result{}, verrors.New(verrors.CodeSourceChanged, "the source changed while it was being transferred").
			WithHint("Retry after the source is no longer being modified.")
	}
	e.report(options, Progress{
		Phase: "verifying", Source: source, Destination: destination,
		TransferredBytes: localBytes, TotalBytes: initial.Size(), ResumedBytes: offset,
		CurrentFile: fileIndex, TotalFiles: totalFiles,
	})
	if !e.verifyRemoteFast(ctx, partialPath, localDigest, localBytes) {
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
	}
	if err := e.applyFilePolicy(partialPath, initial, options); err != nil {
		return Result{}, err
	}
	e.report(options, Progress{
		Phase: "finalizing", Source: source, Destination: destination,
		TransferredBytes: localBytes, TotalBytes: initial.Size(), ResumedBytes: offset,
		CurrentFile: fileIndex, TotalFiles: totalFiles,
	})
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
	if options.GenerateThumbnails {
		if err := e.uploadThumbnail(destination, options); err != nil {
			return Result{}, err
		}
	}
	e.report(options, Progress{
		Phase: "verified", Source: source, Destination: destination,
		TransferredBytes: localBytes, TotalBytes: initial.Size(), ResumedBytes: offset,
		CurrentFile: fileIndex, TotalFiles: totalFiles,
	})
	return result, nil
}

// resolveDestination applies the same directory-append rule copyFile has
// always used, then optionally renames the leaf filename for media servers,
// then makes the existence decision (skip, reject, or proceed) against
// whichever path will actually be written. skip reports a NoClobber match.
func (e Engine) resolveDestination(source, destination string, options Options) (string, bool, error) {
	existing, statErr := e.Remote.Lstat(destination)
	if statErr == nil {
		if existing.Mode()&fs.ModeSymlink != 0 {
			return "", false, verrors.New(verrors.CodeUnsupportedFileType,
				fmt.Sprintf("remote symbolic link %q is not followed", destination))
		}
		if existing.IsDir() {
			destination = path.Join(destination, filepath.Base(source))
			existing, statErr = e.Remote.Lstat(destination)
		}
	} else if !isNotExist(statErr) {
		return "", false, verrors.Wrap(verrors.CodeDestinationNotWritable, "the destination could not be inspected", statErr)
	}

	if options.FixMediaNames {
		if renamed := renameForMediaServers(destination); renamed != destination {
			destination = renamed
			existing, statErr = e.Remote.Lstat(destination)
		}
	}

	if statErr == nil {
		if existing.Mode()&fs.ModeSymlink != 0 {
			return "", false, verrors.New(verrors.CodeUnsupportedFileType,
				fmt.Sprintf("remote symbolic link %q is not followed", destination))
		}
		if options.NoClobber {
			return destination, true, nil
		}
		if !options.Overwrite {
			return "", false, verrors.New(verrors.CodeDestinationExists,
				fmt.Sprintf("destination %q already exists", destination)).WithHint(
				"Choose another destination, pass --overwrite explicitly, or pass --no-clobber to skip it and copy the rest.")
		}
	} else if !isNotExist(statErr) {
		return "", false, verrors.Wrap(verrors.CodeDestinationNotWritable, "the destination could not be inspected", statErr)
	}
	return destination, false, nil
}

// renameForMediaServers removes a bare release year that collides with a
// following SxxExx tag; see the medianame package. Non-video files and
// filenames without the collision are returned unchanged.
func renameForMediaServers(destination string) string {
	dir, base := path.Split(destination)
	renamed, ok := medianame.RewriteFilename(base)
	if !ok {
		return destination
	}
	return path.Join(dir, renamed)
}

// uploadThumbnail writes a generated placeholder JPEG next to a just-verified
// episode file, using the video's own base name with a .jpg extension. That
// exact-name-match is the local-image convention Jellyfin and Emby use for
// an item's Primary (poster/grid) image, so it replaces a frame extracted
// from the video instead of only affecting an unrelated image slot. It is a
// no-op for files without a recognizable season/episode tag.
func (e Engine) uploadThumbnail(destination string, options Options) error {
	label, ok := medianame.EpisodeLabel(path.Base(destination))
	if !ok {
		return nil
	}
	image, err := thumbnail.Generate(label)
	if err != nil {
		return verrors.Wrap(verrors.CodeTransferFailed, "could not generate a placeholder thumbnail", err)
	}
	thumbnailPath := strings.TrimSuffix(destination, path.Ext(destination)) + ".jpg"
	file, err := e.Remote.OpenFile(thumbnailPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY)
	if err != nil {
		return verrors.Wrap(verrors.CodeDestinationNotWritable, "could not create the placeholder thumbnail", err)
	}
	_, writeErr := file.Write(image)
	closeErr := file.Close()
	if writeErr != nil {
		return verrors.Wrap(verrors.CodeTransferFailed, "could not write the placeholder thumbnail", writeErr)
	}
	if closeErr != nil {
		return verrors.Wrap(verrors.CodeTransferFailed, "could not finalize the placeholder thumbnail", closeErr)
	}
	if err := e.Remote.Chmod(thumbnailPath, options.Policy.File); err != nil {
		return verrors.Wrap(verrors.CodeInvalidPermission, "could not apply the file permission policy to the placeholder thumbnail", err)
	}
	return nil
}

// verifyRemoteFast reports whether path was confirmed to match localDigest
// and localBytes using e.Hasher instead of reading the remote file back. It
// returns false on any uncertainty at all (no hasher, unavailable, error, or
// mismatch) so the caller always falls back to the authoritative read-back
// check; this method never itself declares a transfer failed.
func (e Engine) verifyRemoteFast(ctx context.Context, path, localDigest string, localBytes int64) bool {
	if e.Hasher == nil {
		return false
	}
	digest, ok, err := e.Hasher.HashSHA256(ctx, path)
	if err != nil || !ok || digest != localDigest {
		return false
	}
	remoteInfo, err := e.Remote.Lstat(path)
	if err != nil {
		return false
	}
	return remoteInfo.Size() == localBytes
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

func (e Engine) upload(ctx context.Context, source, partialPath string, offset int64, progress func(int64)) (string, int64, error) {
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
	written, err := io.Copy(&progressWriter{
		writer: io.MultiWriter(remote, hash),
		onWrite: func(written int64) {
			if progress != nil {
				progress(offset + written)
			}
		},
	}, &contextReader{ctx: ctx, reader: local})
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

type progressWriter struct {
	writer  io.Writer
	written int64
	onWrite func(int64)
}

func (w *progressWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	if n > 0 {
		w.written += int64(n)
		w.onWrite(w.written)
	}
	return n, err
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

// countRegularFiles walks source once to count the regular files it
// contains, giving progress reporting a denominator before the transfer
// itself starts walking the same tree. It mirrors copyDirectory's own
// file/directory classification, but only counts; it does not reject
// symlinks or special files here; the real walk still does that.
func countRegularFiles(source string) (int, error) {
	count := 0
	err := filepath.WalkDir(source, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
			count++
		}
		return nil
	})
	return count, err
}

// Download transfers a regular file or directory tree from the remote
// source to a local destination. It mirrors Copy's guarantees (resumable
// partial state, byte-for-byte SHA-256 verification, truthful progress) in
// the opposite direction: the remote side is read-only here, and the local
// destination is where partial state, policy, and the final file live.
func (e Engine) Download(ctx context.Context, source, destination string, options Options) (Result, error) {
	if options.Overwrite && options.NoClobber {
		return Result{}, verrors.New(verrors.CodeInvalidArguments, "--overwrite and --no-clobber cannot be used together")
	}
	info, err := e.Remote.Lstat(source)
	if err != nil {
		return Result{}, verrors.Wrap(verrors.CodeInvalidRemoteSource, "the remote source cannot be inspected", err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return Result{}, verrors.New(verrors.CodeUnsupportedFileType, "remote symbolic links are not followed by default")
	}
	if info.IsDir() {
		if !options.Recursive {
			return Result{}, verrors.New(verrors.CodeInvalidArguments, "the source is a directory; use --recursive")
		}
		return e.downloadDirectory(ctx, source, destination, options)
	}
	if !info.Mode().IsRegular() {
		return Result{}, verrors.New(verrors.CodeUnsupportedFileType, "only regular files and directories are supported")
	}
	return e.downloadFile(ctx, source, destination, options, 1, 1)
}

func (e Engine) downloadDirectory(ctx context.Context, source, destination string, options Options) (Result, error) {
	result := Result{Source: source, Destination: destination, Verified: true, DryRun: options.DryRun}
	type directoryRecord struct {
		local string
		info  fs.FileInfo
	}
	directories := make([]directoryRecord, 0)
	totalFiles, _ := e.countRemoteRegularFiles(source)
	fileIndex := 0
	if err := validateLocalParents(destination); err != nil {
		return Result{}, err
	}
	if existing, err := os.Lstat(destination); err == nil && existing.Mode()&os.ModeSymlink != 0 {
		return Result{}, verrors.New(verrors.CodeUnsupportedFileType,
			fmt.Sprintf("local symbolic link %q is not followed", destination))
	} else if err == nil && !options.Overwrite && !options.NoClobber {
		return Result{}, verrors.New(verrors.CodeDestinationExists,
			fmt.Sprintf("destination directory %q already exists", destination)).WithHint(
			"Choose another destination, pass --overwrite to merge explicitly, or pass --no-clobber to add only what is missing.")
	} else if err != nil && !os.IsNotExist(err) {
		return Result{}, verrors.Wrap(verrors.CodeDestinationNotWritable, "the local destination directory could not be inspected", err)
	}
	err := e.walkRemoteDir(source, ".", func(relative string, isDir bool, entry fs.FileInfo) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		remotePath := source
		if relative != "." {
			remotePath = path.Join(source, relative)
		}
		if entry.Mode()&fs.ModeSymlink != 0 {
			return verrors.New(verrors.CodeUnsupportedFileType,
				fmt.Sprintf("remote symbolic link %q is not followed", remotePath))
		}
		localPath := destination
		if relative != "." {
			localPath = filepath.Join(destination, filepath.FromSlash(relative))
		}
		if isDir {
			directories = append(directories, directoryRecord{local: localPath, info: entry})
			if options.DryRun {
				return nil
			}
			if err := os.MkdirAll(localPath, 0o700); err != nil {
				return verrors.Wrap(verrors.CodeDestinationNotWritable, "could not create a local destination directory", err)
			}
			return nil
		}
		if !entry.Mode().IsRegular() {
			return verrors.New(verrors.CodeUnsupportedFileType,
				fmt.Sprintf("special remote file %q is not supported", remotePath))
		}
		fileIndex++
		fileResult, err := e.downloadFile(ctx, remotePath, localPath, options, fileIndex, totalFiles)
		if err != nil {
			return err
		}
		result.Files += fileResult.Files
		result.SkippedFiles += fileResult.SkippedFiles
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
			if err := os.Chtimes(directory.local, directory.info.ModTime(), directory.info.ModTime()); err != nil {
				return Result{}, verrors.Wrap(verrors.CodeInvalidPermission, "could not preserve a directory modification time", err)
			}
		}
	}
	return result, nil
}

func (e Engine) downloadFile(ctx context.Context, source, destination string, options Options, fileIndex, totalFiles int) (Result, error) {
	result := Result{Source: source, Destination: destination, Files: 1, Verified: false, DryRun: options.DryRun}
	initial, err := e.Remote.Lstat(source)
	if err != nil {
		return Result{}, verrors.Wrap(verrors.CodeInvalidRemoteSource, "the remote source cannot be read", err)
	}
	e.report(options, Progress{
		Phase: "preparing", Source: source, Destination: destination, TotalBytes: initial.Size(),
		CurrentFile: fileIndex, TotalFiles: totalFiles,
	})
	if err := validateLocalParents(destination); err != nil {
		return Result{}, err
	}
	destination, skip, err := e.resolveLocalDestination(source, destination, options)
	if err != nil {
		return Result{}, err
	}
	result.Destination = destination
	if skip {
		e.report(options, Progress{
			Phase: "skipped", Source: source, Destination: destination, TotalBytes: initial.Size(),
			CurrentFile: fileIndex, TotalFiles: totalFiles,
		})
		return Result{Source: source, Destination: destination, SkippedFiles: 1, DryRun: options.DryRun}, nil
	}

	prefixDigest, prefixBytes, err := e.remotePrefixSHA256(ctx, source, prefixLimit)
	if err != nil {
		return Result{}, verrors.Wrap(verrors.CodeTransferFailed, "could not fingerprint the remote source", err)
	}
	metadata := PartialMetadata{
		Schema: metadataSchema, SourceSize: initial.Size(), SourceMTime: initial.ModTime().UnixNano(),
		PrefixBytes: prefixBytes, PrefixSHA256: prefixDigest,
	}
	partialPath, sidecarPath := localPartialPaths(destination, metadata)
	if options.DryRun {
		result.Bytes = initial.Size()
		return result, nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return Result{}, verrors.Wrap(verrors.CodeDestinationNotWritable, "could not create the local destination parent", err)
	}

	offset, err := e.prepareLocalPartial(ctx, source, partialPath, sidecarPath, metadata, options.Resume)
	if err != nil {
		return Result{}, err
	}
	result.ResumedBytes = offset
	e.report(options, Progress{
		Phase: "downloading", Source: source, Destination: destination,
		TransferredBytes: offset, TotalBytes: initial.Size(), ResumedBytes: offset,
		CurrentFile: fileIndex, TotalFiles: totalFiles,
	})
	receivedDigest, receivedBytes, err := e.download(ctx, source, partialPath, offset, func(transferred int64) {
		e.report(options, Progress{
			Phase: "downloading", Source: source, Destination: destination,
			TransferredBytes: transferred, TotalBytes: initial.Size(), ResumedBytes: offset,
			CurrentFile: fileIndex, TotalFiles: totalFiles,
		})
	})
	if err != nil {
		return Result{}, err
	}

	finalSource, err := e.Remote.Lstat(source)
	if err != nil || finalSource.Size() != initial.Size() || !finalSource.ModTime().Equal(initial.ModTime()) {
		return Result{}, verrors.New(verrors.CodeSourceChanged, "the source changed while it was being transferred").
			WithHint("Retry after the source is no longer being modified.")
	}
	e.report(options, Progress{
		Phase: "verifying", Source: source, Destination: destination,
		TransferredBytes: receivedBytes, TotalBytes: initial.Size(), ResumedBytes: offset,
		CurrentFile: fileIndex, TotalFiles: totalFiles,
	})
	if !e.verifyRemoteFast(ctx, source, receivedDigest, receivedBytes) {
		remoteReader, err := e.Remote.Open(source)
		if err != nil {
			return Result{}, verrors.Wrap(verrors.CodeVerificationFailed, "could not reopen the remote source", err)
		}
		remoteDigest, remoteBytes, err := checksum.SHA256(remoteReader)
		_ = remoteReader.Close()
		if err != nil {
			return Result{}, verrors.Wrap(verrors.CodeVerificationFailed, "could not calculate the remote SHA-256", err)
		}
		if receivedBytes != remoteBytes || receivedDigest != remoteDigest {
			return Result{}, verrors.New(verrors.CodeChecksumMismatch,
				"the downloaded file does not match the remote source").WithDetails(map[string]any{
				"local_size": receivedBytes, "remote_size": remoteBytes,
				"local_sha256": receivedDigest, "remote_sha256": remoteDigest,
			})
		}
	}
	e.report(options, Progress{
		Phase: "finalizing", Source: source, Destination: destination,
		TransferredBytes: receivedBytes, TotalBytes: initial.Size(), ResumedBytes: offset,
		CurrentFile: fileIndex, TotalFiles: totalFiles,
	})
	if options.Overwrite {
		if _, statErr := os.Lstat(destination); statErr == nil {
			if err := os.Remove(destination); err != nil {
				return Result{}, verrors.Wrap(verrors.CodeDestinationNotWritable, "could not replace the existing local destination", err)
			}
		}
	}
	if err := os.Rename(partialPath, destination); err != nil {
		return Result{}, verrors.Wrap(verrors.CodeTransferFailed,
			"verification succeeded, but finalizing the local file failed", err).
			WithHint("The verified partial file was retained for inspection or retry.")
	}
	_ = os.Remove(sidecarPath)
	result.Bytes, result.SHA256, result.Verified = receivedBytes, receivedDigest, true
	if options.GenerateThumbnails {
		if err := e.downloadThumbnail(destination); err != nil {
			return Result{}, err
		}
	}
	e.report(options, Progress{
		Phase: "verified", Source: source, Destination: destination,
		TransferredBytes: receivedBytes, TotalBytes: initial.Size(), ResumedBytes: offset,
		CurrentFile: fileIndex, TotalFiles: totalFiles,
	})
	return result, nil
}

// walkRemoteDir visits root and every descendant in pre-order (a directory
// before its children), mirroring filepath.WalkDir's contract but driven by
// the RemoteFS interface instead of the local filesystem. relative uses "."
// for root and forward slashes below it regardless of the local OS, since it
// is pure bookkeeping rather than a path handed to either filesystem.
func (e Engine) walkRemoteDir(root, relative string, fn func(relative string, isDir bool, info fs.FileInfo) error) error {
	fullPath := root
	if relative != "." {
		fullPath = path.Join(root, relative)
	}
	info, err := e.Remote.Lstat(fullPath)
	if err != nil {
		return verrors.Wrap(verrors.CodeInvalidRemoteSource, fmt.Sprintf("could not inspect remote path %q", fullPath), err)
	}
	if err := fn(relative, info.IsDir(), info); err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	entries, err := e.Remote.ReadDir(fullPath)
	if err != nil {
		return verrors.Wrap(verrors.CodeInvalidRemoteSource, fmt.Sprintf("could not list remote directory %q", fullPath), err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		childRelative := entry.Name()
		if relative != "." {
			childRelative = path.Join(relative, entry.Name())
		}
		if err := e.walkRemoteDir(root, childRelative, fn); err != nil {
			return err
		}
	}
	return nil
}

func (e Engine) countRemoteRegularFiles(source string) (int, error) {
	count := 0
	err := e.walkRemoteDir(source, ".", func(_ string, isDir bool, info fs.FileInfo) error {
		if !isDir && info.Mode()&fs.ModeSymlink == 0 {
			count++
		}
		return nil
	})
	return count, err
}

// resolveLocalDestination mirrors resolveDestination for a local
// destination: the same directory-append, media-name rename, and
// existence-decision rules, applied with os.* instead of the RemoteFS.
func (e Engine) resolveLocalDestination(source, destination string, options Options) (string, bool, error) {
	existing, statErr := os.Lstat(destination)
	if statErr == nil {
		if existing.Mode()&os.ModeSymlink != 0 {
			return "", false, verrors.New(verrors.CodeUnsupportedFileType,
				fmt.Sprintf("local symbolic link %q is not followed", destination))
		}
		if existing.IsDir() {
			destination = filepath.Join(destination, path.Base(source))
			existing, statErr = os.Lstat(destination)
		}
	} else if !os.IsNotExist(statErr) {
		return "", false, verrors.Wrap(verrors.CodeDestinationNotWritable, "the local destination could not be inspected", statErr)
	}

	if options.FixMediaNames {
		if renamed := renameForMediaServersLocal(destination); renamed != destination {
			destination = renamed
			existing, statErr = os.Lstat(destination)
		}
	}

	if statErr == nil {
		if existing.Mode()&os.ModeSymlink != 0 {
			return "", false, verrors.New(verrors.CodeUnsupportedFileType,
				fmt.Sprintf("local symbolic link %q is not followed", destination))
		}
		if options.NoClobber {
			return destination, true, nil
		}
		if !options.Overwrite {
			return "", false, verrors.New(verrors.CodeDestinationExists,
				fmt.Sprintf("destination %q already exists", destination)).WithHint(
				"Choose another destination, pass --overwrite explicitly, or pass --no-clobber to skip it and download the rest.")
		}
	} else if !os.IsNotExist(statErr) {
		return "", false, verrors.Wrap(verrors.CodeDestinationNotWritable, "the local destination could not be inspected", statErr)
	}
	return destination, false, nil
}

func renameForMediaServersLocal(destination string) string {
	dir, base := filepath.Split(destination)
	renamed, ok := medianame.RewriteFilename(base)
	if !ok {
		return destination
	}
	return filepath.Join(dir, renamed)
}

// downloadThumbnail mirrors uploadThumbnail for a just-verified local file.
func (e Engine) downloadThumbnail(destination string) error {
	label, ok := medianame.EpisodeLabel(filepath.Base(destination))
	if !ok {
		return nil
	}
	image, err := thumbnail.Generate(label)
	if err != nil {
		return verrors.Wrap(verrors.CodeTransferFailed, "could not generate a placeholder thumbnail", err)
	}
	thumbnailPath := strings.TrimSuffix(destination, filepath.Ext(destination)) + ".jpg"
	if err := os.WriteFile(thumbnailPath, image, 0o644); err != nil {
		return verrors.Wrap(verrors.CodeDestinationNotWritable, "could not write the placeholder thumbnail", err)
	}
	return nil
}

// remotePrefixSHA256 fingerprints the first limit bytes of a remote file,
// mirroring the local checksum.PrefixSHA256 call copyFile makes on a local
// source, so resume compatibility can be judged the same way in reverse.
func (e Engine) remotePrefixSHA256(ctx context.Context, source string, limit int64) (string, int64, error) {
	reader, err := e.Remote.Open(source)
	if err != nil {
		return "", 0, err
	}
	defer reader.Close()
	return checksum.PrefixSHA256(&contextReader{ctx: ctx, reader: reader}, limit)
}

// prepareLocalPartial mirrors preparePartial with the resumable partial file
// on local disk instead of the remote server, validating its prefix against
// the remote source instead of a local one.
func (e Engine) prepareLocalPartial(ctx context.Context, source, partialPath, sidecarPath string, expected PartialMetadata, resume bool) (int64, error) {
	partialInfo, partialErr := os.Lstat(partialPath)
	if partialErr == nil && partialInfo.Mode()&os.ModeSymlink != 0 {
		return 0, verrors.New(verrors.CodeUnsupportedFileType,
			fmt.Sprintf("local symbolic link %q is not followed", partialPath))
	}
	if partialErr == nil && resume {
		stored, err := readLocalMetadata(sidecarPath)
		if err != nil || !ResumeCompatible(expected, stored, partialInfo.Size()) {
			return 0, verrors.New(verrors.CodeResumeIncompatible,
				"existing partial state does not match this source").WithHint(
				"Retry without --resume to replace Vericopy's stale partial state.")
		}
		validationBytes := min(expected.PrefixBytes, partialInfo.Size())
		partialReader, err := os.Open(partialPath)
		if err != nil {
			return 0, err
		}
		localPrefix, _, hashErr := checksum.PrefixSHA256(partialReader, validationBytes)
		_ = partialReader.Close()
		if hashErr != nil {
			return 0, hashErr
		}
		remoteReader, err := e.Remote.Open(source)
		if err != nil {
			return 0, err
		}
		remotePrefix, _, hashErr := checksum.PrefixSHA256(&contextReader{ctx: ctx, reader: remoteReader}, validationBytes)
		_ = remoteReader.Close()
		if hashErr != nil {
			return 0, hashErr
		}
		if remotePrefix != localPrefix {
			return 0, verrors.New(verrors.CodeResumeIncompatible, "the local partial prefix does not match the remote source")
		}
		return partialInfo.Size(), nil
	}
	if partialErr != nil && !os.IsNotExist(partialErr) {
		return 0, verrors.Wrap(verrors.CodeDestinationNotWritable, "partial state could not be inspected", partialErr)
	}
	if partialErr == nil {
		if err := os.Remove(partialPath); err != nil {
			return 0, verrors.Wrap(verrors.CodeDestinationNotWritable, "stale partial state could not be removed", err)
		}
		_ = os.Remove(sidecarPath)
	} else if sidecarInfo, sidecarErr := os.Lstat(sidecarPath); sidecarErr == nil {
		if sidecarInfo.Mode()&os.ModeSymlink != 0 {
			return 0, verrors.New(verrors.CodeUnsupportedFileType,
				fmt.Sprintf("local symbolic link %q is not followed", sidecarPath))
		}
		if err := os.Remove(sidecarPath); err != nil {
			return 0, verrors.Wrap(verrors.CodeDestinationNotWritable, "stale resume metadata could not be removed", err)
		}
	} else if !os.IsNotExist(sidecarErr) {
		return 0, verrors.Wrap(verrors.CodeDestinationNotWritable, "resume metadata could not be inspected", sidecarErr)
	}
	if err := writeLocalMetadata(sidecarPath, expected); err != nil {
		return 0, err
	}
	file, err := os.OpenFile(partialPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, verrors.Wrap(verrors.CodeDestinationNotWritable, "could not create a restrictive partial file", err)
	}
	_ = file.Close()
	return 0, nil
}

// download streams a remote source into a local partial file starting at
// offset, mirroring upload's resume-by-rehashing-the-prefix approach in
// reverse: the remote reader is not seekable, so a resumed download rehashes
// its own already-received prefix rather than seeking past it.
func (e Engine) download(ctx context.Context, source, partialPath string, offset int64, progress func(int64)) (string, int64, error) {
	remote, err := e.Remote.Open(source)
	if err != nil {
		return "", 0, verrors.Wrap(verrors.CodeInvalidRemoteSource, "the remote source could not be opened", err)
	}
	defer remote.Close()
	hash := sha256.New()
	if offset > 0 {
		if _, err := io.CopyN(hash, &contextReader{ctx: ctx, reader: remote}, offset); err != nil {
			return "", 0, verrors.Wrap(verrors.CodeTransferFailed, "could not hash the resumed source prefix", err)
		}
	}
	local, err := os.OpenFile(partialPath, os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, verrors.Wrap(verrors.CodeDestinationNotWritable, "could not open the local partial file", err)
	}
	defer local.Close()
	if _, err := local.Seek(offset, io.SeekStart); err != nil {
		return "", 0, verrors.Wrap(verrors.CodeTransferFailed, "could not seek the local partial file for resume", err)
	}
	written, err := io.Copy(&progressWriter{
		writer: io.MultiWriter(local, hash),
		onWrite: func(written int64) {
			if progress != nil {
				progress(offset + written)
			}
		},
	}, &contextReader{ctx: ctx, reader: remote})
	if err != nil {
		if ctx.Err() != nil {
			return "", offset + written, verrors.Wrap(verrors.CodeTransferInterrupted,
				"the transfer was interrupted; compatible partial state was retained", ctx.Err())
		}
		return "", offset + written, verrors.Wrap(verrors.CodeTransferInterrupted,
			"the connection ended before download completed; compatible partial state was retained", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), offset + written, nil
}

func writeLocalMetadata(filename string, metadata PartialMetadata) error {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return verrors.Wrap(verrors.CodeDestinationNotWritable, "could not create resume metadata", err)
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

func readLocalMetadata(filename string) (PartialMetadata, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return PartialMetadata{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return PartialMetadata{}, verrors.New(verrors.CodeUnsupportedFileType,
			fmt.Sprintf("local symbolic link %q is not followed", filename))
	}
	file, err := os.Open(filename)
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

// validateLocalParents checks that every existing ancestor directory from
// the root down to destination's immediate parent is usable as a directory.
// Unlike validateRemoteParents, it deliberately follows symlinks (os.Stat,
// not os.Lstat): an ordinary local machine routinely has symlinked system
// directories (e.g. macOS's /var and /tmp point into /private), and that is
// not the same trust boundary as a remote SSH server redirecting writes, so
// rejecting a symlinked local ancestor would just be a false positive.
func validateLocalParents(destination string) error {
	parent := filepath.Dir(filepath.Clean(destination))
	components := make([]string, 0)
	current := parent
	for {
		components = append(components, current)
		next := filepath.Dir(current)
		if next == current {
			break
		}
		current = next
	}
	for i, j := 0, len(components)-1; i < j; i, j = i+1, j-1 {
		components[i], components[j] = components[j], components[i]
	}
	for _, component := range components {
		info, err := os.Stat(component)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return verrors.Wrap(verrors.CodeDestinationNotWritable,
				fmt.Sprintf("local parent %q could not be inspected", component), err)
		}
		if !info.IsDir() {
			return verrors.New(verrors.CodeDestinationNotWritable,
				fmt.Sprintf("local parent %q is not a directory", component))
		}
	}
	return nil
}

// localPartialPaths mirrors PartialPaths using filepath instead of path, so
// the hidden partial file lands next to the local destination correctly on
// every OS path dialect instead of assuming POSIX separators.
func localPartialPaths(destination string, metadata PartialMetadata) (partial, sidecar string) {
	identity := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s", destination, metadata.SourceSize, metadata.PrefixSHA256)))
	suffix := hex.EncodeToString(identity[:8])
	directory, base := filepath.Split(destination)
	partial = filepath.Join(directory, "."+base+".vericopy-"+suffix+".partial")
	return partial, partial + ".json"
}
