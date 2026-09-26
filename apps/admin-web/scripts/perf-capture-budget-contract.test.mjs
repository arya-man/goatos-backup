import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const lighthouse = readFileSync(new URL("./capture-lighthouse.mjs", import.meta.url), "utf8");
const pagespeed = readFileSync(new URL("./capture-pagespeed.mjs", import.meta.url), "utf8");
const localCi = readFileSync(new URL("../../../tools/ci/run-local-ci.sh", import.meta.url), "utf8");

test("Lighthouse capture fails when category scores miss the configured budget", () => {
  assert.match(lighthouse, /ADMIN_WEB_LIGHTHOUSE_MIN_SCORES/);
  assert.match(lighthouse, /performance=70,accessibility=90,best-practices=90,seo=80/);
  assert.match(lighthouse, /Lighthouse scores below budget/);
  assert.match(lighthouse, /process\.exit\(1\)/);
});

test("PageSpeed capture fails when category scores miss the configured budget", () => {
  assert.match(pagespeed, /PAGESPEED_MIN_SCORES/);
  assert.match(pagespeed, /performance=70,accessibility=90,best-practices=90,seo=80/);
  assert.match(pagespeed, /PageSpeed scores below budget/);
  assert.match(pagespeed, /process\.exit\(1\)/);
});

test("local admin-web CI runs live route drawers and Lighthouse when a live app is supplied", () => {
  assert.match(localCi, /GOATOS_ADMIN_WEB_BASE_URL/);
  assert.match(localCi, /visual:routes:drawers/);
  assert.match(localCi, /ADMIN_WEB_LIGHTHOUSE_URL/);
  assert.match(localCi, /perf:lighthouse/);
});
