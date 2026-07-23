import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const source = readFileSync(new URL('./positions-panel.tsx', import.meta.url), 'utf8');

test('positions panel edits the HRMS vaccination daily animal cap', () => {
  assert.match(source, /vaccination_daily_animal_cap: nextCap/);
  assert.match(source, /api\.updateStaffPosition\(pos\.position_id/);
  assert.match(source, /row_version: pos\.row_version/);
  assert.match(source, /HRMS drive cap is the scheduler source of truth/);
});

test('positions panel can clear a custom cap back to the tenant default', () => {
  assert.match(source, /function clearOperatorCap/);
  assert.match(source, /vaccination_daily_animal_cap: null/);
  assert.match(source, /Use default/);
});

test('positions panel totals capacity from per-operator caps, not default times count', () => {
  assert.match(source, /pos\.vaccination_daily_animal_cap \?\? operatorCap/);
  assert.match(source, /operators\.reduce\(\(sum, pos\) => sum \+ capForPosition\(pos\), 0\)/);
  assert.doesNotMatch(source, /operators\.length \* operatorCap/);
});
