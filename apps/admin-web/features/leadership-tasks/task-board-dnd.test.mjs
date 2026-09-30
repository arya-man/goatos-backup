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
assert.match(dnd, /useDroppable\(\{ id: columnKey, disabled: !droppable \}\)/, "an illegal column is a disabled drop target, so it cannot accept a drop");

// ---- The idempotency key is minted PER DROP, client-side. A server-rendered key replays on back
// navigation and swallows a legitimate second change; that was a real defect on this page.
assert.match(dnd, /crypto\.randomUUID\(\)/, "the drop must mint its own key");
assert.match(dnd, /admin-web-leadership-task-status:/, "and use the same command prefix as the status buttons");
assert.ok(
  dnd.indexOf("crypto.randomUUID()") > dnd.indexOf("const move = ("),
  "the key must be minted inside the drop handler, not at module or render scope",
);

// ---- The FENCE is read when the write is SENT (after any queued write to the task landed),
// never a page-load snapshot and never the number on screen at the drop (2026-09-25).
assert.match(dnd, /formData\.set\("row_version", String\(currentTaskRowVersion\(task\.id,/);
assert.match(dnd, /runTaskWrite\(task\.id,/);
// ---- IN PLACE: the drop never posts the redirecting action (interaction-patterns lock).
assert.match(dnd, /changeLeadershipTaskStatusInPlaceAction\(formData\)/);
assert.doesNotMatch(dnd, /await action\(formData\)/);

// ---- TOUCH + OVERLAY (guard: task-board-touch-dnd, Ravi 2026-09-30). The board used native
// HTML5 drag: touch browsers never fire it (a phone could not move a card) and the drag image was
// the transparent snapshot of the link. It is dnd-kit now, with sensors that keep a tap a tap.
assert.doesNotMatch(dnd, /onDragStart=\{\(event\) => \{\s*event\.dataTransfer/, "no native HTML5 dragstart on the board");
assert.doesNotMatch(dnd, /dataTransfer/, "the board never reads an HTML5 drag payload");
assert.doesNotMatch(dnd, /pointer: fine/, "drag is no longer gated off for touch or phone widths");
assert.match(dnd, /from "@dnd-kit\/core"/, "the board drags with dnd-kit");
assert.match(dnd, /useSensor\(TouchSensor, \{ activationConstraint: TOUCH_ACTIVATION \}\)/, "a TouchSensor with an activation constraint");
assert.match(dnd, /useSensor\(MouseSensor, \{ activationConstraint: MOUSE_ACTIVATION \}\)/, "a MouseSensor with a travel constraint, so a click still opens the card");
assert.match(dnd, /useSensor\(KeyboardSensor,/, "the keyboard sensor stays");
{
  const touch = /export const TOUCH_ACTIVATION = \{ delay: (\d+), tolerance: (\d+) \}/.exec(dnd);
  assert.ok(touch, "TOUCH_ACTIVATION is a delay + tolerance constraint");
  assert.ok(Number(touch[1]) >= 150 && Number(touch[1]) <= 300, "the long-press delay is about 200ms (a tap opens, a hold drags)");
  assert.ok(Number(touch[2]) >= 3 && Number(touch[2]) <= 10, "the tolerance is about 5px (a swipe scrolls)");
  const mouse = /export const MOUSE_ACTIVATION = \{ distance: (\d+) \}/.exec(dnd);
  assert.ok(mouse && Number(mouse[1]) >= 3, "a mouse drag starts only after a few px of travel");
}
assert.match(dnd, /start: \["Space"\]/, "Space picks a card up; Enter stays the link's open");
// The overlay is the SAME card, on the paper background with the template lift, in a portal.
assert.match(dnd, /<DragOverlay[^>]*className="ltb-drag-overlay"/, "a DragOverlay renders the dragged card");
assert.match(dnd, /<BoardDragOverlay>\s*\{draggingTask \? \(\s*<TaskBoardCard[\s\S]*?\boverlay\b/, "the overlay renders TaskBoardCard in overlay mode");
assert.match(dnd, /createPortal\(overlay, document\.body\)/, "the overlay is portalled, so no scroller clips it");
assert.match(card, /const OVERLAY_SX = \(theme: Theme\) => \(\{\s*backgroundColor: theme\.vars\.palette\.background\.paper,/, "the overlay card is paper-backed (never transparent)");
assert.match(card, /boxShadow: theme\.vars\.customShadows\.z24/, "the overlay card is lifted with the template shadow");
assert.match(card, /transform: "rotate\(\d+(?:\.\d+)?deg\) scale\(1\.0\d\)"/, "the overlay card tilts and scales slightly");
assert.match(card, /draggable=\{false\}/, "the link's native drag is always off (no transparent ghost, no URL drag)");
assert.match(card, /dragging \? \{ filter: "grayscale\(1\)", "& > \*": \{ opacity: 0\.4 \} \}/, "the source slot keeps the template --dragging placeholder");
assert.match(card, /WebkitTouchCallout: "none"/, "a long-press does not raise the iOS link callout");
assert.match(dnd, /canDrag=\{task\.statusOptions\.length > 0\}/, "a card with no legal move is not draggable at all");
assert.match(dnd, /useClickSuppressor\(\)/, "the click that follows a drop never opens the drawer");
// The drop is still the same in-place, optimistic write with rollback.
assert.match(dnd, /onDragEnd=\{\(event: DragEndEvent\) => \{[\s\S]*?move\(task, column\.key\);/, "a drop calls the same move()");
assert.match(dnd, /publishTaskRow\(task\.id, before\);/, "a refused move rolls back");
// The e2e that drags for real (mouse 1440, touch 390) and the gate plugin that runs it on /tasks.
{
  const journey = read("../../scripts/lib/task-board-dnd-journey.mjs");
  assert.match(journey, /Input\.dispatchTouchEvent/, "the e2e drags with real touch events");
  assert.match(journey, /route\.abort\("failed"\)/, "the e2e never lets the status write reach the API");
  assert.doesNotMatch(journey, /route\.continue\(/, "the e2e never continues a server action");
  const plugin = read("../../scripts/r2-audit-checks/task-board-dnd.mjs");
  assert.match(plugin, /runBoardDragJourney/, "the visual gate runs the drag journey on /tasks");
}

// ---- ARIA: no deprecated aria-grabbed anywhere, and the announcement is a real live region.
// The attribute form, not the word: the file's own comment explains WHY it is not used.
assert.doesNotMatch(dnd, /aria-grabbed=/, "aria-grabbed is deprecated and must not be rendered");
assert.doesNotMatch(card, /aria-grabbed/, "the card must not carry a deprecated drag attribute");
assert.match(dnd, /aria-live="polite"/);

// ---- Brand palette only for the drag affordance: no hex in the new CSS block.
// The drag affordance is the template kanban column state (sections/kanban/column/styles.tsx):
// a legal target takes the column-over state, the one under the pointer task-over -- theme
// palette tokens, never a hex colour in feature code.
assert.match(dnd, /kanbanColumnState\.taskOver/, "the column under the pointer takes the template task-over state");
assert.match(dnd, /kanbanColumnState\.columnOver/, "a legal drop column takes the template column-over state");
assert.doesNotMatch(dnd, /#[0-9a-fA-F]{3,8}\b/, "the drag affordance must use theme tokens, never a hex colour");

// ---- The refusal re-read happens on the FAILURE path only, and the name rides only a refusal
// that is genuinely about a person.
assert.match(actions, /STATUS_REFUSALS_WORTH_A_REREAD\.includes\(code\)/);
assert.match(actions, /code === "not_assignee" && task\.assignee_name/);
assert.match(actions, /code === "not_raiser" && task\.raised_by_name/);
assert.match(actions, /task\.status !== fromStatus/, "the status is named only when the task actually moved");

console.log("task board drag-and-drop: legality, refusal copy, key minting, touch sensors, overlay and palette all pinned");
