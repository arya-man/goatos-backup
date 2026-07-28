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

test("control-tower drawer keeps workflow IDs out of the evidence cell", () => {
  assert.match(drawerSource, /function evidenceSummary/);
  assert.match(drawerSource, /\{evidenceSummary\(alert, pageContract\)\}/);
  assert.doesNotMatch(drawerSource, /\{alert\.evidence_link \|\| copy\(pageContract, "label\.not_ready"\)\}/);
});

test("control-tower drawer renders backend-owned scope and evidence fields", () => {
  assert.match(drawerSource, /alert\.scope_label/);
  assert.match(drawerSource, /alert\.evidence_summary\?\.trim\(\)/);
  assert.match(drawerSource, /alert\.proof_summary\?\.trim\(\)/);
  assert.doesNotMatch(drawerSource, /detail\.split/);
  assert.doesNotMatch(drawerSource, /evidence\.\$\{alert\.work_state\}/);
});
