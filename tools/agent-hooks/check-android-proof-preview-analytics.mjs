#!/usr/bin/env node
// Android proof preview cards must report shared preview control actions.

import { execSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const ROOT = "apps/goatos-android";
const BASE = process.env.ANDROID_PROOF_PREVIEW_BASE || "origin/main";

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
  const changed = new Set();
  try {
    for (const cmd of [
      `git -C "${repo}" diff --name-only ${BASE}...HEAD`,
      `git -C "${repo}" diff --name-only --cached`,
      `git -C "${repo}" diff --name-only`,
    ]) {
      execSync(cmd, { encoding: "utf8" })
        .split("\n")
        .map((line) => line.trim())
        .filter(isProductionAndroidSource)
        .forEach((rel) => changed.add(rel));
    }
  } catch {
    return [];
  }
  return [...changed].sort();
}

function lineForOffset(text, offset) {
  return text.slice(0, offset).split("\n").length;
}

function findCallEnd(text, open) {
  let depth = 0;
  let inString = false;
  let escaped = false;
  for (let i = open; i < text.length; i += 1) {
    const ch = text[i];
    if (inString) {
      if (escaped) escaped = false;
      else if (ch === "\\") escaped = true;
      else if (ch === "\"") inString = false;
      continue;
    }
    if (ch === "\"") {
      inString = true;
      continue;
    }
    if (ch === "(") depth += 1;
    if (ch === ")") {
      depth -= 1;
      if (depth === 0) return i + 1;
    }
  }
  return -1;
}

function scanText(rel, text) {
  if (rel.endsWith("/ProofMediaPreview.kt")) return [];
  const findings = [];
  const pattern = /ProofMediaPreview\s*\(/g;
  let match;
  while ((match = pattern.exec(text)) !== null) {
    const open = text.indexOf("(", match.index);
    const end = findCallEnd(text, open);
    const call = text.slice(match.index, end === -1 ? undefined : end);
    const before = text.slice(Math.max(0, match.index - 180), match.index);
    if (before.includes("proof-preview-analytics:ignore")) continue;
    if (!/onPreviewAction\s*=/.test(call)) {
      findings.push({
        rel,
        line: lineForOffset(text, match.index),
        reason: "ProofMediaPreview call must pass onPreviewAction for play/pause/fullscreen/share/failure analytics",
        snippet: call.split("\n").slice(0, 4).join(" ").trim().slice(0, 180),
      });
    } else if (/onPreviewAction\s*=\s*\{\s*(?:_\s*->)?\s*\}/s.test(call)) {
      findings.push({
        rel,
        line: lineForOffset(text, match.index),
        reason: "ProofMediaPreview onPreviewAction must route actions to analytics; empty callbacks hide fullscreen/share/play/retry journeys",
        snippet: call.split("\n").slice(0, 4).join(" ").trim().slice(0, 180),
      });
    }
  }
  return findings;
}

function scan(files) {
  // A DELETED production source is in the changed set but has no text to scan: skipping it is
  // the only honest answer (a retired screen cannot leak a preview action).
  return files
    .filter((rel) => existsSync(join(repo, rel)))
    .flatMap((rel) => scanText(rel, readFileSync(join(repo, rel), "utf8")));
}

function selfTest() {
  const bad = "ProofMediaPreview(path = path, kind = ProofMediaPreviewKind.Video)";
  const good = "ProofMediaPreview(path = path, kind = kind, onPreviewAction = { action -> onEvent(action) })";
  const swallowed = "ProofMediaPreview(path = path, kind = kind, onPreviewAction = { _ -> })";
  const ignored = "// proof-preview-analytics:ignore legacy viewer\nProofMediaPreview(path = path, kind = kind)";
  const badFindings = scanText("apps/goatos-android/feature/x/src/main/Foo.kt", bad);
  const goodFindings = scanText("apps/goatos-android/feature/x/src/main/Foo.kt", good);
  const swallowedFindings = scanText("apps/goatos-android/feature/x/src/main/Foo.kt", swallowed);
  const ignoredFindings = scanText("apps/goatos-android/feature/x/src/main/Foo.kt", ignored);
  if (badFindings.length !== 1 || goodFindings.length !== 0 || swallowedFindings.length !== 1 || ignoredFindings.length !== 0) {
    console.error("android-proof-preview-analytics self-test failed");
    process.exit(1);
  }
  console.log("android-proof-preview-analytics self-test passed");
}

if (process.argv.includes("--self-test")) selfTest();

const files = process.argv.includes("--all")
  ? walk(join(repo, ROOT))
  : changedProductionSources();
const findings = scan(files);
if (findings.length) {
  console.error("android-proof-preview-analytics guard FAILED:");
  for (const finding of findings) {
    console.error(`- ${finding.rel}:${finding.line} ${finding.reason}`);
    console.error(`  ${finding.snippet}`);
  }
  process.exit(1);
}
console.log(`android-proof-preview-analytics guard passed (${files.length} files scanned).`);
