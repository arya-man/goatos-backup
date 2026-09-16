import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { blankProofSlot, blankQuestion, emitWeighing, parseWeighing, weighingProblems } from "./weighing-model.ts";

// The seeded document is the same bytes migration 000315 adds to each tenant's published
// weighing.session version (pinned by the Go test TestMigrationEmbedsTheSeededWeighingSOP).
// Parsing it into editor rows and emitting it back must reproduce it EXACTLY, or opening the
// editor and pressing Publish with no edits would change what the planner and the phone run.
const seedPath = new URL("../../../../backend/internal/weighing/domain/sopseed/weighing_session.json", import.meta.url);

function canonical(v) {
  return JSON.stringify(v, (_k, val) => (val && typeof val === "object" && !Array.isArray(val) ? Object.fromEntries(Object.keys(val).sort().map((k) => [k, val[k]])) : val));
}

test("number question bounds cannot silently disappear when saving a draft or publishing", () => {
  const doc = JSON.parse(readFileSync(seedPath, "utf8"));
  const rows = parseWeighing({ weighing: doc });
  const question = { ...blankQuestion("number"), key: "count", title: "Count" };
  rows.removalQuestions = [question];
  for (const bound of ["min", "max"]) {
    for (const invalid of ["abc", "NaN", "Infinity", "-Infinity", "1e309"]) {
      question[bound] = invalid;
      assert.ok(weighingProblems(rows).includes(`Removal question 1: ${bound} must be a finite number`), `${bound}=${invalid} must block saving`);
    }
    question[bound] = "";
  }
  assert.deepEqual(weighingProblems(rows), []);
  assert.equal("min" in emitWeighing(rows).feed_water_removal.questions[0], false);
  assert.equal("max" in emitWeighing(rows).feed_water_removal.questions[0], false);
  question.min = "-2.5";
  question.max = "0";
  assert.deepEqual(weighingProblems(rows), []);
  const emitted = emitWeighing(rows).feed_water_removal.questions[0];
  assert.equal(emitted.min, -2.5);
  assert.equal(emitted.max, 0);
});

test("the seeded weighing rules round-trip through the editor model byte-faithfully", () => {
  const doc = JSON.parse(readFileSync(seedPath, "utf8"));
  const rows = parseWeighing({ weighing: doc });
  assert.ok(rows);
  assert.equal(rows.removalMode, "required");
  assert.deepEqual(rows.modes, ["individual_animal", "per_shed_partition"]);
  assert.deepEqual(rows.removalProofs.map((p) => [p.key, p.kind, p.required]), [["feed_video", "video", true], ["water_video", "video", true]]);
  assert.equal(rows.removalQuestions.length, 0);
  assert.equal(canonical(emitWeighing(rows)), canonical(doc));
  assert.deepEqual(weighingProblems(rows), []);
});

test("a form_dsl without the section is not a weighing SOP", () => {
  assert.equal(parseWeighing({ fields: [] }), null);
  assert.equal(parseWeighing(null), null);
});

test("pre-checks name the rule: no mode, per-animal video off, lump-sum window, question problems", () => {
  const rows = parseWeighing({ weighing: JSON.parse(readFileSync(seedPath, "utf8")) });
  rows.modes = [];
  rows.individualVideoRequired = false;
  rows.lumpSumVideoMin = "4";
  rows.lumpSumVideoMax = "9";
  rows.defaultCapPerDay = "0";
  rows.removalProofs[1].title = "";
  rows.removalProofs.forEach((p) => { p.required = false; });
  const q = blankQuestion("number");
  q.key = "buckets";
  q.title = "";
  q.min = "5";
  q.max = "2";
  const dep = { ...blankQuestion("text"), key: "why", title: "Why", onlyIfQuestion: "buckets", onlyIfValue: "x" };
  rows.removalQuestions = [q, dep];
  const problems = weighingProblems(rows);
  for (const want of [
    "Offer at least one way of weighing",
    "Default animals per day",
    "Capture 2: needs a title",
    "At least one capture must be compulsory",
    "Removal question 1: needs the question text",
    "Removal question 1: min must not exceed max",
    'Removal question 2: "ask only when" must name a pick-one question',
    "per-animal video cannot be switched off",
    "at most 5",
  ]) {
    assert.ok(problems.some((p) => p.includes(want)), `missing problem containing: ${want}\n${problems.join("\n")}`);
  }
});

