#!/usr/bin/env node

// Guards the anti-pattern that caused Weights to regress back into multi-second
// reads: a server-rendered page asks a sectionable aggregate endpoint for the
// whole payload while rendering only a few widgets. Backends keep omitted
// `sections` for legacy/mobile compatibility; admin-web rendered routes must
// request only the sections they actually show.

import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");

const SECTIONABLE_CALLS = [
  { fn: "getWeighingGrowth", reason: "GET /weighing/leadership/growth fans out into ADG/Growth aggregate sections" },
  { fn: "getWeightDemographics", reason: "GET /weighing/weight-demographics fans out into demographics aggregate sections" },
  { fn: "getGrowthDirector", reason: "GET /growth-director/weights fans out into Growth Director aggregate sections" },
  { fn: "getFeedAnalyticsExecution", reason: "GET /feed-analytics/execution supports sectioned execution reads" },
  { fn: "getFeedAnalyticsStock", reason: "GET /feed-analytics/stock supports sectioned stock reads" },
];

const FILE_RE = /^apps\/admin-web\/features\/.*\.(?:tsx|ts|jsx|js)$/;
const TEST_RE = /(?:^|\/)(?:[^/]+\.test\.[cm]?[jt]s|[^/]+\.spec\.[cm]?[jt]s)$/;

function git(args, allowFailure = false) {
  try {
    return execFileSync("git", args, { cwd: repo, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
  } catch (error) {
    if (allowFailure) return "";
    throw error;
  }
}

function zlist(out) {
  return out.split("\0").filter(Boolean);
}

function changedFiles() {
  const base = process.env.GOATOS_CI_BASE || "origin/main";
  return new Set([
    ...zlist(git(["diff", "--name-only", "-z", `${base}...HEAD`], true)),
    ...zlist(git(["diff", "--name-only", "-z"], true)),
    ...zlist(git(["diff", "--cached", "--name-only", "-z"], true)),
    ...zlist(git(["ls-files", "--others", "--exclude-standard", "-z"], true)),
  ]);
}

function indexMatchingParen(source, openParen) {
  let depth = 0;
  let quote = "";
  let escaped = false;
  for (let i = openParen; i < source.length; i += 1) {
    const ch = source[i];
    if (quote) {
      if (escaped) escaped = false;
      else if (ch === "\\") escaped = true;
      else if (ch === quote) quote = "";
      continue;
    }
    if (ch === '"' || ch === "'" || ch === "`") {
      quote = ch;
      continue;
    }
    if (ch === "(") depth += 1;
    if (ch === ")") {
      depth -= 1;
      if (depth === 0) return i;
    }
  }
  return -1;
}

export function inspectSource(source, file = "<inline>") {
  const findings = [];
  for (const call of SECTIONABLE_CALLS) {
    const pattern = new RegExp(`\\b${call.fn}\\s*\\(`, "g");
    let match;
    while ((match = pattern.exec(source))) {
      const open = source.indexOf("(", match.index);
      const close = indexMatchingParen(source, open);
      if (close < 0) {
        findings.push(`${file}: ${call.fn} call has unbalanced arguments`);
        break;
      }
      const args = source.slice(open + 1, close);
      if (!/\bsections\s*:/.test(args)) {
        const line = source.slice(0, match.index).split("\n").length;
        findings.push(`${file}:${line}: ${call.fn} must pass sections; ${call.reason}`);
      }
      pattern.lastIndex = close + 1;
    }
  }
  return findings;
}

function selfTest() {
  assert.deepEqual(inspectSource("await getWeighingGrowth({ ...scope, ...window, sections: \"rejected\" })"), []);
  assert.deepEqual(inspectSource("await getWeightDemographics({ ...scope, ...window, sections: \"dimensions\" })"), []);
  assert.deepEqual(inspectSource("await getGrowthDirector({ ...scope, sections: directorSections })"), []);
  assert.match(inspectSource("await getWeighingGrowth({ ...scope, ...window })")[0], /getWeighingGrowth must pass sections/);
  assert.match(inspectSource("await getWeightDemographics({ ...scope, ...window })")[0], /getWeightDemographics must pass sections/);
  assert.match(inspectSource("// sectioned-aggregate-reads:allow reason=full-analytics-route\nawait getWeighingGrowth({ ...scope, ...window })")[0], /getWeighingGrowth must pass sections/);
  assert.match(inspectSource("// sectioned-aggregate-reads:allow\nawait getWeighingGrowth({ ...scope, ...window })")[0], /getWeighingGrowth must pass sections/);
  assert.match(inspectSource("await getGrowthDirector({ park_id: parkId })")[0], /getGrowthDirector must pass sections/);
  assert.match(inspectSource("await getFeedAnalyticsExecution({ ...params })")[0], /getFeedAnalyticsExecution must pass sections/);
  console.log("admin-web sectioned aggregate reads guard self-test: OK");
}

function main() {
  if (process.argv.includes("--self-test")) return selfTest();
  const findings = [];
  for (const file of changedFiles()) {
    if (!FILE_RE.test(file) || TEST_RE.test(file)) continue;
    const abs = resolve(repo, file);
    if (!existsSync(abs)) continue;
    findings.push(...inspectSource(readFileSync(abs, "utf8"), file));
  }
  if (findings.length > 0) {
    console.error("admin-web sectioned aggregate reads guard failed:");
    for (const finding of findings) console.error(`- ${finding}`);
    console.error("\nFix: pass a `sections` list matching the widgets rendered on that route, or split the read.");
    console.error("No inline exception is allowed: split the route or pass explicit sections.");
    process.exit(1);
  }
  console.log("admin-web sectioned aggregate reads guard: OK");
}

main();
