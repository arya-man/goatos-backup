// The board's drag-and-drop, at the two places it can silently lie to a reader: which columns it
// offers, and what it says when the write is refused.
//
// Both are checked as BEHAVIOUR where they are behaviour (the refusal sentence is a pure function
// and is called) and as SOURCE SHAPE where the thing being pinned is a wiring decision a future
// edit could quietly undo (the phone gate, the client-minted key, the legality source).
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

import { refusalSentence } from "./task-feedback-copy.ts";

const read = (path) => readFileSync(new URL(path, import.meta.url), "utf8");
const dnd = read("./task-board-dnd.tsx");
const card = read("./task-board-card.tsx");
const actions = read("./actions.ts");
const css = read("../../app/mesha-theme.css");

// The contract lookup, as the banner hands it in. `bare` is a contract with no `feedback.*` keys
// at all, so every sentence is the local fallback — what the live desk renders today.
const bare = (_key, fallback) => fallback;
// And one whose owner HAS authored the keys, to prove the contract still wins.
const AUTHORED = {
  "feedback.version_conflict.status": "Backend wording: now {status}.",
  "feedback.not_assignee.named": "Backend wording: ask {name}.",
};
const authored = (key, fallback) => AUTHORED[key] ?? fallback;

// ---- Each refusal reads as ITSELF. Three codes, three sentences, none of them the generic one.
const conflict = refusalSentence(bare, "version_conflict", "Doing", undefined);
const closed = refusalSentence(bare, "task_closed", "Cancelled", undefined);
const notAssignee = refusalSentence(bare, "not_assignee", undefined, "Satish");
for (const [name, text] of [["version_conflict", conflict], ["task_closed", closed], ["not_assignee", notAssignee]]) {
  assert.ok(text.length > 20, `${name} must have a real sentence`);
}
assert.notEqual(conflict, closed, "a conflict and a closed task must not read the same");
assert.notEqual(conflict, notAssignee, "a conflict and a permission refusal must not read the same");
assert.notEqual(closed, notAssignee, "a closed task and a permission refusal must not read the same");

// ---- The facts are IN the sentence, not merely available to it.
assert.match(conflict, /Doing/, "a conflict must name the status the task is actually in now");
assert.match(closed, /Cancelled/, "a closed task must say which closed state it is in");
assert.match(notAssignee, /Satish/, "a permission refusal must name the person whose move it is");

// ---- No abstract wording, and no placeholder standing in for a person. "Someone else changed
// this" names nobody and is exactly what AGENTS.md bans; a reader on a 424-task board cannot act
// on it. An unresolved name costs the CLAUSE, never a fake noun.
const nameless = refusalSentence(bare, "not_assignee", undefined, undefined);
assert.doesNotMatch(nameless, /\{name\}/, "an unfilled placeholder must never reach a reader");
assert.doesNotMatch(nameless, /someone/i, '"someone" is not a name');
assert.ok(nameless.length > 20, "dropping the name must leave a real sentence behind");
const statusless = refusalSentence(bare, "version_conflict", undefined, undefined);
assert.doesNotMatch(statusless, /\{status\}/, "an unfilled placeholder must never reach a reader");
assert.doesNotMatch(statusless, /someone/i, '"someone" is not a name');
for (const text of [conflict, closed, notAssignee, nameless, statusless]) {
  assert.doesNotMatch(text, /something went wrong|could not be completed/i, "a refusal must not collapse into the generic failure");
}

// ---- The page contract OVERRIDES the fallback, because the copy is backend-owned.
assert.equal(
  refusalSentence(authored, "version_conflict", "Done", undefined),
  "Backend wording: now Done.",
  "an authored feedback key must win over the local fallback",
);
assert.equal(
  refusalSentence(authored, "not_assignee", undefined, "Manju"),
  "Backend wording: ask Manju.",
  "an authored feedback key must win over the local fallback",
);

// ---- An unknown code falls through to the banner's generic sentence rather than rendering a raw
// token at a reader.
assert.equal(refusalSentence(bare, "some_new_backend_code", "Doing", "Ravi"), "");

