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

// BUG-020 (updated): cap editing is now a REAL wired write, not a fabricated
// success state. The operator + animal caps are edited on this screen and
// persisted through PUT /vaccination/capacity-config, which cascades a re-plan.
// Guard against regressing to the old dead form: the editor must call the real
// endpoint and must not resurrect the fabricated 'cap set to N/day' toast.
test("BUG-020: cap editing is backed by a real capacity-config write", () => {
  assert.match(
    source,
    /await api\.putVaccinationCapacityConfig\(/,
    "the cap editor must persist through the real PUT /vaccination/capacity-config endpoint",
  );
  assert.ok(
    !/cap set to \$\{/.test(source),
    "the fabricated 'Saved · cap set to N/day' toast must not return",
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

test("missing authored assignment config can still save the first rowVersion-0 config", () => {
  const persistMatch = source.match(/const persistOperatorConfig = async \(\) => \{(?<body>[\s\S]*?)\n  \};/);
  assert.ok(persistMatch?.groups?.body, "expected persistOperatorConfig body in the screen");
  const body = persistMatch.groups.body;
  assert.ok(
    !/if\s*\([^)]*assignmentConfig/.test(body),
    "saving must not be gated on assignmentConfig; null config is the first-write state",
  );
  assert.match(
    body,
    /rowVersion,/,
    "the save request must pass the current rowVersion through so rowVersion 0 can create the config",
  );
  assert.match(
    body,
    /setAssignmentConfig\(result\.data\)/,
    "after creating the first config, the returned config must become the editable assignmentConfig",
  );
});