test("an authored question and a changed mode emit the wire shape the backend validates", () => {
  const rows = parseWeighing({ weighing: JSON.parse(readFileSync(seedPath, "utf8")) });
  rows.removalMode = "optional";
  rows.removalProofs[0].title = "Feed away";
  const q = blankQuestion("choice");
  q.key = "all_pens";
  q.title = "Every pen emptied?";
  q.options.push({ value: "other", label: "Other" });
  q.allowOther = true;
  rows.removalQuestions = [q];
  const out = emitWeighing(rows);
  assert.equal(out.feed_water_removal.mode, "optional");
  assert.equal(out.feed_water_removal.proofs[0].title, "Feed away");
  assert.equal(out.feed_water_removal.proofs[0].kind, "video");
  assert.deepEqual(out.feed_water_removal.questions[0], {
    id: "all_pens", kind: "choice", title: "Every pen emptied?", required: true,
    options: [{ value: "yes", label: "Yes" }, { value: "no", label: "No" }, { value: "other", label: "Other" }], allow_other: true,
  });
  assert.deepEqual(weighingProblems(rows), []);
});

test("authored capture slots: a photo beside the videos, a video swapped for a photo, an optional slot, a removed slot", () => {
  const rows = parseWeighing({ weighing: JSON.parse(readFileSync(seedPath, "utf8")) });
  rows.removalProofs[1].kind = "photo";
  rows.removalProofs[1].title = "Empty water trough";
  rows.removalProofs.push({ ...blankProofSlot(), key: "gate", title: "Gate closed", kind: "either", required: false });
  assert.deepEqual(weighingProblems(rows), []);
  const out = emitWeighing(rows);
  assert.deepEqual(out.feed_water_removal.proofs.map((p) => [p.key, p.kind, p.required]), [["feed_video", "video", true], ["water_video", "photo", true], ["gate", "either", false]]);
  // Round-trips.
  assert.equal(canonical(emitWeighing(parseWeighing({ weighing: out }))), canonical(out));
  // Drop every slot: refused while the removal is on; fine when it is off.
  rows.removalProofs = [];
  assert.ok(weighingProblems(rows).some((p) => p.includes("at least one capture")));
  rows.removalMode = "off";
  assert.deepEqual(weighingProblems(rows), []);
});

test("the removal evening: blank means the farm's, HH:MM is emitted, anything else is refused", () => {
  const rows = parseWeighing({ weighing: JSON.parse(readFileSync(seedPath, "utf8")) });
  assert.equal(rows.removalCutoffTime, "");
  assert.equal(emitWeighing(rows).feed_water_removal.cutoff_time, undefined);
  rows.removalCutoffTime = "21:30";
  assert.deepEqual(weighingProblems(rows), []);
  assert.equal(emitWeighing(rows).feed_water_removal.cutoff_time, "21:30");
  rows.removalCutoffTime = "9pm";
  assert.ok(weighingProblems(rows).some((p) => p.includes("time like 20:00")));
});

test("a capture slot without the required flag (a pre-flag document) is compulsory", () => {
  const rows = parseWeighing({ weighing: { schema_version: "goatos.sop-weighing.v1", planning: { modes: ["individual_animal"], default_cap_per_day: 100 }, feed_water_removal: { mode: "required", proofs: [{ key: "feed_video", title: "Feed", kind: "video" }, { key: "gate", title: "Gate", kind: "photo", required: false }], questions: [] }, capture: { individual: { video_required: true }, lump_sum: { video_min: 1, video_max: 5 } } } });
  assert.deepEqual(rows.removalProofs.map((p) => p.required), [true, false]);
  assert.deepEqual(weighingProblems(rows), []);
});