// ---- LEGALITY comes from the row, not from a matrix in the frontend.
// BOTH sides must read the row: the one that decides what the reader is INVITED to do, and the
// one that decides what the drop actually does. A hardcoded highlight over a row-derived drop
// check is still a lie to the reader, so counting one occurrence is not enough.
const legalityReads = dnd.match(/statusOptions\.some\(\(option\) => option\.key === (?:columnKey|column\.key)\)/g) ?? [];
assert.equal(
  legalityReads.length,
  2,
  "both the droppable highlight and the drop itself must read the ROW's own status_options",
);
assert.match(
  dnd,
  /const legalFor = \(columnKey: string\): boolean =>\s*Boolean\(draggingTask\?\.statusOptions\.some/,
  "the column highlight must be derived from the dragged row's own options",
);
assert.doesNotMatch(dnd, /"open".*=>.*"in_progress"/s, "no hardcoded transition matrix");
assert.match(dnd, /if \(!droppable\) return;/, "an illegal column must not preventDefault, so it cannot accept a drop");

// ---- The idempotency key is minted PER DROP, client-side. A server-rendered key replays on back
// navigation and swallows a legitimate second change; that was a real defect on this page.
assert.match(dnd, /crypto\.randomUUID\(\)/, "the drop must mint its own key");
assert.match(dnd, /admin-web-leadership-task-status:/, "and use the same command prefix as the status buttons");
assert.ok(
  dnd.indexOf("crypto.randomUUID()") > dnd.indexOf("const move = ("),
  "the key must be minted inside the drop handler, not at module or render scope",
);

// ---- The FENCE is the row's own version, never a page-load snapshot.
assert.match(dnd, /formData\.set\("row_version", String\(task\.rowVersion\)\)/);

// ---- PHONE: no drag below the page's breakpoint, and the anchor's native drag is switched off
// explicitly (an <a> is draggable by default, so silence would ship a URL drag).
assert.match(dnd, /\(min-width: 761px\) and \(pointer: fine\)/, "drag is gated above the 760px breakpoint");
assert.match(dnd, /useState\(false\)/, "the gate must start closed so the server-rendered HTML is the non-drag one");
assert.match(card, /draggable=\{draggable\}/, "the card must state draggable either way");
assert.match(dnd, /dragCapable && task\.statusOptions\.length > 0/, "a card with no legal move is not draggable at all");
assert.match(css, /@media\(max-width:760px\)\{[^}]*\.ltb-dndhint\{display:none\}/s, "the drag hint must be hidden at phone width");

// ---- ARIA: no deprecated aria-grabbed anywhere, and the announcement is a real live region.
// The attribute form, not the word: the file's own comment explains WHY it is not used.
assert.doesNotMatch(dnd, /aria-grabbed=/, "aria-grabbed is deprecated and must not be rendered");
assert.doesNotMatch(card, /aria-grabbed/, "the card must not carry a deprecated drag attribute");
assert.match(dnd, /aria-live="polite"/);

// ---- Brand palette only for the drag affordance: no hex in the new CSS block.
const dragCss = css.slice(css.indexOf("TASK BOARD DRAG AND DROP"), css.indexOf("END TASK BOARD DRAG AND DROP"));
assert.doesNotMatch(dragCss, /#[0-9a-fA-F]{3,8}\b/, "the drag affordance must use brand tokens, never a hex colour");
assert.match(dragCss, /var\(--brand\)/);

// ---- The refusal re-read happens on the FAILURE path only, and the name rides only a refusal
// that is genuinely about a person.
assert.match(actions, /STATUS_REFUSALS_WORTH_A_REREAD\.includes\(code\)/);
assert.match(actions, /code === "not_assignee" && task\.assignee_name/);
assert.match(actions, /code === "not_raiser" && task\.raised_by_name/);
assert.match(actions, /task\.status !== fromStatus/, "the status is named only when the task actually moved");

console.log("task board drag-and-drop: legality, refusal copy, key minting, phone gate and palette all pinned");
