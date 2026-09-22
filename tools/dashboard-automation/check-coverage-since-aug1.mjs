#!/usr/bin/env node
// Proves the OCI live smoke covers every admin-web feature and repeat-bug pattern shipped since
// 2026-08-01 (tools/dashboard-automation/coverage-since-aug1.json), on laptop AND mobile.
//
// Every reference in the manifest must resolve to something the smoke actually runs:
//   route-param / routes -> a route in smoke-visual-live.mjs buildRoutes AND in a module journey
//   overlay              -> a step id in overlay-journeys.mjs overlayJourneys (on one of the entry's routes)
//   safeClick            -> a module-journeys.json safeClick key (in a journey that runs one of the entry's routes)
//   check                -> a pattern id exported by regression-checks.mjs REGRESSION_PATTERNS
//
//   node tools/dashboard-automation/check-coverage-since-aug1.mjs [--self-test] [--commits <dir>]
//
// --commits <dir>: every *.jsonl line with userVisible=true and kind feature|bugfix|ui-polish must
// be referenced by some entry's `commits` or listed in the manifest's `uncoverable` with a reason.
import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { discoverSmokeRoutes } from "./discover-admin-routes.mjs";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
export const manifestPath = join(repoRoot, "tools/dashboard-automation/coverage-since-aug1.json");
const journeysPath = join(repoRoot, "tools/dashboard-automation/module-journeys.json");
const COMMIT_KINDS = new Set(["feature", "bugfix", "ui-polish"]);
const VIEWPORTS = ["laptop", "mobile"];
const INTERACTION_TYPES = new Set(["route-param", "overlay", "safeClick", "check"]);

export function safeClickKey(click) {
  if (click.testId) return `testId:${click.testId}`;
  if (click.css) return `css:${click.css}`;
  if (click.role && click.name) return `role:${click.role}:${click.name}`;
  if (click.text) return `text:${click.text}`;
  return JSON.stringify(click);
}

export async function loadContext() {
  const libDir = join(repoRoot, "apps/admin-web/scripts/lib");
  const { overlayJourneys } = await import(pathToFileURL(join(libDir, "overlay-journeys.mjs")).href);
  const { REGRESSION_PATTERNS } = await import(pathToFileURL(join(libDir, "regression-checks.mjs")).href);
  return {
    smokeRoutes: new Set(discoverSmokeRoutes().map((route) => route.name)),
    journeys: JSON.parse(readFileSync(journeysPath, "utf8")).journeys ?? [],
    overlayJourneys,
    patternIds: new Set(Object.keys(REGRESSION_PATTERNS)),
  };
}

