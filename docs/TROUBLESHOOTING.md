# Troubleshooting

Every entry here is a failure that actually happened, with the symptom as it
appeared and the cause as it was eventually found. Most of them presented as
silence, which is why the first section matters most.

## Start here: read the log

The desktop app has no console. `desktop.log` is the only record of startup errors,
provider failures and pipeline decisions.

```powershell
Get-Content "$env:USERPROFILE\.elasticclaw\desktop.log" -Tail 50
```

From WSL, note the file may contain a NUL byte, which makes `grep` treat it as
binary and print nothing at all. Always pass `-a`:

```bash
grep -a "stage \"" "/mnt/c/Users/<you>/.elasticclaw/desktop.log" | tail
```

Silently getting no output from `grep` on this file has wasted real time.

### Querying the database directly

The log does not record everything. `hub.db` is SQLite, and the `claws` table is
where the truth lives — status, stage, and the preview columns:

```bash
sqlite3 hub.db "select id, status, pipeline_stage, preview_port, preview_url, preview_ready from claws order by created_at desc limit 3"
```

Copy the file first if the hub is running, and copy `hub.db-wal` alongside it or you
will read stale data.

### Do not trust `/api/claws` for diagnosis

It has returned `[]` while runs were plainly active, and unknown routes return the
SPA's HTML with status 200 rather than a 404 — so a check against a mistyped path
appears to succeed. `/api/instances` does not exist; the route is `/api/claws`.
Verify against the database or the log.

---

## Runs

### A run times out at "Connect" / "Preparing workspace 4/5"

```
[reaper] stopping timed out provisioning claw <id>
[stopAgent] claw <id> stopped: provisioning timed out
```

The agent's container could not reach the hub. Two independent causes, both fixed in
`2026.7.24.28`; if you see this on an older build, update.

1. The container was handed an empty hub URL. `hub.yaml` written by the desktop app
   has no `url` or `public_url` — it passes `--addr` instead — so the
   `127.0.0.1` → `host.docker.internal` rewrite never ran.
2. The hub listened only on `127.0.0.1`. Traffic arriving via
   `host.docker.internal` comes from the Docker network, not loopback, and was
   refused.

If it still happens: confirm Docker Desktop is running, confirm the hub is listening
on all interfaces (`Get-NetTCPConnection -OwningProcess <pid> -State Listen` should
show `0.0.0.0` or `::`), and confirm Windows Firewall has not blocked the app.

### An agent finishes its work and then sits doing nothing

Symptom: the agent emits its marker — `[READY_TO_TEST]` — and the run stays in
`working` indefinitely. No error, nothing in the log but poll ticks. It looks like a
hung or looping agent. It is neither: the stage it should have moved to was
unreachable.

`Trigger.UnmarshalYAML` used to switch over known trigger keys with no `default`
case, so an unsupported or misspelled key parsed into a trigger with every field
empty. A trigger with no condition never matches, so no test gate ran, no branch was
pushed, and no pull request was opened.

Fixed in `2026.7.24.29`: unknown trigger keys are now an error naming the supported
ones, and a trigger with no condition is rejected when the workflow loads.

Supported trigger keys:

```
message_contains  message_line_equals  pr_merged  pr_closed
pr_conditions     judge_verdict        gate_result  output_matches
```

Prefer `message_line_equals` for markers. `message_contains` fires on the agent's own
planning prose — "when the tests pass I'll say `[READY_TO_TEST]`" — which runs the
test gate against a branch with no commits.

### The test gate passes suspiciously fast

Not necessarily wrong. `environment.setup.command` has already installed
dependencies, so the gate itself can be quick. The hub does check the exit code:
`workflow command completed` is logged only on success, and a non-zero exit is
reported as `run action failed` unless the stage has a gate or
`continue_on_error: true`.

### A run disappears

`status=deleted` with no explanation. Stopping or deleting a claw is not currently
logged, so if you did not stop it from the UI there is no record of what did. Check
`hub.db` for the final status; the log will show the bridge disconnecting and the
container being terminated, but not the reason.

---

## Pull requests

### A PR is opened but nothing is ever watched

```
[pr-watcher] CRITICAL: GitHub token resolution failed;
             PR watcher is effectively disabled for 1 tracked PR(s)
```

repeating every minute. Token resolution read only `hub.yaml`'s `github_apps`, but an
App created through the setup flow belongs to a **workspace** and is stored under
`workspaces/<name>/.elasticclaw-managed/github_apps.yaml`. With no hub-level Apps the
list was empty, and the poll loop treats an empty token as fatal and returns before
examining anything.

Everything downstream was silently lost: no CI failures relayed, no review comments
delivered, and `pr_merged` / `pr_closed` could never fire — so any pipeline reaching
`review` stayed there permanently. The agent side was unaffected throughout, because
the credential-helper endpoint already merged workspace Apps. Only the watcher did not.

Fixed in `2026.7.24.30`. When working, the poll logs:

