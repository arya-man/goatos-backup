import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  assertGetOnly,
  assertHumanSentence,
  assertProductionApiUrl,
  basePath,
  checkEntry,
  declaredSubtreeForbiddenFindings,
  expandPath,
  forbiddenValueFindings,
  get,
  isLongRunningExempt,
  latencyBudgetFor,
  loadCatalogue,
  missingRequiredFields,
  normalizeLaneChecks,
  percentile,
  PRODUCTION_API_HOSTS,
  redactBody,
  resolvePath,
  resolveRequestUrl,
} from "./check-api-contracts.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "../..");
const BASE = "https://api.goatos.mesha.sg";

// --------------------------------------------------------------------------
// GET-only guard
// --------------------------------------------------------------------------

test("every non-GET method is refused", () => {
  for (const method of ["POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "get ", "", undefined]) {
    assert.throws(() => assertGetOnly({ name: "x", method, path: "/x" }), /may only issue GET/);
  }
  assert.equal(assertGetOnly({ name: "x", method: "GET", path: "/x" }), "GET");
  assert.equal(assertGetOnly({ name: "x", method: "get", path: "/x" }), "GET");
});

test("checkEntry refuses a non-GET entry rather than issuing the request", async () => {
  let called = false;
  await assert.rejects(
    () => checkEntry({ name: "x", method: "POST", path: "/x", page: "P", humanFailure: "h" },
      { baseUrl: BASE, headers: {}, fetchImpl: () => { called = true; } }),
    /may only issue GET/,
  );
  assert.equal(called, false, "nothing may be sent for a non-GET entry");
});

test("the lane has exactly one fetch call site", () => {
  // Structural invariant: GET-only is enforced by construction, not by code review.
  const files = [
    path.join(here, "check-api-contracts.mjs"),
    ...readdirSync(path.join(here, "lib/finding-kinds")).map((name) => path.join(here, "lib/finding-kinds", name)),
  ];
  let sites = 0;
  for (const file of files) {
    for (const line of readFileSync(file, "utf8").split("\n")) {
      if (/^\s*(\/\/|\*)/.test(line)) continue;
      // fetchImpl defaults to fetch in exactly one signature; the call itself is fetchImpl(.
      if (/\bfetchImpl\(/.test(line)) sites += 1;
      assert.ok(!/[^a-zA-Z.]fetch\(/.test(line), `raw fetch( call outside the choke point in ${path.basename(file)}: ${line.trim()}`);
    }
  }
  assert.equal(sites, 1, "fetch must be called from exactly one place");
});

test("get() hard-codes the method and has no method parameter", async () => {
  const seen = [];
  await get(BASE, "/x", { headers: {}, fetchImpl: (url, init) => { seen.push(init); return stubResponse(200, "{}"); } });
  assert.equal(seen[0].method, "GET");
  assert.equal(seen[0].redirect, "manual");
});

// --------------------------------------------------------------------------
// Host allowlist
// --------------------------------------------------------------------------

test("the allowlist matches run.mjs runProductionSmoke exactly", () => {
  const runner = readFileSync(path.join(here, "run.mjs"), "utf8");
  const listed = runner.match(/\["api\.goatos\.mesha\.sg"[^\]]*\]/)?.[0];
  assert.ok(listed, "run.mjs must still carry a production host allowlist");
  assert.deepEqual(JSON.parse(listed), [...PRODUCTION_API_HOSTS]);
});

test("base URLs outside the allowlist are refused", () => {
  for (const bad of [
    "http://api.goatos.mesha.sg",                 // not https
    "https://api.goatos.mesha.sg.evil.test",      // suffix lookalike
    "https://evil.test/?u=api.goatos.mesha.sg",   // substring in the query
    "https://api-goatos.mesha.sg",                // near miss
    "https://user:pw@api.goatos.mesha.sg",        // credentials in the URL
    "https://sub.api.goatos.mesha.sg",            // subdomain is not the host
    "not a url",
    "",
  ]) {
    assert.throws(() => assertProductionApiUrl(bad), /refuses/, `should refuse ${bad}`);
  }
  for (const good of PRODUCTION_API_HOSTS) assert.ok(assertProductionApiUrl(`https://${good}/x`));
});

test("the RESOLVED request url is re-checked, not the input string", () => {
  // Off-host paths are refused outright.
  for (const escape of ["//evil.test/x", "https://evil.test/x", "http://api.goatos.mesha.sg/x"]) {
    assert.throws(() => resolveRequestUrl(BASE, escape), /refuses/, `should refuse ${escape}`);
  }
  // These escape a CONCATENATED prefix but are harmless once resolved against the base.
  // Both halves are asserted so the resolution can never be swapped back for concatenation.
  for (const neutralised of ["@evil.test/x", ".evil.test/x"]) {
    assert.notEqual(new URL(`${BASE}${neutralised}`).hostname, "api.goatos.mesha.sg",
      `${neutralised} must still be a concatenation escape, or this test is pointless`);
    assert.equal(resolveRequestUrl(BASE, neutralised).hostname, "api.goatos.mesha.sg");
  }
  assert.equal(resolveRequestUrl(BASE, "/herd").href, `${BASE}/herd`);
});

test("a redirect off the allowlist is refused and never followed", async () => {
  const calls = [];
  const fetchImpl = (url) => {
    calls.push(url);
    if (calls.length === 1) return stubResponse(302, "", { location: "https://evil.test/steal" });
    return stubResponse(200, "{}");
  };
  await assert.rejects(() => get(BASE, "/x", { headers: {}, fetchImpl }), /refuses non-production API URL/);
  assert.equal(calls.length, 1, "the off-allowlist hop must never be requested");
});

test("a redirect that stays on the allowlist is followed and capped", async () => {
  let n = 0;
  const onHost = () => { n += 1; return stubResponse(302, "", { location: `${BASE}/next${n}` }); };
  await assert.rejects(() => get(BASE, "/x", { headers: {}, fetchImpl: onHost }), /more than 3 redirects/);
  assert.ok(n <= 5);
});

// --------------------------------------------------------------------------
// Required fields
// --------------------------------------------------------------------------

test("required-field detection finds missing and null, and tolerates empty arrays", () => {
  const payload = { summary: { total: 0, name: null }, rows: [], nested: { a: { b: 1 } } };
  assert.deepEqual(missingRequiredFields(payload, ["summary.total", "rows", "nested.a.b"]), []);
  assert.deepEqual(missingRequiredFields(payload, ["summary.name"]).map((p) => p.reason), ["null"]);
  assert.deepEqual(missingRequiredFields(payload, ["summary.missing"]).map((p) => p.reason), ["missing"]);
  assert.deepEqual(missingRequiredFields(payload, ["nope.deep"]).map((p) => p.reason), ["missing"]);
  // false and 0 are real values, not absences.
  assert.deepEqual(missingRequiredFields({ a: false, b: 0, c: "" }, ["a", "b", "c"]), []);
});

test("required fields reach into every element of an array", () => {
  const payload = { rows: [{ name: "a" }, { name: null }, {}] };
  const problems = missingRequiredFields(payload, ["rows[].name"]);
  assert.equal(problems.length, 2);
  assert.deepEqual(problems.map((p) => p.concrete), ["rows[1].name", "rows[2].name"]);
});

test("resolvePath marks a field whose parent is not an array", () => {
  assert.equal(resolvePath({ rows: {} }, "rows[]")[0].notAnArray, true);
  assert.equal(resolvePath({ rows: [] }, "rows[]").length, 0, "an empty array yields no elements to check");
});

// --------------------------------------------------------------------------
// Forbidden values
// --------------------------------------------------------------------------

test("forbidden values catch null, NaN, empty string and the string undefined", () => {
  const payload = { rows: [{ v: null }, { v: Number.NaN }, { v: "" }, { v: "undefined" }, { v: "NaN" }, { v: "fine" }] };
  const problems = forbiddenValueFindings(payload, [{ path: "rows[].v" }]);
  assert.deepEqual(problems.map((p) => p.reason), ["null", "NaN", "empty", "undefined", "NaN"]);
});

test("an enum member the screen has no label for is a finding", () => {
  const spec = [{ path: "rows[].status", enum: ["due", "completed"] }];
  assert.deepEqual(forbiddenValueFindings({ rows: [{ status: "due" }] }, spec), []);
  const problems = forbiddenValueFindings({ rows: [{ status: "surprise_new_state" }] }, spec);
  assert.equal(problems.length, 1);
  assert.equal(problems[0].reason, "unknown-enum");
});

test("a nested NaN under a declared field is found; one nobody renders is not", () => {
  const entry = { requiredFields: ["totals"], forbiddenValues: [] };
  const deep = { totals: { by_month: [{ gain: { value: Number.NaN } }] } };
  const problems = declaredSubtreeForbiddenFindings(deep, entry);
  assert.equal(problems.length, 1);
  assert.equal(problems[0].reason, "NaN");
  assert.equal(problems[0].concrete, "totals.by_month[0].gain.value");
  // The same NaN somewhere no page reads is deliberately not a finding.
  assert.deepEqual(declaredSubtreeForbiddenFindings({ debug: { t: Number.NaN } }, entry), []);
});

// --------------------------------------------------------------------------
// p95
// --------------------------------------------------------------------------

test("percentile is character-for-character the latency gate's", () => {
  const source = readFileSync(path.join(repo, "tools/perf/api-latency-gate.mjs"), "utf8");
  const body = source.match(/function percentile\(sorted, pct\) \{[\s\S]*?\n\}/)?.[0];
  assert.ok(body, "the latency gate must still define percentile(sorted, pct)");
  const gatePercentile = new Function(`${body}; return percentile;`)();
  for (const n of [1, 3, 5, 10, 19, 20, 21, 50]) {
    const samples = Array.from({ length: n }, (_, i) => (i + 1) * 7.3);
    for (const pct of [0, 50, 90, 95, 99, 100]) {
      assert.equal(percentile(samples, pct), gatePercentile(samples, pct), `p${pct} over ${n} samples must match the gate`);
    }
  }
});

test("p95 is computed by nearest rank over the sorted samples", () => {
  assert.equal(percentile([], 95), 0);
  assert.equal(percentile([5], 95), 5);
  const twenty = Array.from({ length: 20 }, (_, i) => i + 1);
  assert.equal(percentile(twenty, 95), 19, "with 20 samples p95 is the second-worst, as in the gate");
  assert.equal(percentile(twenty, 100), 20);
});

test("latency budgets come from config.json and honour the long-running exemption", () => {
  const config = JSON.parse(readFileSync(path.join(here, "config.json"), "utf8"));
  assert.equal(latencyBudgetFor({}), config.apiLatencyPolicy.hotPathP95Ms);
  assert.equal(latencyBudgetFor({ latencyBudgetMs: 200 }), 200);
  // Nothing may quietly buy itself a bigger budget than policy allows.
  assert.equal(latencyBudgetFor({ latencyBudgetMs: 9000 }), config.apiLatencyPolicy.hotPathP95Ms);
  assert.ok(isLongRunningExempt("/app/proof-media/upload"));
  assert.ok(isLongRunningExempt("/reports/export.csv?x=1"));
  assert.ok(!isLongRunningExempt("/counts/breakdown?limit=50"));
});

test("a slow finding needs a sustained breach, not one cold sample", async () => {
  const entry = { name: "x", method: "GET", path: "/x", page: "P", pageUrl: null, humanFailure: "h", requiredFields: ["ok"], latencyBudgetMs: 100 };
  // One slow sample among fast ones: p95 may exceed, median does not. No finding.
  const spiky = timedFetch([900, 10, 10, 10, 10, 10, 10, 10, 10, 10]);
  const spikyResult = await checkEntry(entry, { baseUrl: BASE, headers: {}, samples: 10, warmup: 0, fetchImpl: spiky });
  assert.deepEqual(spikyResult.findings.filter((f) => f.code === "slow"), []);
  // Consistently slow: both over budget. Finding.
  const slow = timedFetch(Array(10).fill(400));
  const slowResult = await checkEntry(entry, { baseUrl: BASE, headers: {}, samples: 10, warmup: 0, fetchImpl: slow });
  assert.equal(slowResult.findings.filter((f) => f.code === "slow").length, 1);
});

test("warmup requests are made but kept out of the samples", async () => {
  let calls = 0;
  const fetchImpl = () => { calls += 1; return stubResponse(200, JSON.stringify({ ok: 1 })); };
  const entry = { name: "x", method: "GET", path: "/x", page: "P", humanFailure: "h", requiredFields: ["ok"] };
  const result = await checkEntry(entry, { baseUrl: BASE, headers: {}, samples: 4, warmup: 3, fetchImpl });
  assert.equal(calls, 7, "3 warmup + 4 measured requests");
  assert.equal(result.samples, 4, "only the measured requests count");
});

// --------------------------------------------------------------------------
// Sweep behaviour
// --------------------------------------------------------------------------

test("one failing endpoint does not stop the sweep", async () => {
  const entry = { name: "x", method: "GET", path: "/x", page: "P", humanFailure: "h", requiredFields: ["ok"] };
  const result = await checkEntry(entry, {
    baseUrl: BASE, headers: {}, samples: 3, warmup: 0,
    fetchImpl: () => { throw new Error("connection reset"); },
  });
  assert.equal(result.passed, false);
  assert.equal(result.findings[0].code, "unreachable");
});

test("a 5xx is reported and the payload checks are skipped", async () => {
  const entry = { name: "x", method: "GET", path: "/x", page: "P", humanFailure: "h", requiredFields: ["ok"] };
  const result = await checkEntry(entry, {
    baseUrl: BASE, headers: {}, samples: 2, warmup: 0,
    fetchImpl: () => stubResponse(503, "upstream unavailable"),
  });
  assert.deepEqual(result.findings.map((f) => f.code), ["server-error"]);
});

test("date placeholders resolve in the farm's timezone, not UTC", () => {
  // 22:00 UTC on the 22nd is already the 23rd in IST; a "today" screen must match the farm.
  const lateUtc = new Date("2026-09-22T22:00:00Z");
  assert.equal(expandPath("/x?d={today}", lateUtc), "/x?d=2026-09-23");
  assert.equal(expandPath("/x?d={today_plus_1}", lateUtc), "/x?d=2026-09-24");
  assert.equal(expandPath("/x?a={today_minus_30}", lateUtc), "/x?a=2026-08-24");
  assert.equal(expandPath("/x?m={today_start_month}&n={today_end_month}", lateUtc), "/x?m=2026-09-01&n=2026-09-30");
});

// --------------------------------------------------------------------------
// Redaction
// --------------------------------------------------------------------------

test("redaction covers JSON-quoted secrets that lib/redact.mjs cannot see", () => {
  for (const [body, secret] of [
    ['{"token":"eyJhbGciOiJIUzI1NiJ9.abc.def"}', "eyJhbGciOiJIUzI1NiJ9.abc.def"],
    ['{"password":"hunter2supersecret"}', "hunter2supersecret"],
    ['{"api_key":"sk-live-abcdef123456"}', "sk-live-abcdef123456"],
    ['{"refresh_token":"1//0gSECRETVALUE"}', "1//0gSECRETVALUE"],
    ['{"Authorization":"Bearer abc.def.ghi"}', "abc.def.ghi"],
  ]) {
    const redacted = redactBody(body);
    assert.ok(!redacted.includes(secret), `secret survived redaction: ${body}`);
    assert.ok(redacted.includes("[REDACTED]"));
  }
  assert.ok(!redactBody("Authorization: Bearer abc.def").includes("abc.def"));
  // Ordinary content is untouched.
  assert.equal(redactBody('{"shed_name":"Castro 3"}'), '{"shed_name":"Castro 3"}');
});

test("a response body carrying a secret never reaches a finding unredacted", async () => {
  const entry = { name: "x", method: "GET", path: "/x", page: "P", humanFailure: "h", requiredFields: ["ok"] };
  const result = await checkEntry(entry, {
    baseUrl: BASE, headers: {}, samples: 1, warmup: 0,
    fetchImpl: () => stubResponse(500, '{"error":"boom","token":"eyJsecretvalue"}'),
  });
  const text = JSON.stringify(result);
  assert.ok(!text.includes("eyJsecretvalue"));
  assert.ok(text.includes("[REDACTED]"));
});

test("the bearer token is never written into a result", async () => {
  const entry = { name: "x", method: "GET", path: "/x", page: "P", humanFailure: "h", requiredFields: ["ok"] };
  const result = await checkEntry(entry, {
    baseUrl: BASE,
    headers: { Authorization: "Bearer supersecrettokenvalue", "X-GoatOS-Tenant-ID": "t" },
    samples: 1, warmup: 0,
    fetchImpl: () => stubResponse(200, JSON.stringify({ ok: 1 })),
  });
  assert.ok(!JSON.stringify(result).includes("supersecrettokenvalue"));
});

// --------------------------------------------------------------------------
// The catalogue
// --------------------------------------------------------------------------

test("every catalogue entry is a GET with a page, a sentence and its commits", () => {
  const catalogue = loadCatalogue();
  assert.ok(catalogue.endpoints.length >= 40, "the catalogue must cover the dashboard, not a sample");
  for (const entry of catalogue.endpoints) {
    assert.equal(assertGetOnly(entry), "GET");
    assert.ok(entry.page && entry.page.trim(), `${entry.name} needs the page it feeds`);
    assert.ok(entry.pageUrl?.startsWith("https://dashboard.mesha.sg"), `${entry.name} needs a link to that page`);
    assert.ok(entry.requiredFields.length > 0, `${entry.name} needs at least one required field`);
    assert.ok(entry.sourceCommits.length > 0, `${entry.name} must name the commits it comes from`);
    for (const sha of entry.sourceCommits) assert.match(sha, /^[0-9a-f]{7,40}$/, `${entry.name} has a malformed commit sha`);
    assert.ok(entry.latencyBudgetMs > 0 && entry.latencyBudgetMs <= 500, `${entry.name} budget must respect policy`);
  }
  const names = catalogue.endpoints.map((e) => e.name);
  assert.equal(new Set(names).size, names.length, "entry names must be unique");
});

test("every humanFailure is a sentence a farm manager reads, with nothing technical in it", () => {
  for (const entry of loadCatalogue().endpoints) {
    assert.ok(assertHumanSentence(entry.name, entry.humanFailure));
    const sentence = entry.humanFailure;
    assert.ok(!sentence.includes("/"), `${entry.name}: URL path in the Slack sentence`);
    assert.ok(!/\b[45]\d\d\b/.test(sentence), `${entry.name}: status code in the Slack sentence`);
    assert.ok(!/\b[a-z]+_[a-z_]+\b/.test(sentence), `${entry.name}: snake_case identifier in the Slack sentence`);
    assert.ok(!/[a-z_]+\.[a-z_]+/i.test(sentence.replace(/\.(\s|$)/g, "$1")), `${entry.name}: field path in the Slack sentence`);
    assert.ok(sentence.length > 40, `${entry.name}: the sentence must actually describe what a person sees`);
    assert.match(sentence, /^[A-Z]/, `${entry.name}: the sentence must read as prose`);
  }
});

test("the sentence lint rejects the things it is supposed to reject", () => {
  // Guard the guard: a lint that cannot fail is not a check.
  assert.throws(() => assertHumanSentence("t", "The weights page calls /weighing/shed-weights and fails."), /URL path/);
  assert.throws(() => assertHumanSentence("t", "The server answered HTTP 500 for this screen."), /status code/);
  assert.throws(() => assertHumanSentence("t", "The field summary.total_count came back empty on screen."), /field path/);
  assert.throws(() => assertHumanSentence("t", "The check required_field_missing fired on this screen."), /check code/);
  assert.throws(() => assertHumanSentence("t", ""), /empty/);
  assert.ok(assertHumanSentence("t", "The weights page would show a blank column where the average daily gain should be."));
});

test("the catalogue covers the hot paths the perf manifests already track", () => {
  const catalogue = loadCatalogue();
  const covered = new Set(catalogue.endpoints.map((e) => basePath(e.path)));
  const manifestDir = path.join(repo, "tools/perf");
  const hot = new Set();
  for (const name of readdirSync(manifestDir).filter((n) => /^hot-paths\..*\.json$/.test(n))) {
    for (const endpoint of JSON.parse(readFileSync(path.join(manifestDir, name), "utf8")).endpoints ?? []) {
      hot.add(basePath(endpoint.path));
    }
  }
  const missed = [...hot].filter((p) => !covered.has(p));
  // The catalogue is page-driven, so it does not have to mirror every perf variant, but it
  // must not drift into covering only a corner of them.
  assert.ok(hot.size - missed.length >= 25, `only ${hot.size - missed.length} hot paths are covered by the catalogue`);
});

// --------------------------------------------------------------------------
// History-mined checks
// --------------------------------------------------------------------------

test("a history-mined row without a human sentence is parked, never rendered", () => {
  const { accepted, parked } = normalizeLaneChecks([
    { id: "lane3.a", name: "A", path: "/a", required: ["x"], sourceShas: ["abc1234"] }, // no sentence
    { id: "lane3.b", name: "B", path: "/b", failureSentence: "The board would be empty.", required: ["x"], sourceShas: ["abc1234"] },
    { id: "lane3.c", name: "C", method: "POST", path: "/c", failureSentence: "s", required: ["x"], sourceShas: ["abc1234"] },
    { id: "lane3.d", name: "D", failureSentence: "s", required: ["x"], sourceShas: ["abc1234"] }, // no path
    { id: "lane3.e", name: "E", path: "/e", failureSentence: "s", required: [], sourceShas: ["abc1234"] }, // no fields
    { id: "lane3.f", name: "F", path: "/f", failureSentence: "s", required: ["x"], sourceShas: [] }, // no commits
  ], { includeUngrounded: true });
  assert.deepEqual(accepted.map((e) => e.name), ["lane3.b"]);
  assert.equal(parked.length, 5);
  for (const row of parked) assert.ok(row.reason && row.reason.length > 5, "every parked row needs a reason");
  // Nothing accepted may produce an "undefined" bullet in Slack.
  for (const entry of accepted) {
    assert.equal(typeof entry.humanFailure, "string");
    assert.ok(entry.humanFailure.trim().length > 0);
    assert.equal(entry.method, "GET");
  }
});

test("a history-mined row for an endpoint the catalogue already verifies is parked", () => {
  const { accepted, parked } = normalizeLaneChecks(
    [{ id: "lane3.dup", path: "/control-tower/vaccination?category=x", failureSentence: "s", required: ["a"], sourceShas: ["abc1234"] }],
    { knownPaths: new Set(["/control-tower/vaccination"]) },
  );
  assert.deepEqual(accepted, []);
  assert.match(parked[0].reason, /already covered/);
});

test("the miner's forbidden-token list becomes per-field specs", () => {
  const { accepted } = normalizeLaneChecks([{
    id: "lane3.g", name: "G", path: "/g", failureSentence: "The screen would be blank.",
    required: ["rows[].name"], forbidden: ["null", "NaN", "", "Invalid Date"], sourceShas: ["abc1234"],
  }], { includeUngrounded: true });
  assert.deepEqual(accepted[0].forbiddenValues, [{ path: "rows[].name", disallow: ["null", "NaN", "empty"] }]);
});

test("a malformed or absent lane-checks file never takes the sweep down", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "lane3-checks-"));
  try {
    const bad = path.join(dir, "lane-checks.json");
    writeFileSync(bad, "{ not json");
    const result = normalizeLaneChecks(undefined);
    assert.deepEqual(result.accepted, []);
    assert.deepEqual(normalizeLaneChecks(null).accepted, []);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("history-mined checks are parked by default, because their fields are unverified", () => {
  // Proved on production: these 17 endpoints produced 1184 findings that production never
  // promised, against 12 real ones from the verified catalogue. Default must be off.
  const row = { id: "lane3.x", name: "X", path: "/x", failureSentence: "The screen would be blank.", required: ["a"], sourceShas: ["abc1234"] };
  const off = normalizeLaneChecks([row]);
  assert.deepEqual(off.accepted, []);
  assert.match(off.parked[0].reason, /derived from commit text/);
  assert.match(off.parked[0].reason, /GOATOS_DASHBOARD_API_CONTRACTS_INCLUDE_HISTORY/);
  const on = normalizeLaneChecks([row], { includeUngrounded: true });
  assert.deepEqual(on.accepted.map((e) => e.name), ["lane3.x"]);
});

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

function stubResponse(status, body, headers = {}) {
  return {
    status,
    ok: status >= 200 && status < 300,
    headers: { get: (key) => headers[String(key).toLowerCase()] ?? null },
    text: async () => body,
  };
}

// Returns a fetch stub whose nth call blocks the clock for durations[n] ms.
function timedFetch(durations) {
  let i = 0;
  return async () => {
    const ms = durations[Math.min(i++, durations.length - 1)];
    const until = performance.now() + ms;
    while (performance.now() < until) { /* deliberate busy wait: the clock is the assertion */ }
    return stubResponse(200, JSON.stringify({ ok: 1 }));
  };
}
