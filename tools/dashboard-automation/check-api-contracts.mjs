#!/usr/bin/env node
// Lane 3: read-only backend API contract + latency sweep against the production API.
//
// One question, asked for every screen on the dashboard: "when this page loads, does the
// server hand it usable data, fast enough that a person does not notice?"
//
// Safety, by construction rather than by convention:
//   * `get()` below is the ONLY place this lane calls fetch, and it hard-codes
//     method: "GET". There is no method parameter to get wrong.
//   * every request URL is RESOLVED against the base and then re-checked against the
//     production host allowlist (the same list as run.mjs runProductionSmoke). Paths come
//     from a catalogue and from lane-checks.json, which another automation generates, so
//     the allowlist is applied to the URL actually fetched, never to the input string.
//   * redirects are NOT followed automatically; each hop is re-checked against the
//     allowlist, with a hop cap.
//
// Latency: this lane does NOT replace tools/perf/api-latency-gate.mjs. That gate certifies
// a build. This is a broad per-run sweep. It uses the gate's own percentile function and
// the same warmup-then-measure shape so the numbers are comparable, and it only reports a
// screen as slow when the breach is sustained (see SLOW_RULE).
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { containsUnredactedSecret, redactText } from "./lib/redact.mjs";
import { API_LATENCY_POLICY_MS, normalizeApiLatencyEndpoints } from "../perf/api-latency-policy.mjs";
import { reportHtml, toFindings as findingsForReport } from "./lib/finding-kinds/api-contracts.mjs";

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const CATALOGUE_PATH = path.join(repo, "tools/dashboard-automation/api-contract-checks.json");
const LANE_CHECKS_PATH = path.join(repo, "tools/dashboard-automation/lane-checks.json");
const CONFIG_PATH = path.join(repo, "tools/dashboard-automation/config.json");

// Mirrors run.mjs runProductionSmoke. Nothing outside this list may be called.
export const PRODUCTION_API_HOSTS = Object.freeze(["api.goatos.mesha.sg", "api.mesha.sg", "goatos-api.mesha.sg"]);

// Warmup is discarded; measurement matches tools/perf/api-latency-gate.mjs (20 iterations,
// warmup kept out of the percentiles) so a p95 here means what a p95 there means.
export const DEFAULT_WARMUP = 3;
export const DEFAULT_SAMPLES = 20;
export const MAX_REDIRECT_HOPS = 3;

// A single slow sample is not something a person saw. We only call a screen slow when the
// breach is sustained: the median request is over budget too, i.e. it is slow more often
// than not. p95 is still reported, and the HTML report carries every sample.
export const SLOW_RULE = "p95 and median both over budget";

// ---------------------------------------------------------------------------
// Guards
// ---------------------------------------------------------------------------

export function assertGetOnly(entry) {
  const method = String(entry?.method ?? "").toUpperCase();
  if (method !== "GET") {
    throw new Error(`api-contract ${entry?.name ?? "?"} declares method ${method || "(none)"}; this lane is read-only and may only issue GET`);
  }
  return "GET";
}

// Validates a RESOLVED url object or string. Rejects non-https, credentials in the URL,
// and any host that is not exactly one of the production API hosts.
export function assertProductionApiUrl(rawUrl) {
  let url;
  try {
    url = rawUrl instanceof URL ? rawUrl : new URL(String(rawUrl ?? ""));
  } catch {
    throw new Error("api-contract sweep refuses an unparseable API URL");
  }
  if (url.protocol !== "https:") {
    throw new Error(`api-contract sweep refuses non-https API URL: ${url.protocol}//${url.hostname}`);
  }
  if (url.username || url.password) {
    throw new Error(`api-contract sweep refuses an API URL carrying credentials: ${url.hostname}`);
  }
  if (!PRODUCTION_API_HOSTS.includes(url.hostname)) {
    throw new Error(`api-contract sweep refuses non-production API URL: ${url.origin}`);
  }
  return url;
}

// Resolve a catalogue path against the base and re-check the result. `new URL(path, base)`
// is used rather than string concatenation: "@evil.test/x" and "../../" both escape a
// concatenated prefix, and only the resolved object tells the truth about the host.
export function resolveRequestUrl(baseUrl, requestPath) {
  let resolved;
  try {
    resolved = new URL(String(requestPath ?? ""), baseUrl);
  } catch {
    throw new Error("api-contract sweep refuses an unresolvable request path");
  }
  return assertProductionApiUrl(resolved);
}

// ---------------------------------------------------------------------------
// The single choke point. This is the only fetch call in the lane.
// ---------------------------------------------------------------------------

export async function get(baseUrl, requestPath, { headers, timeoutMs = 20000, fetchImpl = fetch } = {}) {
  let target = resolveRequestUrl(baseUrl, requestPath);
  for (let hop = 0; hop <= MAX_REDIRECT_HOPS; hop += 1) {
    const response = await fetchImpl(target.href, {
      method: "GET",
      headers,
      cache: "no-store",
      redirect: "manual",
      signal: AbortSignal.timeout(timeoutMs),
    });
    if (![301, 302, 303, 307, 308].includes(response.status)) {
      return { response, url: target, hops: hop };
    }
    const location = response.headers?.get?.("location");
    if (!location) return { response, url: target, hops: hop };
    // Re-run the allowlist on the hop. Leaving the allowlist is itself reportable.
    target = resolveRequestUrl(target.href, location);
  }
  throw new Error(`api-contract sweep refuses more than ${MAX_REDIRECT_HOPS} redirects`);
}

// ---------------------------------------------------------------------------
// Path resolution: dotted paths, with "[]" meaning "every element of this array"
// ---------------------------------------------------------------------------

export function resolvePath(payload, dotted) {
  const segments = String(dotted ?? "").split(".").filter(Boolean);
  let frontier = [{ path: "", value: payload }];
  for (const segment of segments) {
    const isArray = segment.endsWith("[]");
    const key = isArray ? segment.slice(0, -2) : segment;
    const next = [];
    for (const node of frontier) {
      const container = key ? node.value?.[key] : node.value;
      const here = key ? (node.path ? `${node.path}.${key}` : key) : node.path;
      if (!isArray) {
        next.push({ path: here, value: container, missing: !isObjectLike(node.value) || !(key in Object(node.value)) });
        continue;
      }
      if (!Array.isArray(container)) {
        next.push({ path: here, value: undefined, missing: true, notAnArray: true });
        continue;
      }
      container.forEach((item, index) => next.push({ path: `${here}[${index}]`, value: item, missing: false }));
    }
    frontier = next;
  }
  return frontier;
}

