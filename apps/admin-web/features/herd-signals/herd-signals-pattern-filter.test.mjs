import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

// The Alerts tab selects the practical watchlist partition SERVER-SIDE by sending
// risk_state="attention". The backend supports that sentinel, but it must stay out of the row
// risk_state enum because no tag is ever IN state "attention".
// The generated client therefore could not legally express the call admin-web was making: a typed
// consumer either could not build an Alerts view at all, or had to bypass the generated contract.
//
// This test fails if those three ever drift apart again: the value the board sends, the filter enum
// the generated client accepts, and the row enum that must not accept the sentinel.
const SENTINEL = "attention";

const read = (path) => readFileSync(new URL(path, import.meta.url), "utf8");

test("the Alerts tab sends the watchlist partition sentinel", () => {
  const board = read("./herd-signals-board.tsx");
  assert.match(
    board,
    new RegExp(`riskState:\\s*"${SENTINEL}"`),
    "the Alerts tab must select the watchlist partition server-side, not filter a fetched page",
  );
});

test("the generated API client accepts the sentinel the Alerts tab sends", () => {
  const generated = read("../../../../packages/api-client/src/generated/app-api.ts");
  const line = generated.split("\n").find((l) => l.includes("HerdSignalRiskFilter:"));
  assert.ok(line, "HerdSignalRiskFilter must exist in the generated client");
  assert.ok(
    line.includes(`"${SENTINEL}"`),
    `the risk_state query parameter must accept "${SENTINEL}"; generated union was: ${line.trim()}`,
  );
});

test("risk_state on a ROW stays the real state enum", () => {
  const generated = read("../../../../packages/api-client/src/generated/app-api.ts");
  const line = generated.split("\n").find((l) => l.includes("HerdSignalRiskState:"));
  assert.ok(line, "HerdSignalRiskState must exist in the generated client");
  assert.ok(
    !line.includes(`"${SENTINEL}"`),
    "no tag is ever IN state attention; the sentinel belongs to the filter enum only",
  );
});
