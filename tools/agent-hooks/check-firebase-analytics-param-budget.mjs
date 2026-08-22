#!/usr/bin/env node
// Firebase analytics param-budget guard.
// Firebase/GA4 gets a small allowlisted envelope; backend analytics may carry richer proof details.
//
// GA4 enforces a HARD ceiling of 25 custom params per logged event at ingestion -- params beyond
// that are silently dropped with no client-visible error. This guard therefore FAILS (not warns)
// any allowlist that would let more than 25 entries through, and pins the constant itself at 25.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const rel = "apps/goatos-android/core/core-analytics/src/main/kotlin/sg/mesha/goatos/core/analytics/FirebaseAnalyticsAdapter.kt";

const GA4_HARD_PARAM_CAP = 25;

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

function stripLineComments(text) {
  // Strip `// ...` line comments first so a stray `)` inside a comment (e.g. "(2026-08-15)")
  // cannot truncate the listOf(...) match below.
  return text
    .split("\n")
    .map((line) => line.replace(/\/\/.*$/, ""))
    .join("\n");
}

function block(text, name) {
  const match = stripLineComments(text).match(new RegExp(`private val ${name} = listOf\\(([\\s\\S]*?)\\)`, "m"));
  return match?.[1] ?? "";
}

// Required compact proof/diagnostic params. Each entry matches either a raw quoted string
// ("proof_id") or a symbolic AnalyticsEvents.Params.* / AnalyticsEvents.UserProps.* reference,
// since some required params are only ever referenced via the shared constant, never as a literal.
const requiredAllowlistEntries = [
  { label: "proof_id", pattern: /"proof_id"/ },
  { label: "task_id", pattern: /"task_id"/ },
  { label: "field_key", pattern: /"field_key"/ },
  { label: "feature_surface", pattern: /"feature_surface"/ },
  { label: "rfid_tag", pattern: /"rfid_tag"/ },
  { label: "processing_state", pattern: /"processing_state"/ },
  { label: "duration_bucket", pattern: /"duration_bucket"/ },
  { label: "proof_upload_status", pattern: /"proof_upload_status"/ },
  { label: "submit_status", pattern: /"submit_status"/ },
  // split-operator slot info
  { label: "slot_mask (Params.SLOT_MASK)", pattern: /AnalyticsEvents\.Params\.SLOT_MASK/ },
  { label: "local_slot_state (Params.LOCAL_SLOT_STATE)", pattern: /AnalyticsEvents\.Params\.LOCAL_SLOT_STATE/ },
  // submit source
  { label: "source (Params.SOURCE)", pattern: /AnalyticsEvents\.Params\.SOURCE\b/ },
  // retry / failure reason
  { label: "retry_count (Params.RETRY_COUNT)", pattern: /AnalyticsEvents\.Params\.RETRY_COUNT/ },
  { label: "reason (Params.REASON)", pattern: /AnalyticsEvents\.Params\.REASON\b/ },
  { label: "failure_kind", pattern: /"failure_kind"/ },
  // live-status transition
  { label: "previous (Params.PREVIOUS)", pattern: /AnalyticsEvents\.Params\.PREVIOUS/ },
  { label: "next (Params.NEXT)", pattern: /AnalyticsEvents\.Params\.NEXT/ },
  { label: "status (Params.STATUS)", pattern: /AnalyticsEvents\.Params\.STATUS\b/ },
];

function findingsFor(text) {
  const findings = [];
  if (!new RegExp(`internal const val FIREBASE_MAX_EVENT_PARAMS = ${GA4_HARD_PARAM_CAP}\\b`).test(text)) {
    findings.push(`Firebase event param max must stay at the GA4 hard cap of ${GA4_HARD_PARAM_CAP}.`);
  }
  if (/internal const val FIREBASE_MAX_EVENT_PARAMS = (\d+)/.test(text)) {
    const value = Number(text.match(/internal const val FIREBASE_MAX_EVENT_PARAMS = (\d+)/)[1]);
    if (value > GA4_HARD_PARAM_CAP) {
      findings.push(`FIREBASE_MAX_EVENT_PARAMS=${value} exceeds GA4's real platform cap of ${GA4_HARD_PARAM_CAP}; params beyond ${GA4_HARD_PARAM_CAP} are silently dropped by Firebase itself.`);
    }
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
    if (entries.length > GA4_HARD_PARAM_CAP) {
      findings.push(`Firebase param allowlist has ${entries.length} entries; GA4 hard-drops any event param beyond ${GA4_HARD_PARAM_CAP} at ingestion. This must be a HARD FAIL, not a warning.`);
    }
    for (const { label, pattern } of requiredAllowlistEntries) {
      if (!pattern.test(allowlist)) {
        findings.push(`Required compact Firebase param missing: ${label}`);
      }
    }
  }
  for (const key of bannedFirebaseParams) {
    if (allowlist.includes(`"${key}"`)) {
      findings.push(`Rich/high-cardinality proof param must stay backend-only, not Firebase: ${key}`);
    }
  }
  return findings;
}

function selfTest() {
  const compactRequired = [
    '"proof_id"', '"task_id"', '"field_key"', '"feature_surface"', '"rfid_tag"',
    '"processing_state"', '"duration_bucket"', '"proof_upload_status"', '"submit_status"',
    "AnalyticsEvents.Params.SLOT_MASK", "AnalyticsEvents.Params.LOCAL_SLOT_STATE",
    "AnalyticsEvents.Params.SOURCE", "AnalyticsEvents.Params.RETRY_COUNT",
    "AnalyticsEvents.Params.REASON", '"failure_kind"', "AnalyticsEvents.Params.PREVIOUS",
    "AnalyticsEvents.Params.NEXT", "AnalyticsEvents.Params.STATUS",
  ].join(", ");

  const header = `internal const val FIREBASE_MAX_EVENT_PARAMS = ${GA4_HARD_PARAM_CAP}\ninternal const val FIREBASE_MAX_PARAM_VALUE_LENGTH = 100\n`;

  const badBackfill = findingsFor(`${header}private val FIREBASE_PARAM_ALLOWLIST = listOf(${compactRequired})\nfun f(props: Map<String,String>) { for ((key, value) in props) result.putIfAbsent(key, value.firebaseParamValue()) }`).length > 0;
  const badRich = findingsFor(`${header}private val FIREBASE_PARAM_ALLOWLIST = listOf(${compactRequired}, "geocoded_address", "latitude")`).length > 0;
  const badOverCap37 = findingsFor(`internal const val FIREBASE_MAX_EVENT_PARAMS = 37\ninternal const val FIREBASE_MAX_PARAM_VALUE_LENGTH = 100\nprivate val FIREBASE_PARAM_ALLOWLIST = listOf(${compactRequired})`).length > 0;
  const badTooManyEntries = findingsFor(`${header}private val FIREBASE_PARAM_ALLOWLIST = listOf(${compactRequired}, "a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t")`).length > 0;
  const badMissingRequired = findingsFor(`${header}private val FIREBASE_PARAM_ALLOWLIST = listOf("proof_id", "task_id")`).length > 0;
  const good = findingsFor(`${header}private val FIREBASE_PARAM_ALLOWLIST = listOf(${compactRequired})`).length === 0;

  const ok = badBackfill && badRich && badOverCap37 && badTooManyEntries && badMissingRequired && good;
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
