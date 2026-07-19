# Desktop release verification

Vericopy releases are native desktop applications. A public release is ready
only when the app has been built and accepted on Windows, macOS, and Linux and
the published package for each platform has matching checksum and provenance
evidence.

The repository does not currently publish signed desktop installers. Existing
development executables and command-interface archives are engineering
artifacts, not the end-user product release.

## Required release artifacts

The exact package extensions follow each platform's native packaging decision,
but a release must contain:

- a signed Windows desktop package;
- a signed and notarized macOS desktop package;
- an accepted Linux desktop package;
- `SHA256SUMS` covering every published package;
- build provenance or GitHub artifact attestations;
- release notes and the relevant security limitations.

Do not publish or install a package when its checksum, signature, notarization,
or provenance check fails.

## Native build rule

Build each Wails package on the operating system where it will run:

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
make desktop-package
```

Generic Go cross-compilation is useful for testing the shared engine, but it
does not produce an accepted native Wails application package.

## Acceptance rule

Before signing a package, complete the
[desktop acceptance checklist](desktop-acceptance.md) on that exact artifact.
At minimum, verify:

1. native launch and window behavior;
2. light, dark, keyboard, reduced-motion, and narrow-window states;
3. strict rejection of unknown and changed host keys;
4. key/agent and one-time password authentication against a disposable host;
5. file and folder transfer, bounded multi-job queuing, minimized progress,
   resume, cancellation, overwrite protection, and permission policy;
6. remote readback and SHA-256 result evidence;
7. saved-session persistence and password non-persistence;
8. restart recovery, password re-entry, redacted activity, and job/history
   clearing behavior;
9. package signature or notarization status;
10. artifact SHA-256 and recorded build identity.

## Checksum verification

Linux:

```sh
sha256sum --ignore-missing -c SHA256SUMS
```

macOS:

```sh
shasum -a 256 -c SHA256SUMS
```

Windows PowerShell:

```powershell
$Package = 'Vericopy-package-name'
$Expected = (Select-String -Path .\SHA256SUMS -Pattern $Package).Line.Split(' ')[0]
$Actual = (Get-FileHash -Algorithm SHA256 $Package).Hash.ToLowerInvariant()
if ($Actual -ne $Expected.ToLowerInvariant()) {
  throw "Checksum mismatch for $Package"
}
```

## GitHub artifact attestation

With the GitHub command-line client installed and authenticated:

```sh
gh attestation verify Vericopy-package-name \
  --repo bashatahamal/vericopy
```

Attestation verifies build provenance. It does not replace the native package
signature, acceptance testing, or the user's decision to trust the software.

## Maintainer release checklist

1. Confirm [project status](project-status.md), the changelog, security model,
   and desktop acceptance evidence are current.
2. Change `VERSION` from the development identifier to `X.Y.Z`.
3. Move changelog entries under `[X.Y.Z] - YYYY-MM-DD`.
4. Run unit, desktop-tagged, race, vet, static, vulnerability, OpenSSH
   integration, and frontend checks.
5. Build the desktop package independently on Windows, macOS, and Linux.
6. Complete acceptance against each exact package.
7. Sign or notarize the applicable packages.
8. Generate and independently verify checksums and provenance.
9. Create a signed `vX.Y.Z` tag and a draft GitHub release.
10. Inspect the downloaded draft artifacts before publishing the release.

Final publication remains a manual decision. A passing engine build or command
archive alone is not sufficient evidence for a desktop release.

## Reproducibility notes

Builds use `-trimpath`, disable automatic VCS embedding, and inject version,
commit, and build date. Reproducing a desktop package also requires the recorded
Go version, Wails version, native compiler and WebView dependencies, operating
system packaging tools, and signing configuration.
