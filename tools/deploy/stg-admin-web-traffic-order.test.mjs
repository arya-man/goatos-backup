import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { fileURLToPath } from "node:url";

const script = readFileSync(new URL("./stg-clouddeploy-task.sh", import.meta.url), "utf8");
const taskScript = fileURLToPath(new URL("./stg-clouddeploy-task.sh", import.meta.url));
const localCiScript = readFileSync(new URL("../ci/run-local-ci.sh", import.meta.url), "utf8");
const releaseScript = readFileSync(new URL("./stg-clouddeploy-release.sh", import.meta.url), "utf8");
const bootstrap = readFileSync(new URL("../../backend/internal/bootstrap/api.go", import.meta.url), "utf8");
const analyticsRoutingScript = readFileSync(
  new URL("./stg-analytics-events-routing.sh", import.meta.url),
  "utf8",
);
const runnerDockerfile = readFileSync(
  new URL("../../deploy/clouddeploy/stg/runner.Dockerfile", import.meta.url),
  "utf8",
);
const apiTerraform = readFileSync(
  new URL("../../infra/envs/stg/cloud_run_services.tf", import.meta.url),
  "utf8",
);
const stgMainTerraform = readFileSync(
  new URL("../../infra/envs/stg/main.tf", import.meta.url),
  "utf8",
);

function indexOfOrThrow(needle) {
  const index = script.indexOf(needle);
  assert.notEqual(index, -1, `missing ${needle}`);
  return index;
}

function indexOfOrThrowAfter(needle, start) {
  const index = script.indexOf(needle, start);
  assert.notEqual(index, -1, `missing ${needle} after index ${start}`);
  return index;
}

test("admin-web deploy waits for the created revision before switching traffic", () => {
  const deploy = indexOfOrThrow('run gcloud run services update "$ADMIN_WEB_SERVICE"');
  const noTraffic = indexOfOrThrowAfter("    --no-traffic", deploy);
  const captureCreated = indexOfOrThrow("admin_web_revision=\"$(gcloud run services describe \"$ADMIN_WEB_SERVICE\"");
  const waitCreated = indexOfOrThrow('wait_revision_ready "$admin_web_revision" "admin-web pre-traffic"');
  const switchTraffic = indexOfOrThrow('run gcloud run services update-traffic "$ADMIN_WEB_SERVICE"');

  assert.ok(noTraffic > deploy, "admin-web deploy must create the revision without sending live traffic to it");
  assert.ok(captureCreated > noTraffic, "deploy must capture the new admin-web revision after creation");
  assert.ok(waitCreated > captureCreated, "deploy must wait for the captured revision to become ready");
  assert.ok(switchTraffic > waitCreated, "traffic must move only after the captured admin-web revision is ready");
});

test("api post-migration restore preserves staging latency scale settings", () => {
  const migrate = indexOfOrThrow('run gcloud run jobs execute "$MIGRATE_JOB"');
  const restore = indexOfOrThrowAfter('run gcloud run services update "$API_SERVICE"', migrate);
  const apiTraffic = indexOfOrThrowAfter('run gcloud run services update-traffic "$API_SERVICE"', restore);
  const restoreBlock = script.slice(restore, apiTraffic);
  const captureCreated = indexOfOrThrowAfter("api_revision=\"$(gcloud run services describe \"$API_SERVICE\"", restore);
  const waitCreated = indexOfOrThrowAfter('wait_revision_ready "$api_revision" "api post-migration pre-traffic"', captureCreated);

  assert.match(restoreBlock, /--min=1\s+\\/, "api restore must keep one warm instance");
  assert.match(restoreBlock, /--max=2\s+\\/, "api restore must restore the intended cost-capped max scale");
  assert.match(restoreBlock, /--min-instances=1\s+\\/, "api restore must preserve min instance annotation");
  assert.match(restoreBlock, /--max-instances=2\s+\\/, "api restore must preserve max instance annotation");
  assert.match(restoreBlock, /--concurrency=10\s+\\/, "api restore must cap per-instance request concurrency to the default DB pool");
  assert.match(restoreBlock, /--no-traffic\s+\\/, "api restore must create the new revision without sending live traffic to it");
  assert.ok(captureCreated > restore, "deploy must capture the new api revision after creation");
  assert.ok(waitCreated > captureCreated, "deploy must wait for the captured api revision before traffic");
  assert.ok(apiTraffic > waitCreated, "api traffic must move only after the captured revision is ready");
  assert.match(
    script.slice(apiTraffic, indexOfOrThrowAfter('wait_service_ready "$API_SERVICE" "post-migration restore"', apiTraffic)),
    /--to-revisions=\$\{api_revision\}=100/,
    "api traffic must pin the verified revision, not blindly route to latest",
  );
});

