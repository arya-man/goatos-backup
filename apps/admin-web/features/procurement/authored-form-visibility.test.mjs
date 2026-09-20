import assert from "node:assert/strict";
import test from "node:test";
import { visibleQuestionIds } from "./authored-form-visibility.ts";

test("cross-page typed parent masks stale children from rendered controls and submitted ids", () => {
  const pages = [
    [{ id: "payment_status" }],
    [{ id: "b", only_if: { question_id: "payment_status", value: "Paid" } }],
    [{ id: "c", only_if: { question_id: "b", value: "yes" } }],
  ];
  const answers = { payment_status: "Pending", b: "yes", c: "old detail" };
  const ids = visibleQuestionIds(pages.flat(), answers);
  assert.deepEqual([...ids], ["payment_status"]);
  const submission = Object.fromEntries([...ids].map((id) => [id, answers[id]]));
  assert.deepEqual(submission, { payment_status: "Pending" });
  assert.deepEqual([...visibleQuestionIds(pages.flat(), { ...answers, payment_status: "Paid" })], ["payment_status", "b", "c"]);
});

test("a blank visible parent does not activate its child", () => {
  assert.deepEqual([...visibleQuestionIds([{id:"a"},{id:"b",only_if:{question_id:"a",value:"yes"}}], {})], ["a"]);
});

test("invalid blank condition never activates for an absent or blank optional parent", () => {
  const questions = [{id:"a"},{id:"b",only_if:{question_id:"a",value:""}}];
  for (const answers of [{b:"stale"},{a:"",b:"stale"}]) {
    assert.deepEqual([...visibleQuestionIds(questions, answers)], ["a"]);
  }
});
