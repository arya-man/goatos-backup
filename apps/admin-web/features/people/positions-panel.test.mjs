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

// Restored functionality: Backup configuration visibility
test('positions panel loads and displays backup configurations', () => {
  assert.match(source, /api\.listBackupConfig\(\)/);
  assert.match(source, /setBackupConfigs\(backupResponse\.data\?.items/);
  assert.match(source, /Configured backup — per center × group/);
  assert.match(source, /backupConfigs\.map\(\(config\)/);
});

// Restored functionality: Active coverage windows
test('positions panel loads and displays active coverage windows', () => {
  assert.match(source, /api\.listCoverage\(\)/);
  assert.match(source, /setActiveCoverages\(coverageResponse\.data\?.items/);
  assert.match(source, /Active coverage — this period/);
  assert.match(source, /activeCoverages\.map\(\(coverage\)/);
});

// Restored functionality: Row-click position profile drawer
test('positions panel opens position profile drawer on row click', () => {
  assert.match(source, /getAdminApi\(\)\.getStaffPositionProfile\(positionId\)/);
  assert.match(source, /const openProfile = async/);
  assert.match(source, /setProfile\(res\.data\.profile/);
  assert.match(source, /onClick=\{\(\) => void openProfile\(pos\.position_id\)\}/);
  assert.match(source, /Position Profile/);
});

// Restored functionality: Full positions table (not filtered to vaccination only)
test('positions panel shows all active positions, not just vaccination operators', () => {
  assert.match(source, /Positions — three independent axes/);
  assert.match(source, /positions\.map\(\(pos\)/);
  assert.match(source, /Three axes stay independent/);
  // Verify the positions table has all columns (including Tier, HR grade, Backup group)
  assert.match(source, /<th>Tier<\/th>/);
  assert.match(source, /<th>Position title<\/th>/);
  assert.match(source, /<th>HR grade<\/th>/);
  assert.match(source, /<th>Backup group<\/th>/);
});
