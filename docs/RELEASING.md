# Releasing elasticclaw

Releases are the update channel. Publishing a tag is what makes a new version
reachable by `elasticclaw upgrade` on every installed machine, so the artifact
names in a release are a compatibility contract, not a cosmetic choice.

## The contract

`pkg/release.AssetName()` builds download URLs from these exact names. If a
release is missing one, or names it differently, clients on that platform can no
longer self-update — and the failure only shows up on user machines.

| Asset | Consumer |
| --- | --- |
| `elasticclaw-linux-amd64` | Linux clients, `elasticclaw install`, `elasticclaw hub upgrade` |
| `elasticclaw-linux-arm64` | Linux arm64 clients |
| `elasticclaw-darwin-amd64` | macOS Intel clients |
| `elasticclaw-darwin-arm64` | macOS Apple Silicon clients |
| `elasticclaw-windows-amd64.exe` | Windows clients |
| `elasticclaw-windows-arm64.exe` | Windows arm64 clients |
| `claw-bridge-linux-amd64` | downloaded by the hub into every sandbox at run time |
| `checksums.txt` | verified by `elasticclaw upgrade` and `install.ps1` before installing |

`checksums.txt` is mandatory. The updater refuses to install an artifact that is
absent from it or whose hash disagrees, so a release published without it leaves
clients unable to upgrade (they fail safe rather than installing blindly).

## Versioning

This project uses CalVer: `2026.7.24`, with same-day patches as `2026.7.24.1`,
`2026.7.24.2`, and prereleases as `2026.7.24-beta.1`. Versions are compared
component-by-component as numbers, so `2026.7.9` correctly precedes `2026.7.10`.

## Cutting a release

```bash
git tag 2026.7.24
git push origin 2026.7.24
```

`.github/workflows/release.yml` then runs `go vet`, `go test`, builds every
artifact via `scripts/build-release.sh`, asserts the asset contract above, and
publishes the release. Tags containing a hyphen (`2026.7.24-beta.1`) are
published as prereleases.

## Release channels

A client upgrades to the newest release in its own channel and never crosses
channels, so a prerelease is invisible to stable installs:

- a client on any stable version upgrades to the newest stable tag, including
  across version lines (`2026.7.22` -> `2026.7.24`)
- a client on `2026.7.22-beta.3` upgrades to the newest beta (`2026.7.24-beta.1`)
- `beta -> stable` never happens automatically; that requires a reinstall
- a client never downgrades, so a build ahead of every published release is left
  alone rather than rolled back

Use this to stage a rollout: tag a beta, upgrade your own machine, then tag stable.

Channels are derived from the prerelease suffix alone (`pkg/release.Channel`) and
are deliberately independent of the version numbers. An earlier implementation
derived the upgrade "track" from the leading version components, which meant a
client could only ever move within its own version line — under date-based tags
that stranded every install on the day it was built, with `elasticclaw upgrade`
reporting "no releases found on track 2026.7.22" indefinitely. If you change this
logic, keep `pkg/release/version_test.go` green: it pins that scenario.

## Releasing from a fork

`RELEASE_REPO` is baked into binaries at build time as the self-update source,
and CI sets it to `${{ github.repository }}`. A build produced in
`nicoprofe/elasticclaw` therefore upgrades from that fork, not from upstream — no
source changes needed.

To retarget an already-installed binary without rebuilding it:

```bash
ELASTICCLAW_RELEASE_REPO=owner/repo elasticclaw upgrade
```

## Building locally

```bash
# everything, as CI would build it
VERSION=2026.7.24 RELEASE_REPO=nicoprofe/elasticclaw scripts/build-release.sh

# reuse an existing web build (much faster while iterating)
SKIP_WEB=1 VERSION=2026.7.24 scripts/build-release.sh

# just the Windows exe
make dist-windows VERSION=2026.7.24 RELEASE_REPO=nicoprofe/elasticclaw
```

Artifacts land in `dist/` (git-ignored). The `-tags embedweb` build serves the
dashboard from files embedded in the binary; `make build-web` must have run or
the hub reports "web UI not built" at run time.

## Verifying a release before announcing it

```bash
# the binary knows its own version
./dist/elasticclaw-linux-amd64 version

# the dashboard is really embedded, without touching your own hub config
HOME=$(mktemp -d) ./dist/elasticclaw-linux-amd64 hub \
  --addr 127.0.0.1:18099 --db /tmp/t.db --config /tmp/t.yaml \
  --token t --claw-token c &
curl -fsS -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18099/   # expect 200

# upgrade path works end to end, from a previous release
elasticclaw upgrade
```

## Installing on Windows

```powershell
irm https://raw.githubusercontent.com/nicoprofe/elasticclaw/main/scripts/install.ps1 | iex
```

Unsigned binaries trigger a SmartScreen warning on first download; that is
expected until the release is code-signed.

`scripts/install.ps1` must stay pure ASCII. Windows PowerShell 5.1 decodes a
BOM-less UTF-8 script as CP1252, where a multi-byte character such as an em dash
becomes a smart quote that PowerShell honors as a string delimiter — which
silently corrupts the script on user machines while parsing fine elsewhere.
