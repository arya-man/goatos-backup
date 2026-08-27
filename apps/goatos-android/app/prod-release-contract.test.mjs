import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

const here = new URL(".", import.meta.url).pathname;
const buildGradle = readFileSync(join(here, "build.gradle.kts"), "utf8");
const googleServices = JSON.parse(readFileSync(join(here, "src/prod/google-services.json"), "utf8"));

test("prod flavor uses public production-facing package and URLs", () => {
  const prodBlock = buildGradle.match(/create\("prod"\) \{[\s\S]*?firebaseAppDistribution \{/)?.[0] ?? "";

  assert.match(buildGradle, /applicationId = "sg\.mesha\.goatos"/);
  assert.match(prodBlock, /API_BASE_URL", "\\"https:\/\/api\.goatos\.mesha\.sg\/\\""/);
  assert.match(prodBlock, /AUTH_ACTION_CONTINUE_URL", "\\"https:\/\/dashboard\.mesha\.sg\/login\\""/);
  assert.doesNotMatch(prodBlock, /stg-api\.dashboard\.mesha\.sg|stg\.dashboard\.mesha\.sg/);
});

test("prod Firebase config contains the production-facing package client", () => {
  assert.equal(googleServices.project_info?.project_id, "goatos-stg");

  const prodClient = googleServices.client?.find(
    (client) => client.client_info?.android_client_info?.package_name === "sg.mesha.goatos",
  );

  assert.ok(prodClient, "expected Firebase Android client for sg.mesha.goatos");
  assert.equal(prodClient.client_info?.mobilesdk_app_id, "1:514832198871:android:2b3a80736ff2e8d9f19492");
});