test("api pre-migration quiesce waits on the hidden created revision", () => {
  const fallback = indexOfOrThrow("Emergency fallback for a known destructive migration");
  const quiesce = indexOfOrThrowAfter('run gcloud run services update "$API_SERVICE"', fallback);
  const workerDrain = indexOfOrThrowAfter('run gcloud run services update "$KERNEL_WORKER_SERVICE"', quiesce);
  const quiesceBlock = script.slice(quiesce, workerDrain);
  const captureCreated = indexOfOrThrowAfter("api_revision=\"$(gcloud run services describe \"$API_SERVICE\"", quiesce);
  const waitCreated = indexOfOrThrowAfter('wait_revision_ready "$api_revision" "pre-migration quiesce"', captureCreated);

  assert.match(quiesceBlock, /--no-traffic\s+\\/, "pre-migration API quiesce must keep live traffic on the old revision");
  assert.ok(captureCreated > quiesce, "deploy must capture the hidden pre-migration API revision");
  assert.ok(waitCreated > captureCreated, "hidden 0% traffic revision must be checked directly");
  assert.doesNotMatch(
    quiesceBlock,
    /wait_service_ready "\$API_SERVICE" "pre-migration quiesce"/,
    "service latestReady can remain on the old live revision for 0% traffic candidates",
  );
});

