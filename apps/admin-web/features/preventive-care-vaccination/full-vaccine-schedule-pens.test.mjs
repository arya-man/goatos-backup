import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { addSchedulePen, schedulePenKey } from "./full-vaccine-schedule-pens.ts";

const CPT = "00000000-0000-4000-8000-000000003002";
const CBE = "00000000-0000-4000-8000-000000003001";
const GANDHI = "e2f9bd7e-43a6-5000-8c9e-16ff4fd682f7";
const GODEL1 = "a80948b1-63a1-51b4-b4e3-e0cd3d8e2daa";
const YASHODA = "d389d0d4-3d17-5f44-bb0c-7aec222175d9";

function day(rows) {
  const pens = [];
  for (const row of rows) addSchedulePen(pens, row, row.animals);
  return pens.map((pen) => `${pen.display} ${pen.animals}`);
}

test("an operator-day lists each pen, never the building", () => {
  // The live 2026-09-04 CPT drive: two Gandhi pens and four Godel 1 pens. It rendered "Gandhi 84"
  // and "Godel 1 120".
  assert.deepEqual(
    day([
      { parkId: CPT, shedId: GANDHI, physicalShed: "Gandhi", partitionLabel: "1", animals: 42 },
      { parkId: CPT, shedId: GANDHI, physicalShed: "Gandhi", partitionLabel: "3", animals: 42 },
      { parkId: CPT, shedId: GODEL1, physicalShed: "Godel 1", partitionLabel: "Part 1", animals: 30 },
      { parkId: CPT, shedId: GODEL1, physicalShed: "Godel 1", partitionLabel: "Part 2", animals: 30 },
    ]),
    ["Gandhi 1 42", "Gandhi 3 42", "Godel 1 - Part 1 30", "Godel 1 - Part 2 30"],
  );
});

test("two animals in Yashoda 3 read as Yashoda 3, not as pen Yashoda 2", () => {
  assert.deepEqual(day([{ parkId: CPT, shedId: YASHODA, physicalShed: "Yashoda", partitionLabel: "3", animals: 2 }]), ["Yashoda 3 2"]);
});

test("the backend-composed display wins, and a whole pen shows no partition", () => {
  assert.deepEqual(
    day([
      { parkId: CPT, shedId: GANDHI, physicalShed: "Gandhi", partitionLabel: "2", partition_label: "2", operational_location_display: "Gandhi 2", animals: 30 },
      { parkId: CBE, shedId: "b8ff0919-7407-5fd7-ada1-2c258f080085", physicalShed: "Ho Chi Minh 1", partitionLabel: "whole", animals: 17 },
    ]),
    ["Gandhi 2 30", "Ho Chi Minh 1 17"],
  );
});

test("pens are keyed by park + shed id + partition, never by name", () => {
  const cbeCastro = { parkId: CBE, shedId: "4b5bfb76-1d79-5678-8a1b-d4b5e7c659a0", physicalShed: "Castro", partitionLabel: "1" };
  const cptCastro = { parkId: CPT, shedId: "62241795-628e-58ef-9591-aa384fb0f0f7", physicalShed: "Castro", partitionLabel: "1" };
  assert.notEqual(schedulePenKey(cbeCastro), schedulePenKey(cptCastro));
  assert.equal(
    schedulePenKey({ parkId: CPT, shedId: GODEL1, physicalShed: "Godel 1", partitionLabel: "Part 3" }),
    schedulePenKey({ parkId: CPT, shedId: GODEL1, physicalShed: "Godel 1", partitionLabel: "3" }),
  );
});

test("the schedule renders pens through the helper and never groups by shed name", () => {
  const source = readFileSync(new URL("./full-vaccine-schedule.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(source, /item\.name === row\.physicalShed/);
  assert.doesNotMatch(source, /schedule\.partition\.prefix/);
  assert.match(source, /title=\{penTitle\(pen\)\}>\s*\{pen\.display\}/);
  assert.match(source, /label: pen\.display/);
  assert.match(source, /partition_label: pen\.partitionLabel/);
});
