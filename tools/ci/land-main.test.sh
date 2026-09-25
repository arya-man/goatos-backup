#!/usr/bin/env bash
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="$repo/tools/ci/land-main.sh"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/goatos-land-main-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
# The real default lock is machine-wide; never let this self-test touch it (a
# real landing holding it would make the test refuse its own runs).
export GOATOS_LAND_MAIN_LOCK_DIR="$tmp/selftest-default.lock"

git init --bare --initial-branch=main "$tmp/origin.git" >/dev/null
git init --initial-branch=main "$tmp/seed" >/dev/null
git -C "$tmp/seed" config user.name "GoatOS Test"
git -C "$tmp/seed" config user.email "goatos-test@example.invalid"
printf 'base\n' >"$tmp/seed/base.txt"
git -C "$tmp/seed" add base.txt
git -C "$tmp/seed" commit -m base >/dev/null
git -C "$tmp/seed" remote add origin "$tmp/origin.git"
git -C "$tmp/seed" push -u origin main >/dev/null

git clone "$tmp/origin.git" "$tmp/candidate" >/dev/null 2>&1
git clone "$tmp/origin.git" "$tmp/publisher" >/dev/null 2>&1
for checkout in candidate publisher; do
  git -C "$tmp/$checkout" config user.name "GoatOS Test"
  git -C "$tmp/$checkout" config user.email "goatos-test@example.invalid"
done

git -C "$tmp/candidate" switch -c feature >/dev/null
printf 'candidate\n' >"$tmp/candidate/candidate.txt"
git -C "$tmp/candidate" add candidate.txt
git -C "$tmp/candidate" commit -m candidate >/dev/null

printf 'main moved\n' >"$tmp/publisher/main.txt"
git -C "$tmp/publisher" add main.txt
git -C "$tmp/publisher" commit -m main-moved >/dev/null
git -C "$tmp/publisher" push origin main >/dev/null

cat >"$tmp/fake-ci.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
git merge-base --is-ancestor origin/main HEAD
[ -z "\$(git status --porcelain --untracked-files=all)" ]
count=0
[ ! -f "$tmp/ci-count" ] || count="\$(cat "$tmp/ci-count")"
count=\$((count + 1))
printf '%s\n' "\$count" >"$tmp/ci-count"
if [ "\$count" -eq 1 ]; then
  printf 'main raced\n' >"$tmp/publisher/race.txt"
  git -C "$tmp/publisher" add race.txt
  git -C "$tmp/publisher" commit -m main-raced >/dev/null
  git -C "$tmp/publisher" push origin main >/dev/null
fi
EOF
chmod +x "$tmp/fake-ci.sh"

(
  cd "$tmp/candidate"
  GOATOS_LAND_TEST_MODE=1 \
    GOATOS_LAND_TEST_CI_COMMAND="$tmp/fake-ci.sh" \
    bash "$script"
)

test "$(cat "$tmp/ci-count")" = "2"
git -C "$tmp/candidate" merge-base --is-ancestor origin/main HEAD
test -f "$tmp/candidate/candidate.txt"
test -f "$tmp/candidate/main.txt"
test -f "$tmp/candidate/race.txt"

git clone "$tmp/origin.git" "$tmp/reuse-candidate" >/dev/null 2>&1
git clone "$tmp/origin.git" "$tmp/reuse-publisher" >/dev/null 2>&1
for checkout in reuse-candidate reuse-publisher; do
  git -C "$tmp/$checkout" config user.name "GoatOS Test"
  git -C "$tmp/$checkout" config user.email "goatos-test@example.invalid"
done

git -C "$tmp/reuse-candidate" switch -c reuse-feature >/dev/null
printf 'reuse candidate\n' >"$tmp/reuse-candidate/reuse-candidate.txt"
git -C "$tmp/reuse-candidate" add reuse-candidate.txt
git -C "$tmp/reuse-candidate" commit -m reuse-candidate >/dev/null

cat >"$tmp/fake-ci-with-receipt.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
git merge-base --is-ancestor origin/main HEAD
[ -z "\$(git status --porcelain --untracked-files=all)" ]
count=0
[ ! -f "$tmp/reuse-ci-count" ] || count="\$(cat "$tmp/reuse-ci-count")"
count=\$((count + 1))
printf '%s\n' "\$count" >"$tmp/reuse-ci-count"
sha="\$(git rev-parse HEAD)"
node "$repo/tools/ci/check-local-ci-evidence.mjs" --record "\$sha" --mode all --base "\$(git rev-parse origin/main)" --screenshots skipped
if [ "\$count" -eq 1 ]; then
  printf 'reuse race\n' >"$tmp/reuse-publisher/reuse-race.txt"
  git -C "$tmp/reuse-publisher" add reuse-race.txt
  git -C "$tmp/reuse-publisher" commit -m reuse-race >/dev/null
  git -C "$tmp/reuse-publisher" push origin main >/dev/null
