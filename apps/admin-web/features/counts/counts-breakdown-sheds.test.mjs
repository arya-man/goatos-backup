import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { buildShedFilterOptions } from "./counts-breakdown-sheds.ts";

// Two sheds named "Castro 1", one in each park — the real-data case the backend facet exists
// for: 66 of 154 shed names exist in both parks. Sourcing from the locations master collapsed
// same-named sheds and never cascaded by park; this asserts the fix does neither.
const PARK_A = "11111111-1111-1111-1111-111111111111";
const PARK_B = "22222222-2222-2222-2222-222222222222";
const SHED_A = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa";
const SHED_B = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb";

const facetSheds = [
  { key: SHED_A, label: "Castro 1", park_id: PARK_A },
  { key: SHED_B, label: "Castro 1", park_id: PARK_B },
  // The blank-key "no shed assigned" bucket must never become a dropdown option: its
  // <option value=""> collides with the "All" sentinel.
  { key: "", label: "No shed", park_id: PARK_A },
];

test("same-named sheds in different parks stay distinct options", () => {
  const options = buildShedFilterOptions(facetSheds, "");

  // Both "Castro 1" sheds survive — not collapsed to one entry.
  assert.equal(options.length, 2);
  assert.deepEqual(
    options.map((o) => o.value).sort(),
    [SHED_A, SHED_B].sort(),
  );

  // Each option carries a composite park_id + shed_id React key, so two same-named sheds never
  // share a key, while `value` stays the bare shed UUID for the backend round-trip.
  const keys = options.map((o) => o.key);
  assert.equal(new Set(keys).size, 2, "composite keys must be distinct");
  assert.ok(keys.includes(`${PARK_A}|${SHED_A}`));
  assert.ok(keys.includes(`${PARK_B}|${SHED_B}`));

  // The blank-key bucket is dropped.
  assert.ok(!options.some((o) => o.value === ""));
});

test("selecting a park narrows the shed list to that park's sheds", () => {
  const options = buildShedFilterOptions(facetSheds, PARK_A);

  assert.equal(options.length, 1);
  assert.equal(options[0].value, SHED_A);
  assert.equal(options[0].key, `${PARK_A}|${SHED_A}`);
  // The other park's same-named shed is gone.
  assert.ok(!options.some((o) => o.value === SHED_B));
});

// Real STG shape: one physical shed per park, both named "Yashoda", each subdivided into pens.
// This is the case the dropdown got wrong — 148 flat rows in which "Yashoda" appeared twice and
// "Yashoda - 1" appeared twice, with nothing on screen telling the two parks apart.
const PARTITIONED = [
  { key: SHED_A, shed_id: SHED_A, label: "Yashoda", count: 121, park_id: PARK_A },
  { key: `${SHED_A}#1`, shed_id: SHED_A, label: "Yashoda", partition_label: "1", operational_location_display: "Yashoda - 1", count: 6, park_id: PARK_A },
  { key: `${SHED_A}#2`, shed_id: SHED_A, label: "Yashoda", partition_label: "2", operational_location_display: "Yashoda - 2", count: 115, park_id: PARK_A },
  { key: SHED_B, shed_id: SHED_B, label: "Yashoda", count: 40, park_id: PARK_B },
  { key: `${SHED_B}#1`, shed_id: SHED_B, label: "Yashoda", partition_label: "1", operational_location_display: "Yashoda - 1", count: 40, park_id: PARK_B },
  // An undivided shed: one row, no partitions.
  { key: "cccccccc-cccc-cccc-cccc-cccccccccccc", shed_id: "cccccccc-cccc-cccc-cccc-cccccccccccc", label: "Ho Chi Minh 1", count: 9, park_id: PARK_A },
];
const PARK_LABELS = new Map([[PARK_A, "CBE"], [PARK_B, "CPT"]]);

