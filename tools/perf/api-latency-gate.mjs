#!/usr/bin/env node
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

const defaultEndpoints = [
  { name: "control_tower", method: "GET", path: "/control-tower/vaccination?category=vaccination", p95_ms: 1200, p99_ms: 2500 },
  { name: "action_center", method: "GET", path: "/vaccination/action-center?category=vaccination&limit=50", p95_ms: 1200, p99_ms: 2500 },
  { name: "protocol_adherence", method: "GET", path: "/vaccination/adherence?category=vaccination&limit=50", p95_ms: 1200, p99_ms: 2500 },
  { name: "calendar_vaccination", method: "GET", path: "/calendar/vaccination/events?limit=50", p95_ms: 1200, p99_ms: 2500 },
  { name: "vaccination_execution", method: "GET", path: "/vaccination/execution?limit=50", p95_ms: 1200, p99_ms: 2500 },
  { name: "vaccination_operations", method: "GET", path: "/vaccination/operations?limit=50", p95_ms: 1200, p99_ms: 2500 },
  { name: "vaccination_shed_summary", method: "GET", path: "/vaccination/sheds?limit=50", p95_ms: 1200, p99_ms: 2500 },
];

const args = parseArgs(process.argv.slice(2));
const baseUrl = trimTrailingSlash(args.baseUrl ?? process.env.GOATOS_API_BASE_URL ?? "");
const tenantId = args.tenantId ?? process.env.GOATOS_TENANT_ID ?? "00000000-0000-4000-8000-000000000001";
const bearerToken = args.bearerToken ?? process.env.GOATOS_BEARER_TOKEN ?? "";
const cookie = args.cookie ?? process.env.GOATOS_PERF_COOKIE ?? "";
const iterations = numberArg(args.iterations ?? process.env.GOATOS_PERF_ITERATIONS, 20);
const warmup = numberArg(args.warmup ?? process.env.GOATOS_PERF_WARMUP, 2);
const concurrency = numberArg(args.concurrency ?? process.env.GOATOS_PERF_CONCURRENCY, 1);
const timeoutMs = numberArg(args.timeoutMs ?? process.env.GOATOS_PERF_TIMEOUT_MS, 30000);
const failOnThreshold = boolArg(args.failOnThreshold ?? process.env.GOATOS_PERF_FAIL_ON_THRESHOLD, true);
const output = args.output ?? process.env.GOATOS_PERF_OUTPUT ?? "";
const endpoints = loadEndpoints(args.manifest ?? process.env.GOATOS_PERF_MANIFEST);

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
  for (let i = 0; i < warmup; i++) {
    await requestOnce(endpoint);
  }
  const samples = [];
  const failures = [];
  let remaining = iterations;
  while (remaining > 0) {
    const batchSize = Math.min(concurrency, remaining);
    const batch = Array.from({ length: batchSize }, () => requestOnce(endpoint));
    const settled = await Promise.allSettled(batch);
    for (const item of settled) {
      if (item.status === "fulfilled") {
        samples.push(item.value.ms);
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
    p95_threshold_ms: Number(endpoint.p95_ms ?? 1200),
    p99_threshold_ms: Number(endpoint.p99_ms ?? 2500),
  };
  result.passed = result.failures === 0 && result.p95_ms <= result.p95_threshold_ms && result.p99_ms <= result.p99_threshold_ms;
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
    const ms = performance.now() - started;
    if (!response.ok) {
      const body = await response.text().catch(() => "");
      throw new Error(`${endpoint.name} HTTP ${response.status}: ${body.slice(0, 300)}`);
    }
    await response.arrayBuffer();
    return { ms };
  } finally {
    clearTimeout(timer);
  }
}

function loadEndpoints(manifestPath) {
  if (!manifestPath) return defaultEndpoints;
  const parsed = JSON.parse(readFileSync(manifestPath, "utf8"));
  const endpoints = Array.isArray(parsed) ? parsed : parsed.endpoints;
  if (!Array.isArray(endpoints) || endpoints.length === 0) {
    throw new Error(`perf manifest has no endpoints: ${manifestPath}`);
  }
  return endpoints;
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

function numberArg(value, fallback) {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function boolArg(value, fallback) {
  if (value === undefined || value === null || value === "") return fallback;
  return ["1", "true", "yes", "y"].includes(String(value).toLowerCase());
}

function trimTrailingSlash(value) {
  return String(value).replace(/\/+$/, "");
}

function fail(message) {
  console.error(message);
  process.exit(2);
}