function isObjectLike(value) {
  return value !== null && typeof value === "object";
}

// ---------------------------------------------------------------------------
// Assertions
// ---------------------------------------------------------------------------

// A required field must be present and non-null. An empty array is allowed — a farm can
// legitimately have no rows today. A missing or null field is not: that is what makes a
// column render blank.
export function missingRequiredFields(payload, requiredFields = []) {
  const problems = [];
  for (const field of requiredFields) {
    const nodes = resolvePath(payload, field);
    if (nodes.length === 0) {
      problems.push({ field, concrete: field, reason: "missing" });
      continue;
    }
    for (const node of nodes) {
      if (node.missing || node.value === undefined) problems.push({ field, concrete: node.path || field, reason: "missing" });
      else if (node.value === null) problems.push({ field, concrete: node.path || field, reason: "null" });
    }
  }
  return problems;
}

const FORBIDDEN_TOKENS = Object.freeze({
  null: (value) => value === null,
  NaN: (value) => (typeof value === "number" && Number.isNaN(value)) || value === "NaN",
  empty: (value) => value === "",
  undefined: (value) => value === undefined || value === "undefined",
});

export const DEFAULT_FORBIDDEN = Object.freeze(["null", "NaN", "empty", "undefined"]);

// Values the UI renders must not be null / NaN / "" / "undefined", and an enum field must
// not carry a member the screen has no label for — that is how a raw code reaches a user.
export function forbiddenValueFindings(payload, specs = []) {
  const problems = [];
  for (const spec of specs) {
    const disallow = spec.disallow ?? (spec.enum ? [] : DEFAULT_FORBIDDEN);
    const nodes = resolvePath(payload, spec.path);
    for (const node of nodes) {
      if (node.notAnArray) continue;
      for (const token of disallow) {
        const test = FORBIDDEN_TOKENS[token];
        if (test && test(node.value)) {
          problems.push({ path: spec.path, concrete: node.path, reason: token, value: describeValue(node.value) });
        }
      }
      if (Array.isArray(spec.enum) && node.value !== undefined && node.value !== null) {
        if (!spec.enum.includes(node.value)) {
          problems.push({ path: spec.path, concrete: node.path, reason: "unknown-enum", value: describeValue(node.value) });
        }
      }
    }
  }
  return problems;
}

// A deep sweep, but ONLY under subtrees a page is known to render (the entry's declared
// fields). A NaN in a field no screen reads is not something to wake anyone for, so an
// undeclared subtree is never walked.
export function declaredSubtreeForbiddenFindings(payload, entry, { tokens = ["NaN", "undefined"], maxNodes = 5000 } = {}) {
  const roots = [
    ...(entry.requiredFields ?? []),
    ...(entry.forbiddenValues ?? []).map((spec) => spec.path),
  ];
  const problems = [];
  let visited = 0;
  const walk = (value, trail) => {
    if (visited++ > maxNodes) return;
    if (Array.isArray(value)) return value.forEach((item, index) => walk(item, `${trail}[${index}]`));
    if (isObjectLike(value)) {
      for (const [key, item] of Object.entries(value)) walk(item, trail ? `${trail}.${key}` : key);
      return;
    }
    for (const token of tokens) {
      const test = FORBIDDEN_TOKENS[token];
      if (test && test(value)) problems.push({ concrete: trail, reason: token, value: describeValue(value) });
    }
  };
  const seen = new Set();
  for (const root of roots) {
    for (const node of resolvePath(payload, root)) {
      if (node.missing || seen.has(node.path)) continue;
      seen.add(node.path);
      walk(node.value, node.path);
    }
  }
  return problems;
}

// ---------------------------------------------------------------------------
// Relations: whether a number is RIGHT, not merely present
// ---------------------------------------------------------------------------
// Everything above this point answers "is the field there, and is it non-null".
// That is the whole vocabulary this lane had, and it is why a Weights page could
// report a herd daily gain of zero, and a vaccination board could report more
// doses than animals, without a single check going off.
//
// The bug history since 2026-08-01 is not made of missing fields. It is made of
// numbers that disagree with other numbers on the same screen:
//   b2d61bd54  the male and female halves of the Weights page came to 490 + 225
//              against a headline of 791 - 76 kids belonged to neither half.
//   99008fb21  the Weights page printed two different farm daily-gain figures at
//              once: 133 g/day in the headline, 200 g/day in the chart below it.
//   dad4b3c18  a 100-goat drive showed "Total 100" over tiles adding up to 97.
//   442ebce13  a combined drive multiplied its animal count by the number of
//              vaccines in it: 120 goats reported as 360.
//   e5b9a96ac  the Overdue tile read 0 while the grid underneath it was red.
//   51bae949a  the operators column came to 95 against its own tile of 199.
//   d085b2a4d  an animal that gained 2.3 kg in 8 days was reported at 764 g/day.
//
// A relation is a rule between figures the same response already carries. It is
// checked only when every figure it needs is really there and really a number;
// otherwise it records NOT-ATTEMPTED and says so, because a rule that quietly
// skips itself is how a screen ends up with a green tick and a wrong number.

const RELATION_KINDS = Object.freeze([
  "parts-sum-to-whole",
  "figures-agree",
  "at-most",
  "share-matches-its-parts",
  "list-matches-its-count",
  "within-range",
  "no-repeated-row",
  "populated-together",
  "average-matches-its-total",
  "rate-matches-the-movement",
  "never-more-than",
  "difference-matches"
]);

function numbersAt(payload, path) {
  const nodes = resolvePath(payload, path).filter((node) => !node.missing && !node.notAnArray);
  const values = [];
  for (const node of nodes) {
    if (typeof node.value === "number" && Number.isFinite(node.value)) values.push(node.value);
    else if (typeof node.value === "string" && node.value.trim() !== "" && Number.isFinite(Number(node.value))) values.push(Number(node.value));
    else return null; // a non-number anywhere means this relation has no honest inputs
  }
  return values;
}

