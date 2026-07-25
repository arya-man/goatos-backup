#!/usr/bin/env node

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";

const repoRoot = resolve(import.meta.dirname, "../../..");
const read = (rel) => readFileSync(join(repoRoot, "apps/admin-web", rel), "utf8");

const shared = read("lib/vaccination-command-lenses.ts");
const requiredPaths = [
  "/vaccination",
  "/vaccination/execution",
  "/calendar",
  "/action-center",
  "/protocol-adherence",
  "/workflows",
  "/",
];

for (const path of requiredPaths) {
  assert.match(
    shared,
    new RegExp(JSON.stringify(path).replaceAll("/", "\\/")),
    `shared vaccination command-lens invalidation must include ${path}`,
  );
}
assert.match(shared, /VACCINATION_COMMAND_LENS_PATHS/, "shared invalidation path list must be explicit");
assert.match(shared, /revalidateVaccinationCommandLenses/, "shared invalidation helper must exist");
assert.match(
  shared,
  /revalidatePath\(\s*["'`]\/vaccination\/execution\/sheds\/\[shedId\]["'`]\s*,\s*["'`]page["'`]\s*\)/,
  "shared invalidation must include the dynamic vaccination execution shed drilldown route",
);

const mutationFiles = [
  "features/preventive-care-vaccination/full-vaccine-schedule.tsx",
  "features/calendar/calendar-actions.ts",
  "features/process-integrity/actions.ts",
  "lib/api/vaccination-actions.ts",
];

for (const rel of mutationFiles) {
  const source = read(rel);
  assert.match(
    source,
    /revalidateVaccinationCommandLenses/,
    `${rel} must invalidate every vaccination command lens after shared due-work mutation`,
  );
  assert.doesNotMatch(
    source,
    /revalidatePath\s*\(\s*["'`](?:\/vaccination|\/calendar|\/action-center|\/protocol-adherence|\/workflows|\/)["'`]\s*\)/,
    `${rel} must not hand-maintain one-off command-lens revalidation paths`,
  );
}

console.log("vaccination command-lens invalidation guard: ok");