export function checkCoverage(manifest, { smokeRoutes, journeys, overlayJourneys, patternIds }) {
  const findings = [];
  const journeyRoutes = new Set(journeys.flatMap((journey) => journey.routes ?? []));
  const clickRoutes = new Map(); // safeClick key -> routes of every journey that carries it
  for (const journey of journeys) {
    for (const click of journey.safeClicks ?? []) {
      const key = safeClickKey(click);
      clickRoutes.set(key, [...(clickRoutes.get(key) ?? []), ...(journey.routes ?? [])]);
    }
  }
  const overlayRoutes = new Map(); // step id -> route names that run it
  for (const [route, steps] of Object.entries(overlayJourneys)) {
    for (const step of steps) overlayRoutes.set(step.id, [...(overlayRoutes.get(step.id) ?? []), route]);
  }
  const entries = Array.isArray(manifest.entries) ? manifest.entries : [];
  if (entries.length === 0) findings.push("manifest has no entries");
  const ids = new Set();
  for (const entry of entries) {
    const tag = entry.id ?? "<missing id>";
    if (!entry.id) findings.push(`entry without id: ${JSON.stringify(entry).slice(0, 80)}`);
    if (ids.has(entry.id)) findings.push(`${tag}: duplicate id`);
    ids.add(entry.id);
    if (entry.kind !== "feature" && entry.kind !== "pattern") findings.push(`${tag}: kind must be feature|pattern`);
    if (!entry.title) findings.push(`${tag}: missing title`);
    if (!entry.shippedIn) findings.push(`${tag}: missing shippedIn`);
    if (!Array.isArray(entry.commits)) findings.push(`${tag}: commits must be an array (may be empty)`);
    for (const viewport of VIEWPORTS) {
      if (!entry.viewports?.includes(viewport)) findings.push(`${tag}: viewports must include ${viewport}`);
    }
    if (entry.status !== "covered" && entry.status !== "gap") findings.push(`${tag}: status must be covered|gap`);
    if (entry.status === "gap" && !String(entry.gapReason ?? "").trim()) findings.push(`${tag}: status gap without gapReason`);
    const routes = entry.routes ?? [];
    const interactions = entry.interactions ?? [];
    if (entry.status === "covered" && routes.length === 0) findings.push(`${tag}: covered but lists no routes`);
    if (entry.status === "covered" && interactions.length === 0) findings.push(`${tag}: covered but lists no interactions`);
    const routeRefs = [...routes, ...interactions.filter((i) => i.type === "route-param").map((i) => i.ref)];
    for (const route of new Set(routeRefs)) {
      if (!smokeRoutes.has(route)) findings.push(`${tag}: route ${route} is not in smoke-visual-live.mjs buildRoutes`);
      else if (!journeyRoutes.has(route)) findings.push(`${tag}: route ${route} is in no module journey, so it never runs on OCI`);
    }
    for (const interaction of interactions) {
      const where = `${tag}: ${interaction.type} ${JSON.stringify(interaction.ref)}`;
      if (!INTERACTION_TYPES.has(interaction.type)) findings.push(`${where}: unknown interaction type`);
      else if (interaction.type === "overlay") {
        const runsOn = overlayRoutes.get(interaction.ref);
        if (!runsOn) findings.push(`${where}: not a step id in overlay-journeys.mjs overlayJourneys`);
        else if (!runsOn.some((route) => routes.includes(route))) findings.push(`${where}: runs on ${runsOn.join(", ")}, none of which is in this entry's routes`);
      } else if (interaction.type === "safeClick") {
        const runsOn = clickRoutes.get(interaction.ref);
        if (!runsOn) findings.push(`${where}: not a safeClick in module-journeys.json (key form testId:|css:|role:<role>:<name>|text:)`);
        else if (!runsOn.some((route) => routes.includes(route))) findings.push(`${where}: its module journeys run none of this entry's routes`);
      } else if (interaction.type === "check" && !patternIds.has(interaction.ref)) {
        findings.push(`${where}: not a pattern id exported by regression-checks.mjs REGRESSION_PATTERNS`);
      }
    }
  }
  for (const item of manifest.uncoverable ?? []) {
    if (!item.sha || !String(item.reason ?? "").trim()) findings.push(`uncoverable entry needs sha and reason: ${JSON.stringify(item)}`);
  }
  return findings;
}

export function readCommitJsonl(dir) {
  const commits = [];
  for (const file of readdirSync(dir).filter((name) => name.endsWith(".jsonl")).sort()) {
    for (const [index, line] of readFileSync(join(dir, file), "utf8").split("\n").entries()) {
      if (!line.trim()) continue;
      try {
        commits.push(JSON.parse(line));
      } catch (error) {
        throw new Error(`${file}:${index + 1}: invalid JSON (${error.message})`);
      }
    }
  }
  return commits;
}

export function checkCommits(manifest, commits) {
  const referenced = new Set();
  for (const entry of manifest.entries ?? []) for (const sha of entry.commits ?? []) referenced.add(String(sha));
  for (const item of manifest.uncoverable ?? []) if (item.sha && item.reason) referenced.add(String(item.sha));
  const matches = (sha) => [...referenced].some((ref) => ref.length >= 7 && (sha.startsWith(ref) || ref.startsWith(sha)));
  const findings = [];
  let required = 0;
  for (const commit of commits) {
    if (commit.userVisible !== true || !COMMIT_KINDS.has(commit.kind)) continue;
    required += 1;
    const sha = String(commit.sha ?? "");
    if (!sha || !matches(sha)) findings.push(`commit ${sha || "<no sha>"} (${commit.kind}) ${String(commit.subject ?? commit.title ?? "").slice(0, 80)} is not referenced by any entry or listed as uncoverable`);
  }
  return { findings, required };
}

