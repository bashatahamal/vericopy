# Vericopy project status

This is the living record of what Vericopy is building, what is complete, and
what comes next. Update it with every meaningful product or release change.

Last updated: 2026-07-17

## Product vision

Vericopy makes large cross-platform transfers over SSH predictable. It treats
host identity, resume safety, byte-for-byte verification, path dialects, and
destination access as one transfer problem rather than a collection of shell
incantations.

The product is a native CLI. Its interface is terminal output, stable JSON,
diagnostic codes, documentation, and release artifacts. A browser application
is not part of the initial product.

## Current release

- Version: `0.1.0-dev`
- Stability: active development, interfaces may change before `1.0.0`
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
| M5: release candidate | Complete | Unit, race, integration, static, vulnerability, and release checks pass |

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
- [ ] Create the reviewed local commit history and first release tag.

## Now

1. Complete the initial local commit series after configuring the Git author.
2. Run `./integration/run.sh` in CI and on a Docker-enabled workstation.
3. Run `./scripts/race-container.sh` when a host compiler is unavailable.

## Next

1. Exercise a disposable Linux host from Windows PowerShell and Git Bash.
2. Close any defects found by the real OpenSSH and Windows acceptance runs.
3. Prepare `0.1.0` release artifacts and a signed version tag.
4. Enable required GitHub checks, private security reporting, tag protection,
   and artifact attestations before publishing the first release.

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
- No remote repository, publishing, push, or pull request is authorized.

## Decision log

| Date | Decision | Reason |
| --- | --- | --- |
| 2026-07-17 | Native CLI is the product UI | The transfer and diagnostic workflow belongs in terminals and automation. |
| 2026-07-17 | Native SFTP is the required default backend | It avoids shell interpolation and external rsync path ambiguity. |
| 2026-07-17 | Version starts at `0.1.0-dev` | The command contract exists before the first stable release. |
| 2026-07-17 | Track status in one living document | Vision, delivery state, next work, and acceptance evidence remain reviewable. |
| 2026-07-17 | Derive, do not copy, the reference branding | Preserve accessibility and licensing clarity while keeping visual continuity. |
| 2026-07-17 | Verify containers with Podman when Docker Desktop WSL is unavailable | The scripts retain Docker defaults and allow an equivalent local runtime. |

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
