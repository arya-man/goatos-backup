#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
PROJECT_NUMBER="${PROJECT_NUMBER:-514832198871}"
REGION="${REGION:-asia-south1}"
ARTIFACT_REPOSITORY="${ARTIFACT_REPOSITORY:-goatos}"
DELIVERY_PIPELINE="${DELIVERY_PIPELINE:-goatos-stg}"
TARGET_ID="${TARGET_ID:-goatos-stg}"
WAIT_FOR_ROLLOUT="${GOATOS_STG_RELEASE_WAIT:-1}"
ROLLOUT_TIMEOUT_SECONDS="${GOATOS_STG_ROLLOUT_TIMEOUT_SECONDS:-1800}"
ROLLOUT_POLL_SECONDS="${GOATOS_STG_ROLLOUT_POLL_SECONDS:-20}"
SLACK_WEBHOOK_SECRET="${SLACK_WEBHOOK_SECRET:-goatos-stg-deploy-slack-webhook-url}"
TRIGGERED_BY="${TRIGGERED_BY:-unknown Slack user}"
CONSOLE_AUTHUSER="${CONSOLE_AUTHUSER:-ravi@mesha.sg}"

die() {
  echo "ERROR: $*" >&2
  exit 1
}

slack_webhook_url() {
  gcloud secrets versions access latest \
    --project="$PROJECT_ID" \
    --secret="$SLACK_WEBHOOK_SECRET" 2>/dev/null || true
}

notify_slack_success() {
  local webhook build_id
  webhook="$(slack_webhook_url)"
  [[ -n "$webhook" ]] || return 0
  build_id="${BUILD_ID:-local}"

  python3 - "$commit_sha" "$release_id" "$build_id" "$TRIGGERED_BY" "$PROJECT_NUMBER" "$REGION" "$CONSOLE_AUTHUSER" <<'PY' | curl -fsS -X POST -H 'Content-Type: application/json' --data-binary @- "$webhook" >/dev/null || true
import json
import sys
from urllib.parse import urlencode

sha, release, build_id, triggered_by, project_number, region, authuser = sys.argv[1:]
query = urlencode({"project": project_number, "authuser": authuser})
build_url = f"https://console.cloud.google.com/cloud-build/builds;region={region}/{build_id}?{query}"
deploy_url = f"https://console.cloud.google.com/deploy/delivery-pipelines/{region}/goatos-stg/releases/{release}?{query}"
payload = {
    "attachments": [{
        "color": "#2EB67D",
        "title": "GoatOS deploy succeeded",
        "text": "Backend/web/kernel rollout succeeded and live images were verified.",
        "fields": [
            {"title": "Commit", "value": sha, "short": True},
            {"title": "Release", "value": release, "short": True},
            {
                "title": "Verified services",
                "value": "goatos-api-stg, goatos-admin-web-stg, goatos-kernel-worker-stg, goatos-mcp-stg, migrate/outbox/analytics jobs",
                "short": False,
            },
            {"title": "Triggered by", "value": triggered_by, "short": False},
        ],
        "actions": [
            {"type": "button", "text": "Cloud Build logs", "url": build_url},
            {"type": "button", "text": "Cloud Deploy", "url": deploy_url},
            {"type": "button", "text": "Dashboard", "url": "https://dashboard.mesha.sg/login"},
        ],
    }],
}
print(json.dumps(payload))
PY
}

post_deploy_panel() {
  local webhook
  webhook="$(slack_webhook_url)"
  [[ -n "$webhook" ]] || return 0
  [[ -f tools/deploy/slack-stg-deploy-bot/deploy-card.json ]] || return 0

  curl -fsS -X POST \
    -H 'Content-Type: application/json' \
    --data-binary @tools/deploy/slack-stg-deploy-bot/deploy-card.json \
    "$webhook" >/dev/null || true
}

[[ "$PROJECT_ID" == "goatos-stg" ]] || die "PROJECT_ID must be goatos-stg, got $PROJECT_ID"
[[ "$PROJECT_NUMBER" == "514832198871" ]] || die "PROJECT_NUMBER must be 514832198871, got $PROJECT_NUMBER"
[[ "$REGION" == "asia-south1" ]] || die "REGION must be asia-south1, got $REGION"

if repo_root="$(git rev-parse --show-toplevel 2>/dev/null)"; then
  has_git_checkout=1
else
  has_git_checkout=0
  repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fi
cd "$repo_root"

