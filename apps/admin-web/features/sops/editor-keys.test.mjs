// E2E 2026-09-17 (feed SOP editor): a NEW capture or question typed one key at a time froze its key at
// the first letter ("Trough photo" -> "t"), because `key || slugKey(title)` never looked at the title
// again. keyForTitle (weighing-model.ts) fixed the feed editor; every other SOP editor that derives a
// new slot / question / page key from its title must use it too, and a key that follows its title
// must take its dependents' "ask only when" with it.
import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { followQuestionKey, keyForTitle } from "./weighing-model.ts";

const editors = ["shifting-editor.tsx", "weighing-editor.tsx", "capture-editor.tsx", "inspection-editor.tsx", "feed-editor.tsx", "pc-care-editor.tsx"];

test("no SOP editor freezes a new key at the title's first keystroke", () => {
  for (const file of editors) {
    const src = readFileSync(new URL(`./${file}`, import.meta.url), "utf8");
    assert.doesNotMatch(src, /\.key \|\| slugKey\(title/, `${file} still derives a new key as key || slugKey(title)`);
  }
});

test("editors that render slots or questions hand the loaded keys to them", () => {
  for (const file of ["shifting-editor.tsx", "weighing-editor.tsx", "capture-editor.tsx", "inspection-editor.tsx", "pc-care-editor.tsx"]) {
    const src = readFileSync(new URL(`./${file}`, import.meta.url), "utf8");
    assert.match(src, /savedKeys/, `${file} must know which keys the loaded version already carries`);
  }
});

test("a new key typed letter by letter ends as the whole title", () => {
  let key = "";
  for (const title of ["G", "Ga", "Gate latched"]) key = keyForTitle(title, key, new Set(["pen_video"]), new Set(["pen_video", key]), "capture");
  assert.equal(key, "gate_latched");
});

test("a question key that follows its title takes its dependents' only_if with it", () => {
  const rows = [
    { id: "a", key: "g", title: "G", onlyIfQuestion: "", onlyIfValue: "" },
    { id: "b", key: "notes", title: "Notes", onlyIfQuestion: "g", onlyIfValue: "yes" },
    { id: "c", key: "other", title: "Other", onlyIfQuestion: "", onlyIfValue: "" },
  ];
  const next = followQuestionKey(rows, "a", { title: "Gate", key: "gate" });
  assert.deepEqual(next.map((r) => [r.key, r.onlyIfQuestion]), [["gate", ""], ["notes", "gate"], ["other", ""]]);
  // A patch that does not move the key touches only its own row.
  assert.equal(followQuestionKey(rows, "c", { title: "More" })[1].onlyIfQuestion, "g");
});

// The herd follow-up editor had the same freeze: a new step's key was set on the first keystroke and
// `step.key ? { title } : ...` never looked at the title again.
import { followStepKey } from "./followup-model.ts";
test("a new herd step's key follows its title and takes requires / after-step with it", () => {
  const src = readFileSync(new URL("./followup-editor.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(src, /step\.key \? \{ title \} : \{ title, key: slugKey/, "followup-editor still freezes a new step key");
  assert.match(src, /keyForTitle\(title, step\.key, savedKeys/, "followup-editor must derive a new step key with keyForTitle");
  const steps = [
    { id: "a", key: "c", requires: [], afterStep: "" },
    { id: "b", key: "notes", requires: ["c"], afterStep: "c" },
  ];
  const next = followStepKey(steps, "a", { title: "Check water", key: "check_water" });
  assert.deepEqual(next.map((s) => [s.key, s.requires, s.afterStep]), [["check_water", [], ""], ["notes", ["check_water"], "check_water"]]);
});