test("legacy SOP calendar metadata remains round-trip compatible", () => {
  const seed = JSON.parse(readFileSync(seedPath, "utf8"));
  const rows = parseWeighing({ weighing: seed });
  assert.equal(rows.weightsFromMode, "fixed_date");
  assert.equal(rows.weightsFromDate, "2026-08-03");
  assert.equal(rows.weightsEarliestDate, "2026-08-01");
  assert.deepEqual(emitWeighing(rows).weights_pages, { default_from_mode: "fixed_date", default_from_date: "2026-08-03", earliest_date: "2026-08-01" });

  // A document published before the block existed reads as the seed, exactly as the backend does.
  const { weights_pages: _dropped, ...older } = seed;
  const olderRows = parseWeighing({ weighing: older });
  assert.equal(olderRows.weightsFromDate, "2026-08-03");
  assert.equal(olderRows.weightsEarliestDate, "2026-08-01");

  const rolling = { ...rows, weightsFromMode: "rolling_days", weightsFromDays: "45" };
  assert.deepEqual(weighingProblems(rolling), []);
  assert.deepEqual(emitWeighing(rolling).weights_pages, { default_from_mode: "rolling_days", default_from_days: 45, earliest_date: "2026-08-01" });
  assert.ok(weighingProblems({ ...rolling, weightsFromDays: "0" }).some((p) => p.includes("1 to 3650")));
  assert.ok(weighingProblems({ ...rows, weightsFromDate: "2026-07-20" }).some((p) => p.includes("cannot be before the earliest day")));
  assert.ok(weighingProblems({ ...rows, weightsEarliestDate: "" }).some((p) => p.includes("earliest day the calendar offers")));
  assert.ok(weighingProblems({ ...rows, weightsFromDate: "3 Aug" }).some((p) => p.includes("pick the day the pages open from")));
});

test("Other explanation is only publishable for pick-one questions", () => {
  const rows = parseWeighing({weighing:JSON.parse(readFileSync(seedPath,"utf8"))});
  const q = {...blankQuestion("choice"),key:"reason",title:"Reason",options:[{value:"other",label:"Other"}],allowOther:true};
  rows.removalQuestions = [q];
  assert.deepEqual(weighingProblems(rows),[]);
  for (const kind of ["multi","text","number"]) {
    q.kind = kind;
    assert.ok(weighingProblems(rows).some(p=>p.includes("only supported for pick-one")),kind);
  }
});


test("SOP editor and summary cannot advertise obsolete calendar settings", () => {
  for (const name of ["weighing-editor.tsx", "weighing-summary.tsx"]) {
    const source = readFileSync(new URL(name, import.meta.url), "utf8");
    assert.doesNotMatch(source, /weightsFrom|weightsEarliest|wsop\.(?:weights\.|section\.weights|summary\.weights_)/, name);
  }
  const contract = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");
  assert.doesNotMatch(contract, /wsop\.(?:weights\.|section\.weights|summary\.weights_)/);
  const original = JSON.parse(readFileSync(seedPath, "utf8"));
  const rows = parseWeighing({ weighing: original });
  rows.defaultCapPerDay = "125";
  assert.deepEqual(emitWeighing(rows).weights_pages, original.weights_pages);
});

// --- THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16) ---------------------
//
// Two separate capture sections, per animal and whole pen, each with its own slots and
// questions. The defaults reach the web model ONLY through the page contract's
// `wsop.capture.defaults` copy string (never a backend JSON import); the test lifts that same
// string out of service.go so the two are pinned equal without the model reading it.
import { CAPTURE_DEFAULTS_COPY_KEY, MAX_CAPTURE_QUESTIONS, MAX_INDIVIDUAL_PROOF_SLOTS, MAX_LUMP_SUM_PROOFS_TOTAL, MAX_LUMP_SUM_PROOF_SLOTS, blankCountedSlot, parseCaptureDefaults, withCaptureDefaults } from "./weighing-model.ts";

const serviceGo = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");
const defaultsJSON = /const weighingCaptureSlotDefaultsJSON = `([\s\S]*?)`/.exec(serviceGo)[1];
const captureDefaults = parseCaptureDefaults(defaultsJSON);
const seedDoc = () => JSON.parse(readFileSync(seedPath, "utf8"));

test("the capture defaults copy key the screens read is declared in the backend copy map (its value is a Go constant the copy-keys guard cannot see)", () => {
  assert.equal(CAPTURE_DEFAULTS_COPY_KEY, "wsop.capture.defaults");
  assert.match(serviceGo, /"wsop\.capture\.defaults":\s*weighingCaptureSlotDefaultsJSON/);
  for (const name of ["weighing-editor.tsx", "weighing-summary.tsx"]) {
    const source = readFileSync(new URL(name, import.meta.url), "utf8");
    assert.match(source, /copy\(pc, CAPTURE_DEFAULTS_COPY_KEY, ""\)/, name);
  }
  assert.match(captureDefaults.individual.proofs[0].hint, /scale reading 0 kg/);
  assert.match(captureDefaults.lump_sum.proofs[0].hint, /scale reading 0 kg/);
});

