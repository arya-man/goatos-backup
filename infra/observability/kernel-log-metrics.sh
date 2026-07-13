#!/bin/bash
# Kernel-Pipeline Log-Based Metrics Builder
# Creates Cloud Monitoring DISTRIBUTION metrics from Cloud Run Job structured logs.
# Idempotent: checks existence before creating. Run this on both stg and prod.
#
# Usage: ./kernel-log-metrics.sh [project_id] [region]
# Defaults to goatos-stg / asia-south1

set -euo pipefail

PROJECT="${1:-goatos-stg}"
REGION="${2:-asia-south1}"
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

echo "=== Kernel Log-Based Metrics Builder ==="
echo "Project: $PROJECT"
echo "Region: $REGION"

# Helper to check if metric exists
metric_exists() {
  local name="$1"
  if gcloud logging metrics describe "$name" --project="$PROJECT" &>/dev/null; then
    return 0
  else
    return 1
  fi
}

# Python helper to create metrics with proper JSON escaping
create_metrics_via_python() {
  local tmpdir="$1"
  python3 << PYTHON_EOF
import json
import subprocess

project = "$PROJECT"
tmpdir = "$tmpdir"

def metric_exists(name):
    result = subprocess.run(
        ["gcloud", "logging", "metrics", "describe", name, f"--project={project}"],
        capture_output=True, text=True
    )
    return result.returncode == 0

def create_metric(name, description, log_filter, value_extractor=None):
    if metric_exists(name):
        print(f"✓ Metric already exists: {name}")
        return

    metric_descriptor = {
        "metricKind": "DELTA",
        "unit": "1",
        "displayName": name
    }

    config = {
        "name": f"projects/{project}/metrics/user/{name}",
        "description": description,
        "filter": log_filter,
        "metricDescriptor": metric_descriptor
    }

    if value_extractor:
        # Distribution metric
        metric_descriptor["valueType"] = "DISTRIBUTION"
        config["valueExtractor"] = value_extractor
        config["bucketOptions"] = {
            "exponentialBuckets": {
                "numFiniteBuckets": 64,
                "growthFactor": 2,
                "scale": 1
            }
        }
    else:
        # Counter metric
        metric_descriptor["valueType"] = "INT64"

    config_file = f"{tmpdir}/{name}.json"
    with open(config_file, "w") as f:
        json.dump(config, f, indent=2)

    print(f"  Creating: {name}")
    result = subprocess.run(
        ["gcloud", "logging", "metrics", "create", name,
         f"--config-from-file={config_file}",
         f"--project={project}",
         "--quiet"],
        capture_output=True, text=True
    )
    if result.returncode == 0:
        print(f"✓ Created: {name}")
    else:
        print(f"ERROR: {result.stderr}", file=__import__('sys').stderr)
        __import__('sys').exit(1)

# === OUTBOX RELAY METRICS ===
print("\\n--- OUTBOX RELAY Metrics ---")

create_metric(
    "goatos_kernel_outbox_published",
    "Outbox relay: events published to Pub/Sub",
    f'resource.type=cloud_run_job AND resource.labels.project_id={project} AND resource.labels.job_name={project}-outbox-relay AND jsonPayload.msg="outbox relay run complete"',
    'EXTRACT(jsonPayload.published)'
)

create_metric(
    "goatos_kernel_outbox_failed",
    "Outbox relay: events failed (retryable)",
    f'resource.type=cloud_run_job AND resource.labels.project_id={project} AND resource.labels.job_name={project}-outbox-relay AND jsonPayload.msg="outbox relay run complete"',
    'EXTRACT(jsonPayload.failed)'
)

create_metric(
    "goatos_kernel_outbox_dead_letter",
    "Outbox relay: events moved to dead letter (unrecoverable)",
    f'resource.type=cloud_run_job AND resource.labels.project_id={project} AND resource.labels.job_name={project}-outbox-relay AND jsonPayload.msg="outbox relay run complete"',
    'EXTRACT(jsonPayload.dead_letter)'
)

create_metric(
    "goatos_kernel_outbox_claimed",
    "Outbox relay: events claimed for processing",
    f'resource.type=cloud_run_job AND resource.labels.project_id={project} AND resource.labels.job_name={project}-outbox-relay AND jsonPayload.msg="outbox relay run complete"',
    'EXTRACT(jsonPayload.claimed)'
)

create_metric(
    "goatos_kernel_outbox_reclaimed",
    "Outbox relay: stale leases reclaimed (retry)",
    f'resource.type=cloud_run_job AND resource.labels.project_id={project} AND resource.labels.job_name={project}-outbox-relay AND jsonPayload.msg="outbox relay run complete"',
    'EXTRACT(jsonPayload.reclaimed_stale)'
)

create_metric(
    "goatos_kernel_outbox_retry_scheduled",
    "Outbox relay: events scheduled for retry",
    f'resource.type=cloud_run_job AND resource.labels.project_id={project} AND resource.labels.job_name={project}-outbox-relay AND jsonPayload.msg="outbox relay run complete"',
    'EXTRACT(jsonPayload.retry_scheduled)'
)

create_metric(
    "goatos_kernel_outbox_batches",
    "Outbox relay: batches processed",
    f'resource.type=cloud_run_job AND resource.labels.project_id={project} AND resource.labels.job_name={project}-outbox-relay AND jsonPayload.msg="outbox relay run complete"',
    'EXTRACT(jsonPayload.batches_processed)'
)

# === NOTIFICATION DISPATCHER METRICS ===
print("\\n--- NOTIFICATION DISPATCHER Metrics ---")

create_metric(
    "goatos_kernel_notify_sent",
    "Notification dispatcher: notifications sent successfully",
    f'resource.type=cloud_run_job AND resource.labels.project_id={project} AND resource.labels.job_name={project}-notification-dispatcher AND jsonPayload.msg="notification dispatch complete"',
    'EXTRACT(jsonPayload.sent)'
)

create_metric(
    "goatos_kernel_notify_failed",
    "Notification dispatcher: notifications failed (retryable)",
    f'resource.type=cloud_run_job AND resource.labels.project_id={project} AND resource.labels.job_name={project}-notification-dispatcher AND jsonPayload.msg="notification dispatch complete"',
    'EXTRACT(jsonPayload.failed)'
)

create_metric(
    "goatos_kernel_notify_exhausted",
    "Notification dispatcher: notifications exhausted all retry attempts",
    f'resource.type=cloud_run_job AND resource.labels.project_id={project} AND resource.labels.job_name={project}-notification-dispatcher AND jsonPayload.msg="notification dispatch complete"',
    'EXTRACT(jsonPayload.exhausted)'
)

create_metric(
    "goatos_kernel_notify_claimed",
    "Notification dispatcher: notifications claimed for processing",
    f'resource.type=cloud_run_job AND resource.labels.project_id={project} AND resource.labels.job_name={project}-notification-dispatcher AND jsonPayload.msg="notification dispatch complete"',
    'EXTRACT(jsonPayload.claimed)'
)

create_metric(
    "goatos_kernel_notify_reclaimed",
    "Notification dispatcher: stale leases reclaimed (retry)",
    f'resource.type=cloud_run_job AND resource.labels.project_id={project} AND resource.labels.job_name={project}-notification-dispatcher AND jsonPayload.msg="notification dispatch complete"',
    'EXTRACT(jsonPayload.reclaimed_stale)'
)

# === OBLIGATION SWEEPER METRICS ===
print("\\n--- OBLIGATION SWEEPER Metrics (Counter) ---")

create_metric(
    "goatos_kernel_sweeper_runs",
    "Obligation sweeper: number of sweeper runs (counts result log lines)",
    f'resource.type=cloud_run_job AND resource.labels.project_id={project} AND resource.labels.job_name={project}-obligation-sweeper AND textPayload=~"^swept version="'
)
PYTHON_EOF
}

create_metrics_via_python "$TMPDIR"

echo ""
echo "=== Metric creation complete ==="
echo ""
echo "To verify metric data is being collected, check a metric:"
echo "  (there is no 'gcloud monitoring time-series list' command — use the Monitoring REST API)"
echo "  TOKEN=\$(gcloud auth print-access-token)"
echo "  curl -s -H \"Authorization: Bearer \$TOKEN\" \\"
echo "    \"https://monitoring.googleapis.com/v3/projects/$PROJECT/timeSeries?filter=metric.type%3D%22logging.googleapis.com%2Fuser%2Fgoatos_kernel_outbox_published%22&interval.startTime=\$(date -u -d '30 minutes ago' +%Y-%m-%dT%H:%M:%SZ)&interval.endTime=\$(date -u +%Y-%m-%dT%H:%M:%SZ)\""
echo ""
echo "In Grafana, query these metrics via Google Cloud Monitoring datasource:"
echo "  Resource: cloud_run_job"
echo "  Metric: logging.googleapis.com/user/goatos_kernel_outbox_published (etc)"
