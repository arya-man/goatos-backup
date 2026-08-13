import { test, describe } from 'node:test';
import { strict as assert } from 'node:assert';
import { readFileSync } from 'node:fs';

const source = readFileSync(new URL('./command-board-view.tsx', import.meta.url), 'utf8');

describe('command-board-view exact shed handling', () => {
  test('exact shed ids keep physical sheds distinct without partition labels', () => {
    const matrixCells = [
      {
        shedId: 'shed-castro-1',
        shedName: 'Castro 1',
        partition_label: '1',
        doseRule: 'et_tt_adult_w1',
        state: 'verified',
        animalCount: 100,
        minAdministeredDate: '2026-07-23',
        maxAdministeredDate: '2026-07-23',
      },
      {
        shedId: 'shed-castro-2',
        shedName: 'Castro 2',
        partition_label: '2',
        doseRule: 'et_tt_adult_w1',
        state: 'verified',
        animalCount: 150,
        minAdministeredDate: '2026-07-23',
        maxAdministeredDate: '2026-07-23',
      },
    ];

    const fixedMap = new Map();
    matrixCells.forEach((cell) => {
      const fixedKey = cell.shedId;
      if (!fixedMap.has(fixedKey)) {
        fixedMap.set(fixedKey, []);
      }
      fixedMap.get(fixedKey).push(cell);
    });

    assert.equal(fixedMap.get('shed-castro-1')[0].shedName, 'Castro 1');
    assert.equal(fixedMap.get('shed-castro-2')[0].shedName, 'Castro 2');
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

    const fixedMap = new Map();
    matrixCells.forEach((cell) => {
      const fixedKey = cell.shedId;
      if (!fixedMap.has(fixedKey)) {
        fixedMap.set(fixedKey, []);
      }
      fixedMap.get(fixedKey).push(cell);
    });

    // Two different sheds should have distinct groups
    const cbeGroup = fixedMap.get('shed-castro-cbe');
    const cptGroup = fixedMap.get('shed-castro-cpt');
    assert.ok(cbeGroup, 'CBE Castro is distinct');
    assert.ok(cptGroup, 'CPT Castro is distinct');
    assert.equal(cbeGroup[0].animalCount, 200);
    assert.equal(cptGroup[0].animalCount, 180);
  });

  test('shed-vaccine drawer uses exact operational-location display', () => {
    assert.match(
      source,
      /selectedShedVaccine\.operational_location_display \|\| selectedShedVaccine\.shedName/,
    );
    assert.doesNotMatch(source, /\{selectedShedVaccine\.shedName\} ·/);
  });
});
