# Changelog

All notable changes to Vericopy are documented here. The format follows Keep a
Changelog, and releases follow Semantic Versioning.

## [Unreleased]

### Fixed

- Wails binding generation now includes the desktop entry point without making
  ordinary CLI-focused Go test runs compile the native desktop shell.

### Added

- Native `copy`, `verify`, `doctor`, `inspect-path`, `check-access`, and `version`
  commands with stable human and JSON output.
- Strict `known_hosts` verification with SSH agent and private-key authentication.
- Resumable SFTP partial uploads bound to source metadata and prefix bytes.
- Size and SHA-256 verification before permission policy and final rename.
- Recursive transfer with default symlink and special-file rejection.
- Windows drive, UNC, MINGW, Cygwin, POSIX, space, and Unicode path handling.
- `private`, `shared`, `service-readonly`, `public-readonly`, and `preserve`
  permission policies with validated overrides and optional group application.
- Read-only service-account traversal and target-read diagnostics.
- Optional rsync adapter with executable dialect classification and argument-only
  process construction.
- Stable diagnostic codes, exit categories, secret redaction, and signal-aware
  cancellation.
- Unit tests and a Docker-isolated OpenSSH integration harness.
- Podman-compatible integration and race-test helpers for environments without
  Docker Desktop WSL integration.
- Cross-platform CI, CodeQL, dependency updates, release packaging, SBOMs, and
  artifact-attestation support.
- Branded repository banner, security model, architecture diagrams, platform
  contract, debugging guides, release verification, and contribution policies.
- Living product status, roadmap, acceptance evidence, and version tracker.
- Desktop connection profiles that persist only non-secret remote destination,
  port, and known-hosts references on the local machine.
- Engine-backed per-file desktop progress and redacted, user-clearable local
  transfer history.
- A Basha Editorial desktop workspace with token-driven light and dark modes,
  accessible navigation state, and system-consistent transfer controls.
- A redesigned compact desktop dashboard and Go-persisted saved sessions that
  retain complete transfer form state independently of WebView storage.
- Explicit desktop authentication selection with recommended SSH key/agent
  mode and non-persistent, one-time SSH password mode.
- Contextual connection guidance and an in-app Help view covering quick start,
  host verification, authentication choices, and credential handling.
- A task-first desktop redesign with bundled Source Serif 4 and IBM Plex fonts,
  a verification-led dashboard, quieter readiness state, numbered transfer
  sequence, and row-based local records.

[Unreleased]: https://github.com/bashatahamal/vericopy/compare/v0.1.0...HEAD
