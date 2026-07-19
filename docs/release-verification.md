# Release verification

Every tagged release produces six platform archives, a `SHA256SUMS` file, SBOM
files, and GitHub artifact attestations.

These are currently CLI archives. Native desktop packages are a separate
release gate: Wails must build each package on its target operating system, and
Windows/macOS signing credentials must be available before those packages are
published. See the [desktop acceptance checklist](desktop-acceptance.md).

```text
vericopy_windows_amd64.zip
vericopy_windows_arm64.zip
vericopy_darwin_amd64.tar.gz
vericopy_darwin_arm64.tar.gz
vericopy_linux_amd64.tar.gz
vericopy_linux_arm64.tar.gz
SHA256SUMS
```

Do not install a binary when the checksum or attestation fails.

## macOS and Linux

Download the archive and `SHA256SUMS` from the same release page. Then run:

```sh
sha256sum --ignore-missing -c SHA256SUMS
```

macOS ships `shasum` instead:

```sh
grep 'vericopy_darwin_arm64.tar.gz' SHA256SUMS | shasum -a 256 -c -
```

Extract, inspect the build identity, and run the local prerequisite check:

```sh
tar -xzf vericopy_linux_amd64.tar.gz
./vericopy version
./vericopy doctor
```

The reported version must match the release tag. The commit should match the
tagged commit shown by the repository.

## Windows PowerShell

```powershell
$Archive = 'vericopy_windows_amd64.zip'
$Expected = (Select-String -Path .\SHA256SUMS -Pattern $Archive).Line.Split(' ')[0]
$Actual = (Get-FileHash -Algorithm SHA256 $Archive).Hash.ToLowerInvariant()
if ($Actual -ne $Expected.ToLowerInvariant()) {
  throw "Checksum mismatch for $Archive"
}
Expand-Archive $Archive -DestinationPath .\vericopy-release
.\vericopy-release\vericopy.exe version
.\vericopy-release\vericopy.exe doctor
```

## GitHub artifact attestation

With GitHub CLI installed and authenticated:

```sh
gh attestation verify vericopy_linux_amd64.tar.gz \
  --repo bashatahamal/vericopy
```

Repeat for the archive you install and `SHA256SUMS`. Attestation verifies the
GitHub Actions build provenance, not whether the software is safe for every use.

## Maintainer release checklist

1. Confirm `docs/project-status.md` and the acceptance checklist are current.
2. Change `VERSION` from the development identifier to `X.Y.Z`.
3. Move changelog entries under `[X.Y.Z] - YYYY-MM-DD` and update comparison
   links.
4. Run formatting, unit, race, vet, static, vulnerability, integration, and
   cross-build checks.
5. Run `goreleaser release --snapshot --clean` and inspect all archive names,
   contents, checksums, SBOMs, and version output.
6. Commit the release and create a signed `vX.Y.Z` tag.
7. Push only after review. The tag workflow creates a draft release.
8. Verify checksums and attestations from the draft before publishing it.
9. For a desktop release, run `make desktop-package` on each native platform,
   complete the desktop acceptance checklist, sign the packages, and add their
   checksums to the release evidence before publishing.

GitHub Actions, artifact-attestation permissions, `main` required checks,
private vulnerability reporting, Dependabot alerts/security updates, and an
immutable `v*` tag ruleset are enabled for this repository. The release job
requests `contents: write`, `id-token: write`, and `attestations: write` only
in that job. Final release publication remains a manual decision after draft
artifact inspection.

## Reproducible builds

Builds use `-trimpath`, disable VCS auto-embedding, and inject version, commit,
and a commit-derived date. Local Make builds default the date to `unknown` so a
wall clock does not create unexplained differences. Release environments should
derive `BUILD_DATE` from the tagged commit or `SOURCE_DATE_EPOCH`.

Go module downloads are authenticated through `go.sum`. A bit-for-bit rebuild
also depends on matching the Go toolchain, operating system packaging behavior,
and GoReleaser version recorded by the workflow.
