import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import test from "node:test";

const pageSource = readFileSync(new URL("./verification-review-page.tsx", import.meta.url), "utf8");
const moduleSource = readFileSync(new URL("./module-filter.tsx", import.meta.url), "utf8");

test("module chips use a pending-aware client component", () => {
  assert.match(pageSource, /import \{ ModuleFilter \} from "\.\/module-filter";/);
  assert.match(pageSource, /<ModuleFilter[\s\S]*allHref=\{hrefWith\(sp, \{ nav_module: null, category: null, \.\.\.RESET_ON_FILTER \}\)\}/);
  assert.doesNotMatch(pageSource, /<div className="vr-legend" role="group" aria-label=\{copy\(pageContract, "filter\.module"\)\}>/);
});

test("module chip navigation gives immediate pending feedback without prefetch", () => {
  assert.match(moduleSource, /"use client";/);
  assert.match(moduleSource, /useRouter\(\)/);
  assert.match(moduleSource, /setOptimistic\(\{ key, navKey \}\)/);
  assert.match(moduleSource, /router\.replace\(href, \{ scroll: false \}\)/);
  assert.match(moduleSource, /aria-busy=\{busy\}/);
});

// The stale rows give way to the shared TableSkeleton: the queue table sits in UrlSuspense keyed by
// the module params, so no hand-drawn loading veil (the retired .vr-results-loading overlay and its
// body[data-vr-module-loading] flag) paints over them (FIXJ3 legacy-free-zone).
test("parent module navigation masks stale result rows while backend results load", () => {
  assert.match(pageSource, /const QUEUE_WATCH = \[[^\]]*"category", "nav_module"/);
  assert.match(pageSource, /<UrlSuspense searchParams=\{sp\} watch=\{QUEUE_WATCH\} fallback=\{<TableSkeleton/);
  assert.doesNotMatch(pageSource, /vr-results-loading/);
  assert.doesNotMatch(moduleSource, /vrModuleLoading/);
});
