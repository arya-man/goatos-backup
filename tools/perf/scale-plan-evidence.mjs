#!/usr/bin/env node
import { readFileSync, writeFileSync } from "node:fs";

function args(argv) {
  const out = { plans: [] };
  for (let index = 0; index < argv.length; index += 1) {
    const key = argv[index];
    const value = argv[index + 1];
    if (key === "--plan") out.plans.push(value);
    else if (key.startsWith("--")) out[key.slice(2)] = value;
    index += 1;
  }
  return out;
}

export function inspectPlan(label, raw) {
  const parsed = JSON.parse(raw);
  const root = parsed[0]?.Plan;
  if (!root) return { label, passed: false, failures: ["missing root plan"] };
  const nodes = [];
  const visit = (node) => {
    nodes.push(node);
    for (const child of node.Plans ?? []) visit(child);
  };
  visit(root);
  const targetNodes = nodes.filter((node) =>
    [
      "process_integrity_projection_rows",
      "process_integrity_projection_summaries",
      "calendar_event_projections",
      "vaccination_shed_projection_rows",
    ].includes(node["Relation Name"]),
  );
  const failures = [];
  const boundedSummaryPlan = label === "process_integrity_summary";
  if (targetNodes.length === 0) failures.push("projection table is missing from plan");
  if (targetNodes.some((node) => node["Node Type"] === "Seq Scan") && !boundedSummaryPlan) failures.push("projection table uses a sequential scan");
  if (boundedSummaryPlan && targetNodes.some((node) => node["Node Type"] === "Seq Scan" && Number(node["Actual Rows"] ?? Number.POSITIVE_INFINITY) > 10_000)) {
    failures.push("process-integrity summary sequential scan exceeds 10,000 bounded rows");
  }
  if (!boundedSummaryPlan && !targetNodes.some((node) => ["Index Scan", "Index Only Scan", "Bitmap Heap Scan"].includes(node["Node Type"]))) {
    failures.push("projection table has no indexed access path");
  }
  const executionMs = Number(parsed[0]?.["Execution Time"] ?? Number.POSITIVE_INFINITY);
  if (executionMs > 1000) failures.push(`EXPLAIN ANALYZE execution ${executionMs}ms exceeds 1000ms`);
  if (Number(root["Actual Rows"] ?? 0) > 51) failures.push(`bounded query returned ${root["Actual Rows"]} rows`);
  return {
    label,
    passed: failures.length === 0,
    execution_ms: executionMs,
    actual_rows: Number(root["Actual Rows"] ?? 0),
    target_nodes: targetNodes.map((node) => ({
      node_type: node["Node Type"],
      index_name: node["Index Name"] ?? null,
      actual_rows: node["Actual Rows"] ?? null,
      shared_read_blocks: node["Shared Read Blocks"] ?? 0,
    })),
    failures,
  };
}

