import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const page = readFileSync(new URL("./animal-purchases.tsx", import.meta.url), "utf8");
const form = readFileSync(new URL("./animal-purchase-decision-form.tsx", import.meta.url), "utf8");
const action = readFileSync(new URL("./animal-purchase-actions.ts", import.meta.url), "utf8");
const sop = readFileSync(new URL("./animal-purchase-sop.tsx", import.meta.url), "utf8");
const serverRead = readFileSync(new URL("../../lib/api/procurement-server.ts", import.meta.url), "utf8");
const lightbox = readFileSync(new URL("./animal-purchase-lightbox.tsx", import.meta.url), "utf8");

test("every animal's captures are on the card itself and a click opens one big, never a mint-on-preview gate", () => {
  // Maintainer decision 2026-09-14: photos and videos are visible by default on every card and
  // open big on click. The list read signs every capture; there is no per-animal media route.
  assert.doesNotMatch(page, /getAnimalPurchaseMedia|preview_id|previewMedia/);
  assert.match(page, /url: animal\.media_url, kind: "video"/);
  assert.match(page, /copy\(pageContract, "video\.empty"\)/);
});


test("a legacy row (questionnaire_version 0) still takes the single-video branch", () => {
  // The card shape is decided by the backend's questionnaire_version: a row recorded under the
  // SOP renders the media slots and answers; a legacy row keeps the old video + facts card.
  const sopBranch = page.slice(page.indexOf("animal.questionnaire_version > 0"), page.indexOf("// A legacy row"));
  assert.match(sopBranch, /<AnimalPurchaseMedia slots=\{animal\.media_slots \?\? \[\]\}/);
  assert.match(sopBranch, /<AnimalPurchaseAnswers rows=\{animal\.answer_rows \?\? \[\]\}/);
  assert.doesNotMatch(sopBranch, /animal\.media_url/);
  const legacyBranch = page.slice(page.indexOf("// A legacy row"), page.lastIndexOf("</article>"));
  assert.match(legacyBranch, /animal\.media_url \? \(/);
  assert.match(legacyBranch, /<AnimalPurchaseLightbox/);
  assert.match(legacyBranch, /animalColumn\("breed"\)/);
  assert.doesNotMatch(legacyBranch, /AnimalPurchaseAnswers|AnimalPurchaseMedia /);
  // Both shapes carry the same heading and the same decision block.
  assert.match(sopBranch, /\{heading\}[\s\S]*\{decisionBlock\}/);
  assert.match(legacyBranch, /\{heading\}[\s\S]*\{decisionBlock\}/);
});

test("media slots render a photo thumbnail or an inline video by mime, never a guess", () => {
  assert.match(sop, /startsWith\("image\/"\)/);
  assert.match(sop, /startsWith\("video\/"\)/);
  // A photo is a thumbnail link that opens the full image in a new tab (the toxin review shape).
  // Every capture with a link and a known mime is a lightbox item: a photo thumbnail is a button
  // that opens it big, a video plays inline (metadata only) with an expand button; in the order
  // the inspector recorded them.
  assert.match(sop, /kind: isImage\(item\) \? \("photo" as const\) : \("video" as const\)/);
  assert.match(sop, /<AnimalPurchaseLightbox items=\{items\}/);
  // Uniform tiles: a photo is its image, a video its first frame (metadata only) under a play badge.
  assert.match(lightbox, /className="ap-tile"[\s\S]*?<img src=\{item\.url\} alt="" loading="lazy" \/>/);
  assert.match(lightbox, /<video src=\{item\.url\} preload="metadata" muted playsInline tabIndex=\{-1\} \/>/);
  assert.match(lightbox, /<video src=\{open\.url\} controls autoPlay playsInline/);
  assert.match(lightbox, /role="dialog"[\s\S]*?aria-modal="true"/);
  assert.match(lightbox, /event\.key === "Escape"/);
  assert.doesNotMatch(lightbox, /useRouter|router\.push|href=/);
  // Anything else says so with backend copy rather than rendering a broken tag.
  assert.match(sop, /\{copy\.mediaEmpty\}/);
  // The signed links are relative to the API; every slot item is absolutized like the legacy video.
  assert.match(serverRead, /media_slots: \(animal\.media_slots \?\? \[\]\)\.map/);
  assert.match(serverRead, /item\.media_url \? \{ \.\.\.item, media_url: absolutizeAgainstApi\(item\.media_url, baseUrl\) \} : item/);
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
  assert.match(form, /disabled=\{pending \|\| decided \|\| !note\.trim\(\)\}/);
  // The decision lands IN PLACE: the action returns its outcome, the form shows the backend's
  // sentence and soft-refreshes the server data; nothing redirects or moves the scroll.
  assert.match(form, /useActionState\(decideAnimalPurchaseAction, INITIAL\)/);
  assert.match(form, /router\.refresh\(\)/);
  assert.doesNotMatch(action, /redirect\(/);
  assert.match(action, /Promise<AnimalPurchaseDecisionState>/);
  assert.match(action, /`animal-purchase-\$\{candidateId\}-\$\{rowVersion\}-\$\{decision\}`/);
  assert.doesNotMatch(action, /randomUUID/);
  assert.doesNotMatch(action, /revalidatePath/);
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
