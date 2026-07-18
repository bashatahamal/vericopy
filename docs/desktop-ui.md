# Desktop UI plan

## Product decision

Vericopy is desktop-first. The desktop application is the easiest way to set up
and review a secure transfer; the CLI remains the automation, diagnostics, and
power-user interface. Both call the same Go parsing, SSH, SFTP, transfer,
permission, and verification code.

The app is local software, not a hosted transfer service. It does not accept an
SSH password, send telemetry, or bypass `known_hosts` validation.

## Desktop foundation

The first desktop milestone is deliberately narrow and operational:

1. Inspect a selected local source using the runtime path rules.
2. Parse and review an explicit `user@host:path` destination.
3. Show strict host-key and SSH-agent prerequisites before a connection.
4. Choose a destination policy and safe transfer options.
5. Execute native SFTP with resume, SHA-256 readback, and final access checks.
6. Return a concrete verified result or stable diagnostic with next steps.

This is a real call into the shared transfer engine. It is not a mock transfer
screen. The foundation intentionally does not claim features that the engine
does not yet expose, such as per-byte UI progress or persisted history.

## Planned experience

| Stage | Status | Scope |
| --- | --- | --- |
| Branded shell and reviewed transfer form | Implemented, native acceptance pending | Desktop-first workspace derived from the Basha editorial tokens |
| Local source and destination review | Implemented, native acceptance pending | Native source selection plus path and remote-spec validation |
| Verified transfer execution | Implemented, native acceptance pending | Native SFTP, strict host identity, resume, SHA-256, and permissions |
| Saved connection profiles | Implemented, native acceptance pending | Non-secret remote target, port, and known-hosts reference only |
| Progress and cancellation detail | Implemented, native acceptance pending | Engine-backed per-file byte progress, SHA-256 state, and safe interruption controls |
| Transfer history and exports | Implemented, native acceptance pending | Local, user-controlled redacted history; exports remain out of scope for this milestone |
| Native packaging and signing | Planned | Per-platform Wails package builds, signing, checksums, and release evidence |
| Accessibility and acceptance runs | Planned | Keyboard, screen-reader, reduced-motion, and three-platform QA |

## Security rules in the UI

- A destination must include an explicit SSH user; the UI never guesses one.
- A source is chosen locally and never converted into a shell command.
- `known_hosts` is mandatory. Unknown or changed host keys stop the transfer.
- SSH-agent authentication is preferred. A selected key path is supported, but
  encrypted keys still require the agent.
- Password fields, `StrictHostKeyChecking=no`, and remote-shell file paths are
  out of scope.
- The result view distinguishes completed verification from an interrupted or
  unverified transfer.
- Saved profiles exclude source paths and identity key paths. History excludes
  complete source paths, complete remote paths, identity paths, known-hosts
  paths, and SHA-256 values.

## Technology boundary

Wails provides the native desktop window and local webview. Go owns the
security-sensitive work. The frontend owns presentation, form state, and
accessible interaction only. No security rule is implemented solely in
JavaScript.
