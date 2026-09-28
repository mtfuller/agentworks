# Release and installation

Tagged releases are built for macOS and Linux on amd64/arm64 and Windows on amd64.
The release workflow refuses to publish an unsigned build. macOS binaries are signed with a
Developer ID certificate and submitted to Apple's notarization service; Windows binaries are
Authenticode-signed and timestamped. GitHub also publishes a build-provenance attestation and a
SHA-256 manifest.

The repository must configure these release secrets:

- `MACOS_CERTIFICATE_P12`, `MACOS_CERTIFICATE_PASSWORD`, `MACOS_SIGNING_IDENTITY`
- `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD`
- `WINDOWS_CERTIFICATE_PFX`, `WINDOWS_CERTIFICATE_PASSWORD`

The scheduled authenticated runtime-conformance workflow additionally requires:

- `CLAUDE_CODE_OAUTH_TOKEN`, `COPILOT_GITHUB_TOKEN`
- `JIRA_BASE_URL`, `JIRA_EMAIL`, `JIRA_API_TOKEN`
- `CONNECTOR_GITHUB_REPOSITORY`, `CONNECTOR_GITHUB_TOKEN`

Missing conformance credentials fail the scheduled job rather than silently skipping a service.

Tag-driven release jobs are blocked while
[`docs/operations/m9-release-evidence.yaml`](operations/m9-release-evidence.yaml) is pending.
Approve it only after attaching the completed soak, real Jira/GitHub software-factory dogfood,
green authenticated-conformance run, security review, tested commit, and reviewer identity.

Release archives include native user-level install and uninstall scripts. On macOS/Linux run
`./install.sh`; on Windows run `./install.ps1`. The default destinations are `~/.local/bin` and
`%LOCALAPPDATA%\AgentWorks\bin`. `AGENTWORKS_INSTALL_DIR` or the scripts' explicit destination
arguments override them. Uninstall removes only the executable and deliberately preserves
projects and local runtime history.

## Channels

GitHub Releases are the source of truth. Stable tags use `vX.Y.Z`; release candidates use
`vX.Y.Z-rc.N`. The release workflow rejects every other tag shape and requires the tag to descend
from the approved tested commit with no source changes; the sole permitted difference is the
committed release-evidence file. This makes the soak evidence reviewable without allowing a later
code change to inherit its approval. The workflow then runs build, formatting, vet, race-test,
and coverage preflight before it signs or publishes an archive.

On macOS, Homebrew has two intentionally separate formulae:

```sh
# Stable only; this never follows a release candidate.
brew install mtfuller/tap/agentworks

# Explicitly opt into the newest published release candidate.
brew install mtfuller/tap/agentworks-rc
```

The formulae conflict because both install an `agentworks` executable. Uninstall or unlink one
before linking the other. Publishing a prerelease updates only `agentworks-rc`; publishing a
stable release updates only `agentworks`.

For Linux and Windows, download the archive for an exact tag from GitHub Releases rather than a
moving channel. Verify it before extracting:

```sh
sha256sum -c SHA256SUMS                 # Linux
shasum -a 256 -c SHA256SUMS             # macOS
gh attestation verify ARCHIVE -R mtfuller/agentworks
```

On Windows, use `Get-FileHash ARCHIVE -Algorithm SHA256` and compare its value with
`SHA256SUMS`, then verify the executable's Authenticode signature in Explorer or with
`Get-AuthenticodeSignature`. GitHub's provenance attestation is optional but recommended for
all platforms.

macOS release archives contain a Developer ID-signed standalone binary submitted to Apple's
notary service. Standalone binaries cannot carry a stapled ticket, so the first Gatekeeper check
needs network access. A future `.pkg` or `.dmg` distribution can add an offline-stapled option.

## Release operator checklist

1. Merge the release workflow, Homebrew workflows, and the intended runtime changes to `main`.
2. Configure the signing, notarization, authenticated-conformance, connector, and tap secrets
   listed above.
3. Complete the M9 soak and dogfood evidence for a tested commit. Commit the approved evidence
   document without changing any other file, then tag that evidence-only descendant.
4. Push `vX.Y.Z-rc.N` for an opt-in RC, validate its GitHub assets and `agentworks-rc` formula,
   and run Studio from the released archive on each supported host.
5. After RC acceptance, repeat with `vX.Y.Z`; only that stable tag updates the default formula.

Studio remains a foreground application in the first local release. The installers do not
register launchd, systemd, Windows services, login items, or scheduled tasks. Service
installation will remain unavailable until foreground Studio completes the M9 soak and its
restart behavior is considered stable enough to run unattended.
