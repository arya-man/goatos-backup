#!/usr/bin/env node
import { createHash, randomUUID } from 'node:crypto';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { observeApiPayload } from './api-latency-evidence.mjs';

export const LANES = ['todo', 'in_progress', 'in_review', 'done'];
const PARKS = { CBE: '00000000-0000-4000-8000-000000003001', CPT: '00000000-0000-4000-8000-000000003002' };
const hash = (value) => createHash('sha256').update(value).digest('hex');
const canonical = (value) => JSON.stringify(sortObject(value));
function sortObject(value) {
  if (Array.isArray(value)) return value.map(sortObject);
  if (!value || typeof value !== 'object') return value;
  return Object.fromEntries(Object.keys(value).sort().map((key) => [key, sortObject(value[key])]));
}
export function actorFingerprint(token, tenant) {
  let claims;
  try { claims = JSON.parse(Buffer.from(token.split('.')[1], 'base64url').toString()); } catch { throw new Error('Bearer must be a JWT with a subject for scenario identity'); }
  if (!claims.sub) throw new Error('Bearer JWT subject missing');
  return hash(canonical({ tenant, sub: claims.sub, iss: claims.iss ?? '' }));
}
export function scenarioFailures(before, after) {
  return ['actor_fingerprint', 'tenant_id', 'business_date', 'limit', 'parks', 'iterations'].filter((key) => canonical(before[key]) !== canonical(after[key])).map((key) => `scenario ${key} differs`);
}
export function compareBoardReports(before, after) {
  const failures = scenarioFailures(before, after);
  const incomparable = [];
  for (const park of Object.keys(after.parks ?? {})) {
    for (const kind of ['legacy', 'page']) {
      const prior = before.results?.[park]?.[kind] ?? [];
      const current = after.results?.[park]?.[kind] ?? [];
      if (prior.length !== before.iterations || current.length !== after.iterations) failures.push(`${park} ${kind} missing samples`);
      for (let i = 0; i < Math.max(prior.length, current.length); i++) {
        if (!prior[i]?.valid || !current[i]?.valid) {
          incomparable.push(`${park} ${kind} sample ${i}: invalid/missing response; row parity unproven`);
          continue;
        }
        failures.push(...parityFailures(prior[i].snapshot, current[i].snapshot).map((message) => `${park} ${kind} sample ${i}: ${message}`));
      }
    }
  }
  return { passed: failures.length === 0 && incomparable.length === 0, failures, incomparable };
}

export function boardSnapshot(summary, lanes, envelope = {}) {
  observeApiPayload({ name: 'work-board' }, { summary, lanes, ...envelope });
  if (!Number.isInteger(summary?.total) || summary.total <= 0) throw new Error('Work Board summary total must be nonzero');
  const rowIDs = {};
  const cursors = {};
  for (const lane of LANES) {
    if (!Number.isInteger(summary.by_lane?.[lane]) || summary.by_lane[lane] < 0) throw new Error(`Missing summary lane ${lane}`);
    if (!Array.isArray(lanes?.[lane]?.rows)) throw new Error(`Missing rows for ${lane}`);
    rowIDs[lane] = lanes[lane].rows.map((row) => {
      if (!row.row_key || row.lane !== lane) throw new Error(`Invalid identity/lane in ${lane}`);
      return row.row_key;
    });
    if (new Set(rowIDs[lane]).size !== rowIDs[lane].length) throw new Error(`Duplicate row identities in ${lane}`);
    if (summary.by_lane[lane] > 0 && rowIDs[lane].length === 0) throw new Error(`Nonempty ${lane} summary has no first-page rows`);
    cursors[lane] = Boolean(lanes[lane].next_cursor);
  }
  if (LANES.reduce((sum, lane) => sum + summary.by_lane[lane], 0) !== summary.total) throw new Error('Summary total and lanes disagree');
  const counts = Object.fromEntries(['total', 'by_lane', 'by_state', 'by_module', 'by_module_lane', 'needs_attention'].map((key) => [key, summary[key]]));
  return { counts, row_ids: rowIDs, row_counts: Object.fromEntries(LANES.map((lane) => [lane, rowIDs[lane].length])), has_next_cursor: cursors, degraded: [] };
}
export function parityFailures(legacy, page) {
  return ['counts', 'row_ids', 'has_next_cursor'].filter((key) => canonical(legacy[key]) !== canonical(page[key])).map((key) => `legacy/page ${key} mismatch`);
}
export function statistics(samples) {
  const times = samples.map((sample) => sample.ms).sort((a, b) => a - b);
  const percentile = (p) => times.length ? times[Math.max(0, Math.ceil(times.length * p / 100) - 1)] : null;
  return { samples: times.length, p90_ms: percentile(90), p95_ms: percentile(95), p99_ms: percentile(99), max_ms: percentile(100), response_bytes_max: samples.length ? Math.max(...samples.map((s) => s.response_bytes)) : 0 };
}

