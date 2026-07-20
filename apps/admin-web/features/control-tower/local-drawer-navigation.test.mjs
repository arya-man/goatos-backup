import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./index.tsx", import.meta.url), "utf8");
const drawerSource = readFileSync(new URL("./control-tower-local-drawer.tsx", import.meta.url), "utf8");

test("control-tower record drawers open locally without a route/RSC navigation", () => {
  assert.match(pageSource, /LocalOverlayLink/);
  assert.match(pageSource, /#ct_alert=/);
  assert.match(pageSource, /ControlTowerLocalDrawer/);
  assert.match(drawerSource, /useLocalOverlaySelection/);
  assert.match(drawerSource, /selectionKey: "ct_alert"/);
  assert.match(drawerSource, /className=\{`scrim\$\{drawerOpen \? " on" : ""\}`\}/);
  assert.doesNotMatch(drawerSource, /<Link[^>]+className="veil"/);
});
