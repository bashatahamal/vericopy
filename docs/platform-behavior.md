# Platform behavior

Vericopy separates the path a user typed from the path dialect the running
binary can open. `inspect-path` makes that decision visible without connecting
to a server.

## Recognized local forms

| Form | Example | Classification |
| --- | --- | --- |
| Native Windows drive | `C:\Users\me\Documents\annual-report.pdf` | `windows-drive` |
| Windows drive with slashes | `C:/Users/me/Documents/annual-report.pdf` | `windows-drive` |
| UNC | `\\server\shared\annual-report.pdf` | `windows-unc` |
| Git Bash or MINGW | `/c/Users/me/Documents/annual-report.pdf` | `mingw` |
| Cygwin | `/cygdrive/c/Users/me/Documents/annual-report.pdf` | `cygwin` |
| POSIX absolute | `/home/me/Documents/annual-report.pdf` | `posix` |
| Relative | `reports/annual-report.pdf` | `relative` |

Spaces and Unicode remain data within one process argument. Quote paths for the
launching shell, then Vericopy preserves the resulting argument.

## Runtime opening rules

The Windows build converts MINGW and common Cygwin input to native drive paths.
It accepts UNC paths supported by the current Windows account. A POSIX absolute
path cannot be mapped without an explicit drive and is rejected.

The macOS and Linux builds open POSIX and relative paths. They can classify
Windows, MINGW, and Cygwin forms for diagnostics but do not guess a mount mapping.
On WSL, pass the actual mounted POSIX form such as `/mnt/c/Users/...` to the Linux
binary, or run the Windows binary with a Windows form.

## Remote specifications

Remote destinations use `[user@]host:path`. Bracketed IPv6 is supported:

```text
user@[2001:db8::20]:/srv/shared/annual-report.pdf
```

A colon after a Windows drive letter is never treated as the remote separator.
Control characters, empty hosts or paths, and `..` path segments are rejected.
Relative remote paths are resolved by the SFTP server relative to the SSH
account's starting directory. Absolute paths are recommended for service access
checks and operational clarity.

## Permission compatibility contract

- POSIX to POSIX: preserve modes and timestamps only when requested. Otherwise,
  apply the chosen destination policy.
- Windows to POSIX: apply an explicit destination policy. Do not reproduce
  synthetic Cygwin permission bits by default.
- POSIX to Windows: preserve content, timestamps where supported, and limited
  read-only semantics. Do not claim ACL equivalence.
- Exact Windows ACL replication: outside the initial scope.

The default policy is `private`, not `preserve`.

## Rsync path dialects

The optional backend locates the actual executable and classifies Cygwin,
MSYS/MINGW, native Windows, or Unix behavior where possible. The dialect belongs
to the binary. A Cygwin rsync launched from Git Bash still expects Cygwin paths.

Vericopy rejects known mismatches with `SOURCE_PATH_DIALECT_MISMATCH`. Cygwin
conversion should use the matching installation's `cygpath`, because the drive
prefix can be customized. See the [debugging guide](debugging/windows-cygwin-rsync-paths.md).
