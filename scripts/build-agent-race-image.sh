#!/usr/bin/env bash
# Builds the Docker image the desktop app uses for agent-race runs.
#
# Two layers, built in order:
#   -uv312       uv plus a CPython 3.12 interpreter. Generic; nothing invalidates it.
#   -agent-race  agent-race's resolved dependency caches. Stale when its lockfiles move.
#
# The manifests for the second layer are fetched with gh, because agent-race is
# private and a docker build has no credentials of its own. Re-run this whenever
# agent-race changes pyproject.toml or web/package-lock.json.
set -euo pipefail

REPO="${AGENT_RACE_REPO:-nicoprofe/agent-race}"
BASE_TAG="${BASE_TAG:-elasticclaw/openclaw-codex-ready:2026.7.1-2-uv312}"
WARM_TAG="${WARM_TAG:-elasticclaw/openclaw-codex-ready:2026.7.1-2-agent-race}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

ctx="$(mktemp -d)"
trap 'rm -rf "$ctx"' EXIT

echo "→ ${BASE_TAG}"
docker build -f "${HERE}/build/docker/exe-codex-ready.Dockerfile" -t "$BASE_TAG" "$ctx"

echo "→ fetching agent-race manifests from ${REPO}"
mkdir -p "$ctx/web"
fetch() {
  local path="$1" dest="$2"
  gh api "repos/${REPO}/contents/${path}" --jq '.content' | base64 -d > "$dest"
  # A private repo without access yields valid base64 of an error page rather than a
  # hard failure, so check the content is plausible instead of trusting the exit code.
  [[ -s "$dest" ]] || { echo "error: ${path} came back empty" >&2; exit 1; }
  echo "    ${path} ($(wc -c < "$dest") bytes)"
}
fetch pyproject.toml           "$ctx/pyproject.toml"
fetch web/package.json         "$ctx/web/package.json"
fetch web/package-lock.json    "$ctx/web/package-lock.json"

echo "→ ${WARM_TAG}"
docker build -f "${HERE}/build/docker/exe-agent-race-warm.Dockerfile" -t "$WARM_TAG" "$ctx"

echo
echo "✓ built ${WARM_TAG}"
echo "  point the hub at it with providers.docker.image in hub.yaml, then restart the app."