test("same-named sheds across parks are told apart by their park", () => {
  // The disambiguation was DEAD CODE: it read `park_label` off the shed facet, a field neither the
  // Go struct nor the OpenAPI schema declares and nothing has ever sent. Passing the park
  // vocabulary from facets.parks is what makes it fire.
  const labels = buildShedFilterOptions(PARTITIONED, "", PARK_LABELS).map((o) => o.label);
  assert.ok(labels.includes("Yashoda · CBE"), `expected a CBE-suffixed option, got ${JSON.stringify(labels)}`);
  assert.ok(labels.includes("Yashoda · CPT"));
  assert.ok(labels.includes("Yashoda - 1 · CBE"));
  assert.ok(labels.includes("Yashoda - 1 · CPT"));
  // Every visible option is unique — no two rows read identically.
  assert.equal(new Set(labels).size, labels.length, `duplicate visible labels: ${JSON.stringify(labels)}`);
  // A name that exists in only ONE park stays clean; the suffix is disambiguation, not decoration.
  assert.ok(labels.includes("Ho Chi Minh 1"));
});

test("a park's own sheds need no park suffix, and read in name order", () => {
  const labels = buildShedFilterOptions(PARTITIONED, PARK_A, PARK_LABELS).map((o) => o.label);
  // Ho Chi Minh before Yashoda: the facet arrives ordered by shed UUID, which is an artifact.
  assert.deepEqual(labels, ["Ho Chi Minh 1", "Yashoda", "Yashoda - 1", "Yashoda - 2"]);
});

test("pens sort naturally, so 10 comes after 2", () => {
  const many = [
    { key: SHED_A, shed_id: SHED_A, label: "Yashoda", count: 3, park_id: PARK_A },
    ...["10", "2", "1"].map((p) => ({
      key: `${SHED_A}#${p}`, shed_id: SHED_A, label: "Yashoda", partition_label: p,
      operational_location_display: `Yashoda - ${p}`, count: 1, park_id: PARK_A,
    })),
  ];
  assert.deepEqual(
    buildShedFilterOptions(many, PARK_A, PARK_LABELS).map((o) => o.label),
    ["Yashoda", "Yashoda - 1", "Yashoda - 2", "Yashoda - 10"],
  );
});

test("a subdivided shed's pens are filed under the shed, undivided sheds stay loose", () => {
  const options = buildShedFilterOptions(PARTITIONED, PARK_A, PARK_LABELS);
  const yashoda = options.filter((o) => o.label.startsWith("Yashoda"));
  assert.deepEqual(new Set(yashoda.map((o) => o.group)), new Set(["Yashoda"]), "pens belong to their shed's group");
  // A one-option group is chrome around a single row.
  assert.equal(options.find((o) => o.label === "Ho Chi Minh 1").group, undefined);
});

test("park id is never leaked as a label when the park vocabulary is missing", () => {
  // Rendering a raw UUID in front of a CEO is the copy-firewall violation; an ambiguous-but-clean
  // label beats a leaked identifier.
  const labels = buildShedFilterOptions(PARTITIONED, "", new Map()).map((o) => o.label);
  for (const label of labels) assert.doesNotMatch(label, /[0-9a-f]{8}-[0-9a-f]{4}/, label);
});

test("grouped runs never merge two same-named groups from different parks", () => {
  const source = readFileSync(new URL("./counts-breakdown-filters.tsx", import.meta.url), "utf8");
  // Gathering by NAME instead of walking consecutive runs would merge CBE's Yashoda group with
  // CPT's — the name-keyed merge the operational-location convention bans (OL-1).
  assert.match(source, /function groupRuns\(/);
  assert.match(source, /last\.group === option\.group\) last\.options\.push\(option\)/);
  assert.match(source, /<optgroup/);
});

test("shed filter is sourced from the backend facet, not the locations master", () => {
  const source = readFileSync(new URL("./counts-breakdown.tsx", import.meta.url), "utf8");
  // Sheds come from the response facet + the shared cascade helper...
  assert.match(source, /buildShedFilterOptions\(breakdown\?\.facets\.sheds, selectedParkId, parkLabelsById\)/);
  // ...and the park vocabulary it disambiguates with comes from the SAME response, never a second read.
  assert.match(source, /const parkLabelsById = new Map\(\s*\n\s*\(breakdown\?\.facets\.parks \?\? \[\]\)/);
  // ...and the locations-master shed source is gone entirely.
  assert.doesNotMatch(source, /locations\.sheds/);
  assert.doesNotMatch(source, /getCensusLocations/);
});
