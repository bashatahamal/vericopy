# Contributing

Vericopy welcomes focused changes that improve its native desktop experience
while preserving strict transfer defaults and cross-platform predictability.

## Before starting

Read the [project status](docs/project-status.md),
[architecture](docs/architecture.md), and [security model](docs/security-model.md).
Open an issue before a large behavior or compatibility change so the guarantee
can be agreed before implementation.

## Development setup

Use Go 1.25 or later and Docker for the isolated OpenSSH suite.

```sh
go mod download
go test ./...
go test -tags desktop ./cmd/vericopy-desktop
go vet ./...
./integration/run.sh
```

Run the full local checks before requesting review:

```sh
make check
make race
make integration
make cross-build
```

If a tool is unavailable, state which check was not run and why.
`integration/run.sh` and `scripts/race-container.sh` default to Docker and also
accept `CONTAINER_RUNTIME=podman` for a compatible local runtime.

## Change rules

- Add tests for success, rejection, and unsafe-input cases.
- Treat the desktop application as the primary product interface. Supporting
  command behavior must not force command-line concepts into the user workflow.
- Keep native transfers free of shell interpolation.
- Never add a host-key bypass or password argument.
- Treat new JSON fields and diagnostics as public interfaces.
- Update the living tracker and changelog for user-visible behavior.
- Document limitations next to guarantees.
- Do not commit keys, hostnames, personal paths, build output, or generated test
  credentials.

Use conventional commit subjects such as `fix(paths): distinguish drive colons`.
Keep commits technical and scoped. Pull requests should explain the problem,
security effect, testing, platform behavior, and any breaking change.
Changes to the desktop workflow or visuals should also record the app states
that were exercised and update the native acceptance checklist when relevant.

## Tests

Unit tests must not require a network or personal SSH material. Integration tests
generate an ephemeral client key, build an isolated OpenSSH container, bind it to
loopback, and remove the container and temporary state after the run.

Avoid timing-based interruption tests. Inject deterministic I/O failure at a
known byte count, then verify resume behavior against the same isolated server.

## Documentation style

Use sentence case, plain language, and concrete evidence. Avoid hype, emoji, and
claims that are not supported by the current code and tests.