function oneNumberAt(payload, path) {
  const values = numbersAt(payload, path);
  if (!values || values.length !== 1) return null;
  return values[0];
}

function keysAt(payload, path) {
  return resolvePath(payload, path)
    .filter((node) => !node.missing && !node.notAnArray && node.value !== null && node.value !== undefined)
    .map((node) => String(node.value).trim().toLowerCase())
    .filter((key) => key !== "");
}

/**
 * Checks every relation an endpoint declares.
 * Returns { findings, notAttempted } - never a bare pass. A relation whose inputs
 * are absent lands in notAttempted, so the run can say "this was not checked"
 * instead of counting it as proof.
 */
export function relationFindings(payload, relations = []) {
  const findings = [];
  const notAttempted = [];
  for (const rule of relations) {
    const name = rule.name ?? rule.kind;
    const skip = (why) => notAttempted.push({ name, why });
    if (!RELATION_KINDS.includes(rule.kind)) { skip(`unknown kind ${rule.kind}`); continue; }
    // A response that says it cut a list short is not disagreeing with itself; it is
    // telling the truth about a cap. Checking it anyway is how a check earns a
    // reputation for crying wolf, and a check nobody trusts is worse than no check.
    if (rule.unlessTruncated) {
      const flag = resolvePath(payload, rule.unlessTruncated).find((node) => !node.missing);
      if (flag?.value === true) { skip("the response says this list was cut short at a cap"); continue; }
    }
    const tol = Number(rule.tolerance ?? 0);

    if (rule.kind === "parts-sum-to-whole") {
      const parts = (rule.parts ?? []).map((path) => numbersAt(payload, path));
      const whole = oneNumberAt(payload, rule.whole);
      if (whole === null || parts.some((p) => p === null) || parts.every((p) => p.length === 0)) { skip("the figures it compares were not all present as numbers"); continue; }
      const total = parts.flat().reduce((a, b) => a + b, 0);
      if (Math.abs(total - whole) > tol) findings.push({ name, detail: `the parts come to ${total} against a whole of ${whole}` });
      continue;
    }

    if (rule.kind === "figures-agree") {
      const groups = (rule.figures ?? []).map((path) => numbersAt(payload, path));
      if (groups.some((g) => g === null) || groups.some((g) => g.length === 0)) { skip("one of the figures it compares was not present as a number"); continue; }
      const flat = groups.flat();
      const lo = Math.min(...flat), hi = Math.max(...flat);
      if (hi - lo > tol) findings.push({ name, detail: `the same figure is reported as ${lo} in one place and ${hi} in another` });
      continue;
    }

    if (rule.kind === "at-most") {
      const left = numbersAt(payload, rule.left);
      const right = oneNumberAt(payload, rule.right);
      if (left === null || right === null || left.length === 0) { skip("the figures it compares were not both present as numbers"); continue; }
      const worst = Math.max(...left);
      if (worst > right + tol) findings.push({ name, detail: `${worst} where at most ${right} is possible` });
      continue;
    }

    if (rule.kind === "share-matches-its-parts") {
      const part = oneNumberAt(payload, rule.part);
      const whole = oneNumberAt(payload, rule.whole);
      const share = oneNumberAt(payload, rule.share);
      if (part === null || whole === null || share === null) { skip("the figures it compares were not all present as numbers"); continue; }
      if (whole === 0) { skip("there is nothing to take a share of"); continue; }
      const expected = (part / whole) * (rule.scale ?? 100);
      if (Math.abs(expected - share) > (tol || 0.5)) findings.push({ name, detail: `reported as ${share} where the figures behind it give ${expected.toFixed(1)}` });
      continue;
    }

    if (rule.kind === "list-matches-its-count") {
      const rows = resolvePath(payload, rule.list).filter((node) => !node.missing && !node.notAnArray);
      const stated = oneNumberAt(payload, rule.count);
      if (stated === null) { skip("the stated total was not present as a number"); continue; }
      if (resolvePath(payload, rule.list).some((node) => node.notAnArray)) { skip("the list it counts was not present"); continue; }
      if (rule.onlyWhenNotPaged && stated > (rule.pageSize ?? Infinity)) { skip("the list is only one page of a longer list"); continue; }
      if (rows.length !== stated) findings.push({ name, detail: `${rows.length} listed against a stated total of ${stated}` });
      continue;
    }

    if (rule.kind === "within-range") {
      const values = numbersAt(payload, rule.path);
      if (values === null || values.length === 0) { skip("the figure it checks was not present as a number"); continue; }
      const offenders = values.filter((v) => (rule.ignoreZero && v === 0 ? false : v < rule.min || v > rule.max));
      if (offenders.length) findings.push({ name, detail: `${offenders[0]} where anything from ${rule.min} to ${rule.max} is believable` });
      continue;
    }

    // The key must identify the thing, not its NAME. Pen names repeat across farms on
    // purpose - there really are two Castro 1 and two Godel 2 - so keying a duplicate
    // check on the displayed name reports two different pens as one (2b0320963 fixed
    // exactly this on the vaccination board, by keying on the pen and not its label).
    if (rule.kind === "no-repeated-row") {
      const parts = Array.isArray(rule.key) ? rule.key : [rule.key];
      const rows = resolvePath(payload, rule.rows ?? "").filter((node) => !node.missing && !node.notAnArray);
      if (rows.length === 0) { skip("there were no rows to compare"); continue; }
      // A field a row legitimately omits (an unpartitioned pen has no partition) counts
      // as an empty part of the key, not as a reason to give up on the whole check.
      const keys = rows.map((node) => parts.map((path) => (keysAt(node.value, path)[0] ?? "")).join("|"));
      if (keys.every((key) => key.replaceAll("|", "") === "")) { skip("no row carried anything that identifies it"); continue; }
      const seen = new Set(); let repeated = null;
      for (const key of keys) { if (seen.has(key)) { repeated = key; break; } seen.add(key); }
      if (repeated) findings.push({ name, detail: `the same row is listed more than once` });
      continue;
    }

    // An average that is not its own total divided by its own head count is the
    // single most reported weighing defect: the pen reads one number and the rows
    // behind it say another.
    if (rule.kind === "average-matches-its-total") {
      const rows = resolvePath(payload, rule.each ?? "").filter((node) => !node.missing && !node.notAnArray);
      const scope = rule.each ? rows.map((node) => node.value) : [payload];
      if (scope.length === 0) { skip("there were no rows to check"); continue; }
      let attempted = 0;
      for (const item of scope) {
        const total = oneNumberAt(item, rule.total);
        const count = oneNumberAt(item, rule.count);
        const average = oneNumberAt(item, rule.average);
        if (total === null || count === null || average === null || count <= 0) continue;
        attempted += 1;
        const expected = total / count;
        if (Math.abs(expected - average) > (tol || 0.5)) {
          findings.push({ name, detail: `an average of ${average} where the total and the head count behind it give ${expected.toFixed(1)}` });
          break;
        }
      }
      if (attempted === 0) skip("no row carried a total, a head count and an average together");
      continue;
    }

    // A daily gain that is not the weight actually gained divided by the days it took.
    // d085b2a4d: a kid that gained 2.3 kg in 8 days was reported at 764 g/day, where
    // the movement it is built on gives 287.
    if (rule.kind === "rate-matches-the-movement") {
      const rows = resolvePath(payload, rule.each ?? "").filter((node) => !node.missing && !node.notAnArray);
      if (rows.length === 0) { skip("there were no rows to check"); continue; }
      let attempted = 0;
      for (const node of rows) {
        const item = node.value;
        const from = oneNumberAt(item, rule.from);
        const to = oneNumberAt(item, rule.to);
        const days = oneNumberAt(item, rule.days);
        const rate = oneNumberAt(item, rule.rate);
        if (from === null || to === null || days === null || rate === null || days <= 0) continue;
        attempted += 1;
        const expected = ((to - from) * (rule.scale ?? 1)) / days;
        if (Math.abs(expected - rate) > (tol || 1)) {
          findings.push({ name, detail: `a daily gain of ${rate} where the weights and the days behind it give ${expected.toFixed(1)}` });
          break;
        }
      }
      if (attempted === 0) skip("no row carried both weights, the days between them and a rate");
      continue;
    }

    // A figure that counts a subset can never exceed the figure that counts the set.
    if (rule.kind === "never-more-than") {
      const subset = oneNumberAt(payload, rule.subset);
      const set = oneNumberAt(payload, rule.set);
      if (subset === null || set === null) { skip("the two figures it compares were not both present as numbers"); continue; }
      if (subset > set + tol) findings.push({ name, detail: `${subset} counted inside a group of ${set}` });
      continue;
    }

    // Remaining work is what was scheduled minus what actually closed. 51bae949a: the
    // board took it from proof arrival instead, so a drive with every video in and
    // almost nothing closed rendered as finished.
    if (rule.kind === "difference-matches") {
      const rows = rule.each ? resolvePath(payload, rule.each).filter((node) => !node.missing && !node.notAnArray) : [{ value: payload }];
      if (rows.length === 0) { skip("there were no rows to check"); continue; }
      let attempted = 0;
      for (const node of rows) {
        const item = node.value;
        const from = oneNumberAt(item, rule.from);
        const less = oneNumberAt(item, rule.less);
        const result = oneNumberAt(item, rule.result);
        if (from === null || less === null || result === null) continue;
        attempted += 1;
        const expected = rule.flooredAtZero ? Math.max(0, from - less) : from - less;
        if (Math.abs(expected - result) > tol) {
          findings.push({ name, detail: `${result} where the two figures behind it give ${expected}` });
          break;
        }
      }
      if (attempted === 0) skip("no row carried all three figures together");
      continue;
    }

    if (rule.kind === "populated-together") {
      const lead = numbersAt(payload, rule.when);
      if (lead === null || lead.length === 0) { skip("the figure it keys off was not present as a number"); continue; }
      if (!lead.some((v) => v > 0)) { skip("there was nothing on this screen to be consistent about"); continue; }
      const rows = resolvePath(payload, rule.expect).filter((node) => !node.missing && !node.notAnArray);
      if (rows.length === 0) findings.push({ name, detail: `one half of the screen has figures and the other half has none` });
      continue;
    }
  }
  return { findings, notAttempted };
}

