#!/usr/bin/env node
// Guard the vaccination/weighing proof overlay contract:
// - per-animal captures pass an RFID into the shared processor;
// - shed/lump-sum captures do not pass RFID;
// - captions/titles name the feature/task context;
// - fresh vaccination scans do not enter visible/Room scanned state until camera returns video;
// - proof-uploaded vaccination sheds stay openable until final submit/verification state;
// - submitted/review vaccination sheds do not reopen scan because of stale open counts;
// - phone QA seed keeps tiny 2/3-animal sheds plus a two-vaccine case.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const SCAN_VM = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/ScanViewModel.kt";
const SHEDS_VM = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/ShedsViewModel.kt";
const WEIGHING_VM = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/WeighingViewModel.kt";
const FEED_PACKING_VM = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedPackingCompleteViewModel.kt";
const FEED_TRANSPORT_VM = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedTransportViewModel.kt";
const FEED_COMPLETE_VM = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedCompleteViewModel.kt";
const OVERLAY_CONTEXT = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/ProofOverlayContext.kt";
const SEED = "tools/local/phone-qa-throwaway-seed.sh";

function lineNo(text, needle) {
  const index = text.indexOf(needle);
  return index < 0 ? 1 : text.slice(0, index).split("\n").length;
}

function expect(text, rel, needle, reason) {
  if (!text.includes(needle)) {
    return { rel, line: 1, reason, snippet: needle };
  }
  return null;
}

function expectOrder(text, rel, first, second, reason) {
  const firstIndex = text.indexOf(first);
  const secondIndex = text.indexOf(second);
  if (firstIndex < 0 || secondIndex < 0 || firstIndex >= secondIndex) {
    return { rel, line: 1, reason, snippet: firstIndex < 0 ? first : second };
  }
  return null;
}

function expectAbsent(text, rel, needle, reason) {
  if (text.includes(needle)) {
    return { rel, line: 1, reason, snippet: needle };
  }
  return null;
}

function selfTest() {
  const bad = collectFindings({
    scan: "proofCaptureRepository.capture(caption = row.vaccineLabel)",
    sheds: 'private fun VaccinationExecutionRowDto.hasSubmittedRecord(): Boolean =\n    sopStatus.isSubmissionTerminalStatus() ||\n        proofStatus.equals("uploaded", ignoreCase = true)',
    weighing: "caption = \"Weighing\"",
    seed: "INSERT INTO qa_sheds VALUES (1, 'x', 'p', 'Godel 1', '', 20",
  });
  const good = collectFindings({
    overlayContext: "fun proofOverlayContextLine(",
    scan: 'title = vaccinationProofTitle(row, state.value.cohortLabel, taskDetail.value)\ncaption = vaccinationProofCaption(row, state.value.cohortLabel, taskDetail.value)\nrfidTag = row.primaryTag.takeIf { it.isNotBlank() }\nfeature = "Vaccination"\nvaccinationParkLabel(detail)\nvaccinationLocationLabel(screenTitle, detail)\n.replace(" + ", VACCINE_LABEL_SEPARATOR)\n.joinToString(VACCINE_LABEL_SEPARATOR)\nprivate const val VACCINE_LABEL_SEPARATOR = " · "\npendingScanCommit = PendingScanCommit(\nval captured = proofCaptureSource.captureVideo(\npendingScanCommit?.let { commit ->\nmarkRowDone(row, commit.capturedAtMs, commit.obligationIds)',
    sheds: 'internal fun List<VaccinationExecutionRowDto>.opensSubmittedRecordOnly(): Boolean =\n    isNotEmpty() &&\n        none { row -> row.needsRedo() } &&\n        all { row -> row.hasSubmittedRecord() }\n\nprivate fun VaccinationExecutionRowDto.hasSubmittedRecord(): Boolean =\n    sopStatus.isSubmissionTerminalStatus() ||\n        verificationStatus.equals("pending", ignoreCase = true)',
    weighing: 'title = weighingIndividualProofTitle(row)\ntitle = weighingLumpSumProofTitle()\ncaption = weighingIndividualProofCaption(row)\nrfidTag = row.primaryTag.takeIf { it.isNotBlank() }\ncaption = weighingLumpSumProofCaption(slotNumber)\nrfidTag = null\nfeature = "Weighing"\nweighingOverlayParkLabel()',
    feedPacking: 'ProofCaptureContext(\nfeature = "Feed packing"\nparkLabel = parkLabel.ifBlank { parkId }',
    feedTransport: 'ProofCaptureContext(title=feedTransportProofCaption()\nproofOverlayContextLine("Feed transport",parkLabel,shedLabel.ifBlank{shedId})',
    feedComplete: 'ProofCaptureContext(\nfeature = "Feed direction"',
    seed: "'Godel 1',   '',    2\n'Yashoda 1', 'Y1-', 3\nr.sequence = 2 AND d.shed_seq IN (2, 6)\nTHEN ARRAY['91000000-0000-4000-8000-000000000503','91000000-0000-4000-8000-000000000504']::uuid[]",
  });
  const ok = bad.length >= 10 && good.length === 0;
  console.log(ok ? "android-vaccine-weighing-proof-context self-test: ok" : "android-vaccine-weighing-proof-context self-test: FAIL");
  process.exit(ok ? 0 : 1);
}

