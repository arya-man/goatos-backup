#!/usr/bin/env bash
# gradle-machine-setup.sh — install the machine-level Goat OS Gradle settings
# into the SHARED Gradle home (${GRADLE_USER_HOME:-$HOME/.gradle}), so they
# apply to every Gradle run on this Mac, including sessions that type
# ./gradlew directly and never use a repo script.
#
# Installs, idempotently:
# 1. init.d/goatos-machine-lock.init.gradle (from tools/ci/gradle-init/): one
#    goatos Android build at a time, OS file lock, released on exit/death.
# 2. A managed block in gradle.properties:
#      org.gradle.daemon.idletimeout=600000   (idle daemons exit after 10 min)
#      org.gradle.java.home=<verified JDK 21>  (only if a JDK 21 is found)
#    Lines outside the block are left alone. The block is written last, so its
#    values win over an older value of the same key above it.
#
# Run by `make ai-setup` and, via gradle-home.sh, by every Gradle entrypoint.
# Never fails the caller: problems are printed and skipped.
# GOATOS_GRADLE_MACHINE_SETUP=0 skips it.

_GOATOS_GMS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tools/ci/java21.sh
. "$_GOATOS_GMS_DIR/java21.sh"

GOATOS_GRADLE_BLOCK_BEGIN="# >>> goatos managed (tools/ci/gradle-machine-setup.sh) >>>"
GOATOS_GRADLE_BLOCK_END="# <<< goatos managed <<<"

_gms_java_home() {
  # The JDK 21 home to pin, or nothing. Homebrew's full JDK path first.
  local brew="${GOATOS_GRADLE_BREW_JDK21:-/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home}"
  if [ "$(java21_major "$brew")" = "21" ]; then printf '%s\n' "$brew"; return 0; fi
  java21_resolve "${JAVA_HOME:-}" 2>/dev/null || true
}

gradle_machine_install() {
  case "${GOATOS_GRADLE_MACHINE_SETUP:-1}" in 0|false|no) return 0 ;; esac
  local home="${GRADLE_USER_HOME:-$HOME/.gradle}"
  local src="$_GOATOS_GMS_DIR/gradle-init/goatos-machine-lock.init.gradle"
  local dst="$home/init.d/goatos-machine-lock.init.gradle"
  local props="$home/gradle.properties" jh block tmp

  mkdir -p "$home/init.d" 2>/dev/null || { echo "gradle-machine-setup: cannot create $home/init.d; skipped" >&2; return 0; }
  if [ -f "$src" ] && ! cmp -s "$src" "$dst" 2>/dev/null; then
    tmp="$dst.tmp.$$"
    if cp "$src" "$tmp" 2>/dev/null && mv -f "$tmp" "$dst" 2>/dev/null; then
      echo "gradle-machine-setup: installed $dst" >&2
    else
      rm -f "$tmp" 2>/dev/null
      echo "gradle-machine-setup: could not install $dst; skipped" >&2
    fi
  fi

  jh="$(_gms_java_home)"
  block="$GOATOS_GRADLE_BLOCK_BEGIN
org.gradle.daemon.idletimeout=600000"
  [ -n "$jh" ] && block="$block
org.gradle.java.home=$jh"
  block="$block
$GOATOS_GRADLE_BLOCK_END"

  tmp="$props.tmp.$$"
  {
    if [ -f "$props" ]; then
      awk -v b="$GOATOS_GRADLE_BLOCK_BEGIN" -v e="$GOATOS_GRADLE_BLOCK_END" '
        $0 == b { skip = 1; next }
        $0 == e { skip = 0; next }
        !skip { print }
      ' "$props"
    fi
    printf '%s\n' "$block"
  } >"$tmp" 2>/dev/null || { rm -f "$tmp"; echo "gradle-machine-setup: cannot write $props; skipped" >&2; return 0; }
  if cmp -s "$tmp" "$props" 2>/dev/null; then
    rm -f "$tmp"
  else
    mv -f "$tmp" "$props" 2>/dev/null || rm -f "$tmp"
  fi
  return 0
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  gradle_machine_install
fi