test("capture defaults: the seed derives animal_video and pen_video 1..5 from the copy defaults and an untouched seed still emits the seed", () => {
  const rows = parseWeighing({ weighing: seedDoc() }, captureDefaults);
  assert.deepEqual(rows.individualProofs.map((p) => [p.key, p.kind, p.required]), [["animal_video", "video", true]]);
  assert.equal(rows.individualProofsExplicit, false);
  assert.deepEqual(rows.lumpSumProofs.map((p) => [p.key, p.kind, p.min, p.max]), [["pen_video", "video", "1", "5"]]);
  assert.equal(rows.lumpSumProofsExplicit, false);
  assert.deepEqual(rows.individualQuestions, []);
  assert.deepEqual(rows.lumpSumQuestions, []);
  assert.equal(canonical(emitWeighing(rows)), canonical(seedDoc()));
  assert.deepEqual(weighingProblems(rows), []);
  // A document carrying its own window keeps it on the derived pen_video slot.
  const narrow = seedDoc();
  narrow.capture.lump_sum = { video_min: 2, video_max: 3 };
  const narrowRows = parseWeighing({ weighing: narrow }, captureDefaults);
  assert.deepEqual(narrowRows.lumpSumProofs.map((p) => [p.min, p.max]), [["2", "3"]]);
  assert.equal(canonical(emitWeighing(narrowRows)), canonical(narrow));
  // The editor receives rows parsed without the copy defaults and applies them afterwards.
  const bare = parseWeighing({ weighing: seedDoc() });
  const applied = withCaptureDefaults(bare, captureDefaults);
  assert.equal(applied.individualProofs[0].hint, captureDefaults.individual.proofs[0].hint);
  assert.equal(canonical(emitWeighing(applied)), canonical(seedDoc()));
  // The built-in fallback (copy absent) still yields both sections.
  const fallback = parseWeighing({ weighing: seedDoc() });
  assert.equal(fallback.individualProofs.length, 1);
  assert.equal(fallback.lumpSumProofs.length, 1);
  assert.equal(canonical(emitWeighing(fallback)), canonical(seedDoc()));
});

function explicitDoc() {
  const doc = seedDoc();
  doc.capture = {
    individual: {
      video_required: true,
      proofs: [
        { key: "animal_video", title: "Weighing video", hint: "On the scale.", kind: "video", required: true },
        { key: "ear_tag", title: "Ear tag close-up", kind: "photo", required: false },
      ],
      questions: [{ id: "limping", kind: "choice", title: "Limping?", required: true, options: [{ value: "yes", label: "Yes" }, { value: "no", label: "No" }] }],
    },
    lump_sum: {
      // The window is the mirror of the video slots (2..4); the backend serves it that way too.
      video_min: 2,
      video_max: 4,
      proofs: [
        { key: "pen_video", title: "Weighing video", hint: "Pen on the scale.", kind: "video", min: 2, max: 4 },
        { key: "scale_photo", title: "Scale reading", kind: "photo", min: 1, max: 2 },
      ],
      questions: [{ id: "all_on", kind: "choice", title: "Every animal on the scale?", required: true, options: [{ value: "yes", label: "Yes" }, { value: "no", label: "No" }] }],
    },
  };
  return doc;
}

test("a document with explicit capture slots and questions round-trips byte-faithfully, min/max included", () => {
  const doc = explicitDoc();
  const rows = parseWeighing({ weighing: doc }, captureDefaults);
  assert.equal(rows.individualProofsExplicit, true);
  assert.equal(rows.lumpSumProofsExplicit, true);
  assert.deepEqual(rows.individualProofs.map((p) => [p.key, p.kind, p.required]), [["animal_video", "video", true], ["ear_tag", "photo", false]]);
  assert.deepEqual(rows.lumpSumProofs.map((p) => [p.key, p.kind, p.min, p.max]), [["pen_video", "video", "2", "4"], ["scale_photo", "photo", "1", "2"]]);
  assert.equal(rows.individualQuestions[0].key, "limping");
  assert.equal(rows.lumpSumQuestions[0].key, "all_on");
  assert.deepEqual(weighingProblems(rows), []);
  assert.equal(canonical(emitWeighing(rows)), canonical(doc));
});

