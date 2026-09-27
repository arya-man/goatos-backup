import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: skeleton-busy-nowrap (SK2). frame.css `.screen[aria-busy="true"] div { flex-wrap: wrap;
// min-width: 0 }` reaches every div of a route skeleton. The loaded rows never wrap, so every shared
// block that is a flex row / column pins itself with a selector that outranks it (`&&&`, or `&&` plus
// a class): a wrapping Stack sized its children at max-content (a 456px column on a 390 phone), the
// InvoiceAnalytic strip stacked its 200px cells ring-over-text, the order toolbar broke its row.
const blocks = readFileSync(new URL("./blocks.tsx", import.meta.url), "utf8");
const body = (name) => {
  const start = blocks.indexOf(`export function ${name}(`);
  assert.ok(start >= 0, name);
  return blocks.slice(start, blocks.indexOf("\nexport function ", start + 10));
};

test("flex-row / flex-column blocks outrank the busy wrap rule", () => {
  assert.match(body("StackSkeleton"), /"&&&": \{ flexWrap: "nowrap" \}/);
  assert.match(body("TabsSkeleton"), /"&& \.MuiTabs-list": \{ flexWrap: "nowrap" \}/);
  assert.match(body("OrderToolbarSkeleton"), /"&&&": \{ flexWrap: "nowrap" \}/);
  const strip = body("StatStripSkeleton");
  assert.match(strip, /"&&&": \{ minWidth: 200, flexWrap: "nowrap" \}/);
  assert.match(strip, /sx=\{\{ py: 2, "&&&": \{ flexWrap: "nowrap" \} \}\}/);
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
