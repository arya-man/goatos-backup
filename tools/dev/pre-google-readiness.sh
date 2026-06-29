#!/usr/bin/env bash
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "$ROOT"

allow_dirty="${GOATOS_PRE_GOOGLE_ALLOW_DIRTY:-0}"
skip_backend="${GOATOS_PRE_GOOGLE_SKIP_BACKEND:-0}"
skip_frontend="${GOATOS_PRE_GOOGLE_SKIP_FRONTEND:-0}"
go_bin="${GO:-go}"

usage() {
  cat <<'EOF'
Usage: tools/dev/pre-google-readiness.sh

Runs the local pre-Google readiness gate. By default this requires a clean
worktree so unrelated dirty files cannot be carried into a Google deployment.

Environment overrides:
  GOATOS_PRE_GOOGLE_ALLOW_DIRTY=1     Report dirty files but keep running.
  GOATOS_PRE_GOOGLE_SKIP_BACKEND=1    Skip focused backend package tests.
  GOATOS_PRE_GOOGLE_SKIP_FRONTEND=1   Skip admin-web typecheck/lint.
  GO=/path/to/go                      Go binary to use for backend tests.
EOF
}

for arg in "$@"; do
  case "$arg" in
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $arg" >&2
      usage >&2
      exit 2
      ;;
  esac
done

step() {
  printf '\n### %s\n' "$1"
}

step "repository boundary"
remote_url="$(git remote get-url origin)"
branch="$(git branch --show-current)"
if [[ "$remote_url" != "https://github.com/vgoats/goatos.git" ]]; then
  echo "origin remote must be https://github.com/vgoats/goatos.git, got: $remote_url" >&2
  exit 1
fi
if [[ "$branch" != "main" ]]; then
  echo "pre-Google gate must run on main, got: $branch" >&2
  exit 1
fi
printf 'remote=%s\nbranch=%s\n' "$remote_url" "$branch"

step "worktree hygiene"
if ! git diff --quiet || ! git diff --cached --quiet || [[ -n "$(git ls-files --others --exclude-standard)" ]]; then
  git status -sb
  if [[ "$allow_dirty" != "1" ]]; then
    echo "worktree must be clean before Google readiness is accepted" >&2
    exit 1
  fi
  echo "GOATOS_PRE_GOOGLE_ALLOW_DIRTY=1 set; continuing with dirty worktree for local reconstruction only."
else
  echo "worktree clean"
fi

step "static guardrails"
git diff --check
bash tools/agent-hooks/check-boundaries.sh
bash tools/agent-hooks/check-contract-drift.sh
make verify-google-dev-seed-fixtures

if [[ "$skip_frontend" != "1" ]]; then
  step "admin-web checks"
  npm --prefix apps/admin-web run typecheck
  npm --prefix apps/admin-web run lint
else
  step "admin-web checks skipped"
fi

if [[ "$skip_backend" != "1" ]]; then
  step "focused backend tests"
  (
    cd backend
    "$go_bin" test \
      ./internal/adminui/app \
      ./internal/bootstrap \
      ./internal/calendar/app \
      ./internal/calendar/adapters/postgres \
      ./internal/protocol/app \
      ./internal/vaccination/app \
      ./internal/obligation/app \
      ./internal/obligation/adapters/postgres \
      ./internal/domainconsumer/app \
      ./internal/domainconsumer/adapters/postgres \
      ./internal/inventory/app \
      ./internal/inventory/adapters/postgres \
      ./internal/sop/app \
      ./internal/sopbridge \
      ./internal/identity/app \
      ./internal/identity/adapters/postgres
  )
else
  step "focused backend tests skipped"
fi

step "pre-Google local readiness OK"
