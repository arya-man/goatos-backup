import assert from "node:assert/strict";
import test from "node:test";
import { resolveSexFilter, sexControlValue, sexFilterFromUrl, sexLabel, sexSentenceKey } from "./sex-filter.ts";

// The farm's genders as Configuration lists them, with a third one added on Items & settings.
const choices = [
  { value: "female", label: "Female" },
  { value: "male", label: "Male" },
  { value: "castrated", label: "Castrated male" },
];

test("the Sex filter offers the farm's genders: male by default, all explicit, a third gender honoured", () => {
  assert.equal(resolveSexFilter(undefined, choices), "male", "absent means male (maintainer request 2026-09-01)");
  assert.equal(resolveSexFilter("all", choices), "", "all means every kid");
  assert.equal(resolveSexFilter("female", choices), "female");
  assert.equal(resolveSexFilter("castrated", choices), "castrated", "a gender added on Configuration is a real filter");
  assert.equal(resolveSexFilter("goat", choices), "male", "a value that is not one of the farm's genders falls back");
  assert.equal(sexControlValue(""), "all");
  assert.equal(sexControlValue("castrated"), "castrated");
});

test("without a contract, any well-shaped gender code passes and a malformed one falls back", () => {
  assert.equal(sexFilterFromUrl("castrated"), "castrated");
  assert.equal(sexFilterFromUrl("all"), "");
  assert.equal(sexFilterFromUrl(undefined), "male");
  assert.equal(sexFilterFromUrl("Male!"), "male");
});

test("a third gender reads the generic sentence with its own name, never a missing key or a raw code", () => {
  const keys = { all: "caption", perSex: (sex) => `caption_${sex}` };
  assert.deepEqual(sexSentenceKey("", keys, choices), { key: "caption", sex: "" });
  assert.deepEqual(sexSentenceKey("male", keys, choices), { key: "caption_male", sex: "" });
  assert.deepEqual(sexSentenceKey("castrated", keys, choices), { key: "caption_other", sex: "castrated male" });
  assert.equal(sexLabel("castrated", choices), "Castrated male");
  assert.equal(sexLabel("unlisted", choices), "unlisted");
});
