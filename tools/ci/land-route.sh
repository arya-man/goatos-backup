# shellcheck shell=bash
# land-route.sh — WHERE does `make land-main` run?
#
# Maintainer rule (2026-09-25): every Claude/Codex session on the laptop lands
# through the `land` workflow on the single self-hosted runner, so landings are
# serialised FIFO by GitHub and only ONE landing CI runs on the Mac at a time.
# Sessions kept calling `make land-main` directly (the workflow had zero runs),
# which is exactly how two 40-minute landings ended up fighting over one Gradle
# lock. So the handoff lives in code, not in a doc line -- but opt-in per machine:
#
# OPT-IN PER MACHINE: other developers' `make land-main` must never queue on
# the maintainer's runner (it pushes with his token), so nothing changes unless
# the machine opts in:
#   GOATOS_WORKSPACE_ROOT=<dir>  landing from a checkout outside <dir> is refused
#   GOATOS_LAND_VIA_QUEUE=1      laptop landings hand off to land.yml
#
#   inside the runner (GITHUB_ACTIONS=true)             -> local  (the runner IS the queue)
#   test harness (GOATOS_LAND_TEST_MODE=1)               -> local
#   GOATOS_WORKSPACE_ROOT set and checkout outside it    -> refuse
#   GOATOS_LAND_LOCAL=1 (runner / emergency)             -> local
#   GOATOS_LAND_VIA_QUEUE=1                              -> queue  (hand off to land.yml)
#   otherwise                                            -> local  (today's behaviour)
#
# land_route_decide is PURE (reads only its arguments and the environment) so
# tools/ci/land-route.test.sh can pin every branch without git or gh.

land_route_required_root() {
  printf '%s' "${GOATOS_WORKSPACE_ROOT:-}"
}

# land_route_decide <repo-toplevel> -> prints local | queue | refuse
land_route_decide() {
  local repo="$1" root
  case "${GITHUB_ACTIONS:-}" in true|TRUE|1) echo local; return 0 ;; esac
  [ "${GOATOS_LAND_TEST_MODE:-0}" = "1" ] && { echo local; return 0; }
  root="$(land_route_required_root)"
  root="${root%/}"
  if [ -n "$root" ]; then
    case "$repo/" in
      "$root"/*) ;;
      *) echo refuse; return 0 ;;
    esac
  fi
  case "${GOATOS_LAND_LOCAL:-0}" in 1|true|TRUE|yes) echo local; return 0 ;; esac
  case "${GOATOS_LAND_VIA_QUEUE:-0}" in 1|true|TRUE|yes) echo queue; return 0 ;; esac
  echo local
}

land_route_refuse_message() { # repo
  cat >&2 <<EOF
land-main: refusing to land from $1
land-main: GOATOS_WORKSPACE_ROOT says Goat OS work on this machine lives under $(land_route_required_root) only.
land-main: create a worktree there instead:
land-main:   git -C $(land_route_required_root)/goatos worktree add $(land_route_required_root)/goatos-wt-<topic> origin/main
EOF
}

# land_queue_handoff — push the branch, find/create its PR, dispatch land.yml,
# follow the run, and return its exit status. Never runs CI locally.
land_queue_handoff() {
  local repo_slug="vgoats/goatos" branch head remote_sha pr started run_id tries
  command -v gh >/dev/null 2>&1 || { echo "land-main: gh is required to hand off to the land queue" >&2; return 1; }
  branch="$(git symbolic-ref --short -q HEAD || true)"
  [ -n "$branch" ] || { echo "land-main: detached HEAD; check out a branch so the land queue has a PR to land" >&2; return 1; }
  case "$branch" in main|master) echo "land-main: refusing to queue branch '$branch'; land from a topic branch" >&2; return 1 ;; esac
  head="$(git rev-parse HEAD)"

  remote_sha="$(git ls-remote origin "refs/heads/$branch" | awk '{print $1}')"
  if [ "$remote_sha" != "$head" ]; then
    echo "land-main: pushing $branch ($(printf '%.12s' "$head")) for the land queue"
    git mesha-push "HEAD:refs/heads/$branch" || { echo "land-main: push of $branch failed (non-fast-forward? pull/rebase it first)" >&2; return 1; }
  fi

  pr="$(gh pr list -R "$repo_slug" --head "$branch" --state open --json number --jq '.[0].number // empty')"
  if [ -z "$pr" ]; then
    echo "land-main: opening a PR for $branch"
    gh pr create -R "$repo_slug" --head "$branch" --base main --fill >/dev/null || { echo "land-main: could not create a PR for $branch" >&2; return 1; }
    pr="$(gh pr list -R "$repo_slug" --head "$branch" --state open --json number --jq '.[0].number // empty')"
  fi
  [ -n "$pr" ] || { echo "land-main: no open PR for $branch" >&2; return 1; }

  started="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "land-main: handing PR #$pr to the land queue (land.yml on the self-hosted runner, FIFO)"
  gh workflow run land.yml -R "$repo_slug" -f pr="$pr" || { echo "land-main: gh workflow run land.yml failed" >&2; return 1; }

  run_id=""; tries=0
  while [ -z "$run_id" ] && [ "$tries" -lt 30 ]; do
    sleep 2
    run_id="$(gh run list -R "$repo_slug" --workflow land.yml --event workflow_dispatch -L 20 \
      --json databaseId,createdAt,displayTitle \
      --jq "[.[] | select(.createdAt >= \"$started\") | select(.displayTitle == \"land PR $pr\")] | sort_by(.createdAt) | last | .databaseId // empty")"
    tries=$((tries + 1))
  done
  [ -n "$run_id" ] || { echo "land-main: dispatched, but could not find the run; watch: gh run list -R $repo_slug --workflow land.yml" >&2; return 1; }
  echo "land-main: following https://github.com/$repo_slug/actions/runs/$run_id (queued runs wait FIFO for the single runner)"
  gh run watch "$run_id" -R "$repo_slug" --exit-status --interval 30
}