if [[ "$has_git_checkout" == "1" && "${GOATOS_ALLOW_NON_MAIN_STG_RELEASE:-}" != "1" ]]; then
  origin_url="$(git remote get-url origin 2>/dev/null || true)"
  [[ "$origin_url" == "git@github.com:vgoats/goatos.git" || "$origin_url" == "ssh://git@github.com/vgoats/goatos.git" || "$origin_url" == "https://github.com/vgoats/goatos.git" || "$origin_url" == "https://github.com/vgoats/goatos" ]] \
    || die "staging releases must run from vgoats/goatos; got origin=$origin_url"
  head_sha="$(git rev-parse --verify HEAD)"
  if [[ -n "${BUILD_ID:-}" ]]; then
    echo "Cloud Build source is trigger-resolved; using checked-out HEAD ${head_sha} without a private origin fetch."
  else
    git fetch origin main --quiet
    main_sha="$(git rev-parse --verify origin/main)"
    [[ "$head_sha" == "$main_sha" ]] \
      || die "refusing staging release from non-main commit: HEAD=$head_sha origin/main=$main_sha. Land on main first, or set GOATOS_ALLOW_NON_MAIN_STG_RELEASE=1 for an explicit break-glass release."
  fi
fi

if [[ "$has_git_checkout" == "1" && "${GOATOS_ALLOW_DIRTY_RELEASE:-}" != "1" ]]; then
  git diff --quiet || die "working tree has unstaged changes; commit or set GOATOS_ALLOW_DIRTY_RELEASE=1"
  git diff --cached --quiet || die "working tree has staged changes; commit or set GOATOS_ALLOW_DIRTY_RELEASE=1"
fi

# Accounts permitted to release Goat OS staging. Deliberately an EXPLICIT NAMED LIST, not a
# `*@mesha.sg` domain test and not an environment override: staging deploys are authorised
# per person, and the list of people is reviewable in this file's git history. Anyone added
# here can build and push images and roll out Cloud Run staging services and jobs, so adding
# an entry IS the act of granting deploy authority -- treat it as a maintainer decision.
#
# 2026-08-05: manohark@mesha.sg added alongside ravi@mesha.sg at the maintainer's request.
STG_DEPLOY_ACCOUNTS=(
  "ravi@mesha.sg"
  "manohark@mesha.sg"
  "goatos-github-deploy-stg@goatos-stg.iam.gserviceaccount.com"
)

active_account="$(gcloud config get-value account 2>/dev/null)"
active_project="$(gcloud config get-value project 2>/dev/null)"
account_allowed=0
for allowed in "${STG_DEPLOY_ACCOUNTS[@]}"; do
  [[ "$active_account" == "$allowed" ]] && account_allowed=1 && break
done
[[ "$account_allowed" == "1" ]] || die "active gcloud account must be one of: ${STG_DEPLOY_ACCOUNTS[*]}; got $active_account"
[[ "$active_project" == "$PROJECT_ID" ]] || die "active gcloud project must be $PROJECT_ID, got $active_project"

if [[ -n "${COMMIT_SHA:-}" ]]; then
  commit_sha="$(printf '%s' "$COMMIT_SHA" | cut -c1-12)"
elif [[ "$has_git_checkout" == "1" ]]; then
  commit_sha="$(git rev-parse --short=12 HEAD)"
else
  die "COMMIT_SHA is required when running from a source archive without .git"
fi
registry="${REGION}-docker.pkg.dev/${PROJECT_ID}/${ARTIFACT_REPOSITORY}"
backend_image="${registry}/backend:${commit_sha}"
migration_image="${registry}/migrate:${commit_sha}"
admin_web_image="${registry}/admin-web:${commit_sha}"
release_id="${RELEASE_ID:-r-${commit_sha}-$(date -u +%H%M%S)}"

echo "Creating Goat OS staging Cloud Deploy release"
echo "account=$active_account"
echo "project=$active_project"
echo "commit=$commit_sha"
echo "release=$release_id"

if [[ "${GOATOS_SKIP_IMAGE_BUILD:-}" == "1" ]]; then
  echo "Image build skipped: expecting prebuilt Artifact Registry images for $commit_sha"
else
  gcloud auth configure-docker "${REGION}-docker.pkg.dev" --quiet

  docker build --platform linux/amd64 --build-arg GIT_SHA="$commit_sha" -f backend/Dockerfile -t "$backend_image" .
  docker push "$backend_image"

  docker build --platform linux/amd64 --build-arg GIT_SHA="$commit_sha" -f backend/Dockerfile.migrate -t "$migration_image" .
  docker push "$migration_image"

  docker build --platform linux/amd64 -f apps/admin-web/Dockerfile -t "$admin_web_image" .
  docker push "$admin_web_image"
