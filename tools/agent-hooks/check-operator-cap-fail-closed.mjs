#!/usr/bin/env node

import { readFileSync, readdirSync, existsSync } from 'node:fs';
import { join, resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

// 18 operatorCapacityPlanner*/effectiveOperatorAnimalCap* callsites exist in
// backend/internal/obligation/app today. The floor is deliberately well below
// that so a refactor does not trip it, and deliberately above zero so a
// detector that goes blind cannot report a pass.
const MIN_CALLSITES = 8;

/**
 * check-operator-cap-fail-closed.mjs
 *
 * Machine guard to prevent fail-OPEN leaks at operator capacity planner callsites.
 *
 * When operatorCapacityPlanner or effectiveOperatorAnimalCap returns cap <= 0 due to
 * operator exhaustion (original > 0, effective <= 0), every consumer MUST check
 * for exhaustion BEFORE passing the cap into capacity locks/limits (where 0 means "uncapped").
 *
 * Fail-closed pattern (sweeper/preflight):
 *   capPlanner, err := s.operatorCapacityPlanner(...)
 *   if err != nil { ... }
 *   if driveOperatorCapacityExhausted(planner, capPlanner) {
 *       return ... // admit nothing, skip
 *   }
 *   // safe to use capPlanner
 *
 * Fail-closed pattern (combo_align):
 *   if maxDriveCells > 0 && effectiveCap <= 0 {
 *       continue // exhausted, skip
 *   }
 *
 * This guard detects when operatorCapacityPlanner or effectiveOperatorAnimalCap is called
 * and the result flows into a sink (lock/limit call) WITHOUT an exhaustion check nearby.
 */

function checkFile(filepath, source, callsitesSeen = []) {
  const lines = source.split('\n');
  const violations = [];

  // Pattern: detect operatorCapacityPlanner or effectiveOperatorAnimalCap calls
  // followed by sink usage (limitUnbatchedSelectionByDriveAnimals, lockAndRefreshDriveCapacity, etc)
  // without driveOperatorCapacityExhausted or exhaustion guard in between.

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    // Skip if this line has ignore annotation
    if (line.includes('operator-cap-fail-closed:ignore')) continue;

    // Look for operatorCapacityPlanner* or effectiveOperatorAnimalCap* calls.
    // The `\w*` is load-bearing: production renamed the planner to
    // operatorCapacityPlannerForTargets(, so the old anchored spelling matched
    // NONE of the 11 real callsites in sweeper.go / preflight.go /
    // park_consolidation.go. Measured 2026-09-23: replacing the canonical
    // fail-closed check in sweeper.go:1332 with `if false {` left this guard
    // printing "PASS — all callsites properly guarded".
    if (/(operatorCapacityPlanner\w*|effectiveOperatorAnimalCap\w*)\s*\(/.test(line)) {
      callsitesSeen.push(`${filepath}:${i + 1}`);
      // Look ahead up to 30 lines for sink calls or exhaustion checks
      let foundSink = false;
      let foundGuard = false;

      for (let j = i; j < Math.min(i + 30, lines.length); j++) {
        const lookAhead = lines[j];

        // Check for fail-closed guards
        if (/driveOperatorCapacityExhausted\s*\(/.test(lookAhead)) {
          foundGuard = true;
          break;
        }

        // Check for combo_align style guards: maxDriveCells > 0 && (effectiveCap|effectiveDriveCap) <= 0
        if (/maxDriveCells\s*>\s*0\s*&&\s*(effectiveCap|effectiveDriveCap)\s*<=\s*0/.test(lookAhead)) {
          foundGuard = true;
          break;
        }

        // Check for (effectiveCap|effectiveDriveCap) <= 0 guards with control flow
        if (/if\s+(maxDriveCells|maxShotsPerAnimalPerDrive)/.test(lookAhead) && /<=\s*0/.test(lookAhead)) {
          foundGuard = true;
          break;
        }

        // Look for sink calls: these consume the cap without checking
        if (/(limitUnbatchedSelectionByDriveAnimals|limitParkSelectionByDriveAnimals|lockAndRefreshDriveCapacity|comboBatchExceedsDriveCapacityAtDate)\s*\(/.test(lookAhead)) {
          foundSink = true;
          break;
        }
      }

      // If we found a sink before finding a guard, that's a leak
      if (foundSink && !foundGuard) {
        violations.push({
          file: filepath,
          line: i + 1,
          text: line.trim(),
        });
      }
    }
  }

  return violations;
}

function main() {
  const args = process.argv.slice(2);

  if (args.includes('--self-test')) {
    // Adversarial self-test: the guard MUST flag an unguarded callsite and MUST
    // pass a properly fail-closed one. A green self-test that doesn't actually
    // exercise the guard is exactly the failure mode this whole guard exists to prevent.
    const badFixture = [
      'func (s *SweeperService) leak(ctx context.Context) {',
      '\tcapPlanner, _ := s.operatorCapacityPlanner(ctx, tenantID, parkID, day, planner, session)',
      '\t_ = limitUnbatchedSelectionByDriveAnimals(now, rows, ids, day, capPlanner, session)',
      '}',
    ].join('\n');
    const guardedFixture = [
      'func (s *SweeperService) safe(ctx context.Context) {',
      '\tcapPlanner, _ := s.operatorCapacityPlanner(ctx, tenantID, parkID, day, planner, session)',
      '\tif driveOperatorCapacityExhausted(planner, capPlanner) {',
      '\t\treturn',
      '\t}',
      '\t_ = limitUnbatchedSelectionByDriveAnimals(now, rows, ids, day, capPlanner, session)',
      '}',
    ].join('\n');
    const comboFixture = [
      'func (s *SweeperService) combo(ctx context.Context) {',
      '\teffectiveDriveCap, _ := s.effectiveOperatorAnimalCap(ctx, tenantID, parkID, target, maxDriveCells, session)',
      '\tif maxDriveCells > 0 && effectiveDriveCap <= 0 {',
      '\t\treturn',
      '\t}',
      '\t_ = comboBatchExceedsDriveCapacityAtDate(batch, target, effectiveDriveCap, session)',
      '}',
    ].join('\n');
    // The spelling production ACTUALLY uses. The old fixtures all hand-wrote
    // `s.operatorCapacityPlanner(`, which is why this guard stayed green while
    // seeing 1 of 14 real callsites.
    const realSpellingFixture = [
      'func (s *SweeperService) real(ctx context.Context) {',
      '\tcapPlanner, err := s.operatorCapacityPlannerForTargets(ctx, tenantID, parkID, day, planner, session, targetIDs)',
      '\t_ = limitUnbatchedSelectionByDriveAnimals(now, rows, ids, day, capPlanner, session)',
      '}',
    ].join('\n');
    if (checkFile('selftest-real-spelling.go', realSpellingFixture).length === 0) {
      console.error('operator-cap-fail-closed self-test FAILED: did NOT flag an unguarded operatorCapacityPlannerForTargets -> limit sink');
      return 1;
    }
    // And the floor: a detector that sees nothing must not be able to pass.
    if (MIN_CALLSITES < 1) {
      console.error('operator-cap-fail-closed self-test FAILED: MIN_CALLSITES floor disabled');
      return 1;
    }
    const badViolations = checkFile('selftest-bad.go', badFixture);
    const guardedViolations = checkFile('selftest-guarded.go', guardedFixture);
    const comboViolations = checkFile('selftest-combo.go', comboFixture);
    if (badViolations.length === 0) {
      console.error('operator-cap-fail-closed self-test FAILED: did NOT flag an unguarded operatorCapacityPlanner -> limit sink');
      return 1;
    }
    if (guardedViolations.length !== 0) {
      console.error('operator-cap-fail-closed self-test FAILED: flagged a properly driveOperatorCapacityExhausted-guarded callsite');
      return 1;
    }
    if (comboViolations.length !== 0) {
      console.error('operator-cap-fail-closed self-test FAILED: flagged a properly maxDriveCells>0 && effectiveCap<=0 combo-align guard');
      return 1;
    }
    console.log('✓ operator-cap-fail-closed self-test passed (flags unguarded sink; passes exhausted-check + combo-align guards)');
    return 0;
  }

  // Script-relative, NOT cwd. `find . ... || true` meant running this guard from
  // any other directory produced zero files and a PASS.
  const repoRoot = (args[0] && !args[0].startsWith('--')) ? args[0] : resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');

  try {
    // Find all Go files in obligation/app excluding tests
    const scanDir = join(repoRoot, 'backend/internal/obligation/app');
    if (!existsSync(scanDir)) {
      console.error(`operator-cap-fail-closed: FAIL — scan directory ${scanDir} does not exist. This guard checked nothing.`);
      return 1;
    }
    const files = readdirSync(scanDir)
      .filter((f) => f.endsWith('.go') && !f.endsWith('_test.go'))
      .map((f) => join(scanDir, f));
    if (files.length === 0) {
      console.error(`operator-cap-fail-closed: FAIL — no Go files under ${scanDir}. This guard checked nothing.`);
      return 1;
    }

    const callsitesSeen = [];
    let totalViolations = 0;

    for (const filepath of files) {
      try {
        const source = readFileSync(filepath, 'utf8');
        const violations = checkFile(filepath, source, callsitesSeen);

        if (violations.length > 0) {
          console.error(`operator-cap-fail-closed: ${filepath}`);
          violations.forEach(v => {
            console.error(`  line ${v.line}: ${v.text}`);
          });
          totalViolations += violations.length;
        }
      } catch (e) {
        // Skip unreadable files
      }
    }

    // Floor. A detector that matches nothing reports "all callsites properly
    // guarded", which is a verdict on a check that did not run (CONTRACT.md §4).
    // 18 callsites exist today; the floor is set below that so ordinary
    // refactoring does not trip it, but a detector that goes blind does.
    if (callsitesSeen.length < MIN_CALLSITES) {
      console.error(
        `operator-cap-fail-closed: FAIL — the detector matched ${callsitesSeen.length} callsite(s), expected at least ${MIN_CALLSITES}.`,
      );
      console.error('  The planner was probably renamed again. Fix the detector; do not lower the floor.');
      return 1;
    }

    if (totalViolations === 0) {
      console.log(`operator-cap-fail-closed: PASS — ${callsitesSeen.length} callsite(s) checked, all properly guarded`);
      return 0;
    } else {
      console.error(`operator-cap-fail-closed: FAIL — ${totalViolations} unguarded callsite(s) found`);
      return 1;
    }
  } catch (e) {
    console.error('operator-cap-fail-closed: error:', e.message);
    return 1;
  }
}

process.exit(main());
