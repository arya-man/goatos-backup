#!/usr/bin/env node
// Pen visits must use the real shared proof preview surface, not only status icons/list badges.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const screenFile = "apps/goatos-android/feature/feature-pen-visits/src/main/kotlin/sg/mesha/goatos/feature/penvisits/PenVisitDetailScreen.kt";
const viewModelFile = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/PenVisitDetailViewModel.kt";

function assertIncludes(text, needle, message, findings) {
  if (!text.includes(needle)) findings.push(message);
}

function check(screenText, viewModelText) {
  const findings = [];
  assertIncludes(screenText, "ProofMediaPreview(", "PenVisitDetailScreen must render the shared ProofMediaPreview component.", findings);
  assertIncludes(screenText, "state.previewPath.isNotBlank()", "PenVisitDetailScreen must gate the preview on a real previewPath, not a tab/list icon.", findings);
  assertIncludes(screenText, "path = state.previewPath", "PenVisitDetailScreen must feed the durable proof previewPath into ProofMediaPreview.", findings);
  assertIncludes(screenText, "kind = ProofMediaPreviewKind.Video", "PenVisitDetailScreen must render the pen proof as a video preview.", findings);
  assertIncludes(
    screenText,
    "onPreviewAction = { action -> onEvent(PenVisitDetailEvent.ProofPreviewAction(action)) }",
    "PenVisitDetailScreen must route preview controls through PenVisitDetailEvent.ProofPreviewAction.",
    findings,
  );
  assertIncludes(viewModelText, "AnalyticsEventsPenVisits.PROOF_PREVIEW_ACTION", "PenVisitDetailViewModel must track pen proof preview actions.", findings);
  assertIncludes(viewModelText, "proofCaptureRepository.observeLatest(videoSlot)", "PenVisitDetailViewModel must derive preview state from the durable pen proof slot.", findings);
  assertIncludes(
    viewModelText,
    "previewPath = proof?.let { it.processedUri ?: it.localUri }.orEmpty()",
    "PenVisitDetailViewModel must expose the saved proof URI as previewPath.",
    findings,
  );
  return findings;
}

function selfTest() {
  const goodScreen = `
    if (state.previewPath.isNotBlank()) {
      ProofMediaPreview(
        path = state.previewPath,
        kind = ProofMediaPreviewKind.Video,
        onPreviewAction = { action -> onEvent(PenVisitDetailEvent.ProofPreviewAction(action)) },
      )
    }
  `;
  const goodViewModel = `
    analytics.track(AnalyticsEventsPenVisits.PROOF_PREVIEW_ACTION)
    proofCaptureRepository.observeLatest(videoSlot)
    previewPath = proof?.let { it.processedUri ?: it.localUri }.orEmpty()
  `;
  const badScreen = `
    Icon(MeshaIcons.Tasks, contentDescription = "For me")
    Text("No tasks yet")
  `;
  if (check(goodScreen, goodViewModel).length !== 0) {
    console.error("android-pen-visit-proof-preview self-test failed: good fixture rejected");
    process.exit(1);
  }
  if (check(badScreen, goodViewModel).length === 0) {
    console.error("android-pen-visit-proof-preview self-test failed: icon-only fixture accepted");
    process.exit(1);
  }
  console.log("android-pen-visit-proof-preview self-test: ok");
}

if (process.argv.includes("--self-test")) selfTest();

const findings = check(
  readFileSync(resolve(repo, screenFile), "utf8"),
  readFileSync(resolve(repo, viewModelFile), "utf8"),
);
if (findings.length) {
  console.error("android-pen-visit-proof-preview guard FAILED:");
  for (const finding of findings) console.error(`  ${finding}`);
  process.exit(1);
}

console.log("android-pen-visit-proof-preview: ok");
