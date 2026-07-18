# Vericopy project status

This is the living record of what Vericopy is building, what is complete, and
what comes next. Update it with every meaningful product or release change.

Last updated: 2026-07-18

## Product vision

Vericopy makes large cross-platform transfers over SSH predictable. It treats
host identity, resume safety, byte-for-byte verification, path dialects, and
destination access as one transfer problem rather than a collection of shell
incantations.

The product is desktop-first: a native application makes verified transfers
operable without command-line expertise. The CLI remains a first-class interface
for automation, diagnostics, and expert workflows. The desktop application is
local, not a hosted browser service, and calls the same Go transfer engine as
the CLI.

## Current release

- Version: `0.1.0-dev`
- Stability: active development; desktop and CLI interfaces may change before `1.0.0`
- Source of truth: `VERSION`
- Release history: `CHANGELOG.md`
- Build identity: version, commit, and build date reported by `vericopy version`
- Version policy: Semantic Versioning after the first tagged release

## Milestones

| Milestone | Status | Acceptance signal |
| --- | --- | --- |
| M0: repository foundation | Complete | Builds, policies, living tracker, version command |
| M1: path and diagnostic core | Complete | Cross-platform path tests and stable JSON errors pass |
| M2: secure single-file transfer | Complete | Unit and isolated OpenSSH evidence pass |
| M3: directory and permission policies | Complete | Unit and isolated OpenSSH evidence pass |
| M4: optional rsync adapter | Complete | Binary dialect detection and safe argument-only execution |
| M5: CLI release pipeline | Complete | Unit, race, integration, static, vulnerability, and CLI release checks pass |
| M6: desktop workflow | In progress | Branded native shell, non-secret profiles, truthful progress, and redacted history operate through the shared engine |
| M7: desktop release | Planned | Native packages and acceptance evidence exist for Windows, macOS, and Linux |

Status values are `Planned`, `In progress`, `Blocked`, and `Complete`. A
milestone becomes complete only when its acceptance signal is verified.

## Work completed

- [x] Confirmed the project is a CLI, not a web application.
- [x] Inspected the reference design system read-only.
- [x] Defined the product vision and release tracking model.
- [x] Implemented the command surface and stable output contract.
- [x] Implemented the native SFTP transfer lifecycle.
- [x] Added substantial unit tests and an isolated OpenSSH integration harness.
- [x] Completed public documentation and repository automation.
- [x] Cross-compiled the six supported platform and architecture combinations.
- [x] Run the isolated OpenSSH suite through a compatible local container runtime.
- [x] Run race detection in a disposable Go container with a C compiler.
- [x] Create the reviewed local commit history and publish the public repository.
- [x] Add a branded Wails desktop shell backed by the shared Go transfer engine.
- [x] Add desktop request review, native source/key selection, confirmation,
  cancellation, and verified SFTP execution boundaries.
- [x] Add non-secret saved connections, engine-backed per-file progress, and
  redacted, locally controlled transfer history.
- [x] Apply the Basha Editorial design system to the desktop workspace with
  token-driven light and dark modes, desktop-appropriate navigation, and
  restrained interaction states.
- [x] Repair the Windows path, OpenSSH integration, and Go patch-level CI
  failures; all GitHub pull-request checks now pass.
- [ ] Create and verify the first release tag.
- [ ] Complete desktop workflow acceptance on Windows, macOS, and Linux.
- [ ] Build signed native desktop packages on their respective target platforms.

## Now

1. Exercise a disposable SSH host from the desktop app using an ordinary test
   folder, then verify the checksum result, cancellation, and resume behavior.
2. Validate the desktop app in native Windows, macOS, and Linux sessions using
   the [desktop acceptance checklist](desktop-acceptance.md), including light,
   dark, keyboard, and reduced-motion UI states.
3. Build a native desktop package on each target operating system and record
   the produced artifact names and checksums.

## Next

1. Close any defects found by native desktop and Windows acceptance runs.
2. Add release automation for native desktop packages after the three native
   package commands have been proven outside a cross-compilation assumption.
3. Prepare `0.1.0` release artifacts and a signed version tag.
4. Review the required GitHub checks, private security reporting, release-tag
   protection, and artifact attestations after the first draft release.

## Acceptance checklist

### Core behavior

- [x] Clean checkout builds on the supported Go toolchain.
- [x] `doctor`, `inspect-path`, and `version` work without a server.
- [x] Native SFTP is the default and does not invoke a shell for file paths.
- [x] Unknown and changed SSH host keys are rejected in unit tests.
- [x] Interrupted single-file uploads resume only after compatibility checks.
- [x] Size and SHA-256 match before finalization in unit tests.
- [x] Existing destinations are protected by default in unit tests.
- [x] Symlinks and special files are rejected by default in unit tests.