fi
EOF
chmod +x "$tmp/fake-ci-with-receipt.sh"

(
  cd "$tmp/reuse-candidate"
  GOATOS_LAND_TEST_MODE=1 \
    GOATOS_LAND_TEST_CI_COMMAND="$tmp/fake-ci-with-receipt.sh" \
    bash "$script"
)

test "$(cat "$tmp/reuse-ci-count")" = "1"
git -C "$tmp/reuse-candidate" merge-base --is-ancestor origin/main HEAD
test -f "$tmp/reuse-candidate/reuse-candidate.txt"
test -f "$tmp/reuse-candidate/reuse-race.txt"

git clone "$tmp/origin.git" "$tmp/bypass-candidate" >/dev/null 2>&1
git -C "$tmp/bypass-candidate" config user.name "GoatOS Test"
git -C "$tmp/bypass-candidate" config user.email "goatos-test@example.invalid"
git -C "$tmp/bypass-candidate" switch -c bypass-feature >/dev/null
printf 'bypass candidate\n' >"$tmp/bypass-candidate/bypass-candidate.txt"
git -C "$tmp/bypass-candidate" add bypass-candidate.txt
git -C "$tmp/bypass-candidate" commit -m bypass-candidate >/dev/null

cat >"$tmp/fake-ci-should-not-run.sh" <<'EOF'
#!/usr/bin/env bash
echo "bypass self-test: CI command unexpectedly ran" >&2
exit 99
EOF
chmod +x "$tmp/fake-ci-should-not-run.sh"

if (
  cd "$tmp/bypass-candidate"
  GOATOS_LAND_TEST_MODE=1 \
    GOATOS_BYPASS_LOCAL_CI=1 \
    GOATOS_LAND_TEST_CI_COMMAND="$tmp/fake-ci-should-not-run.sh" \
    bash "$script"
) >"$tmp/bypass.out" 2>&1; then
  echo "land-main self-test: GOATOS_BYPASS_LOCAL_CI should have been rejected" >&2
  exit 1
fi
grep -q "GOATOS_BYPASS_LOCAL_CI is not supported" "$tmp/bypass.out"

printf 'dirty\n' >>"$tmp/candidate/candidate.txt"
if (
  cd "$tmp/candidate"
  GOATOS_LAND_TEST_MODE=1 \
    GOATOS_LAND_TEST_CI_COMMAND="$tmp/fake-ci.sh" \
    bash "$script"
) >"$tmp/dirty.out" 2>&1; then
  echo "land-main self-test: dirty worktree should have been rejected" >&2
  exit 1
fi
grep -q "worktree is dirty" "$tmp/dirty.out"

# Landing queue: a live holder makes land-main refuse (no wait, no CI run, no
# kill); a lock left by an exited pid is reclaimed.
git -C "$tmp/candidate" checkout -- candidate.txt
git clone "$tmp/origin.git" "$tmp/lock-candidate" >/dev/null 2>&1
git -C "$tmp/lock-candidate" config user.name "GoatOS Test"
git -C "$tmp/lock-candidate" config user.email "goatos-test@example.invalid"
git -C "$tmp/lock-candidate" switch -c lock-feature >/dev/null
printf 'lock candidate\n' >"$tmp/lock-candidate/lock.txt"
git -C "$tmp/lock-candidate" add lock.txt
git -C "$tmp/lock-candidate" commit -m lock-candidate >/dev/null
mkdir "$tmp/land.lock"
printf 'pid=%s\nworktree=/elsewhere\nsha=deadbeef\nstarted=now\n' "$$" >"$tmp/land.lock/holder"
if (
  cd "$tmp/lock-candidate"
  GOATOS_LAND_TEST_MODE=1 \
    GOATOS_LAND_MAIN_LOCK_DIR="$tmp/land.lock" \
    GOATOS_LAND_TEST_CI_COMMAND="$tmp/fake-ci-should-not-run.sh" \
    bash "$script"
) >"$tmp/lock.out" 2>&1; then
  echo "land-main self-test: a held landing lock should have been refused" >&2
  exit 1
