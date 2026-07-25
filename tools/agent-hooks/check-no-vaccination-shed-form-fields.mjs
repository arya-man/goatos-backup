#!/usr/bin/env node
/**
 * CI Guard: Prevent banned vaccination SOP form fields from being reintroduced
 *
 * Fails if any of the banned field keys reappear in:
 * - Migrations (*.sql) as a field being re-added to a vaccination SOP form_dsl
 * - Seed files (backend/cmd/seed-*) as a field being seeded into vaccination SOP form_dsl
 *
 * Ignores:
 * - vaccination_completions column definitions (server-side, not form fields)
 * - Protocol matrix metadata (route_site in the matrix is clinical metadata, not a form field)
 * - Past records in history
 *
 * ADR: docs/decisions/vaccination-shed-ack-not-form.md
 * Migration: backend/migrations/postgres/000007_r50_vaccination_shed_ack_dml.sql
 */

import { execSync } from 'child_process';
import { readFileSync } from 'fs';
import path from 'path';

const BANNED_FIELDS = [
  'vaccine_lot_id',
  'cold_chain_verified',
  'dose_ml_given',
  'doses',  // if it was a manual-entry field
  'route_site',
  'administered_at',
  'adverse_reaction',
  'adverse_reaction_notes',
  'vial_lot_video',
  'administration_video',
  'extra_video',
];

const VACCINATION_SOP_CODES = ['vaccination.drive', 'vaccination.session'];

function hasAllowedShedVideoPolicy(content) {
  return /shed_video/.test(content) &&
    /proof_mode[\s\S]{0,120}shed_level_video/i.test(content) &&
    /(?:proof_subject|subject_scope)[\s\S]{0,120}shed/i.test(content);
}

// Helper: check if a line is a migration/seed context line (not just a table definition)
function isFormFieldContext(line, bannedField) {
  // Form field contexts:
  // 1. form_dsl mentions
  // 2. jsonb_build_object with 'key' = the banned field
  // 3. SOP version field definitions
  const formIndicators = [
    'form_dsl',
    `'key'.*${bannedField}`,
    `"key".*${bannedField}`,
    `key.*${bannedField}`,
  ];
  return formIndicators.some(indicator =>
    new RegExp(indicator, 'i').test(line)
  );
}

// Check migrations
function checkMigrations() {
  console.log('Checking migrations...');
  let failures = [];
  try {
    const migFiles = execSync('find backend/migrations -name "*.sql" -type f',
      { encoding: 'utf-8' }).trim().split('\n');

    for (const file of migFiles) {
      if (!file) continue;
      // Skip baseline (000001) - it's the initial seed, not a re-introduction
      if (file.includes('000001_')) continue;

      const content = readFileSync(file, 'utf-8');
      // Only check the Up section, ignore Down (reverting is allowed)
      const upMatch = content.match(/--\s*\+goose\s+Up([\s\S]*?)(?:--\s*\+goose\s+Down|$)/i);
      if (!upMatch) continue;

      const upSection = upMatch[1];

      for (const field of BANNED_FIELDS) {
        if (upSection.includes(field)) {
          const lines = upSection.split('\n');
          const startLine = content.indexOf(upSection);
          for (let i = 0; i < lines.length; i++) {
            const line = lines[i];
            if (line.includes(field) && isFormFieldContext(line, field)) {
              // Check if it looks like re-adding (jsonb_build_object, string appending, etc)
              if (/jsonb_build_object|'key'|"key"|field|--.*add.*field/i.test(line)) {
                failures.push({
                  file,
                  line: i + 1,
                  text: line.trim(),
                  issue: `Re-introducing banned field '${field}' in form_dsl (Up section)`,
                });
              }
            }
          }
        }
      }

      if (upSection.includes('shed_video') && !hasAllowedShedVideoPolicy(upSection)) {
        const lines = upSection.split('\n');
        for (let i = 0; i < lines.length; i++) {
          const line = lines[i];
          if (line.includes('shed_video') && isFormFieldContext(line, 'shed_video')) {
            failures.push({
              file,
              line: i + 1,
              text: line.trim(),
              issue: "Field 'shed_video' is allowed only with proof_mode=shed_level_video and shed proof subject/scope",
            });
          }
        }
      }
    }
  } catch (e) {
    console.error('Error checking migrations:', e.message);
  }
  return failures;
}

