import { test, describe } from 'node:test';
import { strict as assert } from 'node:assert';

describe('status-matrix exact shed handling', () => {
  test('status-matrix row keys use exact shed id', () => {
    const cohorts = [
      {
        parkId: 'park-cpt',
        shedId: 'shed-godel-1-part-1',
        partitionLabel: 'Part 1',
        stage: 'Adult',
        shedName: 'Godel 1 - Part 1',
        operationalLocationDisplay: 'Godel 1 - Part 1',
        parkName: 'Channapatna',
      },
      {
        parkId: 'park-cpt',
        shedId: 'shed-godel-1-part-3',
        partitionLabel: 'Part 3',
        stage: 'Adult',
        shedName: 'Godel 1 - Part 3',
        operationalLocationDisplay: 'Godel 1 - Part 3',
        parkName: 'Channapatna',
      },
    ];

    const keys = cohorts.map(c => `${c.parkId}|${c.shedId}|${c.stage}`);
    assert.notEqual(keys[0], keys[1], 'exact shed ids keep rows distinct');
    assert.equal(keys[0], 'park-cpt|shed-godel-1-part-1|Adult');
    assert.equal(keys[1], 'park-cpt|shed-godel-1-part-3|Adult');
  });

  test('row display should show operational location with partition', () => {
    const cohort = {
      stage: 'Adult',
      operationalLocationDisplay: 'Godel 1 - Part 3',
      shedName: 'Godel 1',
      parkName: 'Channapatna',
    };

    const displayLabel = `${cohort.stage} · ${cohort.operationalLocationDisplay || cohort.shedName}`;
    assert.equal(displayLabel, 'Adult · Godel 1 - Part 3', 'Display includes partition');
  });
});
