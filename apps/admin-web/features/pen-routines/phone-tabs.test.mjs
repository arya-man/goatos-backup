import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { applySavedTab, decodePhoneTabStatus, decodePhoneTabWrite, routinesByPark, TAB_FORM_JSON_FIELDS } from "./phone-tab-model.ts";

// Phone tabs on /routines (maintainer instruction 2026-10-01, docs/decisions/simple-task-phone-tabs.md).
const here = new URL("./", import.meta.url);
const read = (rel) => readFileSync(new URL(rel, here), "utf8");
const section = read("./phone-tabs-section.tsx");
const actions = read("./phone-tab-actions.ts");
const feature = read("./routines-page.tsx");

function form(fields) {
  const data = new FormData();
  for (const [key, value] of Object.entries(fields)) data.set(key, value);
  return data;
}

test("decoder: a create carries the name, module, icon and both lists, and no fence", () => {
  const body = decodePhoneTabWrite(
    form({
      label: "  Fumigation   round ",
      module_key: "pc_care",
      icon_key: "fumigation",
      [TAB_FORM_JSON_FIELDS.filters]: JSON.stringify(["status", "pen", "status", "bogus"]),
      [TAB_FORM_JSON_FIELDS.routineIds]: JSON.stringify(["r1", "r2", "r1", ""]),
    }),
  );
  assert.deepEqual(body, { label: "Fumigation round", module_key: "pc_care", icon_key: "fumigation", filters: ["status", "pen"], routine_ids: ["r1", "r2"] });
});

test("decoder: an edit carries its row_version as a number", () => {
  const body = decodePhoneTabWrite(form({ label: "Wash", module_key: "pen_routines", icon_key: "water", row_version: "4" }));
  assert.strictEqual(body.row_version, 4);
  assert.deepEqual(body.filters, []);
  assert.deepEqual(body.routine_ids, []);
});

test("decoder: a tab with no name, module or icon is a form error, never a post", () => {
  assert.throws(() => decodePhoneTabWrite(form({ label: " ", module_key: "pc_care", icon_key: "water" })));
  assert.throws(() => decodePhoneTabWrite(form({ label: "Wash", module_key: "", icon_key: "water" })));
  assert.throws(() => decodePhoneTabWrite(form({ label: "Wash", module_key: "pc_care" })));
  assert.throws(() => decodePhoneTabWrite(form({ label: "Wash", module_key: "pc_care", icon_key: "water", [TAB_FORM_JSON_FIELDS.filters]: "{}" })));
});

test("status decoder accepts only active / retired", () => {
  assert.deepEqual(decodePhoneTabStatus(form({ status: "retired", row_version: "2" })), { status: "retired", row_version: 2 });
  assert.throws(() => decodePhoneTabStatus(form({ status: "paused", row_version: "2" })));
});

test("a saved tab replaces itself and pulls its routines off every other tab", () => {
  const tabs = [
    { tab_id: "a", routines: [{ routine_id: "r1" }, { routine_id: "r2" }] },
    { tab_id: "b", routines: [{ routine_id: "r3" }] },
  ];
  const saved = { tab_id: "b", routines: [{ routine_id: "r3" }, { routine_id: "r2" }] };
  const next = applySavedTab(tabs, saved);
  assert.deepEqual(next, [{ tab_id: "a", routines: [{ routine_id: "r1" }] }, saved]);
  const created = { tab_id: "c", routines: [{ routine_id: "r1" }] };
  assert.deepEqual(applySavedTab(next, created).map((tab) => [tab.tab_id, tab.routines.length]), [["a", 0], ["b", 2], ["c", 1]]);
  assert.equal(tabs[0].routines.length, 2, "the input list is not mutated");
});

test("the picker groups routines by park in served order and drops retired ones", () => {
  const routines = [
    { routine_id: "r1", name: "Fumigation", park_id: "cbe", park_name: "Coimbatore", status: "active" },
    { routine_id: "r2", name: "Wash", park_id: "cpt", park_name: "Channapatna", status: "paused" },
    { routine_id: "r3", name: "Old", park_id: "cbe", park_name: "Coimbatore", status: "retired" },
    { routine_id: "r4", name: "Sweep", park_id: "cbe", park_name: "Coimbatore", status: "active" },
  ];
  const groups = routinesByPark(routines, new Set());
  assert.deepEqual(groups.map((group) => [group.parkName, group.routines.map((routine) => routine.routine_id)]), [["Coimbatore", ["r1", "r4"]], ["Channapatna", ["r2"]]]);
  assert.deepEqual(routinesByPark(routines, new Set(["r3"]))[0].routines.map((routine) => routine.routine_id), ["r1", "r3", "r4"]);
});

test("the tab actions return the row and never revalidate or redirect", () => {
  assert.doesNotMatch(actions, /revalidate(Path|Tag)\(|redirect\(/);
  assert.match(actions, /tab: result\.data\.tab/);
  assert.match(actions, /randomUUID\(\)/);
  assert.match(actions, /result\.error\.message/);
  assert.doesNotMatch(actions, /["'][A-Z][a-z]+ [a-z]+[^"']*["']/, "no visible sentence literal in the actions");
});

test("the section opens its drawer locally, gates writes on the contract, and uses real checkboxes", () => {
  assert.match(section, /<LocalOverlayDrawer/);
  assert.match(section, /selectionKey=\{PARAM_TAB\}/);
  assert.match(section, /applySavedTab\(current, saved\)/);
  assert.match(section, /type="checkbox"/);
  assert.doesNotMatch(section, /window\.confirm|alert\(|role="checkbox"|aria-checked/);
  assert.doesNotMatch(section, /ceo_internal|park_head|role ===|\.role\b/, "no role-string conditional");
  assert.match(feature, /canCreate=\{canCreate\}/);
  assert.match(feature, /controlEnabled\(pageContract, "create_routine", false\)/);
  assert.notEqual(section.match(/PARAM_TAB = "([a-z_]+)"/)[1], feature.match(/PARAM_EDIT = "([a-z_]+)"/)[1], "the two drawers keep separate params");
});
