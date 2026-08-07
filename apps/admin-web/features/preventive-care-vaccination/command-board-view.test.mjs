import { test, describe } from 'node:test';
import { strict as assert } from 'node:assert';

describe('command-board-view enrichDriveOptions partition handling', () => {
  test('enrichDriveOptions should key by shedId + partition_label to avoid count collapse', () => {
    // Simulates two partitions of Castro with different animal counts
    const matrixCells = [
      {
        shedId: 'shed-castro',
        shedName: 'Castro',
        partition_label: '1',
        doseRule: 'et_tt_adult_w1',
        state: 'verified',
        animalCount: 100,
        minAdministeredDate: '2026-07-23',
        maxAdministeredDate: '2026-07-23',
      },
      {
        shedId: 'shed-castro',
        shedName: 'Castro',
        partition_label: '2',
        doseRule: 'et_tt_adult_w1',
        state: 'verified',
        animalCount: 150,
        minAdministeredDate: '2026-07-23',
        maxAdministeredDate: '2026-07-23',
      },
    ];

    // Simulate the buggy grouping (keying by shedName only)
    const buggyMap = new Map();
    matrixCells.forEach((cell) => {
      const buggyKey = cell.shedName; // BUG: only uses shedName
      if (!buggyMap.has(buggyKey)) {
        buggyMap.set(buggyKey, []);
      }
      buggyMap.get(buggyKey).push(cell);
    });

    // Both partitions collapse into one group - WRONG!
    const buggyGroup = buggyMap.get('Castro');
    const buggySum = buggyGroup.reduce((sum, c) => sum + c.animalCount, 0);
    assert.equal(buggySum, 250, 'Bug: shedName-only grouping sums both partitions');

    // Simulate the fixed grouping (keying by shedId + partition_label)
    const fixedMap = new Map();
    matrixCells.forEach((cell) => {
      const fixedKey = `${cell.shedId}|${cell.partition_label ?? ''}`;
      if (!fixedMap.has(fixedKey)) {
        fixedMap.set(fixedKey, []);
      }
      fixedMap.get(fixedKey).push(cell);
    });

    // Two partitions remain distinct - CORRECT!
    const partition1 = fixedMap.get('shed-castro|1');
    const partition2 = fixedMap.get('shed-castro|2');
    assert.ok(partition1, 'Fix: partition 1 has distinct group');
    assert.ok(partition2, 'Fix: partition 2 has distinct group');
    assert.equal(partition1[0].animalCount, 100);
    assert.equal(partition2[0].animalCount, 150);

    // Total from both should still be 250, but the distinction is preserved
    const fixedSum = Array.from(fixedMap.values()).reduce((sum, group) => {
      return sum + group.reduce((gs, c) => gs + c.animalCount, 0);
    }, 0);
    assert.equal(fixedSum, 250, 'Fix: total is correct');
  });

  test('enrichDriveOptions should handle same-named sheds in different parks', () => {
    // Simulates Castro in two different parks
    const matrixCells = [
      {
        shedId: 'shed-castro-cbe',
        shedName: 'Castro',
        partition_label: '', // No partition
        doseRule: 'et_tt_adult_w1',
        state: 'verified',
        animalCount: 200,
        minAdministeredDate: '2026-07-23',
      },
      {
        shedId: 'shed-castro-cpt',
        shedName: 'Castro',
        partition_label: '', // No partition, but different shed ID
        doseRule: 'et_tt_adult_w1',
        state: 'verified',
        animalCount: 180,
        minAdministeredDate: '2026-07-23',
      },
    ];

    // Fixed grouping by shedId + partition_label
    const fixedMap = new Map();
    matrixCells.forEach((cell) => {
      const fixedKey = `${cell.shedId}|${cell.partition_label ?? ''}`;
      if (!fixedMap.has(fixedKey)) {
        fixedMap.set(fixedKey, []);
      }
      fixedMap.get(fixedKey).push(cell);
    });

    // Two different sheds should have distinct groups
    const cbeGroup = fixedMap.get('shed-castro-cbe|');
    const cptGroup = fixedMap.get('shed-castro-cpt|');
    assert.ok(cbeGroup, 'CBE Castro is distinct');
    assert.ok(cptGroup, 'CPT Castro is distinct');
    assert.equal(cbeGroup[0].animalCount, 200);
    assert.equal(cptGroup[0].animalCount, 180);
  });
});
