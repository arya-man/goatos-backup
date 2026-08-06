import test from "node:test";
import assert from "node:assert/strict";
import { operationalLocationLabel, hasOperationalPartition } from "./operational-location.ts";

test("operationalLocationLabel: non-partitioned shed returns bare shed name", () => {
  assert.equal(operationalLocationLabel({ shedName: "Yashoda", partitionLabel: null }), "Yashoda");
  assert.equal(operationalLocationLabel({ shedName: "Yashoda", partitionLabel: "" }), "Yashoda");
  assert.equal(operationalLocationLabel({ shedName: "Yashoda", partitionLabel: undefined }), "Yashoda");
});

test("operationalLocationLabel: never renders the literal string 'whole'", () => {
  const label = operationalLocationLabel({ shedName: "Yashoda", partitionLabel: "whole" });
  assert.equal(label, "Yashoda");
  assert.doesNotMatch(label, /whole/i);
  // Case-insensitive
  assert.equal(operationalLocationLabel({ shedName: "Yashoda", partitionLabel: "WHOLE" }), "Yashoda");
});

test("operationalLocationLabel: numeric partition joins with a dash", () => {
  assert.equal(operationalLocationLabel({ shedName: "Castro", partitionLabel: "2" }), "Castro - 2");
  assert.equal(operationalLocationLabel({ shedName: "Gandhi", partitionLabel: "1" }), "Gandhi - 1");
});

test("operationalLocationLabel: prefixed partition convention joins with a dash", () => {
  assert.equal(operationalLocationLabel({ shedName: "Godel 1", partitionLabel: "Part 3" }), "Godel 1 - Part 3");
  assert.equal(operationalLocationLabel({ shedName: "Godel 1", partitionLabel: "part 3" }), "Godel 1 - part 3");
});

// The reason the separator changed (2026-08-06): a shed name that itself ends in a
// digit made the old space form unreadable -- "Godel 1 1", and worse "Godel 1 10".
// 98 of 130 live STG destination options had this shape.
test("operationalLocationLabel: digit-terminated shed names stay readable", () => {
  assert.equal(operationalLocationLabel({ shedName: "Godel 1", partitionLabel: "1" }), "Godel 1 - 1");
  assert.equal(operationalLocationLabel({ shedName: "Godel 1", partitionLabel: "10" }), "Godel 1 - 10");
  assert.equal(operationalLocationLabel({ shedName: "Sumathi 2", partitionLabel: "7" }), "Sumathi 2 - 7");
});

test("operationalLocationLabel: falls back to sourceShedName when shedName missing", () => {
  assert.equal(
    operationalLocationLabel({ shedName: null, sourceShedName: "Castro 1", partitionLabel: "1" }),
    "Castro 1 - 1",
  );
});

test("hasOperationalPartition: distinguishes real partitions from sentinels", () => {
  assert.equal(hasOperationalPartition(null), false);
  assert.equal(hasOperationalPartition(""), false);
  assert.equal(hasOperationalPartition("whole"), false);
  assert.equal(hasOperationalPartition("Whole"), false);
  assert.equal(hasOperationalPartition("2"), true);
  assert.equal(hasOperationalPartition("Part 3"), true);
});
