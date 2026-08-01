# ElasticClaw Desktop

The desktop app is a native window hosting the platform's own web view, with the hub
running in-process. It is not a browser tab and does not depend on which browser is
installed: WebView2 on Windows, WKWebView on macOS, WebKitGTK on Linux.

It is a separate binary from the `elasticclaw` CLI on purpose. A CLI needs the
console subsystem so its output lands in a terminal; a desktop app must be linked
with `-H=windowsgui` on Windows so double-clicking it does not flash up a console
window. One executable cannot be both.

## Platform support

| Platform | What ships | Install |
| --- | --- | --- |
| Windows | `elasticclaw-desktop-windows-{amd64,arm64}.exe` | double-click, then accept the install offer |
| macOS | `ElasticClaw-macos.zip` — a universal `ElasticClaw.app` | `install.sh`, or unzip and drag to Applications |
| Linux | `elasticclaw-desktop-linux-amd64` | `install.sh` |

The one-line installers handle the CLI **and** the desktop app:

```sh
curl -fsSL https://raw.githubusercontent.com/nicoprofe/elasticclaw/main/scripts/install.sh | sh
```

```powershell
# Windows CLI (the desktop app is a separate download)
irm https://raw.githubusercontent.com/nicoprofe/elasticclaw/main/scripts/install.ps1 | iex
```

Both verify every download against the release `checksums.txt` and refuse to install
anything whose hash does not match. Set `ELASTICCLAW_SKIP_DESKTOP=1` to install only
the command-line tool.

There is no Homebrew formula for this fork. `brew install elasticclaw` does not
exist in homebrew-core, and upstream's tap installs upstream's build rather than
this one.

## Install (macOS)

`install.sh` is the recommended route, and not only for convenience: it downloads
with curl, which sets no quarantine attribute, so the installed app opens without a
Gatekeeper warning. It puts `ElasticClaw.app` in `/Applications` — or
`~/Applications` on a Mac where the first is not writable.

