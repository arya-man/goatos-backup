import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: herd-signals-no-dev-note (TR2-P2-9). The "Live tag signals" card sub-title was a developer
// paragraph ("Counts are whole-filter aggregates computed by the backend…").
const board = readFileSync(new URL("./herd-signals-board.tsx", import.meta.url), "utf8");
const skeletons = readFileSync(new URL("./herd-signals-skeletons.tsx", import.meta.url), "utf8");

test("guard: herd-signals-no-dev-note - no implementation note as a card subheader", () => {
  assert.doesNotMatch(board, /whole-filter aggregates|tenant-scoped query/);
  assert.doesNotMatch(skeletons, /rows=\{LIMIT_DEFAULT\} subheader/);
});
