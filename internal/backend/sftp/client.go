package sftp

import (
	"io"
	"io/fs"
	"time"

	pkgSFTP "github.com/pkg/sftp"

	"github.com/bashatahamal/vericopy/internal/sshclient"
)

// Client wraps the native SFTP implementation behind a testable filesystem surface.
type Client struct {
	client *pkgSFTP.Client
}

func New(sshConnection *sshclient.Client) (*Client, error) {
	client, err := pkgSFTP.NewClient(sshConnection.Client)
	if err != nil {
		return nil, err
	}
	return &Client{client: client}, nil
}

func (c *Client) Close() error                            { return c.client.Close() }
func (c *Client) Open(path string) (io.ReadCloser, error) { return c.client.Open(path) }
func (c *Client) OpenFile(path string, flags int) (File, error) {
	return c.client.OpenFile(path, flags)
}
func (c *Client) Stat(path string) (fs.FileInfo, error)     { return c.client.Stat(path) }
func (c *Client) Lstat(path string) (fs.FileInfo, error)    { return c.client.Lstat(path) }
func (c *Client) Mkdir(path string) error                   { return c.client.Mkdir(path) }
func (c *Client) MkdirAll(path string) error                { return c.client.MkdirAll(path) }
func (c *Client) Chmod(path string, mode fs.FileMode) error { return c.client.Chmod(path, mode) }
func (c *Client) Chown(path string, uid, gid int) error     { return c.client.Chown(path, uid, gid) }
func (c *Client) Chtimes(path string, atime, mtime time.Time) error {
	return c.client.Chtimes(path, atime, mtime)
}
func (c *Client) Rename(oldPath, newPath string) error       { return c.client.Rename(oldPath, newPath) }
func (c *Client) Remove(path string) error                   { return c.client.Remove(path) }
func (c *Client) ReadDir(path string) ([]fs.FileInfo, error) { return c.client.ReadDir(path) }

// RemoveAll deletes path, recursing into and removing a directory's
// contents first. It is destructive and irreversible.
func (c *Client) RemoveAll(path string) error { return c.client.RemoveAll(path) }

// File is the seekable remote file contract needed for resumable uploads.
type File interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer
	Stat() (fs.FileInfo, error)
}
