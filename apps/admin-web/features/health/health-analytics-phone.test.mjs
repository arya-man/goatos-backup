import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// /health/analytics at phone width (PR #294 O9 / O16).
const source = readFileSync(new URL("./health-analytics.tsx", import.meta.url), "utf8");

test("the six-tab strip carries the template scroll arrows so Mortality / Diagnosis engine are reachable", () => {
  const strip = source.slice(source.indexOf("<TemplateTabs"), source.indexOf("items={TABS.map("));
  assert.match(strip, /scrollButtons="auto"/);
});

test("chart cards halve their gutter on a phone; no fixed 24px chart box remains", () => {
  assert.match(source, /const CHART_BOX_SX = \{ p: \{ xs: 1\.5, sm: 3 \} \} as const;/);
  assert.doesNotMatch(source, /<Box sx=\{\{ p: 3 \}\}>/);
});

test("an all-parks death list names the park beside each pen", () => {
  assert.match(source, /ageBands,\s*fmtDate,\s*!parkId,\s*\);/);
});

test("the health config pager names what it counts and drops the lone (i) (PR #294 O17)", () => {
  const config = readFileSync(new URL("./health-config.tsx", import.meta.url), "utf8");
  assert.match(config, /copy\(pageContract, "pager\.noun_plural"\)/);
  assert.doesNotMatch(config, /pager\.rows_note/);
});
