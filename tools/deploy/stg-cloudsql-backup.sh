#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
PROJECT_NUMBER="${PROJECT_NUMBER:-514832198871}"
INSTANCE_ID="${INSTANCE_ID:-goatos-stg-core-db}"
DESCRIPTION="${DESCRIPTION:-pre-deploy-$(git rev-parse --short=12 HEAD 2>/dev/null || date -u +%Y%m%d%H%M%S)}"
COMMIT_SHA="$(git rev-parse --short=12 HEAD 2>/dev/null || true)"
BACKUP_MARKER="${GOATOS_STG_BACKUP_MARKER:-.openai/stg-cloudsql-backup-${COMMIT_SHA}.ok}"

die() {
  echo "ERROR: $*" >&2
  exit 1
}

[[ "$PROJECT_ID" == "goatos-stg" ]] || die "PROJECT_ID must be goatos-stg, got $PROJECT_ID"
[[ "$PROJECT_NUMBER" == "514832198871" ]] || die "PROJECT_NUMBER must be 514832198871, got $PROJECT_NUMBER"
[[ "$INSTANCE_ID" == "goatos-stg-core-db" ]] || die "INSTANCE_ID must be goatos-stg-core-db, got $INSTANCE_ID"

active_account="$(gcloud config get-value account 2>/dev/null)"
active_project="$(gcloud config get-value project 2>/dev/null)"
[[ "$active_account" == "ravi@mesha.sg" || "$active_account" == "manohark@mesha.sg" ]] \
  || die "active gcloud account must be ravi@mesha.sg or manohark@mesha.sg; got $active_account"
[[ "$active_project" == "$PROJECT_ID" ]] || die "active gcloud project must be $PROJECT_ID, got $active_project"

actual_project_number="$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')"
[[ "$actual_project_number" == "$PROJECT_NUMBER" ]] \
  || die "project number mismatch for $PROJECT_ID: expected $PROJECT_NUMBER got $actual_project_number"
project_parent="$(gcloud projects describe "$PROJECT_ID" --format='value(parent.type,parent.id)')"
case "$project_parent" in
  "folder 188649904255"|"organization 848015369910")
    ;;
  *)
    die "project parent mismatch for $PROJECT_ID: expected folder 188649904255 or organization 848015369910 got $project_parent"
    ;;
esac

echo "Creating Goat OS STG Cloud SQL backup"
echo "account=$active_account"
echo "project=$PROJECT_ID"
echo "instance=$INSTANCE_ID"
echo "description=$DESCRIPTION"

gcloud sql backups create \
  --project="$PROJECT_ID" \
  --instance="$INSTANCE_ID" \
  --description="$DESCRIPTION"

mkdir -p "$(dirname "$BACKUP_MARKER")"
{
  echo "project=$PROJECT_ID"
  echo "project_number=$PROJECT_NUMBER"
  echo "instance=$INSTANCE_ID"
  echo "commit=$COMMIT_SHA"
  echo "description=$DESCRIPTION"
  date -u '+created_at=%Y-%m-%dT%H:%M:%SZ'
} > "$BACKUP_MARKER"

echo
echo "Latest backups for $INSTANCE_ID:"
gcloud sql backups list \
  --project="$PROJECT_ID" \
  --instance="$INSTANCE_ID" \
  --limit=5 \
  --sort-by='~endTime' \
  --format='table(id,status,type,windowStartTime,endTime,description)'
