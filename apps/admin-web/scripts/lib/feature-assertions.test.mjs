import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { WRITE_WORDS, loadFeatureAssertions } from "./feature-assertions.mjs";

test("feature assertion steps refuse anything that writes", () => {
  for (const word of ["Save", "Approve", "Reject", "Delete", "Submit", "Upload", "Download", "Assign", "Publish", "Mark reached", "Create task"]) {
    assert.ok(WRITE_WORDS.test(word), word);
  }
  for (const word of ["Stock", "List", "Flow", "Overdue", "Filters", "Status-wise"]) assert.ok(!WRITE_WORDS.test(word), word);
});

test("feature assertions manifest loads and every runnable entry names a route and an expectation", () => {
  for (const entry of loadFeatureAssertions()) {
    assert.ok(entry.route, entry.sha);
    assert.ok(Array.isArray(entry.expect) && entry.expect.length > 0, entry.sha);
    for (const step of entry.steps ?? []) assert.ok(!WRITE_WORDS.test(step.click?.text ?? step.click?.css ?? ""), `${entry.sha} step writes`);
  }
});

test("smoke runs feature assertions on every route after overlays", () => {
  const smoke = readFileSync(new URL("../smoke-visual-live.mjs", import.meta.url), "utf8");
  assert.match(smoke, /await check\(\(\) => assertFeaturesPresent\(page,/);
});