export function captureSample(timing, summary, lanes, envelope = {}) {
  const sample = { ...timing, valid: true };
  try { sample.snapshot = boardSnapshot(summary, lanes, envelope); }
  catch (error) {
    sample.valid = false;
    sample.error = error.message;
    // Preserve diagnostic data and timing even when partial work invalidates the sample.
    sample.invalid_response = { summary, degraded: envelope.degraded ?? [], lanes: Object.fromEntries(LANES.map((lane) => [lane, {
      row_count: Array.isArray(lanes?.[lane]?.rows) ? lanes[lane].rows.length : null,
      row_ids: lanes?.[lane]?.rows?.map((row) => row.row_key) ?? null,
      degraded: lanes?.[lane]?.degraded ?? [],
      has_next_cursor: Boolean(lanes?.[lane]?.next_cursor),
    }])) };
  }
  return sample;
}

export async function runBenchmark(options) {
  const { baseUrl, token, tenant, iterations, date, limit } = options;
  const headers = { Authorization: `Bearer ${token}`, 'X-GoatOS-Tenant-ID': tenant, Accept: 'application/json' };
  async function request(path) {
    const start = performance.now();
    const response = await fetch(`${baseUrl.replace(/\/$/, '')}${path}`, { headers, signal: AbortSignal.timeout(30000), cache: 'no-store' });
    const body = await response.text();
    if (!response.ok) throw new Error(`${path.split('?')[0]} HTTP ${response.status}`);
    const payload = JSON.parse(body);
    return { payload, ms: performance.now() - start, response_bytes: Buffer.byteLength(body) };
  }
  const version = (await request('/version')).payload;
  if (!version.build_sha || ['unknown', 'dev'].includes(version.build_sha)) throw new Error('/version must identify the actual API build_sha');
  const report = { schema_version: 1, started_at: new Date().toISOString(), api_build_sha: version.build_sha, base_url: baseUrl, tenant_id: tenant, actor_fingerprint: actorFingerprint(token, tenant), business_date: date, limit, parks: PARKS, iterations, cache_mode: 'unique perf_sample on every HTTP request', cold_definition: 'first request per path in this run; process/DB caches may already be warm', results: {}, failures: [] };
  async function measure(kind, park) {
    const path = (endpoint, lane) => `/work-board/${endpoint}?${new URLSearchParams({ park, business_date: date, limit: String(limit), ...(lane ? { lane } : {}), perf_sample: randomUUID() })}`;
    const start = performance.now();
    if (kind === 'page') {
      const result = await request(path('page'));
      return captureSample({ ms: result.ms, response_bytes: result.response_bytes, http_requests: 1 }, result.payload.summary, result.payload.lanes, { degraded: result.payload.degraded });
    }
    const [summary, ...rows] = await Promise.all([request(path('summary')), ...LANES.map((lane) => request(path('rows', lane)))]);
    return captureSample({ ms: performance.now() - start, response_bytes: [summary, ...rows].reduce((sum, r) => sum + r.response_bytes, 0), http_requests: 5 }, summary.payload, Object.fromEntries(LANES.map((lane, index) => [lane, rows[index].payload])));
  }
  for (const [name, park] of Object.entries(PARKS)) {
    const result = { cold: {}, legacy: [], page: [] };
    report.results[name] = result;
    for (let index = -1; index < iterations; index++) {
      const pair = {};
      // Alternate order to avoid systematically favoring the second path's DB cache.
      for (const kind of (index % 2 === 0 ? ['page', 'legacy'] : ['legacy', 'page'])) {
        try {
          pair[kind] = await measure(kind, park);
          if (!pair[kind].valid) report.failures.push(`${name} sample ${index} ${kind}: ${pair[kind].error}`);
        }
        catch (error) { report.failures.push(`${name} sample ${index} ${kind}: ${error.message}`); }
      }
      if (pair.legacy?.valid && pair.page?.valid) report.failures.push(...parityFailures(pair.legacy.snapshot, pair.page.snapshot).map((failure) => `${name} sample ${index}: ${failure}`));
      if (index === -1) result.cold = pair;
      else for (const kind of ['legacy', 'page']) if (pair[kind]) result[kind].push(pair[kind]);
    }
    result.statistics = { legacy: statistics(result.legacy.filter((sample) => sample.valid)), page: statistics(result.page.filter((sample) => sample.valid)) };
    result.raw_statistics_including_invalid = { legacy: statistics(result.legacy), page: statistics(result.page) };
    result.invalid_samples = { legacy: result.legacy.filter((sample) => !sample.valid).length, page: result.page.filter((sample) => !sample.valid).length };
    for (const [key, ceiling] of Object.entries({ p90_ms: 300, p95_ms: 500, p99_ms: 500 })) {
      if (result.statistics.page[key] === null || result.statistics.page[key] > ceiling) report.failures.push(`${name} page ${key}=${result.statistics.page[key]} exceeds ${ceiling}`);
    }
    for (const kind of ['legacy', 'page']) if (result[kind].length !== iterations) report.failures.push(`${name} ${kind} missing measured samples`);
  }
  const endVersion = (await request('/version')).payload;
  if (endVersion.build_sha !== report.api_build_sha) report.failures.push('API build changed during benchmark');
  report.finished_at = new Date().toISOString();
  report.passed = report.failures.length === 0;
  return report;
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const args = {};
  for (let i = 2; i < process.argv.length; i += 2) args[process.argv[i].replace(/^--/, '')] = process.argv[i + 1];
  const options = { baseUrl: args['base-url'] ?? process.env.GOATOS_API_BASE_URL, token: process.env.GOATOS_BEARER_TOKEN, tenant: args.tenant ?? process.env.GOATOS_TENANT_ID ?? '00000000-0000-4000-8000-000000000001', iterations: Number(args.iterations ?? 20), date: args.date ?? '2026-08-10', limit: Number(args.limit ?? 50) };
  if (!options.baseUrl || !options.token || !args.output || !Number.isInteger(options.iterations) || options.iterations < 1 || !Number.isInteger(options.limit) || options.limit < 1) throw new Error('Require --output, API URL/token env, positive --iterations and --limit');
  const report = await runBenchmark(options);
  if (args.before) {
    report.before_after_parity = compareBoardReports(JSON.parse(readFileSync(args.before, 'utf8')), report);
    report.latency_passed = report.passed;
    report.passed = report.passed && report.before_after_parity.passed;
  }
  mkdirSync(dirname(resolve(args.output)), { recursive: true });
  writeFileSync(args.output, `${JSON.stringify(report, null, 2)}\n`);
  console.log(JSON.stringify({ output: args.output, api_build_sha: report.api_build_sha, passed: report.passed, failures: report.failures, statistics: Object.fromEntries(Object.entries(report.results).map(([park, result]) => [park, result.statistics])) }, null, 2));
  if (!report.passed && args['fail-on-threshold'] !== 'false') process.exitCode = 1;
}
