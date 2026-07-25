// Package credentialstore is the only place a password may be written to
// persistent storage, and only when the user explicitly opts in. It never
// writes to Vericopy's own files: passwords go to the operating system's own
// secure credential store (Windows Credential Manager, macOS Keychain,
// Linux Secret Service), keyed by session name, so removing Vericopy or its
// local state does not leave a plaintext secret behind.
package credentialstore

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const service = "Vericopy"

// ErrNotFound reports that no password is stored for the given session.
var ErrNotFound = keyring.ErrNotFound

// Save stores password for the given saved-session name, replacing any
// existing entry. sessionName must already be validated (non-empty); it is
// used verbatim as the keyring account identifier.
func Save(sessionName, password string) error {
	if sessionName == "" {
		return errors.New("credentialstore: a session name is required")
	}
	if err := keyring.Set(service, sessionName, password); err != nil {
		return fmt.Errorf("credentialstore: could not store the password: %w", err)
	}
	return nil
}

// Load returns the stored password for sessionName, or ErrNotFound if none
// is stored (not stored is expected and not itself an error condition for
// callers to treat as fatal).
func Load(sessionName string) (string, error) {
	password, err := keyring.Get(service, sessionName)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("credentialstore: could not read the stored password: %w", err)
	}
	return password, nil
}

// Delete removes any stored password for sessionName. Deleting a name with
// no stored password is not an error.
func Delete(sessionName string) error {
	if err := keyring.Delete(service, sessionName); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("credentialstore: could not remove the stored password: %w", err)
	}
	return nil
}
