import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: audit-table-fits (J2 P2-4). The /operations/audit activity table forced Action and Target
// onto one line each, so at 1440 the last column header was cut ("Pr…"). Those two cells wrap.
const src = readFileSync(new URL("./audit-log.tsx", import.meta.url), "utf8");

test("guard: audit-table-fits - action and target cells wrap", () => {
  assert.equal((src.match(/<TableCell sx=\{AUDIT_WRAP_CELL_SX\}>/g) ?? []).length, 2);
  assert.match(src, /AUDIT_WRAP_CELL_SX = \{ minWidth: 150, overflowWrap: "anywhere" \}/);
});
