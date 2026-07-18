# Desktop acceptance checklist

This checklist is the live release gate for the native application. Use a
disposable SSH host and ordinary test documents such as `annual-report.pdf` or
a `reports` folder. Never use production credentials or irreplaceable files.

## Before testing

- [ ] Verify the host fingerprint independently and add it to `known_hosts`.
- [ ] Load a test key into the SSH agent, or prepare a selected test key path.
- [ ] Create a destination owned by the explicit test SSH account.
- [ ] Use a source file and folder containing spaces and Unicode characters.
- [ ] Confirm that the destination does not contain an existing file unless
  replacement behavior is the condition being tested.

## Windows acceptance

- [ ] Launch the production Wails build from a normal Windows desktop session.
- [ ] Select a file and a folder using the native dialogs.
- [ ] Save, apply, and remove a connection profile; verify no source or key
  path is displayed after reopening the application.
- [ ] Complete a file transfer and observe uploading, verification, and
  completion states backed by real bytes.
- [ ] Interrupt a large test transfer, then resume it and verify the final
  SHA-256 result.
- [ ] Confirm an unknown or changed host key stops before any transfer bytes.
- [ ] Confirm the local history redacts full source and remote paths, then
  clear it.

## macOS and Linux acceptance

Repeat the Windows scenarios on a native macOS session and a native Linux
session. For Linux, record the WebKit development/runtime package and build tag
used by the supported distribution.

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
