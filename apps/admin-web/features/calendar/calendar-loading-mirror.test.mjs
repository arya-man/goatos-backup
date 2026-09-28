import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// guard: calendar-loading-mirror (SK1). /calendar is the template calendar view: a heading with no crumb
// row and no action, then ONE calendar card (toolbar + month grid). The old loading drew a heading
// toggle, a chip row and a tab row that TR1-#25 removed from the page.
const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

test("calendar loading is the heading and the calendar card", () => {
  const loading = read("../../app/(admin)/calendar/(index)/loading.tsx");
  assert.match(loading, /<PageHeaderSkeleton crumbs=\{false\}/);
  assert.match(loading, /<BlockSkeleton height=\{CALENDAR_CARD_HEIGHT\} \/>/);
  assert.doesNotMatch(loading, /ChipRowSkeleton|TabsSkeleton|actionWidths/);
});
