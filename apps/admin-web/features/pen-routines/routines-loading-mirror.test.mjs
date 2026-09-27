import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// guard: routines-loading-mirror (SK1). /routines' loading.tsx is the page block for block: the Today
// tiles on the page's own Grid size (rendered at zero too, so the skeleton tile row never lands on a
// page without one), the routines card and the tasks card with the page's toolbar fields and its
// always-on day chip strip.
const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

test("routines loading mirrors the page blocks and layout constants", () => {
  const loading = read("../../app/(admin)/routines/loading.tsx");
  const page = read("./routines-page.tsx");
  assert.match(loading, /<KpiRowSkeleton count=\{5\} size=\{ROUTINE_TILE_SIZE\} \/>/);
  assert.match(loading, /fields=\{ROUTINES_TOOLBAR_FIELDS\}/);
  assert.match(loading, /inCard summary fields=\{TASKS_TOOLBAR_FIELDS\}/);
  assert.match(page, /<Grid key=\{tile\.key\} size=\{ROUTINE_TILE_SIZE\}>/);
  assert.match(page, /fallback=\{<KpiRowSkeleton count=\{5\} size=\{ROUTINE_TILE_SIZE\} \/>\}/);
  assert.doesNotMatch(page, /summaryTiles\.some\(/, "the Today tiles must not hide at all-zero");
});