function describeValue(value) {
  if (typeof value === "number" && Number.isNaN(value)) return "NaN";
  if (value === undefined) return "undefined";
  return redactBody(JSON.stringify(value) ?? String(value)).slice(0, 80);
}

// lib/redact.mjs cannot see JSON-quoted secrets: its value class excludes the double
// quote, so {"token":"..."} matches nothing. Response bodies are JSON, so redact by field
// name here as well before anything is stored.
const JSON_SECRET_FIELDS = /("(?:[a-z_-]*(?:token|secret|password|passwd|api[_-]?key|authorization|cookie|credential|signature|bearer)[a-z_-]*)"\s*:\s*)"(?:[^"\\]|\\.)*"/gi;

export function redactBody(value) {
  return redactText(String(value ?? "").replace(JSON_SECRET_FIELDS, '$1"[REDACTED]"'));
}

// ---------------------------------------------------------------------------
// Latency
// ---------------------------------------------------------------------------

// Nearest-rank percentile, identical to tools/perf/api-latency-gate.mjs. With the default
// 20 samples this matches the gate's index exactly; check-api-contracts.test.mjs pins the
// function against the gate's own source so the two cannot drift.
export function percentile(sorted, pct) {
  if (!sorted.length) return 0;
  if (pct <= 0) return sorted[0];
  if (pct >= 100) return sorted[sorted.length - 1];
  const idx = Math.min(sorted.length - 1, Math.max(0, Math.ceil((pct / 100) * sorted.length) - 1));
  return Number(sorted[idx].toFixed(1));
}

let cachedConfig = null;
function policy() {
  if (!cachedConfig) cachedConfig = JSON.parse(readFileSync(CONFIG_PATH, "utf8"));
  return cachedConfig.apiLatencyPolicy ?? {};
}

// config.json apiLatencyPolicy.allowLongRunningPatterns names the paths that are allowed to
// take their time (uploads, imports, exports). They are exempt from the latency finding.
export function isLongRunningExempt(requestPath, patterns = policy().allowLongRunningPatterns ?? []) {
  return patterns.some((pattern) => String(requestPath).includes(pattern));
}

