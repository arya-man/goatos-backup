#!/bin/sh
set -eu

host="${PUBSUB_EMULATOR_HOST:-pubsub:8085}"
project="${PUBSUB_PROJECT_ID:-goatos-local}"
base="http://${host}/v1/projects/${project}"
source_topic="${base}/topics/goatos-local-outbox-events"
dlq_topic="${base}/topics/goatos-local-outbox-events-dlq"

put_resource() {
  label="$1"
  url="$2"
  body="${3:-{}}"
  status="$(curl -sS -o /tmp/pubsub-bootstrap-response -w '%{http_code}' \
    -X PUT -H 'Content-Type: application/json' --data "$body" "$url")"
  case "$status" in
    200|201|409)
      printf 'pubsub emulator resource ready: %s (HTTP %s)\n' "$label" "$status"
      ;;
    *)
      printf 'pubsub emulator bootstrap failed: %s (HTTP %s)\n' "$label" "$status" >&2
      cat /tmp/pubsub-bootstrap-response >&2
      exit 1
      ;;
  esac
}

put_resource "source topic" "$source_topic" '{}'
put_resource "dead-letter topic" "$dlq_topic" '{}'
put_resource "domain subscription" "${base}/subscriptions/goatos-local-domain-events" "$(cat <<JSON
{
  "topic": "projects/${project}/topics/goatos-local-outbox-events",
  "ackDeadlineSeconds": 30,
  "deadLetterPolicy": {
    "deadLetterTopic": "projects/${project}/topics/goatos-local-outbox-events-dlq",
    "maxDeliveryAttempts": 5
  }
}
JSON
)"
put_resource "dead-letter inspection subscription" "${base}/subscriptions/goatos-local-outbox-events-dlq-inspect" "$(cat <<JSON
{
  "topic": "projects/${project}/topics/goatos-local-outbox-events-dlq",
  "ackDeadlineSeconds": 30
}
JSON
)"