function runWithFakeGcloud(extra = {}) {
  const dir = mkdtempSync(path.join(tmpdir(), "stg-hidden-api-"));
  const mock = `#!/usr/bin/env python3
import json
import os
import pathlib
import sys

root = pathlib.Path(os.environ["FIXTURE"])
name = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
cmd = " ".join(args)

with open(root / "calls", "a") as f:
    f.write(name + " " + cmd + "\\n")

def touched(flag):
    return (root / flag).exists()

def touch(flag):
    (root / flag).touch()

def service_name():
    for idx, arg in enumerate(args):
        if arg in {"describe", "update", "update-traffic", "deploy"} and idx + 1 < len(args):
            return args[idx + 1]
    return ""

def image_for(service):
    if service == "goatos-admin-web-stg":
        return os.environ["ADMIN_WEB_IMAGE"] if touched("goatos-admin-web-stg_updated") else "admin-old"
    return os.environ["BACKEND_IMAGE"] if touched(service + "_updated") else "backend-old"

def service_doc(service):
    latest_created = service + "-old"
    latest_ready = service + "-old"
    traffic_revision = service + "-old"
    image = image_for(service)
    if service == "goatos-api-stg" and touched("api_pre_created"):
        latest_created = "api-new"
        latest_ready = "api-old"
        traffic_revision = "api-old"
        image = os.environ["BACKEND_IMAGE"]
    if service == "goatos-api-stg" and touched("goatos-api-stg_updated"):
        latest_created = "api-final"
        latest_ready = "api-final"
        traffic_revision = "api-final" if touched("api_traffic") else "api-old"
        image = os.environ["BACKEND_IMAGE"]
    if service == "goatos-admin-web-stg" and touched("goatos-admin-web-stg_updated"):
        latest_created = "admin-new"
        latest_ready = "admin-new"
        traffic_revision = "admin-new" if touched("admin_traffic") else "admin-old"
        image = os.environ["ADMIN_WEB_IMAGE"]
    return {
        "status": {
            "url": "https://" + service + ".run.app",
            "latestCreatedRevisionName": latest_created,
            "latestReadyRevisionName": latest_ready,
            "traffic": [{"revisionName": traffic_revision, "percent": 100}],
            "conditions": [{"type": "Ready", "status": "True"}],
        },
        "spec": {"template": {"spec": {"containers": [{"image": image}]}}},
    }

if name == "sleep":
    sys.exit(0)

if name == "curl":
    if "dashboard.mesha.sg" in cmd:
        print("200", end="")
    else:
        print("204", end="")
    sys.exit(0)

if args[:4] == ["artifacts", "docker", "images", "describe"]:
    sys.exit(0)

if args[:3] == ["iam", "service-accounts", "describe"]:
    sys.exit(0)

if args[:2] == ["secrets", "get-iam-policy"]:
    print(json.dumps({"bindings": [
        {"role": "roles/secretmanager.secretAccessor", "members": ["serviceAccount:goatos-events-stg@goatos-stg.iam.gserviceaccount.com"]},
    ]}))
    sys.exit(0)

if args[:3] == ["run", "jobs", "describe"]:
    if args[3] == "goatos-stg-migrate":
        if "--format=json" in args:
            print(json.dumps({"spec": {"template": {"spec": {"template": {"spec": {"containers": [{"image": os.environ["CLOUD_DEPLOY_customTarget_migrationImage"]}]}}}}}}))
        sys.exit(0)
    if args[3] == "goatos-stg-analytics-rollup":
        env = [
            {"name": "GOATOS_ANALYTICS_SOURCE_APP_ID", "value": os.environ.get("GOATOS_ANALYTICS_SOURCE_APP_ID", "sg.mesha.goatos")},
            {"name": "GOATOS_CRASHLYTICS_BQ_TABLE", "value": os.environ.get("GOATOS_CRASHLYTICS_BQ_TABLE", "goatos-stg.firebase_crashlytics.sg_mesha_goatos_ANDROID")},
            {"name": "GOATOS_CRASHLYTICS_SESSIONS_TABLE", "value": os.environ.get("GOATOS_CRASHLYTICS_SESSIONS_TABLE", "goatos-stg.firebase_sessions.sg_mesha_goatos_ANDROID")},
            {"name": "GOATOS_PERFORMANCE_BQ_TABLE", "value": os.environ.get("GOATOS_PERFORMANCE_BQ_TABLE", "goatos-stg.firebase_performance.sg_mesha_goatos_ANDROID")},
        ]
        print(json.dumps({"spec": {"template": {"spec": {"template": {"spec": {"containers": [{"image": os.environ["BACKEND_IMAGE"], "env": env}]}}}}}}))
        sys.exit(0)
    sys.exit(1)

if args[:3] == ["run", "jobs", "get-iam-policy"]:
    print(json.dumps({"bindings": [
        {"role": "roles/run.jobsExecutorWithOverrides", "members": ["serviceAccount:worker@goatos-stg.iam.gserviceaccount.com"]},
        {"role": "roles/run.viewer", "members": ["serviceAccount:worker@goatos-stg.iam.gserviceaccount.com"]},
    ]}))
    sys.exit(0)

if args[:3] == ["run", "jobs", "list"]:
    sys.exit(0)

if args[:3] == ["run", "jobs", "update"] or args[:3] == ["run", "jobs", "execute"]:
    if "goatos-stg-migrate" in args:
        touch("migration")
    sys.exit(0)

if args[:3] == ["run", "services", "update"]:
    service = service_name()
    if service == "goatos-api-stg" and "--no-traffic" in args and not touched("migration"):
        touch("api_pre_created")
    else:
        touch(service + "_updated")
    sys.exit(0)

if args[:2] == ["run", "deploy"]:
    touch(service_name() + "_updated")
    sys.exit(0)

if args[:3] == ["run", "services", "update-traffic"]:
    service = service_name()
    if service == "goatos-api-stg":
        touch("api_traffic")
    if service == "goatos-admin-web-stg":
        touch("admin_traffic")
    sys.exit(0)

if args[:3] == ["run", "revisions", "describe"]:
    revision = args[3]
    hidden_ready = (root / "hidden_ready").read_text().strip()
    ready = "False" if hidden_ready == "false" and revision == "api-new" else "True"
    print(json.dumps({"status": {"conditions": [{"type": "Ready", "status": ready}]}}))
    sys.exit(0)

if args[:3] == ["run", "revisions", "delete"]:
    sys.exit(0)

if args[:3] == ["run", "services", "describe"]:
    service = args[3]
    if "value(status.latestCreatedRevisionName)" in cmd:
        print(service_doc(service)["status"]["latestCreatedRevisionName"])
    elif "value(status.latestReadyRevisionName)" in cmd:
        print(service_doc(service)["status"]["latestReadyRevisionName"])
    elif "value(status.url)" in cmd:
        print(service_doc(service)["status"]["url"])
    elif "value(spec.template.spec.serviceAccountName)" in cmd:
        print("worker@goatos-stg.iam.gserviceaccount.com")
    else:
        print(json.dumps(service_doc(service)))
    sys.exit(0)

if args[:2] == ["storage", "cp"]:
    sys.exit(0)

sys.exit("unexpected " + name + " " + cmd)
`;
  for (const name of ["gcloud", "curl", "sleep"]) {
    writeFileSync(path.join(dir, name), mock, { mode: 0o755 });
  }
  const routingDir = path.join(dir, "tools", "deploy");
  mkdirSync(routingDir, { recursive: true });
  writeFileSync(path.join(routingDir, "stg-analytics-events-routing.sh"), "#!/usr/bin/env bash\nexit 0\n", { mode: 0o755 });
  writeFileSync(path.join(dir, "hidden_ready"), extra.HIDDEN_REVISION_READY === "false" ? "false" : "true");
  try {
    const env = {
      ...process.env,
      PATH: `${dir}:${process.env.PATH}`,
      FIXTURE: dir,
      CLOUD_DEPLOY_customTarget_commitSha: "abcdef123456",
      CLOUD_DEPLOY_customTarget_backendImage: "asia-south1-docker.pkg.dev/goatos-stg/goatos/backend:abcdef123456",
      CLOUD_DEPLOY_customTarget_migrationImage: "asia-south1-docker.pkg.dev/goatos-stg/goatos/migrations:abcdef123456",
      CLOUD_DEPLOY_customTarget_adminWebImage: "asia-south1-docker.pkg.dev/goatos-stg/goatos/admin-web:abcdef123456",
      CLOUD_DEPLOY_customTarget_zeroDowntimeDeploy: "false",
      BACKEND_IMAGE: "asia-south1-docker.pkg.dev/goatos-stg/goatos/backend:abcdef123456",
      ADMIN_WEB_IMAGE: "asia-south1-docker.pkg.dev/goatos-stg/goatos/admin-web:abcdef123456",
      CLOUD_DEPLOY_OUTPUT_GCS_PATH: "gs://fixture",
      ...extra,
    };
    const result = spawnSync("bash", [taskScript, "deploy"], { cwd: dir, encoding: "utf8", env });
    return { ...result, calls: readFileSync(path.join(dir, "calls"), "utf8") };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

test("pre-migration hidden API revision can be ready while service latestReady stays old", () => {
  const result = runWithFakeGcloud({ HIDDEN_REVISION_READY: "true" });

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.calls, /run revisions describe api-new/);
  assert.match(result.calls, /run jobs execute goatos-stg-migrate/);
  assert.ok(
    result.calls.indexOf("run revisions describe api-new") < result.calls.indexOf("run jobs execute goatos-stg-migrate"),
    "migration must wait for the hidden API revision readiness check",
  );
  assert.ok(
    result.calls.indexOf("run jobs execute goatos-stg-migrate") < result.calls.indexOf("run services update-traffic goatos-api-stg"),
    "API traffic must not move until after migrations",
  );
  assert.ok(
    result.calls.indexOf("run jobs execute goatos-stg-migrate") < result.calls.indexOf("run services update goatos-admin-web-stg"),
    "admin-web must not be touched until after migrations",
  );
});

test("unready hidden API revision fails before migration and public traffic changes", () => {
  const result = runWithFakeGcloud({ HIDDEN_REVISION_READY: "false" });

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /revision api-new did not reach pre-migration quiesce readiness/);
  assert.match(result.calls, /run revisions describe api-new/);
  assert.doesNotMatch(result.calls, /run jobs execute goatos-stg-migrate/);
  assert.doesNotMatch(result.calls, /run services update-traffic goatos-api-stg/);
  assert.doesNotMatch(result.calls, /run services update goatos-admin-web-stg/);
});

