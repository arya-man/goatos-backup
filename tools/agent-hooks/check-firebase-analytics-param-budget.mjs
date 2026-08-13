#!/usr/bin/env node
// Firebase analytics param-budget guard.
// Firebase/GA4 gets a small allowlisted envelope; backend analytics may carry richer proof details.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const rel = "apps/goatos-android/core/core-analytics/src/main/kotlin/sg/mesha/goatos/core/analytics/FirebaseAnalyticsAdapter.kt";

const bannedFirebaseParams = [
  "geocoded_address",
  "latitude",
  "longitude",
  "gps_accuracy_m",
  "input_width",
  "input_height",
  "object_key",
  "upload_url",
  "backend_artifact_url",
  "local_uri",
  "processed_uri",
  "original_uri",
  "error_message",
  "stacktrace",
];

function block(text, name) {
  const match = text.match(new RegExp(`private val ${name} = listOf\\(([\\s\\S]*?)\\)`, "m"));
  return match?.[1] ?? "";
}

function findingsFor(text) {
  const findings = [];
  if (!/internal const val FIREBASE_MAX_EVENT_PARAMS = 25/.test(text)) {
    findings.push("Firebase event param max must stay at GA4-safe 25.");
  }
  if (!/internal const val FIREBASE_MAX_PARAM_VALUE_LENGTH = 100/.test(text)) {
    findings.push("Firebase param values must stay bounded to 100 chars.");
  }
  if (/for\s*\(\s*\([^)]*key[^)]*value[^)]*\)\s+in\s+props\s*\)/.test(text)) {
    findings.push("Firebase params must not backfill arbitrary props; use the explicit allowlist only.");
  }
  if (/putIfAbsent\s*\(\s*key\s*,\s*value\.firebaseParamValue\(\)\s*\)/.test(text)) {
    findings.push("Firebase params must not put arbitrary caller keys into GA4.");
  }
  const allowlist = block(text, "FIREBASE_PARAM_ALLOWLIST");
  if (!allowlist) {
    findings.push("Missing FIREBASE_PARAM_ALLOWLIST.");
  } else {
    const entries = [...allowlist.matchAll(/(?:AnalyticsEvents\.[A-Za-z]+\.|")[A-Za-z0-9_."()]+/g)];
    if (entries.length > 25) {
      findings.push(`Firebase param allowlist has ${entries.length} entries; entries after 25 are silently dropped.`);
    }
  }
  for (const key of bannedFirebaseParams) {
    if (allowlist.includes(`"${key}"`)) {
      findings.push(`Rich/high-cardinality proof param must stay backend-only, not Firebase: ${key}`);
    }
  }
  for (const required of ["proof_id", "task_id", "field_key", "feature_surface", "rfid_tag", "processing_state", "duration_bucket", "processed_size_bucket", "proof_upload_status", "submit_status"]) {
    if (!allowlist.includes(`"${required}"`)) {
      findings.push(`Required compact proof Firebase param missing: ${required}`);
    }
  }
  return findings;
}

function selfTest() {
  const compactRequired = "\"proof_id\", \"task_id\", \"field_key\", \"feature_surface\", \"rfid_tag\", \"processing_state\", \"duration_bucket\", \"processed_size_bucket\", \"proof_upload_status\", \"submit_status\"";
  const badBackfill = findingsFor(`internal const val FIREBASE_MAX_EVENT_PARAMS = 25\ninternal const val FIREBASE_MAX_PARAM_VALUE_LENGTH = 100\nprivate val FIREBASE_PARAM_ALLOWLIST = listOf(${compactRequired})\nfun f(props: Map<String,String>) { for ((key, value) in props) result.putIfAbsent(key, value.firebaseParamValue()) }`).length > 0;
  const badRich = findingsFor(`internal const val FIREBASE_MAX_EVENT_PARAMS = 25\ninternal const val FIREBASE_MAX_PARAM_VALUE_LENGTH = 100\nprivate val FIREBASE_PARAM_ALLOWLIST = listOf(${compactRequired}, \"geocoded_address\", \"latitude\")`).length > 0;
  const badTooMany = findingsFor(`internal const val FIREBASE_MAX_EVENT_PARAMS = 25\ninternal const val FIREBASE_MAX_PARAM_VALUE_LENGTH = 100\nprivate val FIREBASE_PARAM_ALLOWLIST = listOf(${compactRequired}, \"a\", \"b\", \"c\", \"d\", \"e\", \"f\", \"g\", \"h\", \"i\", \"j\", \"k\", \"l\", \"m\", \"n\", \"o\", \"p\")`).length > 0;
  const good = findingsFor(`internal const val FIREBASE_MAX_EVENT_PARAMS = 25\ninternal const val FIREBASE_MAX_PARAM_VALUE_LENGTH = 100\nprivate val FIREBASE_PARAM_ALLOWLIST = listOf(${compactRequired})`).length === 0;
  const ok = badBackfill && badRich && badTooMany && good;
  console.log(ok ? "firebase-analytics-param-budget self-test: ok" : "firebase-analytics-param-budget self-test: FAIL");
  process.exit(ok ? 0 : 1);
}

if (process.argv.includes("--self-test")) selfTest();

const findings = findingsFor(readFileSync(resolve(repo, rel), "utf8"));
if (findings.length) {
  console.error("firebase-analytics-param-budget guard FAILED:");
  for (const finding of findings) console.error(`  ${rel}: ${finding}`);
  process.exit(1);
}

console.log("firebase-analytics-param-budget: ok");
