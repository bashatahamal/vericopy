# Vericopy project status

Last updated: 2026-07-19

## Product direction

Vericopy is a native desktop application for verified file transfer over SSH.
The application is the product surface: setup, authentication, review, progress,
verification evidence, saved sessions, activity, and help belong in the app.

The repository still contains a command interface because it is useful for
automation, diagnostics, integration tests, and transfer-engine development. It
is a supporting engineering interface, not a second product experience and not
the focus of product documentation or release messaging.

Vericopy is local software, not a hosted transfer service. The desktop frontend
calls a Go service and shared transfer engine in the same process.

## Current release

- Version: `0.1.0-dev`
- Product maturity: development build; not yet a signed public release
- Primary interface: native Wails desktop application
- Supported transfer transport: native SFTP over SSH
- Target desktop platforms: Windows, macOS, and Linux
- Source of truth for version: `VERSION`

## Implemented

- Native file and folder selection.
- Explicit `user@host:path` destination review.
- Strict `known_hosts` verification with no bypass.
- SSH agent, selected private key, and non-persistent one-time password modes.
- Native SFTP copy, compatible resume, remote readback, size comparison, and
  SHA-256 verification before finalization.
- Explicit permission policies and optional service-account access checks.
- Numbered transfer setup, review, progress, cancellation, and result states.
- Go-persisted saved sessions without passwords or key contents.
- Redacted local activity history.
- Contextual guidance and an in-app Help view.
- Editorial light and dark interface with bundled offline fonts.
- Unit, race, OpenSSH integration, static, vulnerability, cross-platform, and
  CodeQL checks in CI.

## Release work remaining

1. Complete native transfer acceptance on Windows using a disposable SSH host,
   including password, key/agent, cancellation, resume, and saved-session cases.
2. Complete the same UI and transfer acceptance on macOS and Linux.
3. Produce native packages on each target operating system.
4. Sign and notarize packages where the platform requires it.
5. Publish checksums, provenance, release notes, and the first signed version
   tag.

Until those steps are complete, repository builds are development artifacts and
must not be presented as signed end-user installers.

## Acceptance summary

| Area | State | Evidence |
| --- | --- | --- |
| Transfer engine | Complete for current scope | Unit and isolated OpenSSH tests pass |
| Host verification | Complete for current scope | Unknown and changed keys fail closed in tests |
| Key/agent authentication | Implemented | Unit and integration coverage pass |
| One-time password authentication | Implemented | Method selection, required secret, and non-persistence tests pass |
| Desktop workflow | Implemented | Source, review, progress, cancellation, history, sessions, and help are wired to Go |
| Desktop visual system | Implemented | Light, dark, compact, review, password, activity, and help states inspected |
| Windows production compilation | Pass | Wails build completed with embedded WebView2 |
| Windows native transfer acceptance | Pending | Must be exercised against a disposable SSH host |
| macOS native acceptance and package | Pending | Requires a native macOS environment |
| Linux native acceptance and package | Pending | Requires a native Linux desktop environment |
| Signing and public installers | Pending | Platform credentials and packaging evidence required |

## Verification evidence

The current mainline has passed:

- `go test -count=1 ./...`
- desktop-tagged Go tests
- race detection
- isolated OpenSSH integration
- `go vet`, Staticcheck, and Govulncheck
- Windows, macOS, and Linux engine builds
- frontend syntax and browser-state checks
- Windows Wails production compilation
- GitHub CodeQL

The latest reviewed Windows desktop artifact was compiled on 2026-07-19 with
SHA-256
`0c4b5488868e0540fc05ccd3f53974869bb103ff0bd10b46168154b4d02a35e5`.
That checksum identifies the reviewed development artifact; it is not a public
release signature.

## Product decisions

| Date | Decision | Reason |
| --- | --- | --- |
| 2026-07-19 | Treat Vericopy as an app-based product | Users should not need command-line knowledge to make a safe transfer |
| 2026-07-19 | Keep the command interface as a supporting engineering surface | Automation and diagnostics remain useful without competing with the app experience |
| 2026-07-19 | Offer explicit one-time password authentication | It lowers setup friction while preserving strict host verification and non-persistence |
| 2026-07-18 | Persist complete saved sessions in the Go state store | Reusable setups survive app and WebView restarts without storing passwords |
| 2026-07-18 | Keep history local and redacted | Transfer records should help users without becoming a sensitive path ledger |
| 2026-07-18 | Use the Basha Editorial system as a visual source | It gives the app a restrained identity while allowing application-specific layouts |
| 2026-07-17 | Use native SFTP as the default backend | It avoids shell interpolation and external path-dialect ambiguity |

## Update policy

Update this file when the product direction, release readiness, or acceptance
state changes. User-visible behavior belongs in `CHANGELOG.md`; security
guarantees belong in `docs/security-model.md`; native test evidence belongs in
`docs/desktop-acceptance.md`.