export function latencyBudgetFor(entry) {
  const ceiling = Number(policy().hotPathP95Ms ?? API_LATENCY_POLICY_MS.p95_ms);
  const budget = Number(entry?.latencyBudgetMs ?? ceiling);
  if (!Number.isFinite(budget) || budget <= 0) return ceiling;
  return Math.min(budget, ceiling);
}

// ---------------------------------------------------------------------------
// Catalogue
// ---------------------------------------------------------------------------

export function validateEntry(entry, index = 0) {
  const where = entry?.name ?? `entry[${index}]`;
  assertGetOnly(entry);
  for (const field of ["name", "path", "page", "humanFailure"]) {
    if (typeof entry?.[field] !== "string" || !entry[field].trim()) {
      throw new Error(`api-contract ${where} is missing ${field}`);
    }
  }
  if (!Array.isArray(entry.requiredFields) || entry.requiredFields.length === 0) {
    throw new Error(`api-contract ${where} must declare at least one required field`);
  }
  if (!Array.isArray(entry.sourceCommits) || entry.sourceCommits.length === 0) {
    throw new Error(`api-contract ${where} must record the commits it comes from`);
  }
  for (const rule of entry.relations ?? []) {
    const rname = `${where} relation ${rule?.name ?? rule?.kind ?? "<unnamed>"}`;
    if (!RELATION_KINDS.includes(rule?.kind)) throw new Error(`${rname} has no known kind`);
    if (!rule?.name) throw new Error(`${rname} has no name`);
    // A relation with no shipped bug behind it is a guess, and a guess is how false
    // alarms get in. Every one names the commits whose fix it would catch the loss of.
    if (!Array.isArray(rule.sourceCommits) || rule.sourceCommits.length === 0) {
      throw new Error(`${rname} names no commit it comes from`);
    }
    assertHumanSentence(rname, rule.humanFailure);
  }
  return entry;
}

export function loadCatalogue(file = CATALOGUE_PATH) {
  const parsed = JSON.parse(readFileSync(file, "utf8"));
  const entries = Array.isArray(parsed) ? parsed : parsed.endpoints;
  if (!Array.isArray(entries) || entries.length === 0) throw new Error(`api contract catalogue has no endpoints: ${file}`);
  entries.forEach(validateEntry);
  // Budgets must survive the same hard ceiling the shared latency policy enforces.
  normalizeApiLatencyEndpoints(entries.map((entry) => ({
    name: entry.name,
    path: entry.path,
    p95_ms: latencyBudgetFor(entry),
    p90_ms: Math.min(latencyBudgetFor(entry), API_LATENCY_POLICY_MS.p90_ms),
    p99_ms: latencyBudgetFor(entry),
  })));
  return { ...(Array.isArray(parsed) ? {} : parsed), endpoints: entries };
}

// The history miner writes per-commit derived checks here. Its rows carry checkId/checkName,
// not a full catalogue entry, so they are NORMALISED and then put through exactly the same
// gate as hand-written entries. A row that cannot be normalised is parked with a reason and
// never reaches a finding — an un-normalised row would print "undefined" in Slack.
const TOKEN_ALIASES = { "": "empty", "Invalid Date": null };
function forbiddenFromTokens(row) {
  const tokens = (Array.isArray(row?.forbidden) ? row.forbidden : [])
    .map((token) => (token in TOKEN_ALIASES ? TOKEN_ALIASES[token] : token))
    .filter((token) => token && DEFAULT_FORBIDDEN.includes(token));
  if (!tokens.length) return [];
  return (Array.isArray(row?.required) ? row.required : []).map((field) => ({ path: field, disallow: tokens }));
}

export const basePath = (value) => String(value ?? "").split("?")[0];

// History-mined rows describe fields derived from COMMIT TEXT, not from a response anyone
// has seen. Running them against production produced 1184 findings from 17 endpoints, all
// of them expectations production never promised, against 12 real findings from the 57
// production-verified catalogue entries. So they are parked by default and only run when
// somebody explicitly asks, while they are being grounded one at a time.
export function normalizeLaneChecks(rows, { knownNames = new Set(), knownPaths = new Set(), includeUngrounded = false } = {}) {
  const accepted = [];
  const parked = [];
  for (const [index, row] of (Array.isArray(rows) ? rows : []).entries()) {
    const name = String(row?.id ?? row?.name ?? row?.checkId ?? `lane-check[${index}]`);
    const method = String(row?.method ?? "GET").toUpperCase();
    if (method !== "GET") {
      parked.push({ name, reason: "not a GET; this lane is read-only" });
      continue;
    }
    // Dedupe by PATH as well as name: the miner names checks differently, and its field
    // expectations are derived from commit text rather than from a real response. Where the
    // catalogue already covers an endpoint, the production-verified entry wins and the
    // history row is parked (its commits are already folded into the catalogue entry).
    if (knownNames.has(name) || knownPaths.has(basePath(row?.path ?? row?.endpoint))) {
      parked.push({ name, reason: "already covered by a production-verified catalogue entry" });
      continue;
    }
    // The miner's schema is {id, name, method, path, required, forbidden[], latencyBudget,
    // failureSentence, sourceShas}; map it onto a full catalogue entry.
    const entry = {
      name,
      method: "GET",
      path: row?.path ?? row?.endpoint ?? null,
      page: row?.page ?? row?.screen ?? row?.name ?? null,
      pageUrl: row?.pageUrl ?? null,
      humanFailure: row?.humanFailure ?? row?.failureSentence ?? null,
      requiredFields: Array.isArray(row?.requiredFields) ? row.requiredFields : (Array.isArray(row?.required) ? row.required : []),
      // The miner's `forbidden` is a list of disallowed TOKENS, applied to the fields it
      // declares required — not a list of paths.
      forbiddenValues: Array.isArray(row?.forbiddenValues) ? row.forbiddenValues : forbiddenFromTokens(row),
      latencyBudgetMs: row?.latencyBudgetMs ?? row?.latencyBudget?.p95Ms ?? undefined,
      sourceCommits: Array.isArray(row?.sourceCommits) ? row.sourceCommits : (Array.isArray(row?.sourceShas) ? row.sourceShas : []),
      derivedFromHistory: true,
    };
    try {
      validateEntry(entry, index);
      if (!includeUngrounded) {
        parked.push({ name, reason: "field expectations are derived from commit text, not from a production response; set GOATOS_DASHBOARD_API_CONTRACTS_INCLUDE_HISTORY=1 to run it anyway" });
        continue;
      }
      accepted.push(entry);
    } catch (error) {
      parked.push({ name, reason: redactText(error?.message ?? String(error)) });
    }
  }
  return { accepted, parked };
}