function collectFindings({ scan, sheds, weighing, feedPacking = "", feedTransport = "", feedComplete = "", overlayContext = "", seed }) {
  return [
    expect(overlayContext, OVERLAY_CONTEXT, "fun proofOverlayContextLine(", "proof captions must use the shared feature/park/location context helper"),
    expect(scan, SCAN_VM, "title = vaccinationProofTitle(row, state.value.cohortLabel, taskDetail.value)", "vaccination camera title must include task context"),
    expect(scan, SCAN_VM, "caption = vaccinationProofCaption(row, state.value.cohortLabel, taskDetail.value)", "vaccination overlay caption must be built from task + vaccine context"),
    expect(scan, SCAN_VM, "rfidTag = row.primaryTag.takeIf { it.isNotBlank() }", "vaccination per-animal proof must burn the scanned RFID"),
    expect(scan, SCAN_VM, 'feature = "Vaccination"', "vaccination overlay title must name the feature"),
    expect(scan, SCAN_VM, "vaccinationParkLabel(detail)", "vaccination overlay must include park when backend provides it"),
    expect(scan, SCAN_VM, "vaccinationLocationLabel(screenTitle, detail)", "vaccination overlay must include shed/partition context"),
    expect(scan, SCAN_VM, 'private const val VACCINE_LABEL_SEPARATOR = " · "', "two-vaccine labels must use the human-readable ET+TT · PPR separator"),
    expect(scan, SCAN_VM, ".joinToString(VACCINE_LABEL_SEPARATOR)", "two-vaccine scan rows must share the same readable separator"),
    expect(scan, SCAN_VM, '.replace(" + ", VACCINE_LABEL_SEPARATOR)', "camera overlay must normalize old two-vaccine labels to the readable separator"),
    expect(scan, SCAN_VM, "pendingScanCommit = PendingScanCommit(", "fresh per-goat scan commit must be deferred until camera returns video"),
    expectOrder(
      scan,
      SCAN_VM,
      "val captured = proofCaptureSource.captureVideo(",
      "pendingScanCommit?.let { commit ->",
      "fresh vaccination scan must not mark the animal scanned before camera returns a real video",
    ),
    expectOrder(
      scan,
      SCAN_VM,
      "pendingScanCommit?.let { commit ->",
      "markRowDone(row, commit.capturedAtMs, commit.obligationIds)",
      "deferred scan commit must add the visible animal row only after captureVideo returns",
    ),
    expect(sheds, SHEDS_VM, "internal fun List<VaccinationExecutionRowDto>.opensSubmittedRecordOnly(): Boolean", "vaccination shed card must keep a single submitted-record gate"),
    expect(sheds, SHEDS_VM, "none { row -> row.needsRedo() }", "record-only gate must be vetoed by explicit verifier redo state"),
    expectAbsent(sheds, SHEDS_VM, "sumOf { row -> row.openCount.coerceAtLeast(0) } == 0", "stale open counts must not reopen already-submitted vaccination sheds"),
    expect(sheds, SHEDS_VM, "private fun VaccinationExecutionRowDto.hasSubmittedRecord(): Boolean", "submitted-record predicate must stay explicit"),
    expectAbsent(sheds, SHEDS_VM, 'proofStatus.equals("uploaded", ignoreCase = true) ||\n        workState.equals("verification_pending", ignoreCase = true)', "uploaded proof alone must not be treated as a submitted record; it must stay openable for finalize/submit"),
    expect(weighing, WEIGHING_VM, "title = weighingIndividualProofTitle(row)", "individual weighing camera title must include task context"),
    expect(weighing, WEIGHING_VM, "title = weighingLumpSumProofTitle()", "lump-sum weighing camera title must include task context"),
    expect(weighing, WEIGHING_VM, "rfidTag = row.primaryTag.takeIf { it.isNotBlank() }", "individual weighing must burn RFID"),
    expect(weighing, WEIGHING_VM, "rfidTag = null", "lump-sum/shed weighing must not burn RFID"),
    expect(weighing, WEIGHING_VM, '"Weighing"', "weighing overlay must name the feature"),
    expect(weighing, WEIGHING_VM, "weighingOverlayParkLabel()", "weighing overlay must include park"),
    expect(feedPacking, FEED_PACKING_VM, "ProofCaptureContext(", "feed packing camera must receive full overlay context"),
    expect(feedPacking, FEED_PACKING_VM, 'feature = "Feed packing"', "feed packing overlay must include feature"),
    expect(feedPacking, FEED_PACKING_VM, "parkLabel = parkLabel.ifBlank { parkId }", "feed packing overlay must include park"),
    expect(feedTransport, FEED_TRANSPORT_VM, "ProofCaptureContext(title=feedTransportProofCaption()", "feed transport camera must receive full overlay context"),
    expect(feedTransport, FEED_TRANSPORT_VM, 'proofOverlayContextLine("Feed transport",parkLabel,shedLabel.ifBlank{shedId})', "feed transport overlay must include feature, park, and shed"),
    expect(feedComplete, FEED_COMPLETE_VM, "ProofCaptureContext(", "legacy feed direction camera must receive full overlay context"),
    expect(feedComplete, FEED_COMPLETE_VM, 'feature = "Feed direction"', "legacy feed direction overlay must include feature"),
    expectAbsent(feedPacking, FEED_PACKING_VM, "captureVideo(ProofCapturePrompt.FEED_PACKING)", "feed packing must not open camera with prompt-only context"),
    expectAbsent(feedTransport, FEED_TRANSPORT_VM, "captureVideo(ProofCapturePrompt.FEED_TRANSPORT)", "feed transport must not open camera with prompt-only context"),
    expectAbsent(feedComplete, FEED_COMPLETE_VM, "captureVideo(ProofCapturePrompt.FEED_DISTRIBUTION)", "feed direction complete must not open camera with prompt-only context"),
    expect(seed, SEED, "'Godel 1',   '',    2", "phone QA seed must include 2-animal sheds"),
    expect(seed, SEED, "'Yashoda 1', 'Y1-', 3", "phone QA seed must include 3-animal sheds"),
    expect(seed, SEED, "r.sequence = 2 AND d.shed_seq IN (2, 6)", "phone QA seed must include selected two-vaccine sheds"),
    expect(seed, SEED, "THEN ARRAY['91000000-0000-4000-8000-000000000503','91000000-0000-4000-8000-000000000504']::uuid[]", "two-vaccine assignment must advertise both vaccine rules"),
  ].filter(Boolean);
}

if (process.argv.includes("--self-test")) selfTest();

const scan = readFileSync(resolve(repo, SCAN_VM), "utf8");
const sheds = readFileSync(resolve(repo, SHEDS_VM), "utf8");
const weighing = readFileSync(resolve(repo, WEIGHING_VM), "utf8");
const feedPacking = readFileSync(resolve(repo, FEED_PACKING_VM), "utf8");
const feedTransport = readFileSync(resolve(repo, FEED_TRANSPORT_VM), "utf8");
const feedComplete = readFileSync(resolve(repo, FEED_COMPLETE_VM), "utf8");
const overlayContext = readFileSync(resolve(repo, OVERLAY_CONTEXT), "utf8");
const seed = readFileSync(resolve(repo, SEED), "utf8");
const findings = collectFindings({ scan, sheds, weighing, feedPacking, feedTransport, feedComplete, overlayContext, seed });

if (findings.length) {
  console.error("android-vaccine-weighing-proof-context guard FAILED:");
  for (const finding of findings) {
    console.error(`  ${finding.rel}:${lineNo(readFileSync(resolve(repo, finding.rel), "utf8"), finding.snippet)} ${finding.reason}`);
  }
  process.exit(1);
}

console.log("android-vaccine-weighing-proof-context: ok");
