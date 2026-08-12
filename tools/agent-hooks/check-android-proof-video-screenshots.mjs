#!/usr/bin/env node
// Ensures proof-video UI review covers every known operator camera surface and state.

import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const testFile = "apps/goatos-android/app/src/test/kotlin/sg/mesha/goatos/ui/ProofProcessingShowcaseScreenshotTest.kt";

const surfaces = [
  "vaccination_scan",
  "vaccination_shed",
  "weighing_individual",
  "weighing_lumpsum",
  "birth_death_workflow",
  "shifting",
  "feed_distribution",
  "feed_complete",
  "feed_packing",
  "feed_transport",
  "milk_preparation",
  "milk_feeding",
];

const states = [
  "PREPARING",
  "COMPRESSING",
  "UPLOADING",
  "UPLOADING_ORIGINAL",
  "UPLOADED",
  "RETRYING",
  "RETRY_ORIGINAL",
  "RECORD_AGAIN",
];

const snapshots = {
  vaccination_scan: "sg.mesha.goatos.ui_ProofProcessingShowcaseScreenshotTest_vaccinationScanFeature_proof_processing_10_vaccination_scan.png",
  vaccination_shed: "sg.mesha.goatos.ui_ProofProcessingShowcaseScreenshotTest_vaccinationSubmitFormFeature_proof_processing_11_vaccination_submit_form.png",
  weighing_individual: "sg.mesha.goatos.ui_ProofProcessingShowcaseScreenshotTest_weighingIndividualFeature_proof_processing_12_weighing_individual.png",
  weighing_lumpsum: "sg.mesha.goatos.ui_ProofProcessingShowcaseScreenshotTest_weighingShedFeature_proof_processing_13_weighing_shed.png",
  birth_death_workflow: "sg.mesha.goatos.ui_ProofProcessingShowcaseScreenshotTest_birthDeathWorkflowFeature_proof_processing_14_birth_death_workflow.png",
  shifting: "sg.mesha.goatos.ui_ProofProcessingShowcaseScreenshotTest_shiftingFeature_proof_processing_15_shifting.png",
  feed_distribution: "sg.mesha.goatos.ui_ProofProcessingShowcaseScreenshotTest_feedDistributionFeature_proof_processing_16_feed_distribution.png",
  feed_complete: "sg.mesha.goatos.ui_ProofProcessingShowcaseScreenshotTest_legacyFeedCompleteFeature_proof_processing_17_feed_complete_optional.png",
  feed_packing: "sg.mesha.goatos.ui_ProofProcessingShowcaseScreenshotTest_feedPackingFeature_proof_processing_18_feed_packing.png",
  feed_transport: "sg.mesha.goatos.ui_ProofProcessingShowcaseScreenshotTest_feedTransportFeature_proof_processing_19_feed_transport.png",
  milk_preparation: "sg.mesha.goatos.ui_ProofProcessingShowcaseScreenshotTest_milkPreparationFeature_proof_processing_20_milk_preparation.png",
  milk_feeding: "sg.mesha.goatos.ui_ProofProcessingShowcaseScreenshotTest_milkFeedingFeature_proof_processing_21_milk_feeding.png",
};

const snapshotDir = "apps/goatos-android/app/src/test/snapshots/images";

function check(text) {
  const missing = [];
  for (const surface of surfaces) {
    if (!text.includes(surface)) missing.push(`surface:${surface}`);
    const snapshot = snapshots[surface];
    if (!snapshot || !existsSync(resolve(repo, snapshotDir, snapshot))) {
      missing.push(`snapshot:${surface}`);
    }
  }
  for (const state of states) {
    if (!text.includes(`ProofUiPhase.${state}`) && !text.includes(`${state}(`)) missing.push(`state:${state}`);
  }
  return missing;
}

function selfTest() {
  const ok = Object.keys(snapshots).length === surfaces.length &&
    surfaces.every((surface) => snapshots[surface]) &&
    check("vaccination_scan ProofUiPhase.PREPARING").length > 0;
  console.log(ok ? "android-proof-video-screenshots self-test: ok" : "android-proof-video-screenshots self-test: FAIL");
  process.exit(ok ? 0 : 1);
}

if (process.argv.includes("--self-test")) selfTest();

let text = "";
try {
  text = readFileSync(resolve(repo, testFile), "utf8");
} catch {
  console.error(`android-proof-video-screenshots guard FAILED — missing ${testFile}`);
  process.exit(1);
}

const missing = check(text);
if (missing.length) {
  console.error("android-proof-video-screenshots guard FAILED — missing proof-video screenshot coverage:");
  for (const item of missing) console.error(`  ${item}`);
  process.exit(1);
}

console.log("android-proof-video-screenshots: ok");
