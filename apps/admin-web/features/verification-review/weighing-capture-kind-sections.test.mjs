// THE WEIGH CAPTURES ARE AUTHORED (maintainer clarification 2026-09-16): a per-animal weigh item and
// a whole-pen weigh item must read differently in the /verify drawer. The backend leads each item
// with "Weighed as: Per animal / Whole pen" (group "Weighing") and groups the operator's answers
// under "Per-animal answers" / "Whole-pen answers". This runs the drawer's OWN section-start
// function (lifted from its source -- the drawer is TSX, which node cannot import here) over the
// exact rows the backend emits for each kind, and pins that each opens its own titled sections.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const drawerSource = readFileSync(new URL("./verification-review-drawer.tsx", import.meta.url), "utf8");

function drawerGroupStart() {
  const match = drawerSource.match(/export function contextRowGroupStart\(([^)]*)\)[^{]*\{([\s\S]*?)\n\}/);
  assert.ok(match, "the drawer must still define contextRowGroupStart");
  const body = match[2];
  // eslint-disable-next-line no-new-func
  return new Function("rows", "index", body);
}

const perAnimal = [
  { label: "Weighed as", value: "Per animal", group: "Weighing" },
  { label: "Limping?", value: "No", group: "Per-animal answers" },
];
const wholePen = [
  { label: "Weighed as", value: "Whole pen", group: "Weighing" },
  { label: "Every animal on the scale?", value: "Yes", group: "Whole-pen answers" },
];

test("each weigh kind opens its own Weighing and answers sections in the drawer", () => {
  const start = drawerGroupStart();
  assert.deepEqual(perAnimal.map((_, i) => start(perAnimal, i)), ["Weighing", "Per-animal answers"]);
  assert.deepEqual(wholePen.map((_, i) => start(wholePen, i)), ["Weighing", "Whole-pen answers"]);
});

test("the per-animal and whole-pen items are told apart on the item itself", () => {
  const start = drawerGroupStart();
  assert.notEqual(perAnimal[0].value, wholePen[0].value);
  assert.notEqual(start(perAnimal, 1), start(wholePen, 1));
});
