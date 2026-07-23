import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./protocol-adherence.tsx", import.meta.url), "utf8");
const drawerSource = readFileSync(new URL("./protocol-adherence-local-drawer.tsx", import.meta.url), "utf8");

test("protocol-adherence record drawers open locally without a route/RSC navigation", () => {
  assert.match(pageSource, /LocalOverlayLink/);
  assert.match(pageSource, /#adh_row=/);
  assert.match(pageSource, /ProtocolAdherenceLocalDrawer/);
  assert.match(drawerSource, /useLocalOverlaySelection/);
  assert.match(drawerSource, /selectionKey: "adh_row"/);
  assert.match(drawerSource, /className=\{`scrim\$\{drawerOpen \? " on" : ""\}`\}/);
	assert.doesNotMatch(drawerSource, /<Link[^>]+className="veil"/);
});

test("process-integrity vaccination rows share the vaccination display mapper", () => {
	assert.match(pageSource, /vaccinationDriveDisplayName/);
	assert.match(pageSource, /readableAdherenceExpected[\s\S]+vaccinationDriveDisplayName/);
});
