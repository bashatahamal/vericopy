# Windows, Cygwin, and rsync path dialects

A Windows path can be spelled several ways, and those spellings are not freely
interchangeable across binaries.

## The common failure

Git Bash and MINGW commonly present drive `C:` as:

```text
/c/Users/me/Documents/annual-report.pdf
```

A Cygwin-built rsync commonly expects:

```text
/cygdrive/c/Users/me/Documents/annual-report.pdf
```

Launching the Cygwin binary from Git Bash does not make it a MINGW binary. Path
dialect belongs to the executable and its runtime, not necessarily the shell
that launched it.

Vericopy's native SFTP backend avoids this boundary. It opens the source with Go
and sends file bytes through SFTP. Use the rsync backend only when its additional
behavior is specifically needed.

## Identify the actual executable

PowerShell:

```powershell
Get-Command rsync -All | Format-List Source,Version
& (Get-Command rsync).Source --version
```

Git Bash or Cygwin:

```sh
type -a rsync
command -v rsync
rsync --version
```

Inspect the resolved directory. Paths under a Cygwin installation, an MSYS2
installation, Git for Windows, WSL, and a native Windows distribution can
expect different forms even when the executable has the same name.

## Convert with the matching Cygwin runtime

Do not blindly replace `/c` with `/cygdrive/c`. Cygwin can use a customized
drive prefix. Ask the `cygpath` installed beside the selected Cygwin tools:

```sh
/path/to/cygwin/bin/cygpath -u 'C:\Users\me\Documents\annual-report.pdf'
```

Use the output as one quoted argument. Do not build a shell command by joining
untrusted path strings.

## Run a safe dry run

With Vericopy:

```sh
vericopy copy '/cygdrive/c/Users/me/Documents/annual-report.pdf' \
  'user@server:/srv/shared/annual-report.pdf' \
  --backend rsync --dry-run --no-clobber
```

With rsync directly, keep options and paths as separate shell arguments:

```sh
rsync --dry-run --itemize-changes --protect-args --ignore-existing -- \
  '/cygdrive/c/Users/me/Documents/annual-report.pdf' \
  'user@server:/srv/shared/annual-report.pdf'
```

Confirm the source count, destination, and total size before removing
`--dry-run`.

## `MSYS_NO_PATHCONV`

MSYS shells may rewrite arguments that look like POSIX paths when launching a
native Windows program. `MSYS_NO_PATHCONV=1` disables that automatic conversion
for a process:

```sh
MSYS_NO_PATHCONV=1 some-windows-program '/literal/argument'
```

It is not a universal fix. It can preserve an argument that a native program
needs, but it does not teach a Cygwin binary to understand MINGW paths or a
MINGW binary to understand Cygwin paths. First identify the executable, then use
the form that executable expects.

## Why compression is usually unhelpful for ZIP archives

ZIP archives, JPEG images, PNG images, and many other common file types are
already compressed. Rsync's transport compression consumes CPU while finding
little additional size reduction. It can reduce throughput on a fast network,
so Vericopy does not add compression by default.

## Diagnostic

A known mismatch returns:

```text
SOURCE_PATH_DIALECT_MISMATCH

The selected rsync executable uses Cygwin paths, but the source uses Git
Bash/MINGW syntax.

Next: Convert the path with that Cygwin installation's cygpath command, then
run a dry-run preflight.
```
