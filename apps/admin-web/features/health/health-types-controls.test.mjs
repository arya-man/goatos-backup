import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const controls = readFileSync(new URL("./health-types-controls.tsx", import.meta.url), "utf8");
const actions = readFileSync(new URL("./health-type-actions.ts", import.meta.url), "utf8");
const section = readFileSync(new URL("./health-types.tsx", import.meta.url), "utf8");

// `docs/decisions/admin-web-interaction-patterns.md` rule 6: a confirm is two buttons where the
// action was, never the browser's "127.0.0.1 says" box. A destructive routing change -- removing
// the wildcard takes a whole age band out of diagnosis -- deserves its consequence in words.
test("a destructive routing change confirms in place, never through the browser", () => {
  // Strip comments first: this file's own header EXPLAINS why window.confirm is banned, and a
  // naive scan for the word cannot tell the ban from a breach of it.
  const code = controls.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
  assert.doesNotMatch(code, /window\.(confirm|alert|prompt)\s*\(/);
  assert.match(controls, /const \[confirmRetire, setConfirmRetire\] = useState\(false\)/);
  assert.match(controls, /const \[confirmRemove, setConfirmRemove\] = useState\(false\)/);
  // The wildcard's consequence is shown at the moment of removing it, not left to be discovered.
  assert.match(controls, /route\.is_wildcard[\s\S]*note\.wildcard_route/);
});

// Rule 2: an action returns a result OR redirects, never both. Revalidating on top of a returned
// result re-renders the route underneath a client still holding the answer.
test("the write actions return a result and never revalidate the route", () => {
  assert.doesNotMatch(actions, /revalidatePath|revalidateTag/);
  assert.match(actions, /"use server";/);
});

// One key per human INTENT, reused across retries of that press and rotated on success -- so a
// network-failed write is safe to press again and cannot write twice.
test("every write carries an intent-scoped idempotency key", () => {
  assert.match(controls, /function useIntentKey\(prefix: string\)/);
  assert.match(controls, /if \(!key\.current\) key\.current = mintKey\(prefix\)/);
  const rotations = controls.match(/intent\.rotate\(\)/g) ?? [];
  const takes = controls.match(/intent\.take\(\)/g) ?? [];
  assert.equal(rotations.length, takes.length, "every take must have a rotate on success");
  // Minted in the HANDLER, never during render: a key generated while rendering changes on every
  // re-render, so a retry would carry a different key.
  assert.doesNotMatch(controls, /const key = mintKey\(/);
});

// Clinical placements say WHERE an animal is, not what it eats, so they cannot choose a rulebook.
// Listing them beside the real gaps would invite a medical call made from a placement fact.
test("clinical placements are reported, never presented as gaps to close", () => {
  assert.match(section, /\.filter\(\(s\) => !s\.clinical_placement\)/);
  assert.match(section, /\.filter\(\(s\) => s\.clinical_placement\)/);
});

// The gaps table is the only surface that can show a stage holding animals nobody can observe, so
// it renders ABOVE the tables that explain it -- a reader who scrolls past has missed the point.
test("the gaps table comes before the types and routing tables", () => {
  const gaps = section.indexOf("section.gaps.title");
  const types = section.indexOf("section.types.title");
  const routes = section.indexOf("section.routes.title");
  assert.ok(gaps > -1 && types > -1 && routes > -1);
  assert.ok(gaps < types && types < routes, "gaps first, then types, then routing");
});

// A type key is a JOIN key. Folding its case here would point a stage at a different rulebook
// than the author typed; the backend normalises inside the same validation that refuses a bad
// shape, so there is one answer rather than two that can drift.
test("no client-side normalisation of a key or stage code", () => {
  assert.doesNotMatch(actions, /\.toLowerCase\(\)|\.trim\(\)/);
});
