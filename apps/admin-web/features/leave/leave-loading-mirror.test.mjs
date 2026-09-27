import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// guard: leave-loading-mirror (SK1). /leave's loading.tsx is the page block for block: the status
// tiles on the page's own Grid size, the "who approves" settings card, the queue card and the list
// card with the page's toolbar fields. The tile row renders at zero too (an empty-but-OK window keeps
// its section), so the skeleton's tile row never lands on a page without one (IoU 0.47 before).
const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

test("leave loading mirrors the page blocks and layout constants", () => {
  const loading = read("../../app/(admin)/leave/loading.tsx");
  const page = read("./leave-page.tsx");
  assert.match(loading, /<KpiRowSkeleton count=\{4\} size=\{LEAVE_TILE_SIZE\} \/>/);
  assert.match(loading, /<FormCardSkeleton /);
  assert.match(loading, /fields=\{LEAVE_TOOLBAR_FIELDS\}/);
  assert.match(page, /<Grid key=\{key\} size=\{LEAVE_TILE_SIZE\}>/);
  assert.match(page, /fallback=\{<KpiRowSkeleton count=\{4\} size=\{LEAVE_TILE_SIZE\} \/>\}/);
  assert.doesNotMatch(page, /statusCounts\)\.some\(/, "the tile row must not hide at all-zero");
});

test("the leave toolbar and its skeleton read one select width (REVIEW-45 O73)", () => {
  const toolbar = read("./leave-toolbar.tsx");
  assert.doesNotMatch(toolbar, /sm: 160\b/);
  assert.equal((toolbar.match(/minWidth: \{ xs: 0, sm: LEAVE_SELECT_WIDTH \}/g) ?? []).length, 2);
  assert.match(read("./leave-layout.ts"), /LEAVE_TOOLBAR_FIELDS[^=]*= \[LEAVE_SELECT_WIDTH, LEAVE_SELECT_WIDTH,/);
  const routines = read("../pen-routines/routines-chrome.tsx");
  assert.doesNotMatch(routines, /sm: 160\b/);
  assert.match(routines, /minWidth: \{ xs: 0, sm: ROUTINES_SELECT_WIDTH \}/);
});