export function loadLaneChecks(file = LANE_CHECKS_PATH, options = {}) {
  if (!existsSync(file)) return { accepted: [], parked: [], present: false };
  try {
    const parsed = JSON.parse(readFileSync(file, "utf8"));
    const rows = Array.isArray(parsed)
      ? parsed
      : (parsed.lanes?.lane3?.checks ?? parsed.lane3?.checks ?? parsed.lane3 ?? parsed.checks ?? parsed.endpoints ?? []);
    return { ...normalizeLaneChecks(rows, options), present: true };
  } catch (error) {
    return { accepted: [], parked: [{ name: path.basename(file), reason: `could not be read: ${redactText(error?.message ?? String(error))}` }], present: true };
  }
}

// ---------------------------------------------------------------------------
// Sweep
// ---------------------------------------------------------------------------

// The farm runs on IST. A "{today}" filter resolved in UTC is a day behind for the first
// 5.5 hours after midnight IST, which would check yesterday's screen.
const IST_OFFSET_MS = 5.5 * 60 * 60 * 1000;

export function expandPath(value, now = new Date()) {
  const ist = new Date(now.getTime() + IST_OFFSET_MS);
  const fmt = (date) => date.toISOString().slice(0, 10);
  const shift = (days) => new Date(ist.getTime() + days * 86400000);
  const monthStart = new Date(Date.UTC(ist.getUTCFullYear(), ist.getUTCMonth(), 1));
  const monthEnd = new Date(Date.UTC(ist.getUTCFullYear(), ist.getUTCMonth() + 1, 0));
  return String(value)
    .replaceAll("{today_start_month}", fmt(monthStart))
    .replaceAll("{today_end_month}", fmt(monthEnd))
    .replaceAll("{today_minus_30}", fmt(shift(-30)))
    .replaceAll("{today_plus_30}", fmt(shift(30)))
    .replaceAll("{today_plus_7}", fmt(shift(7)))
    .replaceAll("{today_plus_1}", fmt(shift(1)))
    .replaceAll("{today}", fmt(ist));
}

// One endpoint: warm it, sample it, then judge. Never throws for a product failure — a
// broken endpoint is a finding and the sweep must reach the next one.
export async function checkEntry(entry, context) {
  const {
    baseUrl, headers, samples = DEFAULT_SAMPLES, warmup = DEFAULT_WARMUP,
    timeoutMs = 20000, fetchImpl = fetch, now = new Date(),
  } = context;
  assertGetOnly(entry);
  const requestPath = expandPath(entry.path, now);
  const budgetMs = latencyBudgetFor(entry);
  const findings = [];
  const relationsNotAttempted = [];
  const statuses = [];
  const durations = [];
  let lastPayload;
  let lastBodyText = "";
  let transportError = null;
  let redirected = false;

  const once = async (measured) => {
    const started = performance.now();
    try {
      const { response, hops } = await get(baseUrl, requestPath, { headers, timeoutMs, fetchImpl });
      const elapsed = performance.now() - started;
      if (hops > 0) redirected = true;
      const text = await response.text();
      if (measured) {
        durations.push(elapsed);
        statuses.push(response.status);
        lastBodyText = text;
        if (response.ok) {
          try {
            lastPayload = JSON.parse(text);
          } catch {
            lastPayload = undefined;
            if (!findings.some((item) => item.code === "invalid-json")) {
              findings.push(finding(entry, "invalid-json", "the response was not readable as data", requestPath, text));
            }
          }
        }
      }
    } catch (error) {
      if (measured) transportError = redactText(error?.message ?? String(error));
    }
  };

  for (let i = 0; i < warmup; i += 1) await once(false);
  for (let i = 0; i < samples; i += 1) await once(true);

  const worstStatus = statuses.length ? Math.max(...statuses) : 0;
  if (statuses.length === 0) {
    findings.push(finding(entry, "unreachable", `the request did not complete: ${transportError ?? "no response"}`, requestPath, ""));
  } else if (statuses.some((status) => status >= 500)) {
    findings.push(finding(entry, "server-error", `the server answered HTTP ${worstStatus}`, requestPath, lastBodyText));
  } else if (statuses.some((status) => status === 401 || status === 403)) {
    findings.push(finding(entry, "not-authorised", `the server answered HTTP ${worstStatus}`, requestPath, lastBodyText));
  } else if (statuses.some((status) => status >= 400)) {
    findings.push(finding(entry, "client-error", `the server answered HTTP ${worstStatus}`, requestPath, lastBodyText));
  }
  if (redirected) {
    findings.push(finding(entry, "redirected", "the server sent the app somewhere else to get this data", requestPath, ""));
  }

  if (lastPayload !== undefined) {
    for (const problem of missingRequiredFields(lastPayload, entry.requiredFields ?? [])) {
      findings.push(finding(entry, "required-field-missing", `${problem.concrete} is ${problem.reason}`, requestPath, ""));
    }
    for (const problem of forbiddenValueFindings(lastPayload, entry.forbiddenValues ?? [])) {
      findings.push(finding(entry, problem.reason === "unknown-enum" ? "unknown-enum" : "forbidden-value", `${problem.concrete} is ${problem.reason} (${problem.value})`, requestPath, ""));
    }
    for (const problem of declaredSubtreeForbiddenFindings(lastPayload, entry).slice(0, 5)) {
      findings.push(finding(entry, "forbidden-value", `${problem.concrete} is ${problem.reason}`, requestPath, ""));
    }
    const relations = relationFindings(lastPayload, entry.relations ?? []);
    for (const problem of relations.findings) {
      // A relation carries its own sentence: the endpoint-wide one says "the page did not
      // load", which is not what happened. The page loaded and lied.
      findings.push({
        ...finding(entry, "figures-disagree", problem.detail, requestPath, ""),
        humanFailure: (entry.relations ?? []).find((rule) => (rule.name ?? rule.kind) === problem.name)?.humanFailure ?? entry.humanFailure,
      });
    }
    relationsNotAttempted.push(...relations.notAttempted);
  }

  const sorted = [...durations].sort((a, b) => a - b);
  const p95 = percentile(sorted, 95);
  const p50 = percentile(sorted, 50);
  const exempt = isLongRunningExempt(requestPath);
  const sustainedlySlow = sorted.length > 0 && !exempt && p95 > budgetMs && p50 > budgetMs;
  if (sustainedlySlow && !findings.some((item) => ["unreachable", "server-error", "not-authorised", "client-error"].includes(item.code))) {
    findings.push(finding(entry, "slow", `half of the requests took longer than ${Math.round(p50)}ms against a ${budgetMs}ms budget (p95 ${Math.round(p95)}ms)`, requestPath, "", { p95Ms: p95, p50Ms: p50, budgetMs }));
  }

  return {
    name: entry.name,
    page: entry.page,
    pageUrl: entry.pageUrl ?? null,
    method: "GET",
    path: requestPath,
    derivedFromHistory: entry.derivedFromHistory === true,
    warmup,
    samples: sorted.length,
    statuses,
    p50_ms: p50,
    p95_ms: p95,
    budget_ms: budgetMs,
    latency_exempt: exempt,
    slow_rule: SLOW_RULE,
    sample_ms: sorted.map((value) => Number(value.toFixed(1))),
    passed: findings.length === 0,
    // A relation that could not be checked is reported, never counted as proof.
    // Silence here is how a screen earns a green tick it did not deserve.
    relations_checked: (entry.relations ?? []).length - relationsNotAttempted.length,
    relations_declared: (entry.relations ?? []).length,
    relations_not_attempted: relationsNotAttempted,
    findings,
  };
}