test("api terraform and deploy restore keep the same latency shape", () => {
  assert.match(
    apiTerraform,
    /max_instance_request_concurrency\s*=\s*10/,
    "terraform must pin api request concurrency to the default DB pool",
  );
  assert.match(apiTerraform, /min_instance_count\s*=\s*1/, "terraform must keep one warm api instance");
  assert.match(apiTerraform, /max_instance_count\s*=\s*2/, "terraform must preserve the staging api billing cap");

  const migrate = indexOfOrThrow('run gcloud run jobs execute "$MIGRATE_JOB"');
  const restore = indexOfOrThrowAfter('run gcloud run services update "$API_SERVICE"', migrate);
  const apiTraffic = indexOfOrThrowAfter('run gcloud run services update-traffic "$API_SERVICE"', restore);
  const restoreBlock = script.slice(restore, apiTraffic);

  assert.match(restoreBlock, /--min=1\s+\\/, "deploy restore must match terraform min scale");
  assert.match(restoreBlock, /--max=2\s+\\/, "deploy restore must match terraform max scale");
  assert.match(restoreBlock, /--concurrency=10\s+\\/, "deploy restore must match terraform concurrency");
  assert.match(restoreBlock, /--ingress=internal-and-cloud-load-balancing\s+\\/, "api must remain reachable through the public HTTPS load balancer");
});

