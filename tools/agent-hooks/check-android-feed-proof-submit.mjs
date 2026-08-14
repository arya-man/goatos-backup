#!/usr/bin/env node
// Feed proof submit guard.
// Blocks regressions where uploaded feed proof blobs are disconnected from the submit cycle.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const files = {
  distributionVm: "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedDistributionCompleteViewModel.kt",
  packingVm: "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedPackingCompleteViewModel.kt",
  transportVm: "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FeedTransportViewModel.kt",
  backendHandler: "backend/internal/feeddirection/adapters/http/handler.go",
};

function lineNo(text, index) {
  return text.slice(0, index).split("\n").length;
}

function finding(rel, text, needle, reason) {
  const index = needle ? text.indexOf(needle) : -1;
  return { rel, line: index >= 0 ? lineNo(text, index) : 1, reason, snippet: needle || "missing required pattern" };
}

function distributionFindings(text, rel = files.distributionVm) {
  const findings = [];
  const longLiteralKey = /feed-distribution-complete:\$groupKey:\$feedWeightPhotoItem:\$videoItem:\$waterVideoItem/.test(text);
  // Each slot contributes whichever reference the phone holds: its own outbox id, or -- since the
  // 2026-08-14 split pen-session -- the SERVER proof id of a proof a teammate recorded. Both forms
  // are accepted, but all THREE slots must still appear: a key missing one is not proof-set-specific
  // and would collapse two different proof sets onto one submission.
  const slotRef = String.raw`\w+(?:\s*\?:\s*\w+)?(?:\.orEmpty\(\))?`;
  const shortProofSetKey = new RegExp(
    String.raw`feedDistributionCompleteKey\s*\([^)]*feedWeightPhotoItem[^)]*videoItem[^)]*waterVideoItem[^)]*\)` +
      String.raw`[\s\S]*listOf\s*\(\s*groupKey\s*,\s*feedWeightPhotoItem${slotRef.slice(2)}\s*,` +
      String.raw`\s*videoItem${slotRef.slice(2)}\s*,\s*waterVideoItem${slotRef.slice(2)}\s*\)` +
      String.raw`[\s\S]*(UUID\.nameUUIDFromBytes|MessageDigest)`,
  ).test(text);
  if (!longLiteralKey && !shortProofSetKey) {
    findings.push(finding(rel, text, "feed-distribution-complete", "distribution submit idempotency key must be proof-set-specific and include all three proof item ids before shortening"));
  }
  if (/observeStatus\(\)[\s\S]{0,220}items\.firstOrNull\s*\{\s*it\.id\s*==\s*itemId\s*\}/.test(text)) {
    findings.push(finding(rel, text, "items.firstOrNull { it.id == itemId }", "distribution submit must observe the exact outbox item with observeItem(itemId), not the bounded global status list"));
  }
  if (!/observeItem\s*\(\s*itemId\s*\)/.test(text)) {
    findings.push(finding(rel, text, "observeOutboxItem", "distribution submit observer must call observeItem(itemId)"));
  }
  return findings;
}

function packingFindings(text, rel = files.packingVm) {
  const findings = [];
  if (!/feed-packing-complete:\$groupKey:\$videoItem/.test(text)) {
    findings.push(finding(rel, text, "feed-packing-complete", "packing submit idempotency key must include the selected packing proof item id"));
  }
  if (!/observeSyncStatus\s*\(\s*\)/.test(text) || !/inFlightCount\s*>\s*0/.test(text)) {
    findings.push(finding(rel, text, "observeSyncStatus", "packing refresh state must observe outbox in-flight status"));
  }
  if (!/packingProofReadyForSubmit\s*\([^)]*\)[\s\S]{0,180}videoCaptured\s*&&\s*[^;\n]*videoStatus\.isQueuedForSubmit\s*\(\s*\)/.test(text)) {
    findings.push(finding(rel, text, "packingProofReadyForSubmit", "packing canComplete must require a captured proof whose upload status is queued, uploading, or synced"));
  }
  if (/canComplete\s*=\s*!writeResult\.isCommitted\s*&&\s*it\.videoCaptured/.test(text)) {
    findings.push(finding(rel, text, "canComplete = !writeResult.isCommitted && it.videoCaptured", "packing completion observer must not re-enable submit after a failed proof"));
  }
  if (/canComplete\s*=\s*it\.videoCaptured\s*&&\s*!committed/.test(text)) {
    findings.push(finding(rel, text, "canComplete = it.videoCaptured && !committed", "packing recompute must not treat a failed proof as complete"));
  }
  return findings;
}

