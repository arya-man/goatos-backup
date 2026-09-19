// Source-shape contract tests for the Assumptions drawer (maintainer decision 2026-09-19), in the
// shape of fcr-tab.contract.test.mjs: every copy key is backend-authored (including the per-key
// labels composed from an assumption key), the drawer renders no literal copy of its own, the
// button is gated on the page contract's `edit_assumptions` control and HIDDEN rather than greyed
// when it is off, and the sale-ready line reaches the weighing count as a parameter on every page
// that shows an "Over N kg" figure.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const drawerSource = readFileSync(join(here, "weights-assumptions.tsx"), "utf8");
const pageSource = readFileSync(join(here, "weights-analytics.tsx"), "utf8");
const sopPageSource = readFileSync(join(here, "..", "..", "app", "(admin)", "weighing", "sops", "page.tsx"), "utf8");
const farmValueSource = readFileSync(join(here, "..", "procurement", "sales-farm-value.tsx"), "utf8");
const weightsSource = readFileSync(join(here, "weights.tsx"), "utf8");
const serviceSource = readFileSync(
  join(here, "..", "..", "..", "..", "backend", "internal", "adminui", "app", "service.go"),
  "utf8",
);

test("every copy key the Assumptions drawer uses is authored by the backend page contract", () => {
  // The drawer's copy lives on the weighing-sops contract: the shared SOP map plus the
  // `case "weighing-sops":` assignments. Keys authored on ANY OTHER page do not count -- the first
  // cut borrowed unit.fcr.rupees from ADG Analytics and took the SOP page down with a missing-key
  // throw, which this scoping would have caught.
  const sopCase = serviceSource.search(/case "counts-sops", "feed-sops", "milk-sops", "weighing-sops"[^\n]*:\n\t\tm := map\[string\]string\{/);
  const sopEnd = serviceSource.indexOf("\n\t\treturn m\n", sopCase);
  assert.ok(sopCase > 0 && sopEnd > sopCase, "the SOP copy block must exist");
  const sopBlock = serviceSource.slice(sopCase, sopEnd);
  const backendKeys = new Set([
    ...[...sopBlock.matchAll(/"([a-z_]+(?:\.[a-z_]+)+)":/g)].map((m) => m[1]),
    ...[...sopBlock.matchAll(/m\["([a-z_]+(?:\.[a-z_]+)+)"\] = /g)].map((m) => m[1]),
  ]);
  const used = new Set();
  for (const m of drawerSource.matchAll(/copy\(pageContract, "([^"$`]+)"\)/g)) used.add(m[1]);
  for (const m of drawerSource.matchAll(/copy\(pageContract, section\.title\)/g)) void m;
  for (const title of ["drawer.assumptions.values.title", "drawer.assumptions.growth.title", "drawer.assumptions.window.title"]) used.add(title);
  // Keys composed from a species or an assumption key expand over the catalog.
  for (const species of ["goat", "sheep"]) used.add(`assumption.${species}.label`);
  for (const key of [
    "sale_ready_threshold_kg", "sale_ready_lower_kg", "load_age_alert_days", "slow_growth_target_g_per_day",
    "bad_scan_loss_g_per_day", "default_period_days", "weight_band_edges_kg",
  ]) {
    used.add(`assumption.${key}.label`);
    used.add(`assumption.${key}.hint`);
  }
  const missing = [...used].filter((key) => !backendKeys.has(key));
  assert.deepEqual(missing, [], "drawer keys that service.go does not author");
});

test("the Assumptions button lives on the Weighing SOP page, gated on edit_assumptions and hidden when off", () => {
  assert.match(sopPageSource, /if \(!controlEnabled\(pageContract, "edit_assumptions", false\)\) return null;/, "the button renders only when the control is enabled");
  assert.doesNotMatch(sopPageSource, /disabled=/, "an off control is absent, never a greyed button");
  assert.doesNotMatch(pageSource, /WeightsAssumptionsControl/, "ADG Analytics no longer carries the button (moved 2026-09-19)");
  assert.match(serviceSource, /m\["action\.assumptions"\] = "Assumptions"/);
});

test("the sale-ready line reaches the weighing count as a parameter on every page that shows it", () => {
  assert.match(pageSource, /sale_threshold_kg: saleThresholdKg/, "ADG Analytics passes the assumption to shed-weights");
  assert.match(farmValueSource, /sale_threshold_kg: saleThresholdKg/, "Farm value passes the assumption to shed-weights");
  // /weighing/weights is the same estate one screen over (PR #320 review finding): it must pass
  // the same lines and edges, or the two Weights pages count "over N kg" at different lines.
  assert.match(weightsSource, /sale_threshold_kg: saleThresholdKg/, "Weights passes the sale line to shed-weights");
  assert.match(weightsSource, /sale_lower_kg: saleLowerKg/, "Weights passes the lower line to shed-weights");
  assert.match(weightsSource, /band_edges_kg: bandEdgesParam\(assumptionRows\)/, "Weights passes the band edges to demographics");
  assert.match(weightsSource, /fillKg\(copy\(pageContract, "kpi\.over35\.label"\), saleThresholdKg\)/);
  assert.match(weightsSource, /fillKg\(copy\(pageContract, "kpi\.over30\.label"\), saleLowerKg \?\? DEFAULT_SALE_READY_LOWER_KG\)/);
  // The label is filled from the SAME value the count used; a literal 35 must not survive.
  assert.match(pageSource, /fillKg\(copy\(pageContract, "kpi\.over35\.label"\), saleThresholdKg\)/);
  assert.match(farmValueSource, /fillKg\(copy\(pageContract, "kpi\.over35"\), over35\.lineKg\)/);
  assert.doesNotMatch(farmValueSource, /Math\.max\(0, 35 - /, "the cut-off line must be computed from the assumption, not a literal 35");
  assert.match(serviceSource, /"kpi\.over35":\s+"Over \{kg\} kg"/, "the label carries the {kg} placeholder");
});

test("a failed assumptions read is a page failure, never a silent fallback to the constants", () => {
  // PR #320 review: only the AUTH failure redirects; any other failure must not render counts at
  // the constants' lines as if valid.
  for (const [name, src] of [["ADG Analytics", pageSource], ["Weights", weightsSource]]) {
    assert.match(src, /if \(!assumptions\.ok\) \{/, `${name} must fail the page on a non-auth assumptions error`);
    assert.doesNotMatch(src, /assumptions\.ok \? assumptions\.data\.values : null/, `${name} must not fall back to null rows`);
  }
  assert.match(farmValueSource, /over35Enabled && !assumptionsFailed \? getShedWeights/, "Farm value must not count at the default line after a failed read");
  assert.match(farmValueSource, /enabled: over35Enabled && !assumptionsFailed/, "Farm value must disable the tuning control when the assumptions read failed");
});

test("a sale price save carries the price the drawer loaded (the fence)", () => {
  assert.match(drawerSource, /loaded_price_per_kg_inr: loaded/, "the loaded price must travel with the new one");
  assert.match(drawerSource, /current\.sale_prices\.find\(\(row\) => row\.species === species\)\?\.price_per_kg_inr \?\? null/);
});

test("the drawer has no literal visible copy", () => {
  const jsxText = [...drawerSource.matchAll(/>\s*([A-Za-z][A-Za-z ,.'-]{3,})\s*</g)].map((m) => m[1].trim());
  assert.deepEqual(jsxText, [], "visible text must come from copy()");
});
