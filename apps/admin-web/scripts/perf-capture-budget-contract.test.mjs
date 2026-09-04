import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const lighthouse = readFileSync(new URL("./capture-lighthouse.mjs", import.meta.url), "utf8");
const pagespeed = readFileSync(new URL("./capture-pagespeed.mjs", import.meta.url), "utf8");

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
