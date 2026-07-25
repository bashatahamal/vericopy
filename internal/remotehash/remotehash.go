// Package remotehash computes a remote file's SHA-256 digest by running a
// hash utility on the SSH server, instead of reading the whole file back
// over SFTP just to hash it locally. Verification bandwidth otherwise
// doubles a transfer's network cost: the server already holds the bytes and
// can hash them against local disk far faster than the file can cross the
// network a second time.
//
// This intentionally does not replace the byte-for-byte SFTP read-back
// verification; it is an optional, best-effort accelerator that callers
// should treat as advisory. Any failure, unavailability, or mismatch should
// fall back to reading the file back, which remains the authoritative check.
package remotehash

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var sha256HexPattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// remoteHashCommands are tried in order until one produces a well-formed
// digest line. "--" ends option parsing so a filename that starts with "-"
// is never mistaken for a flag.
var remoteHashCommands = []string{
	"sha256sum -- %s",
	"shasum -a 256 -- %s",
}

// CommandRunner executes a fixed remote command over an existing SSH
// connection and returns its combined output. github.com/bashatahamal/vericopy/internal/access.SSHRunner
// already satisfies this shape.
type CommandRunner interface {
	Run(command string) ([]byte, error)
}

// Hasher computes remote SHA-256 digests via CommandRunner. The zero value
// with a nil Runner always reports the capability unavailable.
type Hasher struct {
	Runner CommandRunner
}

// HashSHA256 returns the remote SHA-256 digest for path. ok is false when no
// supported remote hash utility was available or the path could not be
// safely passed to a shell command; that is not an error, and callers
// should silently fall back to reading the file back and hashing it
// locally. A non-nil error only reports context cancellation.
func (h Hasher) HashSHA256(ctx context.Context, path string) (digest string, ok bool, err error) {
	if h.Runner == nil {
		return "", false, nil
	}
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	quoted, err := shellQuote(path)
	if err != nil {
		return "", false, nil
	}
	for _, template := range remoteHashCommands {
		output, runErr := h.Runner.Run(fmt.Sprintf(template, quoted))
		if runErr != nil {
			continue
		}
		if digest, matched := parseDigestLine(output); matched {
			return digest, true, nil
		}
	}
	return "", false, nil
}

// parseDigestLine extracts a 64-character hex digest from the first field of
// sha256sum/shasum-style output ("<digest>  <filename>"). The echoed
// filename is never trusted or compared; the caller already knows which
// path it asked about.
func parseDigestLine(output []byte) (string, bool) {
	fields := strings.Fields(string(output))
	if len(fields) == 0 {
		return "", false
	}
	candidate := strings.ToLower(fields[0])
	if !sha256HexPattern.MatchString(candidate) {
		return "", false
	}
	return candidate, true
}

// shellQuote wraps path in single quotes so a POSIX shell treats it as one
// literal argument, escaping embedded single quotes. SSH's exec
// request carries a single command string that the server hands to a shell
// (RFC 4254 6.5) rather than an argv array, so this is the standard safe way
// to pass an untrusted string through it. It rejects NUL bytes, which cannot
// be represented in a shell argument at all.
func shellQuote(path string) (string, error) {
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("remotehash: path contains a NUL byte")
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'", nil
}
