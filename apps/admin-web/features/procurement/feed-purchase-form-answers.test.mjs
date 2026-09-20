import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { readFormAnswers } from "./feed-purchase-form-answers.ts";

const seed = JSON.parse(readFileSync(new URL("../../../../backend/internal/procurement/domain/feedformseed/feed_purchase.json", import.meta.url), "utf8"));
function submission(ids = seed.pages.flatMap(p => p.questions.map(q => q.id))) {
  const data = new FormData();
  data.set("questionnaire_version", "1");
  ids.forEach(id => data.append("questionnaire_question", id));
  data.set("purchase_date", "2026-09-21");
  data.set("farm", "CBE");
  data.set("feed_item", "Maize");
  data.set("quantity_kg", "100");
  data.set("vendor", "Supplier");
  data.set("batch_no", "1000");
  return data;
}
test("manual load number stays a ledger column when the seeded form does not ask it", () => {
  const data = submission();
  const result = readFormAnswers(data);
  assert.equal(data.get("batch_no"), "1000");
  assert.equal(result.answers.batch_no, undefined);
  assert.equal(result.answers.farm_label, "CBE");
  assert.equal(result.answers.feed_item_label, "Maize");
  assert.equal(result.questionnaire_version, 1);
});
test("an authored batch question is included, and a retired typed question is omitted", () => {
  const ids = seed.pages.flatMap(p => p.questions.map(q => q.id)).filter(id => id !== "transport_cost");
  const data = submission([...ids, "batch_no"]);
  data.set("transport_cost", "50");
  data.append("sop.certificates", "organic");
  data.append("sop.certificates", "tested");
  assert.equal(readFormAnswers(data).answers.batch_no, "1000");
  assert.equal(readFormAnswers(data).answers.transport_cost, undefined);
  assert.equal(readFormAnswers(data).answers.certificates, "organic|tested");
});
test("an authored other choice carries the explanation under the backend sidecar key", () => {
  const data = submission();
  data.append("sop.delivery_mode", "other");
  data.set("sop.delivery_mode_other", "Night unload");
  const result = readFormAnswers(data);
  assert.equal(result.answers.delivery_mode, "other");
  assert.equal(result.answers.delivery_mode_other, "Night unload");
});
test("legacy pages without a form version retain the typed-only write", () => {
  const data = submission();
  data.delete("questionnaire_version");
  assert.equal(readFormAnswers(data), null);
});
