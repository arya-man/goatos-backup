#!/usr/bin/env node
// Proves the OCI live smoke covers every admin-web feature and repeat-bug pattern shipped since
// 2026-08-01 (tools/dashboard-automation/coverage-since-aug1.json), on laptop AND mobile.
//
// WHAT "COVERED" MEANS HERE, AND WHY IT CHANGED
//
// This guard used to accept an entry as `covered` when every reference in it resolved to
// something the sweep runs: a route the smoke visits, a drawer it opens, a control it clicks.
// Nothing in it ever asked whether any assertion could FAIL. So an entry naming a page the
// smoke loads, with one interaction listed, counted as covered - and the ledger read ~100%
// while independent judges measuring "would this catch the bug coming back" got 4%.
//
// Visiting a page is not coverage. It is smoke: it proves the sweep got there.
// An entry may only claim `covered` if it carries at least one DISCRIMINATING reference -
// something that can fail while the page still loads and renders:
//
//   check      (on a `pattern` entry)  the defect detector for the very defect class the entry
//                                      is about; the fix returning makes it fire
//   relation   <endpoint>#<relation>   two figures out of one answer held against each other
//                                      (api-contract-checks.json relations)
//   dataCheck  <name>                  a self-contradiction in the recorded facts, expectRows 0
//                                      (data-sanity-checks.json)
//   assertion  <sha>                   a feature-assertions.json entry that compares a VALUE:
//                                      an exact string, or two figures on the screen
//
// and these are SMOKE, never coverage on their own:
//
//   route-param  the sweep loads the page                  overlay    a drawer opens
//   safeClick    a control is clickable                    check      on a `feature` entry: a
//   assertion whose expectations are only "is it on the    background defect sweep that fires
//   page" / "at least one of" / "must not appear"          on any page with that defect
//
// The strength of a reference is read off the artefact it names, never declared in the
// manifest - otherwise the same illusion just moves one level up.
//
// Statuses: covered | smoke-only | gap.  `smoke-only` is not a failure and not a lie: the sweep
// really does visit it, and nothing there can fail. `gap` means the sweep does not reach it.
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
const apiContractsPath = join(repoRoot, "tools/dashboard-automation/api-contract-checks.json");
const dataSanityPath = join(repoRoot, "tools/dashboard-automation/data-sanity-checks.json");
const featureAssertionsPath = join(repoRoot, "tools/dashboard-automation/feature-assertions.json");
const COMMIT_KINDS = new Set(["feature", "bugfix", "ui-polish"]);
const VIEWPORTS = ["laptop", "mobile"];
const INTERACTION_TYPES = new Set(["route-param", "overlay", "safeClick", "check", "relation", "dataCheck", "assertion"]);
export const STATUSES = new Set(["covered", "smoke-only", "gap"]);
// Statuses that mean "the sweep reaches this": both must name routes and interactions.
const REACHED = new Set(["covered", "smoke-only"]);

export function safeClickKey(click) {
  if (click.testId) return `testId:${click.testId}`;
  if (click.css) return `css:${click.css}`;
  if (click.role && click.name) return `role:${click.role}:${click.name}`;
  if (click.text) return `text:${click.text}`;
  return JSON.stringify(click);
}

const readJson = (path, fallback) => (existsSync(path) ? JSON.parse(readFileSync(path, "utf8")) : fallback);

/**
 * The references that can fail, read off the artefacts that define them.
 *
 * relations   a relation compares two figures in one answer, so a wrong number fires it
 * dataChecks  `expectRows: 0` means the SQL returns rows only when the books contradict
 *             themselves; a check that expects rows is a census, not a contradiction
 * valueAssertions  a feature assertion that pins an exact string or holds two figures against
 *                  each other. Presence, "at least one of" and "must not appear" are smoke.
 */
