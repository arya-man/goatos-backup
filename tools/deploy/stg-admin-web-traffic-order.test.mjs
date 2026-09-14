import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const script = readFileSync(new URL("./stg-clouddeploy-task.sh", import.meta.url), "utf8");
const apiTerraform = readFileSync(
  new URL("../../infra/envs/stg/cloud_run_services.tf", import.meta.url),
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
  assert.match(restoreBlock, /--max=4\s+\\/, "api restore must restore the intended cost-capped max scale");
  assert.match(restoreBlock, /--min-instances=1\s+\\/, "api restore must preserve min instance annotation");
  assert.match(restoreBlock, /--max-instances=4\s+\\/, "api restore must preserve max instance annotation");
  assert.match(restoreBlock, /--concurrency=20\s+\\/, "api restore must cap per-instance request concurrency");
});

test("api terraform and deploy restore keep the same latency shape", () => {
  assert.match(
    apiTerraform,
    /max_instance_request_concurrency\s*=\s*20/,
    "terraform must pin api request concurrency",
  );
  assert.match(apiTerraform, /min_instance_count\s*=\s*1/, "terraform must keep one warm api instance");
  assert.match(apiTerraform, /max_instance_count\s*=\s*4/, "terraform must allow bounded api burst scale");

  const migrate = indexOfOrThrow('run gcloud run jobs execute "$MIGRATE_JOB"');
  const restore = indexOfOrThrowAfter('run gcloud run services update "$API_SERVICE"', migrate);
  const apiTraffic = indexOfOrThrowAfter('run gcloud run services update-traffic "$API_SERVICE"', restore);
  const restoreBlock = script.slice(restore, apiTraffic);

  assert.match(restoreBlock, /--min=1\s+\\/, "deploy restore must match terraform min scale");
  assert.match(restoreBlock, /--max=4\s+\\/, "deploy restore must match terraform max scale");
  assert.match(restoreBlock, /--concurrency=20\s+\\/, "deploy restore must match terraform concurrency");
});

test("analytics events has an isolated capped deploy lane", () => {
  assert.match(
    apiTerraform,
    /resource "google_cloud_run_v2_service" "analytics_events"/,
    "terraform must declare the analytics events service",
  );
  assert.match(
    apiTerraform,
    /name\s*=\s*"goatos-analytics-events-stg"/,
    "analytics events service must use the staging service name",
  );
  assert.match(
    apiTerraform,
    /service_account\s*=\s*google_service_account\.runtime\["analytics_events"\]\.email/,
    "analytics events must not run as the main api service account",
  );
  assert.match(
    apiTerraform,
    /GOATOS_API_ROUTE_MODE"[\s\S]*?value\s*=\s*"events"/,
    "analytics events must boot in event-only route mode",
  );
  assert.match(
    apiTerraform,
    /GOATOS_PG_MAX_CONNS"[\s\S]*?value\s*=\s*"2"/,
    "analytics events must have a tiny DB pool",
  );
  assert.match(
    apiTerraform,
    /min_instance_count\s*=\s*0[\s\S]*?max_instance_count\s*=\s*1/,
    "analytics events must be capped separately from the business api",
  );

  assert.match(script, /ANALYTICS_EVENTS_SERVICE="\$\{ANALYTICS_EVENTS_SERVICE:-goatos-analytics-events-stg\}"/);
  assert.match(script, /gcloud run services update "\$ANALYTICS_EVENTS_SERVICE"[\s\S]*?--max=1\s+\\/);
  assert.match(script, /GOATOS_API_ROUTE_MODE=events,GOATOS_ANALYTICS_MAX_IN_FLIGHT=2,GOATOS_PG_MAX_CONNS=2/);
  assert.match(script, /service_image "\$ANALYTICS_EVENTS_SERVICE"/);
});