test("pre-migration fallback cannot take public dashboard or API traffic", () => {
  const fallback = indexOfOrThrow('if [[ "$GOATOS_STG_ZERO_DOWNTIME_DEPLOY" == "true" ]]');
  const migrate = indexOfOrThrow('run gcloud run jobs execute "$MIGRATE_JOB"');
  const preMigrationBlock = script.slice(fallback, migrate);

  assert.doesNotMatch(
    preMigrationBlock,
    /run gcloud run services update "\$ADMIN_WEB_SERVICE"/,
    "admin-web must not be mutated before migrations pass",
  );
  assert.doesNotMatch(
    preMigrationBlock,
    /--ingress=internal\s+\\/,
    "pre-migration updates must not block the HTTPS load balancer",
  );
  assert.match(
    preMigrationBlock,
    /--ingress=internal-and-cloud-load-balancing\s+\\/,
    "pre-migration updates must preserve load-balancer ingress",
  );
  assert.match(preMigrationBlock, /--no-traffic\s+\\/, "pre-migration candidate revisions must not receive live traffic");
  assert.doesNotMatch(
    preMigrationBlock,
    /run gcloud run services update-traffic "\$API_SERVICE"/,
    "API traffic must not move before migrations finish",
  );
});

test("release wrapper verifies the same api latency shape and has safe deploy defaults", () => {
  assert.match(
    releaseScript,
    /GOATOS_STG_ZERO_DOWNTIME_DEPLOY="\$\{GOATOS_STG_ZERO_DOWNTIME_DEPLOY:-true\}"/,
    "release wrapper must not fail under set -u when zero-downtime deploy is unset",
  );
  assert.match(
    releaseScript,
    /customTarget\/zeroDowntimeDeploy=\$\{GOATOS_STG_ZERO_DOWNTIME_DEPLOY\}/,
    "release wrapper must pass the zero-downtime setting through Cloud Deploy",
  );
  assert.match(
    releaseScript,
    /\[\[ "\$concurrency" == "10" \]\] \|\| die "goatos-api-stg concurrency drift: got \$\{concurrency:-unset\} want 10"/,
    "release receipt must expect the API concurrency pinned by terraform and the deploy task",
  );
  assert.doesNotMatch(
    releaseScript,
    /--format='value\([^']*autoscaling\\\.knative\\\.dev\/minScale/,
    "release receipt must not use gcloud value(...) projections for slash-containing annotation keys",
  );
  assert.match(
    releaseScript,
    /--format=json \| python3 -c/,
    "release receipt must parse Cloud Run annotations from JSON so slash-containing keys are safe",
  );
});

