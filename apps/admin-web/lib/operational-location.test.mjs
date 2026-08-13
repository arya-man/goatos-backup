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

test("operationalLocationLabel: numeric partition metadata does not change a shed name", () => {
  assert.equal(operationalLocationLabel({ shedName: "Castro", partitionLabel: "2" }), "Castro");
  assert.equal(operationalLocationLabel({ shedName: "Castro 2", partitionLabel: "2" }), "Castro 2");
  assert.equal(operationalLocationLabel({ shedName: "Gandhi 1", partitionLabel: "1" }), "Gandhi 1");
});

test("operationalLocationLabel: worded partition metadata does not change a shed name", () => {
  assert.equal(operationalLocationLabel({ shedName: "Godel 1", partitionLabel: "Part 3" }), "Godel 1");
  assert.equal(operationalLocationLabel({ shedName: "Godel 1 - Part 3", partitionLabel: "Part 3" }), "Godel 1 - Part 3");
  assert.equal(operationalLocationLabel({ shedName: "Mandela 2 Part 1", partitionLabel: "Part 1" }), "Mandela 2 Part 1");
});

test("operationalLocationLabel: bare numeric labels are ignored when shed name is present", () => {
  assert.equal(operationalLocationLabel({ shedName: "Godel 1", partitionLabel: "1" }), "Godel 1");
  assert.equal(operationalLocationLabel({ shedName: "Godel 1", partitionLabel: "10" }), "Godel 1");
  assert.equal(operationalLocationLabel({ shedName: "Sumathi 2", partitionLabel: "7" }), "Sumathi 2");
});

test("operationalLocationLabel: exact shed names never re-append stale compatibility partition", () => {
  assert.equal(operationalLocationLabel({ shedName: "Castro 2", partitionLabel: "2" }), "Castro 2");
  assert.equal(operationalLocationLabel({ shedName: "Castro 3", partitionLabel: "3" }), "Castro 3");
  assert.equal(operationalLocationLabel({ shedName: "Gandhi 1", partitionLabel: "1" }), "Gandhi 1");
  assert.equal(operationalLocationLabel({ shedName: "Godel 2 - Part 1", partitionLabel: "Part 1" }), "Godel 2 - Part 1");
  assert.equal(operationalLocationLabel({ shedName: "Godel 2 - Part 1", partitionLabel: "1" }), "Godel 2 - Part 1");
});

test("operationalLocationLabel: sourceShedName is already partition-bearing, never re-suffixed", () => {
  // REGRESSION: this used to assert "duplicate Castro suffix". sourceShedName is the raw name the row was
  // normalized FROM and already carries the partition, so appending partitionLabel re-suffixed it
  // and reproduced the "duplicate Godel suffix" defect the convention bans. It is a display value, not a prefix.
  assert.equal(
    operationalLocationLabel({ shedName: null, sourceShedName: "Castro 1", partitionLabel: "1" }),
    "Castro 1",
  );
  // And when a real shed name IS present, partition metadata is ignored.
  assert.equal(
    operationalLocationLabel({ shedName: "Castro 1", partitionLabel: "1" }),
    "Castro 1",
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

// CANONICAL golden fixture -- the SAME row shapes and expected display strings are
// pinned in three languages so the same fact can never render three different ways:
//   - Go:      backend/internal/platform/oploc/golden_fixture_test.go
//   - TS (this file)
//   - Kotlin:  apps/goatos-android/core/core-ui/.../PartitionLabelTest.kt
// Keep row `name` identical across all three files when adding/changing a row.
const goldenFixture = [
  {
    name: "exact partition shed, worded compatibility partition",
    shedId: "shed-godel-1",
    shedName: "Godel 1 - Part 3",
    partitionLabel: "Part 3",
    want: "Godel 1 - Part 3",
  },
  {
    name: "exact partition shed, two-digit worded compatibility partition",
    shedId: "shed-godel-1",
    shedName: "Godel 1 - Part 10",
    partitionLabel: "Part 10",
    want: "Godel 1 - Part 10",
  },
  {
    name: "exact numbered shed, bare numeric compatibility partition",
    shedId: "shed-castro-cbe",
    shedName: "Castro 2",
    partitionLabel: "2",
    want: "Castro 2",
  },
  {
    name: "undivided shed, no partition",
    shedId: "shed-yashoda-cbe",
    shedName: "Yashoda",
    partitionLabel: "",
    want: "Yashoda",
  },
  {
    name: "'whole' sentinel must never reach the user",
    shedId: "shed-yashoda-cbe",
    shedName: "Yashoda",
    partitionLabel: "whole",
    want: "Yashoda",
  },
  {
    name: "empty-string label",
    shedId: "shed-mandela-1",
    shedName: "Mandela 1",
    partitionLabel: "",
    want: "Mandela 1",
  },
  {
    name: "NULL label (read out as null)",
    shedId: "shed-mandela-1",
    shedName: "Mandela 1",
    partitionLabel: null,
    want: "Mandela 1",
  },
  {
    name: "two same-named sheds, different parks -- CBE",
    shedId: "shed-castro-cbe",
    shedName: "Castro 1",
    partitionLabel: "1",
    want: "Castro 1",
  },
  {
    name: "two same-named sheds, different parks -- CPT",
    shedId: "shed-castro-cpt",
    shedName: "Castro 1",
    partitionLabel: "1",
    want: "Castro 1",
  },
];

test("operationalLocationLabel: canonical cross-surface golden fixture", () => {
  for (const row of goldenFixture) {
    const got = operationalLocationLabel({ shedName: row.shedName, partitionLabel: row.partitionLabel });
    assert.equal(got, row.want, `row "${row.name}": got ${JSON.stringify(got)}, want ${JSON.stringify(row.want)}`);
  }
});

test("golden fixture: same-named sheds in different parks share a display string (shed_id is the real key, not exercised by this pure TS helper)", () => {
  const cbe = goldenFixture.find((r) => r.name.endsWith("-- CBE"));
  const cpt = goldenFixture.find((r) => r.name.endsWith("-- CPT"));
  assert.equal(operationalLocationLabel({ shedName: cbe.shedName, partitionLabel: cbe.partitionLabel }), operationalLocationLabel({ shedName: cpt.shedName, partitionLabel: cpt.partitionLabel }));
  // NOTE: this admin-web helper is display-only; it has no Key()/grouping equivalent to oploc.Key().
  // Any admin-web code that groups/counts by shed must key on shedId, never on this label -- see
  // backend/internal/platform/oploc/golden_fixture_test.go -> TestGoldenFixtureKeyDistinguishesSameNamedShedsAcrossParks.
  assert.notEqual(cbe.shedId, cpt.shedId);
});
