import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const source = readFileSync(new URL('./timetable-panel.tsx', import.meta.url), 'utf8');

test('operator timetable accepts backend Channapatna labels for CPT seats', () => {
  assert.match(source, /haystack\.includes\('channapatna'\)/);
  assert.match(source, /haystack\.includes\('vaccination_operator_'\)/);
  assert.doesNotMatch(source, /return haystack\.includes\('cpt'\);/);
});
