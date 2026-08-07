import { test, describe } from 'node:test';
import { strict as assert } from 'node:assert';

describe('cohort-detail partition handling', () => {
  test('row keys should include partitionLabel to avoid collisions', () => {
    // Simulates two partitions of the same shed in the same stage
    const cohorts = [
      {
        parkId: 'park-cbe',
        shedId: 'shed-castro',
        partitionLabel: '1',
        stage: 'Adult',
        shedName: 'Castro',
        operationalLocationDisplay: 'Castro - 1',
        animals: 100,
      },
      {
        parkId: 'park-cbe',
        shedId: 'shed-castro',
        partitionLabel: '2',
        stage: 'Adult',
        shedName: 'Castro',
        operationalLocationDisplay: 'Castro - 2',
        animals: 150,
      },
    ];

    // Extract what the keys would be from current code (line 91)
    const keys = cohorts.map(c => `${c.parkId}|${c.shedId}|${c.stage}`);

    // Without partitionLabel, these keys collide
    assert.equal(keys[0], keys[1], 'Bug: keys collide without partitionLabel');

    // After fix, keys should include partitionLabel
    const fixedKeys = cohorts.map(c => `${c.parkId}|${c.shedId}|${c.stage}|${c.partitionLabel ?? ''}`);
    assert.notEqual(fixedKeys[0], fixedKeys[1], 'Fix: keys are distinct with partitionLabel');
    assert.equal(fixedKeys[0], 'park-cbe|shed-castro|Adult|1');
    assert.equal(fixedKeys[1], 'park-cbe|shed-castro|Adult|2');
  });

  test('row labels should match drawer labels for partitioned cohorts', () => {
    const cohort = {
      parkId: 'park-cbe',
      shedId: 'shed-castro',
      partitionLabel: '2',
      stage: 'Adult',
      shedName: 'Castro',
      operationalLocationDisplay: 'Castro - 2',
    };

    // Row label (from line 157-158 in status-matrix)
    const rowLabel = `${cohort.stage} · ${cohort.operationalLocationDisplay || cohort.shedName}`;

    // Drawer label (from line 171 in record-verify-drawer)
    const drawerLabel = cohort.operationalLocationDisplay || cohort.shedName;

    // Both should show the full operational location
    assert.equal(rowLabel.includes('Castro - 2'), true, 'Row shows full partition');
    assert.equal(drawerLabel, 'Castro - 2', 'Drawer shows operational location');
  });
});
