# Developer command reference

Vericopy is a desktop application. This command interface exists for automated
workflows, diagnostics, integration testing, and contributors working directly
with the shared Go transfer engine. It is not the primary user experience and
does not support one-time password authentication.

## Build

```sh
go build -trimpath -o ./bin/vericopy ./cmd/vericopy
```

## Commands

```text
vericopy copy SOURCE DESTINATION
vericopy verify SOURCE DESTINATION
vericopy doctor
vericopy inspect-path PATH
vericopy check-access DESTINATION --as-user USER
vericopy version
```

Run `vericopy COMMAND --help` for all flags. Common connection flags are
`--identity`, `--port`, and `--known-hosts`. Output flags are `--json`, `--quiet`,
and `--no-color`.

## Example

```sh
ssh-add ~/.ssh/id_ed25519
vericopy doctor
vericopy copy './annual-report.pdf' \
  'transfer@server.example:/srv/shared/annual-report.pdf' \
  --resume --dry-run
vericopy copy './annual-report.pdf' \
  'transfer@server.example:/srv/shared/annual-report.pdf' \
  --resume --permissions shared
```

The command interface requires key or SSH-agent authentication. Passwords and
key passphrases are never accepted as command arguments.

## Common operations

```sh
# Resume and verify a file.
vericopy copy ./archive.tar.zst user@host:/srv/archive.tar.zst --resume

# Copy a directory.
vericopy copy ./reports user@host:/srv/reports --recursive --resume

# Verify an existing destination.
vericopy verify ./archive.tar.zst user@host:/srv/archive.tar.zst

# Check whether a service account can read the destination.
vericopy check-access user@host:/srv/shared/report.pdf --as-user document-indexer
```

## Exit categories

| Exit | Meaning |
| ---: | --- |
| `0` | Success |
| `2` | Invalid input, path, mode, or dialect |
| `3` | Host identity or authentication failure |
| `4` | Connection or transfer failure |
| `5` | Integrity, resume, or verification failure |
| `6` | Group or service-account access failure |
| `10` | Unexpected internal failure |

JSON output uses a stable `ok`, `result`, or `error` envelope for automation.
The security guarantees and limitations are the same as the desktop engine;
see the [security model](security-model.md).
