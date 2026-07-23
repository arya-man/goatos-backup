import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./herd-register.tsx", import.meta.url), "utf8");
const drawerSource = readFileSync(new URL("./herd-passport-local-drawer.tsx", import.meta.url), "utf8");

test("Herd passport previews open and close locally without route navigation", () => {
  assert.match(pageSource, /LocalOverlayLink/);
  assert.match(pageSource, /HerdPassportLocalDrawer/);
  assert.match(drawerSource, /useLocalOverlaySelection/);
  assert.doesNotMatch(drawerSource, /<Link[^>]+className="veil"/);
  assert.match(drawerSource, /href=\{`\/goats\/\$\{encodeURIComponent\(item\.goatId\)\}`\}/);
  assert.match(drawerSource, /<HerdReproductiveEdit/);
});

test("Herd passport drawer includes goat-wise vaccination passport history", () => {
  assert.match(drawerSource, /\/api\/goats\/\$\{encodeURIComponent\(goatId\)\}\/vaccination-passport/);
  assert.match(drawerSource, /vaccination_history/);
  assert.match(drawerSource, /open_obligations/);
  assert.match(drawerSource, /HerdDrawerVaccinationBlock/);
});
