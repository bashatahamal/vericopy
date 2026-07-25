![Vericopy banner](docs/assets/vericopy-banner.svg)

# Vericopy

Vericopy is a native desktop application for sending files and folders to an
SSH server with strict host verification and byte-for-byte SHA-256 validation.

The desktop application is the product. The repository also contains a command
interface used for engine testing, diagnostics, and automation, but it is not
the primary user experience or a separate product direction.

> **Current release state:** Vericopy is `0.1.0-dev`. The Windows desktop build
> compiles and the automated test suite passes. Signed installers for Windows,
> macOS, and Linux are not published yet; native acceptance and signing remain
> the next release work.

![Vericopy desktop dashboard showing verified transfer evidence, connection readiness, saved sessions, and recent activity](docs/assets/vericopy-dashboard.png)

*The Vericopy desktop dashboard. Example paths and transfer records are
illustrative.*

## What the app does

- Select a local file or folder without writing a transfer command.
- Connect with an SSH key, SSH agent, or one-time SSH password.
- Require a verified `known_hosts` entry and stop on unknown or changed hosts.
- Review the source, destination, authentication method, and access policy
  before opening the connection.
- Queue multiple transfers, run up to two at once, and manage each job without
  waiting to prepare the next one.
- Resume compatible interrupted uploads.
- Keep the machine from sleeping while a transfer is active, on a best-effort
  basis per platform.
- Read the uploaded bytes back through SFTP and compare size and SHA-256 before
  finalizing the destination.
- Apply an explicit destination permission policy.
- Keep saved sessions and redacted activity locally, with no telemetry.

## Use the desktop app

### 1. Prepare the server identity

Obtain the SSH server fingerprint from the server administrator or another
trusted channel. Add the matching host key to your local `known_hosts` file.
Vericopy does not provide an accept-any-host bypass.

`ssh-keyscan` can retrieve a public key, but it does not prove that the key
belongs to the intended server. Compare the fingerprint independently before
trusting it.

### 2. Open Vericopy

Until signed installers are available, build the app on the operating system
where it will run:

```sh
git clone https://github.com/bashatahamal/vericopy.git
cd vericopy
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
make desktop-package
```

The packaged application is written to `cmd/vericopy-desktop/build/bin/`.
Development builds can use `make desktop-build`, but the Wails package is the
representative native application build.

The app runs locally. It does not start a hosted transfer service and does not
send files through Vericopy infrastructure.

### 3. Set up a transfer

![Vericopy transfer setup showing source selection, SSH destination, permission policy, and authentication choices](docs/assets/vericopy-transfer-setup.png)

*The transfer workspace keeps source, destination, policy, and authentication
choices visible before Vericopy connects.*

1. Choose **Set up transfer**.
2. Select a local file or folder.
3. Enter a destination such as
   `transfer@server.example:/srv/shared/report.pdf`.
4. Choose **SSH key or agent** or **One-time password**.
5. Review the permission and transfer options.
6. Choose **Review transfer**, confirm the summary, and add it to the queue.
7. Open **Transfers** to cancel, retry, remove, or monitor every job.
8. Keep Vericopy open or minimized while it copies, reads the destination back,
   and reports the verification result.

The built-in **Help** view explains destinations, host identity, authentication,
verification, permissions, and saved local data.

## Multiple transfers and minimized progress

![Vericopy transfer manager showing active, queued, password-required, and verified jobs](docs/assets/vericopy-transfer-manager.png)

*Each job keeps its own progress and controls while the queue remains visible.*

Vericopy runs up to two transfer jobs at once and keeps additional work queued
in creation order. Each job has independent progress, cancellation, retry, and
result state. You can return to **New transfer** immediately after queuing a job.

Transfers continue while the Vericopy window is minimized. If active or queued
work exists when the window is closed, Vericopy asks whether to minimize and
keep running or quit and interrupt safely. It does not install a permanent
background service and cannot continue after the process or operating system
has stopped.

Non-secret job setup survives an app restart. Jobs that were active wait for an
explicit retry instead of reconnecting silently. Password jobs display
**Needs password** because the password is never written to disk; key or agent
jobs display **Paused** until the user retries them.

