#!/usr/bin/env node
// Lane 4, screen half: does each journey's screen assertion actually TELL THE PAGES APART?
//
// §3 of the handover is about assertions that could not fail: 178 of 276 reported green against a
// BLANK PAGE. So the bar here is not "the assertion is declared" and not even "the assertion
// passes" - it is that the assertion FAILS on a page that never loaded, FAILS on the page as it
// reads BEFORE the write, and passes only AFTER. An assertion that cannot do all three is not
// coverage, and this runner says so by name.
//
// It drives no browser and reaches no network. It judges the assertion against the renderings in
// write-journey-screens.json, which are the SPEC of what each assertion must distinguish.
import { existsSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { evaluateScreenAssertion, pageFrom, discriminatingClauses } from "./lib/screen-assertion.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "../..");
const journeysPath = path.join(here, "write-journeys.json");
const screensPath = path.join(here, "write-journey-screens.json");

export function fillTokens(value, tokens) {
  let out = JSON.stringify(value);
  for (const [token, replacement] of Object.entries(tokens)) {
    out = out.split(token).join(replacement);
  }
  return JSON.parse(out);
}

export function loadScreens(file = screensPath) {
  if (!existsSync(file)) {
    // Fail closed: with no renderings there is nothing to tell apart, and a silent pass here
    // would be the very claim this runner exists to refuse.
    throw new Error("the write journey screen renderings are missing, so no screen assertion can be shown able to fail");
  }
  const parsed = JSON.parse(readFileSync(file, "utf8"));
  if (!parsed?.pages || typeof parsed.pages !== "object") {
    throw new Error("the write journey screen renderings carry no pages, so nothing can be told apart");
  }
  return parsed;
}

/** The three-way verdict for one journey. */
export function proveJourney(journey, pages, tokens) {
  const assertion = fillTokens(journey.screenAssertion ?? null, tokens);
  if (!assertion) {
    return { name: journey.name, proved: false, reason: "this journey does not say what must be on screen afterwards" };
  }
  if (!pages) {
    return { name: journey.name, proved: false, reason: "this journey has no before-and-after reading of its screen, so its assertion has never been shown able to fail" };
  }
  const filled = fillTokens(pages, tokens);
  const required = ["blank", "before", "after"];
  for (const name of required) {
    if (!filled[name]) {
      return { name: journey.name, proved: false, reason: `this journey has no ${name} reading of its screen, so its assertion has never been shown able to fail` };
    }
  }
  const blank = evaluateScreenAssertion(assertion, pageFrom(filled.blank.text, filled.blank.columns));
  if (blank.ok) {
    return { name: journey.name, proved: false, reason: "the assertion passes on a page that never loaded, so it cannot prove the write happened" };
  }
  const kind = filled.kind === "invariance" ? "invariance" : "change";
  if (kind === "change") {
    const before = evaluateScreenAssertion(assertion, pageFrom(filled.before.text, filled.before.columns));
    if (before.ok) {
      return { name: journey.name, proved: false, reason: "the assertion passes on the screen as it reads BEFORE the write, so it cannot tell the write apart from nothing happening" };
    }
  } else if (!Object.keys(filled).some((name) => !["blank", "before", "after", "kind"].includes(name))) {
    // A journey whose promise is that nothing changed cannot be judged against the before page -
    // before and after read alike by design. It must name the wrong outcome it catches instead.
    return { name: journey.name, proved: false, reason: "this journey promises the screen does not change, and names no wrong outcome its assertion must catch, so its assertion has never been shown able to fail" };
  }
  const after = evaluateScreenAssertion(assertion, pageFrom(filled.after.text, filled.after.columns));
  if (!after.ok) {
    return { name: journey.name, proved: false, reason: `the assertion does not pass even after the write: ${after.missing.join("; ")}` };
  }
  // Optional fourth reading: the wrong outcome this journey is really guarding against.
  const extras = [];
  for (const name of Object.keys(filled)) {
    if (required.includes(name) || name === "kind") continue;
    const wrong = evaluateScreenAssertion(assertion, pageFrom(filled[name].text, filled[name].columns));
    if (wrong.ok) {
      return { name: journey.name, proved: false, reason: `the assertion also passes on the wrong outcome (${name}), so it does not catch it` };
    }
    extras.push(name);
  }
  return {
    name: journey.name,
    proved: true,
    kind,
    tellsApart: ["a page that never loaded", ...(kind === "change" ? ["the screen before the write"] : []), ...extras.map((e) => `the wrong outcome (${e})`)],
    because: discriminatingClauses(assertion)
  };
}

const args = parse(process.argv.slice(2));
const isMain = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (isMain) process.exit(args.selfTest ? selfTest() : main());

function main() {
  const journeys = JSON.parse(readFileSync(journeysPath, "utf8")).journeys ?? [];
  const screens = loadScreens();
  const report = {
    generatedAt: new Date().toISOString(),
    howProved: "each assertion was judged against the screen as it reads before the write, after it, and on a page that never loaded; no browser was driven and no live page was opened",
    totalJourneys: journeys.length,
    proved: [],
    notProved: []
  };
  for (const journey of journeys) {
    const result = proveJourney(journey, screens.pages[journey.name], screens.tokens ?? {});
    if (result.proved) report.proved.push(result);
    else report.notProved.push({ name: result.name, reason: result.reason });
  }
  report.coverage = `${report.proved.length}/${report.totalJourneys}`;
  const out = path.resolve(args.out ?? path.join(repo, ".codex-goatos-render/dashboard-automation/write-journey-screens.json"));
  mkdirSync(path.dirname(out), { recursive: true });
  writeFileSync(out, `${JSON.stringify(report, null, 2)}\n`);
  console.log(`lane 4 screen assertions: ${report.proved.length} of ${report.totalJourneys} can tell the screen after the write from the screen before it and from a page that never loaded`);
  for (const gap of report.notProved) console.log(`  not proved — ${gap.name}: ${gap.reason}`);
  // A floor, not a threshold to relax. CI holds the number that is proved today so it cannot
  // quietly fall; the gaps stay named and visible in the same breath. Lowering the floor is a
  // visible edit to the build file, which is what the guard-weakening guard reads.
  if (Number.isFinite(args.min)) {
    if (report.proved.length < args.min) {
      console.error(`this run proves ${report.proved.length}, fewer than the ${args.min} proved when this floor was set`);
      return 1;
    }
    return 0;
  }
  return report.notProved.length === 0 ? 0 : 1;
}

function parse(raw) {
  const parsed = {};
  for (let i = 0; i < raw.length; i += 1) {
    if (raw[i] === "--self-test") parsed.selfTest = true;
    else if (raw[i] === "--out") parsed.out = raw[++i];
    else if (raw[i] === "--min") parsed.min = Number(raw[++i]);
    else throw new Error(`unknown argument: ${raw[i]}`);
  }
  return parsed;
}

function selfTest() {
  // Fail closed when the renderings are gone.
  let refused = false;
  try { loadScreens(path.join(here, "does-not-exist.json")); } catch { refused = true; }
  if (!refused) throw new Error("self-test: missing screen renderings must refuse, not pass");

  const pages = { blank: { text: "" }, before: { text: "Plan. A draft is waiting." }, after: { text: "Plan. Live right now." } };
  const strong = { name: "x", screenAssertion: { visible: [{ text: "Live right now" }], notVisible: [{ text: "A draft is waiting" }] } };
  if (!proveJourney(strong, pages, {}).proved) throw new Error("self-test: a discriminating assertion must prove");

  // A word the screen shows either way must NOT prove.
  const weak = { name: "y", screenAssertion: { visible: [{ text: "Plan" }] } };
  if (proveJourney(weak, pages, {}).proved) throw new Error("self-test: an assertion that passes before the write must not prove");

  // An assertion that passes on a blank page must not prove.
  const empty = { name: "z", screenAssertion: {} };
  if (proveJourney(empty, pages, {}).proved) throw new Error("self-test: an empty assertion must not prove");

  // A clause the evaluator cannot judge must refuse rather than pass.
  const unknown = { name: "w", screenAssertion: { visible: [{ text: "Live right now" }], eventually: [{ text: "x" }] } };
  const unknownResult = proveJourney(unknown, pages, {});
  if (unknownResult.proved) throw new Error("self-test: a clause nobody can judge must not prove");

  // An invariance journey that names no wrong outcome must not prove.
  const invariant = { name: "inv", screenAssertion: { visible: [{ text: "Plan" }] } };
  if (proveJourney(invariant, { ...pages, kind: "invariance" }, {}).proved) {
    throw new Error("self-test: an invariance journey naming no wrong outcome must not prove");
  }
  // And one that names a wrong outcome its assertion does not catch must not prove either.
  const blind = { name: "blind", screenAssertion: { visible: [{ text: "Plan" }] } };
  if (proveJourney(blind, { ...pages, kind: "invariance", wrongOutcome: { text: "Plan. Saved." } }, {}).proved) {
    throw new Error("self-test: an invariance journey blind to its own wrong outcome must not prove");
  }

  // A journey with no renderings is a named gap, never a pass.
  if (proveJourney(strong, null, {}).proved) throw new Error("self-test: a journey with no renderings must not prove");

  // Every journey must be accounted for, proved or named.
  const journeys = JSON.parse(readFileSync(journeysPath, "utf8")).journeys ?? [];
  const screens = loadScreens();
  let accounted = 0;
  for (const journey of journeys) {
    const result = proveJourney(journey, screens.pages[journey.name], screens.tokens ?? {});
    if (!result.proved && (!result.reason || result.reason.length < 20)) {
      throw new Error(`self-test: ${journey.name} is not proved and has no named reason`);
    }
    accounted += 1;
  }
  if (accounted !== journeys.length) throw new Error("self-test: every journey must be accounted for");
  console.log(`lane 4 screen assertion prover: self-test passed (${journeys.length} journeys accounted for)`);
  return 0;
}
