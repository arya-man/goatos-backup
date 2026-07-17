#!/usr/bin/env node
// check-android-camera-only-proof-capture.mjs — proof/verification media in the Android app
// must be produced by the in-app CameraX recorder, never by gallery/file picker imports or
// external media capture intents. Diff-scoped by default; --all scans production Android sources.

import { execSync } from "node:child_process";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const ROOT = "apps/goatos-android";
const BASE = process.env.ANDROID_CAMERA_ONLY_BASE || "origin/main";

const BANNED = [
  {
    re: /\bActivityResultContracts\.(GetContent|GetMultipleContents|OpenDocument|OpenMultipleDocuments|PickVisualMedia|PickMultipleVisualMedia|CaptureVideo)\b/,
    reason: "ActivityResultContracts media/file picker or external capture contract",
  },
  {
    re: /\b(Intent\.)?(ACTION_GET_CONTENT|ACTION_OPEN_DOCUMENT|ACTION_OPEN_DOCUMENT_TREE|ACTION_PICK|ACTION_VIDEO_CAPTURE)\b/,
    reason: "Android file picker or external media capture intent",
  },
  {
    re: /\bMediaStore\.(ACTION_PICK_IMAGES|ACTION_VIDEO_CAPTURE)\b/,
    reason: "Android photo picker or external video capture intent",
  },
  {
    re: /\b(PickVisualMedia|PickMultipleVisualMedia|GetContent|GetMultipleContents|OpenDocument|OpenMultipleDocuments)\b/,
    reason: "media/file picker contract type",
  },
];

function isProductionAndroidSource(rel) {
  return rel.startsWith(`${ROOT}/`) && rel.includes("/src/main/") && /\.(kt|java)$/.test(rel);
}

function walk(dir, acc = []) {
  let entries;
  try {
    entries = readdirSync(dir);
  } catch {
    return acc;
  }
  for (const entry of entries) {
    const path = join(dir, entry);
    const stat = statSync(path);
    if (stat.isDirectory()) walk(path, acc);
    else {
      const rel = relative(repo, path);
      if (isProductionAndroidSource(rel)) acc.push(rel);
    }
  }
  return acc;
}

function changedProductionSources() {
  try {
    return execSync(`git -C "${repo}" diff --name-only ${BASE}...HEAD`, { encoding: "utf8" })
      .split("\n")
      .map((line) => line.trim())
      .filter(isProductionAndroidSource);
  } catch {
    return [];
  }
}

function lineIsComment(line, inBlockComment) {
  const trimmed = line.trim();
  return inBlockComment || trimmed.startsWith("//") || trimmed.startsWith("*") || trimmed.startsWith("/*");
}

function scanText(rel, text) {
  const findings = [];
  let inBlockComment = false;
  const lines = text.split("\n");
  lines.forEach((line, index) => {
    const wasInBlockComment = inBlockComment;
    if (line.includes("/*")) inBlockComment = true;
    if (line.includes("*/")) inBlockComment = false;
    if (lineIsComment(line, wasInBlockComment) || line.includes("camera-only:ignore")) return;
    for (const banned of BANNED) {
      if (banned.re.test(line)) {
        findings.push({
          rel,
          line: index + 1,
          reason: banned.reason,
          snippet: line.trim().slice(0, 120),
        });
      }
    }
  });
  return findings;
}

function scanFile(rel) {
  let text;
  try {
    text = readFileSync(resolve(repo, rel), "utf8");
  } catch {
    return [];
  }
  return scanText(rel, text);
}

function selfTest() {
  const badPicker = "val pick = ActivityResultContracts.GetContent()";
  const badIntent = "Intent(Intent.ACTION_OPEN_DOCUMENT)";
  const badExternalCapture = "Intent(MediaStore.ACTION_VIDEO_CAPTURE)";
  const goodPermission = "rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {}";
  const goodCameraX = "val recorder = Recorder.Builder().build()";

  const ok =
    scanText("bad-picker.kt", badPicker).length > 0 &&
    scanText("bad-intent.kt", badIntent).length > 0 &&
    scanText("bad-capture.kt", badExternalCapture).length > 0 &&
    scanText("good-permission.kt", goodPermission).length === 0 &&
    scanText("good-camerax.kt", goodCameraX).length === 0;

  console.log(ok ? "android-camera-only self-test: ok" : "android-camera-only self-test: FAIL");
  process.exit(ok ? 0 : 1);
}

if (process.argv.includes("--self-test")) selfTest();

const targets = process.argv.includes("--all")
  ? walk(resolve(repo, ROOT))
  : changedProductionSources();

if (!targets.length) {
  console.log("android-camera-only: ok (no production Android source files changed)");
  process.exit(0);
}

const findings = targets.flatMap(scanFile);
if (findings.length) {
  console.error("android-camera-only-proof-capture guard FAILED — Android proof/verification capture must use the in-app CameraX recorder only:");
  for (const finding of findings) {
    console.error(`  ${finding.rel}:${finding.line}  ${finding.reason} (${finding.snippet})`);
  }
  process.exit(1);
}

console.log(`android-camera-only: ok (${targets.length} production Android source file(s) scanned)`);
