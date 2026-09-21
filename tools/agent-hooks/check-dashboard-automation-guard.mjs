import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { compareRoutes, discoverFilesystemRoutes, discoverSmokeRoutes } from "../dashboard-automation/discover-admin-routes.mjs";

const selfTest = process.argv.includes("--self-test");

if (selfTest) {
  const dir = mkdtempSync(join(tmpdir(), "dashboard-automation-guard-"));
  try {
    const appRoot = join(dir, "app", "(admin)");
    writeFileSync(joinWithDirs(appRoot, "new-surface", "page.tsx"), "export default function Page() { return null; }\n");
    const fsRoutes = discoverFilesystemRoutes(appRoot);
    const smokeRoutes = [{ name: "control-tower", path: "/?scope_mode=company" }];
    const result = compareRoutes(fsRoutes, smokeRoutes);
    assert.equal(result.missing.length, 1);
    assert.equal(result.missing[0].path, "/new-surface");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
  console.log("dashboard automation guard self-test: PASS");
  process.exit(0);
}

const comparison = compareRoutes();
const failures = [];
if (comparison.missing.length > 0) {
  failures.push(`admin-web route(s) missing deterministic smoke coverage: ${comparison.missing.map((route) => `${route.path} (${route.source})`).join(", ")}`);
}
failures.push(...dashboardBugPatternCoverageFindings());

const smokeRoutes = discoverSmokeRoutes();
const requiredNames = new Set(smokeRoutes.map((route) => route.name));
for (const required of ["work-board", "weighing-weights", "herd-signals", "action-center", "calendar"]) {
  if (!requiredNames.has(required)) failures.push(`smoke route inventory lost historically risky route: ${required}`);
}

for (const file of [
  "tools/dashboard-automation/config.json",
  "tools/dashboard-automation/bug-pattern-coverage.json",
  "tools/dashboard-automation/run.mjs",
  "tools/dashboard-automation/run-oci.sh",
  "tools/dashboard-automation/install-oci-user-timer.sh",
  "tools/dashboard-automation/check-business-data-parity.mjs",
  "tools/dashboard-automation/module-journeys.json",
  "tools/dashboard-automation/check-module-journeys.mjs",
  "tools/dashboard-automation/run-module-journeys.mjs",
  "tools/dashboard-automation/notify-slack.mjs",
  "tools/dashboard-automation/self-heal-pr.mjs",
  "docs/runbooks/dashboard-automation-oci.md",
]) {
  if (!existsSync(file)) failures.push(`required dashboard automation file missing: ${file}`);
}
failures.push(...dashboardRuntimeFindings());
failures.push(...dashboardModuleJourneyFindings());
failures.push(...dashboardStateContractFindings());

if (failures.length > 0) {
  console.error(failures.join("\n"));
  process.exit(1);
}

console.log(`dashboard automation guard: PASS (${comparison.filesystemRoutes.length} filesystem routes, ${smokeRoutes.length} smoke entries)`);

function dashboardBugPatternCoverageFindings() {
  const rel = "tools/dashboard-automation/bug-pattern-coverage.json";
  if (!existsSync(rel)) return [`required dashboard automation bug-pattern coverage file missing: ${rel}`];
  const coverage = JSON.parse(readFileSync(rel, "utf8"));
  const findings = [];
  const patterns = Array.isArray(coverage.patterns) ? coverage.patterns : [];
  const flows = Array.isArray(coverage.regularReadOnlyFlows) ? coverage.regularReadOnlyFlows : [];
  const domains = new Set(patterns.flatMap((item) => String(item.domain ?? "").split("-")).filter(Boolean));
  for (const required of ["backend", "admin", "web", "android"]) {
    if (!domains.has(required)) findings.push(`${rel}: bug pattern coverage must include ${required} fixes from the Aug 1 review window`);
  }
  for (const required of [
    "sql-bind-arity-and-control-flow",
    "admin-web-contract-and-known-failure-screens",
    "picker-url-state-and-tab-scope-drift",
    "mobile-webview-layout-and-touch-regressions",
    "api-fanout-and-latency-regression",
    "stg-oci-data-parity-and-field-reconciliation",
    "weighing-pen-alias-form-drift",
    "sop-authored-form-cross-client-drift",
    "android-proof-sync-session-and-ui-regressions",
  ]) {
    if (!patterns.some((item) => item.id === required)) findings.push(`${rel}: missing bug-pattern automation coverage entry ${required}`);
  }
  for (const item of patterns) {
    if (!Array.isArray(item.automationCoverage) || item.automationCoverage.length === 0) {
      findings.push(`${rel}: ${item.id ?? "unnamed pattern"} must list automationCoverage, not only prose`);
    }
    if (!item.ociNightlyCoverage) {
      findings.push(`${rel}: ${item.id ?? "unnamed pattern"} must say how OCI nightly covers or excludes it`);
    }
  }
  for (const requiredFlow of ["admin_web_full_surface_read_only_flow", "critical_business_data_read_flow", "dashboard_hot_api_latency_flow"]) {
    if (!flows.some((item) => item.name === requiredFlow)) findings.push(`${rel}: missing regular read-only flow coverage ${requiredFlow}`);
  }
  if (coverage.androidOciFeasibility?.defaultInDashboardAutomation !== false) {
    findings.push(`${rel}: Android emulator/device flow must stay disabled by default in dashboard OCI automation until host capacity is proven`);
  }
  if (coverage.slackAlerts?.channelId !== "C0C39G90FCJ") {
    findings.push(`${rel}: Slack alerts must target goatos-automation-alerts channel C0C39G90FCJ unless the OCI env overrides it`);
  }
  const weighingAliasPattern = patterns.find((item) => item.id === "weighing-pen-alias-form-drift");
  const manoharCases = Array.isArray(weighingAliasPattern?.manoharTestCases) ? weighingAliasPattern.manoharTestCases : [];
  const requiredManoharCases = [
    "single_physical_pen_row_in_shed_weights",
    "gain_span_days_matches_requested_window",
    "window_variance_changes_adg",
    "operational_location_display_not_doubled",
    "planner_create_round_trip_uses_alias_form_a",
    "no_form_b_when_alias_exists",
  ];
  for (const requiredCase of requiredManoharCases) {
    if (!manoharCases.some((item) => item.id === requiredCase)) {
      findings.push(`${rel}: weighing-pen-alias-form-drift must preserve Manohar test case ${requiredCase}`);
    }
  }
  const formBGuard = manoharCases.find((item) => item.id === "no_form_b_when_alias_exists");
  if (!String(formBGuard?.automation ?? "").includes("weighing_pen_alias_form_b_rows")) {
    findings.push(`${rel}: Manohar Form-B guard must point at the implemented weighing_pen_alias_form_b_rows sentinel`);
  }
  const roundTrip = manoharCases.find((item) => item.id === "planner_create_round_trip_uses_alias_form_a");
  if (roundTrip?.environment !== "disposable_preview_only") {
    findings.push(`${rel}: planner create/store round-trip is a write-path case and must stay marked disposable_preview_only, not STG/prod/OCI`);
  }
  return findings;
}

function dashboardRuntimeFindings() {
  const findings = [];
  const configRel = "tools/dashboard-automation/config.json";
  const parityRel = "tools/dashboard-automation/check-business-data-parity.mjs";
  const runnerRel = "tools/dashboard-automation/run.mjs";
  if (!existsSync(configRel) || !existsSync(parityRel) || !existsSync(runnerRel)) return findings;
  const config = JSON.parse(readFileSync(configRel, "utf8"));
  const paritySource = readFileSync(parityRel, "utf8");
  const runnerSource = readFileSync(runnerRel, "utf8");
  const moduleRunnerRel = "tools/dashboard-automation/run-module-journeys.mjs";
  const selfHealRel = "tools/dashboard-automation/self-heal-pr.mjs";
  const smokeRel = "apps/admin-web/scripts/smoke-visual-live.mjs";
  const moduleRunnerSource = existsSync(moduleRunnerRel) ? readFileSync(moduleRunnerRel, "utf8") : "";
  const selfHealSource = existsSync(selfHealRel) ? readFileSync(selfHealRel, "utf8") : "";
  const smokeSource = existsSync(smokeRel) ? readFileSync(smokeRel, "utf8") : "";

  for (const required of ["cbe_herd_analytics_window", "castro_reconciliation", "godel_2_timewise_adg", "weighing_pen_alias_form_b_rows", "sales_sold_weight_coverage"]) {
    const configured = config.businessDataParity?.sentinelQueries?.some((item) => item.name === required && item.implementationStatus === "implemented");
    if (!configured) findings.push(`${configRel}: sentinel ${required} must be configured as implemented`);
    if (!paritySource.includes(required)) findings.push(`${parityRel}: sentinel ${required} is configured but not invoked by the parity runner`);
  }

  for (const fragment of ["begin read only", "transaction_read_only", "default_transaction_read_only", "GOATOS_STG_READONLY_DATABASE_URL", "GOATOS_OCI_READONLY_DATABASE_URL", "alias_location_id", "shed_partitions"]) {
    if (!paritySource.includes(fragment)) findings.push(`${parityRel}: missing parity safety/alias fragment ${fragment}`);
  }

  if (config.businessDataParity?.enabledByDefault !== true) {
    findings.push(`${configRel}: business data parity must be default-on for OCI automation`);
  }
  if (config.slackAlerts?.enabledByDefault !== true) {
    findings.push(`${configRel}: Slack alerts must be default-on for OCI automation`);
  }
  if (config.selfHealing?.enabledByDefault !== true) {
    findings.push(`${configRel}: self-healing PR creation must be default-on for failing OCI automation`);
  }
  if (!runnerSource.includes("enabled(\"GOATOS_DASHBOARD_DATA_PARITY\"") || !runnerSource.includes("config.businessDataParity.enabledByDefault")) {
    findings.push(`${runnerRel}: business data parity must use config default, not an opt-in-only env gate`);
  }
  if (!runnerSource.includes("enabled(\"GOATOS_DASHBOARD_API_LATENCY\", true")) {
    findings.push(`${runnerRel}: API latency must be default-on unless explicitly disabled`);
  }
  if (!selfHealSource.includes("enabled(\"GOATOS_DASHBOARD_SELF_HEALING\", config.selfHealing.enabledByDefault")) {
    findings.push(`${selfHealRel}: self-healing PR creation must honor config default-on, not require env opt-in`);
  }

  for (const envFlag of ["GOATOS_DASHBOARD_DATA_PARITY", "GOATOS_DASHBOARD_API_LATENCY", "GOATOS_DASHBOARD_LIGHTHOUSE", "GOATOS_DASHBOARD_GRAFANA_SMOKE", "GOATOS_DASHBOARD_SLACK_ALERTS", "GOATOS_DASHBOARD_SELF_HEALING"]) {
    if (!runnerSource.includes(envFlag)) findings.push(`${runnerRel}: dashboard automation env flag ${envFlag} is not wired`);
  }
  for (const required of ["tools/perf/api-latency-gate.mjs", "apps/admin-web/scripts/capture-lighthouse.mjs", "tools/deploy/smoke-stg-grafana-dashboards.mjs", "tools/dashboard-automation/run-module-journeys.mjs", "tools/dashboard-automation/notify-slack.mjs", "tools/dashboard-automation/self-heal-pr.mjs"]) {
    if (!runnerSource.includes(required)) findings.push(`${runnerRel}: runner no longer invokes ${required}`);
  }
  if (!runnerSource.includes("GOATOS_SMOKE_READ_ONLY")) {
    findings.push(`${runnerRel}: production/post-main browser automation must force GOATOS_SMOKE_READ_ONLY=1`);
  }
  for (const fragment of ["GOATOS_SMOKE_MODULE_ASSERT_TEXT", "GOATOS_SMOKE_MODULE_SAFE_CLICKS"]) {
    if (!moduleRunnerSource.includes(fragment)) findings.push(`${moduleRunnerRel}: module runner must pass ${fragment} into Playwright`);
    if (!smokeSource.includes(fragment)) findings.push(`${smokeRel}: visual smoke must consume ${fragment}, not leave module manifest fields as metadata`);
  }
  for (const fragment of ["assertModuleTextObserved", "exerciseManifestSafeClicks"]) {
    if (!smokeSource.includes(fragment)) findings.push(`${smokeRel}: missing module journey enforcement helper ${fragment}`);
  }
  if (config.apiLatencyPolicy?.normalDashboardApisMustStayUnderMs !== 500) {
    findings.push(`${configRel}: normal dashboard APIs must stay under the 500ms policy`);
  }
  if (!Array.isArray(config.apiLatencyPolicy?.allowLongRunningPatterns) || !config.apiLatencyPolicy.allowLongRunningPatterns.includes("/imports")) {
    findings.push(`${configRel}: long-running import/export/upload exclusions must be explicit`);
  }
  return findings;
}

function dashboardModuleJourneyFindings() {
  const rel = "tools/dashboard-automation/module-journeys.json";
  if (!existsSync(rel)) return [`required dashboard automation module journey matrix missing: ${rel}`];
  const matrix = JSON.parse(readFileSync(rel, "utf8"));
  const findings = [];
  const modules = Array.isArray(matrix.modules) ? matrix.modules : matrix.journeys ?? [];
  const assigned = new Set(modules.flatMap((mod) => mod.routes ?? []));
  const smokeNames = new Set(discoverSmokeRoutes().map((route) => route.name));
  if (matrix.policy?.defaultReadOnly !== true && !Array.isArray(matrix.forbiddenActions)) {
    findings.push(`${rel}: dashboard automation journeys must declare read-only policy or forbiddenActions`);
  }
  if (matrix.policy?.viewports) {
    for (const required of ["laptop", "mobile"]) {
      if (!matrix.policy.viewports.includes(required)) findings.push(`${rel}: missing required viewport ${required}`);
    }
  }
  if (modules.length < 8) {
    findings.push(`${rel}: module journey coverage must stay module-wise; do not collapse it into a single generic smoke`);
  }
  for (const mod of modules) {
    if (!Array.isArray(mod.routes) || mod.routes.length === 0) findings.push(`${rel}: ${mod.id ?? "unknown module"} has no route ownership`);
    if (!Array.isArray(mod.requiredInteractions) && !Array.isArray(mod.coverage)) findings.push(`${rel}: ${mod.id ?? "unknown module"} has no requiredInteractions/coverage`);
    if (!Array.isArray(mod.forbiddenWrites) && !Array.isArray(matrix.forbiddenActions)) findings.push(`${rel}: ${mod.id ?? "unknown module"} has no forbiddenWrites/forbiddenActions`);
    if (!Array.isArray(mod.safeClicks) || mod.safeClicks.length === 0) {
      findings.push(`${rel}: ${mod.id ?? "unknown module"} has no manifest safe-click contract`);
    } else if (!mod.safeClicks.some((click) => click.requireObserved === true)) {
      findings.push(`${rel}: ${mod.id ?? "unknown module"} has no requireObserved safe-click contract`);
    }
    for (const route of mod.routes ?? []) {
      if (!smokeNames.has(route)) findings.push(`${rel}: ${mod.id} references unknown smoke route ${route}`);
    }
  }
  for (const route of smokeNames) {
    if (!assigned.has(route)) findings.push(`${rel}: smoke route ${route} is not owned by any module journey`);
  }
  return findings;
}

function dashboardStateContractFindings() {
  const findings = [];
  for (const rel of adminWebSourceFiles()) {
    const source = readFileSync(rel, "utf8");
    if (/defaultFrom[\s\S]{0,160}defaultTo[\s\S]{0,160}delete\([^)]*(?:param|toParam)/.test(source)) {
      findings.push(
        `${rel}: date/range URL state must not delete explicit picked params just because the pick equals a default. Absence can mean a derived server window; explicit choices must survive tabs, park, scope, and sibling filter changes.`,
      );
    }
    if (/defaultTo[\s\S]{0,160}defaultFrom[\s\S]{0,160}delete\([^)]*(?:param|toParam)/.test(source)) {
      findings.push(
        `${rel}: date/range URL state must not delete explicit picked params just because the pick equals a default. Absence can mean a derived server window; explicit choices must survive tabs, park, scope, and sibling filter changes.`,
      );
    }
  }
  const worklistFilters = "apps/admin-web/components/worklist-filters.tsx";
  if (existsSync(worklistFilters)) {
    const source = readFileSync(worklistFilters, "utf8");
    const applyRange = source.match(/function applyRange\([\s\S]*?\n  \}/)?.[0] ?? "";
    if (/from\s*===\s*field\.defaultFrom[\s\S]*to\s*===\s*field\.defaultTo/.test(applyRange)) {
      findings.push(
        `${worklistFilters}: daterange applyRange must not delete wt_from/wt_to just because a picked span equals defaultFrom/defaultTo. Absence can mean a derived server window, so picked URL state must stay pinned across park/scope/filter changes.`,
      );
    }
    if (!/next\.set\(field\.param,\s*from\);[\s\S]*next\.set\(field\.toParam,\s*to\);/.test(applyRange)) {
      findings.push(`${worklistFilters}: daterange applyRange must write both from/to params in one transition so half windows never hit the backend.`);
    }
  }
  return findings;
}

function adminWebSourceFiles() {
  const roots = ["apps/admin-web/app", "apps/admin-web/components", "apps/admin-web/features"];
  const files = [];
  for (const root of roots) walkFiles(root, files);
  return files.filter((file) => /\.(?:tsx|ts|jsx|js|mjs)$/.test(file));
}

function walkFiles(path, files) {
  if (!existsSync(path)) return;
  const stat = statSync(path);
  if (stat.isFile()) {
    files.push(path);
    return;
  }
  for (const entry of readdirSync(path)) {
    if (entry === "node_modules" || entry.startsWith(".")) continue;
    walkFiles(`${path}/${entry}`, files);
  }
}

function joinWithDirs(root, ...parts) {
  const full = join(root, ...parts);
  const dir = full.split("/").slice(0, -1).join("/");
  mkdirSync(dir, { recursive: true });
  return full;
}
