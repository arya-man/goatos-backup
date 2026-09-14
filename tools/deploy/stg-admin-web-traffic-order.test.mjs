import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const script = readFileSync(new URL("./stg-clouddeploy-task.sh", import.meta.url), "utf8");
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
  const noTraffic = indexOfOrThrow("    --no-traffic");
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

  assert.match(restoreBlock, /--min=1\s+\\/, "api restore must keep one warm instance");
  assert.match(restoreBlock, /--max=2\s+\\/, "api restore must restore the intended cost-capped max scale");
  assert.match(restoreBlock, /--min-instances=1\s+\\/, "api restore must preserve min instance annotation");
  assert.match(restoreBlock, /--max-instances=2\s+\\/, "api restore must preserve max instance annotation");
  assert.match(restoreBlock, /--concurrency=10\s+\\/, "api restore must cap per-instance request concurrency to the default DB pool");
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
    "analytics events must include proof-storage boot env because api bootstrap builds it before route filtering",
  );
  assert.match(
    eventsService,
    /GOATOS_GCS_BUCKET"[\s\S]*?google_storage_bucket\.proof_media\.name/,
    "analytics events must include the proof media bucket required by shared bootstrap",
  );
  assert.match(
    eventsService,
    /GOATOS_GCS_SERVICE_ACCOUNT_JSON"[\s\S]*?proof_gcs_service_account_json/,
    "analytics events must include the proof media signer secret required by shared bootstrap",
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
  assert.match(script, /GOATOS_ANALYTICS_MAX_IN_FLIGHT=2/);
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
