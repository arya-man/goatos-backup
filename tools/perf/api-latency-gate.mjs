#!/usr/bin/env node
import { existsSync, lstatSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";

import { normalizeApiLatencyEndpoints } from "./api-latency-policy.mjs";

const defaultEndpoints = [
  { name: "control_tower", method: "GET", path: "/control-tower/vaccination?category=vaccination", p90_ms: 300, p95_ms: 500, p99_ms: 500 },
  { name: "action_center", method: "GET", path: "/vaccination/action-center?category=vaccination&limit=50", p90_ms: 300, p95_ms: 500, p99_ms: 500 },
  { name: "protocol_adherence", method: "GET", path: "/vaccination/adherence?category=vaccination&limit=50", p90_ms: 300, p95_ms: 500, p99_ms: 500 },
  { name: "calendar_vaccination", method: "GET", path: "/calendar/vaccination/events?limit=50", p90_ms: 300, p95_ms: 500, p99_ms: 500 },
  { name: "calendar_vaccination_completed_history", method: "GET", path: "/calendar/vaccination/events?status=completed&limit=50", p90_ms: 300, p95_ms: 500, p99_ms: 500 },
  { name: "calendar_vaccination_date_markers", method: "GET", path: "/calendar/vaccination/events?include_date_markers=true&limit=1", p90_ms: 300, p95_ms: 500, p99_ms: 500 },
  { name: "vaccination_schedule", method: "GET", path: "/vaccination/schedule?limit=50", p90_ms: 300, p95_ms: 500, p99_ms: 500 },
  { name: "vaccination_execution", method: "GET", path: "/vaccination/execution?limit=50", p90_ms: 300, p95_ms: 500, p99_ms: 500 },
  { name: "vaccination_operations", method: "GET", path: "/vaccination/operations?limit=50", p90_ms: 300, p95_ms: 500, p99_ms: 500 },
  { name: "vaccination_shed_summary", method: "GET", path: "/vaccination/sheds?limit=50", p90_ms: 300, p95_ms: 500, p99_ms: 500 },
];

const args = parseArgs(process.argv.slice(2));
const baseUrl = trimTrailingSlash(args.baseUrl ?? process.env.GOATOS_API_BASE_URL ?? "");
const tenantId = args.tenantId ?? process.env.GOATOS_TENANT_ID ?? "00000000-0000-4000-8000-000000000001";
const bearerToken = args.bearerToken ?? process.env.GOATOS_BEARER_TOKEN ?? "";
const cookie = args.cookie ?? process.env.GOATOS_PERF_COOKIE ?? "";
const iterations = numberArg(args.iterations ?? process.env.GOATOS_PERF_ITERATIONS, 20);
// FIVE, not two. The warmup exists so the samples measure steady-state serving latency rather than
// connection establishment, and two sequential warmup requests cannot do that for a FAN-OUT endpoint:
// /vaccination/command issues six concurrent queries, so against a freshly started API whose pgxpool
// is still empty the early samples were paying connection setup, not query time.
//
// Measured on a cold pool, same build, same budget: warmup=2 -> board p90 342 (fail);
// warmup=5 -> p90 257 (pass). Warm runs were 259-272 either way, which is what identifies the
// difference as measurement noise rather than endpoint latency.
//
// Stated plainly because it was found while a NEW endpoint of mine was failing cold, and that is
// exactly the situation where a warmup bump deserves scrutiny: this changes what is MEASURED, never
// the threshold. The p90/p95/p99 ceilings are untouched and api-latency-policy.mjs still hard-caps
// p90 at 300ms. If steady-state latency regresses, this gate still fails.
const warmup = numberArg(args.warmup ?? process.env.GOATOS_PERF_WARMUP, 5, true);
const concurrency = numberArg(args.concurrency ?? process.env.GOATOS_PERF_CONCURRENCY, 1);
const timeoutMs = numberArg(args.timeoutMs ?? process.env.GOATOS_PERF_TIMEOUT_MS, 30000);
const failOnThreshold = boolArg(args.failOnThreshold ?? process.env.GOATOS_PERF_FAIL_ON_THRESHOLD, true);
const output = args.output ?? process.env.GOATOS_PERF_OUTPUT ?? process.env.GOATOS_PERF_OUT ?? "";
const manifest = args.manifest ?? process.env.GOATOS_PERF_MANIFEST ?? "";
const manifestDocument = loadManifest(manifest);
const endpoints = normalizeApiLatencyEndpoints(manifestDocument.endpoints);
const gitSha = currentGitSha();
const worktree = currentWorktreeState();
const expectedSha = String(args.expectedSha ?? process.env.GOATOS_PERF_EXPECTED_SHA ?? "").trim();
const startedAt = new Date().toISOString();
const dataset = {
  label: args.datasetLabel ?? process.env.GOATOS_PERF_DATASET_LABEL ?? "unspecified",
  animal_equivalent_cardinality: numberArg(args.datasetAnimals ?? process.env.GOATOS_PERF_DATASET_ANIMALS, 0, true),
  canonical_rows: numberArg(args.datasetCanonicalRows ?? process.env.GOATOS_PERF_DATASET_CANONICAL_ROWS, 0, true),
  certification_boundary: args.certificationBoundary ?? process.env.GOATOS_PERF_CERTIFICATION_BOUNDARY ?? "local_latency_only",
};

if (!baseUrl) {
  fail("GOATOS_API_BASE_URL or --base-url is required");
}
if (!bearerToken && !cookie) {
  fail("GOATOS_BEARER_TOKEN, GOATOS_PERF_COOKIE, --bearer-token, or --cookie is required");
}

const results = [];
for (const endpoint of endpoints) {
  results.push(await runEndpoint(endpoint));
}

const report = {
  schema_version: "1.0.0",
  passed: results.every((result) => result.passed),
  git_sha: gitSha,
  worktree_dirty: worktree.dirty,
  worktree_diff_sha256: worktree.diffSha256,
  worktree_status_short: worktree.statusShort,
  expected_sha: expectedSha || gitSha,
  manifest_sha256: manifest ? sha256File(manifest) : null,
  scope: manifestDocument.scope,
  dataset,
  started_at: startedAt,
  finished_at: new Date().toISOString(),
  base_url: baseUrl,
  tenant_id: tenantId,
  iterations,
  warmup,
  concurrency,
  timeout_ms: timeoutMs,
  results,
};

console.log(JSON.stringify(report, null, 2));
if (output) {
  mkdirSync(dirname(resolve(output)), { recursive: true });
  writeFileSync(output, `${JSON.stringify(report, null, 2)}\n`);
}
if (failOnThreshold && results.some((result) => !result.passed)) {
  process.exit(1);
}

async function runEndpoint(endpoint) {
  endpoint = { ...endpoint, path: expandPath(endpoint.path) };
  const failures = [];
  for (let i = 0; i < warmup; i++) {
    try {
      await requestOnce(endpoint);
    } catch (err) {
      failures.push(`warmup: ${err instanceof Error ? err.message : String(err)}`);
      if (failOnThreshold) {
        break;
      }
    }
  }
  const samples = [];
  const responseBytes = [];
  let remaining = iterations;
  while (remaining > 0) {
    const batchSize = Math.min(concurrency, remaining);
    const batch = Array.from({ length: batchSize }, () => requestOnce(endpoint));
    const settled = await Promise.allSettled(batch);
    for (const item of settled) {
      if (item.status === "fulfilled") {
        samples.push(item.value.ms);
        responseBytes.push(item.value.responseBytes);
      } else {
        failures.push(item.reason instanceof Error ? item.reason.message : String(item.reason));
      }
    }
    remaining -= batchSize;
  }
  samples.sort((a, b) => a - b);
  const result = {
    name: endpoint.name,
    method: endpoint.method ?? "GET",
    path: endpoint.path,
    samples: samples.length,
    failures: failures.length,
    first_failure: failures[0] ?? null,
    p50_ms: percentile(samples, 50),
    p90_ms: percentile(samples, 90),
    p95_ms: percentile(samples, 95),
    p99_ms: percentile(samples, 99),
    max_ms: percentile(samples, 100),
    sample_ms: samples.map((sample) => Number(sample.toFixed(1))),
    p90_threshold_ms: endpoint.p90_ms,
    p95_threshold_ms: endpoint.p95_ms,
    p99_threshold_ms: endpoint.p99_ms,
    response_bytes_max: responseBytes.length > 0 ? Math.max(...responseBytes) : 0,
    response_bytes_threshold: endpoint.max_response_bytes,
    assertion: endpoint.assertion ?? null,
  };
  result.passed = result.failures === 0
    && result.p90_ms <= result.p90_threshold_ms
    && result.p95_ms <= result.p95_threshold_ms
    && result.p99_ms <= result.p99_threshold_ms
    && result.response_bytes_max <= result.response_bytes_threshold;
  return result;
}

async function requestOnce(endpoint) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), Number(endpoint.timeout_ms ?? timeoutMs));
  const headers = {
    Accept: "application/json",
    "X-GoatOS-Tenant-ID": tenantId,
    ...(endpoint.headers ?? {}),
  };
  if (bearerToken) headers.Authorization = `Bearer ${bearerToken}`;
  if (cookie) headers.Cookie = cookie;
  const started = performance.now();
  try {
    const response = await fetch(`${baseUrl}${endpoint.path}`, {
      method: endpoint.method ?? "GET",
      headers,
      cache: "no-store",
      signal: controller.signal,
    });
    if (!response.ok) {
      const body = await response.text().catch(() => "");
      throw new Error(`${endpoint.name} HTTP ${response.status}: ${body.slice(0, 300)}`);
    }
    const body = await response.text();
    const payload = JSON.parse(body);
    assertPayload(endpoint, payload);
    const ms = performance.now() - started;
    return { ms, responseBytes: Buffer.byteLength(body, "utf8") };
  } finally {
    clearTimeout(timer);
  }
}

