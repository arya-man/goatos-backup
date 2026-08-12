#!/usr/bin/env bash
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="$repo/tools/ci/land-main.sh"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/goatos-land-main-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

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

echo "land-main self-test: passed"