export function validateProcessIntegrityListSource(source) {
  const start = source.indexOf("const processIntegrityProjectionRowsSQL");
  const end = source.indexOf("const processIntegrityProjectionCountsSQL", start);
  if (start < 0 || end < 0) return ["cannot locate processIntegrityProjectionRowsSQL request query"];
  const listSQL = source.slice(start, end);
  if (/count\s*\(\s*\*\s*\)\s*over\s*\(/i.test(listSQL)) {
    return ["process-integrity request list uses COUNT(*) OVER before LIMIT"];
  }
  return [];
}

export function validateCardinality(cardinality) {
  const processRows = Number(cardinality?.process_integrity_projection_rows ?? 0);
  const processUniqueDueAt = Number(cardinality?.process_integrity_unique_due_at ?? 0);
  const processSummaryRows = Number(cardinality?.process_integrity_projection_summaries ?? 0);
  const processSummaryCoveredRows = Number(cardinality?.process_integrity_summary_covered_rows ?? 0);
  const processSummaryBusinessDates = Number(cardinality?.process_integrity_summary_business_dates ?? 0);
  const calendarRows = Number(cardinality?.calendar_event_projections ?? 0);
  const shedRows = Number(cardinality?.vaccination_shed_projection_rows ?? 0);
  const shedAnimals = Number(cardinality?.vaccination_shed_projection_animals ?? 0);
  const executionRows = Number(cardinality?.vaccination_execution_projection_rows ?? 0);
  const operationsRows = Number(cardinality?.vaccination_operations_projection_rows ?? 0);
  const failures = [];
  if (processRows < 1_000_000) failures.push("process-integrity projection has fewer than 1M rows");
  if (processUniqueDueAt < 1_000_000) failures.push("process-integrity adversarial fixture has fewer than 1M unique due_at values");
  if (processSummaryRows <= 0 || processSummaryRows > 10_000) failures.push("process-integrity summary cardinality is not within 1..10,000 rows");
  if (processSummaryCoveredRows !== processRows) failures.push("process-integrity summary does not cover every source projection row");
  if (processSummaryBusinessDates <= 0 || processSummaryBusinessDates > 366) failures.push("process-integrity summary business-date grain is not bounded to one leap-year window");
  if (calendarRows < 1_000_000) failures.push("calendar projection has fewer than 1M rows");
  if (shedRows < 1_000) failures.push("vaccination-shed projection has fewer than 1,000 shed rows");
  if (shedAnimals < 1_000_000) failures.push("vaccination-shed projection represents fewer than 1M animals");
  if (executionRows <= 0) failures.push("vaccination-execution local projection is empty");
  if (operationsRows <= 0) failures.push("vaccination-operations local projection is empty");

  const metadata = cardinality?.projection_metadata;
  for (const name of ["process_integrity", "calendar", "vaccination_shed", "vaccination_execution", "vaccination_operations"]) {
    const item = metadata?.[name];
    if (!item || typeof item !== "object") {
      failures.push(`${name} projection metadata is missing`);
      continue;
    }
    const age = Number(item.projection_age_seconds);
    const lag = Number(item.as_of_lag_seconds);
    if (!Number.isFinite(age) || age < 0 || age > 1800) failures.push(`${name} projection age is not within 0..1800s`);
    if (!Number.isFinite(lag) || lag < 0 || lag > 1800) failures.push(`${name} as-of lag is not within 0..1800s`);
    if (!item.freshness_status || !item.serving_state) failures.push(`${name} freshness/serving state is missing`);
  }

  for (const name of ["process_integrity", "calendar", "vaccination_shed", "vaccination_execution", "vaccination_operations"]) {
    const item = metadata?.[name];
    if (Number(item?.projection_version) <= 0 || Number(item?.projection_version) !== Number(item?.serving_projection_version)) {
      failures.push(`${name} serving projection version does not match the built version`);
    }
  }

  return {
    process_integrity_projection_rows: processRows,
    process_integrity_unique_due_at: processUniqueDueAt,
    process_integrity_projection_summaries: processSummaryRows,
    process_integrity_summary_covered_rows: processSummaryCoveredRows,
    process_integrity_summary_business_dates: processSummaryBusinessDates,
    calendar_event_projections: calendarRows,
    vaccination_shed_projection_rows: shedRows,
    vaccination_shed_projection_animals: shedAnimals,
    vaccination_execution_projection_rows: executionRows,
    vaccination_operations_projection_rows: operationsRows,
    projection_rows: processRows + calendarRows + shedRows,
    animal_equivalent_cardinality: Math.min(processRows, calendarRows, shedAnimals),
    projection_metadata: metadata ?? null,
    failures,
    passed: failures.length === 0,
  };
}

if (process.argv[1]?.endsWith("scale-plan-evidence.mjs")) {
  const options = args(process.argv.slice(2));
  if (!options.sha || !options.output || !options["cardinality-file"] || !options["pi-source-file"] || options.plans.length === 0) {
    console.error("usage: scale-plan-evidence.mjs --sha SHA --output FILE --cardinality-file FILE --pi-source-file FILE --plan label=file [--plan ...]");
    process.exit(2);
  }
  const cardinality = validateCardinality(JSON.parse(readFileSync(options["cardinality-file"], "utf8")));
  const results = options.plans.map((entry) => {
    const separator = entry.indexOf("=");
    return inspectPlan(entry.slice(0, separator), readFileSync(entry.slice(separator + 1), "utf8"));
  });
  const sourceGuardFailures = validateProcessIntegrityListSource(readFileSync(options["pi-source-file"], "utf8"));
  const report = {
    git_sha: options.sha,
    cardinality,
    certification_boundary: "scale_shaped_projection_smoke_not_full_chain_1m_certification",
    explicit_non_certifications: [
      "full_chain_1m_command_write_or_end_to_end_load",
      "fixture_load_timing",
      "projection_build_throughput",
      "pubsub_delivery",
      "dlq_recovery",
      "idempotency_under_concurrency",
      "consumer_backpressure",
    ],
    results,
    process_integrity_list_source_guard: {
      rejects_count_star_over_before_limit: true,
      failures: sourceGuardFailures,
      passed: sourceGuardFailures.length === 0,
    },
    passed: cardinality.passed && results.every((result) => result.passed) && sourceGuardFailures.length === 0,
  };
  writeFileSync(options.output, `${JSON.stringify(report, null, 2)}\n`);
  console.log(JSON.stringify(report, null, 2));
  if (!report.passed) process.exit(1);
}