To install by hand instead, download **`ElasticClaw-macos.zip`** from the
[releases page](https://github.com/nicoprofe/elasticclaw/releases/latest), unzip it,
and drag `ElasticClaw.app` to Applications. On first launch from a browser download,
macOS says the developer cannot be verified — the app is ad-hoc signed but not
notarized. Open **System Settings → Privacy & Security** and choose **Open Anyway**,
or clear the flag yourself:

```sh
xattr -dr com.apple.quarantine /Applications/ElasticClaw.app
```

Run the app from anywhere and it offers, once, to move itself to Applications;
`--install` and `--uninstall` do the same from a terminal.

> The release deliberately does **not** publish a bare `elasticclaw-desktop-darwin-*`
> executable. A browser saves one without the execute bit and Finder will not launch
> it, so it looks like a download that does nothing at all. Only the `.app` bundle is
> installable on macOS.

## Install (Linux)

`install.sh` installs `elasticclaw-desktop` alongside the CLI, registers a desktop
entry and icon under `$XDG_DATA_HOME`, and tells you if WebKitGTK is missing.

Unlike every other binary here, the Linux desktop app is dynamically linked and needs
**libwebkit2gtk-4.1** at runtime. Without it the dynamic loader kills the process
before any of this code runs, which from a desktop icon looks like nothing happening:

```sh
sudo apt install libwebkit2gtk-4.1-0     # Debian/Ubuntu
sudo dnf install webkit2gtk4.1           # Fedora
sudo pacman -S webkit2gtk-4.1            # Arch
```

Only the release runner's own architecture (amd64) is built, because cgo
cross-compilation would need a full sysroot for the other one. On arm64, run
`elasticclaw hub` and use the dashboard in a browser.

`--install` and `--uninstall` manage the desktop entry; the binary itself is left
where it is.

## Install (Windows desktop)

Download from the landing page or the
[releases page](https://github.com/nicoprofe/elasticclaw/releases/latest), then
double-click. On first run it offers to install itself; accepting copies it to

```
%LOCALAPPDATA%\Programs\ElasticClaw\
```

and adds Start menu and desktop shortcuts plus an Add or Remove Programs entry.
This is a per-user install, so it needs no administrator rights.

Windows shows a SmartScreen warning because the binary is not code-signed. Choose
**More info → Run anyway**. Signing requires a certificate; it is not in place yet.

`--install` and `--uninstall` are also available from a terminal.

## Where things live

| Path | Contents |
| --- | --- |
| `%LOCALAPPDATA%\Programs\ElasticClaw\` | the installed executable |
| `%USERPROFILE%\.elasticclaw\hub.yaml` | hub config: providers, LLM keys, UI password |
| `%USERPROFILE%\.elasticclaw\hub.db` | runs, messages, pull requests, previews |
| `%USERPROFILE%\.elasticclaw\workspaces\<name>\` | workspace config and workflows |
| `%USERPROFILE%\.elasticclaw\workspaces\<name>\.elasticclaw-managed\` | GitHub App credentials |
| `%USERPROFILE%\.elasticclaw\desktop.log` | **the log — read this first when anything fails** |

On macOS and Linux the data directory is `~/.elasticclaw/` with the same contents,
including `desktop.log`. The app itself lives in:

| Path | Contents |
| --- | --- |
| `/Applications/ElasticClaw.app` (or `~/Applications/`) | the macOS app bundle |
| `~/.local/bin/elasticclaw-desktop` | the Linux binary |
| `~/.local/share/applications/elasticclaw.desktop` | the Linux desktop entry |
| `~/.local/share/icons/hicolor/512x512/apps/elasticclaw.png` | the Linux icon |

The app has no console, so `desktop.log` is the only place startup errors, provider
failures and pipeline decisions are recorded. It is appended to, never truncated.

## Building the desktop app

macOS and Linux both need cgo — WKWebView and WebKitGTK cannot be cross-compiled —
so each is built on its own platform by its own release job:

```sh
DESKTOP_TARGET=darwin VERSION=v0.0.0-dev ./scripts/build-release.sh   # on a Mac
DESKTOP_TARGET=linux  VERSION=v0.0.0-dev ./scripts/build-release.sh   # on Linux
```

The macOS run also assembles and ad-hoc signs `ElasticClaw.app` via
[`scripts/package-macos-app.sh`](../scripts/package-macos-app.sh), which can be run
on its own against binaries you already have. The bundle's icon is
`build/macos/ElasticClaw.icns`, checked in and regenerated by
`python3 scripts/make-macos-icns.py` only when the source artwork changes.

Two things in the bundle are load-bearing and easy to lose in a refactor:
`NSAllowsLocalNetworking` in `Info.plist`, without which App Transport Security
blocks the `http://127.0.0.1` the window points at and the app opens to a blank
window; and the ad-hoc signature, without which Apple Silicon refuses to execute the
binary at all. `lipo` strips the signature the Go linker applies, so the merged
executable must be re-signed after merging. Both are asserted by the release
workflow; the bundle's structure is asserted by `go test ./scripts/`.

## Updating

The app checks for newer releases and can update itself. Updates are verified
against `checksums.txt` before the running executable is replaced, and the previous
copy is kept alongside so a failed swap can be rolled back.

To update by hand, download the new exe and run it with `--install`.

## Signing in

The UI password is generated on first run and stored in `hub.yaml`:

```powershell
Select-String -Path "$env:USERPROFILE\.elasticclaw\hub.yaml" -Pattern "^ui_password:"
```

## Network exposure — read this

The hub binds `0.0.0.0`, not just loopback. This is required: agents run in Docker
containers and reach the host through `host.docker.internal`, which arrives from the
Docker network rather than over loopback. A hub bound to `127.0.0.1` refuses those
connections and every run dies at the Connect stage.

The consequence is that the hub is reachable from your local network. It requires
its token, so what is exposed is authenticated, but on an untrusted network this is
a real consideration. Windows Firewall will prompt on first run; the app needs the
allowance to function with the Docker provider.

## How a run works

1. **Provision** — the provider starts a sandbox. Docker publishes any preview port.
2. **Connect** — `claw-bridge` inside the sandbox dials the hub back and authenticates.
3. **Setup** — the workflow's `environment.setup.command` prepares the repository
   (dependency installs and similar).
4. **Preflight** — `environment.preflight` validates the prepared repository.
5. **Working** — the entry stage's `inject` is delivered and the agent starts.
6. **Test** — entered when the agent emits the stage's trigger marker. The hub runs
   the stage's `run.command` and checks its exit code before continuing.
7. **Review** — entered on the next marker. The pull request is tracked from here.
8. **Merged / Closed** — terminal, driven by `pr_merged` and `pr_closed`.

Each transition is logged. If a run appears stuck, the log names the stage it is in.

## Providers

`docker` and `daytona` are both supported, and a workflow may offer a choice at
manual-trigger time:

```yaml
allowed_providers:
  - provider: docker
    label: Local Docker
  - provider: daytona
    label: Daytona cloud
```

A provider must also be configured in `hub.yaml`, or selecting it fails with
"provider is not configured on this hub". Docker requires Docker Desktop to be
running. Browser previews work on `docker` and `daytona` only.

## Browser previews

A workflow that declares a preview gets an ephemeral, credential-free URL for
reviewer QA:

```yaml
preview:
  port: 3000
  label: Open QA preview
  ttl: 30m
```

The hub publishes the port through the provider and allocates the URL when the
sandbox starts. It does **not** start your application — the agent does, because
only the agent knows the repository's own start command. The agent is instructed to:

1. start the app bound to `0.0.0.0:<port>` via `POST /api/claws/<id>/preview/start`,
   so the hub owns the process and it survives past the tool call that launched it;
2. verify the specific route showing the change;
3. `POST /api/claws/<id>/preview/ready` with that route.

Only then does the link appear on the agent card.

**Marking the preview ready stops the agent.** The claw becomes `status=preview` and
retains only the preview until the TTL expires. A workflow whose review stage asks
the agent to stay available for CI feedback cannot also have a published preview for
that run — you get one or the other.

Because the agent must be *told* at the right moment, the instruction belongs in the
stage `inject` that fires once the pull request is open. Instructions in `CONTEXT.md`
alone are not reliable: that file is read at agent start, and the agent follows the
live injected message stream over background context. See
[TROUBLESHOOTING.md](TROUBLESHOOTING.md#a-preview-url-never-appears).