// Check seed files
function checkSeeds() {
  console.log('Checking seed files...');
  let failures = [];
  try {
    const seedFiles = execSync('find backend/cmd/seed-* -name "*.go" -type f 2>/dev/null || true',
      { encoding: 'utf-8', stdio: ['pipe', 'pipe', 'ignore'] }).trim().split('\n');

    for (const file of seedFiles) {
      if (!file) continue;
      try {
        const content = readFileSync(file, 'utf-8');

        for (const field of BANNED_FIELDS) {
          if (content.includes(field)) {
            const lines = content.split('\n');
            for (let i = 0; i < lines.length; i++) {
              const line = lines[i];
              // Skip protocol matrix metadata like RouteSite in scheduleRow
              if (/scheduleRow|matrix|protocol.*metadata/i.test(line)) {
                continue;
              }
              // Check vaccination SOP form context
              if (line.includes(field) &&
                  (line.includes('form_dsl') ||
                   line.includes('Answers') ||
                   line.includes('fields') ||
                   line.includes('vaccination.*sop'))) {
                failures.push({
                  file,
                  line: i + 1,
                  text: line.trim(),
                  issue: `Banned field '${field}' referenced in vaccination SOP context`,
                });
              }
            }
          }
        }

        if (content.includes('shed_video') && !hasAllowedShedVideoPolicy(content)) {
          const lines = content.split('\n');
          for (let i = 0; i < lines.length; i++) {
            const line = lines[i];
            if (line.includes('shed_video') &&
                (line.includes('form_dsl') ||
                 line.includes('Answers') ||
                 line.includes('fields') ||
                 line.includes('vaccination.*sop'))) {
              failures.push({
                file,
                line: i + 1,
                text: line.trim(),
                issue: "Field 'shed_video' is allowed only with proof_mode=shed_level_video and shed proof subject/scope",
              });
            }
          }
        }
      } catch (e) {
        // Skip files that can't be read
      }
    }
  } catch (e) {
    // find may not have results, that's ok
  }
  return failures;
}

// Main
function main() {
  console.log('CI Guard: vaccination-shed-ack-not-form');
  console.log('=========================================\n');

  const migFailures = checkMigrations();
  const seedFailures = checkSeeds();

  const allFailures = [...migFailures, ...seedFailures];

  if (allFailures.length === 0) {
    console.log('✓ No banned vaccination shed form fields found.');
    console.log('✓ ADR vaccination-shed-ack-not-form.md is enforced.\n');
    process.exit(0);
  }

  console.error('\n✗ GUARD FAILED: Banned vaccination shed form fields detected\n');
  console.error('Banned fields (do not reintroduce):');
  console.error(BANNED_FIELDS.map(f => `  - ${f}`).join('\n'));
  console.error('  - shed_video unless proof_mode=shed_level_video with shed proof subject/scope');
  console.error('\nViolations:\n');

  for (const failure of allFailures) {
    console.error(`${failure.file}:${failure.line}`);
    console.error(`  Issue: ${failure.issue}`);
    console.error(`  ${failure.text}`);
    console.error('');
  }

  console.error('See: docs/decisions/vaccination-shed-ack-not-form.md');
  console.error('See: backend/migrations/postgres/000007_r50_vaccination_shed_ack_dml.sql\n');

  process.exit(1);
}

// Adversarial self-test: prove the detector flags a reintroduced banned key
// and does not flag a clean line. Exits non-zero if the guard has gone blind.
function selfTest() {
  console.log('vaccination-shed-ack guard self-test');
  const bannedLine = `"fields": [{"key": "cold_chain_verified", "type": "boolean"}], "form_dsl": {}`;
  const badShedVideo = `"fields": [{"key": "shed_video", "type": "video_proof"}], "form_dsl": {}`;
  const goodShedVideo = `"proof_mode": "shed_level_video", "subject_scope": "shed", "fields": [{"key": "shed_video", "type": "video_proof", "proof_subject": "shed"}]`;
  const cleanLine = `"fields": [{"key": "goat_ids", "type": "goat_scan"}]`;
  const detectsBanned = isFormFieldContext(bannedLine, 'cold_chain_verified');
  const rejectsLooseShedVideo = !hasAllowedShedVideoPolicy(badShedVideo);
  const acceptsPolicyShedVideo = hasAllowedShedVideoPolicy(goodShedVideo);
  const ignoresClean = !isFormFieldContext(cleanLine, 'cold_chain_verified');
  if (detectsBanned && rejectsLooseShedVideo && acceptsPolicyShedVideo && ignoresClean) {
    console.log('✓ self-test passed: detector flags a reintroduced banned form field.');
    process.exit(0);
  }
  console.error('✗ self-test FAILED: detector is blind to banned form fields.');
  console.error(`  detectsBanned=${detectsBanned} rejectsLooseShedVideo=${rejectsLooseShedVideo} acceptsPolicyShedVideo=${acceptsPolicyShedVideo} ignoresClean=${ignoresClean}`);
  process.exit(1);
}

if (process.argv.includes('--self-test')) {
  selfTest();
} else {
  main();
}
