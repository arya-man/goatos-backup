#!/usr/bin/env bash
# node22.sh — put a Node >= 22 first on PATH for local CI, or refuse to start.
#
# Why: a shell whose PATH starts with Node 20 fails `agent: refresh-binding` and
# `api-latency-policy` (they need --experimental-strip-types). That is a setup
# failure, not a code failure, and it RED-ed a landing twice. Same shape as
# tools/ci/java21.sh: every candidate is version-checked; never guess.
# Sourced; defines node22_major, node22_resolve, node22_export_or_die.

node22_major() {
  # $1 = node binary. Prints the major version, or nothing.
  [ -n "${1:-}" ] && [ -x "$1" ] || return 0
  "$1" --version 2>/dev/null | sed -n '1s/^v\([0-9][0-9]*\).*/\1/p'
}

node22_candidates() {
  # Overridable so the self-test runs without real Node installs.
  if [ -n "${GOATOS_NODE22_CANDIDATES:-}" ]; then
    printf '%s\n' $GOATOS_NODE22_CANDIDATES
    return 0
  fi
  local d
  # nvm v24 first (newest first), then Homebrew, then whatever PATH has.
  for d in $(ls -d "$HOME"/.nvm/versions/node/v24*/bin 2>/dev/null | sort -rV 2>/dev/null || ls -d "$HOME"/.nvm/versions/node/v24*/bin 2>/dev/null); do
    printf '%s\n' "$d/node"
  done
  printf '%s\n' /opt/homebrew/bin/node /usr/local/bin/node
  command -v node 2>/dev/null || true
}

node22_resolve() {
  # Prints the path of a node binary with major >= 22 and returns 0, or prints a
  # reason on stderr and returns 1.
  local candidate major
  while IFS= read -r candidate; do
    [ -n "$candidate" ] || continue
    major="$(node22_major "$candidate")"
    if [ -n "$major" ] && [ "$major" -ge 22 ]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done < <(node22_candidates)
  echo "node22: no Node >= 22 found (PATH node: $(command -v node 2>/dev/null || echo none) $(node --version 2>/dev/null || true)). Install: nvm install 24 (or brew install node)" >&2
  return 1
}

node22_export_or_die() {
  # Prepend the resolved Node's bin dir to PATH (so every child step, npm and npx
  # included, gets it), or exit before any step runs.
  local node
  node="$(node22_resolve)" || { echo "ERROR: local CI needs Node >= 22 (--experimental-strip-types); refusing to start." >&2; exit 1; }
  export PATH="$(dirname "$node"):$PATH"
  hash -r 2>/dev/null || true
}
