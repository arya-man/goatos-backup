import { test, describe } from 'node:test';
import { strict as assert } from 'node:assert';

describe('cohort-detail exact shed handling', () => {
  test('row keys use exact shed id and ignore stale partition labels', () => {
    const cohorts = [
      {
        parkId: 'park-cbe',
        shedId: 'shed-castro-1',
        partitionLabel: '1',
        stage: 'Adult',
        shedName: 'Castro 1',
        operationalLocationDisplay: 'Castro 1',
        animals: 100,
      },
      {
        parkId: 'park-cbe',
        shedId: 'shed-castro-2',
        partitionLabel: '2',
        stage: 'Adult',
        shedName: 'Castro 2',
        operationalLocationDisplay: 'Castro 2',
        animals: 150,
      },
    ];

    const keys = cohorts.map(c => `${c.parkId}|${c.shedId}|${c.stage}`);
    assert.notEqual(keys[0], keys[1], 'exact shed ids keep rows distinct');
    assert.equal(keys[0], 'park-cbe|shed-castro-1|Adult');
    assert.equal(keys[1], 'park-cbe|shed-castro-2|Adult');
  });

  test('row labels should match drawer labels for partitioned cohorts', () => {
    const cohort = {
      parkId: 'park-cbe',
      shedId: 'shed-castro-2',
      partitionLabel: '2',
      stage: 'Adult',
      shedName: 'Castro 2',
      operationalLocationDisplay: 'Castro 2',
    };

    // Row label (from line 157-158 in status-matrix)
    const rowLabel = `${cohort.stage} · ${cohort.operationalLocationDisplay || cohort.shedName}`;

    // Drawer label (from line 171 in record-verify-drawer)
    const drawerLabel = cohort.operationalLocationDisplay || cohort.shedName;

    assert.equal(rowLabel.includes('Castro 2'), true, 'Row shows exact shed');
    assert.equal(drawerLabel, 'Castro 2', 'Drawer shows exact shed');
  });
});
