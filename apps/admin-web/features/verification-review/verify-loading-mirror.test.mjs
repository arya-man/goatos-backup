import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// guard: verify-loading-mirror (SK1). /verify loading.tsx is the page (InvoiceListView): header with the
// panel buttons, the status strip card, then the board card (status tabs, module + capture date toolbar,
// table). The strip's UrlSuspense fallback is the same StatStripSkeleton with no extra margin box (the
// page grid spaces it; an `mb` wrapper made the board jump when the strip landed).
const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

test("verify loading mirrors the page blocks", () => {
  const loading = read("../../app/(admin)/verify/loading.tsx");
  const page = read("./verification-review-page.tsx");
  assert.match(loading, /<StatStripSkeleton count=\{STATUS_STRIP_CELLS\} \/>/);
  assert.match(loading, /tabs=\{<TabsSkeleton count=\{STATUS_TABS\} counts \/>\}/);
  assert.match(loading, /fields=\{BOARD_TOOLBAR_FIELDS\}/);
  assert.match(loading, /actionWidths=\{HEADER_ACTION_WIDTHS\}/);
  assert.match(page, /fallback=\{<StatStripSkeleton count=\{STATUS_STRIP_CELLS\} \/>\}/);
  assert.doesNotMatch(page, /mb: \{ xs: 3, md: 5 \} \}\}><StatStripSkeleton/);
});
