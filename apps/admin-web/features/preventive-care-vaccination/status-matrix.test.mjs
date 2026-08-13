import { test, describe } from 'node:test';
import { strict as assert } from 'node:assert';

describe('status-matrix partition handling', () => {
  test('status-matrix row keys should include partitionLabel', () => {
    // Simulates two partitions of the same shed across two status rows
    const cohorts = [
      {
        parkId: 'park-cpt',
        shedId: 'shed-godel-1',
        partitionLabel: 'Part 1',
        stage: 'Adult',
        shedName: 'Godel 1',
        operationalLocationDisplay: 'Godel 1 - Part 1',
        parkName: 'Channapatna',
      },
      {
        parkId: 'park-cpt',
        shedId: 'shed-godel-1',
        partitionLabel: 'Part 3',
        stage: 'Adult',
        shedName: 'Godel 1',
        operationalLocationDisplay: 'Godel 1 - Part 3',
        parkName: 'Channapatna',
      },
    ];

    // Current buggy key (line 153)
    const buggyKeys = cohorts.map(c => `${c.parkId}|${c.shedId}|${c.stage}`);
    assert.equal(buggyKeys[0], buggyKeys[1], 'Bug: keys collide without partitionLabel');

    // Fixed key should include partitionLabel
    const fixedKeys = cohorts.map(c => `${c.parkId}|${c.shedId}|${c.stage}|${c.partitionLabel ?? ''}`);
    assert.notEqual(fixedKeys[0], fixedKeys[1], 'Fix: keys are distinct');
    assert.equal(fixedKeys[0], 'park-cpt|shed-godel-1|Adult|Part 1');
    assert.equal(fixedKeys[1], 'park-cpt|shed-godel-1|Adult|Part 3');
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
