import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { buildPenVocabulary, classifyPenLabel, composePenLabel, penLabelFindings } from "./pen-label-checks.mjs";
import { REGRESSION_PATTERNS, penLabelVocabulary } from "./regression-checks.mjs";

const manifest = JSON.parse(readFileSync(new URL("./pen-label-vocabulary.json", import.meta.url), "utf8"));
const vocab = buildPenVocabulary(manifest);
const verdict = (text) => classifyPenLabel(text, vocab)?.pattern ?? null;

// ---------------------------------------------------------------------------------------------
// The vocabulary is the farm's data, not a guess.
// ---------------------------------------------------------------------------------------------
test("the vocabulary carries the real sheds and both naming conventions", () => {
  const names = manifest.sheds.map((s) => s.name);
  for (const shed of ["Castro", "Gandhi", "Ho Chi Minh", "Old Yashoda", "Yashoda", "Godel 1", "Godel 2", "Mandela 1", "Mandela 2", "Sumathi 1", "Sumathi 2"]) {
    assert.ok(names.includes(shed), `vocabulary is missing ${shed}`);
  }
  // Every shed on this farm is partitioned. If that ever stops being true the generator must
  // say so in the data, never this file.
  for (const shed of manifest.sheds) assert.ok(shed.labels.length > 0, `${shed.name} has no partitions`);
  assert.equal(manifest.sheds.find((s) => s.name === "Castro").convention, "numeric");
  assert.equal(manifest.sheds.find((s) => s.name === "Godel 1").convention, "worded");
});

test("the composer applies the documented separator rule", () => {
  assert.equal(composePenLabel("Castro", "2"), "Castro 2"); // bare numeral -> SPACE
  assert.equal(composePenLabel("Godel 1", "Part 3"), "Godel 1 - Part 3"); // worded -> DASH
  assert.equal(composePenLabel("Yashoda", ""), "Yashoda");
  assert.equal(composePenLabel("Yashoda", "whole"), "Yashoda");
});

// ---------------------------------------------------------------------------------------------
// FIRES — the four shapes reported from the floor, plus the sentinel leak.
// ---------------------------------------------------------------------------------------------
test("fires on a doubled worded partition", () => {
  assert.equal(verdict("Godel 1 - Part 1 - Part 1"), "P-pen-part-doubled");
  assert.equal(verdict("Mandela 1 - Part 10 - Part 10"), "P-pen-part-doubled");
  assert.equal(verdict("Sumathi 2 - Part 6 - Part 6"), "P-pen-part-doubled");
});

test("fires on a doubled numeral", () => {
  assert.equal(verdict("Castro 1 1"), "P-pen-number-doubled");
  assert.equal(verdict("Yashoda 10 10"), "P-pen-number-doubled");
  // The shed name already ends in the numeral that was appended a second time.
  assert.equal(verdict("Godel 1 1"), "P-pen-number-doubled");
  assert.equal(verdict("Mandela 2 2"), "P-pen-number-doubled");
});

test("fires on the wrong separator for the convention", () => {
  assert.equal(verdict("Godel 1 Part 3"), "P-pen-separator-wrong"); // worded needs a dash
  assert.equal(verdict("Sumathi 1 Part 2"), "P-pen-separator-wrong");
  assert.equal(verdict("Castro - 2"), "P-pen-separator-wrong"); // numeric needs a space
  assert.equal(verdict("Old Yashoda - 3"), "P-pen-separator-wrong");
});

test("fires on the whole sentinel reaching a label", () => {
  assert.equal(verdict("Yashoda whole"), "P-pen-whole-leaked");
  assert.equal(verdict("Castro - whole"), "P-pen-whole-leaked");
  assert.equal(verdict("Godel 2 whole"), "P-pen-whole-leaked");
});

// ---------------------------------------------------------------------------------------------
// DOES NOT FIRE — a check that flags a correct label is worse than no check at all.
// ---------------------------------------------------------------------------------------------
test("stays silent on correct labels", () => {
  for (const good of [
    "Castro 2", "Castro 1", "Gandhi 3", "Ho Chi Minh 1", "Yashoda 1", "Yashoda 10", "Old Yashoda 3",
    "Godel 1 - Part 3", "Godel 2 - Part 7", "Mandela 1 - Part 4", "Mandela 2 - Part 9",
    "Sumathi 1 - Part 2", "Sumathi 2 - Part 6",
  ]) {
    assert.equal(verdict(good), null, `false positive on the correct label "${good}"`);
  }
});

test("a genuine shed name ending in a digit is not read as shed + partition", () => {
  // "Godel 1" is a BUILDING, not "Godel" partition 1. Six live shed names end in a digit,
  // which is the whole reason worded partitions take a dash.
  for (const shed of ["Godel 1", "Godel 2", "Mandela 1", "Mandela 2", "Sumathi 1", "Sumathi 2"]) {
    const v = classifyPenLabel(shed, vocab);
    assert.equal(v?.pattern, undefined, `${shed} must not be reported as a malformed label`);
    assert.equal(v?.pending, true, `${shed} bare is only evidence beside partitioned siblings`);
  }
  // And a partition of one of them is clean.
  assert.equal(verdict("Godel 1 - Part 1"), null);
});

