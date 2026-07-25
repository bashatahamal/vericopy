# Security model

This document states the guarantees Vericopy is designed to make, the boundaries
those guarantees depend on, and the cases it does not solve.

## Assets

- Source file confidentiality and integrity while in transit.
- Destination integrity before final naming.
- SSH credentials and agent access.
- Server identity recorded in `known_hosts`.
- Existing destination data protected from accidental replacement.
- Local and remote paths protected from command injection and traversal.
- App activity, diagnostics, and supporting automation output protected from
  credential disclosure.

## Trust boundaries

The local process and source filesystem are trusted to provide bytes and
metadata. The SSH transport is trusted only after cryptographic authentication
and strict host-key verification. The remote SSH account and SFTP subsystem are
trusted to enforce their reported filesystem operations. The remote filesystem,
kernel, administrators, and privileged processes remain outside Vericopy's
control.

The optional rsync executable is an additional local trust boundary. It is not
used by the default backend.

## Attacker assumptions

Vericopy considers an attacker who can observe or alter network traffic, present
an unknown SSH server, manipulate untrusted path strings, leave unrelated
partial files, or trigger confusing backend errors. It does not defend a host
that is already fully compromised, a malicious privileged remote administrator,
or a malicious source file that the user intentionally selects.

## SSH host-key policy

The default is strict matching against an explicit `known_hosts` file. Missing,
unknown, and changed keys fail closed with `HOST_KEY_REJECTED` or
`KNOWN_HOSTS_UNAVAILABLE`. There is no automatic trust-on-first-use workflow and
no flag that disables verification.

Retrieving a key with `ssh-keyscan` does not authenticate it. Users must compare
the fingerprint through a separate trusted channel before storing it.

## Authentication

SSH agent and private-key authentication are supported and remain recommended.
The desktop app also supports an explicitly selected, one-time SSH password.
Passwords and key passphrases are never accepted as command arguments by the
supporting developer interface. Encrypted keys should be loaded into an agent
with `ssh-add`. Private-key contents, passwords, and authentication causes are
not serialized in user-visible diagnostics.

The desktop password crosses the local WebView-to-Go bridge only when a transfer
is queued, remains only in the in-memory runtime job until its worker begins,
is used by the native SSH handshake, and is excluded from transfer
reviews, saved sessions, history, progress, and logs. The visible field is
cleared when the job enters the queue. Like any credential typed into an application,
the value exists briefly in process memory and cannot be promised absent from a
compromised host or process dump. Password mode never relaxes strict host-key
verification and is never attempted as an automatic fallback from key mode.
It implements SSH password authentication only; keyboard-interactive and
multi-factor challenge flows are not answered automatically.

## Path and command injection

Native transfer paths are passed to SFTP operations, not interpolated into a
remote shell. Remote destination parsing rejects control characters, malformed
endpoints, Windows drive ambiguity, and `..` traversal segments. Local device
paths and special files are rejected.

Service-user and group lookup requires conservative account names and constructs
only fixed `id` and `getent` commands. Destination paths are never included in
those commands; SFTP metadata calls inspect them directly.

Verification may run a fixed remote hash command (`sha256sum` or `shasum -a
256`, tried in that order) as a faster alternative to reading the destination
back over SFTP. This is the one place a destination path is placed into a
remote command. The path is never concatenated as a raw string: it is
wrapped in single quotes with embedded single quotes escaped (`'` becomes
`'\''`), the standard POSIX-safe way to pass an arbitrary string through a
shell, and `--` is placed before it so a name starting with `-` cannot be
read as a flag. Command output is validated against a strict 64-character
hex pattern before being trusted as a digest; anything else, including a
missing command or an unreadable remote, is treated as the fast path being
unavailable, not an error, and Vericopy falls back to the byte-for-byte
SFTP read-back that remains the authoritative verification.

The optional rsync backend uses `exec.CommandContext` with an argument slice. It
does not create a local shell command string. SSH transport options are placed
in rsync's `-e` argument as required by rsync, with user-controlled remote paths
remaining separate arguments.

## Partial upload and resume risks

Partial files and metadata use mode `0600` until final policy application. A
content-derived identifier and versioned metadata bind state to a destination,
source size, modification time, and prefix digest. Resume compares available
prefix bytes before append and rejects oversized or incompatible state.

A collision or malicious modification beyond the validated prefix can waste
transfer time, but cannot pass finalization unless the complete remote size and
SHA-256 match the complete local file observed at verification. Invalid state is
replaced only when the user starts a transfer without resume; ordinary
connection interruptions retain state.

