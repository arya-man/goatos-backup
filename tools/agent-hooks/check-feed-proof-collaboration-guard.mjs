#!/usr/bin/env node

// check-feed-proof-collaboration-guard.mjs — Feed distribution proof slots are a SHARED,
// PARALLEL workspace: one shed-session, three proof slots (weight photo, feed video, water
// video), filled by any mix of peer operators from different phones in any split. No slot
// gates another; capture is camera-only; processed media is ALWAYS compressed + overlay-
// burned before upload. See docs/product/feed-proof-collaboration.md (canonical contract,
// 2026-08-15) and docs/decisions/feed-distribution-verification.md (superseded "sequential"
// wording — do not resurrect it).
//
// Failure modes:
//   1. sequential-slot-gating — a FeedDistributionUiState slot-enabled getter references
//      ANOTHER slot's captured/status flag (e.g. waterVideoCaptureEnabled depending on
//      videoCaptured). Slots may depend only on their own in-flight capture state and the
//      session-level submitted/lock state.
//   2. mime-blind-backstop — CaptureRepository's Gate-3 backstop calls validateVideoFile on
//      the capture path without an image/-mime branch. The video duration probe on a JPEG
//      silently pushed every valid photo to the raw-original fallback THREE times
//      (field bugs 2026-08-15); the mime branch must stay.
//   3. processed-validation-mime-blind — CaptureRepository validates a PROCESSED artifact
//      without passing the output mime type (single-argument validateProcessedArtifact).
//   4. overlay-pipeline-dropped — AppProofMediaProcessor's photo or video path no longer
//      burns the audit overlay, or the photo path no longer JPEG-compresses. Raw,
//      overlay-free media reaching the server is always a defect, never a fallback outcome.
//   5. regression-tests-deleted — the pinned regression tests are missing:
//      ProcessedPhotoValidatorRegressionTest.kt (processor URI shape through the validator),
//      FeedDistributionSequenceTest.kt (slot independence), and the gate-3 real-JPEG test in
//      CaptureRepositoryTest.kt.

