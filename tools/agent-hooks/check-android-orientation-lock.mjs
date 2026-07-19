#!/usr/bin/env node
// check-android-orientation-lock.mjs — the Goat OS Android app is portrait-only.
//
// Business rule: field/operator and leadership screens must not rotate into landscape.
// A future dedicated live-camera/video-recorder Activity may opt out, because video capture can
// legitimately need sensor/landscape handling. Today the CameraX recorder is an overlay inside
// MainActivity, so MainActivity itself is still locked to portrait.

import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const ROOT = "apps/goatos-android";
const ACTIVITY_RE = /<activity\b([\s\S]*?)(?:\/>|>[\s\S]*?<\/activity>)/g;
const ATTR_RE = /\bandroid:([A-Za-z0-9_]+)\s*=\s*"([^"]*)"/g;
const CAMERA_ACTIVITY_RE = /(?:camera|video|recorder)/i;
const ALLOWED_CAMERA_ORIENTATIONS = new Set([
  "portrait",
  "sensorPortrait",
  "fullSensor",
  "sensor",
  "landscape",
  "sensorLandscape",
  "fullUser",
  "user",
  "unspecified",
]);

function walk(dir, acc = []) {
  let entries;
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch {
    return acc;
  }
  for (const entry of entries) {
    const path = join(dir, entry.name);
    const stat = statSync(path);
    if (stat.isDirectory()) {
      if (entry.name === "build") continue;
      walk(path, acc);
    } else if (entry.name === "AndroidManifest.xml" && path.includes("/src/main/")) {
      acc.push(path);
    }
  }
  return acc;
}

function attrsOf(activityXml) {
  const attrs = new Map();
  for (const match of activityXml.matchAll(ATTR_RE)) {
    attrs.set(match[1], match[2]);
  }
  return attrs;
}

function findingsForManifest(file, text) {
  const findings = [];
  for (const match of text.matchAll(ACTIVITY_RE)) {
    const activityXml = match[0];
    const attrs = attrsOf(activityXml);
    const name = attrs.get("name") ?? "(unnamed activity)";
    const orientation = attrs.get("screenOrientation");
    const isCameraActivity = CAMERA_ACTIVITY_RE.test(name);

    if (!orientation) {
      findings.push({
        file,
        activity: name,
        message: "missing android:screenOrientation=\"portrait\"",
      });
      continue;
    }

    if (orientation !== "portrait") {
      if (isCameraActivity && ALLOWED_CAMERA_ORIENTATIONS.has(orientation)) {
        continue;
      }
      findings.push({
        file,
        activity: name,
        message:
          `screenOrientation="${orientation}" is not allowed; only dedicated Camera/Video/Recorder activities may opt out`,
      });
    }
  }
  return findings;
}

function selfTest() {
  const good = `<activity android:name=".MainActivity" android:screenOrientation="portrait" />`;
  const badMissing = `<activity android:name=".MainActivity" />`;
  const badLandscape = `<activity android:name=".MainActivity" android:screenOrientation="landscape" />`;
  const allowedCamera = `<activity android:name=".VideoRecorderActivity" android:screenOrientation="sensorLandscape" />`;

  const ok =
    findingsForManifest("good.xml", good).length === 0 &&
    findingsForManifest("bad-missing.xml", badMissing).length === 1 &&
    findingsForManifest("bad-landscape.xml", badLandscape).length === 1 &&
    findingsForManifest("camera.xml", allowedCamera).length === 0;

  console.log(ok ? "android-orientation-lock self-test: ok" : "android-orientation-lock self-test: FAIL");
  process.exit(ok ? 0 : 1);
}

if (process.argv.includes("--self-test")) selfTest();

const manifests = walk(resolve(repo, ROOT));
const findings = manifests.flatMap((file) => findingsForManifest(file, readFileSync(file, "utf8")));

if (findings.length) {
  console.error("android-orientation-lock guard FAILED — app screens must be portrait-only:");
  for (const finding of findings) {
    console.error(
      `  ${relative(repo, finding.file)}: activity ${finding.activity}: ${finding.message}`,
    );
  }
  process.exit(1);
}

console.log(`android-orientation-lock: ok (${manifests.length} manifest(s) scanned)`);
