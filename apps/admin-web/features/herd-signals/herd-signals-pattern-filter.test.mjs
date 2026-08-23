import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

// The Alerts tab selects the alerting partition SERVER-SIDE by sending pattern="not_normal".
// The backend supports that sentinel, but the OpenAPI query parameter once pointed at
// HerdSignalPatternState -- the enum of states a tag can actually BE in, which has no such member.
// The generated client therefore could not legally express the call admin-web was making: a typed
// consumer either could not build an Alerts view at all, or had to bypass the generated contract.
//
// This test fails if those three ever drift apart again: the value the board sends, the enum the
// generated client accepts, and the sentinel the repository understands.
const SENTINEL = "not_normal";

const read = (path) => readFileSync(new URL(path, import.meta.url), "utf8");

test("the Alerts tab sends the partition sentinel", () => {
  const board = read("./herd-signals-board.tsx");
  assert.match(
    board,
    new RegExp(`pattern:\\s*"${SENTINEL}"`),
    'the Alerts tab must select the alerting partition server-side, not filter a fetched page',
  );
});

test("the generated API client accepts the sentinel the Alerts tab sends", () => {
  const generated = read("../../../../packages/api-client/src/generated/app-api.ts");
  const line = generated.split("\n").find((l) => l.includes("HerdSignalPatternFilter:"));
  assert.ok(line, "HerdSignalPatternFilter must exist in the generated client");
  assert.ok(
    line.includes(`"${SENTINEL}"`),
    `the pattern query parameter must accept "${SENTINEL}"; generated union was: ${line.trim()}`,
  );
});

test("pattern_state on a ROW stays the real state enum", () => {
  const generated = read("../../../../packages/api-client/src/generated/app-api.ts");
  const line = generated.split("\n").find((l) => l.includes("HerdSignalPatternState:"));
  assert.ok(line, "HerdSignalPatternState must exist in the generated client");
  assert.ok(
    !line.includes(`"${SENTINEL}"`),
    "no tag is ever IN state not_normal; the sentinel belongs to the filter enum only",
  );
});
