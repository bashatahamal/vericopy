package desktop

import (
	"context"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	nativesftp "github.com/bashatahamal/vericopy/internal/backend/sftp"
	"github.com/bashatahamal/vericopy/internal/medianame"
	"github.com/bashatahamal/vericopy/internal/sshclient"
	"github.com/bashatahamal/vericopy/internal/thumbnail"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

// BrowseEntry is one file or directory shown in the browse view.
type BrowseEntry struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

// BrowseListing is a directory's contents plus enough context to navigate.
// Parent is empty at the root of the browsed filesystem (a drive root
// locally, or "/" remotely).
type BrowseListing struct {
	Path    string        `json:"path"`
	Parent  string        `json:"parent,omitempty"`
	Entries []BrowseEntry `json:"entries"`
}

// BrowseDeleteFailure reports one path that could not be deleted; other
// requested paths may still have succeeded.
type BrowseDeleteFailure struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// BrowseDeleteResult reports what a delete request actually did. Deletion
// proceeds independently per path: one failure does not stop the rest.
type BrowseDeleteResult struct {
	Deleted  int                   `json:"deleted"`
	Failures []BrowseDeleteFailure `json:"failures,omitempty"`
}

func sortBrowseEntries(entries []BrowseEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

// ListLocalDirectory lists a local directory for the browse view. Entries
// that cannot be individually inspected (e.g. a broken symlink) are skipped
// rather than failing the whole listing.
func (s *Service) ListLocalDirectory(dirPath string) (BrowseListing, error) {
	cleaned := filepath.Clean(dirPath)
	info, err := os.Stat(cleaned)
	if err != nil {
		return BrowseListing{}, verrors.Wrap(verrors.CodeInvalidLocalPath, "could not inspect the local path", err)
	}
	if !info.IsDir() {
		return BrowseListing{}, verrors.New(verrors.CodeInvalidLocalPath, "the local path is not a directory")
	}
	entries, err := os.ReadDir(cleaned)
	if err != nil {
		return BrowseListing{}, verrors.Wrap(verrors.CodeInvalidLocalPath, "could not list the local directory", err)
	}
	listing := BrowseListing{Path: cleaned}
	if parent := filepath.Dir(cleaned); parent != cleaned {
		listing.Parent = parent
	}
	for _, entry := range entries {
		entryInfo, err := entry.Info()
		if err != nil {
			continue
		}
		listing.Entries = append(listing.Entries, BrowseEntry{
			Name: entry.Name(), IsDir: entry.IsDir(), Size: entryInfo.Size(), ModTime: entryInfo.ModTime(),
		})
	}
	sortBrowseEntries(listing.Entries)
	return listing, nil
}

// DeleteLocalPaths permanently deletes each given local path, recursing
// into directories. This is destructive and irreversible; callers must
// confirm with the user before calling it.
func (s *Service) DeleteLocalPaths(paths []string) BrowseDeleteResult {
	result := BrowseDeleteResult{}
	for _, target := range paths {
		cleaned := filepath.Clean(target)
		if err := os.RemoveAll(cleaned); err != nil {
			result.Failures = append(result.Failures, BrowseDeleteFailure{Path: cleaned, Message: err.Error()})
			continue
		}
		result.Deleted++
	}
	return result
}

// RemoteBrowseRequest carries enough of a transfer request's shape to open
// a connection for browsing or deleting on a remote server. Password is
// never persisted; it exists only for the duration of the connection this
// request opens.
type RemoteBrowseRequest struct {
	Destination    string `json:"destination"`
	Port           int    `json:"port"`
	Authentication string `json:"authentication,omitempty"`
	Password       string `json:"password,omitempty"`
	Identity       string `json:"identity,omitempty"`
	KnownHosts     string `json:"known_hosts,omitempty"`
}

func (s *Service) dialForBrowsing(request RemoteBrowseRequest) (*nativesftp.Client, func(), error) {
	password := request.Password
	request.Password = ""
	destination, err := parseBrowseDestination(request.Destination)
	if err != nil {
		return nil, nil, err
	}
	port := request.Port
	if port == 0 {
		port = 22
	}
	knownHosts := request.KnownHosts
	if knownHosts == "" {
		knownHosts = sshclient.DefaultKnownHosts()
	}
	authentication := request.Authentication
	if authentication == "" {
		authentication = sshclient.AuthenticationKey
	}
	if authentication == sshclient.AuthenticationPassword && password == "" {
		return nil, nil, verrors.New(verrors.CodeAuthenticationFailed, "enter the SSH password before browsing this destination")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sshConnection, err := sshclient.Dial(ctx, sshclient.Options{
		User: destination.user, Host: destination.host, Port: port,
		KnownHosts: knownHosts, Identity: request.Identity,
		Authentication: authentication, Password: password, Timeout: 15 * time.Second,
	})
	password = ""
	if err != nil {
		return nil, nil, err
	}
	remoteFS, err := nativesftp.New(sshConnection)
	if err != nil {
		_ = sshConnection.Close()
		return nil, nil, verrors.Wrap(verrors.CodeConnectionFailed, "the server did not accept the SFTP subsystem", err)
	}
	cleanup := func() {
		_ = remoteFS.Close()
		_ = sshConnection.Close()
	}
	return remoteFS, cleanup, nil
}

type browseDestination struct {
	user, host, path string
}

// parseBrowseDestination accepts either a full "user@host:/path" reference
// (from a fresh browse) or a bare absolute path (once already browsing, so
// the caller does not need to resend user/host on every navigation).
func parseBrowseDestination(raw string) (browseDestination, error) {
	if strings.HasPrefix(raw, "/") {
		return browseDestination{path: raw}, nil
	}
	at := strings.Index(raw, "@")
	colon := strings.Index(raw, ":")
	if at <= 0 || colon <= at {
		return browseDestination{}, verrors.New(verrors.CodeInvalidArguments,
			"the destination must look like user@host:/absolute/path")
	}
	return browseDestination{user: raw[:at], host: raw[at+1 : colon], path: raw[colon+1:]}, nil
}

// ListRemoteDirectory connects and lists one remote directory. request.Destination
// is the directory to list.
func (s *Service) ListRemoteDirectory(request RemoteBrowseRequest) (BrowseListing, error) {
	destination, err := parseBrowseDestination(request.Destination)
	if err != nil {
		return BrowseListing{}, err
	}
	remoteFS, cleanup, err := s.dialForBrowsing(request)
	if err != nil {
		return BrowseListing{}, err
	}
	defer cleanup()

	cleaned := path.Clean(destination.path)
	info, err := remoteFS.Stat(cleaned)
	if err != nil {
		return BrowseListing{}, verrors.Wrap(verrors.CodeDestinationNotReadable, "could not inspect the remote path", err)
	}
	if !info.IsDir() {
		return BrowseListing{}, verrors.New(verrors.CodeInvalidRemoteDestination, "the remote path is not a directory")
	}
	entries, err := remoteFS.ReadDir(cleaned)
	if err != nil {
		return BrowseListing{}, verrors.Wrap(verrors.CodeDestinationNotReadable, "could not list the remote directory", err)
	}
	listing := BrowseListing{Path: cleaned}
	if parent := path.Dir(cleaned); parent != cleaned {
		listing.Parent = parent
	}
	for _, entry := range entries {
		listing.Entries = append(listing.Entries, BrowseEntry{
			Name: entry.Name(), IsDir: entry.IsDir(), Size: entry.Size(), ModTime: entry.ModTime(),
		})
	}
	sortBrowseEntries(listing.Entries)
	return listing, nil
}

// DeleteRemotePaths connects once and permanently deletes each given
// absolute remote path, recursing into directories. This is destructive and
// irreversible; callers must confirm with the user before calling it.
func (s *Service) DeleteRemotePaths(request RemoteBrowseRequest, paths []string) (BrowseDeleteResult, error) {
	remoteFS, cleanup, err := s.dialForBrowsing(request)
	if err != nil {
		return BrowseDeleteResult{}, err
	}
	defer cleanup()

	result := BrowseDeleteResult{}
	for _, target := range paths {
		cleaned := path.Clean(target)
		if err := remoteFS.RemoveAll(cleaned); err != nil {
			result.Failures = append(result.Failures, BrowseDeleteFailure{Path: cleaned, Message: err.Error()})
			continue
		}
		result.Deleted++
	}
	return result, nil
}

// BrowseRenameOutcome reports what happened to one path passed to
// FixRemoteMediaNames. Renamed is false both when the name already had no
// collision to fix and when the rename itself failed; Message distinguishes
// the two.
type BrowseRenameOutcome struct {
	Path    string `json:"path"`
	NewPath string `json:"new_path,omitempty"`
	Renamed bool   `json:"renamed"`
	Message string `json:"message,omitempty"`
}

// BrowseRenameResult reports the outcome for every path FixRemoteMediaNames
// was asked to fix.
type BrowseRenameResult struct {
	Outcomes []BrowseRenameOutcome `json:"outcomes"`
}

// FixRemoteMediaNames applies the same year/SxxExx collision fix Copy can
// apply during a transfer (see medianame.RewriteFilename) directly to files
// already sitting on the remote server, so a file copied before the fix
// existed does not need a fresh transfer just to be renamed.
func (s *Service) FixRemoteMediaNames(request RemoteBrowseRequest, paths []string) (BrowseRenameResult, error) {
	remoteFS, cleanup, err := s.dialForBrowsing(request)
	if err != nil {
		return BrowseRenameResult{}, err
	}
	defer cleanup()

	result := BrowseRenameResult{}
	for _, target := range paths {
		cleaned := path.Clean(target)
		dir, base := path.Split(cleaned)
		outcome := BrowseRenameOutcome{Path: cleaned}
		renamed, ok := medianame.RewriteFilename(base)
		if !ok {
			outcome.Message = "no colliding release year found; the name was left unchanged"
			result.Outcomes = append(result.Outcomes, outcome)
			continue
		}
		newPath := path.Join(dir, renamed)
		if err := remoteFS.Rename(cleaned, newPath); err != nil {
			outcome.Message = err.Error()
			result.Outcomes = append(result.Outcomes, outcome)
			continue
		}
		outcome.Renamed = true
		outcome.NewPath = newPath
		result.Outcomes = append(result.Outcomes, outcome)
	}
	return result, nil
}

// BrowseThumbnailOutcome reports what happened to one path passed to
// GenerateRemoteThumbnails.
type BrowseThumbnailOutcome struct {
	Path      string `json:"path"`
	ThumbPath string `json:"thumb_path,omitempty"`
	Generated bool   `json:"generated"`
	Message   string `json:"message,omitempty"`
}

// BrowseThumbnailResult reports the outcome for every path
// GenerateRemoteThumbnails was asked to generate a thumbnail for.
type BrowseThumbnailResult struct {
	Outcomes []BrowseThumbnailOutcome `json:"outcomes"`
}

// GenerateRemoteThumbnails writes a placeholder "<name>.jpg" next to each
// recognized episode file already on the remote server (see the thumbnail
// package). That exact-name match is the local-image convention Jellyfin
// and Emby use for an item's Primary (poster/grid) image, so it replaces a
// frame extracted from inside the video rather than only affecting an
// unrelated image slot. Files without a recognizable season/episode tag are
// reported, not treated as an error.
func (s *Service) GenerateRemoteThumbnails(request RemoteBrowseRequest, paths []string) (BrowseThumbnailResult, error) {
	remoteFS, cleanup, err := s.dialForBrowsing(request)
	if err != nil {
		return BrowseThumbnailResult{}, err
	}
	defer cleanup()

	result := BrowseThumbnailResult{}
	for _, target := range paths {
		cleaned := path.Clean(target)
		outcome := BrowseThumbnailOutcome{Path: cleaned}
		label, ok := medianame.EpisodeLabel(path.Base(cleaned))
		if !ok {
			outcome.Message = "not a recognized video episode file"
			result.Outcomes = append(result.Outcomes, outcome)
			continue
		}
		image, err := thumbnail.Generate(label)
		if err != nil {
			outcome.Message = err.Error()
			result.Outcomes = append(result.Outcomes, outcome)
			continue
		}
		mode := fs.FileMode(0o644)
		if info, statErr := remoteFS.Stat(cleaned); statErr == nil {
			mode = info.Mode().Perm()
		}
		thumbPath := strings.TrimSuffix(cleaned, path.Ext(cleaned)) + ".jpg"
		file, err := remoteFS.OpenFile(thumbPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY)
		if err != nil {
			outcome.Message = err.Error()
			result.Outcomes = append(result.Outcomes, outcome)
			continue
		}
		_, writeErr := file.Write(image)
		closeErr := file.Close()
		if writeErr != nil {
			outcome.Message = writeErr.Error()
			result.Outcomes = append(result.Outcomes, outcome)
			continue
		}
		if closeErr != nil {
			outcome.Message = closeErr.Error()
			result.Outcomes = append(result.Outcomes, outcome)
			continue
		}
		_ = remoteFS.Chmod(thumbPath, mode)
		outcome.Generated = true
		outcome.ThumbPath = thumbPath
		result.Outcomes = append(result.Outcomes, outcome)
	}
	return result, nil
}
