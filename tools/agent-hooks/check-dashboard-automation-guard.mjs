import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { compareRoutes, discoverFilesystemRoutes, discoverSmokeRoutes } from "../dashboard-automation/discover-admin-routes.mjs";
import { coverageSelfTest, runCoverageGuard } from "../dashboard-automation/check-coverage-since-aug1.mjs";
import { EXECUTED_STATUSES, LANE_OPERATOR_HINT, OPERATOR_HINT, PARKED_STATUSES, describeLanePlan, describePlan, laneCoverageSelfTest, laneFindings, runSync, summarizeLanePlan, summarizePlan } from "../dashboard-automation/sync-coverage.mjs";

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
  coverageSelfTest();
  // Lanes 2-5: the read-only SQL contract, plain-English failure sentences, and the
  // exactly-once accounting that makes "every commit since Aug 1" a checkable claim.
  laneCoverageSelfTest();
  // needs-assertion / needs-review are parking statuses: the runner must never execute them.
  for (const parked of PARKED_STATUSES) {
    assert.ok(!EXECUTED_STATUSES.has(parked), `${parked} must not be an executed assertion status`);
  }
  const runnerSource = readFileSync("apps/admin-web/scripts/lib/feature-assertions.mjs", "utf8");
  assert.match(runnerSource, /\["assert", "data-dependent", "mobile-only"\]\.includes\(entry\.status\)/,
    "feature-assertions runner must keep filtering to executed statuses, so parked entries never run");
  const guardSource = readFileSync(new URL(import.meta.url), "utf8");
  assert.match(guardSource, /if \(strictSync\) \{\n  failures\.push\(\.\.\.sync\.findings\);/,
    "coverage-sync drift must stay non-blocking at landing (it caused the land-main rerun loop)");
  const runSource = readFileSync("tools/dashboard-automation/run.mjs", "utf8");
  assert.ok(runSource.includes('layer("coverage-sync"'), "OCI run must keep enforcing coverage-sync once the landing gate stops");
  console.log("dashboard automation guard self-test: PASS");
  process.exit(0);
}

const comparison = compareRoutes();
const failures = [];
if (comparison.missing.length > 0) {
  failures.push(`admin-web route(s) missing deterministic smoke coverage: ${comparison.missing.map((route) => `${route.path} (${route.source})`).join(", ")}`);
}
failures.push(...dashboardBugPatternCoverageFindings());
// Every feature / repeat-bug pattern shipped since 2026-08-01 must resolve to smoke routes, overlays,
// safeClicks and regression checks that actually run on OCI at laptop and mobile.
const coverage = await runCoverageGuard();
console.log(coverage.summary);
failures.push(...coverage.findings.map((finding) => `coverage-since-aug1: ${finding}`));

// Coverage must be SELF-UPDATING: new user-visible admin-web commits on origin/main, and
// assertions whose selector/copy has been renamed away, both have to be reported here rather
// than silently falling out of the smoke.
// Sync drift is about commits OTHER people already landed on origin/main, never the
// candidate's own diff. Failing the merge gate on it forced a ledger-only commit on
// every landing, which invalidated the exact-SHA receipt and re-ran the whole scope
// (Android included) in a loop whenever main moved mid-run. The OCI dashboard run
// (run.mjs `coverage-sync` layer) owns this check; here it warns unless --strict-sync.
const strictSync = process.argv.includes("--strict-sync") || process.env.DASHBOARD_GUARD_STRICT_SYNC === "1";
const sync = await runCoverageSyncFindings();
console.log(sync.summary);
if (strictSync) {
  failures.push(...sync.findings);
} else if (sync.findings.length > 0) {
  console.warn(`dashboard automation guard: WARN (not blocking landing; OCI coverage-sync layer enforces)\n${sync.findings.join("\n")}`);
}

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
  "tools/dashboard-automation/sync-coverage.mjs",
  "tools/dashboard-automation/coverage-state.json",
  "tools/dashboard-automation/commit-classification",
  "tools/dashboard-automation/commit-classification/lane2.jsonl",
  "tools/dashboard-automation/commit-classification/lane3.jsonl",
  "tools/dashboard-automation/commit-classification/lane4.jsonl",
  "tools/dashboard-automation/commit-classification/lane5-android.jsonl",
  "tools/dashboard-automation/commit-classification/not-automatable.jsonl",
  "tools/dashboard-automation/lane-checks.json",
  "tools/dashboard-automation/LANE-COVERAGE-REPORT.md",
  "tools/dashboard-automation/lane-coverage.test.mjs",
  "tools/dashboard-automation/notify-slack.mjs",
  "tools/dashboard-automation/self-heal-pr.mjs",
  "tools/dashboard-automation/refresh-firebase-token.mjs",
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

