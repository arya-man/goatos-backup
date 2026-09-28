import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: config-row-edit-icon (TR2-P2-8). /configuration/items rows used an "Edit" text button
// beside the ⋮; the template user-table-row uses a pencil IconButton (with a Tooltip) + the ⋮.
const actions = readFileSync(new URL("./row-actions.tsx", import.meta.url), "utf8");

test("guard: config-row-edit-icon - row edit is a pencil IconButton, not a text button", () => {
  assert.match(actions, /<IconButton component=\{LocalOverlayLink\} href=\{editHref\}[^>]*aria-label=\{labels\.edit\}/);
  assert.match(actions, /solar:pen-bold/);
  assert.doesNotMatch(actions, /className="btn sm ghost"/);
});
