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

// The shared SlotCard (feed-editor.tsx) renders on the Feed, Preventive Care and capture editors,
// whose contracts carry DIFFERENT namespaces. A literal key inside SlotCard must therefore exist in
// the Preventive Care copy too, or /pc-care/sops Edit hits the error boundary (FJ3 P0-1: the card
// once labelled its kind select with fsop.proofs, which only the Feed page serves). Page-specific
// wording comes in through props (kindLabel) instead.
test("SlotCard only reads copy keys the Preventive Care SOP contract also carries", () => {
  const feed = readFileSync(new URL("feed-editor.tsx", dir), "utf8");
  const start = feed.indexOf("export function SlotCard(");
  assert.ok(start > 0, "SlotCard not found");
  const body = feed.slice(start);
  const keys = [...new Set([...body.matchAll(/copy\(pc, "([a-z0-9_.]+)"\)/g)].map((m) => m[1]))];
  const fn = backend.indexOf("func pcCareSOPEditorCopy()");
  assert.ok(fn > 0, "pcCareSOPEditorCopy not found");
  const pcCare = backend.slice(fn, backend.indexOf("\n}\n", fn));
  const pcCareDeclared = new Set([...pcCare.matchAll(/"([a-z0-9_.]+)":\s+"/g)].map((m) => m[1]));
  // Keys pcCareSOPEditorCopy inherits from the shared inspection editor copy.
  const shared = keys.filter((k) => k.startsWith("inspection."));
  const missing = keys.filter((k) => !k.startsWith("inspection.") && !pcCareDeclared.has(k));
  assert.deepEqual(missing, []);
  for (const k of shared) assert.ok(declared.has(k), k);
});