function finding(entry, code, detail, requestPath, bodyFragment, extra = {}) {
  return {
    check: "api-contract",
    code,
    name: entry.name,
    page: entry.page,
    pageUrl: entry.pageUrl ?? null,
    // The only thing Slack is ever allowed to print: no path, no field, no status, no code.
    humanFailure: entry.humanFailure,
    // Everything below stays in the HTML report / receipt artifact.
    detail: redactBody(detail),
    path: requestPath,
    bodyFragment: bodyFragment ? redactBody(String(bodyFragment)).slice(0, 600) : "",
    ...extra,
  };
}

// ---------------------------------------------------------------------------
// CLI
// ---------------------------------------------------------------------------

// Non-zero whenever anything was found. The layer, and therefore the Slack alert, hangs
// off this one number.
export function exitCodeFor(report) {
  const results = report?.results ?? [];
  if (results.length === 0) return 1;
  return results.some((result) => !result.passed) || (report.findings ?? []).length > 0 ? 1 : 0;
}

function parseArgs(raw) {
  const parsed = {};
  for (let i = 0; i < raw.length; i += 1) {
    const arg = raw[i];
    if (arg === "--self-test") parsed.selfTest = true;
    else if (arg === "--out") parsed.out = raw[++i];
    else if (arg === "--only") parsed.only = raw[++i];
    else if (arg === "--samples") parsed.samples = Number(raw[++i]);
    else if (arg === "--warmup") parsed.warmup = Number(raw[++i]);
    else throw new Error(`unknown argument: ${arg}`);
  }
  return parsed;
}

export async function main(argv) {
  const args = parseArgs(argv);
  if (args.selfTest) return selfTest();

  const catalogue = loadCatalogue();
  const laneChecks = loadLaneChecks(LANE_CHECKS_PATH, {
    knownNames: new Set(catalogue.endpoints.map((entry) => entry.name)),
    knownPaths: new Set(catalogue.endpoints.map((entry) => basePath(entry.path))),
    includeUngrounded: ["1", "true", "yes"].includes(String(process.env.GOATOS_DASHBOARD_API_CONTRACTS_INCLUDE_HISTORY ?? "").toLowerCase()),
  });
  const all = [...catalogue.endpoints, ...laneChecks.accepted];
  const entries = args.only ? all.filter((entry) => entry.name === args.only) : all;
  if (entries.length === 0) throw new Error(`no api contract entries matched --only ${args.only}`);

  const baseUrlRaw = String(process.env.GOATOS_API_BASE_URL ?? "").replace(/\/+$/, "");
  const bearer = String(process.env.GOATOS_BEARER_TOKEN ?? "").trim();
  const tenant = String(process.env.GOATOS_TENANT_ID ?? "").trim();
  const missing = [
    !baseUrlRaw ? "GOATOS_API_BASE_URL" : null,
    !bearer ? "GOATOS_BEARER_TOKEN" : null,
    !tenant ? "GOATOS_TENANT_ID" : null,
  ].filter(Boolean);
  if (missing.length) throw new Error(`auth_blocked: missing api contract env: ${missing.join(", ")}`);
  assertProductionApiUrl(baseUrlRaw);

  const headers = { Accept: "application/json", "X-GoatOS-Tenant-ID": tenant, Authorization: `Bearer ${bearer}` };
  const samples = Number.isFinite(args.samples) && args.samples > 0 ? args.samples : DEFAULT_SAMPLES;
  const warmup = Number.isFinite(args.warmup) && args.warmup >= 0 ? args.warmup : DEFAULT_WARMUP;

  const startedAt = new Date().toISOString();
  const results = [];
  for (const entry of entries) {
    try {
      results.push(await checkEntry(entry, { baseUrl: baseUrlRaw, headers, samples, warmup }));
    } catch (error) {
      // A guard refusal or a programming error must not end the sweep.
      results.push({
        name: entry.name, page: entry.page, pageUrl: entry.pageUrl ?? null, method: "GET", path: entry.path,
        warmup, samples: 0, statuses: [], p50_ms: 0, p95_ms: 0, budget_ms: latencyBudgetFor(entry),
        passed: false,
        findings: [finding(entry, "check-error", redactText(error?.message ?? String(error)), entry.path, "")],
      });
    }
  }

  const findings = results.flatMap((result) => result.findings);
  const report = {
    schema_version: "1.0.0",
    check: "api-contracts",
    started_at: startedAt,
    finished_at: new Date().toISOString(),
    base_url: baseUrlRaw,
    catalogue_entries: catalogue.endpoints.length,
    history_derived_entries: laneChecks.accepted.length,
    parked: laneChecks.parked,
    samples_per_endpoint: samples,
    warmup_per_endpoint: warmup,
    slow_rule: SLOW_RULE,
    passed: findings.length === 0,
    results,
    findings,
  };

  if (args.out) {
    const text = `${JSON.stringify(report, null, 2)}\n`;
    if (containsUnredactedSecret(text)) throw new Error("refusing to write an api contract report containing an unredacted secret");
    const outFile = path.resolve(args.out);
    mkdirSync(path.dirname(outFile), { recursive: true });
    writeFileSync(outFile, text);
    // Paths, status codes and response fragments belong in the report, never in Slack.
    const reportFile = path.join(path.dirname(outFile), "api-contracts-report.html");
    writeFileSync(reportFile, reportHtml(findingsForReport(null, path.dirname(outFile)), { checked: results.length, parked: laneChecks.parked }));
  }

  // run.mjs's runNode() throws only on a non-zero exit, and that throw is what fails the
  // api-contracts layer and makes Slack fire. If this ever returns 0 with findings present,
  // every finding below is silently swallowed. exitCodeFor() is unit-tested for that.
  const bad = results.filter((result) => !result.passed);
  if (laneChecks.parked.length) console.log(`api contract sweep: ${laneChecks.parked.length} history-derived check(s) parked`);
  process.exitCode = exitCodeFor(report);
  if (bad.length) {
    console.error(`api contract sweep: ${bad.length} of ${results.length} endpoint(s) failed`);
    for (const result of bad.slice(0, 20)) console.error(`- ${result.page}: ${result.findings.map((item) => item.detail).join("; ")}`);
    return report;
  }
  console.log(`api contract sweep: ${results.length} endpoint(s) passed shape + latency checks`);
  return report;
}

