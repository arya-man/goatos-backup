// Value assertions for the web lane.
//
// Until now every one of the 926 entries in feature-assertions.json could only say that
// something APPEARS: `visible`, `absent`, `count >= 1`, `url`. None of them could say what a
// number, label or state SHOULD BE. That is why "Spend share only shows the feeds the farm
// buys" is asserted by the words "Feed spend share" — the chart's heading — and why putting
// every feed back into the pie leaves the check green. It is the same hole that let a screen
// render "Castro 1 1" for ten sightings while the sweep reported the page healthy.
//
// This module adds the missing half of the vocabulary: read the value off the screen and say
// what it must be.
//
//   equals     the rendered text is exactly this
//   matches    the rendered text has this shape
//   value      the rendered number, compared to a number
//   reconcile  one figure on the screen against another figure that must agree with it
//
// WHERE THE EXPECTED VALUE COMES FROM matters more than the comparison. In order of preference:
//
//   1. RECONCILIATION — two places on the product that must agree: a badge against the rows it
//      counts, a total against the sum of its parts, a chart's slices against its legend. It
//      needs no fixture, it cannot go stale, and it is wrong exactly when the product is wrong.
//   2. A READ-ONLY QUERY against goatos-stg for the same figure the screen claims to show.
//      Single capped SELECT inside BEGIN READ ONLY, rolled back — the lane-2 guard, reused, not
//      re-implemented.
//   3. A PINNED number. Last resort, because it goes stale; it must carry `why` and a date.
//
// A code comment is never a source of truth. `operational-location.ts` says shed "Yashoda" is
// unpartitioned; the database says it has partitions 1-10.
//
// NO UNEARNED VERDICTS. A comparison whose figure could not be read reports NOT ATTEMPTED and
// says so out loud. It never quietly counts as a pass. An `expect` object this file does not
// recognise is an error, not a silent pass.

import { spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { assertSelectOnly, cappedSql, readOnlySql } from "../../../../tools/dashboard-automation/check-data-sanity.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const queryCataloguePath = join(here, "../../../../tools/dashboard-automation/value-assertion-queries.json");

// The four comparison forms this module owns. Anything else in an `expect` belongs to the
// presence vocabulary in feature-assertions.mjs.
export const VALUE_EXPECT_KEYS = ["equals", "matches", "value", "reconcile"];

export function isValueExpect(expect) {
  return VALUE_EXPECT_KEYS.some((key) => expect && Object.hasOwn(expect, key));
}

// ---------------------------------------------------------------------------------------------
// Reading a number off the screen
// ---------------------------------------------------------------------------------------------

// The farm reads rupees in the Indian grouping (₹1,23,456), weights with a unit ("312.5 kg"),
// coverage as a percentage, and counts bare. All of those are one number. Anything with a
// second number in it is ambiguous, and guessing which one was meant is how a check ends up
// silently comparing the wrong figure — so ambiguity is an error unless the assertion says
// which one it wants.
const NUMBER_PATTERN = /-?\d[\d,   ]*(?:\.\d+)?/g;

export function parseNumbers(text) {
  const cleaned = String(text ?? "")
    // A dash used as a placeholder ("—", "–", "-") is not a number.
    .replaceAll("—", " ")
    .replaceAll("–", " ");
  const out = [];
  for (const match of cleaned.matchAll(NUMBER_PATTERN)) {
    const bare = match[0].replace(/[,   ]/g, "");
    if (bare === "" || bare === "-") continue;
    const n = Number(bare);
    if (Number.isFinite(n)) out.push(n);
  }
  return out;
}

// `pick` defaults to "only": if the text holds more than one number the assertion must say
// which, rather than the checker choosing for it.
export function numberFrom(text, pick = "only", what = "value") {
  const numbers = parseNumbers(text);
  if (numbers.length === 0) return { error: `${what}: no number in "${short(text)}"` };
  if (pick === "first") return { number: numbers[0] };
  if (pick === "last") return { number: numbers[numbers.length - 1] };
  if (pick === "sum") return { number: numbers.reduce((a, b) => a + b, 0) };
  if (typeof pick === "number") {
    if (pick >= numbers.length) return { error: `${what}: asked for number ${pick + 1} of "${short(text)}"` };
    return { number: numbers[pick] };
  }
  if (numbers.length > 1) {
    return { error: `${what}: "${short(text)}" holds ${numbers.length} numbers and the check does not say which one it means` };
  }
  return { number: numbers[0] };
}

function short(text) {
  const one = String(text ?? "").replace(/\s+/g, " ").trim();
  return one.length > 60 ? `${one.slice(0, 57)}…` : one;
}

export function normaliseText(text) {
  return String(text ?? "")
    .replace(/ /g, " ")
    .replace(/\s+/g, " ")
    .trim();
}

// ---------------------------------------------------------------------------------------------
// Reading one side of a comparison
// ---------------------------------------------------------------------------------------------
//
// A side is one of:
//   { css, nth?, attr?, pick?, says }   the number (or text) one element shows
//   { countOf: css, says }              how many of those elements there are
//   { sumOf: css, pick?, says }         the numbers on all of them, added up
//   { literal: 40, says, why }          a pinned number — last resort
//   { query: "id", says }               a read-only figure from goatos-stg
//
// Every side carries `says`: the words a person would use for it on the screen. That is what
// reaches Slack. No selector, no column name, no SQL ever does.

export function describeSide(side) {
  return side?.says ?? side?.css ?? side?.countOf ?? side?.sumOf ?? side?.query ?? String(side?.literal ?? "");
}

async function elementText(page, { css, nth = 0, attr }) {
  const loc = page.locator(css);
  const total = await loc.count();
  if (total === 0) return { missing: true };
  const index = nth < 0 ? total + nth : nth;
  if (index >= total || index < 0) return { missing: true };
  const one = loc.nth(index);
  const text = attr ? await one.getAttribute(attr) : await one.innerText();
  return { text: normaliseText(text), loc: one };
}

// Returns { number } | { text } | { missing: true } | { notAttempted, why } | { error }
export async function readSide(page, side, { queries } = {}) {
  if (!side || typeof side !== "object") return { error: "a comparison needs two sides" };
  const what = describeSide(side);

  if (Object.hasOwn(side, "literal")) {
    if (!side.why) return { error: `${what}: a pinned number must say why it is pinned and when it was taken` };
    return { number: Number(side.literal), pinned: true };
  }

  if (side.query) {
    const answer = await readQuery(side.query, queries);
    return answer;
  }

  if (side.countOf) {
    const n = await page.locator(side.countOf).count();
    return { number: n, counted: true };
  }

  if (side.sumOf) {
    const loc = page.locator(side.sumOf);
    const total = await loc.count();
    if (total === 0) return { missing: true };
    let sum = 0;
    for (let i = 0; i < total; i += 1) {
      const raw = side.attr ? await loc.nth(i).getAttribute(side.attr) : await loc.nth(i).innerText();
      const one = numberFrom(raw, side.pick ?? "only", what);
      if (one.error) return { error: one.error };
      sum += one.number;
    }
    return { number: round(sum), summed: total };
  }

  if (side.css) {
    const read = await elementText(page, side);
    if (read.missing) return { missing: true };
    if (side.asText) return { text: read.text, loc: read.loc };
    const one = numberFrom(read.text, side.pick ?? "only", what);
    if (one.error) return { error: one.error, loc: read.loc };
    return { number: one.number, loc: read.loc, text: read.text };
  }

  return { error: "a comparison side needs one of css, countOf, sumOf, query or literal" };
}

// Floating point: 0.1 + 0.2 must not make a green check red.
function round(n) {
  return Math.round(n * 1e6) / 1e6;
}

// ---------------------------------------------------------------------------------------------
// The read-only query source
// ---------------------------------------------------------------------------------------------
//
// Same discipline as lane 2, imported rather than re-implemented: SELECT only, no chaining, no
// comments, balanced parentheses, a structural row cap, and an explicit BEGIN READ ONLY that is
// rolled back. stg is production data — this may only ever read it.

let cachedQueries = null;
export function loadValueQueries(path = queryCataloguePath) {
  if (cachedQueries && cachedQueries.path === path) return cachedQueries.byId;
  const byId = new Map();
  if (existsSync(path)) {
    for (const entry of JSON.parse(readFileSync(path, "utf8")).queries ?? []) {
      assertSelectOnly(entry.sql, entry.id);
      byId.set(entry.id, entry);
    }
  }
  cachedQueries = { path, byId };
  return byId;
}

const queryAnswers = new Map();

export async function readQuery(id, queries = loadValueQueries()) {
  if (queryAnswers.has(id)) return queryAnswers.get(id);
  const entry = queries.get?.(id) ?? queries?.[id];
  if (!entry) {
    const answer = { error: `no read-only query is registered as "${id}"` };
    queryAnswers.set(id, answer);
    return answer;
  }
  const dsn = process.env.GOATOS_STG_READONLY_DATABASE_URL;
  if (!dsn) {
    // Contract rule 4: a check that did not run must never render a verdict.
    const answer = { notAttempted: true, why: "the read-only farm database was not reachable from this run" };
    queryAnswers.set(id, answer);
    return answer;
  }
  const psql = process.env.GOATOS_PSQL_BIN || (existsSync("/usr/bin/psql") ? "/usr/bin/psql" : "psql");
  const child = spawnSync(psql, [dsn, "-v", "ON_ERROR_STOP=1", "-X", "-A", "-t", "-F", "\t"], {
    input: `${readOnlySql(cappedSql(entry.sql, 1), 20000)}\n`,
    encoding: "utf8",
    timeout: 45_000,
  });
  if (child.status !== 0) {
    // Never a pass and never an accusation against the product: the check did not run.
    const answer = { notAttempted: true, why: "the read-only farm database could not be read for this figure" };
    queryAnswers.set(id, answer);
    return answer;
  }
  const line = String(child.stdout ?? "").split("\n").map((l) => l.trim()).filter(Boolean).pop();
  const one = numberFrom(line, entry.pick ?? "only", entry.says ?? id);
  const answer = one.error ? { notAttempted: true, why: "the farm database did not return a single number for this figure" } : { number: one.number, fromDatabase: true };
  queryAnswers.set(id, answer);
  return answer;
}

export function resetQueryCache() {
  queryAnswers.clear();
  cachedQueries = null;
}

// ---------------------------------------------------------------------------------------------
// The comparisons
// ---------------------------------------------------------------------------------------------

const RELATIONS = {
  equals: (a, b, tol) => Math.abs(a - b) <= tol,
  notEquals: (a, b, tol) => Math.abs(a - b) > tol,
  atLeast: (a, b, tol) => a >= b - tol,
  atMost: (a, b, tol) => a <= b + tol,
  greaterThan: (a, b, tol) => a > b + tol,
  lessThan: (a, b, tol) => a < b - tol,
};

const RELATION_WORDS = {
  equals: "should be the same as",
  notEquals: "should not be the same as",
  atLeast: "should be at least",
  atMost: "should be at most",
  greaterThan: "should be more than",
  lessThan: "should be less than",
};

// The same relations as one half of a sentence about two things on a screen, so a reconciliation
// reads as English: "… says 99999, and that should be no more than the deaths tile, which says 6".
const PLAIN_RELATION = {
  notEquals: "different from",
  atLeast: "at least",
  atMost: "no more than",
  greaterThan: "more than",
  lessThan: "less than",
};

export function formatNumber(n) {
  if (!Number.isFinite(n)) return String(n);
  return Number.isInteger(n) ? String(n) : String(round(n));
}

// Returns null when the comparison holds, otherwise a finding in the farm's words.
//   { what, loc }                 -> a real mismatch, box it on the screenshot
//   { notAttempted: true, what }  -> the figure could not be read; NOT a pass and NOT a failure
//   { missing: true, ... }        -> the element is not on the page; the presence vocabulary's job
export async function checkValueExpect(page, expect, options = {}) {
  if (expect.equals) return checkEquals(page, expect.equals);
  if (expect.matches) return checkMatches(page, expect.matches);
  if (expect.value) return checkValue(page, expect.value, options);
  if (expect.reconcile) return checkReconcile(page, expect.reconcile, options);
  return null;
}

async function checkEquals(page, spec) {
  const what = describeSide(spec);
  const read = await elementText(page, spec);
  if (read.missing) return { missing: true, what: `not visible: ${what}` };
  const actual = read.text;
  const wanted = normaliseText(spec.is);
  if (actual === wanted) return null;
  return { what: `${what} reads "${short(actual)}" but should read "${short(wanted)}"`, loc: read.loc };
}

async function checkMatches(page, spec) {
  const what = describeSide(spec);
  const read = await elementText(page, spec);
  if (read.missing) return { missing: true, what: `not visible: ${what}` };
  let re;
  try {
    re = new RegExp(spec.pattern, spec.flags ?? "");
  } catch {
    return { what: `${what}: the check's own pattern is not usable`, loc: null };
  }
  const hit = re.test(read.text);
  const wantHit = spec.expect !== false;
  if (hit === wantHit) return null;
  const how = spec.shouldRead ?? (wantHit ? "is not in the shape it should be" : "is in a shape it should never be");
  return { what: `${what} reads "${short(read.text)}", which ${how}`, loc: read.loc };
}

async function checkValue(page, spec, options) {
  const what = describeSide(spec);
  const side = await readSide(page, spec, options);
  if (side.missing) return { missing: true, what: `not visible: ${what}` };
  if (side.notAttempted) return { notAttempted: true, what: `${what}: ${side.why}` };
  if (side.error) return { what: side.error, loc: side.loc ?? null };
  for (const relation of Object.keys(RELATIONS)) {
    if (!Object.hasOwn(spec, relation)) continue;
    const wanted = Number(spec[relation]);
    const tol = Number(spec.tolerance ?? 0);
    if (RELATIONS[relation](side.number, wanted, tol)) continue;
    return {
      what: `${what} shows ${formatNumber(side.number)}, which ${RELATION_WORDS[relation]} ${formatNumber(wanted)}`,
      loc: side.loc ?? null,
    };
  }
  return null;
}

async function checkReconcile(page, spec, options) {
  const relation = spec.relation ?? "equals";
  if (!RELATIONS[relation]) return { what: `this check asks for a comparison that does not exist`, loc: null };
  const left = await readSide(page, spec.left, options);
  const right = await readSide(page, spec.right, options);

  // Either side absent: that is the presence vocabulary's job, and on a page with no rows it is
  // not a defect. Say it did not run rather than claiming either verdict.
  for (const [side, read] of [[spec.left, left], [spec.right, right]]) {
    if (read.missing) return { missing: true, what: `not visible: ${describeSide(side)}` };
    if (read.notAttempted) return { notAttempted: true, what: `${describeSide(side)}: ${read.why}` };
    if (read.error) return { what: read.error, loc: read.loc ?? null };
  }

  // Both sides counted nothing. 0 == 0 is true and proves nothing: it is the "page is empty"
  // state, not a reconciliation. Reporting it as a pass is exactly the unearned verdict the
  // contract forbids, so it reports as nothing-to-reconcile instead.
  if (left.number === 0 && right.number === 0 && (left.counted || left.summed !== undefined) && (right.counted || right.summed !== undefined)) {
    return { missing: true, what: `not visible: ${describeSide(spec.left)}` };
  }

  const tol = Number(spec.tolerance ?? 0);
  if (RELATIONS[relation](left.number, right.number, tol)) return null;

  const leftWords = `${describeSide(spec.left)} says ${formatNumber(left.number)}`;
  const rightWords = `${describeSide(spec.right)} ${right.summed || right.counted ? "comes to" : "says"} ${formatNumber(right.number)}`;
  const what = relation === "equals"
    ? `${leftWords} but ${rightWords}`
    : `${leftWords}, and that should be ${PLAIN_RELATION[relation]} ${rightWords}`;
  return { what, loc: left.loc ?? right.loc ?? null };
}

// ---------------------------------------------------------------------------------------------
// Catalogue guard
// ---------------------------------------------------------------------------------------------
//
// "Evidence path invented" is the failure this lane already caught once and must never commit.
// Every value assertion has to name, in `provenance`, where its expected value comes from, and
// pinned numbers have to justify themselves. This runs as a test over the manifest, so a new
// entry cannot be added without one.

export const PROVENANCE_KINDS = ["reconciliation", "read-only-query", "pinned"];

export function validateValueExpect(expect, where = "entry") {
  const problems = [];
  const sides = [];
  if (expect.reconcile) {
    if (!expect.reconcile.left || !expect.reconcile.right) problems.push(`${where}: a reconciliation needs both sides`);
    sides.push(expect.reconcile.left, expect.reconcile.right);
  }
  if (expect.value) sides.push(expect.value);
  if (expect.value && !Object.keys(RELATIONS).some((r) => Object.hasOwn(expect.value, r))) {
    problems.push(`${where}: a value check must say what the number should be`);
  }
  if (expect.equals && expect.equals.is === undefined) problems.push(`${where}: an equals check must say what the text should read`);
  if (expect.matches && !expect.matches.pattern) problems.push(`${where}: a matches check must carry a pattern`);
  for (const side of sides.filter(Boolean)) {
    if (!side.says) problems.push(`${where}: every side must say, in the farm's words, what a person sees`);
    if (Object.hasOwn(side, "literal") && !side.why) problems.push(`${where}: a pinned number must say why and when`);
    const shapes = ["css", "countOf", "sumOf", "query", "literal"].filter((k) => Object.hasOwn(side, k));
    if (shapes.length !== 1) problems.push(`${where}: a side must be exactly one of css, countOf, sumOf, query or literal`);
  }
  for (const side of [expect.equals, expect.matches].filter(Boolean)) {
    if (!side.says) problems.push(`${where}: every check must say, in the farm's words, what a person sees`);
    if (!side.css) problems.push(`${where}: text checks read one element, so they need a css target`);
  }
  return problems;
}

// A finding must reach Slack in the farm's words. Selectors, column names, SQL and check codes
// are contract violations, so the catalogue is tested against this.
const JARGON = [
  /\bdata-testid\b/i, /\bclassName\b/i, /\baria-\w+/i, /[.#][a-z][\w-]*\s*[>{]/i,
  /\bselect\b.*\bfrom\b/i, /\bnull\b/i, /\bundefined\b/i, /\bNaN\b/i,
  /\blane\d\./i, /_[a-z]+_[a-z]+/i, /\bHTTP \d{3}\b/, /\bcss\b/i,
];

export function saysSoundsHuman(text) {
  const value = String(text ?? "");
  if (value.trim().length < 4) return false;
  return !JARGON.some((pattern) => pattern.test(value));
}
