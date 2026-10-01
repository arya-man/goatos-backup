// A routine published from a module SOP is changed there, never on /routines
// (docs/decisions/simple-task-phone-tabs.md; the backend refuses with 409 managed_by_sop).
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { routineEditableHere, sliceAuthorsPhoneTasks, sopManagedBy } from "./sop-managed.ts";

test("a routine with an SOP code is read-only here and names the SOP page that owns it", () => {
  assert.equal(sopManagedBy(""), null);
  assert.equal(sopManagedBy(null), null);
  assert.deepEqual(sopManagedBy("pc_care.fumigation"), { slice: "pc_care", href: "/pc-care/sops" });
  assert.deepEqual(sopManagedBy("counts.pen_wash"), { slice: "counts", href: "/counts/sops" });
  assert.deepEqual(sopManagedBy("feed.trough_clean"), { slice: "feed", href: "/feed/sops" });
  assert.deepEqual(sopManagedBy("mystery.task"), { slice: null, href: "" }, "an unknown SOP still owns it; it is linked nowhere");
  assert.equal(routineEditableHere({ sop_code: "" }, true), true);
  assert.equal(routineEditableHere({ sop_code: "pc_care.fumigation" }, true), false);
  assert.equal(routineEditableHere({ sop_code: "" }, false), false, "the caller's own grant still decides a hand-made routine");
});

test("only module SOP pages author phone tasks", () => {
  for (const slice of ["pc_care", "feed", "weighing", "counts", "milk", "procurement", "sales"]) assert.equal(sliceAuthorsPhoneTasks(slice), true, slice);
  for (const slice of ["general", "vaccination"]) assert.equal(sliceAuthorsPhoneTasks(slice), false, slice);
});

test("/routines renders an SOP routine read-only with the note, and the Phone tabs editor is gone", () => {
  const page = readFileSync(new URL("./routines-page.tsx", import.meta.url), "utf8");
  assert.match(page, /routineEditableHere\(routine, canEdit\)/);
  assert.match(page, /routineEditableHere\(routine, canSetStatus\)/);
  assert.match(page, /managedNote=\{sopNote\(routine\)\}/);
  assert.match(page, /canSave=\{editable\}/);
  assert.doesNotMatch(page, /PhoneTabsSection|phone-tabs-section/);
  const route = readFileSync(new URL("../../app/(admin)/routines/page.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(route, /listPenRoutineTabs/);
  const server = readFileSync(new URL("../../lib/api/pen-routines-server.ts", import.meta.url), "utf8");
  assert.doesNotMatch(server, /createPenRoutineTab|updatePenRoutineTab|setPenRoutineTabStatus/);
});
