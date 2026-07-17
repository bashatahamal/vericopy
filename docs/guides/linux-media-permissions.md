# Linux media permissions

A transfer can succeed, verify its checksum, and still produce media that an
application cannot open. Transfer integrity and service-account access are
separate checks.

## Directory traversal and file reads

A service needs execute permission on every parent directory to traverse the
path. It needs read permission on a file to open its bytes. A readable file
under an untraversable parent remains inaccessible.

For `/media/jellyfin/movies/Film.mkv`, inspect each component:

```sh
namei -l /media/jellyfin/movies/Film.mkv
```

Vericopy performs an equivalent mode-and-ownership check without inserting the
destination path into a remote shell command:

```sh
vericopy check-access transfer@server:/media/jellyfin/movies/Film.mkv \
  --as-user jellyfin
```

## Group-based access

A common policy gives the transfer account ownership, a shared media group, and
read-only group access:

```text
directories 2750
files       0640
```

The leading `2` sets the setgid bit on directories. New children normally inherit
the directory's group, which keeps a managed media tree consistent.

```sh
vericopy copy ./Film.mkv transfer@server:/media/jellyfin/movies/Film.mkv \
  --permissions media-readonly \
  --group media \
  --readable-by jellyfin
```

The SSH account must already be allowed to set that group. Vericopy does not run
`sudo` or add users to groups.

## Default ACLs as an advanced option

Default POSIX ACLs can make new files inherit more detailed access rules:

```sh
setfacl -m d:g:media:rx /media/jellyfin/movies
setfacl -m d:m:rx /media/jellyfin/movies
```

ACL administration is intentionally outside Vericopy's initial scope. Apply it
with normal system-management controls, verify the ACL mask, and document the
policy. The current service-user check models ordinary mode bits and groups, so
inspect ACLs separately with `getfacl` when they are in use.

## Why Windows archive modes are risky

Cygwin and compatibility layers can synthesize POSIX-looking bits from Windows
ACLs. Preserving those bits onto Linux can create unexpectedly private,
executable, or inaccessible files. Windows-to-POSIX transfers therefore use an
explicit destination preset by default. `preserve` is opt-in.

## Why `chmod -R 777` is not a fix

World-writable trees let unrelated accounts alter media and replace content.
Recursive changes also erase intentional distinctions across existing files.
Identify the first blocked path component, then change the narrowest owner,
group, directory mode, file mode, or ACL that satisfies the documented policy.

## When filesystem access looks correct

If the service user can traverse and read the file, inspect application causes:

- Jellyfin server logs for the exact library item and scan time.
- FFmpeg logs for codec, container, probe, or hardware-acceleration failures.
- Container bind mounts and user namespaces.
- SELinux or AppArmor denial logs.
- Network filesystem mount options and stale handles.
- Filename encoding and application library configuration.

Do not weaken permissions when the evidence points to a codec, mount, or
application problem.