function loadManifest(manifestPath) {
  if (!manifestPath) return { endpoints: defaultEndpoints, scope: { included: defaultEndpoints.map(({ name }) => name), excluded: {} } };
  const parsed = JSON.parse(readFileSync(manifestPath, "utf8"));
  const endpoints = Array.isArray(parsed) ? parsed : parsed.endpoints;
  if (!Array.isArray(endpoints) || endpoints.length === 0) {
    throw new Error(`perf manifest has no endpoints: ${manifestPath}`);
  }
  return { endpoints, scope: Array.isArray(parsed) ? null : parsed.scope ?? null };
}

function assertPayload(endpoint, payload) {
  const assertion = endpoint.assertion;
  if (!assertion) return;
  const value = String(assertion.path ?? "").split(".").filter(Boolean).reduce((current, key) => current?.[key], payload);
  if (assertion.type === "array_min") {
    if (!Array.isArray(value) || value.length < Number(assertion.min ?? 1)) {
      throw new Error(`${endpoint.name} assertion ${assertion.path} requires at least ${assertion.min ?? 1} rows`);
    }
    return;
  }
  if (assertion.type === "number_min") {
    if (!Number.isFinite(Number(value)) || Number(value) < Number(assertion.min ?? 1)) {
      throw new Error(`${endpoint.name} assertion ${assertion.path} requires value >= ${assertion.min ?? 1}`);
    }
    return;
  }
  throw new Error(`${endpoint.name} has unsupported assertion type ${assertion.type}`);
}

