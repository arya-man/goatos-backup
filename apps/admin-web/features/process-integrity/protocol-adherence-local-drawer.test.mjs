import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./protocol-adherence.tsx", import.meta.url), "utf8");
const drawerSource = readFileSync(new URL("./protocol-adherence-local-drawer.tsx", import.meta.url), "utf8");
const contractSource = readFileSync(new URL("../../lib/admin-ui-contract.ts", import.meta.url), "utf8");

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

test("protocol-adherence expected detail prefers shed and partition context", () => {
	assert.match(pageSource, /function adherenceLocationDetail/);
	assert.match(pageSource, /row\.shed_name/);
	assert.match(pageSource, /row\.partition_label/);
	assert.match(pageSource, /expectedDetail = adherenceLocationDetail\(row\) \|\| expected\.detail/);
	assert.match(pageSource, /expectedDetail: locationDetail \|\| expected\.detail/);
});

test("protocol-adherence tolerates stale work-state values from staging data", () => {
	assert.match(contractSource, /const PROCESS_WORK_STATE_OPTIONS/);
	assert.match(contractSource, /proof_pending[\s\S]+Proof pending/);
	assert.match(contractSource, /verification_pending[\s\S]+Verification pending/);
	assert.match(contractSource, /completed[\s\S]+Completed/);
	assert.match(contractSource, /"protocol-adherence": PROCESS_OPTION_GROUP_FALLBACKS/);
	assert.match(pageSource, /optionGroup\(pageContract, "work_state_filter_chips"\)/);
	assert.match(drawerSource, /function workStateTone[\s\S]+optionTone\(pageContract, "work_state_filter_chips", workState\)/);
	assert.doesNotMatch(pageSource, /optionTone\(pageContract, "work_state_filter_chips", row\.work_state\)/);
	assert.doesNotMatch(drawerSource, /optionTone\(pageContract, "work_state_filter_chips", row\.work_state\)/);
});

test("protocol-adherence tolerates stale severity contract values from staging data", () => {
	assert.match(contractSource, /const PROCESS_SEVERITY_OPTIONS[\s\S]+at_risk[\s\S]+broken/);
	assert.match(pageSource, /optionTone\(pageContract, "severity_chips", row\.severity\)/);
	assert.match(drawerSource, /optionTone\(pageContract, "severity_chips", row\.severity\)/);
	assert.doesNotMatch(pageSource, /SEVERITY_META/);
	assert.doesNotMatch(drawerSource, /SEVERITY_META/);
});
