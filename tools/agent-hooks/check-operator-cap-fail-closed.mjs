#!/usr/bin/env node

import { readFileSync } from 'fs';
import { execSync } from 'child_process';

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

function checkFile(filepath, source) {
  const lines = source.split('\n');
  const violations = [];

  // Pattern: detect operatorCapacityPlanner or effectiveOperatorAnimalCap calls
  // followed by sink usage (limitUnbatchedSelectionByDriveAnimals, lockAndRefreshDriveCapacity, etc)
  // without driveOperatorCapacityExhausted or exhaustion guard in between.

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    // Skip if this line has ignore annotation
    if (line.includes('operator-cap-fail-closed:ignore')) continue;

    // Look for operatorCapacityPlanner or effectiveOperatorAnimalCap calls
    if (/(operatorCapacityPlanner|effectiveOperatorAnimalCap)\s*\(/.test(line)) {
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

  const repoRoot = process.argv[2] || '.';

  try {
    // Find all Go files in obligation/app excluding tests
    const files = execSync(`find ${repoRoot}/backend/internal/obligation/app -name '*.go' ! -name '*_test.go' -type f 2>/dev/null || true`)
      .toString()
      .trim()
      .split('\n')
      .filter(f => f.length > 0);

    let totalViolations = 0;

    for (const filepath of files) {
      try {
        const source = readFileSync(filepath, 'utf8');
        const violations = checkFile(filepath, source);

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

    if (totalViolations === 0) {
      console.log('operator-cap-fail-closed: PASS — all callsites properly guarded');
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
