import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const page = readFileSync(new URL("./animal-purchases.tsx", import.meta.url), "utf8");
const form = readFileSync(new URL("./animal-purchase-decision-form.tsx", import.meta.url), "utf8");
const action = readFileSync(new URL("./animal-purchase-actions.ts", import.meta.url), "utf8");
const sop = readFileSync(new URL("./animal-purchase-sop.tsx", import.meta.url), "utf8");
const serverRead = readFileSync(new URL("../../lib/api/procurement-server.ts", import.meta.url), "utf8");
const lightbox = readFileSync(new URL("./animal-purchase-lightbox.tsx", import.meta.url), "utf8");
const styles = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");

test("every animal's captures are tap-gated on the card and a click opens one big, never a mint-on-preview gate", () => {
  // Maintainer decision 2026-09-14: every capture has a card tile and opens big on click. The
  // tile itself is a proof-media egress gate: no remote proof bytes load just because a list rendered.
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

test("media slots render byte-free tiles that open one selected proof by mime, never a guess", () => {
  assert.match(sop, /startsWith\("image\/"\)/);
  assert.match(sop, /startsWith\("video\/"\)/);
  // Every capture with a link and a known mime is a tile in the order the inspector recorded it.
  // Preview bytes are bounded to the visible/lookahead viewport band; clicking opens the selected
  // proof in the in-page lightbox.
  assert.match(sop, /kind: isImage\(item\) \? \("photo" as const\) : \("video" as const\)/);
  assert.match(sop, /thumbnailUrl: item\.thumbnail_url \?\? \(isImage\(item\) \? item\.media_url : undefined\)/);
  assert.match(sop, /<AnimalPurchaseLightbox items=\{items\}/);
  // Uniform tiles: image/poster previews attach only after IntersectionObserver marks the tile
  // visible or near-visible. Original videos still load only in the opened dialog.
  assert.match(lightbox, /new IntersectionObserver/);
  assert.match(lightbox, /rootMargin: "320px 0px"/);
  assert.match(lightbox, /closest\("\.ap-animal"\)/);
  assert.match(lightbox, /previewsEnabled && item\.thumbnailUrl/);
  assert.match(lightbox, /key=\{`\$\{item\.proofRef\}-\$\{index\}`\}/);
  assert.match(styles, /\.ap-lightbox-strip\{display:contents\}/);
  assert.match(lightbox, /<img src=\{item\.thumbnailUrl\} alt="" className="ap-tile-preview" loading="lazy" decoding="async" \/>/);
  assert.doesNotMatch(lightbox, /<img src=\{item\.url\}/);
  assert.doesNotMatch(lightbox, /<video src=\{item\.url\}/);
  assert.doesNotMatch(lightbox, /<video src=\{item\.thumbnailUrl\}/);
  assert.match(lightbox, /admin-proof-media-egress:ignore[\s\S]*?<img src=\{open\.url\} alt="" className="ap-lightbox-media" \/>/);
  assert.match(lightbox, /<video src=\{open\.url\} controls autoPlay playsInline/);
  assert.match(lightbox, /role="dialog"[\s\S]*?aria-modal="true"/);
  assert.match(lightbox, /event\.key === "Escape"/);
  assert.doesNotMatch(lightbox, /useRouter|router\.push|href=/);
  // Anything else says so with backend copy rather than rendering a broken tag.
  assert.match(sop, /\{copy\.mediaEmpty\}/);
  // The proof download routes are bearer-only API routes the browser cannot load (STG 401,
  // 2026-09-15): the legacy video and every slot item are rewritten onto the same-origin
  // /api/proof-media proxy, never absolutized onto the API host.
  assert.match(serverRead, /media_slots: \(animal\.media_slots \?\? \[\]\)\.map/);
  assert.match(serverRead, /media_url: animal\.media_url \? browserProofMediaURL\(animal\.media_url, baseUrl\)/);
  assert.match(serverRead, /thumbnail_url: item\.thumbnail_url \? browserProofMediaURL\(item\.thumbnail_url, baseUrl\) : item\.thumbnail_url/);
  assert.match(serverRead, /PROOF_DOWNLOAD_ROUTE = \/\^\\\/app\\\/proofs\\\/\(\[\^\/\?#\]\+\)\\\/download\$\//);
  assert.match(serverRead, /return `\/api\/proof-media\/\$\{encodeURIComponent\(decodeURIComponent\(match\[1\]\)\)\}`/);
  assert.doesNotMatch(serverRead, /media_url: absolutizeAgainstApi\(/);
});

test("answers group by served section and an attention row is flagged", () => {
  // Sections are formed from the served order, never re-sorted; the top block has no heading.
  assert.match(sop, /last && last\.section === section/);
  // Web reading order: breed directly under the goat id; the verdict section named as the director's.
  assert.match(sop, /row\.question_id === "goat_id"\) ordered\.push\(\{ \.\.\.breed, section: row\.section \}\)/);
  assert.match(sop, /copy\.verdictSection/);
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
