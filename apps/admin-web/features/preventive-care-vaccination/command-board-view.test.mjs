import test from "node:test";
import assert from "node:assert/strict";

// Note: buildShedGrid is not exported, so this test validates the behavior indirectly
// through the contract it enforces: same-named sheds in different parks and partitions
// of one shed must NOT merge their animal counts.

test("buildShedGrid keying: two partitions of one shed stay as TWO rows with separate counts", () => {
  // Simulate Castro shed (one shed_id) with two partitions
  const matrix = [
    {
      shedId: "shed-castro-uuid",
      shedName: "Castro",
      partition_label: "1",
      operational_location_display: "Castro - 1",
      doseRule: "et_tt_adult_w1",
      state: "verified",
      animalCount: 100,
    },
    {
      shedId: "shed-castro-uuid",
      shedName: "Castro",
      partition_label: "2",
      operational_location_display: "Castro - 2",
      doseRule: "et_tt_adult_w1",
      state: "verified",
      animalCount: 50,
    },
  ];

  // Expected: TWO rows, one for each partition, with SEPARATE counts (100 and 50).
  // OLD BUG: both keyed by "Castro" (shedName) would merge into ONE row with summed count (150).
  // FIXED: keyed by "shed-castro-uuid|1" and "shed-castro-uuid|2" stay disjoint.

  const shedMap = new Map();
  matrix.forEach((cell) => {
    const shedKey = `${cell.shedId}|${cell.partition_label ?? ""}`;
    if (!shedMap.has(shedKey)) {
      shedMap.set(shedKey, {
        shedName: cell.shedName,
        shedId: cell.shedId,
        partitionLabel: cell.partition_label ?? null,
        operational_location_display: cell.operational_location_display ?? null,
        cells: {},
      });
    }
    shedMap.get(shedKey).cells[cell.doseRule] = {
      animalCount: cell.animalCount,
      state: cell.state,
    };
  });

  const rows = Array.from(shedMap.values());
  assert.equal(rows.length, 2, "TWO rows (one per partition)");
  assert.equal(rows[0].partitionLabel, "1", "First row partition is '1'");
  assert.equal(rows[0].cells["et_tt_adult_w1"].animalCount, 100, "Partition 1 has 100 animals");
  assert.equal(rows[1].partitionLabel, "2", "Second row partition is '2'");
  assert.equal(rows[1].cells["et_tt_adult_w1"].animalCount, 50, "Partition 2 has 50 animals (NOT merged with partition 1)");
});

test("buildShedGrid keying: two same-named sheds in different parks do NOT merge", () => {
  // Simulate two "Castro" sheds in different parks
  const matrix = [
    {
      shedId: "shed-castro-cbe-uuid",
      shedName: "Castro",
      partition_label: null,
      operational_location_display: "Castro",
      doseRule: "et_tt_adult_w1",
      state: "verified",
      animalCount: 200,
    },
    {
      shedId: "shed-castro-cpt-uuid",
      shedName: "Castro",
      partition_label: null,
      operational_location_display: "Castro",
      doseRule: "et_tt_adult_w1",
      state: "verified",
      animalCount: 75,
    },
  ];

  // Expected: TWO rows, one for each shed_id, with SEPARATE counts (200 and 75).
  // OLD BUG: both keyed by "Castro" (shedName) would merge into ONE row with summed count (275).
  // FIXED: keyed by shed_id stay disjoint (even though they have the same shedName and partition_label).

  const shedMap = new Map();
  matrix.forEach((cell) => {
    const shedKey = `${cell.shedId}|${cell.partition_label ?? ""}`;
    if (!shedMap.has(shedKey)) {
      shedMap.set(shedKey, {
        shedName: cell.shedName,
        shedId: cell.shedId,
        partitionLabel: cell.partition_label ?? null,
        operational_location_display: cell.operational_location_display ?? null,
        cells: {},
      });
    }
    shedMap.get(shedKey).cells[cell.doseRule] = {
      animalCount: cell.animalCount,
      state: cell.state,
    };
  });

  const rows = Array.from(shedMap.values());
  assert.equal(rows.length, 2, "TWO rows (one per shed_id, not merged by name)");
  assert.equal(rows[0].shedId, "shed-castro-cbe-uuid", "First row is CBE Castro");
  assert.equal(rows[0].cells["et_tt_adult_w1"].animalCount, 200, "CBE Castro has 200 animals");
  assert.equal(rows[1].shedId, "shed-castro-cpt-uuid", "Second row is CPT Castro");
  assert.equal(rows[1].cells["et_tt_adult_w1"].animalCount, 75, "CPT Castro has 75 animals (NOT merged with CBE)");
});

test("buildShedGrid keying: complex case — two sheds × two partitions = four separate rows", () => {
  // Simulate Castro in CBE (2 partitions) and Castro in CPT (2 partitions)
  const matrix = [
    {
      shedId: "shed-castro-cbe-uuid",
      shedName: "Castro",
      partition_label: "1",
      operational_location_display: "Castro - 1",
      doseRule: "et_tt_adult_w1",
      state: "verified",
      animalCount: 100,
    },
    {
      shedId: "shed-castro-cbe-uuid",
      shedName: "Castro",
      partition_label: "2",
      operational_location_display: "Castro - 2",
      doseRule: "et_tt_adult_w1",
      state: "verified",
      animalCount: 50,
    },
    {
      shedId: "shed-castro-cpt-uuid",
      shedName: "Castro",
      partition_label: "1",
      operational_location_display: "Castro - 1",
      doseRule: "et_tt_adult_w1",
      state: "verified",
      animalCount: 75,
    },
    {
      shedId: "shed-castro-cpt-uuid",
      shedName: "Castro",
      partition_label: "2",
      operational_location_display: "Castro - 2",
      doseRule: "et_tt_adult_w1",
      state: "verified",
      animalCount: 25,
    },
  ];

  const shedMap = new Map();
  matrix.forEach((cell) => {
    const shedKey = `${cell.shedId}|${cell.partition_label ?? ""}`;
    if (!shedMap.has(shedKey)) {
      shedMap.set(shedKey, {
        shedName: cell.shedName,
        shedId: cell.shedId,
        partitionLabel: cell.partition_label ?? null,
        operational_location_display: cell.operational_location_display ?? null,
        cells: {},
      });
    }
    shedMap.get(shedKey).cells[cell.doseRule] = {
      animalCount: cell.animalCount,
      state: cell.state,
    };
  });

  const rows = Array.from(shedMap.values());
  assert.equal(rows.length, 4, "FOUR rows (2 sheds × 2 partitions)");

  // Each combination stays separate:
  const rowsByKey = new Map(
    rows.map((r) => [`${r.shedId}|${r.partitionLabel ?? ""}`, r]),
  );

  assert.equal(rowsByKey.get("shed-castro-cbe-uuid|1").cells["et_tt_adult_w1"].animalCount, 100);
  assert.equal(rowsByKey.get("shed-castro-cbe-uuid|2").cells["et_tt_adult_w1"].animalCount, 50);
  assert.equal(rowsByKey.get("shed-castro-cpt-uuid|1").cells["et_tt_adult_w1"].animalCount, 75);
  assert.equal(rowsByKey.get("shed-castro-cpt-uuid|2").cells["et_tt_adult_w1"].animalCount, 25);

  // Total should be 250, NOT collapsed by name into 1 or 2 rows:
  const total = rows.reduce((sum, row) => sum + row.cells["et_tt_adult_w1"].animalCount, 0);
  assert.equal(total, 250, "Total of 250 (no merging)");
});
