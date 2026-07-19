# Desktop UI plan

## Product decision

Vericopy is desktop-first. The desktop application is the easiest way to set up
and review a secure transfer; the CLI remains the automation, diagnostics, and
power-user interface. Both call the same Go parsing, SSH, SFTP, transfer,
permission, and verification code.

The app is local software, not a hosted transfer service. It can accept a
one-time SSH password for a live connection, but never stores it in a saved
session, history, or log. It sends no telemetry and never bypasses
`known_hosts` validation.

## Desktop foundation

The first desktop milestone is deliberately narrow and operational:

1. Inspect a selected local source using the runtime path rules.
2. Parse and review an explicit `user@host:path` destination.
3. Show strict host-key prerequisites and an explicit choice between recommended
   SSH key/agent authentication and a one-time password.
4. Choose a destination policy and safe transfer options.
5. Execute native SFTP with resume, SHA-256 readback, and final access checks.
6. Return a concrete verified result or stable diagnostic with next steps.

This is a real call into the shared transfer engine. It is not a mock transfer
screen. The desktop shell now exposes engine-backed per-file byte progress,
redacted local history, and complete saved transfer sessions. It does not claim
aggregate folder percentages that the engine cannot truthfully calculate.

## Visual system

The desktop frontend uses the user-provided Basha Editorial design system as its
visual source. It adapts the system for a working desktop application rather
than copying a portfolio page into the product.

- Warm neutral surfaces, green action states, and gold structural details use
  the shared token values only.
- Bundled Source Serif 4 renders product headings, IBM Plex Sans renders the
  interface, and IBM Plex Mono is restricted to technical facts.
- Each view has one primary action at most. Gold remains a structural detail,
  never a button fill.
- The dashboard leads with the verified-transfer job and evidence; readiness
  and saved records remain secondary. The transfer form is a numbered sequence,
  not a flat settings grid.
- Cards are reserved for a real grouping or trust statement. Rows and hairlines
  handle ordinary records to avoid a generic dashboard appearance.
- The app follows the operating-system color preference by default and offers a
  remembered light or dark choice in the window chrome.
- Motion is restricted to short hover feedback and is disabled for people who
  request reduced motion.

## Planned experience

| Stage | Status | Scope |
| --- | --- | --- |
| Branded shell and reviewed transfer form | Implemented, native acceptance pending | Desktop-first workspace using the Basha Editorial system, including light and dark modes |
| Local source and destination review | Implemented, native acceptance pending | Native source selection plus path and remote-spec validation |
| Verified transfer execution | Implemented, native acceptance pending | Native SFTP, strict host identity, resume, SHA-256, and permissions |
| Saved sessions | Implemented, native acceptance pending | Complete form state in the Go store, including source and identity-key paths; legacy no-path profiles migrate once |
| Authentication guidance | Implemented, native acceptance pending | Explicit key/agent or one-time password selection, contextual guidance, and a dedicated Help view |
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
- Password authentication is an explicit alternative, never an automatic
  fallback. The password is required only when the transfer starts and is
  excluded from review output, saved sessions, history, progress, and logs.
- Password mode does not answer keyboard-interactive or multi-factor prompts.
- `StrictHostKeyChecking=no` behavior and remote-shell file paths remain out of
  scope.
- The result view distinguishes completed verification from an interrupted or
  unverified transfer.
- Saved sessions intentionally include source and identity-key paths plus the
  selected authentication method so the reusable form survives app restarts
  and cleared WebView data. They contain no password, passphrase, or key
  contents and remain in the user-protected Go state file.
- Legacy connection profiles exclude source and identity-key paths and remain
  only for one-time migration. History still excludes complete source paths,
  complete remote paths, identity paths, known-hosts paths, and SHA-256 values.

## Technology boundary

Wails provides the native desktop window and local webview. Go owns the
security-sensitive work. The frontend owns presentation, form state, and
accessible interaction only. No security rule is implemented solely in
JavaScript. Saved sessions use the Go state bridge rather than `localStorage`;
only the presentational theme preference uses WebView storage.
