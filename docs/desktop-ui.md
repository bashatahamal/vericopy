# Desktop product specification

## Product boundary

Vericopy is an app-based product. The native desktop application owns the
complete user journey: choosing a source, establishing trust, authenticating,
reviewing the request, transferring, verifying, and understanding the result.
Users should not need to learn or compose a command-line transfer.

The app runs locally and calls the Go transfer engine in the same process. It is
not a hosted service and does not send files through a Vericopy server.

## Current experience

1. The dashboard explains the verified-transfer promise and begins a transfer.
2. The user selects a local file or folder with a native picker.
3. The user enters and reviews an explicit `user@host:path` destination.
4. The user chooses recommended key/agent authentication or a one-time password.
5. The app checks local prerequisites and presents advanced security and access
   settings without making them the primary workflow.
6. A review screen shows the non-secret request before any connection opens.
7. The reviewed request enters a bounded transfer queue. Up to two jobs run at
   once while additional work waits in creation order.
8. The Transfers view reports truthful per-job progress, verification,
   finalization, cancellation, retry, removal, and concrete results.
9. Saved sessions can restore a setup. Redacted local activity records previous
   outcomes. The Help view explains setup and security concepts in context.

## Transfer manager and background behavior

The transfer form creates jobs; it does not become a global progress screen.
After queuing work, the user can immediately prepare another request. The
Dashboard shows a compact queue summary and the Transfers view is the complete
manager.

Two jobs run concurrently by default. This bound prevents an accidental burst
of SSH connections and keeps progress understandable. Queued jobs can be
canceled before connection; running jobs receive context cancellation and keep
compatible partial state for resume.

Transfers continue while the native window is minimized because the local
process remains alive. Closing the window while jobs are running or queued
offers **Keep running**, which minimizes the window, or **Quit and stop**, which
cancels active contexts and leaves recoverable non-secret job state. Vericopy
does not install a permanent daemon or claim to continue after process exit.

On restart, previously queued or running key/agent jobs become **Paused** and
require an explicit retry. Password jobs become **Needs password**. They never
resume silently and the secret is never restored from disk.

## Authentication

SSH agent or private-key authentication is recommended. A selected encrypted
key must be loaded into an agent because the app does not collect key
passphrases.

One-time password mode is an explicit alternative, never an automatic fallback.
The password is required only for the live transfer request and is excluded from
review output, saved sessions, activity, progress, and logs. The visible field
is cleared when transfer begins. Password mode does not answer
keyboard-interactive or multi-factor challenges.

Every authentication mode requires strict `known_hosts` verification. Unknown
or changed server keys stop the request.

## Saved local data

Saved sessions and recoverable transfer-job setup retain the form state needed
to repeat or retry a transfer, including
source paths, destination details, identity-key paths, authentication choice,
and transfer preferences. They never contain passwords, private-key contents,
or key passphrases.

The transfer manager and activity views are deliberately less detailed. They
expose source names and redacted destinations while excluding complete paths,
identity paths, `known_hosts` paths, passwords, and content hashes. Deleting a
session, job, or activity record never deletes a source file or remote
destination.

## Visual system

The desktop interface adapts the Basha Editorial design system to a working
application rather than reproducing a portfolio layout.

- Source Serif 4 carries product headings, IBM Plex Sans carries interface text,
  and IBM Plex Mono is limited to paths and technical facts.
- Warm neutral surfaces, green verified/action states, and restrained gold
  structure come from the shared tokens.
- Each view has one primary action.
- The transfer form is a numbered sequence rather than a flat settings grid.
- Rows and hairlines carry ordinary records; cards are reserved for meaningful
  grouping or trust statements.
- Light and dark modes follow the operating-system preference by default.
- Reduced-motion preferences remove nonessential movement.

## Security rules in the interface

- A destination includes an explicit SSH user; the app never guesses one.
- Selected paths are passed to native filesystem and SFTP operations, not a
  remote shell command.
- The app offers no `StrictHostKeyChecking=no` equivalent.
- Existing destinations remain protected unless overwrite is explicitly chosen.
- A successful state is shown only after remote size and SHA-256 match.
- An interrupted or failed verification is never styled as completed.
- Queued jobs never open a connection before a scheduler slot is available.
- Restarted jobs never reconnect without an explicit user retry.
- No security rule exists solely in frontend JavaScript.

## Technology boundary

Wails provides the native window and local WebView. The frontend owns layout,
accessible interaction, and ephemeral form presentation. Go owns validation,
state persistence, bounded scheduling, SSH authentication, host verification,
SFTP operations, permission policy, per-job progress events, cancellation, and
verification.

Only the theme preference uses WebView storage. Saved sessions and activity use
the atomic Go state store.

## Release boundary

The workflow and visual system are implemented. The remaining work is native
acceptance, packaging, signing, and release evidence on Windows, macOS, and
Linux. Development executables must not be described as signed installers.
See the [desktop acceptance checklist](desktop-acceptance.md) and
[project status](project-status.md).
