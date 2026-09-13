import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const page = readFileSync(new URL("./animal-purchases.tsx", import.meta.url), "utf8");
const form = readFileSync(new URL("./animal-purchase-decision-form.tsx", import.meta.url), "utf8");
const action = readFileSync(new URL("./animal-purchase-actions.ts", import.meta.url), "utf8");
const sop = readFileSync(new URL("./animal-purchase-sop.tsx", import.meta.url), "utf8");
const serverRead = readFileSync(new URL("../../lib/api/procurement-server.ts", import.meta.url), "utf8");

test("each animal mints a signed video link only after the reviewer opens that preview", () => {
  assert.match(page, /getAnimalPurchaseMedia\(previewId\)/);
  assert.match(page, /preview_id: animal\.candidate_id/);
  assert.match(page, /previewMedia\?\.candidate_id === animal\.candidate_id/);
  assert.match(page, /src=\{previewMedia\.media_url\}/);
  assert.doesNotMatch(page, /src=\{animal\.media_url\}/);
});

test("a legacy row (questionnaire_version 0) still takes the single-video branch", () => {
  // The card shape is decided by the backend's questionnaire_version: a row recorded under the
  // SOP renders the media slots and answers; a legacy row keeps the old video + facts card.
  const sopBranch = page.slice(page.indexOf("animal.questionnaire_version > 0"), page.indexOf("// A legacy row"));
  // The captures come from the on-demand media read for the ONE opened animal, never from the
  // list row (which carries the slots without links); every other row shows the open button.
  assert.match(sopBranch, /previewMedia\?\.candidate_id === animal\.candidate_id \? \([\s\S]*?<AnimalPurchaseMedia slots=\{previewMedia\.media_slots \?\? \[\]\}/);
  assert.match(sopBranch, /preview_id: animal\.candidate_id/);
  assert.match(sopBranch, /\{sopCopy\.mediaOpen\}/);
  assert.doesNotMatch(sopBranch, /animal\.media_slots/);
  assert.match(sopBranch, /<AnimalPurchaseAnswers rows=\{animal\.answer_rows \?\? \[\]\}/);
  assert.doesNotMatch(sopBranch, /animal\.media_url/);
  const legacyBranch = page.slice(page.indexOf("// A legacy row"), page.lastIndexOf("</article>"));
  assert.match(legacyBranch, /previewMedia\?\.candidate_id === animal\.candidate_id \? \(/);
  assert.match(legacyBranch, /animalColumn\("breed"\)/);
  assert.doesNotMatch(legacyBranch, /AnimalPurchaseAnswers|AnimalPurchaseMedia/);
  // Both shapes carry the same heading and the same decision block.
  assert.match(sopBranch, /\{heading\}[\s\S]*\{decisionBlock\}/);
  assert.match(legacyBranch, /\{heading\}[\s\S]*\{decisionBlock\}/);
});

test("media slots render a photo thumbnail or an inline video by mime, never a guess", () => {
  assert.match(sop, /startsWith\("image\/"\)/);
  assert.match(sop, /startsWith\("video\/"\)/);
  // A photo is a thumbnail link that opens the full image in a new tab (the toxin review shape).
  assert.match(sop, /item\.media_url && isImage\(item\) \? \([\s\S]*?<a[\s\S]*?href=\{item\.media_url\}[\s\S]*?target="_blank"[\s\S]*?rel="noreferrer"[\s\S]*?<img src=\{item\.media_url\}/);
  // A video plays inline, metadata only until the reviewer presses play.
  assert.match(sop, /item\.media_url && isVideo\(item\) \? \([\s\S]*?<video src=\{item\.media_url\} controls preload="metadata" playsInline \/>/);
  // Anything else says so with backend copy rather than rendering a broken tag.
  assert.match(sop, /\{copy\.mediaEmpty\}/);
  // The signed links are relative to the API; the on-demand media read absolutizes every slot
  // item the same way as the legacy video, and the list read signs nothing at all.
  assert.match(serverRead, /media_slots: \(result\.data\.media_slots \?\? \[\]\)\.map/);
  assert.match(serverRead, /item\.media_url \? \{ \.\.\.item, media_url: absolutizeAgainstApi\(item\.media_url, config\.data\.baseUrl\) \} : item/);
  assert.doesNotMatch(serverRead, /absolutizeAnimalPurchaseMedia/);
});

test("answers group by served section and an attention row is flagged", () => {
  // Sections are formed from the served order, never re-sorted; the top block has no heading.
  assert.match(sop, /last && last\.section === row\.section/);
  assert.match(sop, /\{group\.section \? <h4 className="ap-sop-section-title">\{group\.section\}<\/h4> : null\}/);
  assert.match(sop, /<dt>[\s\S]*?\{row\.question\}[\s\S]*?<\/dt>\s*<dd>\{row\.answer\}<\/dd>/);
  // The reject signal: a class the stylesheet colours warn, plus a warn dot with the backend hint.
  assert.match(sop, /className=\{row\.attention \? "ap-sop-row attention" : "ap-sop-row"\}/);
  assert.match(sop, /\{row\.attention \? <span className="dot l" title=\{copy\.attentionHint\}/);
});

test("the field verdict chip renders from the backend label with the tone of the verdict", () => {
  assert.match(sop, /if \(!animal\.field_verdict \|\| !animal\.field_verdict_label\) return null;/);
  assert.match(sop, /tone=\{animal\.field_verdict === "selected" \? "ok" : "warn"\}/);
  assert.match(sop, /\{animal\.field_verdict_label\}/);
  // It sits in the shared heading beside the CEO's decision chip on both card shapes.
  const heading = page.slice(page.indexOf("const heading = ("), page.indexOf("if (animal.questionnaire_version > 0)"));
  assert.match(heading, /<Tag tone=\{decisionTone\(animal\.decision_tone\)\}>\{animal\.decision_label\}<\/Tag>\s*<FieldVerdictChip animal=\{animal\}/);
});

test("the decision form renders only for a pending animal behind the backend control", () => {
  const branch = page.slice(page.indexOf('animal.decision !== "pending"'), page.indexOf("const heading = ("));
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
  assert.doesNotMatch(sop, /shed/i);
});
