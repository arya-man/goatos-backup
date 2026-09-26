// Source contract for three OCI production browser-smoke mobile failures (smoke-visual-live.mjs):
// health-config treatment tables need a registered horizontal scroll owner, notification-matrix
// checkboxes need a >=40px label hit area, and breadcrumb links need >=40px tap height on mobile.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");
const css = read("../app/mesha-theme.css");

test("health-config feed tables sit inside the shared mobile scroll owner", () => {
  const src = read("./health/health-config.tsx");
  const wrappers = src.match(/className="bd[^"]*"[\s\S]{0,200}?<Table className="feed-table"/g) ?? [];
  assert.equal(wrappers.length, 2);
  for (const w of wrappers) assert.match(w, /className="bd[^"]*\btablewrap feed-stock-tablewrap feed-scroll\b/);
  assert.match(css, /\.main \.feed-stock-tablewrap\{[^}]*overflow-x:auto[^}]*touch-action:pan-x pan-y/);
});

test("notification-matrix checkboxes are wrapped in a 40px label hit area", () => {
  const src = read("./people/notification-matrix.tsx");
  assert.match(src, /<Checkbox className="nmatrix-hit"/);
  assert.match(css, /\.nmatrix-hit\{[^}]*min-width:40px;min-height:40px/);
});

test("page-header breadcrumb and back links get a 44px tap box on phone widths", () => {
  // The page header is the template CustomBreadcrumbs; PhoneTapStyles raises its links below `md`.
  const tap = read("../components/minimal/_shared/phone-tap-styles.tsx");
  assert.match(tap, /'\.MuiBreadcrumbs-li > a, a\.minimal__breadcrumbs__back': \{\s*minHeight: TAP,\s*minWidth: TAP/);
  assert.match(tap, /const TAP = 44;/);
});
