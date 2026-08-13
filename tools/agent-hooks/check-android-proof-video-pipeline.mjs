#!/usr/bin/env node
// Android proof-video pipeline guard.
// Feature code may request capture and render product states, but compression/upload/Firebase
// plumbing must stay behind shared app/core ports.

import { execSync } from "node:child_process";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const BASE = process.env.ANDROID_PROOF_VIDEO_BASE || "origin/main";
const FEATURE_ROOT = "apps/goatos-android/feature";
const APP_VM_ROOT = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel";
const APP_MODULE = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/di/AppModule.kt";
const APP_PROOF_MEDIA_PROCESSOR = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/capture/AppProofMediaProcessor.kt";
const CAPTURE_REPOSITORY = "apps/goatos-android/core/core-data/src/main/kotlin/sg/mesha/goatos/core/data/capture/CaptureRepository.kt";
const CAPTURE_ACCESS_GATE = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/capture/CaptureAccessGate.kt";
const APP_PERMISSION_CATALOG = "apps/goatos-android/core/core-permissions/src/main/kotlin/sg/mesha/goatos/core/permissions/AppPermission.kt";

const banned = [
  { re: /\benqueueProofUpload\s*\(/g, reason: "direct proof upload enqueue; use shared proof capture/orchestration" },
  { re: /\bcom\.google\.firebase\./g, reason: "direct Firebase SDK usage" },
  { re: /\b(FirebaseAnalytics|FirebaseCrashlytics|FirebasePerformance)\b/g, reason: "direct Firebase SDK usage" },
  { re: /\b(MediaCodec|MediaMuxer|MediaExtractor|MediaMetadataRetriever)\b/g, reason: "per-screen media processing" },
  { re: /\bandroidx\.media3\.transformer\./g, reason: "per-screen Media3 transformer use" },
  { re: /\b(Transformer|EditedMediaItem|Effects)\b/g, reason: "per-screen media processing" },
  { re: /\b(WorkManager|CoroutineWorker|ListenableWorker)\b/g, reason: "per-screen worker orchestration" },
  { re: /\b(registerProof|uploadProofBlob|GCS)\b/gi, reason: "direct upload implementation" },
  { re: /\bfun\s+\w*(compressVideo|muxVideo|burnOverlay|drawProofOverlay|transcode)\w*\s*\(/gi, reason: "per-screen compression/overlay implementation" },
];

function isScannedSource(rel) {
  return (
    (rel.startsWith(`${FEATURE_ROOT}/`) || rel.startsWith(`${APP_VM_ROOT}/`)) &&
    rel.includes("/src/main/") &&
    /\.(kt|java)$/.test(rel)
  );
}

function walk(dir, acc = []) {
  let entries = [];
  try { entries = readdirSync(dir); } catch { return acc; }
  for (const entry of entries) {
    const full = join(dir, entry);
    const stat = statSync(full);
    if (stat.isDirectory()) walk(full, acc);
    else {
      const rel = relative(repo, full);
      if (isScannedSource(rel)) acc.push(rel);
    }
  }
  return acc;
}

function changedSources() {
  try {
    return execSync(`git -C "${repo}" diff --name-only ${BASE}...HEAD`, { encoding: "utf8" })
      .split("\n")
      .map((line) => line.trim())
      .filter(isScannedSource);
  } catch {
    return [];
  }
}

function lineNo(text, index) {
  return text.slice(0, index).split("\n").length;
}

function scanText(rel, text) {
  const findings = [];
  for (const rule of banned) {
    for (const match of text.matchAll(rule.re)) {
      const line = text.split("\n")[lineNo(text, match.index ?? 0) - 1] ?? "";
      const trimmed = line.trim();
      if (trimmed.startsWith("//") || trimmed.startsWith("*") || trimmed.includes("proof-video-guard:ignore")) continue;
      findings.push({ rel, line: lineNo(text, match.index ?? 0), reason: rule.reason, snippet: trimmed.slice(0, 120) });
    }
  }
  return findings;
}

function scanFile(rel) {
  return scanText(rel, readFileSync(resolve(repo, rel), "utf8"));
}

function selfTest() {
  const badFirebase = scanText("apps/goatos-android/feature/x/src/main/Foo.kt", "FirebaseCrashlytics.getInstance()").length === 1;
  const badMedia = scanText("apps/goatos-android/feature/x/src/main/Foo.kt", "val muxer = MediaMuxer(path, 0)").length === 1;
  const badDirectUpload = scanText("apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/Foo.kt", "syncRepository.enqueueProofUpload(group, key, request, uri, duration)").length === 1;
  const goodPort = scanText("apps/goatos-android/feature/x/src/main/Foo.kt", "analytics.track(\"proof_upload_started\")").length === 0;
  const badNoopBinding = productionProcessorFindings("mediaProcessor = ProofMediaProcessor.Noop").length === 1;
  const goodBinding = productionProcessorFindings("mediaProcessor = AppProofMediaProcessor(context)").length === 0;
  const badPassThrough = appProcessorFindings("return ProofMediaProcessingResult(outputUri = request.originalUri, processedBytes = bytes)").length === 4;
  const goodRealProcessor = appProcessorFindings("Transformer.Builder(context).build()\nCanvas.drawText(\"proof\", 0f, 0f, paint)\nreturn ProofMediaProcessingResult(outputUri = processedUri, processedBytes = processedBytes)").length === 0;
  const badRepositoryOriginalByDefault = captureRepositoryFindings("galleryProofSaver.saveProofCopy(entity.originalUri ?: entity.localUri, request, entity.idempotencyKey)\nsyncRepository.enqueueProofUpload(localFilePath = entity.originalUri ?: entity.localUri)").length === 2;
  const goodRepositoryFinalArtifact = captureRepositoryFindings("val uploadEntity = prepareFinalArtifact(entity)\nsaveFinalArtifactToGallery(uploadEntity, request)\nsyncRepository.enqueueProofUpload(localFilePath = uploadEntity.localUri)").length === 0;
  const badMissingScan = permissionContractFindings("CaptureAccessGate.kt", "add(Manifest.permission.BLUETOOTH_CONNECT)").length === 1;
  const goodScan = permissionContractFindings("CaptureAccessGate.kt", "add(Manifest.permission.BLUETOOTH_CONNECT)\nadd(Manifest.permission.BLUETOOTH_SCAN)").length === 0;
  const ok = badFirebase && badMedia && badDirectUpload && goodPort && badNoopBinding && goodBinding && badPassThrough && goodRealProcessor && badRepositoryOriginalByDefault && goodRepositoryFinalArtifact && badMissingScan && goodScan;
  console.log(ok ? "android-proof-video-pipeline self-test: ok" : "android-proof-video-pipeline self-test: FAIL");
  process.exit(ok ? 0 : 1);
}

if (process.argv.includes("--self-test")) selfTest();

const targets = process.argv.includes("--all")
  ? [...walk(resolve(repo, FEATURE_ROOT)), ...walk(resolve(repo, APP_VM_ROOT))]
  : changedSources();

const findings = targets.flatMap(scanFile);
findings.push(...productionProcessorFindings(readFileSync(resolve(repo, APP_MODULE), "utf8")));
findings.push(...appProcessorFindings(readFileSync(resolve(repo, APP_PROOF_MEDIA_PROCESSOR), "utf8")));
findings.push(...captureRepositoryFindings(readFileSync(resolve(repo, CAPTURE_REPOSITORY), "utf8")));
findings.push(...permissionContractFindings(CAPTURE_ACCESS_GATE, readFileSync(resolve(repo, CAPTURE_ACCESS_GATE), "utf8")));
findings.push(...permissionContractFindings(APP_PERMISSION_CATALOG, readFileSync(resolve(repo, APP_PERMISSION_CATALOG), "utf8")));
if (findings.length) {
  console.error("android-proof-video-pipeline guard FAILED — use the shared proof-video pipeline and telemetry ports:");
  for (const finding of findings) {
    console.error(`  ${finding.rel}:${finding.line} ${finding.reason} (${finding.snippet})`);
  }
  process.exit(1);
}

if (!targets.length) {
  console.log("android-proof-video-pipeline: ok (no scanned Android feature/viewmodel files changed)");
  process.exit(0);
}

console.log(`android-proof-video-pipeline: ok (${targets.length} file(s) scanned)`);

function productionProcessorFindings(text) {
  const findings = [];
  for (const match of text.matchAll(/\bProofMediaProcessor\.Noop\b/g)) {
    findings.push({
      rel: APP_MODULE,
      line: lineNo(text, match.index ?? 0),
      reason: "production proof media processor is Noop; bind an app-layer processor",
      snippet: "ProofMediaProcessor.Noop",
    });
  }
  return findings;
}

function appProcessorFindings(text) {
  const findings = [];
  const passThroughPatterns = [
    {
      re: /outputUri\s*=\s*request\.originalUri/g,
      reason: "production proof processor passes through the original URI; successful processing must create a new compressed/overlaid artifact",
      snippet: "outputUri = request.originalUri",
    },
    {
      re: /processedBytes\s*=\s*bytes\b/g,
      reason: "production proof processor reports processed bytes equal to original bytes; successful processing must produce a processed artifact",
      snippet: "processedBytes = bytes",
    },
  ];
  for (const rule of passThroughPatterns) {
    for (const match of text.matchAll(rule.re)) {
      findings.push({
        rel: APP_PROOF_MEDIA_PROCESSOR,
        line: lineNo(text, match.index ?? 0),
        reason: rule.reason,
        snippet: rule.snippet,
      });
    }
  }
  if (!/\b(Transformer|MediaCodec|MediaMuxer)\b/.test(text)) {
    findings.push({
      rel: APP_PROOF_MEDIA_PROCESSOR,
      line: 1,
      reason: "production proof processor must perform real video compression/transcode, not a metadata-only pass-through",
      snippet: "missing Transformer/MediaCodec/MediaMuxer",
    });
  }
  if (!/\b(Canvas|Bitmap|Overlay|GlEffect|TextureOverlay)\b/.test(text)) {
    findings.push({
      rel: APP_PROOF_MEDIA_PROCESSOR,
      line: 1,
      reason: "production proof processor must burn audit overlay into video pixels and photo pixels",
      snippet: "missing overlay renderer",
    });
  }
  return findings;
}

function captureRepositoryFindings(text) {
  const findings = [];
  for (const match of text.matchAll(/saveProofCopy\s*\(\s*entity\.originalUri\b|saveProofCopy\s*\([^,\n]*originalUri[^,\n]*,/g)) {
    findings.push({
      rel: CAPTURE_REPOSITORY,
      line: lineNo(text, match.index ?? 0),
      reason: "Gallery save must use the final selected artifact after processing/fallback, not the original by default",
      snippet: text.slice(match.index ?? 0, (match.index ?? 0) + 100).replace(/\s+/g, " "),
    });
  }
  for (const match of text.matchAll(/localFilePath\s*=\s*entity\.originalUri\b|localFilePath\s*=\s*[^,\n]*originalUri[^,\n]*/g)) {
    findings.push({
      rel: CAPTURE_REPOSITORY,
      line: lineNo(text, match.index ?? 0),
      reason: "proof upload must use the final selected artifact after processing/fallback, not the original by default",
      snippet: text.slice(match.index ?? 0, (match.index ?? 0) + 100).replace(/\s+/g, " "),
    });
  }
  return findings;
}

function permissionContractFindings(rel, text) {
  const findings = [];
  if (rel.endsWith("CaptureAccessGate.kt")) {
    if (!/add\s*\(\s*Manifest\.permission\.BLUETOOTH_CONNECT\s*\)[\s\S]{0,160}add\s*\(\s*Manifest\.permission\.BLUETOOTH_SCAN\s*\)/.test(text)) {
      findings.push({
        rel,
        line: 1,
        reason: "Android 12+ operator capture gate must require both BLUETOOTH_CONNECT and BLUETOOTH_SCAN",
        snippet: "mandatoryCapturePermissionsForSdk",
      });
    }
  }
  if (rel.endsWith("AppPermission.kt")) {
    if (!/\bBLUETOOTH_SCAN\s*\([\s\S]{0,180}Manifest\.permission\.BLUETOOTH_SCAN/.test(text)) {
      findings.push({
        rel,
        line: 1,
        reason: "login-time permission catalog must include Android 12+ BLUETOOTH_SCAN",
        snippet: "AppPermission.BLUETOOTH_SCAN",
      });
    }
  }
  return findings;
}
