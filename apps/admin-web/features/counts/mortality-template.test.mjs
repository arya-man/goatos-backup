import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// /counts/mortality side-by-side vs the Minimal analytics overview (R3CNT 2026-09-27).
const source = readFileSync(new URL("./mortality.tsx", import.meta.url), "utf8");

test("rate-table rows show words, never raw codes; heads are capitalised; labels hold one line", () => {
  // "male" / "goat" rows sat under "Deaths" / "animals" heads (rhythm|raw-code-label).
  assert.match(source, /function bucketLabel\([\s\S]{0,200}humanizeEnum\(label\)/);
  assert.doesNotMatch(source, /\{bucket\.label \|\| unassignedLabel/);
  assert.match(source, /animalsLabel\.charAt\(0\)\.toUpperCase\(\) \+ animalsLabel\.slice\(1\)/);
  assert.equal((source.match(/whiteSpace: "nowrap" \}\}>\{bucketLabel\(bucket\.label\)/g) ?? []).length, 2);
});

test("months chart keeps the template colour pair: no palette KEY handed to chart.colors (audit chart-black)", () => {
  assert.doesNotMatch(source, /seriesColorVar/);
});