test("adding a whole-pen photo slot emits explicit whole-pen slots and the derived video window mirror", () => {
  const rows = parseWeighing({ weighing: seedDoc() }, captureDefaults);
  rows.lumpSumProofs.push({ ...blankCountedSlot(), key: "scale_photo", title: "Scale reading", kind: "photo", min: "1", max: "2" });
  assert.deepEqual(weighingProblems(rows), []);
  const out = emitWeighing(rows);
  assert.deepEqual(out.capture.lump_sum.proofs.map((p) => [p.key, p.kind, p.min, p.max]), [["pen_video", "video", 1, 5], ["scale_photo", "photo", 1, 2]]);
  assert.deepEqual(out.capture.lump_sum.questions, []);
  // The mirror counts video / either slots only: 1..5 from pen_video, the photo does not add.
  assert.equal(out.capture.lump_sum.video_min, 1);
  assert.equal(out.capture.lump_sum.video_max, 5);
  // The per-animal section was not touched, so it stays as the seed wrote it.
  assert.deepEqual(out.capture.individual, { video_required: true });
  // Two video slots 1..3 + 1..3 mirror to 2..5 (clamped to the 5 ceiling).
  rows.lumpSumProofs = [
    { ...blankCountedSlot(), key: "front", title: "Front", kind: "video", min: "1", max: "3" },
    { ...blankCountedSlot(), key: "side", title: "Side", kind: "either", min: "1", max: "3" },
  ];
  const two = emitWeighing(rows);
  assert.equal(two.capture.lump_sum.video_min, 2);
  assert.equal(two.capture.lump_sum.video_max, 5);
  // Emitted explicit slots round-trip.
  assert.equal(canonical(emitWeighing(parseWeighing({ weighing: two }, captureDefaults))), canonical(two));
});

test("capture pre-checks name the rule: compulsory per-animal slot, slot ceilings, whole-pen min/max, total, question ceiling", () => {
  const fresh = () => parseWeighing({ weighing: seedDoc() }, captureDefaults);
  const has = (rows, want) => assert.ok(weighingProblems(rows).some((p) => p.includes(want)), `missing problem containing: ${want}\n${weighingProblems(rows).join("\n")}`);
  let rows = fresh();
  rows.individualProofs[0].required = false;
  has(rows, "Per animal: at least one capture must be compulsory");
  rows = fresh();
  rows.individualProofs = [];
  has(rows, "Per animal: needs at least one capture");
  rows = fresh();
  for (let i = 0; i < MAX_INDIVIDUAL_PROOF_SLOTS; i += 1) rows.individualProofs.push({ ...blankProofSlot(), key: `extra_${i}`, title: `Extra ${i}` });
  has(rows, `Per animal: at most ${MAX_INDIVIDUAL_PROOF_SLOTS} captures`);
  rows = fresh();
  rows.individualProofs.push({ ...blankProofSlot(), key: "Bad Key", title: "Bad" });
  has(rows, "Per animal, capture 2: key must be lowercase letters, digits and underscores");
  rows = fresh();
  rows.lumpSumProofs[0].min = "4";
  rows.lumpSumProofs[0].max = "2";
  has(rows, "Whole pen, capture 1: at least must not exceed at most");
  rows = fresh();
  rows.lumpSumProofs[0].max = "6";
  has(rows, "Whole pen, capture 1: at most must be a whole number from 1 to 5");
  rows = fresh();
  rows.lumpSumProofs[0].min = "-1";
  has(rows, "Whole pen, capture 1: at least must be a whole number of 0 or more");
  rows = fresh();
  rows.lumpSumProofs[0].min = "0";
  has(rows, "Whole pen: at least one capture must have a minimum of 1");
  rows = fresh();
  rows.lumpSumProofs = [];
  has(rows, "Whole pen: needs at least one capture");
  rows = fresh();
  for (let i = 0; i < MAX_LUMP_SUM_PROOF_SLOTS; i += 1) rows.lumpSumProofs.push({ ...blankCountedSlot(), key: `extra_${i}`, title: `Extra ${i}` });
  has(rows, `Whole pen: at most ${MAX_LUMP_SUM_PROOF_SLOTS} captures`);
  rows = fresh();
  rows.lumpSumProofs.push({ ...blankCountedSlot(), key: "photo_a", title: "A", kind: "photo", min: "1", max: "5" });
  rows.lumpSumProofs.push({ ...blankCountedSlot(), key: "photo_b", title: "B", kind: "photo", min: "0", max: "1" });
  has(rows, `Whole pen: at most ${MAX_LUMP_SUM_PROOFS_TOTAL} captures per pen in total`);
  rows = fresh();
  rows.lumpSumProofs.push({ ...blankCountedSlot(), key: "pen_video", title: "Again" });
  has(rows, 'Whole pen, capture 2: key "pen_video" is used twice');
  rows = fresh();
  rows.individualQuestions = Array.from({ length: MAX_CAPTURE_QUESTIONS + 1 }, (_, i) => ({ ...blankQuestion("text"), key: `q${i}`, title: `Q ${i}` }));
  has(rows, `Per-animal questions: at most ${MAX_CAPTURE_QUESTIONS}`);
  rows = fresh();
  rows.lumpSumQuestions = Array.from({ length: MAX_CAPTURE_QUESTIONS + 1 }, (_, i) => ({ ...blankQuestion("text"), key: `q${i}`, title: `Q ${i}` }));
  has(rows, `Whole-pen questions: at most ${MAX_CAPTURE_QUESTIONS}`);
  rows = fresh();
  rows.individualQuestions = [{ ...blankQuestion("text"), key: "why", title: "" }];
  rows.lumpSumQuestions = [{ ...blankQuestion("number"), key: "n", title: "N", min: "5", max: "2" }];
  has(rows, "Per-animal question 1: needs the question text");
  has(rows, "Whole-pen question 1: min must not exceed max");
  // The legacy window is judged only while the whole-pen slots are derived (not emitted).
  rows = fresh();
  rows.lumpSumVideoMin = "4";
  rows.lumpSumVideoMax = "9";
  has(rows, "at most 5");
  rows.lumpSumProofs.push({ ...blankCountedSlot(), key: "scale_photo", title: "Scale", kind: "photo", min: "0", max: "1" });
  assert.deepEqual(weighingProblems(rows), []);
});