### Cross-platform and access behavior

- [x] Windows drive, UNC, MINGW, Cygwin, POSIX, Unicode, and space cases pass.
- [x] Permission presets and explicit overrides are validated and tested.
- [x] Service-user traversal and read access is covered by injected unit tests.
- [x] Optional rsync invocation uses argument arrays and explains dialect errors.

### Release quality

- [x] Unit tests pass.
- [x] Race tests pass in a cgo-enabled container.
- [x] Isolated OpenSSH integration tests pass.
- [x] Formatting, vet, static analysis, and reachable vulnerability checks pass.
- [x] Linux, macOS, and Windows release builds pass.
- [x] GoReleaser 2.15.4 validates the release configuration.
- [x] Documentation, security review, secret scan, and asset review are complete.
- [x] `main` requires passing CI/CodeQL checks and resolved conversations;
  Dependabot alerts/updates, private reporting, and immutable `v*` release tags
  are enabled in GitHub.

## Verification evidence

Verified on 2026-07-17 with Go 1.26.5 on Linux/amd64:

| Check | Result |
| --- | --- |
| `go test ./...` | Pass |
| Isolated OpenSSH integration runtime through Podman | Pass |
| Race detector through Go 1.26.5 Alpine container | Pass |
| `go vet ./...` | Pass |
| Staticcheck 0.7.0 | Pass |
| Govulncheck 1.6.0 | No reachable vulnerabilities |
| Cross-build six release targets | Pass |
| CLI version/path/JSON/exit smoke test | Pass |
| GoReleaser 2.15.4 configuration check | Pass with temporary local remote metadata |
| Banner SVG render and visual inspection | Pass |
| `git diff --check` and public-text sweep | Pass |
| GitHub Actions pull-request checks | Pass: Ubuntu, macOS, Windows, race, OpenSSH, static/vulnerability, cross-build, and CodeQL |
| Direct host race detector | Not available: cgo and a C compiler are absent |
| Docker Desktop WSL CLI | Not available: WSL integration is disabled |

Govulncheck also reports the module-level `GO-2026-5932` advisory for the
unmaintained `golang.org/x/crypto/openpgp` package. Vericopy imports
`golang.org/x/crypto/ssh`, not `openpgp`; no imported package or reachable symbol
is affected.

## Known constraints

- Docker Desktop is not currently integrated with this WSL environment. The
  integration and race helpers default to Docker and were verified locally with
  Podman, which uses the same isolated-container model.
- GNU Make is not installed locally. Its cross-build commands were executed
  directly with the same variables and targets.
- Race detection is unavailable because cgo and a C compiler are absent.
- The installed Windows Go toolchain is available, and a verified project-local
  Linux Go toolchain is used for development checks.
- Wails packages must be produced on native target operating systems; generic
  Go cross-builds do not create an installable Wails application bundle.
- A Windows code-signing certificate, Apple Developer signing/notarization
  credentials, and final Linux package-format decision are not available in
  this workspace. These remain release gates, not bypassable build switches.

## Decision log

| Date | Decision | Reason |
| --- | --- | --- |
| 2026-07-17 | Desktop is the primary product UI; CLI remains first-class | The app makes safe transfers operable without removing automation or diagnostics. |
| 2026-07-17 | Native SFTP is the required default backend | It avoids shell interpolation and external rsync path ambiguity. |
| 2026-07-17 | Version starts at `0.1.0-dev` | The command contract exists before the first stable release. |
| 2026-07-17 | Track status in one living document | Vision, delivery state, next work, and acceptance evidence remain reviewable. |
| 2026-07-18 | Use the user-provided Basha Editorial system as the desktop visual source | Apply its tokens, typography roles, dark mode, and component restraint while adapting layouts to a transfer application. |
| 2026-07-17 | Verify containers with Podman when Docker Desktop WSL is unavailable | The scripts retain Docker defaults and allow an equivalent local runtime. |
| 2026-07-18 | Keep profiles and history local and redacted | Connection references are useful convenience data; source paths, key paths, full remote paths, and digests are not needed for the history view. |
| 2026-07-18 | Protect mainline and release references in GitHub | All current CI checks gate `main`; release tags can be created once but not moved or deleted. |

## Update protocol

Every change that affects behavior should update at least one of these:

- this file for delivery status or decisions;
- `CHANGELOG.md` for user-visible behavior;
- `VERSION` for a release boundary;
- an architecture or security document for a changed guarantee;
- tests for the acceptance evidence.

Release procedure: change `VERSION`, move relevant changelog entries from
`Unreleased` to the dated version, run release verification, commit the release,
and create a signed `vX.Y.Z` tag. Tags and publishing remain manual actions.
