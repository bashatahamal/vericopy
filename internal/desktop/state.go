package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/bashatahamal/vericopy/internal/remote"
	"github.com/bashatahamal/vericopy/internal/sshclient"
	"github.com/bashatahamal/vericopy/internal/verrors"
)

const (
	stateSchema       = 1
	maxHistoryEntries = 100
)

// ConnectionProfile stores only non-secret connection references. Source paths
// and identity key paths are intentionally never persisted.
type ConnectionProfile struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Destination string    `json:"destination"`
	Port        int       `json:"port"`
	KnownHosts  string    `json:"known_hosts"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TransferHistoryEntry is a redacted, local audit record. It never contains a
// full local source path, identity key path, known_hosts path, or file digest.
type TransferHistoryEntry struct {
	ID             string       `json:"id"`
	StartedAt      time.Time    `json:"started_at"`
	CompletedAt    time.Time    `json:"completed_at"`
	Status         string       `json:"status"`
	SourceName     string       `json:"source_name"`
	Destination    string       `json:"destination"`
	Files          int          `json:"files,omitempty"`
	Bytes          int64        `json:"bytes,omitempty"`
	ResumedBytes   int64        `json:"resumed_bytes,omitempty"`
	Verified       bool         `json:"verified"`
	DiagnosticCode verrors.Code `json:"diagnostic_code,omitempty"`
}

type persistedState struct {
	Schema   int                    `json:"schema"`
	Profiles []ConnectionProfile    `json:"profiles"`
	History  []TransferHistoryEntry `json:"history"`
}

// StateStore keeps optional desktop convenience data in the user's local
// configuration directory. Writes are atomic and restricted to the user.
type StateStore struct {
	mu   sync.Mutex
	path string
}

// DefaultStatePath returns the local, user-scoped state location.
func DefaultStatePath() (string, error) {
	configuration, err := os.UserConfigDir()
	if err != nil {
		return "", verrors.Wrap(verrors.CodeInternal, "could not locate the local configuration directory", err)
	}
	return filepath.Join(configuration, "vericopy", "desktop-state.json"), nil
}

func newStateStore(filename string) *StateStore {
	return &StateStore{path: filename}
}

// ListProfiles returns saved connection references in a stable display order.
func (s *StateStore) ListProfiles() ([]ConnectionProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	profiles := append([]ConnectionProfile(nil), state.Profiles...)
	sort.Slice(profiles, func(left, right int) bool {
		return strings.ToLower(profiles[left].Name) < strings.ToLower(profiles[right].Name)
	})
	return profiles, nil
}

// SaveProfile creates or replaces a non-secret connection reference.
func (s *StateStore) SaveProfile(profile ConnectionProfile) (ConnectionProfile, error) {
	profile, err := normalizeProfile(profile)
	if err != nil {
		return ConnectionProfile{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readLocked()
	if err != nil {
		return ConnectionProfile{}, err
	}
	if profile.ID == "" {
		profile.ID, err = randomID()
		if err != nil {
			return ConnectionProfile{}, err
		}
	}
	profile.UpdatedAt = time.Now().UTC()
	updated := false
	for index, existing := range state.Profiles {
		if existing.ID == profile.ID {
			state.Profiles[index] = profile
			updated = true
			break
		}
	}
	if !updated {
		state.Profiles = append(state.Profiles, profile)
	}
	if err := s.writeLocked(state); err != nil {
		return ConnectionProfile{}, err
	}
	return profile, nil
}

// DeleteProfile removes one saved connection reference and reports whether it
// existed. It has no effect on any key, host-key file, or transfer state.
func (s *StateStore) DeleteProfile(id string) (bool, error) {
	if id == "" {
		return false, verrors.New(verrors.CodeInvalidArguments, "the profile identifier is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readLocked()
	if err != nil {
		return false, err
	}
	profiles := state.Profiles[:0]
	removed := false
	for _, profile := range state.Profiles {
		if profile.ID == id {
			removed = true
			continue
		}
		profiles = append(profiles, profile)
	}
	if !removed {
		return false, nil
	}
	state.Profiles = profiles
	if err := s.writeLocked(state); err != nil {
		return false, err
	}
	return true, nil
}

// ListTransferHistory returns the newest local audit entries first.
func (s *StateStore) ListTransferHistory() ([]TransferHistoryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	history := append([]TransferHistoryEntry(nil), state.History...)
	sort.Slice(history, func(left, right int) bool {
		return history[left].CompletedAt.After(history[right].CompletedAt)
	})
	return history, nil
}

// ClearTransferHistory removes only the local redacted audit entries.
func (s *StateStore) ClearTransferHistory() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readLocked()
	if err != nil {
		return err
	}
	if len(state.History) == 0 {
		return nil
	}
	state.History = nil
	return s.writeLocked(state)
}

func (s *StateStore) recordHistory(entry TransferHistoryEntry) error {
	entry.ID, _ = randomID()
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("entry-%d", time.Now().UnixNano())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.readLocked()
	if err != nil {
		return err
	}
	state.History = append(state.History, entry)
	if len(state.History) > maxHistoryEntries {
		state.History = state.History[len(state.History)-maxHistoryEntries:]
	}
	return s.writeLocked(state)
}

func (s *StateStore) readLocked() (persistedState, error) {
	if s == nil || s.path == "" {
		return persistedState{}, verrors.New(verrors.CodeInternal, "the local desktop state path is unavailable")
	}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return persistedState{Schema: stateSchema}, nil
	}
	if err != nil {
		return persistedState{}, verrors.Wrap(verrors.CodeInternal, "could not read local desktop state", err)
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return persistedState{}, verrors.Wrap(verrors.CodeInternal, "local desktop state is not valid JSON", err)
	}
	if state.Schema != stateSchema {
		return persistedState{}, verrors.New(verrors.CodeInternal, "local desktop state uses an unsupported schema")
	}
	return state, nil
}

func (s *StateStore) writeLocked(state persistedState) error {
	if s == nil || s.path == "" {
		return verrors.New(verrors.CodeInternal, "the local desktop state path is unavailable")
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return verrors.Wrap(verrors.CodeInternal, "could not create the local desktop state directory", err)
	}
	file, err := os.CreateTemp(directory, ".desktop-state-*")
	if err != nil {
		return verrors.Wrap(verrors.CodeInternal, "could not prepare local desktop state", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return verrors.Wrap(verrors.CodeInternal, "could not protect local desktop state", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		_ = file.Close()
		return verrors.Wrap(verrors.CodeInternal, "could not encode local desktop state", err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return verrors.Wrap(verrors.CodeInternal, "could not write local desktop state", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return verrors.Wrap(verrors.CodeInternal, "could not finish local desktop state", err)
	}
	if err := file.Close(); err != nil {
		return verrors.Wrap(verrors.CodeInternal, "could not close local desktop state", err)
	}
	if err := os.Rename(temporary, s.path); err != nil {
		return verrors.Wrap(verrors.CodeInternal, "could not save local desktop state", err)
	}
	return nil
}

func normalizeProfile(profile ConnectionProfile) (ConnectionProfile, error) {
	profile.Name = strings.TrimSpace(profile.Name)
	if profile.Name == "" || len(profile.Name) > 80 || strings.IndexFunc(profile.Name, unicode.IsControl) >= 0 {
		return ConnectionProfile{}, verrors.New(verrors.CodeInvalidArguments, "a profile name of up to 80 printable characters is required")
	}
	destination, err := remote.Parse(strings.TrimSpace(profile.Destination))
	if err != nil {
		return ConnectionProfile{}, err
	}
	if destination.User == "" {
		return ConnectionProfile{}, verrors.New(verrors.CodeInvalidArguments,
			"saved profiles require an explicit SSH user").WithHint("Use user@host:/absolute/remote/path.")
	}
	if !strings.HasPrefix(destination.Path, "/") {
		return ConnectionProfile{}, verrors.New(verrors.CodeInvalidArguments,
			"saved profiles require an absolute remote path").WithHint("Use user@host:/absolute/remote/path.")
	}
	profile.Destination = canonicalDestination(destination)
	if profile.Port == 0 {
		profile.Port = 22
	}
	if profile.Port < 1 || profile.Port > 65535 {
		return ConnectionProfile{}, verrors.New(verrors.CodeInvalidArguments, "SSH port must be between 1 and 65535")
	}
	profile.KnownHosts = strings.TrimSpace(profile.KnownHosts)
	if profile.KnownHosts == "" {
		profile.KnownHosts = sshclient.DefaultKnownHosts()
	}
	return profile, nil
}

func canonicalDestination(destination remote.Destination) string {
	host := destination.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return destination.User + "@" + host + ":" + destination.Path
}

func redactedDestination(destination remote.Destination) string {
	host := destination.Host
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	name := path.Base(destination.Path)
	if name == "." || name == "/" || name == "" {
		name = "destination"
	}
	return destination.User + "@" + host + ":…/" + name
}

func randomID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", verrors.Wrap(verrors.CodeInternal, "could not create a local identifier", err)
	}
	return hex.EncodeToString(bytes), nil
}
