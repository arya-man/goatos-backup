#!/usr/bin/env node
// Android proof capture must not let feature policy collapse into the shared default.

import { execSync } from "node:child_process";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const ROOT = "apps/goatos-android";
const BASE = process.env.ANDROID_PROOF_POLICY_BASE || "origin/main";

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

function scanText(rel, text) {
  const findings = [];
  const lines = text.split("\n");
  lines.forEach((line, index) => {
    if (/proofPolicy\s*:\s*ProofPolicy\s*=\s*ProofPolicy\.Default/.test(line) && !line.includes("proof-policy-default:ignore")) {
      findings.push({ rel, line: index + 1, reason: "ProofCaptureRepository API must not default to shared ProofPolicy.Default", snippet: line.trim().slice(0, 140) });
    }
    if (/proofPolicy\s*=\s*ProofPolicy\.Default(?!\s*\.copy)/.test(line) && !line.includes("proof-policy-default:ignore")) {
      findings.push({ rel, line: index + 1, reason: "raw ProofPolicy.Default passed as feature policy", snippet: line.trim().slice(0, 140) });
    }
    if (/\b(?:proofPolicy\s*=|val\s+proofPolicy\s*=).*?\?:\s*ProofPolicy\.Default\b/.test(line) && !line.includes("proof-policy-default:ignore")) {
      findings.push({ rel, line: index + 1, reason: "missing backend policy must not silently fall back to ProofPolicy.Default", snippet: line.trim().slice(0, 140) });
    }
    if (/\bMAX_PROOFS_PER_GOAT\b/.test(line) && !line.includes("proof-policy-default:ignore")) {
      findings.push({ rel, line: index + 1, reason: "generic 5-proof goat cap name reintroduced", snippet: line.trim().slice(0, 140) });
    }
  });
  const callPattern = /\b(?:proofCaptureRepository|captureRepository|proofs|repo)\.capture(?:ReplacingLatest)?\s*\(/g;
  let match;
  while ((match = callPattern.exec(text)) !== null) {
    const start = match.index;
    const open = text.indexOf("(", start);
    let depth = 0;
    let end = -1;
    for (let i = open; i < text.length; i += 1) {
      if (text[i] === "(") depth += 1;
      else if (text[i] === ")") {
        depth -= 1;
        if (depth === 0) {
          end = i;
          break;
        }
      }
    }
    if (end < 0) continue;
    const call = text.slice(start, end + 1);
    const startLine = text.slice(0, start).split("\n").length;
    if (!/proofPolicy\s*=/.test(call) && !call.includes("proof-policy-default:ignore")) {
      findings.push({
        rel,
        line: startLine,
        reason: "production proof capture call omits feature-owned proofPolicy",
        snippet: call.split("\n")[0].trim().slice(0, 140),
      });
    }
    callPattern.lastIndex = end + 1;
  }
  return findings;
}

function scanFile(rel) {
  try {
    return scanText(rel, readFileSync(resolve(repo, rel), "utf8"));
  } catch {
    return [];
  }
}

function selfTest() {
  const bad = "capture(proofPolicy = ProofPolicy.Default)";
  const badDefaultParam = "proofPolicy: ProofPolicy = ProofPolicy.Default,";
  const badElvisFallback = "proofPolicy = sopVersion?.toProofPolicy() ?: ProofPolicy.Default";
  const badOmitted = "proofCaptureRepository.capture(taskId = id, fieldKey = key)";
  const badCap = "const val MAX_PROOFS_PER_GOAT = 5";
  const goodCopy = "capture(proofPolicy = ProofPolicy.Default.copy(maximumCountPerField = 1))";
  const goodExplicit = "proofCaptureRepository.capture(\n  taskId = id,\n  proofPolicy = policy,\n)";
  const goodIgnored = "capture(proofPolicy = ProofPolicy.Default) // proof-policy-default:ignore legacy task policy fallback";
  const ok =
    scanText("bad.kt", bad).length === 1 &&
    scanText("bad-default-param.kt", badDefaultParam).length === 1 &&
    scanText("bad-elvis-fallback.kt", badElvisFallback).length === 1 &&
    scanText("bad-omitted.kt", badOmitted).length === 1 &&
    scanText("bad-cap.kt", badCap).length === 1 &&
    scanText("good-copy.kt", goodCopy).length === 0 &&
    scanText("good-explicit.kt", goodExplicit).length === 0 &&
    scanText("good-ignored.kt", goodIgnored).length === 0;
  console.log(ok ? "android-proof-policy-default self-test: ok" : "android-proof-policy-default self-test: FAIL");
  process.exit(ok ? 0 : 1);
}

if (process.argv.includes("--self-test")) selfTest();

const targets = process.argv.includes("--all")
  ? walk(resolve(repo, ROOT))
  : changedProductionSources();

if (!targets.length) {
  console.log("android-proof-policy-default: ok (no production Android app source files changed)");
  process.exit(0);
}

const findings = targets.flatMap(scanFile);
if (findings.length) {
  console.error("android-proof-policy-default guard FAILED — capture calls must pass feature-owned proof policy, not raw ProofPolicy.Default:");
  for (const finding of findings) {
    console.error(`  ${finding.rel}:${finding.line}  ${finding.reason} (${finding.snippet})`);
  }
  process.exit(1);
}

console.log(`android-proof-policy-default: ok (${targets.length} production Android app source file(s) scanned)`);
