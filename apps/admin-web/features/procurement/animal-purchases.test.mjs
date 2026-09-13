import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const page = readFileSync(new URL("./animal-purchases.tsx", import.meta.url), "utf8");
const form = readFileSync(new URL("./animal-purchase-decision-form.tsx", import.meta.url), "utf8");
const action = readFileSync(new URL("./animal-purchase-actions.ts", import.meta.url), "utf8");

test("each animal mints a signed video link only after the reviewer opens that preview", () => {
  assert.match(page, /getAnimalPurchaseMedia\(previewId\)/);
  assert.match(page, /preview_id: animal\.candidate_id/);
  assert.match(page, /previewMedia\?\.candidate_id === animal\.candidate_id/);
  assert.match(page, /src=\{previewMedia\.media_url\}/);
  assert.doesNotMatch(page, /src=\{animal\.media_url\}/);
});

test("the decision form renders only for a pending animal behind the backend control", () => {
  const branch = page.slice(page.indexOf('animal.decision !== "pending"'), page.indexOf("</article>"));
  assert.ok(branch.includes("AnimalPurchaseDecisionForm"), "the form sits in the pending branch");
  // Decided rows show who decided and when; the form is never offered to them.
  assert.match(branch, /decision\.by/);
  assert.match(branch, /fmtDateTime\(animal\.decided_at\)/);
  // A pending row for a principal without the control sees the backend reason, not a button.
  assert.match(page, /controlEnabled\(pageContract, DECISION_CONTROL, false\)/);
  assert.match(page, /const DECISION_CONTROL = "decide_animal_purchase"/);
  assert.match(branch, /\) : canDecide \? \(/);
  assert.match(branch, /verdict\.disabled_no_access/);
  assert.doesNotMatch(page, /ceo_internal|role ===|grants/);
});

test("reject waits for a note and both buttons post the one derived-key action", () => {
  assert.match(form, /name="decision" value="accept"/);
  assert.match(form, /name="decision"\s+value="reject"/);
  assert.match(form, /disabled=\{pending \|\| !noteReady\}/);
  assert.match(action, /`animal-purchase-\$\{candidateId\}-\$\{rowVersion\}-\$\{decision\}`/);
  assert.doesNotMatch(action, /randomUUID/);
  assert.match(action, /revalidatePath\(PATHNAME\)/);
  // Feedback codes are copy-key suffixes the page resolves through the contract.
  for (const code of ["decided_accepted", "decided_rejected", "decide_failed", "decide_conflict", "error_form"]) {
    assert.ok(action.includes(`"${code}"`), `${code} is emitted by the action`);
  }
  assert.match(page, /copy\(pageContract, `action\.\$\{feedback\.code \?\? ""\}`, ""\)/);
});

test("no visible string says shed", () => {
  const visible = [...page.matchAll(/>([^<>{}]+)</g)].map((m) => m[1]);
  for (const text of visible) assert.doesNotMatch(text, /shed/i);
  assert.doesNotMatch(form, /shed/i);
});
