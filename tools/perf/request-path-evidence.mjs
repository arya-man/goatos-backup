#!/usr/bin/env node
import { readFileSync, writeFileSync } from "node:fs";

export const REQUIRED_PROJECTION_TABLES = Object.freeze([
  "process_integrity_projection_rows",
  "process_integrity_projection_summaries",
  "calendar_event_projections",
  "vaccination_shed_projection_rows",
  "vaccination_execution_projection_rows",
  "vaccination_operations_projection_rows",
]);

export const FORBIDDEN_SOURCE_TABLES = Object.freeze([
  "goats",
  "obligation_instances",
  "vaccination_completions",
]);

export function validateRequestPathUsage(stats) {
  const failures = [];
  const tables = stats?.tables;
  if (!tables || typeof tables !== "object" || Array.isArray(tables)) {
    return ["request-path table statistics are missing"];
  }
  for (const table of REQUIRED_PROJECTION_TABLES) {
    const observation = tables[table];
    if (!observation) {
      failures.push(`required projection table ${table} is missing`);
      continue;
    }
    const seqScans = Number(observation.seq_scan ?? 0);
    const indexScans = Number(observation.idx_scan ?? 0);
    if (indexScans + seqScans <= 0) failures.push(`${table} has no observed request-path read`);
  }
  for (const table of FORBIDDEN_SOURCE_TABLES) {
    const observation = tables[table] ?? {};
    const scans = Number(observation.seq_scan ?? 0) + Number(observation.idx_scan ?? 0);
    if (scans > 0) failures.push(`${table} was read ${scans} times during the projection-only request probe`);
  }
  return failures;
}

function parseArgs(argv) {
  const out = {};
  for (let index = 0; index < argv.length; index += 1) {
    if (!argv[index].startsWith("--")) continue;
    out[argv[index].slice(2)] = argv[index + 1];
    index += 1;
  }
  return out;
}

if (process.argv[1]?.endsWith("request-path-evidence.mjs")) {
  const args = parseArgs(process.argv.slice(2));
  if (!args["stats-file"] || !args.sha || !args.output) {
    console.error("usage: request-path-evidence.mjs --stats-file FILE --sha SHA --output FILE");
    process.exit(2);
  }
  const stats = JSON.parse(readFileSync(args["stats-file"], "utf8"));
  const failures = validateRequestPathUsage(stats);
  const report = {
    git_sha: args.sha,
    captured_at: stats.captured_at ?? null,
    probe_scope: "projection_backed_http_reads_only",
    required_projection_tables: REQUIRED_PROJECTION_TABLES,
    forbidden_source_tables: FORBIDDEN_SOURCE_TABLES,
    observations: stats.tables ?? null,
    failures,
    passed: failures.length === 0,
  };
  writeFileSync(args.output, `${JSON.stringify(report, null, 2)}\n`);
  console.log(JSON.stringify(report, null, 2));
  if (!report.passed) process.exit(1);
}
