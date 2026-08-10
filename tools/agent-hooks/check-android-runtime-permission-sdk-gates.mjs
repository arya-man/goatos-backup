#!/usr/bin/env node
// Android runtime permission SDK guard.
//
// Manifest declarations can mention newer permissions, but runtime request/blocking lists must
// only include OS-versioned permissions on OS versions where Android can actually grant them.
// In particular, POST_NOTIFICATIONS is runtime-grantable only on API 33+; adding it to a
// mandatory list on Android 12 makes the all-granted check impossible to satisfy.

import { execSync } from "node:child_process";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const ROOT = "apps/goatos-android";
const BASE = process.env.ANDROID_PERMISSION_SDK_GATES_BASE || "origin/main";

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
    const full = join(dir, entry);
    const stat = statSync(full);
    if (stat.isDirectory()) {
      if (entry !== "build") walk(full, acc);
    } else {
      const rel = relative(repo, full);
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

function notificationLineIsSdkGated(lines, index) {
  const window = lines.slice(Math.max(0, index - 4), index + 1).join("\n");
  return /Build\.VERSION_CODES\.TIRAMISU|VERSION\.SDK_INT\s*>=\s*33|sdkInt\s*>=\s*33|sdkInt\s*>=\s*Build\.VERSION_CODES\.TIRAMISU/.test(window);
}

function scanText(rel, text) {
  const findings = [];
  let inBlockComment = false;
  const lines = text.split("\n");
  lines.forEach((line, index) => {
    const wasInBlockComment = inBlockComment;
    if (line.includes("/*")) inBlockComment = true;
    if (line.includes("*/")) inBlockComment = false;
    if (lineIsComment(line, wasInBlockComment) || line.includes("permission-sdk-gates:ignore")) return;

    const requestsNotification =
      /add\s*\(\s*Manifest\.permission\.POST_NOTIFICATIONS\s*\)/.test(line) ||
      /Manifest\.permission\.POST_NOTIFICATIONS/.test(line) && /RequestMultiplePermissions|mandatory|required|blocking|permissions/i.test(line);

    if (requestsNotification && !notificationLineIsSdkGated(lines, index)) {
      findings.push({
        rel,
        line: index + 1,
        snippet: line.trim().slice(0, 140),
      });
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
  const bad = "val p = buildList { add(Manifest.permission.POST_NOTIFICATIONS) }";
  const goodTiramisu = [
    "if (sdkInt >= Build.VERSION_CODES.TIRAMISU) {",
    "    add(Manifest.permission.POST_NOTIFICATIONS)",
    "}",
  ].join("\n");
  const good33 = [
    "if (Build.VERSION.SDK_INT >= 33) {",
    "    add(Manifest.permission.POST_NOTIFICATIONS)",
    "}",
  ].join("\n");
  const goodManifestDeclaration = "<uses-permission android:name=\"android.permission.POST_NOTIFICATIONS\" />";

  const ok =
    scanText("bad.kt", bad).length === 1 &&
    scanText("good-tiramisu.kt", goodTiramisu).length === 0 &&
    scanText("good-33.kt", good33).length === 0 &&
    scanText("AndroidManifest.xml", goodManifestDeclaration).length === 0;

  console.log(ok ? "android-runtime-permission-sdk-gates self-test: ok" : "android-runtime-permission-sdk-gates self-test: FAIL");
  process.exit(ok ? 0 : 1);
}

if (process.argv.includes("--self-test")) selfTest();

const targets = process.argv.includes("--all")
  ? walk(resolve(repo, ROOT))
  : changedProductionSources();

if (!targets.length) {
  console.log("android-runtime-permission-sdk-gates: ok (no production Android source files changed)");
  process.exit(0);
}

const findings = targets.flatMap(scanFile);
if (findings.length) {
  console.error("android-runtime-permission-sdk-gates guard FAILED — runtime permission lists must not require permissions on OS versions that cannot grant them:");
  for (const finding of findings) {
    console.error(`  ${finding.rel}:${finding.line}  POST_NOTIFICATIONS must be guarded by API 33+ (${finding.snippet})`);
  }
  process.exit(1);
}

console.log(`android-runtime-permission-sdk-gates: ok (${targets.length} production Android source file(s) scanned)`);
