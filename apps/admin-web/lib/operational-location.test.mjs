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

test("operationalLocationLabel: numeric partition convention appends bare label", () => {
  assert.equal(operationalLocationLabel({ shedName: "Castro", partitionLabel: "2" }), "Castro 2");
  assert.equal(operationalLocationLabel({ shedName: "Gandhi", partitionLabel: "1" }), "Gandhi 1");
});

test("operationalLocationLabel: prefixed partition convention joins with a dash", () => {
  assert.equal(operationalLocationLabel({ shedName: "Godel 1", partitionLabel: "Part 3" }), "Godel 1 - Part 3");
  assert.equal(operationalLocationLabel({ shedName: "Godel 1", partitionLabel: "part 3" }), "Godel 1 - part 3");
});

test("operationalLocationLabel: falls back to sourceShedName when shedName missing", () => {
  assert.equal(
    operationalLocationLabel({ shedName: null, sourceShedName: "Castro 1", partitionLabel: "1" }),
    "Castro 1 1",
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
