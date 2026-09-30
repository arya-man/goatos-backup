import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: passport-empty-and-ids (J2 P1-3/P1-4). The goat passport printed the raw table name
// `goat_identity_events` as a card subheader, obligation hashes (`cee6e124`) in the Workflow
// column, and its empty states as bare text lines. Empty states are the template EmptyContent
// (components/app/empty-state), ids never render as text (runtime twin: r2 text-fit raw-id-text).
const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");
const files = {
  passport: read("./index.tsx"),
  vaccination: read("../preventive-care-vaccination/passport-section.tsx"),
  drawer: read("../counts/herd-passport-local-drawer.tsx"),
};

test("guard: passport-empty-and-ids - empty states are EmptyState, never a bare Typography line", () => {
  for (const [name, src] of Object.entries({ passport: files.passport, vaccination: files.vaccination })) {
    assert.doesNotMatch(src, /<Typography[^>]*>\s*\{copy\(pageContract, "(?:empty\.[\w.]+|vaccination\.empty_[\w]+)"\)\}\s*<\/Typography>/, `${name}: empty copy inside a bare Typography`);
  }
  assert.match(files.passport, /<EmptyState title=\{copy\(pageContract, "empty\.timeline"\)\}/);
  assert.match(files.passport, /<EmptyState title=\{copy\(pageContract, "empty\.evidence"\)\}/);
});

test("guard: passport-empty-and-ids - no raw table name, obligation hash or decision id as text", () => {
  assert.doesNotMatch(files.passport, /label\.identity_events_table/);
  assert.doesNotMatch(files.passport, /shortId\(event\.decision_id\)/);
  for (const [name, src] of Object.entries(files)) {
    assert.doesNotMatch(src, /obligation_id\.slice\(0, 8\)|sourceObligationLabel\(/, `${name}: prints an obligation hash`);
    assert.doesNotMatch(src, /className="gid"/, `${name}: legacy green id chip`);
  }
});
