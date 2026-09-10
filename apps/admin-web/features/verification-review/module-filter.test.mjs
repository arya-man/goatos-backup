import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import test from "node:test";

const pageSource = readFileSync(new URL("./verification-review-page.tsx", import.meta.url), "utf8");
const moduleSource = readFileSync(new URL("./module-filter.tsx", import.meta.url), "utf8");
const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");

test("module chips use a pending-aware client component", () => {
  assert.match(pageSource, /import \{ ModuleFilter \} from "\.\/module-filter";/);
  assert.match(pageSource, /<ModuleFilter[\s\S]*allHref=\{hrefWith\(sp, \{ nav_module: null, category: null, \.\.\.RESET_ON_FILTER \}\)\}/);
  assert.doesNotMatch(pageSource, /<div className="vr-legend" role="group" aria-label=\{copy\(pageContract, "filter\.module"\)\}>/);
});

test("module chip navigation gives immediate pending feedback without prefetch", () => {
  assert.match(moduleSource, /"use client";/);
  assert.match(moduleSource, /useRouter\(\)/);
  assert.match(moduleSource, /document\.body\.dataset\.vrModuleLoading = "1"/);
  assert.match(moduleSource, /setOptimistic\(\{ key, navKey \}\)/);
  assert.match(moduleSource, /router\.replace\(href, \{ scroll: false \}\)/);
  assert.match(moduleSource, /aria-busy=\{busy\}/);
});

test("parent module navigation masks stale result rows while backend results load", () => {
  assert.match(pageSource, /className="vr-results-zone"/);
  assert.match(css, /body\[data-vr-module-loading="1"\] \.vr-results-loading\{display:block\}/);
  assert.match(css, /\.vr-board:has\(\.vr-module-legend\.busy\) \.vr-results-loading\{display:block\}/);
  assert.match(css, /@keyframes vr-loading-sweep/);
});
