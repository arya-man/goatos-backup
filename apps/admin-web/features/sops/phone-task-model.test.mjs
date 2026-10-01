// TASK WITH ITS OWN PHONE TAB (docs/decisions/simple-task-phone-tabs.md): the editor's state turns
// into form_dsl.phone_task EXACTLY as the backend's PhoneTaskDoc reads it (it refuses unknown keys),
// and a stored document reads back into the same state for an edit.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { blankPhoneTask, emitPhoneTask, hasPhoneTask, parsePhoneTask, phoneTaskFormDsl, phoneTaskProblems, PHONE_TASK_NAME_MAX } from "./phone-task-model.ts";

const CBE = "11111111-1111-1111-1111-111111111111";
const CPT = "22222222-2222-2222-2222-222222222222";
const SHED = "33333333-3333-3333-3333-333333333333";
const PERSON = "44444444-4444-4444-4444-444444444444";
// Every key the backend's PhoneTaskDoc declares (penroutines/domain/phone_task.go).
const DOC_KEYS = new Set(["tab", "instruction", "scope_kind", "occupied_only", "cadence_kind", "weekdays", "month_days", "after_work_kinds", "interval_days", "start_date", "due_offset_days", "notify_time", "review_kind", "evidence", "parks"]);

function fumigation() {
  const draft = blankPhoneTask([CBE, CPT], { start_date: "2026-10-01", notify_time: "07:00", interval_days: 7 });
  return {
    ...draft,
    name: "Fumigation",
    icon: "spray",
    filters: ["date", "status"],
    instruction: " Fumigate the pen and film it. ",
    scopeKind: "selected_pens",
    cadenceKind: "every_n_days",
    intervalDays: "14",
    reviewKind: "verifier",
    questions: [{ key: 1, idTouched: false, id: "door_closed", kind: "yes_no", title: "Door closed?", required: true, proof: { kind: "photo", count: "single" } }],
    video: { min: 2, max: 2 },
    presence: "required",
    parks: [
      { parkId: CBE, included: true, assigneeUserId: PERSON, pens: [`${SHED}|Part 1`, `${SHED}|`] },
      { parkId: CPT, included: false, assigneeUserId: "", pens: [] },
    ],
  };
}

test("the editor state becomes form_dsl.phone_task exactly as the backend reads it", () => {
  const doc = emitPhoneTask(fumigation());
  assert.deepEqual(doc, {
    tab: { icon: "spray", filters: ["status", "date"] },
    instruction: "Fumigate the pen and film it.",
    scope_kind: "selected_pens",
    cadence_kind: "every_n_days",
    interval_days: 14,
    start_date: "2026-10-01",
    notify_time: "07:00",
    review_kind: "verifier",
    evidence: {
      questions: [{ id: "door_closed", kind: "yes_no", title: "Door closed?", required: true, proof: { kind: "photo", count: "single" } }],
      photo: { min: 0, max: 0 },
      video: { min: 2, max: 2 },
      presence: "required",
    },
    // An unticked park is not in the document; a pen is shed + partition, an undivided one has "".
    parks: [{ park_id: CBE, assignee_user_id: PERSON, pens: [{ shed_id: SHED, partition_label: "Part 1" }, { shed_id: SHED, partition_label: "" }] }],
  });
  for (const key of Object.keys(doc)) assert.ok(DOC_KEYS.has(key), `${key} is not a PhoneTaskDoc key`);
  assert.equal("label" in doc.tab || "module" in doc.tab, false, "the tab name is the SOP's name and the module is the page, never fields");
});

test("optional keys travel only for the cadence or scope that reads them", () => {
  const every = { ...fumigation(), scopeKind: "all_pens", occupiedOnly: false, cadenceKind: "weekly", weekdays: [5, 1], intervalDays: "14", dueOffsetDays: "" };
  const doc = emitPhoneTask(every);
  assert.equal(doc.occupied_only, false);
  assert.deepEqual(doc.weekdays, [1, 5]);
  assert.equal("interval_days" in doc, false);
  assert.equal("month_days" in doc, false);
  assert.equal("after_work_kinds" in doc, false);
  assert.equal("due_offset_days" in doc, false, "a blank offset lets the backend default apply");
  assert.deepEqual(doc.parks[0].pens, [], "every pen names no pens");

  const park = emitPhoneTask({ ...fumigation(), scopeKind: "park", cadenceKind: "after_work", afterWorkKinds: ["deworming", "made_up"], dueOffsetDays: "1" });
  assert.equal("occupied_only" in park, false);
  assert.deepEqual(park.after_work_kinds, ["deworming"], "an unknown work kind is dropped, never sent");
  assert.equal(park.due_offset_days, 1);
  assert.deepEqual(park.parks[0].pens, []);

  const blankN = emitPhoneTask({ ...fumigation(), intervalDays: "" });
  assert.equal("interval_days" in blankN, false, "a blank N is the backend's to refuse, never invented");
});