function selfTest() {
  const catalogue = loadCatalogue();
  const names = catalogue.endpoints.map((entry) => entry.name);
  if (new Set(names).size !== names.length) throw new Error("self-test: catalogue entry names must be unique");

  let refusedPost = false;
  try { assertGetOnly({ name: "x", method: "POST", path: "/x" }); } catch { refusedPost = true; }
  if (!refusedPost) throw new Error("self-test: non-GET methods must be refused");

  for (const bad of ["http://api.goatos.mesha.sg", "https://api.goatos.mesha.sg.evil.test", "https://evil.test/?u=api.goatos.mesha.sg", "https://user:pw@api.goatos.mesha.sg"]) {
    let refused = false;
    try { assertProductionApiUrl(bad); } catch { refused = true; }
    if (!refused) throw new Error(`self-test: base URL must be refused: ${bad}`);
  }
  assertProductionApiUrl("https://api.goatos.mesha.sg");

  // Off-host paths must be refused outright.
  for (const escape of ["//evil.test/x", "https://evil.test/x", "http://api.goatos.mesha.sg/x"]) {
    let refused = false;
    try { resolveRequestUrl("https://api.goatos.mesha.sg", escape); } catch { refused = true; }
    if (!refused) throw new Error(`self-test: resolved request path must be refused: ${escape}`);
  }
  // These two escape a CONCATENATED prefix (they turn the allowlisted host into userinfo,
  // or into a prefix of the attacker's host). Resolving against the base neutralises them
  // into ordinary paths; assert that, so the resolution is never swapped back for concat.
  for (const neutralised of ["@evil.test/x", ".evil.test/x"]) {
    const resolved = resolveRequestUrl("https://api.goatos.mesha.sg", neutralised);
    if (resolved.hostname !== "api.goatos.mesha.sg") throw new Error(`self-test: ${neutralised} escaped the allowlist`);
    if (new URL(`https://api.goatos.mesha.sg${neutralised}`).hostname === "api.goatos.mesha.sg") {
      throw new Error(`self-test: ${neutralised} is no longer a concatenation escape; the guard comment is stale`);
    }
  }

  if (missingRequiredFields({ a: { b: null } }, ["a.b"]).length !== 1) throw new Error("self-test: null required field must be detected");
  if (forbiddenValueFindings({ rows: [{ v: "NaN" }] }, [{ path: "rows[].v" }]).length !== 1) throw new Error("self-test: NaN must be detected");
  if (declaredSubtreeForbiddenFindings({ a: { b: [{ c: Number.NaN }] } }, { requiredFields: ["a"] }).length !== 1) throw new Error("self-test: nested NaN under a declared field must be detected");
  if (declaredSubtreeForbiddenFindings({ undeclared: { c: Number.NaN } }, { requiredFields: ["a"] }).length !== 0) throw new Error("self-test: a NaN no screen renders must not be a finding");
  if (percentile([1, 2, 3, 4, 5, 6, 7, 8, 9, 10], 95) !== 10) throw new Error("self-test: p95 computation drifted");
  if (!redactBody('{"token":"abc.def.ghi"}').includes("[REDACTED]")) throw new Error("self-test: JSON-quoted secrets must be redacted");

  for (const entry of catalogue.endpoints) assertHumanSentence(entry.name, entry.humanFailure);
  console.log(`api contract sweep: self-test passed (${catalogue.endpoints.length} catalogue entries)`);
  return null;
}

// Slack prints humanFailure verbatim, so it may not carry a URL path, a field path, a
// status code or a check code.
export function assertHumanSentence(name, sentence) {
  const text = String(sentence ?? "");
  if (!text.trim()) throw new Error(`humanFailure for ${name} is empty`);
  if (text.includes("/")) throw new Error(`humanFailure for ${name} contains a URL path`);
  if (/\b(HTTP|[45]\d\d)\b/.test(text)) throw new Error(`humanFailure for ${name} contains a status code`);
  if (/[a-z_]+\.[a-z_]+/i.test(text.replace(/\.(\s|$)/g, "$1"))) throw new Error(`humanFailure for ${name} contains a field path`);
  // snake_case is the internal-code shape. Hyphenated English ("age-group") is fine.
  if (/\b[a-z]+_[a-z_]+\b/.test(text)) throw new Error(`humanFailure for ${name} contains a check code or raw identifier`);
  return true;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main(process.argv.slice(2)).catch((error) => {
    console.error(redactText(error?.message ?? String(error)));
    process.exit(1);
  });
}
