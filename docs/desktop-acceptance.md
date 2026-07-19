# Desktop acceptance checklist

This checklist is the live release gate for the native application. Use a
disposable SSH host and ordinary test documents such as `annual-report.pdf` or
a `reports` folder. Never use production credentials or irreplaceable files.

## Before testing

- [ ] Verify the host fingerprint independently and add it to `known_hosts`.
- [ ] Load a test key into the SSH agent, or prepare a selected test key path.
- [ ] Prepare a disposable password-enabled SSH account for the one-time
  password scenario; never use production credentials.
- [ ] Create a destination owned by the explicit test SSH account.
- [ ] Use a source file and folder containing spaces and Unicode characters.
- [ ] Confirm that the destination does not contain an existing file unless
  replacement behavior is the condition being tested.

## Windows acceptance

- [x] Generate the Windows AMD64 production executable, including Wails binding
  generation and embedded WebView2 support.
- [ ] Launch the production Wails build from a normal Windows desktop session.
- [ ] Select a file and a folder using the native dialogs.
- [ ] Save, apply, and remove a session; verify its source and identity-key paths
  restore after reopening the application and after clearing WebView data.
- [ ] Complete a file transfer and observe uploading, verification, and
  completion states backed by real bytes.
- [ ] Interrupt a large test transfer, then resume it and verify the final
  SHA-256 result.
- [ ] Confirm an unknown or changed host key stops before any transfer bytes.
- [ ] Complete a password-authenticated transfer, then confirm the password is
  absent from the saved session file, history, visible form, and diagnostics.
- [ ] Save and reload a password-mode session; confirm the method returns but
  the password field is empty and requires a new entry.
- [ ] Use the Help view and contextual Connection help action with keyboard-only
  navigation.
- [ ] Confirm the local history redacts full source and remote paths, then
  clear it.

## macOS and Linux acceptance

Repeat the Windows scenarios on a native macOS session and a native Linux
session. For Linux, record the WebKit development/runtime package and build tag
used by the supported distribution.

For WSL specifically, make a separate checkout below the Linux home directory,
such as `~/src/vericopy-wsl`, before running a Linux Wails build. Never reuse
the mounted Windows checkout for both targets: Windows locks a running `.exe`,
which prevents WSL from cleaning the same `build/bin` directory.

## Accessibility and resilience

- [ ] Complete the reviewed-transfer flow with a keyboard only.
- [ ] Inspect focus indicators, errors, progress, and cancellation with a
  screen reader where available.
- [ ] Enable reduced motion and verify the interface remains fully usable.
- [ ] Restart after an interrupted transfer; confirm no transfer automatically
  resumes without a new review and confirmation.
- [ ] Attempt invalid ports, missing users, relative remote paths, and a local
  symlink; confirm each receives a safe diagnostic.

## Evidence to record

For each platform, record the package filename, SHA-256, operating-system
version, Wails version, test date, and every failed scenario. Update
`docs/project-status.md` only after all relevant checks have been witnessed.

### Windows AMD64 build, 2026-07-18

| Evidence | Value |
| --- | --- |
| Wails CLI | `v2.13.0` |
| Command | `wails build -clean -webview2 embed` |
| Artifact | `cmd/vericopy-desktop/build/bin/vericopy-desktop.exe` |
| SHA-256 | `9446dd69840bce870f7ca52c656b7b7a10d79e854d0c6414e72f125e4989fdfe` |
| Result | Binding generation, frontend preparation, embedded WebView2, and production compilation passed. Launch and transfer scenarios remain pending. |
