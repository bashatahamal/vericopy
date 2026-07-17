package access_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	pkgSFTP "github.com/pkg/sftp"

	"github.com/bashatahamal/vericopy/internal/access"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

type runner map[string]string

func (r runner) Run(command string) ([]byte, error) {
	value, ok := r[command]
	if !ok {
		return nil, errors.New("unexpected command")
	}
	return []byte(value), nil
}

type metadataFS map[string]fs.FileInfo

func (m metadataFS) Lstat(name string) (fs.FileInfo, error) {
	info, ok := m[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return info, nil
}

type metadataInfo struct {
	name string
	mode fs.FileMode
	uid  uint32
	gid  uint32
}

func (i metadataInfo) Name() string       { return i.name }
func (i metadataInfo) Size() int64        { return 0 }
func (i metadataInfo) Mode() fs.FileMode  { return i.mode }
func (i metadataInfo) ModTime() time.Time { return time.Time{} }
func (i metadataInfo) IsDir() bool        { return i.mode.IsDir() }
func (i metadataInfo) Sys() any           { return &pkgSFTP.FileStat{UID: i.uid, GID: i.gid} }

func TestValidateAccountName(t *testing.T) {
	for _, valid := range []string{"jellyfin", "media-reader", "svc_media.1"} {
		if err := access.ValidateAccountName(valid); err != nil {
			t.Fatalf("valid name %q rejected: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "root;id", "name with space", "$(id)"} {
		if err := access.ValidateAccountName(invalid); err == nil {
			t.Fatalf("invalid name %q accepted", invalid)
		}
	}
}

func TestResolveUserUsesValidatedFixedCommands(t *testing.T) {
	resolver := access.Resolver{Runner: runner{"id -u jellyfin": "998\n", "id -G jellyfin": "998 44\n"}}
	identity, err := resolver.User(context.Background(), "jellyfin")
	if err != nil {
		t.Fatal(err)
	}
	if identity.UID != 998 || len(identity.Groups) != 2 || identity.Groups[1] != 44 {
		t.Fatalf("unexpected identity: %#v", identity)
	}
	_, err = resolver.User(context.Background(), "jellyfin; id")
	if err == nil || !strings.Contains(err.Error(), "INVALID_ARGUMENTS") {
		t.Fatalf("unsafe name was not rejected: %v", err)
	}
}

func TestAccessCheckUsesParentTraverseAndTargetRead(t *testing.T) {
	remote := metadataFS{
		"/":               metadataInfo{name: "/", mode: fs.ModeDir | 0o755, uid: 0, gid: 0},
		"/media":          metadataInfo{name: "media", mode: fs.ModeDir | 0o750, uid: 1000, gid: 44},
		"/media/Film.mkv": metadataInfo{name: "Film.mkv", mode: 0o640, uid: 1000, gid: 44},
	}
	identity := access.Identity{Name: "jellyfin", UID: 998, Groups: []uint32{44}}
	report, err := access.Check(context.Background(), remote, "/media/Film.mkv", identity)
	if err != nil || !report.Readable {
		t.Fatalf("group access rejected: report=%#v err=%v", report, err)
	}

	remote["/media"] = metadataInfo{name: "media", mode: fs.ModeDir | 0o740, uid: 1000, gid: 44}
	report, err = access.Check(context.Background(), remote, "/media/Film.mkv", identity)
	if err == nil || report.Readable || verrors.As(err).Code != verrors.CodeDestinationNotReadable {
		t.Fatalf("missing traverse permission accepted: report=%#v err=%v", report, err)
	}
}
