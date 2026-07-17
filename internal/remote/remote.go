package remote

import (
	"net"
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/bashatahamal/vericopy/internal/localpath"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

var (
	userPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,63}$`)
	hostPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
)

// Destination is a parsed SSH destination.
type Destination struct {
	User string `json:"user,omitempty"`
	Host string `json:"host"`
	Path string `json:"path"`
}

func hasControl(value string) bool { return strings.IndexFunc(value, unicode.IsControl) >= 0 }

// Parse parses [user@]host:path without confusing Windows drive colons.
func Parse(value string) (Destination, error) {
	if value == "" || localpath.IsWindowsLike(value) {
		return Destination{}, verrors.New(verrors.CodeInvalidRemoteDestination,
			"the destination must use [user@]host:path syntax")
	}
	if hasControl(value) {
		return Destination{}, verrors.New(verrors.CodeInvalidRemoteDestination,
			"the destination contains control characters")
	}

	colon := destinationColon(value)
	if colon < 1 || colon == len(value)-1 {
		return Destination{}, verrors.New(verrors.CodeInvalidRemoteDestination,
			"the destination must include a host and remote path")
	}
	endpoint, remotePath := value[:colon], value[colon+1:]
	user, host := splitEndpoint(endpoint)
	if user != "" && !userPattern.MatchString(user) {
		return Destination{}, verrors.New(verrors.CodeInvalidRemoteDestination, "the SSH user name is invalid")
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		if net.ParseIP(host) == nil {
			return Destination{}, verrors.New(verrors.CodeInvalidRemoteDestination, "the bracketed host is not a valid IP address")
		}
	}
	if host == "" || (!hostPattern.MatchString(host) && net.ParseIP(host) == nil) {
		return Destination{}, verrors.New(verrors.CodeInvalidRemoteDestination, "the SSH host is invalid")
	}
	for _, segment := range strings.Split(strings.ReplaceAll(remotePath, `\`, "/"), "/") {
		if segment == ".." {
			return Destination{}, verrors.New(verrors.CodeInvalidRemoteDestination, "remote path traversal using '..' is not allowed")
		}
	}
	cleaned := path.Clean(remotePath)
	if cleaned == "." {
		return Destination{}, verrors.New(verrors.CodeInvalidRemoteDestination, "the remote path is empty")
	}
	return Destination{User: user, Host: host, Path: cleaned}, nil
}

func destinationColon(value string) int {
	if strings.HasPrefix(value, "[") || strings.Contains(value, "@[") {
		closeBracket := strings.Index(value, "]")
		if closeBracket >= 0 && closeBracket+1 < len(value) && value[closeBracket+1] == ':' {
			return closeBracket + 1
		}
	}
	return strings.IndexByte(value, ':')
}

func splitEndpoint(endpoint string) (string, string) {
	if at := strings.LastIndexByte(endpoint, '@'); at >= 0 {
		return endpoint[:at], endpoint[at+1:]
	}
	return "", endpoint
}
