// HERD OPERATIONS CAPTURE CARD: the editor model round-trips an authored card byte-faithfully,
// treats an absent section as the empty (plain) form, allows a questions-only card, and ignores
// SOP codes that carry no capture card.
import test from "node:test";
import assert from "node:assert/strict";
import { captureProblems, emitCaptureCard, hasCaptureCard, parseCaptureCard } from "./capture-model.ts";

const card = {
  schema_version: "goatos.sop-capture.v1",
  instruction: "Photograph the newborns with the mother.",
  proofs: [
    { key: "newborns_with_mother", title: "Newborns with the mother", kind: "photo", required: true },
    { key: "pen_clip", title: "Pen clip", hint: "Whole pen", kind: "either", required: false },
  ],
  questions: [
    { id: "delivery_type", kind: "choice", title: "How was the delivery?", required: true, options: [{ value: "normal", label: "Normal" }, { value: "assisted", label: "Assisted" }] },
    { id: "assisted_by", kind: "text", title: "Who assisted?", required: true, only_if: { question_id: "delivery_type", value: "assisted" } },
  ],
};

test("an authored birth card round-trips unchanged", () => {
  const rows = parseCaptureCard("counts.birth", { follow_up: {}, capture_card: card });
  assert.ok(rows);
  assert.deepEqual(emitCaptureCard(rows), card);
  assert.deepEqual(captureProblems(rows, "Captures", "Question"), []);
});

test("an absent section is the empty card and a questions-only card is valid", () => {
  const empty = parseCaptureCard("counts.death", { follow_up: {} });
  assert.deepEqual(empty, { instruction: "", proofs: [], questions: [] });
  assert.equal(hasCaptureCard({ follow_up: {} }), false);
  const rows = parseCaptureCard("counts.death", { capture_card: { ...card, proofs: [] } });
  assert.deepEqual(captureProblems(rows, "Captures", "Question"), []);
  assert.deepEqual(emitCaptureCard(rows).proofs, []);
});

test("other SOP codes carry no capture card", () => {
  assert.equal(parseCaptureCard("shifting", { capture_card: card }), null);
  assert.equal(parseCaptureCard("feed.packing", { capture_card: card }), null);
});

test("pre-checks name duplicate keys and missing titles", () => {
  const rows = parseCaptureCard("counts.birth", { capture_card: card });
  rows.proofs[1].key = "newborns_with_mother";
  rows.questions[0].title = "";
  const problems = captureProblems(rows, "Captures", "Question");
  assert.ok(problems.some((p) => p.includes("used twice")), problems.join("\n"));
  assert.ok(problems.some((p) => p.includes("question text")), problems.join("\n"));
});
