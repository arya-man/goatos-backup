import { readFileSync, readdirSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { emitFollowUp, followUpProblems, parseFollowUp, slugKey } from "./followup-model.ts";

// The seeded documents are the same bytes migration 000299 publishes (pinned by the Go test
// TestMigrationEmbedsTheSeededDocuments). Parsing them into editor rows and emitting them back
// must reproduce the document EXACTLY, or opening the editor and pressing Publish with no edits
// would silently change what the phone runs.
const seedDir = new URL("../../../../backend/internal/tasks/domain/sopseed/", import.meta.url);
const seededDocs = readdirSync(seedDir).filter((f) => f.startsWith("counts_") && f.endsWith(".json"));

function canonical(v) {
  return JSON.stringify(v, (_k, val) => {
    if (val && typeof val === "object" && !Array.isArray(val)) {
      return Object.fromEntries(Object.keys(val).sort().map((k) => [k, val[k]]));
    }
    return val;
  });
}

test("the seeded herd-operations documents round-trip through the editor model byte-faithfully", () => {
  assert.ok(seededDocs.length >= 4, `expected the four seeded docs, found ${seededDocs.join(",")}`);
  for (const name of seededDocs) {
    const doc = JSON.parse(readFileSync(new URL(name, seedDir), "utf8"));
    const rows = parseFollowUp({ follow_up: doc });
    assert.ok(rows, `${name}: parse returned null`);
    const emitted = emitFollowUp(rows);
    assert.equal(canonical(emitted), canonical(doc), `${name}: emit differs from the seeded document`);
  }
});

test("the seeded documents raise no client-side problems", () => {
  const answerKinds = { record_yes_no: "yes_no", record_select: "select", record_multiselect: "multiselect", weigh: "number", record_pen: "text" };
  for (const name of seededDocs) {
    const doc = JSON.parse(readFileSync(new URL(name, seedDir), "utf8"));
    assert.deepEqual(followUpProblems(parseFollowUp({ follow_up: doc }), answerKinds), [], name);
  }
});

test("client pre-checks name the field the backend would reject", () => {
  const doc = JSON.parse(readFileSync(new URL("counts_death.json", seedDir), "utf8"));
  const rows = parseFollowUp({ follow_up: doc });
  const step = rows.tracks[0].steps[0];
  step.scheduleKind = "at_fixed_time";
  step.time = "7am";
  const dup = { ...rows.tracks[0].steps[1], id: "x", key: rows.tracks[0].steps[0].key };
  rows.tracks[0].steps.push(dup);
  const problems = followUpProblems(rows, {});
  assert.ok(problems.some((p) => p.includes("HH:MM")), problems.join("\n"));
  assert.ok(problems.some((p) => p.includes("used twice")), problems.join("\n"));
});

test("slugKey derives a stable unique key from the title", () => {
  const taken = new Set(["photo_of_the_ear"]);
  assert.equal(slugKey("Photo of the ear", taken), "photo_of_the_ear_2");
  assert.equal(slugKey("  ??? ", new Set()), "step");
});

test("a document with no follow_up parses to null (a non herd-ops SOP)", () => {
  assert.equal(parseFollowUp({ fields: [] }), null);
});