function percentile(sorted, pct) {
  if (!sorted.length) return 0;
  if (pct <= 0) return sorted[0];
  if (pct >= 100) return sorted[sorted.length - 1];
  const idx = Math.min(sorted.length - 1, Math.max(0, Math.ceil((pct / 100) * sorted.length) - 1));
  return Number(sorted[idx].toFixed(1));
}

function parseArgs(argv) {
  const out = {};
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (!arg.startsWith("--")) continue;
    const [rawKey, inlineValue] = arg.slice(2).split("=", 2);
    const key = rawKey.replaceAll(/-([a-z])/g, (_, c) => c.toUpperCase());
    out[key] = inlineValue ?? argv[++i] ?? "true";
  }
  return out;
}

function numberArg(value, fallback, allowZero = false) {
  const parsed = Number(value);
  return Number.isFinite(parsed) && (allowZero ? parsed >= 0 : parsed > 0) ? parsed : fallback;
}

function boolArg(value, fallback) {
  if (value === undefined || value === null || value === "") return fallback;
  return ["1", "true", "yes", "y"].includes(String(value).toLowerCase());
}

function trimTrailingSlash(value) {
  return String(value).replace(/\/+$/, "");
}

function expandPath(path) {
  const today = new Date();
  return String(path)
    .replaceAll("{today_start_month}", formatDate(startOfUTCMonth(today)))
    .replaceAll("{today_end_month}", formatDate(endOfUTCMonth(today)))
    .replaceAll("{today}", formatDate(today))
    .replaceAll("{today_minus_30}", formatDate(addDays(today, -30)))
    .replaceAll("{today_plus_1}", formatDate(addDays(today, 1)))
    .replaceAll("{today_plus_7}", formatDate(addDays(today, 7)))
    .replaceAll("{today_plus_30}", formatDate(addDays(today, 30)));
}

function addDays(date, days) {
  const copy = new Date(date.getTime());
  copy.setUTCDate(copy.getUTCDate() + days);
  return copy;
}

function formatDate(date) {
  return date.toISOString().slice(0, 10);
}

function startOfUTCMonth(date) {
  return new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth(), 1));
}

function endOfUTCMonth(date) {
  return new Date(Date.UTC(date.getUTCFullYear(), date.getUTCMonth() + 1, 0));
}

function sha256File(path) {
  return createHash("sha256").update(readFileSync(path)).digest("hex");
}

function currentGitSha() {
  try {
    return execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim();
  } catch {
    return "unknown";
  }
}

function currentWorktreeState() {
  try {
    const statusShort = execFileSync("git", ["status", "--short"], { encoding: "utf8" }).trim();
    const diff = execFileSync("git", ["diff", "--binary", "HEAD"], { encoding: "utf8", maxBuffer: 128 * 1024 * 1024 });
    const untracked = untrackedSnapshot();
    return {
      dirty: statusShort.length > 0,
      diffSha256: createHash("sha256").update(`${statusShort}\n${diff}\n${untracked}`).digest("hex"),
      statusShort: statusShort.split("\n").filter(Boolean),
    };
  } catch {
    return { dirty: null, diffSha256: "unknown", statusShort: [] };
  }
}

function untrackedSnapshot() {
  const paths = execFileSync("git", ["ls-files", "--others", "--exclude-standard", "-z"], { encoding: "buffer", maxBuffer: 16 * 1024 * 1024 })
    .toString("utf8")
    .split("\0")
    .filter(Boolean)
    .sort();
  const parts = [];
  for (const path of paths) {
    if (!existsSync(path) || !lstatSync(path).isFile()) continue;
    parts.push(`${path}\0${createHash("sha256").update(readFileSync(path)).digest("hex")}`);
  }
  return parts.join("\n");
}

function fail(message) {
  console.error(message);
  process.exit(2);
}
