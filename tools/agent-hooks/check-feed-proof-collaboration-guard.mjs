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
//   1. sequential-slot-gating — the card is a LIST of slots since FEED SOP (2026-09-16), so the
//      per-slot enablement is FeedDistributionSlotUi.captureEnabled. Its getter may read only
//      the slot's OWN fields (isCapturing/captured/status/required) — never the list, the
//      session state or a sibling. FeedDistributionUiState must not grow named per-slot fields
//      again (feedWeightPhotoCaptured / waterVideoCaptureEnabled ...), which is how a
//      "step unlocks next step" chain re-enters. The ViewModel's capture gate must read
//      `slot.captureEnabled`, not a sibling or the whole list.
//   2. mime-blind-backstop — CaptureRepository's capture path must persist the camera URI in
//      Room before Gate-3 validation, then Gate-3 must branch by mime type. The Room-first order
//      keeps one-time field work retryable when validation/processing fails; the mime branch keeps
//      the video duration probe from rejecting valid JPEG proof photos.
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


// Mode 1 detectors (shared by the self-test and the real run).
const SLOT_OWN_FIELDS = new Set(['isCapturing', 'captured', 'status', 'required', 'isQueuedForSubmit', 'FeedDistributionProofStatus', 'EMPTY', 'QUEUED', 'UPLOADING', 'SYNCED', 'FAILED']);
function slotGetterBody(src) {
  const m = src.match(/val captureEnabled: Boolean\s*\n\s*get\(\) = ([^\n]*(?:\n\s{8,}[^\n]*)*)/);
  return m ? m[1] : null;
}
function slotGetterForeignTokens(src) {
  const body = slotGetterBody(src);
  if (body == null) return ['<getter not found>'];
  const idents = body.match(/[A-Za-z_][A-Za-z0-9_]*/g) || [];
  return idents.filter((t) => !SLOT_OWN_FIELDS.has(t));
}
function perSlotFieldsInState(src) {
  const m = src.match(/data class FeedDistributionUiState\([\s\S]*?\n\}/);
  if (!m) return ['<FeedDistributionUiState not found>'];
  return (m[0].match(/\b(?:feedWeightPhoto|waterVideo|video|feedVideo)[A-Za-z]*(?:Captured|Status|CaptureEnabled|Capturing)\b/g) || []);
}
function captureGateReadsSiblings(src) {
  const m = src.match(/private fun captureSlot\([\s\S]*?\n\s*if \(([^\n]*)\) return/);
  if (!m) return true; // no gate at all is a finding too
  const gate = m[1];
  return !/\bslot\.captureEnabled\b/.test(gate) || /\bslots\b/.test(gate) || /\.(any|all|none)\s*\{/.test(gate);
}

// --self-test: prove the detectors fire on seeded violations before trusting a green run.
if (process.argv.includes('--self-test')) {
  const badGetter = `val captureEnabled: Boolean
        get() = !isCapturing && siblingCaptured && !isFinalSubmitted`;
  if (slotGetterForeignTokens(badGetter).length === 0) {
    console.error('self-test FAILED: sequential-slot-gating detector missed seeded violation (foreign token)');
    process.exit(1);
  }
  const badState = `data class FeedDistributionUiState(
    val slots: List<FeedDistributionSlotUi> = emptyList(),
    val waterVideoCaptured: Boolean = false,
) {
    val videoCaptureEnabled: Boolean
        get() = feedWeightPhotoCaptured`;
  if (perSlotFieldsInState(badState).length === 0) {
    console.error('self-test FAILED: sequential-slot-gating detector missed seeded per-slot state field');
    process.exit(1);
  }
  const badGate = `private fun captureSlot(slotKey: String, requestedKind: String?) {
        val slot = _state.value.slot(slotKey) ?: return
        if (!slot.captureEnabled || _state.value.slots.any { it.isCapturing } || shedId.isBlank()) return`;
  if (!captureGateReadsSiblings(badGate)) {
    console.error('self-test FAILED: sequential-slot-gating detector missed seeded ViewModel gate on the slot list');
    process.exit(1);
  }
  const badGate3 = `dao.insert(entity)
        // Gate 3: Backstop validation
        val validationResult = proofArtifactValidator.validateVideoFile(localUri)
        if (!validationResult.isValid) return AppResult.Err("bad")`;
  const g3 = badGate3.match(/dao\.insert\(entity\)[\s\S]{0,2500}?Gate 3[\s\S]{0,2500}?if \(!validationResult\.isValid\)/);
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
// The slot model moved to core-ui (SOP card, 2026-09-16); FeedDistributionSlotUi is a typealias of
// SopSlotUi, so the per-slot enablement contract is checked where the class now lives.
const slotModelPath =
  'apps/goatos-android/core/core-ui/src/main/kotlin/sg/mesha/goatos/core/ui/sop/SopCard.kt';
const uiState = read(uiStatePath);
const slotModel = read(slotModelPath);
if (uiState == null || slotModel == null) {
  failures.push(`missing-file: ${uiState == null ? uiStatePath : slotModelPath}`);
} else {
  const slotUi = slotModel.match(/data class SopSlotUi\([\s\S]*?\n\}/);
  if (!slotUi) {
    failures.push('sequential-slot-gating: SopSlotUi (FeedDistributionSlotUi) not found');
  } else {
    for (const tok of slotGetterForeignTokens(slotUi[0])) {
      failures.push(
        `sequential-slot-gating: FeedDistributionSlotUi.captureEnabled reads \`${tok}\` — a slot is enabled by its own state only, never a sibling or the session`,
      );
    }
  }
  for (const f of perSlotFieldsInState(uiState)) {
    failures.push(`sequential-slot-gating: FeedDistributionUiState names a per-slot field \`${f}\` — slots are a card-driven list, not fixed fields`);
  }
}
const distVmPath =
  'apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedDistributionCompleteViewModel.kt';
const distVm = read(distVmPath);
if (distVm == null) {
  failures.push(`missing-file: ${distVmPath}`);
} else if (captureGateReadsSiblings(distVm)) {
  failures.push('sequential-slot-gating: FeedDistributionCompleteViewModel.captureSlot gates on something other than slot.captureEnabled (a sibling or the slot list)');
}

// --- Modes 2+3: capture backstop + processed validation must be mime-aware ---------------
const capRepoPath =
  'apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/capture/CaptureRepository.kt';
const capRepo = read(capRepoPath);
if (capRepo == null) {
  failures.push(`missing-file: ${capRepoPath}`);
} else {
  const gate3 = capRepo.match(/dao\.insert\(entity\)[\s\S]{0,2500}?Gate 3[\s\S]{0,2500}?if \(!validationResult\.isValid\)/);
  if (!gate3) {
    failures.push('mime-blind-backstop: Room-first insert followed by Gate-3 backstop block not found');
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


// --- Mode 6: non-canonical flow RATCHET — the set is now EMPTY, permanently ---------------
// docs/product/feed-proof-collaboration.md + the offline-sync architecture decision: milk
// preparation, milk feeding, and workflow_detail were the LAST flows outside
// ProofIdentity/EvidenceSlot; all three migrated (see CaptureModels.kt kdoc). The
// NON_CANONICAL_PROOF_KEY_FLOWS constant is deleted entirely — its reappearance in any form
// means a screen is opting back out of the shared model, so any match fails the guard.
{
  const cm = read('apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/capture/CaptureModels.kt');
  if (cm == null) failures.push('missing-file: CaptureModels.kt');
  else if (/NON_CANONICAL_PROOF_KEY_FLOWS/.test(cm)) {
    failures.push('non-canonical-ratchet: NON_CANONICAL_PROOF_KEY_FLOWS reappeared — the set must stay deleted, migrate the flow instead of opting out');
  }
}

// --- Report ------------------------------------------------------------------------------
if (failures.length > 0) {
  console.error('feed-proof-collaboration-guard FAILED:');
  for (const f of failures) console.error(`  - ${f}`);
  process.exit(1);
}
console.log('feed-proof-collaboration-guard OK');
