import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import test from "node:test";

const source = readFileSync(new URL("./faro-provider.tsx", import.meta.url), "utf8");
const require = createRequire(import.meta.url);
const faroCoreRoot = dirname(require.resolve("@grafana/faro-core/package.json"));
const faroCore = readFileSync(join(faroCoreRoot, "dist/esm/sdk/registerFaro.js"), "utf8");

test("faro-core pre-init export has a truthy noop api (why faro.api is not an init guard)", () => {
  assert.match(faroCore, /export let faro = \{ api: getNoopAPI\(\) \}/);
});

test("FaroProvider guards initialization on faro.config, never on faro.api", () => {
  assert.doesNotMatch(source, /\|\|\s*faro\.api\)/);
  assert.doesNotMatch(source, /if \(!faro\.api\)/);
  assert.match(source, /\(faro as \{ config\?: unknown \}\)\.config/);
  assert.match(source, /initializeFaro\(/);
});

test("root provider initializes Firebase Performance on every route", () => {
  assert.match(source, /preloadFirebasePerformance\(\)/);
});
