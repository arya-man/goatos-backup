import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { legacyCss } from "../../../scripts/lib/legacy-css.mjs";

// guard: skeleton-busy-nowrap (SK2, root cause SK1). frame.css used to force `flex-wrap: wrap; min-width: 0`
// on every div of a busy `.screen` skeleton; the loaded rows never wrap, so blocks fought it with `&&&`
// (a wrapping Stack sized children at max-content, the InvoiceAnalytic strip stacked ring-over-text,
// the /verify strip ran 256px vs 108px at 390). The rule is deleted: blocks carry plain sx again, and
// the rule must not come back.
const blocks = readFileSync(new URL("./blocks.tsx", import.meta.url), "utf8");
const body = (name) => {
  const start = blocks.indexOf(`export function ${name}(`);
  assert.ok(start >= 0, name);
  return blocks.slice(start, blocks.indexOf("\nexport function ", start + 10));
};

test("no busy wrap rule, no outranking workarounds", () => {
  const frame = legacyCss("frame");
  assert.doesNotMatch(frame, /\.screen\[aria-busy="true"\]\s+div\s*\{[^}]*flex-wrap/);
  for (const name of ["StackSkeleton", "TabsSkeleton", "OrderToolbarSkeleton", "StatStripSkeleton"]) {
    assert.doesNotMatch(body(name), /"&&&?[^"]*": \{[^}]*flexWrap: "nowrap"/, `${name}: stale busy-wrap workaround`);
  }
  assert.match(body("StatStripSkeleton"), /minWidth: 200/);
});

test("the order toolbar twin sizes its selects and search from the page's own sx", () => {
  const toolbar = body("OrderToolbarSkeleton");
  assert.match(toolbar, /\.\.\.orderToolbarFilterSx/);
  assert.match(toolbar, /\.\.\.orderToolbarSearchSx/);
});

test("operations and feed purchases route skeletons use the order toolbar twin and the page's strip", () => {
  const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");
  assert.match(read("../../../app/(admin)/operations/audit/loading.tsx"), /<OrderToolbarSkeleton /);
  assert.match(read("../../../app/(admin)/operations/dlq/loading.tsx"), /<OrderToolbarSkeleton /);
  assert.match(read("../../../features/operations-audit/audit-log.tsx"), /fallback=\{<StatStripSkeleton count=\{AUDIT_STRIP_CELLS\} meta wrapBelowMd \/>\}/);
  assert.match(read("../../../app/(admin)/operations/audit/loading.tsx"), /<StatStripSkeleton count=\{AUDIT_STRIP_CELLS\} meta wrapBelowMd \/>/);
  assert.match(read("../../../features/procurement/feed-purchases.tsx"), /fallback=\{<FeedPurchasesStripSkeleton \/>\}/);
  assert.match(read("../../../app/(admin)/procurement/feed-purchases/loading.tsx"), /<FeedPurchasesSkeleton \/>/);
});
