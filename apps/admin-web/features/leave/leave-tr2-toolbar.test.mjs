import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// TR2-P2-4 (/leave): the search sat on a second toolbar row (the date pair grew to 246px each) and
// the "Who approves leave" card showed a permanently grey disabled Save.
const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");

test("guard: leave-toolbar-one-row - the date pair is capped so search shares the filter row", () => {
  assert.match(read("./leave-layout.ts"), /LEAVE_DATE_PAIR_WIDTH = 336/);
  assert.match(read("./leave-toolbar.tsx"), /width: \{ xs: 1, sm: LEAVE_DATE_PAIR_WIDTH \}, flexShrink: 0/);
});

test("guard: leave-save-when-dirty - Save renders only when there is something to save", () => {
  const panel = read("./leave-config-panel.tsx");
  assert.match(panel, /\{canEdit && \(dirty \|\| pending\) \? \(\s*<Button/);
  assert.doesNotMatch(read("../../app/(admin)/leave/loading.tsx"), /FormCardSkeleton[^>]*action=/);
});
