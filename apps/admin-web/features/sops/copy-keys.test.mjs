import { readFileSync, readdirSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

// A copy key the SOP screens read but the backend contract does not carry throws inside
// `copy()` and blanks the page (the 2026-09-14 "Change SOP closes the modal and nothing opens"
// report: the editor asked for followup.step.basis before the backend served it). Every literal
// key in features/sops must be declared in adminui/app/service.go.
const dir = new URL("./", import.meta.url);
const backend = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");
const declared = new Set([...backend.matchAll(/"([a-z0-9_.]+)"(?::\s+"|\]\s*=\s*")/g)].map((m) => m[1]));
// The phone-task editor's keys ship with a LOCAL fallback in lib/admin-ui-contract.ts
// (PHONE_TASK_COPY) until the backend SOP contracts carry them; `copy()` resolves those, so they
// cannot blank the page. Only that named block is read -- any other undeclared key still fails.
const contract = readFileSync(new URL("../../lib/admin-ui-contract.ts", import.meta.url), "utf8");
const phoneTaskBlock = contract.match(/const PHONE_TASK_COPY: Record<string, string> = \{([\s\S]*?)\n\};/);
assert.ok(phoneTaskBlock, "PHONE_TASK_COPY must exist in lib/admin-ui-contract.ts");
for (const m of phoneTaskBlock[1].matchAll(/"([a-z0-9_.]+)":/g)) declared.add(m[1]);
// Keys composed at runtime from a closed vocabulary (kind / capture), listed explicitly here.
for (const k of ["choice", "multi", "text", "number", "media", "vendor"]) declared.add(`inspection.kind.${k}`);
for (const k of ["photo", "video", "both"]) declared.add(`inspection.accepts.${k}`);

test("every copy key read by the SOP screens is declared in the backend copy map", () => {
  const missing = [];
  for (const f of readdirSync(dir).filter((n) => /\.(tsx|ts)$/.test(n))) {
    const src = readFileSync(new URL(f, dir), "utf8");
    for (const m of src.matchAll(/\b(?:copy|c|followUpCopy\(pc\)|followUpCopy\(pageContract\))\(\s*(?:pc|pageContract)?,?\s*"([a-z0-9_.]+)"/g)) {
      if (!declared.has(m[1])) missing.push(`${f}: ${m[1]}`);
    }
    for (const m of src.matchAll(/(?:copy\("|followUpCopy\([^)]*\)\(")([a-z0-9_.]+)"/g)) {
      if (!declared.has(m[1])) missing.push(`${f}: ${m[1]}`);
    }
  }
  assert.deepEqual([...new Set(missing)], []);
});
