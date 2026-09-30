// Source contract for three OCI production browser-smoke mobile failures (smoke-visual-live.mjs):
// health-config treatment tables need a registered horizontal scroll owner, notification-matrix
// checkboxes need a >=40px label hit area, and breadcrumb links need >=40px tap height on mobile.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");
const css = read("../app/mesha-theme.css");

test("health-config tables scroll inside their own template TableContainer", () => {
  // Template table anatomy: MUI TableContainer (overflow-x auto) owns the sideways scroll, so a
  // wide protocol table never scrolls the page on a phone.
  const src = read("./health/health-config.tsx");
  const wrappers = src.match(/<TableContainer[\s\S]{0,200}?<Table sx=\{\{ minWidth: \d+(?:, \.\.\.STICKY_FIRST_COLUMN_SX)? \}\}/g) ?? [];
  assert.equal(wrappers.length, 2);
});

test("notification-matrix checkboxes carry a 44px hit area", () => {
  // Template Checkbox with a tap-min box in sx (the legacy .nmatrix-hit rule is gone, FIXJ2).
  const src = read("./people/notification-matrix.tsx");
  assert.match(src, /const CHECKBOX_HIT_SX = \{.*minWidth: "var\(--tap-min\)", minHeight: "var\(--tap-min\)"/);
  assert.equal((src.match(/sx=\{CHECKBOX_HIT_SX\}/g) ?? []).length, 2, "table and phone-card checkboxes both use the hit box");
});

test("page-header breadcrumb and back links get a 44px tap box on phone widths", () => {
  // The page header is the template CustomBreadcrumbs; PhoneTapStyles raises its links below `md`.
  const tap = read("../components/app/phone-tap-styles.tsx");
  assert.match(tap, /'\.MuiBreadcrumbs-li > a, a\.minimal__breadcrumbs__back': \{\s*minHeight: TAP,\s*minWidth: TAP/);
  assert.match(tap, /const TAP = 44;/);
});