## Authentication and privacy

SSH keys and the local SSH agent remain the recommended authentication method.
An encrypted private key should be loaded into the agent before starting a
transfer.

One-time password mode is an explicit alternative for servers that permit SSH
password authentication. By default the password is sent only to the native
SSH connection when the transfer starts: it is not stored in saved sessions,
reviews, activity, progress, or logs, and the visible field is cleared when the
transfer begins. Like any credential entered into an application, it exists
briefly in process memory while authenticating. Keyboard-interactive and
multi-factor challenge flows are not supported.

A saved session can optionally turn on **Remember this password**. When set,
the password is stored in the operating system's own credential store
(Windows Credential Manager, macOS Keychain, or Linux Secret Service), keyed
by the session name, never in Vericopy's own state file. Turning the option
off removes the stored password. This is opt-in and off by default.

Saved sessions and recoverable job setup can contain local source paths,
identity-key paths, destination details, and preferences. They never contain
key contents or key passphrases, and never contain a password unless Remember
this password was explicitly turned on for that session. The transfer manager
exposes only source names and redacted destinations; activity records are
local and deliberately redacted.

## Transfer guarantees

Vericopy uses native SFTP by default and never interpolates selected file paths
into a remote shell command for the transfer itself. A successful result means
the remote bytes matched the source bytes Vericopy observed by both size and
SHA-256 before the final name was applied.

Verification tries a fast path first: if the server accepts a standard remote
hash command (`sha256sum` or `shasum -a 256`), Vericopy runs it there instead
of reading the whole file back over SFTP just to hash it locally. The
destination path is always passed through strict POSIX shell quoting, never
raw string interpolation. Any failure, unavailability, or digest mismatch on
that fast path falls back to reading the destination back over SFTP and
hashing it locally — the original, authoritative check — so this can only
make a successful verification faster, never weaker.

That result does not prove that the source file is trustworthy or malware-free,
and it cannot prevent a remote administrator from changing a file later. Read
the complete [security model](docs/security-model.md) for the exact boundaries.

## Permission policies

| Policy | Directories | Files | Intended use |
| --- | ---: | ---: | --- |
| `private` | `0700` | `0600` | SSH account only |
| `shared` | `2770` | `0660` | Read/write group collaboration |
| `service-readonly` | `2750` | `0640` | Owner writes; a designated group reads |
| `public-readonly` | `0755` | `0644` | World-readable published content |
| `preserve` | Source mode | Source mode | Explicit POSIX-to-POSIX preservation |

Windows ACL replication is outside the current product scope.

## Platform and release status

The transfer engine is tested on Windows, macOS, and Linux. The redesigned
Windows Wails executable has been compiled successfully. Public desktop release
packages still require native UI acceptance, platform packaging, checksums, and
code signing where applicable.

Track delivery in [project status](docs/project-status.md) and the
[desktop acceptance checklist](docs/desktop-acceptance.md). Release verification
requirements are documented in [release verification](docs/release-verification.md).

## Development

Requirements: Go 1.25 or later. A native Wails package must be built on its
target operating system.

```sh
go test -count=1 ./...
go test -tags desktop -count=1 ./cmd/vericopy-desktop
go vet ./...
make desktop-package
```

The command interface remains available to contributors for automation,
diagnostics, and direct transfer-engine testing. It intentionally does not
accept passwords as arguments. See the
[developer command reference](docs/developer-command-reference.md)
when working on that interface.

## Documentation

- [Desktop product specification](docs/desktop-ui.md)
- [Security model](docs/security-model.md)
- [Architecture and transfer sequence](docs/architecture.md)
- [Platform behavior](docs/platform-behavior.md)
- [Brand and accessibility](docs/brand.md)
- [Contributing](CONTRIBUTING.md)

## Current limitations

- Signed desktop installers are not published yet.
- Native acceptance is not complete on all three target operating systems.
- Symlinks and special files are rejected.
- Exact Windows ACL and extended-attribute replication are not attempted.
- Password mode does not support keyboard-interactive or MFA challenges.
- Final rename behavior cannot be more atomic than the remote filesystem and
  SFTP server permit.

## License

MIT. See [LICENSE](LICENSE).