test("Yashoda and Old Yashoda are told apart (longest shed name wins)", () => {
  assert.equal(verdict("Old Yashoda 3"), null);
  assert.equal(verdict("Old Yashoda 5"), null);
  assert.equal(verdict("Yashoda 3"), null);
  // "Old Yashoda 3" must resolve to the Old Yashoda shed, not to Yashoda.
  assert.equal(classifyPenLabel("Old Yashoda 3", vocab), null);
  assert.equal(classifyPenLabel("Old Yashoda", vocab).shed, "Old Yashoda");
});

test("stays silent on text that merely mentions a pen", () => {
  for (const prose of [
    "Castro 1 has 42 animals",
    "Weighed Godel 1 - Part 3 this morning",
    "the whole herd",
    "Move the whole",
    "Part 3",
    "Part 1 - Part 2",
    "12 12",
    "Castrol drum 2",
    "",
  ]) {
    assert.equal(verdict(prose), null, `false positive on "${prose}"`);
  }
});

test("a well-formed label the vocabulary has not heard of is not a bug", () => {
  // A new pen was added on the farm before the vocabulary was refreshed. Staleness must
  // never invent a defect.
  assert.equal(verdict("Yashoda 11"), null);
  assert.equal(verdict("Godel 1 - Part 11"), null);
});

// ---------------------------------------------------------------------------------------------
// The within-screen comparison: "just godel1 without partition".
// ---------------------------------------------------------------------------------------------
const rows = (texts, group = "t0|c0") => texts.map((text, index) => ({ index, text, group }));

test("fires on a bare shed rendered beside its own partitioned siblings", () => {
  const found = penLabelFindings(rows(["Godel 1 - Part 1", "Godel 1 - Part 2", "Godel 1"]), manifest);
  assert.equal(found.length, 1);
  assert.equal(found[0].pattern, "P-pen-partition-missing");
  assert.equal(found[0].text, "Godel 1");
  assert.match(found[0].detail, /no part number/);
});

test("a shed-grain column where every entry is bare is left alone", () => {
  // Shed-wise charts and shed filters legitimately render the building, not the pen.
  // Flagging them would bury the real defect in noise.
  assert.deepEqual(penLabelFindings(rows(["Castro", "Gandhi", "Yashoda", "Godel 1"]), manifest), []);
});

test("one partitioned sibling is not enough to accuse a bare name", () => {
  assert.deepEqual(penLabelFindings(rows(["Castro 1", "Castro"]), manifest), []);
});

test("a bare shed in a DIFFERENT column than its partitioned siblings is not accused", () => {
  const candidates = [
    ...rows(["Castro 1", "Castro 2"], "t0|c0"),
    { index: 9, text: "Castro", group: "t0|c3" },
  ];
  assert.deepEqual(penLabelFindings(candidates, manifest), []);
});

test("a whole screen of correct pens produces nothing", () => {
  const found = penLabelFindings(rows([
    "Castro 1", "Castro 2", "Castro 3", "Yashoda 1", "Yashoda 10", "Old Yashoda 4",
    "Godel 1 - Part 3", "Mandela 2 - Part 9", "Sumathi 1 - Part 2", "Ho Chi Minh 2",
  ]), manifest);
  assert.deepEqual(found, []);
});

test("every reported finding carries the offending text and a plain-English detail", () => {
  const found = penLabelFindings(rows(["Godel 1 - Part 1 - Part 1", "Castro 1 1", "Yashoda whole"]), manifest);
  assert.equal(found.length, 3);
  for (const f of found) {
    assert.ok(f.text, "a finding must quote the label it saw");
    assert.ok(/pen "/.test(f.detail), "a finding must read as plain English about a pen");
    assert.ok(!/selector|querySelector|data-/.test(f.detail), "no selectors in an operator-facing detail");
  }
});

// ---------------------------------------------------------------------------------------------
// Wiring: lane 1 declares these patterns and loads the farm's vocabulary.
// ---------------------------------------------------------------------------------------------
test("lane 1 declares every pen-label pattern this file can emit", () => {
  for (const name of ["P-pen-part-doubled", "P-pen-number-doubled", "P-pen-separator-wrong", "P-pen-whole-leaked", "P-pen-partition-missing"]) {
    assert.ok(REGRESSION_PATTERNS[name], `declared: ${name}`);
  }
});

test("the vocabulary loads through the lane-1 entry point", () => {
  const loaded = penLabelVocabulary();
  assert.ok(loaded, "pen-label-vocabulary.json must be readable from regression-checks.mjs");
  assert.equal(loaded.sheds.length, manifest.sheds.length);
});

test("the in-page collector is self-contained enough to serialise", async () => {
  const { collectPenLabelCandidates } = await import("./pen-label-checks.mjs");
  const source = collectPenLabelCandidates.toString();
  // page.evaluate ships the function SOURCE into the browser; a reference to anything at
  // module scope would arrive undefined and the check would silently collect nothing.
  for (const moduleScoped of ["composePenLabel", "buildPenVocabulary", "classifyPenLabel", "norm(", "fold("]) {
    assert.ok(!source.includes(moduleScoped), `collector must not close over ${moduleScoped}`);
  }
  assert.match(source, /data-pen-candidate/);
  assert.match(source, /cellIndex/);
});
