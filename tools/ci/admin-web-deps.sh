# shellcheck shell=bash
# admin-web-deps.sh — skip `npm ci` when nothing that determines node_modules
# has changed since the last SUCCESSFUL install in this worktree (plan G).
#
# The stamp is written into node_modules itself, only after `npm ci` exits 0, and
# its key covers every input that decides what `npm ci` would install:
#   sha256(package-lock.json) + sha256(package.json) + `node -v` + `npm -v`
# A changed lockfile, package.json, node or npm version, a missing node_modules,
# a missing/unreadable stamp, or a failed previous install => a fresh `npm ci`.
# A hashing failure yields an empty key, which never matches => install (fail-safe).
#
# GOATOS_ADMIN_WEB_FORCE_NPM_CI=1 always reinstalls.
# The npm download cache stays the user-wide ~/.npm (npm's default), shared by
# every worktree.

admin_web_deps_key() { # dir
  local dir="$1" lock pkg nodev npmv
  lock="$(shasum -a 256 "$dir/package-lock.json" 2>/dev/null | awk '{print $1}')"
  pkg="$(shasum -a 256 "$dir/package.json" 2>/dev/null | awk '{print $1}')"
  nodev="$(node -v 2>/dev/null)"
  npmv="$(npm -v 2>/dev/null)"
  [ -n "$lock" ] && [ -n "$pkg" ] && [ -n "$nodev" ] && [ -n "$npmv" ] || return 1
  printf 'lock=%s pkg=%s node=%s npm=%s' "$lock" "$pkg" "$nodev" "$npmv"
}

admin_web_deps_stamp_file() { printf '%s/node_modules/.goatos-ci-install-stamp' "$1"; }

# admin_web_deps_current <dir> — 0 when the existing install is provably current.
admin_web_deps_current() {
  local dir="$1" key stamp
  case "${GOATOS_ADMIN_WEB_FORCE_NPM_CI:-0}" in 1|true|TRUE|True) return 1 ;; esac
  [ -d "$dir/node_modules" ] || return 1
  key="$(admin_web_deps_key "$dir")" || return 1
  stamp="$(cat "$(admin_web_deps_stamp_file "$dir")" 2>/dev/null)" || return 1
  [ -n "$key" ] && [ "$key" = "$stamp" ]
}

# admin_web_deps_install <dir> — npm ci, then stamp ONLY on success.
admin_web_deps_install() {
  local dir="$1" key
  rm -f "$(admin_web_deps_stamp_file "$dir")" 2>/dev/null
  npm --prefix "$dir" ci || return 1
  key="$(admin_web_deps_key "$dir")" || return 0
  printf '%s' "$key" >"$(admin_web_deps_stamp_file "$dir")" 2>/dev/null || true
  return 0
}
