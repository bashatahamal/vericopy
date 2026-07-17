# Architecture

Vericopy separates parsing, security policy, transport, transfer state, and
presentation. Security-sensitive boundaries accept injected interfaces so tests
can exercise rejection and failure paths without a production server.

## Components

```mermaid
---
config:
  theme: base
  themeVariables:
    primaryColor: '#faf9f7'
    primaryTextColor: '#1d1c1a'
    primaryBorderColor: '#0e6e55'
    lineColor: '#52504b'
    secondaryColor: '#ffffff'
    tertiaryColor: '#e6e3dc'
    clusterBkg: '#ffffff'
    clusterBorder: '#a16f0b'
---
flowchart TB
    CLI["CLI layer<br>Cobra commands and stable output"]
    Parse["Input boundary<br>local paths, remote specs, modes"]
    Policy["Policy layer<br>overwrite, permissions, diagnostics"]
    SSH["SSH boundary<br>agent or key, strict known_hosts"]
    Engine["Transfer engine<br>partial state, resume, SHA-256"]
    SFTP["Native SFTP adapter<br>files and metadata"]
    Access["Access checker<br>validated account lookup and SFTP stat"]
    Rsync["Optional rsync adapter<br>dialect preflight and argument vector"]
    Remote["Remote OpenSSH server<br>SFTP subsystem"]

    CLI --> Parse --> Policy
    Policy --> SSH --> SFTP --> Remote
    Policy --> Engine --> SFTP
    Policy --> Access --> SFTP
    Policy -. explicit selection .-> Rsync
```

## Native transfer sequence

```mermaid
---
config:
  theme: base
  themeVariables:
    actorBkg: '#faf9f7'
    actorBorder: '#0e6e55'
    actorTextColor: '#1d1c1a'
    signalColor: '#52504b'
    signalTextColor: '#1d1c1a'
    noteBkgColor: '#ffffff'
    noteBorderColor: '#a16f0b'
    labelBoxBkgColor: '#e6e3dc'
    labelBoxBorderColor: '#0e6e55'
---
sequenceDiagram
    participant U as User<br>or automation
    participant V as Vericopy<br>transfer engine
    participant S as SSH and SFTP<br>server
    U->>V: copy SOURCE DESTINATION<br>resume and policy
    V->>V: Classify source<br>parse destination
    V->>S: SSH handshake<br>strict known_hosts callback
    S-->>V: Authenticated SFTP channel
    V->>S: Stat destination<br>reject existing by default
    V->>V: Hash source prefix<br>bind partial metadata
    V->>S: Validate or create<br>0600 partial state
    V->>S: Seek and upload<br>retain state on interruption
    V->>V: Re-stat source<br>detect concurrent change
    V->>V: Calculate local<br>size and SHA-256
    V->>S: Read partial through SFTP<br>calculate size and SHA-256
    Note over V,S: Finalization continues only<br>when size and digest match
    V->>S: Apply mode, group,<br>and optional time policy
    V->>S: Rename partial<br>to final destination
    V->>S: Remove resume metadata
    V-->>U: Verified result<br>human or JSON
```

## Package responsibilities

| Package | Responsibility |
| --- | --- |
| `internal/app` | Commands, flags, dependency assembly, output selection |
| `internal/localpath` | Path dialect classification and runtime normalization |
| `internal/remote` | Safe `[user@]host:path` parsing and traversal rejection |
| `internal/sshclient` | Authentication and strict host-key verification |
| `internal/backend/sftp` | Native SFTP filesystem adapter |
| `internal/transfer` | Copy, resume, verification, policy, and finalization |
| `internal/permissions` | Presets, octal validation, and formatting |
| `internal/access` | Remote user/group lookup and POSIX read/traverse checks |
| `internal/backend/rsync` | Optional executable classification and safe arguments |
| `internal/verrors` | Stable diagnostic codes and exit categories |
| `internal/output` | Stable JSON envelope and concise human rendering |

## Partial state

For destination `movie.mkv`, Vericopy creates a hidden name resembling:

```text
.movie.mkv.vericopy-f6247a91b18dbe31.partial
.movie.mkv.vericopy-f6247a91b18dbe31.partial.json
```

The suffix binds the destination, source size, and source prefix digest. The
sidecar records a schema version, source size, modification time, prefix length,
and prefix SHA-256. It does not record a local path, credential, or host secret.

Before resume, Vericopy checks the metadata, ensures the partial is not larger
than the source, and compares the partial bytes available within the validation
prefix with the same local bytes. This detects accidental reuse of unrelated
state. It cannot prove that matching bytes beyond the validated prefix are
unchanged until the final whole-file SHA-256 readback. Finalization never occurs
before that whole-file comparison.

## Finalization and concurrency

The default is no overwrite. Vericopy stats the destination before work and the
SFTP server processes the final rename. Filesystem rename semantics vary, so the
operation is described as best-effort atomic. With `--overwrite`, an existing
file is removed immediately before rename because portable SFTP replacement
semantics differ across servers. That creates a short replacement window and is
why overwrite must be explicit.

## Cancellation

The process derives a context from `SIGINT` and `SIGTERM`. Source reads observe
that context. Ordinary interruption retains compatible partial state and its
restrictive metadata so `--resume` can continue safely.

