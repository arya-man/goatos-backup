import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: care-coverage-no-apply-note (J2 P2-9). The toolbar said "Tick several pens, then Apply."
// while no Apply was on screen (it is inside the pen popover). No note may name a control that is
// not visible where the note is.
const src = readFileSync(new URL("./care-coverage-filters.tsx", import.meta.url), "utf8");

test("guard: care-coverage-no-apply-note - the toolbar renders no apply note", () => {
  assert.doesNotMatch(src, /copy\(pageContract, "filter\.apply_note"\)/);
  assert.match(src, /function apply\(\)/, "Apply stays in the pen popover");
});
