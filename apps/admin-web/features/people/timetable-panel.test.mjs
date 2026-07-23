import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const source = readFileSync(new URL('./timetable-panel.tsx', import.meta.url), 'utf8');

test('operator timetable accepts backend Channapatna labels for CPT seats', () => {
  assert.match(source, /haystack\.includes\('channapatna'\)/);
  assert.match(source, /haystack\.includes\('vaccination_operator_'\)/);
  assert.doesNotMatch(source, /return haystack\.includes\('cpt'\);/);
});

test('operator timetable uses HRMS per-seat animal caps for capacity math', () => {
  assert.match(source, /function operatorDailyCap\(pos: Position, fallback: number\)/);
  assert.match(source, /pos\.vaccination_daily_animal_cap \?\? fallback/);
  assert.match(source, /reduce\(\(sum, operator\) => sum \+ operatorDailyCap\(operator, operatorCap\), 0\)/);
  assert.doesNotMatch(source, /operators\.length \* operatorCap/);
});