fi

gcloud deploy releases create "$release_id" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --delivery-pipeline="$DELIVERY_PIPELINE" \
  --source=. \
  --skaffold-file=deploy/clouddeploy/stg/skaffold.yaml \
  --to-target="$TARGET_ID" \
  --labels="commit_sha=${commit_sha},deployed_by=cloud-deploy" \
  --deploy-parameters="customTarget/commitSha=${commit_sha},customTarget/backendImage=${backend_image},customTarget/migrationImage=${migration_image},customTarget/adminWebImage=${admin_web_image}"

echo "Release submitted: $release_id"

rollout_id="${release_id}-to-${TARGET_ID}-0001"

wait_for_rollout() {
  local deadline state description
  deadline=$(( $(date +%s) + ROLLOUT_TIMEOUT_SECONDS ))
  echo "Waiting for rollout: $rollout_id"
  while true; do
    state="$(
      gcloud deploy rollouts describe "$rollout_id" \
        --project="$PROJECT_ID" \
        --region="$REGION" \
        --delivery-pipeline="$DELIVERY_PIPELINE" \
        --release="$release_id" \
        --format='value(state)' 2>/dev/null || true
    )"
    description="$(
      gcloud deploy rollouts describe "$rollout_id" \
        --project="$PROJECT_ID" \
        --region="$REGION" \
        --delivery-pipeline="$DELIVERY_PIPELINE" \
        --release="$release_id" \
        --format='value(stateDescription)' 2>/dev/null || true
    )"
    case "$state" in
      SUCCEEDED)
        echo "Rollout succeeded: $rollout_id"
        return 0
        ;;
      FAILED|CANCELLED|HALTED)
        die "rollout $rollout_id ended in $state ${description:+- $description}"
        ;;
      "")
        echo "rollout state: not available yet"
        ;;
      *)
        echo "rollout state: $state${description:+ - $description}"
        ;;
    esac
    if (( $(date +%s) >= deadline )); then
      die "timed out waiting for rollout $rollout_id after ${ROLLOUT_TIMEOUT_SECONDS}s"
    fi
    sleep "$ROLLOUT_POLL_SECONDS"
  done
}

expect_service_image() {
  local service="$1"
  local expected="$2"
  local line actual created ready
  line="$(
    gcloud run services describe "$service" \
      --project="$PROJECT_ID" \
      --region="$REGION" \
      --format='value(spec.template.spec.containers[0].image,status.latestCreatedRevisionName,status.latestReadyRevisionName)'
  )"
  IFS=$'\t' read -r actual created ready <<<"$line"
  [[ "$actual" == "$expected" ]] || die "$service image stale: got $actual want $expected"
  [[ "$created" == "$ready" ]] || die "$service has unready latest revision: created=$created ready=$ready"
  echo "verified service image: $service -> $actual ($ready)"
}

expect_job_image() {
  local job="$1"
  local expected="$2"
  local actual
  actual="$(
    gcloud run jobs describe "$job" \
      --project="$PROJECT_ID" \
      --region="$REGION" \
      --format='value(spec.template.spec.template.spec.containers[0].image)'
  )"
  [[ "$actual" == "$expected" ]] || die "$job image stale: got $actual want $expected"
  echo "verified job image: $job -> $actual"
}

verify_stg_images() {
  echo "Verifying staging images for $commit_sha"
  expect_service_image goatos-api-stg "$backend_image"
  expect_service_image goatos-admin-web-stg "$admin_web_image"
  expect_service_image goatos-kernel-worker-stg "$backend_image"
  expect_service_image goatos-mcp-stg "$backend_image"
  expect_job_image goatos-stg-migrate "$migration_image"
  expect_job_image goatos-stg-outbox-dlq "$backend_image"
  expect_job_image goatos-stg-analytics-rollup "$backend_image"
  echo "STG image parity verified for $commit_sha"
}

if [[ "$WAIT_FOR_ROLLOUT" == "1" ]]; then
  wait_for_rollout
  verify_stg_images
  notify_slack_success
  post_deploy_panel
else
  echo "Rollout wait skipped by GOATOS_STG_RELEASE_WAIT=0; image parity not verified."
  die "STG deploy success requires verified rollout/image parity; rerun with GOATOS_STG_RELEASE_WAIT=1"
fi