export function failableRefs({ apiContracts, dataSanity, featureAssertions, isValueExpect }) {
  const relations = new Set();
  for (const endpoint of apiContracts.endpoints ?? []) {
    for (const relation of endpoint.relations ?? []) {
      if (relation.name) relations.add(`${endpoint.name}#${relation.name}`);
    }
  }
  const dataChecks = new Set((dataSanity.checks ?? []).filter((c) => c.expectRows === 0 && c.name).map((c) => c.name));
  const assertions = new Set();
  const valueAssertions = new Set();
  for (const entry of featureAssertions) {
    if (!entry.sha) continue;
    assertions.add(String(entry.sha));
    if ((entry.expect ?? []).some((expect) => isValueExpect(expect))) valueAssertions.add(String(entry.sha));
  }
  return { relations, dataChecks, assertions, valueAssertions };
}

export async function loadContext() {
  const libDir = join(repoRoot, "apps/admin-web/scripts/lib");
  const { overlayJourneys } = await import(pathToFileURL(join(libDir, "overlay-journeys.mjs")).href);
  const { REGRESSION_PATTERNS } = await import(pathToFileURL(join(libDir, "regression-checks.mjs")).href);
  // One definition of "this expectation compares a value", shared with the thing that runs them.
  const { isValueExpect } = await import(pathToFileURL(join(libDir, "feature-assertions.mjs")).href);
  return {
    smokeRoutes: new Set(discoverSmokeRoutes().map((route) => route.name)),
    journeys: readJson(journeysPath, {}).journeys ?? [],
    overlayJourneys,
    patternIds: new Set(Object.keys(REGRESSION_PATTERNS)),
    ...failableRefs({
      apiContracts: readJson(apiContractsPath, { endpoints: [] }),
      dataSanity: readJson(dataSanityPath, { checks: [] }),
      featureAssertions: readJson(featureAssertionsPath, []),
      isValueExpect,
    }),
  };
}

/**
 * Can this one reference fail while the page still loads and renders?
 *
 * The answer comes from the artefact the reference names, never from the manifest, so an entry
 * cannot talk itself into being covered. Returns null when it can fail, or the plain-English
 * reason it cannot - which is what gets printed when an entry claims more than it has.
 */
export function smokeReason(entry, interaction, ctx = {}) {
  const { type, ref } = interaction;
  if (type === "route-param") return "the sweep loads this page";
  if (type === "overlay") return "the sweep opens this overlay";
  if (type === "safeClick") return "the sweep clicks this control";
  if (type === "check") {
    // A regression pattern is a real defect detector: it fires when the page is wrong. It is
    // this entry's own coverage only when the entry IS that defect class - a `pattern` entry.
    // Cited by a feature entry it is the background sweep, which fires the same on a page where
    // the feature works perfectly and the same on a page where the feature is gone.
    return entry.kind === "pattern" ? null : "a background defect sweep that fires on any page, feature working or not";
  }
  if (type === "relation") return ctx.relations?.has(ref) ? null : "unresolved";
  if (type === "dataCheck") return ctx.dataChecks?.has(ref) ? null : "unresolved";
  if (type === "assertion") {
    if (!ctx.assertions?.has(ref)) return "unresolved";
    return ctx.valueAssertions?.has(ref) ? null : "it only checks whether things are on the page";
  }
  return "unresolved";
}

