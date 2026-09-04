import assert from "node:assert/strict";
import test from "node:test";

import { allOpen, dominantKey, penDetailDomId, penRowId, pointLabel } from "./counts-breakdown-pens.ts";

const PARK_A = "11111111-1111-1111-1111-111111111111";
const PARK_B = "22222222-2222-2222-2222-222222222222";
const SHED = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa";

test("penRowId keeps same-named pens in different parks and partitions of one shed apart", () => {
  const a = penRowId({ park_id: PARK_A, shed_id: SHED, partition_label: "2" });
  const b = penRowId({ park_id: PARK_B, shed_id: SHED, partition_label: "2" });
  const c = penRowId({ park_id: PARK_A, shed_id: SHED, partition_label: "3" });
  const whole = penRowId({ park_id: PARK_A, shed_id: SHED, partition_label: null });
  assert.notEqual(a, b);
  assert.notEqual(a, c);
  assert.notEqual(a, whole);
  // A missing park or shed is its own bucket, never folded into another.
  assert.equal(penRowId({ park_id: null, shed_id: null, partition_label: null }), "||");
});

test("penDetailDomId is a stable, DOM-safe id derived from the same identity", () => {
  const pen = { park_id: PARK_A, shed_id: SHED, partition_label: "Part 3" };
  assert.equal(penDetailDomId(pen), penDetailDomId({ ...pen }));
  assert.match(penDetailDomId(pen), /^pen-detail-[A-Za-z0-9_-]+$/);
});

test("pointLabel renders contract copy for the blank bucket and the vocabulary label for a sex", () => {
  const genders = new Map([["female", "Female"], ["male", "Male"]]);
  assert.equal(pointLabel({ key: "", label: "" }, "No stage"), "No stage");
  assert.equal(pointLabel({ key: "K2", label: "K2" }, "No stage"), "K2");
  assert.equal(pointLabel({ key: "female", label: "female" }, "—", genders), "Female");
  // An unknown token still renders rather than vanishing, so a data problem stays visible.
  assert.equal(pointLabel({ key: "unknown", label: "unknown" }, "—", genders), "unknown");
});

test("dominantKey is the first (largest) bucket, blank when the pen has none", () => {
  assert.equal(dominantKey([{ key: "F2" }, { key: "K1" }]), "F2");
  assert.equal(dominantKey([]), "");
});

test("allOpen is true only when every pen on the page is open, and never for an empty page", () => {
  const pens = [
    { park_id: PARK_A, shed_id: SHED, partition_label: "1" },
    { park_id: PARK_A, shed_id: SHED, partition_label: "2" },
  ];
  assert.equal(allOpen(pens, new Set()), false);
  assert.equal(allOpen(pens, new Set([penRowId(pens[0])])), false);
  assert.equal(allOpen(pens, new Set(pens.map(penRowId))), true);
  assert.equal(allOpen([], new Set()), false);
});
