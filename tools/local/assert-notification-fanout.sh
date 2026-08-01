#!/usr/bin/env bash
# Show, and assert on, exactly who the backend decided to notify.
#
# The hard part of testing alerts is that the interesting outcome happens on SOMEONE ELSE'S
# phone: an operator submits, and the director and CEO are supposed to be told. You cannot see
# that by switching roles on one device -- by the time you log in as the director, the push has
# already been delivered or dropped, and an empty Alerts screen looks identical whether the
# notification was never generated, went to the wrong person, or simply arrived while you were
# someone else.
#
# Notifications are queued in `notification_requests` BEFORE any FCM call, so that table is the
# backend's decision, recorded and durable: who, which channel, what words, and which screen the
# tap opens. Asserting there separates two failures that look the same from a phone:
#   * the backend never decided to tell anyone   (a routing/audience bug)
#   * the backend decided, but delivery failed   (a token/credential/transport bug)
# Layer 2 -- real pushes on real devices -- proves delivery. This proves intent.
#
# Usage:
#   tools/local/assert-notification-fanout.sh since '2026-08-01 20:00'   # everything since
#   tools/local/assert-notification-fanout.sh watch                      # tail as they appear
#   tools/local/assert-notification-fanout.sh expect <spec-file>         # assert and exit 1
#
# The expect spec is one rule per line, blank lines and #comments ignored:
#   must    <person>  <type-substring>     this person MUST have been notified
#   mustnot <person>  <type-substring>     this person must NOT have been (leak/mixing check)
#   route   <person>  <type-substring>  <route-substring>
set -euo pipefail

DB="${DATABASE_URL:-postgres://postgres:goatos@127.0.0.1:15544/goatos?sslmode=disable}"
mode="${1:-since}"

die() { echo "notification-fanout: $*" >&2; exit 1; }
command -v psql >/dev/null || die "psql is required"

# recipient_ref is an opaque id; join it back to a human so the output is readable by someone
# who is testing a farm workflow, not reading a database.
readonly RESOLVE="
LEFT JOIN workforce_members wm
  ON wm.user_id::text = n.recipient_ref OR wm.workforce_member_id::text = n.recipient_ref
"

dump() {
  local where="$1"
  psql "$DB" -qAt -F $'\t' -c "
SELECT to_char(n.requested_at,'HH24:MI:SS'),
       COALESCE(wm.display_name, left(n.recipient_ref,12)),
       n.notification_type,
       n.channel,
       n.status,
       COALESCE(n.title,''),
       COALESCE(n.context->>'target', n.context->>'href', n.context->>'screen', '')
FROM notification_requests n
$RESOLVE
WHERE $where
ORDER BY n.requested_at, 2;"
}

case "$mode" in
  since)
    since="${2:-}"
    [ -n "$since" ] || die "usage: $0 since '<timestamp>'"
    printf '%-9s %-18s %-34s %-8s %-10s %s\n' TIME WHO TYPE CHANNEL STATUS TAP-ROUTE
    dump "n.requested_at >= '$since'" | while IFS=$'\t' read -r t who typ ch st title route; do
      printf '%-9s %-18s %-34s %-8s %-10s %s\n' "$t" "$who" "$typ" "$ch" "$st" "${route:-(none)}"
    done
    ;;

  watch)
    echo "watching notification_requests -- run the workflow on the phone now (Ctrl-C to stop)"
    last="$(psql "$DB" -qAt -c "SELECT COALESCE(max(requested_at), now())::text FROM notification_requests;")"
    printf '%-9s %-18s %-34s %-8s %-10s %s\n' TIME WHO TYPE CHANNEL STATUS TAP-ROUTE
    while true; do
      rows="$(dump "n.requested_at > '$last'")"
      if [ -n "$rows" ]; then
        echo "$rows" | while IFS=$'\t' read -r t who typ ch st title route; do
          printf '%-9s %-18s %-34s %-8s %-10s %s\n' "$t" "$who" "$typ" "$ch" "$st" "${route:-(none)}"
        done
        last="$(psql "$DB" -qAt -c "SELECT max(requested_at)::text FROM notification_requests;")"
      fi
      sleep 2
    done
    ;;

  expect)
    spec="${2:-}"
    [ -f "${spec:-}" ] || die "usage: $0 expect <spec-file>"
    since="${3:-}"
    window="true"
    [ -n "$since" ] && window="n.requested_at >= '$since'"

    fails=0
    checks=0
    while read -r rule who typ route || [ -n "${rule:-}" ]; do
      case "${rule:-}" in ''|'#'*) continue ;; esac
      checks=$((checks + 1))
      hits="$(psql "$DB" -qAt -c "
SELECT count(*) FROM notification_requests n $RESOLVE
WHERE $window
  AND COALESCE(wm.display_name,'') ILIKE '%${who}%'
  AND n.notification_type ILIKE '%${typ}%';")"

      case "$rule" in
        must)
          if [ "$hits" -eq 0 ]; then
            echo "FAIL  ${who} was NOT notified of '${typ}' -- nobody told them"
            fails=$((fails + 1))
          else
            echo "ok    ${who} notified of '${typ}' (${hits})"
          fi
          ;;
        mustnot)
          if [ "$hits" -gt 0 ]; then
            echo "FAIL  ${who} WAS notified of '${typ}' (${hits}) -- alerts are leaking across role or feature"
            fails=$((fails + 1))
          else
            echo "ok    ${who} correctly not notified of '${typ}'"
          fi
          ;;
        route)
          bad="$(psql "$DB" -qAt -c "
SELECT count(*) FROM notification_requests n $RESOLVE
WHERE $window
  AND COALESCE(wm.display_name,'') ILIKE '%${who}%'
  AND n.notification_type ILIKE '%${typ}%'
  AND COALESCE(n.context->>'target', n.context->>'href', n.context->>'screen','') NOT ILIKE '%${route}%';")"
          if [ "$hits" -eq 0 ]; then
            echo "FAIL  ${who} has no '${typ}' notification, so its tap route cannot be checked"
            fails=$((fails + 1))
          elif [ "$bad" -gt 0 ]; then
            echo "FAIL  ${who}'s '${typ}' tap route is not '${route}' (${bad} wrong) -- lands on the wrong screen"
            fails=$((fails + 1))
          else
            echo "ok    ${who}'s '${typ}' opens '${route}'"
          fi
          ;;
        *) die "unknown rule '$rule' (want must|mustnot|route)" ;;
      esac
    done < "$spec"

    echo
    if [ "$fails" -gt 0 ]; then
      echo "$fails of $checks notification expectations FAILED"
      exit 1
    fi
    echo "all $checks notification expectations hold"
    ;;

  *) die "unknown mode '$mode' (want since|watch|expect)" ;;
esac
