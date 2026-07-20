import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./herd-register.tsx", import.meta.url), "utf8");
const drawerSource = readFileSync(new URL("./herd-passport-local-drawer.tsx", import.meta.url), "utf8");

test("Herd passport previews open and close locally without route navigation", () => {
  assert.match(pageSource, /LocalOverlayLink/);
  assert.match(pageSource, /HerdPassportLocalDrawer/);
  assert.match(drawerSource, /LOCAL_OVERLAY_URL_CHANGE_EVENT/);
  assert.match(drawerSource, /popstate/);
  assert.match(drawerSource, /currentHistoryEntryIsLocalOverlay/);
  assert.doesNotMatch(drawerSource, /<Link[^>]+className="veil"/);
  assert.match(drawerSource, /href=\{`\/goats\/\$\{encodeURIComponent\(item\.goatId\)\}`\}/);
  assert.match(drawerSource, /<HerdReproductiveEdit/);
});
