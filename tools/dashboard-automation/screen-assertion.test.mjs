import test from "node:test";
import assert from "node:assert/strict";
import { evaluateScreenAssertion, pageFrom, findDoubledWord } from "./lib/screen-assertion.mjs";

test("a clause the evaluator cannot judge is refused, never passed", () => {
  const result = evaluateScreenAssertion({ visible: [{ text: "a" }], eventually: [] }, pageFrom("a"));
  assert.equal(result.ok, false);
});

test("absent is read, not ignored", () => {
  // The defect this closes: `absent` was declared on three journeys and never evaluated.
  assert.equal(evaluateScreenAssertion({ absent: [{ text: "Deleted" }] }, pageFrom("Deleted")).ok, false);
  assert.equal(evaluateScreenAssertion({ absent: [{ text: "Deleted" }] }, pageFrom("In use")).ok, true);
});

test("a doubled word is caught", () => {
  assert.equal(findDoubledWord("Godel 1 1 holds 9"), "1 1");
  assert.equal(findDoubledWord("Godel 1 - Part 3 holds 9"), null);
  assert.equal(evaluateScreenAssertion({ absent: [{ doubledWord: true }] }, pageFrom("Castro Castro")).ok, false);
});

test("inColumn requires the row to be under that column, not merely on the page", () => {
  const assertion = { visible: [{ text: "Automation check AC-1" }], inColumn: "In progress" };
  const onPageWrongColumn = pageFrom("To do: Automation check AC-1", { "In progress": "Order mineral mix" });
  assert.equal(evaluateScreenAssertion(assertion, onPageWrongColumn).ok, false);
  const right = pageFrom("In progress: Automation check AC-1", { "In progress": "Automation check AC-1" });
  assert.equal(evaluateScreenAssertion(assertion, right).ok, true);
});

test("a missing column is a failure, not a pass", () => {
  const assertion = { visible: [{ text: "x" }], inColumn: "In progress" };
  assert.equal(evaluateScreenAssertion(assertion, pageFrom("x")).ok, false);
});

test("notVisible and absent are the same promise and both are read", () => {
  const page = pageFrom("A draft is waiting");
  assert.equal(evaluateScreenAssertion({ notVisible: [{ text: "A draft is waiting" }] }, page).ok, false);
  assert.equal(evaluateScreenAssertion({ absent: [{ text: "A draft is waiting" }] }, page).ok, false);
});
