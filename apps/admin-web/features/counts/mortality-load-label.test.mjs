import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { loadPensIndex, mortalityLoadLabel } from "./mortality-load-label.ts";

const pens128 = [
  { park: "CBE", pen: "Castro 3", animals: 40 },
  { park: "CBE", pen: "Yashoda 10", animals: 20 },
  { park: "CBE", pen: "Yashoda 11", animals: 6 },
];

test("a mortality load cell names the load, its biggest pen and counts the rest (PR #294 O3)", () => {
  const label = mortalityLoadLabel("Load 128", pens128);
  assert.equal(label.text, "Load 128 (CBE Castro 3 +2)");
  assert.equal(label.full, "Load 128 (CBE Castro 3, CBE Yashoda 10, CBE Yashoda 11)");
  assert.deepEqual(mortalityLoadLabel("Farm born", []), { text: "Farm born", full: "Farm born" });
});

test("the cross tab and the deaths list resolve the same load's pens; a shared label goes bare", () => {
  const idx = loadPensIndex([
    { key: "uuid-128", label: "Load 128", pens: pens128 },
    { key: "uuid-a", label: "Load 7", pens: [{ park: "CPT", pen: "Castro 1", animals: 3 }] },
    { key: "uuid-b", label: "Load 7", pens: [{ park: "CBE", pen: "Castro 2", animals: 3 }] },
    { key: "farm_born", label: "Farm born" },
  ]);
  assert.equal(idx.byKey("uuid-128"), pens128);
  assert.equal(idx.byLabel("Load 128"), pens128);
  assert.equal(idx.byLabel("Load 7"), undefined, "two loads named alike never borrow each other's pens");
  assert.ok(idx.byKey("uuid-a"));
  assert.equal(idx.byKey("farm_born"), undefined);
});

const page = readFileSync(new URL("./mortality.tsx", import.meta.url), "utf8");
const tables = readFileSync(new URL("./mortality-tables.tsx", import.meta.url), "utf8");

test("all three load surfaces use the compact composition with the full text in a title", () => {
  // rate table, Load × cause rows, and the deaths list
  assert.doesNotMatch(page, /withLoadPens\(bucketLabel\(bucket\.label\), bucket\.pens\)/, "the two-pen bracket pushed Rate off the card");
  assert.match(page, /mortalityLoadLabel\(/);
  assert.match(page, /rowPens=\{loadPens\.byKey\}/);
  assert.match(page, /loadPens\.byLabel\(d\.load_ref\)/);
  assert.match(tables, /row\.loadPens/);
});

test("compact tables fit a phone: no 540px floor, the label cell ends in an ellipsis beside its counts", () => {
  assert.match(page, /const COMPACT_TABLE_SX = \{ "&&": \{ minWidth: 0 \} \}/);
  assert.equal((page.match(/<Table aria-label=\{ariaLabel\} sx=\{COMPACT_TABLE_SX\}>/g) ?? []).length, 3);
  assert.match(page, /const LABEL_CELL_SX = \{[^}]*textOverflow: "ellipsis"/);
});

test("the deaths list fits 1440: tighter cell padding and word-wrapping breed / stage / load", () => {
  assert.match(tables, /"& th, & td": \{ whiteSpace: "nowrap", verticalAlign: "top", px: 1\.25 \}/);
  assert.match(tables, /const WRAP_WORDS = \{ whiteSpace: "normal"/);
});
