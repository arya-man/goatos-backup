import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// guard: adherence-loading-mirror (SK1). /protocol-adherence loading.tsx is the page: the info "i" as the
// header's only action (wrapping under the crumb row on a phone), four KpiWidget tiles with captions on
// the page's Grid size, then the ledger card with the page's CardHeader margin and toolbar.
const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

test("protocol adherence loading mirrors the page", () => {
  const loading = read("../../app/(admin)/protocol-adherence/loading.tsx");
  const page = read("./protocol-adherence.tsx");
  assert.match(loading, /<KpiRowSkeleton count=\{4\} hint size=\{ADHERENCE_TILE_SIZE\} \/>/);
  assert.match(page, /fallback=\{<KpiRowSkeleton count=\{4\} hint size=\{ADHERENCE_TILE_SIZE\} \/>\}/);
  assert.match(page, /<Grid key=\{kpi\.key\} size=\{ADHERENCE_TILE_SIZE\}>/);
  assert.match(page, /sx=\{ADHERENCE_LEDGER_HEADER_SX\}/);
  assert.match(loading, /headerSx=\{ADHERENCE_LEDGER_HEADER_SX\}/);
  assert.match(page, /minWidth=\{ADHERENCE_SEVERITY_WIDTH\}/);
});