function transportFindings(text, rel = files.transportVm) {
  const findings = [];
  if (!/feed-transport-submit:\$taskId:\$proof/.test(text)) {
    findings.push(finding(rel, text, "feed-transport-submit", "transport submit idempotency key must include the selected proof item id"));
  }
  if (/observeStatus\(\)[\s\S]{0,220}items\.firstOrNull\s*\{\s*it\.id\s*==\s*itemId\s*\}/.test(text)) {
    findings.push(finding(rel, text, "items.firstOrNull{it.id==itemId}", "transport submit must observe the exact outbox item with observeItem(itemId), not the bounded global status list"));
  }
  if (!/observeItem\s*\(\s*itemId\s*\)/.test(text)) {
    findings.push(finding(rel, text, "observeOutboxItem", "transport submit observer must call observeItem(itemId)"));
  }
  if (!/observeSyncStatus\s*\(\s*\)/.test(text) || !/inFlightCount\s*>\s*0/.test(text)) {
    findings.push(finding(rel, text, "observeSyncStatus", "transport refresh state must observe outbox in-flight status"));
  }
  return findings;
}

function handlerFindings(text, rel = files.backendHandler) {
  const findings = [];
  if (!/PartitionLabel\s+string\s+`json:"partition_label"`/.test(text)) {
    findings.push(finding(rel, text, "completePackingRequest", "packing complete HTTP request must parse partition_label"));
  }
  if (!/PartitionLabel:\s+strings\.TrimSpace\(body\.PartitionLabel\)/.test(text)) {
    findings.push(finding(rel, text, "CompletePackingInput", "packing complete HTTP handler must pass partition_label to the service"));
  }
  return findings;
}

