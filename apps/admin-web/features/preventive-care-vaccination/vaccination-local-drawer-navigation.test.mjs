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

test("shed passport drawer opens locally and fetches detail without re-rendering the route", () => {
  const page = source("../vaccination-sheds/shed-detail.tsx");
  const drawer = source("../vaccination-sheds/shed-passport-local-drawer.tsx");
  const route = source("../../app/api/goats/[goat_id]/passport/route.ts");

  assert.match(page, /<LocalOverlayLink/);
  assert.match(page, /<ShedPassportLocalDrawer/);
  assert.doesNotMatch(page, /selectedGoatId \? getGoatPassport/);
  assert.match(drawer, /useLocalOverlaySelection/);
  assert.match(drawer, /fetch\(`\/api\/goats\/\$\{encodeURIComponent\(goatId\)\}\/passport`/);
  assert.match(drawer, /fetch\(`\/api\/goats\/\$\{encodeURIComponent\(goatId\)\}\/vaccination-passport`/);
  assert.match(drawer, /DrawerVaccinationBlock/);
  assert.match(drawer, /vaccination_history/);
  assert.match(route, /UUID_RE\.test\(goat_id\)/);
  assert.match(route, /getGoatPassport\(goat_id\)/);
  assert.match(route, /"Cache-Control": "no-store"/);

  const vaccinationRoute = source("../../app/api/goats/[goat_id]/vaccination-passport/route.ts");
  assert.match(vaccinationRoute, /UUID_RE\.test\(goat_id\)/);
  assert.match(vaccinationRoute, /getGoatVaccinationPassport\(goat_id\)/);
  assert.match(vaccinationRoute, /"Cache-Control": "no-store"/);
});

test("shared local overlay lifecycle covers history, Escape, outside click, focus, and animation", () => {
  const navigation = source("../../components/local-overlay-link.tsx");
  const drawer = source("../../components/local-overlay-drawer.tsx");

  assert.match(navigation, /window\.history\.pushState/);
  assert.match(navigation, /window\.history\.back\(\)/);
  assert.match(navigation, /event\.key !== "Escape"/);
  assert.match(navigation, /previousFocusRef\.current\?\.focus\(\)/);
  assert.match(drawer, /onClick=\{closeDrawer\}/);
  assert.match(drawer, /className=\{`drawer\$\{drawerOpen \? " on" : ""\}`\}/);
});
