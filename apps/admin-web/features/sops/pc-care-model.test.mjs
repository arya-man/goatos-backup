import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

import { PC_CARE_CATEGORIES, blankCapture, emitPcCare, parsePcCare, pcCareProblems } from "./pc-care-model.ts";

// The seeded document, read from the backend's own embedded copy: the editor must round-trip the
// card the farm is actually running, not a hand-written imitation of it.
const seed = JSON.parse(readFileSync(new URL("../../../../backend/internal/pccare/domain/sopseed/pc_care.json", import.meta.url), "utf8"));
const label = (c) => c;
const REMOVAL = "Feed & water removal";

test("the seeded document round-trips byte-faithfully", () => {
  const rows = parsePcCare({ pc_care: seed });
  assert.ok(rows, "the seed must parse");
  assert.deepEqual(emitPcCare(rows), seed);
});

test("the seed has no problems, and every category keeps its card", () => {
  const rows = parsePcCare({ pc_care: seed });
  assert.deepEqual(pcCareProblems(rows, label, REMOVAL), []);
  for (const category of PC_CARE_CATEGORIES) {
    assert.ok(rows.categories[category].proofs.length > 0, `${category} must carry its captures`);
  }
  // The trimming "while" clip keeps its recorder hint; it is the one field PC Care adds.
  const during = rows.categories.hoof_trimming.proofs.find((p) => p.key === "during_video");
  assert.equal(during.minSeconds, "10");
});

test("a document with no pc_care section is not a PC Care document", () => {
  assert.equal(parsePcCare({ weighing: { schema_version: "x" } }), null);
  assert.equal(parsePcCare(null), null);
});

test("a slot published before the required flag existed is compulsory", () => {
  const rows = parsePcCare({
    pc_care: { ...seed, categories: { ...seed.categories, deworming: { proofs: [{ key: "video", title: "Dose", kind: "video" }], questions: [] } } },
  });
  assert.equal(rows.categories.deworming.proofs[0].required, true);
});

test("a category with no compulsory capture is refused before save", () => {
  const rows = parsePcCare({ pc_care: seed });
  rows.categories.ticks_removal.proofs[0].required = false;
  const problems = pcCareProblems(rows, label, REMOVAL);
  assert.ok(
    problems.some((p) => p.includes("ticks_removal") && p.includes("compulsory")),
    `expected a compulsory-capture problem, got ${JSON.stringify(problems)}`,
  );
});

test("the removal must name work it applies to unless it is switched off", () => {
  const rows = parsePcCare({ pc_care: seed });
  rows.removal.appliesTo = [];
  assert.ok(pcCareProblems(rows, label, REMOVAL).some((p) => p.includes(REMOVAL)));
  // Switched off, the same document is clean: there is no card to ask anything of.
  rows.removal.mode = "off";
  rows.removal.proofs = [];
  assert.deepEqual(pcCareProblems(rows, label, REMOVAL), []);
});

test("an authored evening must be a time, and a blank one means the farm's own", () => {
  const rows = parsePcCare({ pc_care: seed });
  assert.equal(rows.removal.cutoffTime, "");
  assert.deepEqual(pcCareProblems(rows, label, REMOVAL), []);
  rows.removal.cutoffTime = "half past eight";
  assert.ok(pcCareProblems(rows, label, REMOVAL).some((p) => p.includes("20:00")));
  rows.removal.cutoffTime = "21:30";
  assert.deepEqual(pcCareProblems(rows, label, REMOVAL), []);
  // The emitted document keeps a blank evening rather than dropping the key: blank MEANS the
  // farm's evening, and an absent key would read as "never authored".
  rows.removal.cutoffTime = "";
  assert.equal(emitPcCare(rows).feed_water_removal.cutoff_time, "");
});

test("a photo capture cannot carry a recorder length", () => {
  const rows = parsePcCare({ pc_care: seed });
  const capture = blankCapture();
  capture.key = "trough";
  capture.title = "Trough";
  capture.kind = "photo";
  capture.minSeconds = "10";
  rows.categories.deworming.proofs.push(capture);
  assert.ok(pcCareProblems(rows, label, REMOVAL).some((p) => p.includes("photo has no length")));
});

test("an added capture emits its length only when it has one", () => {
  const rows = parsePcCare({ pc_care: seed });
  const capture = blankCapture();
  capture.key = "after_dose";
  capture.title = "After the dose";
  rows.categories.deworming.proofs.push(capture);
  const emitted = emitPcCare(rows).categories.deworming.proofs.at(-1);
  assert.deepEqual(emitted, { key: "after_dose", title: "After the dose", kind: "video", required: true });
});

test("fumigation is pen work: its own card, never offered the feed & water removal", async () => {
  const { PC_CARE_REMOVAL_CATEGORIES, isPenCategory, pcCareReachCopyKey } = await import("./pc-care-model.ts");
  const rows = parsePcCare({ pc_care: seed });
  assert.deepEqual(rows.categories.fumigation.proofs.map((p) => p.key), ["mixing_video", "spraying_video"]);
  assert.match(rows.categories.fumigation.instruction, /5 ml/);
  assert.equal(isPenCategory("fumigation"), true);
  assert.equal(isPenCategory("deworming"), false);
  assert.ok(!PC_CARE_REMOVAL_CATEGORIES.includes("fumigation"), "the removal is never offered on a pen spray");
  assert.equal(pcCareReachCopyKey("fumigation"), "pcsop.flow.reach_pen");
  assert.equal(pcCareReachCopyKey("hoof_trimming"), "pcsop.flow.reach_roster");
  assert.equal(pcCareReachCopyKey("deworming"), "pcsop.flow.reach_scan");
});

test("repeat every N days round-trips, stays absent when blank, and is bounded", async () => {
  const rows = parsePcCare({ pc_care: seed });
  assert.equal(rows.categories.fumigation.repeatEveryDays, "");
  assert.equal("repeat_every_days" in emitPcCare(rows).categories.fumigation, false, "blank writes nothing -- the seed carries no repeat");
  rows.categories.fumigation.repeatEveryDays = "30";
  const out = emitPcCare(rows);
  assert.equal(out.categories.fumigation.repeat_every_days, 30);
  assert.equal(parsePcCare({ pc_care: out }).categories.fumigation.repeatEveryDays, "30");
  assert.deepEqual(pcCareProblems(rows, label, REMOVAL), []);
  for (const bad of ["0.5", "366", "-3"]) {
    rows.categories.fumigation.repeatEveryDays = bad;
    assert.ok(pcCareProblems(rows, label, REMOVAL).some((p) => p.includes("repeat every")), `${bad} must be refused`);
  }
});
