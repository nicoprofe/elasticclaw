# A thin layer over the Codex-ready agent image that removes two downloads the
# agent-race workflow otherwise repeats on every single run.
#
# Measured on the Windows desktop build: environment setup took 62-123s per run, and
# the variance was network, not work. Two causes, both avoidable here:
#
#   uv is not in the base image, so the workflow's setup command curls the installer
#   every run.
#
#   The base image ships CPython 3.11 and agent-race declares
#   requires-python = ">=3.12", so `uv venv --python 3.12` makes uv download an
#   interpreter every run. Pinning the workflow to 3.11 is not an option.
#
# Both are baked in below, so the workflow's own setup command is unchanged: its
# `[ ! -x "$UV_BIN" ]` guard now finds uv and skips the install, and `uv venv
# --python 3.12` finds an interpreter already on disk.
#
# What is deliberately NOT baked in: agent-race's own dependencies. That would tie
# this image to one repository and make it stale on every lockfile change. The pip
# and npm downloads that remain are the workflow's real work.
#
# Build:
#   docker build -f build/docker/exe-codex-ready.Dockerfile \
#     -t elasticclaw/openclaw-codex-ready:2026.7.1-2-uv312 build/docker
#
# Then point the hub at it:
#   providers.docker.image: elasticclaw/openclaw-codex-ready:2026.7.1-2-uv312

FROM elasticclaw/openclaw-codex-ready:2026.7.1-2

# The base image already runs as node with HOME=/home/node. Both are restated
# because the paths below have to match what the workflow looks for exactly: it
# resolves uv as "$HOME/.local/bin/uv", so installing it anywhere else silently
# changes nothing.
USER node
ENV HOME=/home/node

# Pinned to the version agent-race's setup command pins, so the two cannot drift
# into the workflow downloading a different uv over the top of this one.
ARG UV_VERSION=0.11.31
ARG PYTHON_VERSION=3.12

RUN set -eu; \
    curl -LsSf "https://astral.sh/uv/${UV_VERSION}/install.sh" \
      | env UV_UNMANAGED_INSTALL="${HOME}/.local/bin" sh; \
    "${HOME}/.local/bin/uv" --version

# uv keeps managed interpreters under its own data directory, so this is what makes
# `uv venv --python 3.12` a local operation. The find at the end fails the build if
# the interpreter did not actually land where uv will look for it — an image that
# quietly lacks it would look fine and cost the download on every run.
RUN set -eu; \
    "${HOME}/.local/bin/uv" python install "${PYTHON_VERSION}"; \
    "${HOME}/.local/bin/uv" python find "${PYTHON_VERSION}"

# Leave PATH alone. The workflow calls uv by absolute path and the base image's
# entrypoint is `sh -lc`; adding to PATH here would be inert for the former and
# risks shadowing something for the latter.