Source metadata is checked again before finalization. A source changing in a way
that preserves size and modification timestamp is a local-filesystem limitation;
the final digest still represents the bytes read during verification.

## Symlinks and special files

Local symbolic links are not followed. Recursive walks reject symlink entries,
named pipes, sockets, devices, and other non-regular files. Remote access checks
stop at symlinks. Explicit symlink support may be designed later, but will not be
enabled by changing a default silently.

## Integrity guarantee

Vericopy calculates the local SHA-256 and reads the remote partial through SFTP
to calculate its SHA-256. It compares both byte count and digest before applying
the final name. This proves that the final destination bytes initially match the
source bytes Vericopy observed. It does not prove the original file is authentic,
safe, or malware-free. It also cannot stop a remote actor from changing a file
after finalization.

## Permission guarantee

Vericopy applies an explicit POSIX mode preset or validated overrides. Group
changes are attempted only as the connected account and never with `sudo`.
Windows ACLs and POSIX modes are not treated as equivalent.

The service-user check models owner, group, and other mode bits across every
parent plus the target. It does not fully model POSIX ACLs, SELinux, AppArmor,
mount options, container namespaces, network filesystems, or application-level
sandboxing. A positive result is evidence for ordinary POSIX access, not a proof
that every application can open the file.

## Logging and redaction

There is no telemetry. App activity contains redacted non-secret transfer
metadata. Supporting automation JSON contains public diagnostics and non-secret
results; internal causes are omitted. Redaction helpers cover common secret
assignments and private-key blocks. Code review must treat every new visible or
serialized field as a security boundary.

Paths can themselves be sensitive. The active app screen necessarily reports
the source and destination involved in a requested operation; users should
protect screenshots and local access accordingly. Resume sidecars do not store
the local source path.

## Desktop local state

The desktop Go state store is local to the operating-system user, written
atomically, and restricted to that user where filesystem permissions support
it. It is separate from WebView storage.

Saved sessions intentionally retain the complete transfer form for convenience,
including the local source path, SSH identity-key path, and selected
authentication method. They store path and preference strings only, never
private-key contents, passwords, or key passphrases. Anyone
who can read the user's Vericopy state file can learn those paths, destination
details, and transfer preferences, so the local user account remains a trust
boundary. Deleting a session removes only its saved form data and never touches
the referenced source, key, host-key file, or remote destination.

Legacy connection profiles remain temporarily available for migration. Unlike
sessions, they contain only a remote destination, port, and `known_hosts`
reference; they never contain source or identity-key paths. Transfer history
keeps its existing redaction contract and does not gain any session fields.

Recoverable transfer jobs persist the non-secret request needed for retry,
including complete source, destination, identity-key, and `known_hosts` paths,
authentication choice, and transfer preferences. The persisted request type has
no password field. The transfer-manager view exposes only a source name and
redacted destination even though the user-protected state file contains the
complete non-secret request.

Running and queued state recovered after process exit never reconnects
automatically. Key/agent jobs become paused; password jobs become
needs-password. The user must explicitly retry, and a password must be entered
again. Closing Vericopy cancels active contexts before shutdown. Minimizing the
window does not cross a new trust boundary because the same local process and
user account continue to own the jobs.

## Concurrency and job isolation

The desktop scheduler permits two active transfers and queues additional jobs.
Each active job has its own context, cancellation function, progress state, SSH
connection, and SFTP client. Canceling one job does not cancel another. The
limit reduces accidental connection and bandwidth bursts; it is not a server
quota or a guarantee of fair bandwidth allocation.

## Existing destination policy

Existing destinations are rejected unless the user explicitly enables
overwrite. Portable SFTP cannot provide a universal race-free create-only rename
primitive, so a privileged concurrent remote actor remains outside the
guarantee.

## Exit behavior

Input, host identity, transfer, integrity, access, and internal failures use
separate stable diagnostic codes. The supporting command adapter maps those
codes to documented exit categories. Cancellation retains partial state when
safe.

## Non-goals

- Malware scanning or source authenticity attestation.
- Exact Windows ACL or extended-attribute replication.
- Privilege escalation, group membership changes, or automatic permission
  weakening.
- Anonymous or password-in-argument authentication.
- Protection from a compromised local machine or privileged remote operator.
- A remote backup format, history store, or bidirectional synchronization engine.
