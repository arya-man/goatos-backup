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

const banned = [
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
  const goodPort = scanText("apps/goatos-android/feature/x/src/main/Foo.kt", "analytics.track(\"proof_upload_started\")").length === 0;
  const ok = badFirebase && badMedia && goodPort;
  console.log(ok ? "android-proof-video-pipeline self-test: ok" : "android-proof-video-pipeline self-test: FAIL");
  process.exit(ok ? 0 : 1);
}

if (process.argv.includes("--self-test")) selfTest();

const targets = process.argv.includes("--all")
  ? [...walk(resolve(repo, FEATURE_ROOT)), ...walk(resolve(repo, APP_VM_ROOT))]
  : changedSources();

if (!targets.length) {
  console.log("android-proof-video-pipeline: ok (no scanned Android feature/viewmodel files changed)");
  process.exit(0);
}

const findings = targets.flatMap(scanFile);
if (findings.length) {
  console.error("android-proof-video-pipeline guard FAILED — use the shared proof-video pipeline and telemetry ports:");
  for (const finding of findings) {
    console.error(`  ${finding.rel}:${finding.line} ${finding.reason} (${finding.snippet})`);
  }
  process.exit(1);
}

console.log(`android-proof-video-pipeline: ok (${targets.length} file(s) scanned)`);