export function summarize(manifest) {
  const entries = manifest.entries ?? [];
  const count = (kind, status) => entries.filter((e) => e.kind === kind && (!status || e.status === status)).length;
  return `coverage-since-aug1: ${count("feature")} features (${count("feature", "covered")} covered, ${count("feature", "gap")} gap), `
    + `${count("pattern")} patterns (${count("pattern", "covered")} covered, ${count("pattern", "gap")} gap)`;
}

function selfTest() {
  const ctx = {
    smokeRoutes: new Set(["feed-analytics", "orphan-route"]),
    journeys: [{ id: "feed", routes: ["feed-analytics"], safeClicks: [{ role: "button", name: "Filters" }] }],
    overlayJourneys: { "feed-analytics": [{ id: "drawer" }] },
    patternIds: new Set(["J-raw-text"]),
  };
  const good = {
    entries: [{
      id: "f", kind: "feature", title: "t", shippedIn: "x", commits: ["abcdef1"], routes: ["feed-analytics"], viewports: ["laptop", "mobile"], status: "covered",
      interactions: [{ type: "route-param", ref: "feed-analytics" }, { type: "overlay", ref: "drawer" }, { type: "safeClick", ref: "role:button:Filters" }, { type: "check", ref: "J-raw-text" }],
    }],
  };
  assert.deepEqual(checkCoverage(good, ctx), []);
  const mutate = (fn) => { const copy = structuredClone(good); fn(copy.entries[0], copy); return checkCoverage(copy, ctx); };
  assert.ok(mutate((e) => { e.routes = ["nope"]; }).some((f) => /not in smoke-visual-live/.test(f)));
  assert.ok(mutate((e) => { e.routes.push("orphan-route"); }).some((f) => /no module journey/.test(f)));
  assert.ok(mutate((e) => { e.interactions[1].ref = "ghost"; }).some((f) => /overlayJourneys/.test(f)));
  assert.ok(mutate((e) => { e.interactions[2].ref = "role:button:Save"; }).some((f) => /module-journeys/.test(f)));
  assert.ok(mutate((e) => { e.interactions[3].ref = "Z-made-up"; }).some((f) => /REGRESSION_PATTERNS/.test(f)));
  assert.ok(mutate((e) => { e.status = "gap"; }).some((f) => /without gapReason/.test(f)));
  assert.ok(mutate((e) => { e.viewports = ["laptop"]; }).some((f) => /include mobile/.test(f)));
  const commits = [
    { sha: "abcdef1234", kind: "feature", userVisible: true },
    { sha: "1111111aaa", kind: "bugfix", userVisible: true },
    { sha: "2222222bbb", kind: "ui-polish", userVisible: false },
    { sha: "3333333ccc", kind: "chore", userVisible: true },
  ];
  const result = checkCommits(good, commits);
  assert.equal(result.required, 2);
  assert.equal(result.findings.length, 1);
  assert.match(result.findings[0], /1111111aaa/);
  assert.deepEqual(checkCommits({ ...good, uncoverable: [{ sha: "1111111", reason: "backend only" }] }, commits).findings, []);
  console.log("coverage-since-aug1 guard self-test: PASS");
}

export async function runCoverageGuard({ commitsDir } = {}) {
  if (!existsSync(manifestPath)) return { findings: [`missing ${manifestPath}`], summary: "" };
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
  const findings = checkCoverage(manifest, await loadContext());
  let summary = summarize(manifest);
  if (commitsDir) {
    const { findings: commitFindings, required } = checkCommits(manifest, readCommitJsonl(commitsDir));
    findings.push(...commitFindings);
    summary += `; commits: ${required - commitFindings.length}/${required} user-visible feature|bugfix|ui-polish commits referenced`;
  }
  return { findings, summary };
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  if (process.argv.includes("--self-test")) {
    selfTest();
    process.exit(0);
  }
  const commitsIndex = process.argv.indexOf("--commits");
  const commitsDir = commitsIndex > -1 ? process.argv[commitsIndex + 1] : undefined;
  if (commitsIndex > -1 && !commitsDir) {
    console.error("--commits needs a directory of *.jsonl files");
    process.exit(2);
  }
  const { findings, summary } = await runCoverageGuard({ commitsDir });
  console.log(summary);
  if (findings.length) {
    console.error(findings.map((finding) => `FAIL ${finding}`).join("\n"));
    process.exit(1);
  }
  console.log("coverage-since-aug1 guard: PASS");
}
export { selfTest as coverageSelfTest };
