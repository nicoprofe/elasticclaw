# ElasticClaw Desktop (Windows)

`elasticclaw-desktop.exe` is a native Windows application: a real Win32 window
hosting WebView2, with the hub running in-process. It is not a browser tab and
does not depend on which browser is installed.

It is a separate binary from `elasticclaw.exe` on purpose. A CLI needs the console
subsystem so its output lands in a terminal; a desktop app must be linked with
`-H=windowsgui` so double-clicking it does not flash up a console window. One
executable cannot be both.

## Platform support

The **desktop window is Windows-only.** Every file in `cmd/elasticclaw-desktop/` is
built with `//go:build windows`; there is no macOS or Linux desktop build.

macOS and Linux get the **CLI** from the same release — `elasticclaw hub` starts the
hub and you open the built-in dashboard in a browser. One-line install:

```sh
curl -fsSL https://raw.githubusercontent.com/nicoprofe/elasticclaw/main/scripts/install.sh | sh
```

```powershell
# Windows CLI (the desktop app is a separate download)
irm https://raw.githubusercontent.com/nicoprofe/elasticclaw/main/scripts/install.ps1 | iex
```

Both scripts verify the download against the release `checksums.txt` and refuse to
install a binary whose hash does not match. On macOS `install.sh` also clears the
quarantine attribute, because the binaries are not notarized and Gatekeeper would
otherwise refuse to run them.

There is no Homebrew formula for this fork. `brew install elasticclaw` does not
exist in homebrew-core, and upstream's tap installs upstream's build rather than
this one.

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

The app has no console, so `desktop.log` is the only place startup errors, provider
failures and pipeline decisions are recorded. It is appended to, never truncated.

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
