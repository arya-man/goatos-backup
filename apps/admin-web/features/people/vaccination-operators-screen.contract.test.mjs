import { test } from "node:test";
import assert from "node:assert";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const source = readFileSync(
  fileURLToPath(new URL("./vaccination-operators-screen.tsx", import.meta.url)),
  "utf8",
);
// The park-scope resolution + data load moved out of the component into a unit-testable module
// (vaccination-operators-scope.test.mjs covers its BEHAVIOUR). These source-shape assertions follow it.
const scopeSource = readFileSync(
  fileURLToPath(new URL("./vaccination-operators-scope.ts", import.meta.url)),
  "utf8",
);

// BUG-019: the roster read must carry an explicit BACKEND-RESOLVED park scope.
// An unscoped listStaffPositions blends every park of a multi-park tenant into
// one roster, capacity KPI, weekly preview, and default-operator dropdown.
test("BUG-019: listStaffPositions is called with an explicit park scope", () => {
  const call = scopeSource.match(/api\.listStaffPositions\(\{[^}]*\}\)/s);
  assert.ok(call, "expected a listStaffPositions call in the screen");
  assert.match(call[0], /scope_type:\s*'center'/, "roster read must be park-scoped");
  assert.match(call[0], /scope_id:\s*resolvedParkId/, "roster read must use the backend-resolved park id");
});

// The park must come from the backend contract, not from row data.
test("BUG-019: park id is not inferred from the first roster row", () => {
  assert.ok(
    !/positions\[0\]\.scope_id/.test(source + scopeSource),
    "park scope must not be derived from positions[0].scope_id",
  );
  assert.match(
    scopeSource,
    /getVaccinationOperatorAssignmentConfig\(chosenParkId\)/,
    "the screen must ask the backend to resolve the park scope (undefined on the first call)",
  );
  assert.match(scopeSource, /config\?\.parkId/, "the screen must use the backend-echoed parkId");
});

// BUG-020: a control path with no real write must never render a fabricated
// success state, and the dead form must not stay mounted/keyboard-reachable.
test("BUG-020: no fabricated cap-save success path exists", () => {
  assert.ok(!/saveCap/.test(source), "the unwired saveCap handler must be deleted");
  assert.ok(!/capEditing|capDraft|capSaving/.test(source), "the dead cap edit state must be deleted");
  assert.ok(
    !/cap set to \$\{/.test(source),
    "the fabricated 'Saved · cap set to N/day' toast must be gone",
  );
});

// Every remaining showToast success must sit downstream of a real API call.
test("BUG-020: remaining Saved toasts follow a backend write", () => {
  const savedToasts = source.match(/showToast\(`?<b[^`)]*Saved[^`)]*/g) ?? [];
  for (const toast of savedToasts) {
    const idx = source.indexOf(toast);
    const preceding = source.slice(Math.max(0, idx - 800), idx);
    assert.match(
      preceding,
      /await api\.[A-Za-z]+\(/,
      `a "Saved" toast must be preceded by an awaited API call: ${toast.slice(0, 60)}`,
    );
  }
});
