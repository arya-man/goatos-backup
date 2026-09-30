import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { fileURLToPath } from "node:url";

function source(relativePath) {
  return readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), "utf8");
}

test("vaccination list-backed drawers use local history instead of route refreshes", () => {
  const cohort = source("./cohort-detail.tsx");
  const matrix = source("./status-matrix.tsx");
  const warmup = source("./supplier-warmup-context.tsx");
  const execution = source("../vaccination-execution/execution-board.tsx");

  assert.match(cohort, /LocalOverlayLink/);
  assert.match(cohort, /VaccinationRecordVerifyLocalDrawer/);
  assert.match(matrix, /LocalOverlayLink/);
  assert.match(matrix, /VaccinationRecordVerifyLocalDrawer/);
  assert.match(warmup, /LocalOverlayLink/);
  assert.match(warmup, /<LocalOverlayDrawer/);
  assert.match(execution, /LocalOverlayLink/);
  assert.match(execution, /<LocalOverlayDrawer/);

  for (const text of [cohort, matrix, warmup, execution]) {
    assert.doesNotMatch(text, /<Link\b[^>]*href=\{(?:drawerHref|href)\}[^>]*className="celllink"/s);
    assert.doesNotMatch(text, /className="veil"/);
  }
});

test("shared local overlay lifecycle covers history, Escape, outside click, focus, and animation", () => {
  const navigation = source("../../components/local-overlay-link.tsx");
  const drawer = source("../../components/local-overlay-drawer.tsx");

  assert.match(navigation, /window\.history\.pushState/);
  assert.match(navigation, /window\.history\.back\(\)/);
  assert.match(navigation, /event\.key !== "Escape"/);
  assert.match(navigation, /previousFocusRef\.current\?\.focus\(\)/);
  // Template MinimalDrawer (portal + backdrop): X, backdrop and Escape all land on closeDrawer.
  assert.match(drawer, /<DetailDrawer/);
  assert.match(drawer, /open=\{drawerOpen\}/);
  assert.match(drawer, /onClose=\{closeDrawer\}/);
  assert.match(navigation, /if \(!openRef\.current\) return;/);
});