import { readFileSync, existsSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const failures = [];

// --self-test: prove the detectors fire on seeded violations before trusting a green run.
if (process.argv.includes('--self-test')) {
  const badGetter = `val waterVideoCaptureEnabled: Boolean
        get() = !isCapturingWaterVideo && videoCaptured && !isFinalSubmitted`;
  const gm = badGetter.match(/val waterVideoCaptureEnabled: Boolean\s*\n\s*get\(\) = ([^\n]*)/);
  if (!gm || !/\bvideoCaptured\b/.test(gm[1])) {
    console.error('self-test FAILED: sequential-slot-gating detector missed seeded violation');
    process.exit(1);
  }
  const badGate3 = `// Gate 3: Backstop validation
        val validationResult = proofArtifactValidator.validateVideoFile(localUri)
        dao.insert(entity)`;
  const g3 = badGate3.match(/Gate 3[\s\S]{0,1500}?dao\.insert/);
  if (!g3 || /startsWith\("image\/"\)/.test(g3[0])) {
    console.error('self-test FAILED: mime-blind-backstop detector missed seeded violation');
    process.exit(1);
  }
  if (!/proofArtifactValidator\.validateProcessedArtifact\(\s*processed\.outputUri\s*\)/
    .test('proofArtifactValidator.validateProcessedArtifact(processed.outputUri)')) {
    console.error('self-test FAILED: processed-validation-mime-blind detector missed seeded violation');
    process.exit(1);
  }
  console.log('feed-proof-collaboration-guard self-test OK');
  process.exit(0);
}

const read = (rel) => {
  const p = join(ROOT, rel);
  return existsSync(p) ? readFileSync(p, 'utf8') : null;
};

// --- Mode 1: sequential-slot-gating -----------------------------------------------------
const uiStatePath =
  'apps/goatos-android/feature/feature-feed/src/main/kotlin/sg/mesha/goatos/feature/feed/FeedDistributionCompleteScreen.kt';
const uiState = read(uiStatePath);
if (uiState == null) {
  failures.push(`missing-file: ${uiStatePath}`);
} else {
  // Extract each slot-enabled getter body and assert it never names another slot's flags.
  const slots = [
    { name: 'feedWeightPhotoCaptureEnabled', foreign: [/videoCaptured/, /waterVideo/i, /\bvideoStatus\b/] },
    { name: 'videoCaptureEnabled', foreign: [/feedWeightPhoto/i, /waterVideo/i] },
    { name: 'waterVideoCaptureEnabled', foreign: [/feedWeightPhoto/i, /\bvideoCaptured\b/, /\bvideoStatus\b/] },
  ];
  for (const slot of slots) {
    const m = uiState.match(new RegExp(`val ${slot.name}: Boolean\\s*\\n\\s*get\\(\\) = ([^\\n]*(?:\\n\\s{8,}[^\\n]*)*)`));
    if (!m) {
      failures.push(`sequential-slot-gating: getter ${slot.name} not found in FeedDistributionUiState`);
      continue;
    }
    const body = m[1];
    for (const re of slot.foreign) {
      if (re.test(body)) {
        failures.push(
          `sequential-slot-gating: ${slot.name} references another slot (${re}) — slots must be independent/parallel`,
        );
      }
    }
  }
}

// --- Modes 2+3: capture backstop + processed validation must be mime-aware ---------------
const capRepoPath =
  'apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/capture/CaptureRepository.kt';
const capRepo = read(capRepoPath);
if (capRepo == null) {
  failures.push(`missing-file: ${capRepoPath}`);
} else {
  const gate3 = capRepo.match(/Gate 3[\s\S]{0,1500}?dao\.insert/);
  if (!gate3) {
    failures.push('mime-blind-backstop: Gate-3 backstop block not found before dao.insert');
  } else if (!/startsWith\("image\/"\)/.test(gate3[0]) || !/validateImageFile/.test(gate3[0])) {
    failures.push(
      'mime-blind-backstop: Gate-3 must branch image/* to validateImageFile — video probe on a JPEG forces raw fallback',
    );
  }
  if (/proofArtifactValidator\.validateProcessedArtifact\(\s*processed\.outputUri\s*\)/.test(capRepo)) {
    failures.push(
      'processed-validation-mime-blind: validateProcessedArtifact must receive processed.outputMimeType',
    );
  }
}

// --- Mode 4: overlay + compression pipeline intact ---------------------------------------
const procPath =
  'apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/capture/AppProofMediaProcessor.kt';
const proc = read(procPath);
if (proc == null) {
  failures.push(`missing-file: ${procPath}`);
} else {
  const photo = proc.match(/private suspend fun processPhoto[\s\S]*?\n    }/);
  const video = proc.match(/private suspend fun processVideo[\s\S]*?exportAwait/);
  if (!photo || !/drawAuditOverlay/.test(photo[0])) {
    failures.push('overlay-pipeline-dropped: processPhoto no longer burns the audit overlay');
  }
  if (!photo || !/Bitmap\.CompressFormat\.JPEG/.test(photo[0])) {
    failures.push('overlay-pipeline-dropped: processPhoto no longer JPEG-compresses the output');
  }
  if (!video || !/overlay/i.test(video[0])) {
    failures.push('overlay-pipeline-dropped: processVideo no longer references the overlay composition');
  }
}

// --- Mode 5: pinned regression tests present ---------------------------------------------
const requiredTests = [
  [
    'apps/goatos-android/core/core-data/src/test/kotlin/sg/mesha/goatos/core/data/capture/ProcessedPhotoValidatorRegressionTest.kt',
    /single-slash uri passes/,
  ],
  [
    'apps/goatos-android/app/src/test/kotlin/sg/mesha/goatos/viewmodel/FeedDistributionSequenceTest.kt',
    /independen/i,
  ],
  [
    'apps/goatos-android/core/core-data/src/test/kotlin/sg/mesha/goatos/core/data/capture/CaptureRepositoryTest.kt',
    /real jpeg passes gate3 backstop validation/,
  ],
];
for (const [rel, marker] of requiredTests) {
  const src = read(rel);
  if (src == null) failures.push(`regression-tests-deleted: missing ${rel}`);
  else if (!marker.test(src)) failures.push(`regression-tests-deleted: pinned test body missing in ${rel} (${marker})`);
}

// --- Report ------------------------------------------------------------------------------
if (failures.length > 0) {
  console.error('feed-proof-collaboration-guard FAILED:');
  for (const f of failures) console.error(`  - ${f}`);
  process.exit(1);
}
console.log('feed-proof-collaboration-guard OK');
