#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-goatos-stg}"
REGION="${REGION:-asia-south1}"
URL_MAP_NAME="${URL_MAP_NAME:-goatos-stg-dashboard-map}"
API_PATH_MATCHER="${API_PATH_MATCHER:-api-host}"
EVENTS_SERVICE="${EVENTS_SERVICE:-goatos-analytics-events-stg}"
EVENTS_NEG="${EVENTS_NEG:-goatos-analytics-events-stg-neg}"
EVENTS_BACKEND="${EVENTS_BACKEND:-goatos-analytics-events-stg-backend}"
EVENTS_PATH="${EVENTS_PATH:-/app/analytics/events}"

run() {
  echo "+ $*"
  "$@"
}

ensure_events_neg() {
  if gcloud compute network-endpoint-groups describe "$EVENTS_NEG" \
    --project="$PROJECT_ID" \
    --region="$REGION" >/dev/null 2>&1; then
    return 0
  fi

  run gcloud compute network-endpoint-groups create "$EVENTS_NEG" \
    --project="$PROJECT_ID" \
    --region="$REGION" \
    --network-endpoint-type=serverless \
    --cloud-run-service="$EVENTS_SERVICE"
}

ensure_events_backend() {
  if ! gcloud compute backend-services describe "$EVENTS_BACKEND" \
    --project="$PROJECT_ID" \
    --global >/dev/null 2>&1; then
    run gcloud compute backend-services create "$EVENTS_BACKEND" \
      --project="$PROJECT_ID" \
      --global \
      --load-balancing-scheme=EXTERNAL_MANAGED \
      --protocol=HTTP \
      --timeout=30
  fi

  local neg_self_link backend_json
  neg_self_link="$(
    gcloud compute network-endpoint-groups describe "$EVENTS_NEG" \
      --project="$PROJECT_ID" \
      --region="$REGION" \
      --format='value(selfLink)'
  )"
  backend_json="$(
    gcloud compute backend-services describe "$EVENTS_BACKEND" \
      --project="$PROJECT_ID" \
      --global \
      --format=json
  )"
  if BACKEND_JSON="$backend_json" NEG_SELF_LINK="$neg_self_link" python3 - <<'PY'
import json
import os
import sys

doc = json.loads(os.environ["BACKEND_JSON"])
neg = os.environ["NEG_SELF_LINK"]
for backend in doc.get("backends", []) or []:
    if backend.get("group") == neg:
        sys.exit(0)
sys.exit(1)
PY
  then
    return 0
  fi

  run gcloud compute backend-services add-backend "$EVENTS_BACKEND" \
    --project="$PROJECT_ID" \
    --global \
    --network-endpoint-group="$EVENTS_NEG" \
    --network-endpoint-group-region="$REGION"
}

ensure_events_path_rule() {
  local url_map_json backend_self_link import_file
  url_map_json="$(
    gcloud compute url-maps describe "$URL_MAP_NAME" \
      --project="$PROJECT_ID" \
      --global \
      --format=json
  )"
  backend_self_link="$(
    gcloud compute backend-services describe "$EVENTS_BACKEND" \
      --project="$PROJECT_ID" \
      --global \
      --format='value(selfLink)'
  )"
  import_file="$(mktemp)"

  set +e
  URL_MAP_JSON="$url_map_json" \
    EVENTS_BACKEND_SELF_LINK="$backend_self_link" \
    API_PATH_MATCHER="$API_PATH_MATCHER" \
    EVENTS_PATH="$EVENTS_PATH" \
    python3 - "$import_file" <<'PY'
import json
import os
import sys

out_path = sys.argv[1]
doc = json.loads(os.environ["URL_MAP_JSON"])
matcher_name = os.environ["API_PATH_MATCHER"]
events_path = os.environ["EVENTS_PATH"]
events_backend = os.environ["EVENTS_BACKEND_SELF_LINK"]

minimal = {
    "name": doc["name"],
    "defaultService": doc.get("defaultService"),
    "hostRules": doc.get("hostRules", []),
    "pathMatchers": [],
}
changed = False
found = False
for matcher in doc.get("pathMatchers", []):
    next_matcher = {
        "name": matcher["name"],
        "defaultService": matcher.get("defaultService"),
    }
    path_rules = list(matcher.get("pathRules", []) or [])
    if matcher.get("name") == matcher_name:
        found = True
        for rule in path_rules:
            paths = rule.get("paths", []) or []
            if events_path in paths:
                if rule.get("service") != events_backend:
                    rule["service"] = events_backend
                    changed = True
                break
        else:
            path_rules.insert(0, {"paths": [events_path], "service": events_backend})
            changed = True
    if path_rules:
        next_matcher["pathRules"] = path_rules
    minimal["pathMatchers"].append(next_matcher)

if not found:
    print(f"ERROR: path matcher {matcher_name!r} not found", file=sys.stderr)
    sys.exit(1)

json.dump(minimal, open(out_path, "w"), indent=2, sort_keys=True)
sys.exit(2 if changed else 0)
PY
  local rc=$?
  set -e
  if [[ "$rc" -eq 0 ]]; then
    echo "$URL_MAP_NAME already routes $EVENTS_PATH to $EVENTS_BACKEND"
    rm -f "$import_file"
    return 0
  fi
  if [[ "$rc" -ne 2 ]]; then
    rm -f "$import_file"
    return "$rc"
  fi

  run gcloud compute url-maps import "$URL_MAP_NAME" \
    --project="$PROJECT_ID" \
    --global \
    --source="$import_file" \
    --quiet
  rm -f "$import_file"
}

verify_events_path_rule() {
  local url_map_json backend_self_link
  url_map_json="$(
    gcloud compute url-maps describe "$URL_MAP_NAME" \
      --project="$PROJECT_ID" \
      --global \
      --format=json
  )"
  backend_self_link="$(
    gcloud compute backend-services describe "$EVENTS_BACKEND" \
      --project="$PROJECT_ID" \
      --global \
      --format='value(selfLink)'
  )"
  URL_MAP_JSON="$url_map_json" \
    EVENTS_BACKEND_SELF_LINK="$backend_self_link" \
    API_PATH_MATCHER="$API_PATH_MATCHER" \
    EVENTS_PATH="$EVENTS_PATH" \
    python3 - <<'PY'
import json
import os
import sys

doc = json.loads(os.environ["URL_MAP_JSON"])
matcher_name = os.environ["API_PATH_MATCHER"]
events_path = os.environ["EVENTS_PATH"]
events_backend = os.environ["EVENTS_BACKEND_SELF_LINK"]
for matcher in doc.get("pathMatchers", []):
    if matcher.get("name") != matcher_name:
        continue
    for rule in matcher.get("pathRules", []) or []:
        if events_path in (rule.get("paths", []) or []) and rule.get("service") == events_backend:
            sys.exit(0)
print(f"ERROR: {events_path} is not routed to {events_backend}", file=sys.stderr)
sys.exit(1)
PY
}

gcloud run services describe "$EVENTS_SERVICE" \
  --project="$PROJECT_ID" \
  --region="$REGION" >/dev/null

ensure_events_neg
ensure_events_backend
ensure_events_path_rule
verify_events_path_rule
