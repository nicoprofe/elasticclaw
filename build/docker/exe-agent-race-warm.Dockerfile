# Pre-warms the package caches agent-race's workflow setup downloads on every run.
#
# Kept separate from exe-codex-ready.Dockerfile on purpose. That image adds uv and a
# CPython 3.12 interpreter, which any Python workflow benefits from and which nothing
# invalidates. This one embeds one repository's resolved dependency set, so it goes
# stale the moment agent-race changes pyproject.toml or web/package-lock.json — and a
# stale cache is not a failure, just a partial miss, but it is a reason not to mix the
# two concerns in one tag.
#
# Only the caches are warmed, never the installed trees. .venv and node_modules live
# inside the repository workspace, which the hub replaces with a fresh clone on every
# run, so anything written there would be discarded. Warming the caches instead means
# `uv pip install` and `npm ci` still run, still verify, and still produce exactly the
# tree the lockfiles describe — with no network.
#
# Build (from the repository root):
#   scripts/build-agent-race-image.sh
#
# Then point the hub at it:
#   providers.docker.image: elasticclaw/openclaw-codex-ready:2026.7.1-2-agent-race

FROM elasticclaw/openclaw-codex-ready:2026.7.1-2-uv312

USER node
ENV HOME=/home/node

# The manifests are fetched into the build context rather than copied from a clone:
# agent-race is private, so a build here has no credentials to clone it with.
COPY --chown=node:node pyproject.toml /tmp/warm/pyproject.toml
COPY --chown=node:node web/package.json web/package-lock.json /tmp/warm/web/

# Warm uv's wheel cache.
#
# `uv pip install '.[dev]'` cannot be used: setuptools is configured to discover
# api*, runner* and suites*, none of which exist in this context, so building the
# project's metadata fails. The dependency lists are read straight out of
# pyproject.toml instead, which keeps that file the single source of truth rather
# than duplicating the pins into this Dockerfile where they would silently drift.
#
# The venv is thrown away; only ~/.cache/uv is kept.
RUN set -eu; \
    cd /tmp/warm; \
    python3 - <<'PY' > requirements.txt
import tomllib
with open("pyproject.toml", "rb") as fh:
    data = tomllib.load(fh)
project = data["project"]
deps = list(project.get("dependencies", []))
# The workflow installs the dev extra, so warm that too. Other extras (proxy,
# swebench) are not installed by the workflow and would bloat the image.
deps += list(project.get("optional-dependencies", {}).get("dev", []))
print("\n".join(deps))
PY
RUN set -eu; \
    cd /tmp/warm; \
    UV="$HOME/.local/bin/uv"; \
    UV_NO_PROGRESS=1 "$UV" venv --python 3.12 .venv-warm; \
    UV_NO_PROGRESS=1 "$UV" pip install --python .venv-warm/bin/python -r requirements.txt; \
    rm -rf .venv-warm; \
    du -sh "$HOME/.cache/uv" | sed 's/^/uv cache: /'

# Warm npm's tarball cache. `npm ci` requires package.json and its lockfile to agree,
# which is why both are copied. node_modules is removed afterwards; ~/.npm is kept.
RUN set -eu; \
    cd /tmp/warm/web; \
    npm ci --no-audit --no-fund; \
    rm -rf node_modules; \
    du -sh "$HOME/.npm" | sed 's/^/npm cache: /'

# Leave nothing behind that could be mistaken for the real checkout.
RUN rm -rf /tmp/warm