test("analytics events has an isolated capped deploy lane", () => {
  const serviceStart = apiTerraform.indexOf('resource "google_cloud_run_v2_service" "analytics_events"');
  const serviceEnd = apiTerraform.indexOf('resource "google_cloud_run_v2_service_iam_member" "analytics_events_public_invoker"');
  assert.notEqual(serviceStart, -1, "terraform must declare the analytics events service");
  assert.notEqual(serviceEnd, -1, "terraform must declare analytics events IAM after the service");
  const eventsService = apiTerraform.slice(serviceStart, serviceEnd);

  assert.match(
    eventsService,
    /resource "google_cloud_run_v2_service" "analytics_events"/,
    "terraform must declare the analytics events service",
  );
  assert.match(
    eventsService,
    /name\s*=\s*"goatos-analytics-events-stg"/,
    "analytics events service must use the staging service name",
  );
  assert.match(
    eventsService,
    /service_account\s*=\s*google_service_account\.runtime\["analytics_events"\]\.email/,
    "analytics events must not run as the main api service account",
  );
  assert.match(
    eventsService,
    /GOATOS_API_ROUTE_MODE"[\s\S]*?value\s*=\s*"events"/,
    "analytics events must boot in event-only route mode",
  );
  assert.match(
    eventsService,
    /GOATOS_MEDIA_STORAGE"[\s\S]*?value\s*=\s*"gcs"/,
    "analytics events keeps compatible proof media env without making it a boot dependency",
  );
  assert.match(
    eventsService,
    /GOATOS_GCS_BUCKET"[\s\S]*?google_storage_bucket\.proof_media\.name/,
    "analytics events keeps a compatible proof media bucket env without making it a boot dependency",
  );
  assert.match(
    eventsService,
    /GOATOS_GCS_SERVICE_ACCOUNT_JSON"[\s\S]*?proof_gcs_service_account_json/,
    "analytics events keeps a compatible proof media signer env without making it a boot dependency",
  );
  const newApiStart = bootstrap.indexOf("func NewAPI(");
  const eventsReturn = bootstrap.indexOf("return newEventsAPI(", newApiStart);
  assert.notEqual(newApiStart, -1, "bootstrap must define NewAPI");
  assert.notEqual(eventsReturn, -1, "bootstrap must return through newEventsAPI in events mode");
  assert.ok(
    eventsReturn < bootstrap.indexOf("bulkImportPreviewSigningKey(cfg)", newApiStart) &&
      eventsReturn < bootstrap.indexOf("buildProofStorage()", newApiStart),
    "events route mode must return before preview signing key and proof storage boot dependencies",
  );

  const proofSecretStart = stgMainTerraform.indexOf("proof_gcs_service_account_json = {");
  const proofSecretEnd = stgMainTerraform.indexOf("firebase_web_config = {", proofSecretStart);
  assert.notEqual(proofSecretStart, -1, "staging secret map must declare proof_gcs_service_account_json");
  assert.notEqual(proofSecretEnd, -1, "staging secret map must keep proof secret before firebase config");
  const proofSecret = stgMainTerraform.slice(proofSecretStart, proofSecretEnd);
  assert.match(
    proofSecret,
    /accessors\s*=\s*\[[\s\S]*?"api"[\s\S]*?"analytics_events"[\s\S]*?\]/,
    "analytics events must have Secret Manager access to every secret injected into its Cloud Run env",
  );

  const previewSecretStart = stgMainTerraform.indexOf("bulk_import_preview_signing_key = {");
  const previewSecretEnd = stgMainTerraform.indexOf("proof_gcs_service_account_json = {", previewSecretStart);
  assert.notEqual(previewSecretStart, -1, "staging secret map must declare bulk_import_preview_signing_key");
  assert.notEqual(previewSecretEnd, -1, "staging secret map must keep bulk preview secret before proof config");
  const previewSecret = stgMainTerraform.slice(previewSecretStart, previewSecretEnd);
  assert.match(
    previewSecret,
    /accessors\s*=\s*\[[\s\S]*?"api"[\s\S]*?"analytics_events"[\s\S]*?\]/,
    "analytics events must be able to read the bulk preview signing key if that secret is injected",
  );

  assert.match(
    eventsService,
    /GOATOS_ANALYTICS_MAX_IN_FLIGHT"[\s\S]*?value\s*=\s*"1"/,
    "analytics events must allow only one in-flight event write per instance",
  );
  assert.match(
    eventsService,
    /GOATOS_PG_MAX_CONNS"[\s\S]*?value\s*=\s*"2"/,
    "analytics events must have a tiny DB pool",
  );
  assert.match(
    eventsService,
    /min_instance_count\s*=\s*0[\s\S]*?max_instance_count\s*=\s*1/,
    "analytics events must be capped separately from the business api",
  );

  assert.match(script, /ANALYTICS_EVENTS_SERVICE="\$\{ANALYTICS_EVENTS_SERVICE:-goatos-analytics-events-stg\}"/);
  assert.match(script, /GOATOS_STG_TENANT_ID="\$\{GOATOS_STG_TENANT_ID:-00000000-0000-4000-8000-000000000001\}"/);
  assert.match(script, /secret_accessor_exists\(\)/, "deploy must define a live Secret Manager IAM preflight");
  assert.match(
    script,
    /secret_accessor_exists "goatos-stg-gcs-service-account-json" "\$ANALYTICS_EVENTS_SERVICE_ACCOUNT"/,
    "deploy must fail before migrations when the events service cannot read the proof GCS secret",
  );
  assert.match(script, /gcloud run deploy "\$ANALYTICS_EVENTS_SERVICE"[\s\S]*?--max-instances=1\s+\\/);
  assert.match(script, /--service-account="\$ANALYTICS_EVENTS_SERVICE_ACCOUNT"/);
  assert.match(script, /--allow-unauthenticated\s+\\/);
  assert.match(script, /--add-cloudsql-instances="\$\{PROJECT_ID\}:\$\{REGION\}:goatos-stg-core-db"/);
  assert.match(script, /--set-secrets="DATABASE_URL=goatos-stg-database-url:latest/);
  assert.match(script, /GOATOS_API_ROUTE_MODE=events/);
  assert.match(script, /GOATOS_MEDIA_STORAGE=gcs/);
  assert.match(script, /GOATOS_GCS_BUCKET=goatos-stg-media/);
  assert.match(script, /GOATOS_GCS_SERVICE_ACCOUNT_JSON=goatos-stg-gcs-service-account-json:latest/);
  assert.match(script, /GOATOS_AUTH_SESSION_ALLOWED_TENANT_IDS=\$\{GOATOS_STG_TENANT_ID\}/);
  assert.match(script, /GOATOS_ANALYTICS_MAX_IN_FLIGHT=1/);
  assert.match(script, /GOATOS_PG_MAX_CONNS=2/);
  assert.match(script, /run_analytics_events_routing/);
  assert.match(script, /service_image "\$ANALYTICS_EVENTS_SERVICE"/);
  assert.match(script, /smoke_public_events_route\(\)/);
  assert.match(script, /curl[\s\S]*"\$STG_API_URL\/app\/analytics\/events"/);
  assert.match(script, /resource\.labels\.service_name=\\?"\$ANALYTICS_EVENTS_SERVICE\\?"/);
  assert.match(script, /public \/app\/analytics\/events smoke did not land on \$ANALYTICS_EVENTS_SERVICE/);
});

test("analytics events routing script isolates only the event path", () => {
  assert.match(
    analyticsRoutingScript,
    /EVENTS_PATH="\$\{EVENTS_PATH:-\/app\/analytics\/events\}"/,
    "routing script must target only the analytics event endpoint",
  );
  assert.match(
    analyticsRoutingScript,
    /EVENTS_BACKEND="\$\{EVENTS_BACKEND:-goatos-analytics-events-stg-backend\}"/,
    "routing script must use the analytics events backend",
  );
  assert.match(
    analyticsRoutingScript,
    /EVENTS_NEG="\$\{EVENTS_NEG:-goatos-analytics-events-stg-neg\}"/,
    "routing script must use the analytics events serverless NEG",
  );
  assert.match(
    analyticsRoutingScript,
    /--cloud-run-service="\$EVENTS_SERVICE"/,
    "serverless NEG must point at the analytics events Cloud Run service",
  );
  assert.match(
    analyticsRoutingScript,
    /path_rules\.insert\(0, \{"paths": \[events_path\], "service": events_backend\}\)/,
    "routing script must add a path rule instead of changing the default API backend",
  );
  assert.doesNotMatch(
    analyticsRoutingScript,
    /defaultService"\]\s*=\s*events_backend/,
    "routing script must not point the API host default service at events",
  );
  assert.match(
    runnerDockerfile,
    /COPY tools\/deploy\/stg-analytics-events-routing\.sh \/usr\/local\/bin\/goatos-stg-analytics-events-routing/,
    "Cloud Deploy runner image must contain the routing script used by the deploy task",
  );
});

test("ai-doctor remains visible but non-blocking for runtime deploy receipts", () => {
  assert.match(
    localCiScript,
    /^\s*optional_step "agent: ai-doctor"\s+make ai-doctor/m,
    "ai-doctor should report stale token/index tooling without blocking product deploy CI",
  );
  assert.doesNotMatch(
    localCiScript,
    /^\s*step "agent: ai-doctor"\s+make ai-doctor/m,
    "ai-doctor must not be a blocking local-CI step",
  );
});
