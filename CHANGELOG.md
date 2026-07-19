# Changelog

All notable changes to Vericopy are documented here. The format follows Keep a
Changelog, and releases follow Semantic Versioning.

## [Unreleased]

### Added

- A native Wails desktop application for selecting, reviewing, transferring,
  and verifying files and folders over SSH.
- A task-first editorial interface with light and dark modes, bundled Source
  Serif 4 and IBM Plex fonts, numbered transfer stages, and row-based records.
- Explicit SSH key/agent and non-persistent one-time password authentication.
- Contextual setup guidance and an in-app Help view covering authentication,
  host identity, destinations, verification, and local data.
- Go-persisted saved sessions, truthful per-file progress, safe cancellation,
  and redacted local activity history.
- Strict `known_hosts` verification with no bypass.
- Resumable native SFTP uploads bound to source metadata and prefix bytes.
- Remote size and SHA-256 readback before permission policy and final rename.
- Recursive transfer with default symlink and special-file rejection.
- Windows drive, UNC, MINGW, Cygwin, POSIX, space, and Unicode path handling.
- `private`, `shared`, `service-readonly`, `public-readonly`, and `preserve`
  permission policies with validated overrides and optional group application.
- Read-only service-account traversal and target-read diagnostics.
- A supporting developer command adapter for automation, diagnostics, and
  direct transfer-engine testing.
- Stable diagnostic codes, exit categories, secret redaction, and signal-aware
  cancellation in the shared engine.
- Unit, desktop, race, isolated OpenSSH, static, vulnerability, cross-platform,
  and CodeQL checks.
- Security, architecture, platform, acceptance, release, and contribution
  documentation.

### Changed

- Clarified Vericopy's product direction: the desktop application is the
  product; the command adapter is a supporting engineering interface.
- Moved detailed command usage out of the product README and into a dedicated
  developer reference.
- Renamed the pre-release `media-readonly` permission policy to
  `service-readonly`.

### Fixed

- Wails binding generation includes the desktop entry point without forcing
  ordinary engine-focused Go tests to compile the native shell.
- Windows path, OpenSSH integration, and Go patch-level CI failures discovered
  during desktop development.

[Unreleased]: https://github.com/bashatahamal/vericopy/compare/v0.1.0...HEAD
