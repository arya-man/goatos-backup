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

test("protocol-adherence tolerates stale work-state values from staging data", () => {
	assert.match(pageSource, /function workStateTone[\s\S]+optionalOption\(pageContract, "work_state_filter_chips", workState\)\?\.tone \?\? "mut"/);
	assert.match(drawerSource, /function workStateTone[\s\S]+optionalOption\(pageContract, "work_state_filter_chips", workState\)\?\.tone \?\? "mut"/);
	assert.doesNotMatch(pageSource, /optionTone\(pageContract, "work_state_filter_chips", row\.work_state\)/);
	assert.doesNotMatch(drawerSource, /optionTone\(pageContract, "work_state_filter_chips", row\.work_state\)/);
});
