import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const script = readFileSync(new URL("./stg-clouddeploy-task.sh", import.meta.url), "utf8");

function indexOfOrThrow(needle) {
  const index = script.indexOf(needle);
  assert.notEqual(index, -1, `missing ${needle}`);
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