test("a stored document reads back into the same state, and an unnamed park is offered unticked", () => {
  const doc = emitPhoneTask(fumigation());
  const formDsl = phoneTaskFormDsl(doc, null);
  assert.equal(hasPhoneTask(formDsl), true);
  assert.equal(hasPhoneTask({ fields: [] }), false);
  const back = parsePhoneTask("Fumigation", formDsl, [CBE, CPT]);
  assert.equal(back.name, "Fumigation");
  assert.deepEqual(back.parks.map((p) => [p.parkId, p.included]), [[CBE, true], [CPT, false]]);
  // Round trip: emitting the read-back state gives the stored document again.
  assert.deepEqual(emitPhoneTask(back), doc);
  assert.equal(back.questions[0].idTouched, true, "a stored question's key never follows a title edit");
  assert.equal(parsePhoneTask("x", { fields: [] }), null);
});

test("the version's form_dsl keeps the base version's other keys, an empty capture form and a schema version", () => {
  const doc = emitPhoneTask(fumigation());
  const fresh = phoneTaskFormDsl(doc, null);
  assert.deepEqual(Object.keys(fresh).sort(), ["fields", "phone_task", "schema_version"]);
  assert.deepEqual(fresh.fields, []);
  const edited = phoneTaskFormDsl(doc, { schema_version: "goatos.sop-form.v1", fields: [{ key: "old" }], other: 1, phone_task: { stale: true } });
  assert.equal(edited.other, 1);
  assert.deepEqual(edited.fields, []);
  assert.deepEqual(edited.phone_task, doc);
});

test("a save is blocked only on what the editor can know", () => {
  const name = (id) => (id === CBE ? "Coimbatore" : "Channapatna");
  assert.deepEqual(phoneTaskProblems(fumigation(), name), []);
  const blank = blankPhoneTask([CBE, CPT]);
  assert.deepEqual(phoneTaskProblems(blank, name).map((p) => p.key), ["ptask.problem.name", "ptask.problem.icon", "ptask.problem.park"]);
  const long = { ...fumigation(), name: "x".repeat(PHONE_TASK_NAME_MAX + 1) };
  assert.deepEqual(phoneTaskProblems(long, name).map((p) => p.key), ["ptask.problem.name_long"]);
  const noOne = { ...fumigation(), parks: [{ parkId: CBE, included: true, assigneeUserId: "", pens: [] }] };
  assert.deepEqual(phoneTaskProblems(noOne, name), [
    { key: "ptask.problem.assignee", park: "Coimbatore" },
    { key: "ptask.problem.pens", park: "Coimbatore" },
  ]);
});

test("every module SOP page offers New phone task and opens a phone-task SOP in its own editor", () => {
  const modulePage = readFileSync(new URL("./module-page.tsx", import.meta.url), "utf8");
  const library = readFileSync(new URL("./sop-library.tsx", import.meta.url), "utf8");
  const editor = readFileSync(new URL("./phone-task-editor.tsx", import.meta.url), "utf8");
  assert.match(modulePage, /sp\.type === "phone_task" && sliceAuthorsPhoneTasks\(slice\)/);
  assert.match(modulePage, /"phone_task" in version\.form_dsl/);
  // The phone-task check runs before every other editor in the chain.
  assert.ok(modulePage.indexOf('"phone_task" in version.form_dsl') < modulePage.indexOf("parseCaptureCard(detail.data.sop.code"));
  assert.match(library, /phoneTaskHref \?/);
  // Publish closes the editor onto the library, like every SOP editor; the action does not also revalidate.
  assert.match(editor, /router\.push\(publishedHref\(basePath, /);
  const actions = readFileSync(new URL("./sop-actions.ts", import.meta.url), "utf8");
  const phoneActions = actions.slice(actions.indexOf("export async function savePhoneTaskVersion"));
  assert.doesNotMatch(phoneActions, /revalidatePath/);
});