```
[pr-watcher] poll: claw=<id> status=connected pr=https://github.com/<owner>/<repo>/pull/<n>
```

### The agent cannot push or open a PR

Look for `github token issued via app_id=<id> for claw <id>`. If absent, the App is
not installed on the repository, or its permissions are short. The manifest requests
exactly: `contents:write`, `pull_requests:write`, `issues:write`, `checks:read`,
`metadata:read`.

---

## Previews

### A preview URL never appears

Check the database first — this distinguishes the two very different causes:

```bash
sqlite3 hub.db "select preview_port, preview_url, preview_ready from claws order by created_at desc limit 1"
```

**`preview_port` is 0 / `preview_url` empty** — the workflow has no `preview:` block,
or the field was silently dropped by a build without preview support. `preview:` is a
**top-level** workflow key, a sibling of `inputs:` and `stages:`, not a stage key.

**`preview_url` is set but `preview_ready` is 0** — the plumbing is fine and the
agent never completed the handshake. The hub allocates the URL and publishes the
port; it does not start your application. Look for the agent's calls:

```
[hub-proxy] req ... POST /api/claws/<id>/preview/start  → status=200
[hub-proxy] req ... POST /api/claws/<id>/preview/ready  → status=200
```

If those calls are missing, the agent was never told to make them **at a moment it
would act on**. The preview instructions live in `CONTEXT.md`, which is read at agent
start — but the agent follows the live injected message stream over background
context. A workflow whose `review` stage injects only "stay available for CI
failures" gets exactly that: the agent opens the PR, reports it, and stops.

Put the request in the stage `inject` that fires once the pull request is open:

```yaml
- id: review
  triggers:
    - message_line_equals: '[PR_OPENED]'
  on_enter:
    inject: |
      The pull request is open. Now publish the browser preview described in
      CONTEXT.md under "Browser Preview Required": start this repository's web app
      on 0.0.0.0:3000 via the preview start endpoint, verify the route that shows
      your change, then POST that route to the preview ready endpoint.
```

Historically the instructions said to do this "before sending `[DONE]`". Only the
single-stage example workflow ends that way; a gated workflow announces its own
marker and parks in review, so its agent never reached the condition. Fixed in
`2026.7.24.31`, which keys the instruction to the pull request being open instead.

### Ordering hazard

Marking the preview ready **detaches and stops the agent**. An agent that marks it
ready before announcing its stage marker is killed before the pipeline can advance,
and the run strands in the stage it was in. Marker first, preview ready last.

### The preview URL loads nothing

The app must bind `0.0.0.0:<port>` inside the sandbox, not `127.0.0.1:<port>` — a
container-loopback listener is not reachable through the published port. Confirm the
mapping exists:

```powershell
docker ps --format "{{.Names}} {{.Ports}}"
# ec-<shortid>  127.0.0.1:60963->3000/tcp
```

Verify from the **Windows** host, not WSL: the port is published to Windows loopback
only, so WSL cannot reach it.

```powershell
Invoke-WebRequest -Uri "http://127.0.0.1:60963/" -UseBasicParsing
```

Previews also expire. After `ttl` the claw is reaped and the URL stops answering.

---

## Windows specifics

### SmartScreen blocks the download

Expected: the binary is unsigned. **More info → Run anyway**. If a downloaded file
behaves oddly, clear the Mark-of-the-Web: `Unblock-File <path>`.

### Console windows flash while an agent runs

Fixed in `2026.7.24.26`. Every `docker` and helper subprocess is now spawned with
`CREATE_NO_WINDOW`. If you see it again, an unguarded `exec.Command` has been added
somewhere — the guard is `procutil.Hide(cmd)`.

### The window has a red or coloured title bar

Fixed in `2026.7.24.25`. A plain Win32 window gets its caption painted with the
user's accent colour; the app now sets its own. Requires Windows 11 22H2 or later —
older builds keep the system caption, which is harmless.

### PowerShell reports the installer script is unparseable

`install.ps1` must stay pure ASCII. Windows PowerShell 5.1 reads it as CP1252, where
an em dash or smart quote becomes a character that terminates a string literal. This
is only ever caught by parsing the script with real PowerShell — not by reading it.

---

## Development notes

### Tests must not read `$HOME`

A hub fix once resolved workspace GitHub Apps by reading `$HOME` unconditionally. The
package's own tests promptly picked up the developer's real App and made live GitHub
API calls with the private key — failing four tests locally while passing in CI,
where no such directory exists. Loaders that touch the filesystem are injected as
`Server` fields so a test-built `Server` reads nothing.

### `&& echo ok` hides failures

`git apply --check ... && echo ok` prints nothing useful when the check fails, and
piping into `head` returns `head`'s exit status, not the command's. Check the real
exit code before reporting a result.

### Unknown YAML keys are still swallowed at stage level

Trigger keys now error, but unknown **stage** keys do not. That is why a `preview:`
block placed inside a stage does nothing. Making it strict is worthwhile, but it
turns existing silent no-ops into workflows that refuse to load, so it needs porting
work first.
