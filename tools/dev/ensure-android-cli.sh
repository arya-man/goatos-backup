#!/usr/bin/env bash
# Install Google's Android CLI for agent/device workflows when it is missing,
# and make sure the official Android agent skills are available.
set -euo pipefail

log() { printf '[android-cli] %s\n' "$*" >&2; }
die() { log "ERROR: $*"; exit 1; }

have_android=0
installed_now=0
if command -v android >/dev/null 2>&1; then
  have_android=1
elif [ -x "$HOME/.local/bin/android" ]; then
  export PATH="$HOME/.local/bin:$PATH"
  have_android=1
fi

ensure_skills() {
  local codex_skill="$HOME/.codex/skills/android-cli/SKILL.md"
  local claude_skill="$HOME/.claude/skills/android-cli/SKILL.md"
  local full_skill="$HOME/.codex/skills/testing-setup/SKILL.md"

  if [ "$installed_now" = "1" ] || [ ! -f "$codex_skill" ] || [ ! -f "$claude_skill" ]; then
    log "initializing Android CLI base agent skill"
    android init
  fi

  if [ "$installed_now" = "1" ] || [ ! -f "$full_skill" ]; then
    log "installing/updating official Android skills for detected agents"
    android skills add --all
  fi
}

if [ "$have_android" = "1" ]; then
  ensure_skills
  exit 0
fi

case "$(uname -s):$(uname -m)" in
  Darwin:arm64) install_url="https://dl.google.com/android/cli/latest/darwin_arm64/install.sh" ;;
  Darwin:x86_64) install_url="https://dl.google.com/android/cli/latest/darwin_x86_64/install.sh" ;;
  Linux:x86_64) install_url="https://dl.google.com/android/cli/latest/linux_x86_64/install.sh" ;;
  *) die "unsupported platform $(uname -s)/$(uname -m); install Android CLI manually from https://developer.android.com/tools/agents/android-cli/download" ;;
esac

command -v curl >/dev/null 2>&1 || die "curl is required to install Android CLI"

log "Android CLI not found; installing user-local CLI"
curl -fsSL "$install_url" | bash
export PATH="$HOME/.local/bin:$PATH"
installed_now=1

command -v android >/dev/null 2>&1 || die "installation completed but android is still not on PATH"

log "checking for Android CLI updates"
android update
ensure_skills