export function checkCoverage(manifest, ctx) {
  const { smokeRoutes, journeys, overlayJourneys, patternIds } = ctx;
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
    if (!STATUSES.has(entry.status)) findings.push(`${tag}: status must be covered|smoke-only|gap`);
    if (entry.status === "gap" && !String(entry.gapReason ?? "").trim()) findings.push(`${tag}: status gap without gapReason`);
    const routes = entry.routes ?? [];
    const interactions = entry.interactions ?? [];
    if (REACHED.has(entry.status) && routes.length === 0) findings.push(`${tag}: ${entry.status} but lists no routes`);
    if (REACHED.has(entry.status) && interactions.length === 0) findings.push(`${tag}: ${entry.status} but lists no interactions`);
    // The whole point of this guard. An entry is covered only if SOMETHING here can fail.
    const failable = interactions.filter((interaction) => smokeReason(entry, interaction, ctx) === null);
    if (entry.status === "covered" && interactions.length > 0 && failable.length === 0) {
      const why = [...new Set(interactions.map((i) => `${i.type} (${smokeReason(entry, i, ctx)})`))].join(", ");
      findings.push(
        `${tag}: claims covered, but nothing it names can fail while the page still renders - ${why}. `
        + "That is smoke-only. Covered needs a relation, a data contradiction, a value assertion, "
        + "or (for a pattern entry) the detector for its own defect class.",
      );
    }
    if (entry.status === "smoke-only" && failable.length > 0) {
      findings.push(`${tag}: status smoke-only but ${failable[0].type} ${JSON.stringify(failable[0].ref)} can fail - this is covered, say so`);
    }
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
      } else if (interaction.type === "relation" && !ctx.relations.has(interaction.ref)) {
        findings.push(`${where}: not a relation in api-contract-checks.json (key form <endpoint name>#<relation name>)`);
      } else if (interaction.type === "dataCheck" && !ctx.dataChecks.has(interaction.ref)) {
        findings.push(`${where}: not a data-sanity-checks.json check with expectRows 0 (only a self-contradiction can fail)`);
      } else if (interaction.type === "assertion" && !ctx.assertions.has(interaction.ref)) {
        findings.push(`${where}: not a sha in feature-assertions.json`);
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

/**
 * The honest number, printed every run.
 *
 * It is small. It is meant to be: it counts entries where a regression makes something go red,
 * not entries the sweep drives past. Do not make it bigger by relabelling - make it bigger by
 * writing checks that can fail.
 */
export function summarize(manifest) {
  const entries = manifest.entries ?? [];
  const count = (kind, status) => entries.filter((e) => e.kind === kind && (!status || e.status === status)).length;
  const line = (kind) => `${count(kind)} ${kind}s (${count(kind, "covered")} covered, ${count(kind, "smoke-only")} smoke-only, ${count(kind, "gap")} gap)`;
  const covered = count("feature", "covered") + count("pattern", "covered");
  const pct = entries.length ? Math.round((covered / entries.length) * 1000) / 10 : 0;
  return `coverage-since-aug1: ${line("feature")}, ${line("pattern")}; `
    + `${covered}/${entries.length} (${pct}%) carry a check that can fail - the rest are visited, not checked`;
}

function selfTest() {
  const ctx = {
    smokeRoutes: new Set(["feed-analytics", "orphan-route"]),
    journeys: [{ id: "feed", routes: ["feed-analytics"], safeClicks: [{ role: "button", name: "Filters" }] }],
    overlayJourneys: { "feed-analytics": [{ id: "drawer" }] },
    patternIds: new Set(["J-raw-text"]),
    relations: new Set(["weighing_shed_weights#the headline average is its own total over its own head count"]),
    dataChecks: new Set(["herd_total_vs_status_breakdown"]),
    assertions: new Set(["aaaa111", "bbbb222"]),
    valueAssertions: new Set(["bbbb222"]),
  };
  const good = {
    entries: [{
      id: "f", kind: "feature", title: "t", shippedIn: "x", commits: ["abcdef1"], routes: ["feed-analytics"], viewports: ["laptop", "mobile"], status: "covered",
      interactions: [
        { type: "route-param", ref: "feed-analytics" }, { type: "overlay", ref: "drawer" },
        { type: "safeClick", ref: "role:button:Filters" }, { type: "check", ref: "J-raw-text" },
        { type: "relation", ref: "weighing_shed_weights#the headline average is its own total over its own head count" },
      ],
    }],
  };
  assert.deepEqual(checkCoverage(good, ctx), []);
  const mutate = (fn) => { const copy = structuredClone(good); fn(copy.entries[0], copy); return checkCoverage(copy, ctx); };
  assert.ok(mutate((e) => { e.routes = ["nope"]; }).some((f) => /not in smoke-visual-live/.test(f)));
  assert.ok(mutate((e) => { e.routes.push("orphan-route"); }).some((f) => /no module journey/.test(f)));
  assert.ok(mutate((e) => { e.interactions[1].ref = "ghost"; }).some((f) => /overlayJourneys/.test(f)));
  assert.ok(mutate((e) => { e.interactions[2].ref = "role:button:Save"; }).some((f) => /module-journeys/.test(f)));
  assert.ok(mutate((e) => { e.interactions[3].ref = "Z-made-up"; }).some((f) => /REGRESSION_PATTERNS/.test(f)));
  assert.ok(mutate((e) => { e.interactions[4].ref = "made#up"; }).some((f) => /not a relation in api-contract-checks/.test(f)));
  assert.ok(mutate((e) => { e.status = "gap"; }).some((f) => /without gapReason/.test(f)));
  assert.ok(mutate((e) => { e.viewports = ["laptop"]; }).some((f) => /include mobile/.test(f)));

  // ------------------------------------------------------------------ the planted phantom
  //
  // THE PROOF THIS GUARD IS WORTH RUNNING. A phantom entry is the one the old guard could not
  // see: a real page, a real drawer, a real control, every reference resolving perfectly - and
  // not one thing in it that a regression could make go red. It claimed covered and was
  // believed. It must now fail the guard, and the failure must say which references are smoke.
  const phantom = {
    entries: [{
      id: "phantom", kind: "feature", title: "a page the sweep drives past", shippedIn: "x", commits: [],
      routes: ["feed-analytics"], viewports: ["laptop", "mobile"], status: "covered",
      interactions: [
        { type: "route-param", ref: "feed-analytics" },
        { type: "overlay", ref: "drawer" },
        { type: "safeClick", ref: "role:button:Filters" },
        { type: "check", ref: "J-raw-text" },
        { type: "assertion", ref: "aaaa111" },
      ],
    }],
  };
  const phantomFindings = checkCoverage(phantom, ctx);
  assert.ok(phantomFindings.some((f) => /claims covered, but nothing it names can fail/.test(f)),
    `a covered entry with only presence references must fail the guard, got: ${JSON.stringify(phantomFindings)}`);
  assert.match(phantomFindings[0], /the sweep loads this page/);
  assert.match(phantomFindings[0], /only checks whether things are on the page/);
  assert.match(phantomFindings[0], /background defect sweep/);
  // The same entry told the truth about itself is fine, and stays visible as not-really-checked.
  const owned = structuredClone(phantom);
  owned.entries[0].status = "smoke-only";
  assert.deepEqual(checkCoverage(owned, ctx), []);
  // And a value assertion, a data contradiction, or its own defect detector each earn it back.
  for (const earned of [
    { type: "assertion", ref: "bbbb222" },
    { type: "dataCheck", ref: "herd_total_vs_status_breakdown" },
  ]) {
    const upgraded = structuredClone(phantom);
    upgraded.entries[0].interactions.push(earned);
    assert.deepEqual(checkCoverage(upgraded, ctx), [], `${earned.type} must count as coverage`);
  }
  const asPattern = structuredClone(phantom);
  asPattern.entries[0].kind = "pattern";
  assert.deepEqual(checkCoverage(asPattern, ctx), [], "a pattern entry's own detector is its coverage");
  // A smoke-only entry that really does carry something failable is mislabelled the other way.
  const understated = structuredClone(owned);
  understated.entries[0].interactions.push({ type: "dataCheck", ref: "herd_total_vs_status_breakdown" });
  assert.ok(checkCoverage(understated, ctx).some((f) => /this is covered, say so/.test(f)));
  // No unearned verdicts in either direction: an entry the sweep never reaches cannot be smoke-only.
  const unreached = structuredClone(owned);
  unreached.entries[0].routes = [];
  unreached.entries[0].interactions = [];
  const unreachedFindings = checkCoverage(unreached, ctx);
  assert.ok(unreachedFindings.some((f) => /smoke-only but lists no routes/.test(f)));
  assert.ok(unreachedFindings.some((f) => /smoke-only but lists no interactions/.test(f)));

  // The printed number says how many entries can go red, and names the rest as visited only.
  assert.match(summarize(owned), /0 covered, 1 smoke-only, 0 gap/);
  assert.match(summarize(owned), /0\/1 \(0%\) carry a check that can fail/);
  assert.match(summarize(good), /1\/1 \(100%\) carry a check that can fail/);
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