test("per-animal and whole-pen captures are authored independently and land under their own section", () => {
  const rows = parseWeighing({ weighing: seedDoc() }, captureDefaults);
  // Edit ONLY the per-animal list: a photo slot and a question.
  rows.individualProofs.push({ ...blankProofSlot(), key: "ear_tag", title: "Ear tag", kind: "photo", required: false });
  rows.individualQuestions = [{ ...blankQuestion("choice"), key: "limping", title: "Limping?" }];
  assert.deepEqual(weighingProblems(rows), []);
  const out = emitWeighing(rows);
  assert.deepEqual(out.capture.individual.proofs.map((p) => p.key), ["animal_video", "ear_tag"]);
  assert.deepEqual(out.capture.individual.questions.map((q) => q.id), ["limping"]);
  assert.equal(out.capture.individual.video_required, true);
  // The whole-pen section is untouched: emitted exactly as the seed wrote it, no slots, no questions.
  assert.deepEqual(out.capture.lump_sum, seedDoc().capture.lump_sum);
  assert.deepEqual(rows.lumpSumProofs.map((p) => p.key), ["pen_video"]);
  assert.deepEqual(rows.lumpSumQuestions, []);
  // And the other way round: a whole-pen question leaves the per-animal section as the seed's.
  const rows2 = parseWeighing({ weighing: seedDoc() }, captureDefaults);
  rows2.lumpSumQuestions = [{ ...blankQuestion("choice"), key: "all_on", title: "Every animal on the scale?" }];
  const out2 = emitWeighing(rows2);
  assert.deepEqual(out2.capture.individual, { video_required: true });
  assert.deepEqual(out2.capture.lump_sum.questions.map((q) => q.id), ["all_on"]);
  assert.deepEqual(out2.capture.lump_sum.proofs.map((p) => p.key), ["pen_video"]);
  // A parsed explicit document keeps the two lists apart by key.
  const back = parseWeighing({ weighing: out }, captureDefaults);
  assert.deepEqual(back.individualProofs.map((p) => p.key), ["animal_video", "ear_tag"]);
  assert.deepEqual(back.lumpSumProofs.map((p) => p.key), ["pen_video"]);
  assert.notDeepEqual(back.individualProofs.map((p) => p.key), back.lumpSumProofs.map((p) => p.key));
});