async function runCoverageSyncFindings() {
  try {
    const { plan, lanePlan } = await runSync({ mode: "check" });
    const findings = [];
    if (plan.newCommits.length || plan.stale.length) {
      findings.push(`coverage-sync: smoke coverage is behind origin/main — ${OPERATOR_HINT}\n${describePlan(plan)}`);
    }
    // Lanes 2-5 make a different promise from lane 1: not "this page is asserted", but "every
    // commit since the window opened is accounted for exactly once". A commit landing that no
    // lane covers fails here rather than quietly shrinking that claim.
    const lane = laneFindings(lanePlan ?? {});
    if (lane.length) findings.push(`lane-coverage: lanes 2-5 are behind origin/main — ${LANE_OPERATOR_HINT}\n${lane.join("\n")}`);
    const weak = describeLanePlan(lanePlan ?? {});
    const summary = [
      findings.length ? summarizePlan(plan) : `${summarizePlan(plan)} — PASS`,
      lanePlan ? summarizeLanePlan(lanePlan) : null,
      // Reported, never failed: a path-only tie means the check is the right one to build, but
      // the commit behind it should be read before anyone claims it proves that exact behaviour.
      weak ? `lane-coverage: how strongly each commit is tied to its check\n${weak}` : null,
    ].filter(Boolean).join("\n");
    return { summary, findings };
  } catch (error) {
    // No origin/main (shallow CI clone) must not silently pass: say so plainly.
    return { summary: "coverage-sync: could not compare against origin/main", findings: [`coverage-sync: ${String(error?.message ?? error)}`] };
  }
}

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

  if (config.businessDataParity?.enabledByDefault !== false) {
    findings.push(`${configRel}: live STG-to-OCI parity must be explicit-only; STG continuously moves and must not block production browser smoke`);
  }
  if (config.businessDataParity?.latestFullParityReceiptRequired !== true) {
    findings.push(`${configRel}: dashboard automation must require the latest full STG-to-OCI READBACK_PASS receipt before OCI-backed smoke`);
  }
  if (config.businessDataParity?.latestFullParityReceiptStatus !== "READBACK_PASS" || config.businessDataParity?.latestFullParityReceiptIncludedTableCount !== 293) {
    findings.push(`${configRel}: latest full parity receipt must require READBACK_PASS for 293 included business tables`);
  }
  for (const requiredExclusion of ["analytics.*", "public.audit_log", "public.domain_event_processed_events", "public.outbox_messages", "public.herd_signal_*"]) {
    if (!config.businessDataParity?.latestFullParityReceiptExcludedPatterns?.includes(requiredExclusion)) {
      findings.push(`${configRel}: latest full parity receipt must preserve exclusion ${requiredExclusion}`);
    }
  }
  if (config.businessDataParity?.criticalTableFingerprintRequired !== true) {
    findings.push(`${configRel}: critical business tables must require deterministic content fingerprints, not only row counts`);
  }
  if (config.businessDataParity?.fieldReconciliationsAreDatedEvidence !== true) {
    findings.push(`${configRel}: field reconciliations must be marked as dated evidence, not evergreen live truth`);
  }
  if (config.slackAlerts?.enabledByDefault !== true) {
    findings.push(`${configRel}: Slack alerts must be default-on for OCI automation`);
  }
  if (config.selfHealing?.enabledByDefault !== true) {
    findings.push(`${configRel}: self-healing PR creation must be default-on for failing OCI automation`);
  }
  if (!runnerSource.includes("enabled(\"GOATOS_DASHBOARD_DATA_PARITY\"") || !runnerSource.includes("config.businessDataParity.enabledByDefault")) {
    findings.push(`${runnerRel}: business data parity must remain explicitly overridable by env/config`);
  }
  for (const fragment of ["runtimePolicyForMode", "dataParityRequiredBeforeBrowser: false", "ran_degraded", "productionSmokeOk", "not_checked_read_only_smoke", "post-main certification must still collect read-only browser evidence", "degraded production smoke only failed parity prerequisites", "receipt.status === \"fail\" && enabled(\"GOATOS_DASHBOARD_SELF_HEALING\""]) {
    if (!runnerSource.includes(fragment)) {
      findings.push(`${runnerRel}: dashboard smoke must still collect read-only browser evidence when parity is red (${fragment})`);
    }
  }
  if (!runnerSource.includes("GOATOS_DASHBOARD_CERTIFICATION_EXTRAS") || !runnerSource.includes('mode !== "production-smoke" || enabled("GOATOS_DASHBOARD_CERTIFICATION_EXTRAS", false)')) {
    findings.push(`${runnerRel}: production smoke must keep broad certification extras explicit-only`);
  }
  if (!runnerSource.includes("runCertificationExtras && enabled(\"GOATOS_DASHBOARD_API_LATENCY\", true")) {
    findings.push(`${runnerRel}: API latency must remain default-on for certification runs but off for production-smoke unless certification extras are explicit`);
  }
  if (!runnerSource.includes("GOATOS_DASHBOARD_REQUIRE_API_SHA") || !runnerSource.includes("--allow-deployed-build")) {
    findings.push(`${runnerRel}: production smoke must latency-test the deployed prod API without requiring latest main SHA; post-main certification may still require exact SHA`);
  }
  if (!selfHealSource.includes("enabled(\"GOATOS_DASHBOARD_SELF_HEALING\", config.selfHealing.enabledByDefault")) {
    findings.push(`${selfHealRel}: self-healing PR creation must honor config default-on, not require env opt-in`);
  }
  if (!selfHealSource.includes("git([\"worktree\", \"add\"") || selfHealSource.includes("checkout\", \"--quiet\", \"-B\"")) {
    findings.push(`${selfHealRel}: self-healing must create report branches in a separate worktree, not mutate the runner checkout`);
  }
  for (const fragment of ["codePatchableFailure", "latest-full-parity-receipt", "shouldOpenFixPr", "safeToPatchCode", "precondition failure"]) {
    if (!selfHealSource.includes(fragment)) {
      findings.push(`${selfHealRel}: self-healing PR creation must skip non-code precondition failures and honor agent code-patchability (${fragment})`);
    }
  }

  for (const envFlag of ["GOATOS_DASHBOARD_DATA_PARITY", "GOATOS_DASHBOARD_CERTIFICATION_EXTRAS", "GOATOS_DASHBOARD_API_LATENCY", "GOATOS_DASHBOARD_LIGHTHOUSE", "GOATOS_DASHBOARD_GRAFANA_SMOKE", "GOATOS_DASHBOARD_VACCINATION_LIFECYCLE", "GOATOS_DASHBOARD_SLACK_ALERTS", "GOATOS_DASHBOARD_SELF_HEALING"]) {
    if (!runnerSource.includes(envFlag)) findings.push(`${runnerRel}: dashboard automation env flag ${envFlag} is not wired`);
  }
  const ociSource = readFileSync("tools/dashboard-automation/run-oci.sh", "utf8");
  if (!ociSource.includes("GOATOS_FIREBASE_REFRESH_TOKEN") || !ociSource.includes("refresh-firebase-token.mjs")) {
    findings.push("tools/dashboard-automation/run-oci.sh: OCI automation must refresh Firebase bearer tokens instead of relying on stale static GOATOS_BEARER_TOKEN");
  }
  for (const fragment of ["tableFingerprintSql", "row_to_json", "content_fingerprint", "table_parity_mismatch"]) {
    if (!paritySource.includes(fragment)) findings.push(`${parityRel}: critical table parity must include fingerprint fragment ${fragment}`);
  }
  for (const fragment of ["roleGrantProofSql", "has_table_privilege", "has_schema_privilege", "writePrivilegedTables"]) {
    if (!paritySource.includes(fragment)) findings.push(`${parityRel}: read-only proof must include catalog grant fragment ${fragment}`);
  }
  if (paritySource.includes("date '2026-09-21'") || paritySource.includes("date '2026-09-14'")) {
    findings.push(`${parityRel}: CBE herd window sentinel must use the current IST business date, not a stale September 2026 literal`);
  }
  const runbookSource = readFileSync("docs/runbooks/dashboard-automation-oci.md", "utf8");
  for (const fragment of ["Herd Signal tables remain best-effort", "Castro field reconciliations are dated evidence", "deterministic content fingerprints"]) {
    if (!runbookSource.includes(fragment)) findings.push(`docs/runbooks/dashboard-automation-oci.md: missing residual-risk/fingerprint note ${fragment}`);
  }
  for (const required of ["tools/dashboard-automation/check-latest-parity-receipt.mjs", "tools/perf/api-latency-gate.mjs", "apps/admin-web/scripts/capture-lighthouse.mjs", "tools/deploy/smoke-stg-grafana-dashboards.mjs", "tools/dashboard-automation/run-module-journeys.mjs", "tools/dashboard-automation/notify-slack.mjs", "tools/dashboard-automation/self-heal-pr.mjs"]) {
    if (!runnerSource.includes(required)) findings.push(`${runnerRel}: runner no longer invokes ${required}`);
  }
  const latestReceiptRel = "tools/dashboard-automation/check-latest-parity-receipt.mjs";
  const latestReceiptSource = existsSync(latestReceiptRel) ? readFileSync(latestReceiptRel, "utf8") : "";
  for (const fragment of ["READBACK_PASS", "includedTableCount", "stgReadOnly", "excludedPatterns", "maxAgeHours"]) {
    if (!latestReceiptSource.includes(fragment)) findings.push(`${latestReceiptRel}: latest parity receipt guard missing fragment ${fragment}`);
  }
  if (!runnerSource.includes("GOATOS_SMOKE_READ_ONLY")) {
    findings.push(`${runnerRel}: production/post-main browser automation must force GOATOS_SMOKE_READ_ONLY=1`);
  }
  for (const fragment of ["dataParityRequiredBeforeBrowser: false", "ran_degraded", "productionSmokeOk", "computeStatus", "production-module-journeys"]) {
    if (!runnerSource.includes(fragment)) {
      findings.push(`${runnerRel}: production read-only smoke must keep running Playwright in degraded mode when only STG-to-OCI parity prerequisites fail (${fragment})`);
    }
  }
  const notifyRel = "tools/dashboard-automation/notify-slack.mjs";
  const notifySource = existsSync(notifyRel) ? readFileSync(notifyRel, "utf8") : "";
  for (const fragment of ["degraded", "Browser", "Data parity", "ran_degraded", "Browser smoke did not run"]) {
    if (!notifySource.includes(fragment)) {
      findings.push(`${notifyRel}: Slack alerts must distinguish degraded browser-smoke runs from skipped Playwright runs (${fragment})`);
    }
  }
  for (const fragment of ["GOATOS_SMOKE_MODULE_ASSERT_TEXT", "GOATOS_SMOKE_MODULE_SAFE_CLICKS"]) {
    if (!moduleRunnerSource.includes(fragment)) findings.push(`${moduleRunnerRel}: module runner must pass ${fragment} into Playwright`);
    if (!smokeSource.includes(fragment)) findings.push(`${smokeRel}: visual smoke must consume ${fragment}, not leave module manifest fields as metadata`);
  }
  const slackRel = "tools/dashboard-automation/notify-slack.mjs";
  const slackSource = existsSync(slackRel) ? readFileSync(slackRel, "utf8") : "";
  for (const fragment of ["Browser", "Data parity", "ran_degraded", "ran_failed", "Browser smoke did not run"]) {
    if (!slackSource.includes(fragment)) findings.push(`${slackRel}: Slack alerts must explicitly say whether Playwright/browser smoke ran (${fragment})`);
  }
  if (smokeSource.includes('GOATOS_SMOKE_BROWSER_CHANNEL || "chrome"')) {
    findings.push(`${smokeRel}: live smoke must not default to the system Chrome channel; OCI uses Playwright's bundled Chromium unless GOATOS_SMOKE_BROWSER_CHANNEL is explicitly set`);
  }
  if (!smokeSource.includes("browserLaunchOptions")) {
    findings.push(`${smokeRel}: live smoke must derive browser launch options so OCI can use bundled Playwright Chromium`);
  }
  const runOciRel = "tools/dashboard-automation/run-oci.sh";
  const runOciSource = existsSync(runOciRel) ? readFileSync(runOciRel, "utf8") : "";
  if (!runOciSource.includes("CHROME_PATH") || !runOciSource.includes("chromium.executablePath")) {
    findings.push(`${runOciRel}: OCI runner must export CHROME_PATH from Playwright for Lighthouse when no system Chrome exists`);
  }
  for (const fragment of ["assertModuleTextObserved", "exerciseManifestSafeClicks"]) {
    if (!smokeSource.includes(fragment)) findings.push(`${smokeRel}: missing module journey enforcement helper ${fragment}`);
  }
  for (const fragment of ["assertWeighingTimeWindowApiSemantics", "assertFocusedPenAdgWindowSemantics", "assertShedWeightsGainSpanSemantics", "weighing ADG 14/21/28 day windows", "doubled operational location display"]) {
    if (!smokeSource.includes(fragment)) findings.push(`${smokeRel}: missing Manohar/Godel executable smoke guard ${fragment}`);
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