function selfTest() {
  const badDistribution = distributionFindings("idempotencyKey = \"feed-distribution-complete:$groupKey\"\nsyncRepository.observeStatus().map { status -> status.items.firstOrNull { it.id == itemId } }").length >= 2;
  const goodDistribution = distributionFindings("idempotencyKey = \"feed-distribution-complete:$groupKey:$feedWeightPhotoItem:$videoItem:$waterVideoItem\"\nsyncRepository.observeItem(itemId)").length === 0;
  const goodShortDistribution = distributionFindings("private fun feedDistributionCompleteKey(groupKey: String, feedWeightPhotoItem: String, videoItem: String, waterVideoItem: String): String { val canonical = listOf(groupKey, feedWeightPhotoItem, videoItem, waterVideoItem).joinToString(\"|\"); return \"feed-distribution-complete:\" + UUID.nameUUIDFromBytes(canonical.toByteArray()).toString() }\nsyncRepository.observeItem(itemId)").length === 0;
  const badShortDistribution = distributionFindings("private fun feedDistributionCompleteKey(groupKey: String, feedWeightPhotoItem: String, videoItem: String): String { val canonical = listOf(groupKey, feedWeightPhotoItem, videoItem).joinToString(\"|\"); return \"feed-distribution-complete:\" + UUID.nameUUIDFromBytes(canonical.toByteArray()).toString() }\nsyncRepository.observeItem(itemId)").length >= 1;
  // The split pen-session form (2026-08-14): each slot contributes a LOCAL outbox id or a
  // teammate's SERVER proof id. Accepted, because all three slots are still in the key.
  const goodSplitDistribution = distributionFindings("private fun feedDistributionCompleteKey(groupKey: String, feedWeightPhotoItem: String?, videoItem: String?, waterVideoItem: String?): String { val canonical = listOf(groupKey, feedWeightPhotoItem.orEmpty(), videoItem.orEmpty(), waterVideoItem.orEmpty()).joinToString(\"|\"); return \"feed-distribution-complete:\" + UUID.nameUUIDFromBytes(canonical.toByteArray()).toString() }\nfeedDistributionCompleteKey(groupKey, feedWeightPhotoItem ?: feedWeightRemote, videoItem ?: videoRemote, waterVideoItem ?: waterVideoRemote)\nsyncRepository.observeItem(itemId)").length === 0;
  // ADVERSARIAL: the same split form with ONE slot dropped must still FAIL. Widening the pattern for
  // the elvis/orEmpty shapes must not make a two-slot key acceptable.
  const badSplitDistribution = distributionFindings("private fun feedDistributionCompleteKey(groupKey: String, feedWeightPhotoItem: String?, videoItem: String?): String { val canonical = listOf(groupKey, feedWeightPhotoItem.orEmpty(), videoItem.orEmpty()).joinToString(\"|\"); return \"feed-distribution-complete:\" + UUID.nameUUIDFromBytes(canonical.toByteArray()).toString() }\nsyncRepository.observeItem(itemId)").length >= 1;
  const badPacking = packingFindings("val completeIdempotencyKey = \"feed-packing-complete:$groupKey\"\nfun syncNow() {}\nfun recompute(){ copy(canComplete = it.videoCaptured && !committed) }").length >= 3;
  const goodPacking = packingFindings("val completeIdempotencyKey = \"feed-packing-complete:$groupKey:$videoItem\"\nfun observeSyncStatus(){ syncRepository.observeStatus().map { it.inFlightCount > 0 } }\nprivate fun packingProofReadyForSubmit(state: FeedPackingCompleteUiState): Boolean = state.videoCaptured && state.videoStatus.isQueuedForSubmit()\nfun recompute(){ copy(canComplete = packingProofReadyForSubmit(it) && !committed) }").length === 0;
  const badTransport = transportFindings("val submitIdempotencyKey=\"feed-transport-submit:$taskId\"\nsync.observeStatus().map{status->status.items.firstOrNull{it.id==itemId}}").length >= 3;
  const goodTransport = transportFindings("val submitIdempotencyKey=\"feed-transport-submit:$taskId:$proof\"\nfun observeOutboxItem(itemId:String){sync.observeItem(itemId)}\nfun observeSyncStatus(){sync.observeStatus().map{it.inFlightCount>0}}").length === 0;
  const badHandler = handlerFindings("type completePackingRequest struct { ShedID string `json:\"shed_id\"` }\nCompletePackingInput{ShedID: body.ShedID}").length >= 2;
  const goodHandler = handlerFindings("type completePackingRequest struct { PartitionLabel string `json:\"partition_label\"` }\nCompletePackingInput{PartitionLabel: strings.TrimSpace(body.PartitionLabel)}").length === 0;
  const ok = badDistribution && goodDistribution && goodShortDistribution && badShortDistribution && goodSplitDistribution && badSplitDistribution && badPacking && goodPacking && badTransport && goodTransport && badHandler && goodHandler;
  console.log(ok ? "android-feed-proof-submit self-test: ok" : "android-feed-proof-submit self-test: FAIL");
  process.exit(ok ? 0 : 1);
}

if (process.argv.includes("--self-test")) selfTest();

const findings = [
  ...distributionFindings(readFileSync(resolve(repo, files.distributionVm), "utf8")),
  ...packingFindings(readFileSync(resolve(repo, files.packingVm), "utf8")),
  ...transportFindings(readFileSync(resolve(repo, files.transportVm), "utf8")),
  ...handlerFindings(readFileSync(resolve(repo, files.backendHandler), "utf8")),
];

if (findings.length) {
  console.error("android-feed-proof-submit guard FAILED:");
  for (const item of findings) {
    console.error(`  ${item.rel}:${item.line} ${item.reason} (${item.snippet})`);
  }
  process.exit(1);
}

console.log("android-feed-proof-submit: ok");
