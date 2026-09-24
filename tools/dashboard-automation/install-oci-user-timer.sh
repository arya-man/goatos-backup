#!/usr/bin/env bash
# Install a user-level systemd timer for the dashboard automation runner.
#
# It does not create OCI resources. It only writes user systemd units on the
# machine where it is run, and refuses unless the caller opts in after reviewing
# the generated unit paths.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SYSTEMD_USER_DIR="${XDG_CONFIG_HOME:-${HOME}/.config}/systemd/user"
UNIT_NAME="${GOATOS_DASHBOARD_AUTOMATION_UNIT_NAME:-goatos-dashboard-automation}"
SERVICE_PATH="${SYSTEMD_USER_DIR}/${UNIT_NAME}.service"
TIMER_PATH="${SYSTEMD_USER_DIR}/${UNIT_NAME}.timer"
POST_MAIN_UNIT_NAME="${GOATOS_DASHBOARD_POST_MAIN_UNIT_NAME:-goatos-dashboard-post-main}"
POST_MAIN_SERVICE_PATH="${SYSTEMD_USER_DIR}/${POST_MAIN_UNIT_NAME}.service"
POST_MAIN_TIMER_PATH="${SYSTEMD_USER_DIR}/${POST_MAIN_UNIT_NAME}.timer"
ON_CALENDAR="${GOATOS_DASHBOARD_AUTOMATION_ON_CALENDAR:-*-*-* 04:00:00 Asia/Kolkata
*-*-* 20:00:00 Asia/Kolkata}"
POST_MAIN_ON_CALENDAR="${GOATOS_DASHBOARD_POST_MAIN_ON_CALENDAR:-*:0/10}"
MODE="${GOATOS_DASHBOARD_AUTOMATION_MODE:-production-smoke}"
ENV_FILE="${GOATOS_DASHBOARD_AUTOMATION_ENV_FILE:-${HOME}/.config/goatos/dashboard-automation.env}"
# Branch the VM tooling checkout tracks (runbook "Tooling ref"); "main" = legacy main-only.
TOOLING_REF="${GOATOS_DASHBOARD_TOOLING_REF:-ops/dashboard-automation}"

die() {
  echo "dashboard-automation-timer: $*" >&2
  exit 2
}

case "$MODE" in
  production-smoke|post-main-certification) ;;
  *) die "unsupported GOATOS_DASHBOARD_AUTOMATION_MODE=${MODE}" ;;
esac

[[ -f "$ENV_FILE" ]] || die "env file not found: ${ENV_FILE}"
command -v systemctl >/dev/null 2>&1 || die "systemctl is required on the OCI runner"

mkdir -p "$SYSTEMD_USER_DIR"

cat >"${SERVICE_PATH}.tmp" <<SERVICE
[Unit]
Description=Goat OS dashboard automation (${MODE})

[Service]
Type=oneshot
WorkingDirectory=${REPO_ROOT}
Environment=GOATOS_DASHBOARD_AUTOMATION_MODE=${MODE}
Environment=GOATOS_DASHBOARD_AUTOMATION_ENV_FILE=${ENV_FILE}
Environment=GOATOS_DASHBOARD_TOOLING_REF=${TOOLING_REF}
ExecStart=${REPO_ROOT}/tools/dashboard-automation/run-oci.sh
Nice=10
IOSchedulingClass=best-effort
IOSchedulingPriority=7
SERVICE

cat >"${TIMER_PATH}.tmp" <<TIMER
[Unit]
Description=Run Goat OS dashboard automation (${MODE})

[Timer]
OnCalendar=${ON_CALENDAR}
Persistent=true
RandomizedDelaySec=10m

[Install]
WantedBy=timers.target
TIMER

cat >"${POST_MAIN_SERVICE_PATH}.tmp" <<SERVICE
[Unit]
Description=Goat OS dashboard post-main certification (tooling from ${TOOLING_REF})

[Service]
Type=oneshot
WorkingDirectory=${REPO_ROOT}
Environment=GOATOS_DASHBOARD_AUTOMATION_ENV_FILE=${ENV_FILE}
Environment=GOATOS_DASHBOARD_TOOLING_REF=${TOOLING_REF}
ExecStart=${REPO_ROOT}/tools/dashboard-automation/run-post-main-if-new.sh
Nice=10
IOSchedulingClass=best-effort
IOSchedulingPriority=7
SERVICE

cat >"${POST_MAIN_TIMER_PATH}.tmp" <<TIMER
[Unit]
Description=Poll origin/main and run Goat OS dashboard post-main certification

[Timer]
OnCalendar=${POST_MAIN_ON_CALENDAR}
Persistent=true
RandomizedDelaySec=2m

[Install]
WantedBy=timers.target
TIMER

echo "dashboard-automation-timer: generated:"
echo "  ${SERVICE_PATH}.tmp"
echo "  ${TIMER_PATH}.tmp"
echo "  ${POST_MAIN_SERVICE_PATH}.tmp"
echo "  ${POST_MAIN_TIMER_PATH}.tmp"

if [[ "${GOATOS_DASHBOARD_AUTOMATION_INSTALL:-0}" != "1" ]]; then
  echo "dashboard-automation-timer: dry run only. Set GOATOS_DASHBOARD_AUTOMATION_INSTALL=1 to install."
  exit 0
fi

mv "${SERVICE_PATH}.tmp" "$SERVICE_PATH"
mv "${TIMER_PATH}.tmp" "$TIMER_PATH"
mv "${POST_MAIN_SERVICE_PATH}.tmp" "$POST_MAIN_SERVICE_PATH"
mv "${POST_MAIN_TIMER_PATH}.tmp" "$POST_MAIN_TIMER_PATH"
systemctl --user daemon-reload
systemctl --user enable --now "${UNIT_NAME}.timer"
systemctl --user enable --now "${POST_MAIN_UNIT_NAME}.timer"
systemctl --user list-timers "${UNIT_NAME}.timer" "${POST_MAIN_UNIT_NAME}.timer" --no-pager