fi
grep -q "landing queue busy" "$tmp/lock.out"
grep -q "worktree=/elsewhere" "$tmp/lock.out"
! grep -q "CI command unexpectedly ran" "$tmp/lock.out"
[ -d "$tmp/land.lock" ] || { echo "land-main self-test: refused run must not remove the holder's lock" >&2; exit 1; }

sh -c 'exit 0' & dead_pid=$!
wait "$dead_pid"
printf 'pid=%s\n' "$dead_pid" >"$tmp/land.lock/holder"
(
  cd "$tmp/lock-candidate"
  GOATOS_LAND_TEST_MODE=1 \
    GOATOS_LAND_MAIN_LOCK_DIR="$tmp/land.lock" \
    GOATOS_LAND_TEST_CI_COMMAND="$tmp/fake-ci.sh" \
    bash "$script"
) >"$tmp/stale.out" 2>&1 || { cat "$tmp/stale.out" >&2; echo "land-main self-test: stale lock should be reclaimed" >&2; exit 1; }
grep -q "reclaiming stale landing lock" "$tmp/stale.out"
[ ! -e "$tmp/land.lock" ] || { echo "land-main self-test: lock must be released on exit" >&2; exit 1; }

# Lock library race cases (tools/ci/land-lock.sh), exercised directly.
lock_lib="$(dirname "$script")/land-lock.sh"
# PID reuse: holder names a LIVE pid but a different start time -> stale.
rm -rf "$tmp/reuse.lock"; mkdir "$tmp/reuse.lock"
printf 'pid=%s\npidstart=Thu Jan  1 00:00:00 1970\nworktree=/old\n' "$$" >"$tmp/reuse.lock/holder"
( source "$lock_lib"; land_lock_acquire "$tmp/reuse.lock" "worktree=/new" ) 2>"$tmp/reuse.out" \
  || { cat "$tmp/reuse.out" >&2; echo "land-main self-test: reused pid should read as stale" >&2; exit 1; }
grep -q "reclaiming stale landing lock" "$tmp/reuse.out"
grep -q "worktree=/new" "$tmp/reuse.lock/holder"
# Same live pid with its REAL start time stays held.
( source "$lock_lib"; printf 'pid=%s\npidstart=%s\n' "$$" "$(land_lock_proc_start "$$")" >"$tmp/reuse.lock/holder"
  ! land_lock_acquire "$tmp/reuse.lock" "worktree=/intruder" ) 2>/dev/null \
  || { echo "land-main self-test: live holder with matching start must stay held" >&2; exit 1; }
# Concurrent reclaim: several runs see the same dead holder; exactly one wins.
sh -c 'exit 0' & dead_pid=$!
wait "$dead_pid"
for round in 1 2 3 4 5 6 7 8 9 10; do
  rm -rf "$tmp/race.lock" "$tmp"/race.lock.stale.* "$tmp/race.wins"; mkdir "$tmp/race.lock"
  printf 'pid=%s\npidstart=x\n' "$dead_pid" >"$tmp/race.lock/holder"
  for r in 1 2 3; do
    ( source "$lock_lib"; land_lock_acquire "$tmp/race.lock" "racer=$r" 2>/dev/null && echo "$r" >>"$tmp/race.wins"; sleep 1 ) &
  done
  wait
  wins="$(wc -l <"$tmp/race.wins" 2>/dev/null | tr -d ' ')"
  [ "$wins" = "1" ] || { echo "land-main self-test: concurrent reclaim round $round had ${wins:-0} winners" >&2; exit 1; }
  grep -q "racer=$(cat "$tmp/race.wins")" "$tmp/race.lock/holder" \
    || { echo "land-main self-test: winner's holder must own the lock" >&2; exit 1; }
done

# Default lock dir is machine-wide under $HOME/.goatos/locks, shared by every
# clone/worktree; GOATOS_LAND_MAIN_LOCK_DIR still overrides it.
got="$(source "$lock_lib"; HOME="$tmp/fakehome" GOATOS_LAND_MAIN_LOCK_DIR= land_lock_default_dir)"
[ "$got" = "$tmp/fakehome/.goatos/locks/goatos-land-main.lock" ] \
  || { echo "land-main self-test: default lock dir must be machine-wide, got $got" >&2; exit 1; }
[ -d "$tmp/fakehome/.goatos/locks" ] || { echo "land-main self-test: lock parent not created" >&2; exit 1; }
got="$(source "$lock_lib"; GOATOS_LAND_MAIN_LOCK_DIR=/x/y.lock land_lock_default_dir)"
[ "$got" = "/x/y.lock" ] || { echo "land-main self-test: override ignored" >&2; exit 1; }

echo "land-main self-test: passed"
