import { readWeighingPolicy } from "../../../tools/perf/weighing-workload.mjs";
import { assertSmokeRouteIdentity, assertAnimalPurchaseHeading } from "./lib/smoke-route-identity.mjs";
import { validateLocalStackReceipt, validateSmokeActor } from "./lib/local-stack-receipt.mjs";
import { copyFileSync, existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, isAbsolute, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import AxeBuilder from "@axe-core/playwright";
import { TENANT_CONTEXT_HEADER } from "@goatos/api-client/constants";
import { chromium } from "@playwright/test";
import pixelmatch from "pixelmatch";
import { PNG } from "pngjs";

const appBaseUrl = trimTrailingSlash(process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3300");
const requiredEnv = ["GOATOS_API_BASE_URL", "GOATOS_BEARER_TOKEN", "GOATOS_TENANT_ID"];
const missing = requiredEnv.filter((key) => !process.env[key]);
const args = parseArgs(process.argv.slice(2));

if (missing.length > 0) {
  console.error(`Missing required live-smoke env: ${missing.join(", ")}`);
  process.exit(2);
}

const apiBaseUrl = trimTrailingSlash(process.env.GOATOS_API_BASE_URL);
const bearerToken = process.env.GOATOS_BEARER_TOKEN;
const tenantId = process.env.GOATOS_TENANT_ID;
const navigationTimeoutMs = Number(process.env.GOATOS_SMOKE_NAVIGATION_TIMEOUT_MS ?? 60_000);
const readOnlySmoke = process.env.GOATOS_SMOKE_READ_ONLY !== "0";
const moduleAssertText = parseJsonEnvArray("GOATOS_SMOKE_MODULE_ASSERT_TEXT");
const moduleSafeClicks = parseJsonEnvArray("GOATOS_SMOKE_MODULE_SAFE_CLICKS");
const observedModuleText = new Set();
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const baselineDir = normalizeRepoPath(args.baselineDir ?? process.env.GOATOS_VISUAL_BASELINE_DIR);
const updateBaseline = args.updateBaseline || process.env.GOATOS_VISUAL_UPDATE_BASELINE === "1";
const requireBaseline = args.requireBaseline || process.env.GOATOS_VISUAL_REQUIRE_BASELINE === "1";
const maxDiffRatio = args.maxDiffRatio ?? Number(process.env.GOATOS_VISUAL_MAX_DIFF_RATIO ?? "0.01");
const screenshotDir = join(
  repoRoot,
  ".codex-goatos-render",
  "admin-web-screenshots",
  new Date().toISOString().replaceAll(/[:.]/g, "-"),
);
const diffDir = join(screenshotDir, "diffs");
let baselineCompared = 0;
let baselineUpdated = 0;
const wideTableScrollOwnerSelector =
  ".tablewrap,.twrap,.cfgtablewrap,.feed-stock-tablewrap,.pa-gridwrap,.lt-tablewrap,.sales-market-wrap,.health-analytics-scroll,.cbm-future-table-wrap,.vplan .scroll";
const smokeWideWindowTo = new Date().toISOString().slice(0, 10);
const smokeWideWindowFrom = new Date(Date.now() - 43 * 24 * 60 * 60 * 1000).toISOString().slice(0, 10);

// Optional focused run: GOATOS_SMOKE_ONLY_ROUTES=calendar,counts-herd restricts the sweep to those
// routes so a targeted assertion (e.g. calendar identity) can run without an unrelated earlier route
// (e.g. a seed-empty Action Center) aborting the whole gate before Calendar is reached.
// Validate the selection UP FRONT — before waiting on the app or resolving any per-route fixture — so a
// typo (or a selection that matches nothing) fails immediately, not after an unrelated network lookup.
// Derived, never hand-maintained: a list that must be kept in step with another list
// eventually is not. The placeholder ids only shape two paths, never the names.
const KNOWN_ROUTE_NAMES = buildRoutes({
  toxinSopId: "placeholder",
  goatId: "placeholder",
  procurementLoadId: "placeholder",
  workflowRowId: "placeholder",
  calendarEventId: "placeholder",
  vaccinationShedPath: "/vaccination/execution/sheds/placeholder?scope_mode=company",
}).map((route) => route.name);
const onlyRoutesRaw = process.env.GOATOS_SMOKE_ONLY_ROUTES;
const onlyRoutes = (onlyRoutesRaw ?? "").split(",").map((s) => s.trim()).filter(Boolean);
// Present-but-empty (e.g. "," or whitespace) is an error: the caller asked to filter but named nothing.
// Only an entirely-unset var falls back to the full sweep.
if (onlyRoutesRaw !== undefined && onlyRoutes.length === 0) {
  throw new Error(
    `GOATOS_SMOKE_ONLY_ROUTES is set (${JSON.stringify(onlyRoutesRaw)}) but resolves to no route names. Unset it to run the full sweep, or name valid routes: ${KNOWN_ROUTE_NAMES.join(", ")}`,
  );
}
const unknownRoutes = onlyRoutes.filter((name) => !KNOWN_ROUTE_NAMES.includes(name));
if (unknownRoutes.length) {
  throw new Error(
    `GOATOS_SMOKE_ONLY_ROUTES has unknown route(s): ${unknownRoutes.join(", ")}. Valid routes: ${KNOWN_ROUTE_NAMES.join(", ")}`,
  );
}
const runsRoute = (name) => onlyRoutes.length === 0 || onlyRoutes.includes(name);

await waitForApp(appBaseUrl);
// Resolve per-route smoke fixtures lazily: only hit /goats/search or the procurement load lookup when a
// selected route actually needs it, so a focused `calendar` run never fails on an unrelated lookup.
const goatId = runsRoute("goat-passport") ? await resolveSmokeGoatID(apiBaseUrl, bearerToken, tenantId) : null;
const procurementLoadId = runsRoute("procurement-load-detail")
  ? await resolveSmokeProcurementLoadID(apiBaseUrl, bearerToken, tenantId)
  : null;
const workflowRowId = runsRoute("workflow-record") ? await resolveSmokeWorkflowRowID(apiBaseUrl, bearerToken, tenantId) : null;
const calendarEventId = runsRoute("calendar-drive-detail") ? await resolveSmokeCalendarEventID(apiBaseUrl, bearerToken, tenantId) : null;
const vaccinationShedPath = runsRoute("vaccination-shed-execution-detail")
  ? await resolveSmokeVaccinationShedPath(apiBaseUrl, bearerToken, tenantId)
  : null;
const toxinSopId = runsRoute("procurement-toxin-list") || runsRoute("procurement-toxin-flow")
  ? await resolveSmokeToxinSopID(apiBaseUrl, bearerToken, tenantId)
  : null;
mkdirSync(screenshotDir, { recursive: true });
if (baselineDir) mkdirSync(diffDir, { recursive: true });

// The routes this sweep visits, as a function of the ids two of them need.
//
// It is a function so the NAME LIST can be derived from it before those ids are resolved --
// the allow-list used to be a second hand-maintained copy and it drifted: counts-sops and
// counts-sops-builder were in this table, so a full sweep visited them, while a focused run
// naming either was rejected as an unknown route.
function buildRoutes({ toxinSopId, goatId, procurementLoadId, workflowRowId, calendarEventId, vaccinationShedPath }) {
  const routes = [
    { name: "control-tower", path: "/?scope_mode=company&lens=control-tower" },
    { name: "action-center", path: "/action-center?scope_mode=company" },
    { name: "action-center-verify", path: "/action-center?scope_mode=company&bucket=verify" },
    { name: "action-center-overdue", path: "/action-center?scope_mode=company&state=overdue" },
    { name: "action-center-due", path: "/action-center?scope_mode=company&state=due" },
    { name: "calendar", path: "/calendar?scope_mode=company&day=week" },
    { name: "calendar-month", path: "/calendar?scope_mode=company&view=month" },
    { name: "calendar-history", path: "/calendar?scope_mode=company&status=completed" },
    { name: "calendar-owner-pc", path: "/calendar?scope_mode=company&day=week&owner_key=pc" },
    { name: "protocol-adherence", path: "/protocol-adherence?scope_mode=company" },
    { name: "protocol-adherence-high", path: "/protocol-adherence?scope_mode=company&severity=high" },
    { name: "protocol-adherence-overdue", path: "/protocol-adherence?scope_mode=company&state=overdue" },
    { name: "work-board", path: "/work-board?scope_mode=company" },
    { name: "work-board-populated", path: "/work-board?scope_mode=company&date=2026-08-10" },
    // Alerts (2026-09-16): the page below the Work Board on both viewports, plus a populated day
    // on the STG-derived data (Coimbatore 2026-09-10 carries pen-feed and stock rows) whose run
    // also opens and closes the Configure drawer.
    { name: "alerts", path: "/alerts?scope_mode=company" },
    { name: "alerts-populated", path: "/alerts?scope_mode=company&date=2026-09-10&park=00000000-0000-4000-8000-000000003001" },
    { name: "workflows", path: "/workflows?scope_mode=company" },
    { name: "approvals", path: "/approvals?scope_mode=company" },
    { name: "approvals-approved", path: "/approvals?scope_mode=company&status=approved" },
    { name: "approvals-rejected", path: "/approvals?scope_mode=company&status=rejected" },
    { name: "verify", path: "/verify?scope_mode=company" },
    { name: "verify-all", path: "/verify?scope_mode=company&status=all" },
    { name: "verify-approved", path: "/verify?scope_mode=company&status=approved" },
    { name: "verify-rejected", path: "/verify?scope_mode=company&status=rejected" },
    { name: "verify-toxin", path: "/verify?scope_mode=company&toxin=1" },
    { name: "actions", path: "/actions?scope_mode=company" },
    { name: "verification", path: "/verification?scope_mode=company" },
    { name: "vaccination", path: "/vaccination?scope_mode=company" },
  {
    name: "vaccination-schedule",
    path: `/vaccination?scope_mode=company&view=schedule&schedule_year=${new Date().getFullYear()}`,
  },
    { name: "vaccination-execution", path: "/vaccination?scope_mode=company#execution" },
    { name: "vaccination-sheds-status-action", path: "/vaccination?scope_mode=company&sheds_status=needs_review#execution" },
    { name: "vaccination-sheds-capacity-action", path: "/vaccination?scope_mode=company&sheds_capacity=capacity_breach#execution" },
    { name: "vaccination-live-tracker", path: "/vaccination/live-tracker?scope_mode=company" },
    { name: "vaccination-plan", path: "/vaccination/plan?scope_mode=company" },
    { name: "vaccination-plan-edit", path: "/vaccination/plan/edit?scope_mode=company" },
    {
      name: "vaccination-shed-execution-detail",
      path: `${vaccinationShedPath ?? "/vaccination/execution/sheds/placeholder?scope_mode=company"}`,
    },
    { name: "procurement", path: "/procurement?scope_mode=company" },
    { name: "procurement-source-entry", path: "/procurement/source-entry?scope_mode=company" },
    { name: "procurement-source-entry-health-pending", path: "/procurement/source-entry?scope_mode=company&status=health_pending" },
    { name: "procurement-source-entry-arrival-review", path: "/procurement/source-entry?scope_mode=company&status=arrival_review" },
    { name: "procurement-source-entry-accepted-intake", path: "/procurement/source-entry?scope_mode=company&status=accepted_intake" },
    { name: "procurement", path: "/procurement?scope_mode=company" },
    { name: "procurement-vendors", path: "/procurement/vendors?scope_mode=company" },
    { name: "procurement-feed-purchases", path: "/procurement/feed-purchases?scope_mode=company" },
    { name: "procurement-animal-purchases", path: "/procurement/animal-purchases?scope_mode=company" },
    { name: "procurement-sops", path: "/procurement/sops?scope_mode=company" },
    { name: "procurement-toxin-list", path: `/procurement/sops?scope_mode=company&compose=1&edit=${encodeURIComponent(toxinSopId)}&view=list` },
    { name: "procurement-toxin-flow", path: `/procurement/sops?scope_mode=company&compose=1&edit=${encodeURIComponent(toxinSopId)}&view=flow` },
    { name: "sales", path: "/sales?scope_mode=company" },
    { name: "sales-sold", path: "/sales/sold?scope_mode=company" },
    { name: "sales-farm-value", path: "/sales/farm-value?scope_mode=company" },
    { name: "sales-loads", path: "/sales/loads?scope_mode=company" },
    { name: "sales-loads-farm-born", path: "/sales/loads?scope_mode=company&view=farm_born" },
    { name: "sales-market-analytics", path: "/sales/market-analytics?scope_mode=company" },
    { name: "sales-buyer-analytics", path: "/sales/buyer-analytics?scope_mode=company" },
    { name: "sales-farm-born", path: "/sales/farm-born?scope_mode=company" },
    { name: "sales-config", path: "/sales/config?scope_mode=company" },
    { name: "sales-sops", path: "/sales/sops?scope_mode=company" },
    { name: "sales-vendors", path: "/sales/vendors?scope_mode=company" },
    { name: "sales-sops", path: "/sales/sops?scope_mode=company" },
    { name: "feed-config", path: "/feed/config?scope_mode=company" },
    { name: "feed-analytics", path: "/feed/analytics?scope_mode=company" },
    { name: "feed-analytics-items", path: "/feed/analytics?scope_mode=company&tab=items" },
    { name: "feed-analytics-peranimal", path: "/feed/analytics?scope_mode=company&tab=peranimal" },
    { name: "feed-analytics-experiment", path: "/feed/analytics?scope_mode=company&tab=experiment" },
    { name: "feed-analytics-execution", path: "/feed/analytics?scope_mode=company&tab=execution" },
    { name: "feed-analytics-stock-only", path: "/feed/analytics?scope_mode=company&stock_only=1&tab=items" },
    { name: "feed-sops", path: "/feed/sops?scope_mode=company" },
    { name: "feed-direction", path: "/feed/direction?scope_mode=company" },
    { name: "feed-packing", path: "/feed/packing?scope_mode=company" },
    { name: "weighing-analytics", path: `/weighing/analytics?scope_mode=company&tab=general&wt_from=${smokeWideWindowFrom}&wt_to=${smokeWideWindowTo}` },
    { name: "weighing-analytics-breed", path: `/weighing/analytics?scope_mode=company&tab=breed&wt_from=${smokeWideWindowFrom}&wt_to=${smokeWideWindowTo}` },
    {
      name: "weighing-analytics-breed-wide",
      path: `/weighing/analytics?scope_mode=company&tab=breed&wt_from=${smokeWideWindowFrom}&wt_to=${smokeWideWindowTo}`,
    },
    { name: "weighing-analytics-birth", path: `/weighing/analytics?scope_mode=company&tab=birth&wt_from=${smokeWideWindowFrom}&wt_to=${smokeWideWindowTo}` },
    { name: "weighing-analytics-shed", path: `/weighing/analytics?scope_mode=company&tab=shed&wt_from=${smokeWideWindowFrom}&wt_to=${smokeWideWindowTo}` },
    { name: "weighing-analytics-weight", path: `/weighing/analytics?scope_mode=company&tab=weight&wt_from=${smokeWideWindowFrom}&wt_to=${smokeWideWindowTo}` },
    // The Feed by weight band card's second view and a table-level filter (maintainer request 2026-09-18).
    { name: "weighing-analytics-weight-not-shown", path: `/weighing/analytics?scope_mode=company&tab=weight&fb_view=unmatched&wt_from=${smokeWideWindowFrom}&wt_to=${smokeWideWindowTo}` },
    { name: "weighing-analytics-weight-band-filter", path: `/weighing/analytics?scope_mode=company&tab=weight&fb_band=25_30&fb_animals=all&wt_from=${smokeWideWindowFrom}&wt_to=${smokeWideWindowTo}` },
    { name: "weighing-analytics-time", path: `/weighing/analytics?scope_mode=company&tab=time&wt_from=${smokeWideWindowFrom}&wt_to=${smokeWideWindowTo}` },
    { name: "weighing-analytics-load", path: `/weighing/analytics?scope_mode=company&tab=load&wt_from=${smokeWideWindowFrom}&wt_to=${smokeWideWindowTo}` },
    { name: "weighing-analytics-fcr", path: `/weighing/analytics?scope_mode=company&tab=fcr&wt_from=${smokeWideWindowFrom}&wt_to=${smokeWideWindowTo}` },
    { name: "weighing-sops", path: "/weighing/sops?scope_mode=company" },
    { name: "weighing-weights", path: `/weighing/weights?scope_mode=company&wt_from=${smokeWideWindowFrom}&wt_to=${smokeWideWindowTo}` },
    { name: "counts-sops", path: "/counts/sops?scope_mode=company" },
    { name: "counts-sops-builder", path: "/counts/sops?compose=1&scope_mode=company" },
    { name: "counts-herd", path: "/counts/herd?scope_mode=company" },
    { name: "counts-analytics", path: "/counts/analytics?scope_mode=company" },
    { name: "counts-mortality", path: "/counts/mortality?scope_mode=company" },
    { name: "counts-breakdown", path: "/counts/breakdown?scope_mode=company" },
    { name: "counts-milk-preparation", path: "/counts/milk-preparation?scope_mode=company" },
    { name: "milk-sops", path: "/milk/sops?scope_mode=company" },
    { name: "configuration-items", path: "/configuration/items?scope_mode=company" },
    { name: "configuration-work-instructions", path: "/configuration/work-instructions?scope_mode=company" },
    { name: "herd-signals", path: "/herd-signals?scope_mode=company" },
    { name: "herd-signals-animals", path: "/herd-signals?scope_mode=company&hs_tab=animals" },
    { name: "herd-signals-mapping", path: "/herd-signals?scope_mode=company&hs_tab=mapping" },
    { name: "herd-signals-alerts", path: "/herd-signals?scope_mode=company&hs_tab=alerts" },
    { name: "herd-signals-gateways", path: "/herd-signals?scope_mode=company&hs_tab=gateways" },
    { name: "herd-signals-insights", path: "/herd-signals?scope_mode=company&hs_tab=insights" },
    { name: "health-analytics", path: "/health/analytics?scope_mode=company" },
    { name: "health-analytics-diseases", path: "/health/analytics?scope_mode=company&tab=diseases" },
    { name: "health-analytics-mortality", path: "/health/analytics?scope_mode=company&tab=mortality" },
    { name: "health-analytics-treatment", path: "/health/analytics?scope_mode=company&tab=treatment" },
    { name: "health-analytics-engine", path: "/health/analytics?scope_mode=company&tab=engine" },
    { name: "health-config", path: "/health/config?scope_mode=company" },
    { name: "configuration-items", path: "/configuration/items?scope_mode=company" },
    { name: "configuration-work-instructions", path: "/configuration/work-instructions?scope_mode=company" },
    { name: "operations-audit", path: "/operations/audit?scope_mode=company" },
    { name: "operations-audit-awaiting", path: "/operations/audit?scope_mode=company&status=verification_pending" },
    { name: "operations-audit-rejected", path: "/operations/audit?scope_mode=company&result=rejected" },
    { name: "operations-audit-proof-gaps", path: "/operations/audit?scope_mode=company&proof_gaps=true" },
    { name: "operations-dlq", path: "/operations/dlq?scope_mode=company" },
    { name: "operations-dlq-failed", path: "/operations/dlq?scope_mode=company&status=failed" },
    { name: "operations-dlq-discarded", path: "/operations/dlq?scope_mode=company&status=discarded" },
    { name: "people", path: "/people?scope_mode=company" },
    { name: "people-vaccination", path: "/people?scope_mode=company&tab=vaccination" },
    { name: "people-clock", path: "/people?scope_mode=company&tab=clock" },
    { name: "people-notifications", path: "/people?scope_mode=company&tab=notifications" },
    { name: "ceo-ai-admin", path: "/ceo-ai-admin?scope_mode=company" },
    { name: "routines", path: "/routines?scope_mode=company" },
    { name: "leave", path: "/leave?scope_mode=company" },
    { name: "leave-approved", path: "/leave?scope_mode=company&status=approved" },
    { name: "leave-rejected", path: "/leave?scope_mode=company&status=rejected" },
    { name: "leave-withdrawn", path: "/leave?scope_mode=company&status=withdrawn" },
    { name: "routines", path: "/routines?scope_mode=company" },
    { name: "tasks", path: "/tasks?scope_mode=company" },
    { name: "workflow-record", path: `/workflows/${encodeURIComponent(workflowRowId)}?scope_mode=company` },
    { name: "calendar-drive-detail", path: `/calendar/drive/${encodeURIComponent(calendarEventId)}?scope_mode=company` },
    { name: "goat-passport", path: `/goats/${encodeURIComponent(goatId)}` },
    {
      name: "procurement-load-detail",
      path: `/procurement/source-entry/loads/${encodeURIComponent(procurementLoadId)}?scope_mode=company`,
    },
  ];
  // The load-detail route needs a real load to visit; its NAME is still valid to ask for.
  return routes.filter((route) => {
    if (route.name === "procurement-load-detail") return Boolean(procurementLoadId);
    if (route.name === "workflow-record") return Boolean(workflowRowId);
    if (route.name === "calendar-drive-detail") return Boolean(calendarEventId);
    if (route.name === "vaccination-shed-execution-detail") return Boolean(vaccinationShedPath);
    return true;
  });
}

const routes = buildRoutes({ toxinSopId, goatId, procurementLoadId, workflowRowId, calendarEventId, vaccinationShedPath });

// Names were already validated up front against KNOWN_ROUTE_NAMES; resolve the selection to concrete
// routes. A requested route the run couldn't build (e.g. procurement-load-detail with no seeded load)
// fails loudly here rather than silently running fewer routes than asked for.
const selectedRoutes = onlyRoutes.length ? routes.filter((route) => onlyRoutes.includes(route.name)) : routes;
if (onlyRoutes.length) {
  const built = new Set(routes.map((route) => route.name));
  const unavailable = onlyRoutes.filter((name) => !built.has(name));
  if (unavailable.length) {
    throw new Error(`GOATOS_SMOKE_ONLY_ROUTES selected route(s) not available in this run: ${unavailable.join(", ")}`);
  }
}
if (selectedRoutes.length === 0) {
  throw new Error("GOATOS_SMOKE_ONLY_ROUTES selected zero routes");
}

const pagerMinimums = new Map([
  ["workflows", 1],
  ["verify", 1],
  ["vaccination", 1],
  ["vaccination-execution", 1],
  ["procurement-source-entry", 1],
  ["procurement-vendors", 1],
  ["procurement-feed-purchases", 1],
  ["sales-sold", 1],
  ["sales-loads", 1],
  ["vaccination-plan", 1],
  ["weighing-analytics", 1],
  ["weighing-weights", 1],
  ["counts-sops", 1],
  ["counts-herd", 1],
  ["operations-audit", 2],
  ["people", 1],
]);

const failureScreenMarkers = [
  "Something went wrong",
  "This screen failed to render",
  "backend_down",
  "Admin-web contract unavailable",
  "The board could not be loaded",
  "Weights could not be loaded",
];
const observedApiVersion = await fetchSmokeJson(`${apiBaseUrl}/version`, bearerToken, tenantId, "API build identity");
if (!observedApiVersion.build_sha || ["unknown", "dev"].includes(observedApiVersion.build_sha)) {
  throw new Error("API /version did not provide a verifiable build_sha");
}
const desiredApiBuild = process.env.GOATOS_SMOKE_API_BUILD_SHA;
if (desiredApiBuild && desiredApiBuild !== observedApiVersion.build_sha) {
  throw new Error(`API build mismatch: wanted ${desiredApiBuild}, observed ${observedApiVersion.build_sha}`);
}
const launchReceiptPath = process.env.GOATOS_LOCAL_STACK_RECEIPT_FILE;
const requiresLocalReceipt = new URL(appBaseUrl).hostname.match(/^(localhost|127\.0\.0\.1|\[::1\])$/)
  && (onlyRoutes.length === 0 || onlyRoutes.some((name) => /work-board|weighing/.test(name)));
if (requiresLocalReceipt && !launchReceiptPath) throw new Error("PR264 local browser proof requires GOATOS_LOCAL_STACK_RECEIPT_FILE from run-local-next");
const launchReceipt = launchReceiptPath ? validateLocalStackReceipt(JSON.parse(readFileSync(launchReceiptPath, "utf8")), {
  git_sha: observedApiVersion.build_sha, api_base_url: apiBaseUrl, admin_web_base_url: appBaseUrl,
}) : null;
const browserActor = launchReceipt ? validateSmokeActor(launchReceipt, bearerToken, tenantId) : null;
const weighingPolicyOptions = {baseUrl: apiBaseUrl, tenantId, bearerToken};
const weighsRoutes = selectedRoutes.some(({path}) => path.startsWith("/weighing/weights") || path.startsWith("/weighing/analytics"));
const weighingPolicy = weighsRoutes ? await readWeighingPolicy(weighingPolicyOptions) : null;
const browserEvidence = {
  ...(weighingPolicy ? {weighing_policy: weighingPolicy} : {}),
  schema_version: "1.0.0",
  same_api_build: Boolean(launchReceipt),
  local_stack_launch_receipt: launchReceipt,
  actor: browserActor,
  api_base_url: apiBaseUrl,
  admin_web_base_url: appBaseUrl,
  api_build_sha: observedApiVersion.build_sha,
  api_build_identity_source: "/version",
  routes: [],
};
const pageLoadBudgetMs = Number(process.env.GOATOS_SMOKE_PAGE_LOAD_BUDGET_MS || 8000);

const browser = await chromium.launch({ channel: process.env.GOATOS_SMOKE_BROWSER_CHANNEL || "chrome" });
try {
  for (const viewport of [
    { label: "laptop", width: 1440, height: 1000 },
    {
      label: "mobile",
      width: 390,
      height: 900,
      isMobile: true,
      hasTouch: true,
      deviceScaleFactor: 3,
      userAgent:
        "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1",
    },
  ]) {
    const context = await browser.newContext({
      viewport: { width: viewport.width, height: viewport.height },
      isMobile: Boolean(viewport.isMobile),
      hasTouch: Boolean(viewport.hasTouch),
      deviceScaleFactor: viewport.deviceScaleFactor ?? 1,
      userAgent: viewport.userAgent,
    });
    const cookieUrl = new URL(appBaseUrl);
    await context.addCookies([
      {
        name: "goatos_firebase_id_token",
        value: bearerToken,
        domain: cookieUrl.hostname,
        path: "/",
        httpOnly: true,
        sameSite: "Lax",
        expires: Math.floor(Date.now() / 1000) + 3600,
      },
    ]);
    for (const route of selectedRoutes) {
      if (route.viewports && !route.viewports.includes(viewport.label)) continue;
      console.log(`visual_route_start=${viewport.label}:${route.name}`);
      const page = await context.newPage();
      try {
        const url = `${appBaseUrl}${appPath(route.path)}`;
        const loadStartedAt = performance.now();
        const response = await gotoWithRetry(page, url);
        await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
        const pageLoadMs = Math.round(performance.now() - loadStartedAt);
        if (Number.isFinite(pageLoadBudgetMs) && pageLoadBudgetMs > 0 && pageLoadMs > pageLoadBudgetMs) {
          throw new Error(`${route.name} ${viewport.label} page load ${pageLoadMs}ms exceeded budget ${pageLoadBudgetMs}ms`);
        }
        if (!response) {
          await page.waitForURL(url, { timeout: 5_000 }).catch(() => undefined);
          if (page.url() !== url) {
            throw new Error(`${route.name} returned HTTP no-response for ${appPath(route.path)}`);
          }
        } else if (!response.ok()) {
          throw new Error(`${route.name} returned HTTP ${response.status()} for ${appPath(route.path)}`);
        }
        const actualPathname = assertSmokeRouteIdentity(url, page.url());
        const html = await page.content();
        const visibleText = await page.locator("body").innerText({ timeout: 5_000 }).catch(() => "");
        assertHealthyHTML(route.name, html, visibleText, bearerToken);
        markObservedModuleText(visibleText);
        const routeSignals = await assertRouteLoadedSignal(page, route.name, visibleText);
        browserEvidence.routes.push({
          name: route.name,
          route: appPath(route.path),
          actual_pathname: actualPathname,
          viewport: viewport.label,
          loaded: true,
          page_load_ms: pageLoadMs,
          page_load_budget_ms: pageLoadBudgetMs,
          forbidden_strings_absent: failureScreenMarkers,
          route_signals: routeSignals,
        });
        await assertLayoutHealthy(page, route.name, viewport.label);
        await assertMobileWideTableGestures(page, route.name, viewport.label, screenshotDir);
        await assertA11y(page, route.name, viewport.label);
        await assertTruncationContracts(page, route.name, viewport.label);
        await assertPaginationControls(page, route.name, viewport.label);
        await assertCoreInteractions(page, route.name, viewport.label);
        await exerciseManifestSafeClicks(page, route.name, viewport.label);
        await settleAtTop(page);
        const screenshotName = `${viewport.label}-${route.name}.png`;
        const screenshotPath = join(screenshotDir, screenshotName);
        await page.screenshot({ path: screenshotPath, fullPage: true });
        if (baselineDir) {
          compareOrUpdateBaseline(screenshotName, screenshotPath);
        }
        console.log(`visual_route_done=${viewport.label}:${route.name}`);
      } finally {
        await page.close().catch(() => {});
      }
    }
    await context.close();
  }
} finally {
  await browser.close();
}

assertModuleTextObserved();

writeFileSync(
  join(screenshotDir, "manifest.json"),
  JSON.stringify(
    {
      app_base_url: appBaseUrl,
      goat_id: goatId,
      procurement_load_id: procurementLoadId,
      workflow_row_id: workflowRowId,
      calendar_event_id: calendarEventId,
      vaccination_shed_path: vaccinationShedPath,
      routes: selectedRoutes.map((route) => appPath(route.path)),
      baseline_dir: baselineDir ? relativeToRepo(baselineDir) : null,
      baseline_compared: baselineCompared,
      baseline_updated: baselineUpdated,
      max_diff_ratio: baselineDir ? maxDiffRatio : null,
      module_assert_text: moduleAssertText,
      module_assert_text_observed: [...observedModuleText],
      module_safe_clicks: moduleSafeClicks,
    },
    null,
    2,
  ),
);
const finalApiVersion = await fetchSmokeJson(`${apiBaseUrl}/version`, bearerToken, tenantId, "Final API build identity");
if (finalApiVersion.build_sha !== observedApiVersion.build_sha) throw new Error("API build changed during browser E2E");
browserEvidence.api_build_sha_end = finalApiVersion.build_sha;
if (weighingPolicy) {
  browserEvidence.weighing_policy_end = await readWeighingPolicy(weighingPolicyOptions);
  if (JSON.stringify(weighingPolicy) !== JSON.stringify(browserEvidence.weighing_policy_end)) throw new Error("Weights page policy changed during browser proof");
}
if (launchReceipt) validateLocalStackReceipt(launchReceipt, {
  git_sha: observedApiVersion.build_sha, api_base_url: apiBaseUrl, admin_web_base_url: appBaseUrl,
});
writeFileSync(join(screenshotDir, "browser-evidence.json"), `${JSON.stringify(browserEvidence, null, 2)}\n`);

console.log(`screenshots_dir=${relativeToRepo(screenshotDir)}`);
console.log(`goat_id=${goatId}`);
console.log(`procurement_load_id=${procurementLoadId}`);
console.log(`workflow_row_id=${workflowRowId}`);
console.log(`calendar_event_id=${calendarEventId}`);
console.log(`vaccination_shed_path=${vaccinationShedPath}`);
console.log(`routes_captured=${selectedRoutes.map((route) => appPath(route.path)).join(",")}`);
if (baselineDir) {
  if (requireBaseline && !updateBaseline && baselineCompared === 0) {
    throw new Error(`Visual baseline was required but no screenshots were compared in ${relativeToRepo(baselineDir)}`);
  }
  console.log(`baseline_dir=${relativeToRepo(baselineDir)}`);
  console.log(`baseline_compared=${baselineCompared}`);
  console.log(`baseline_updated=${baselineUpdated}`);
}

async function waitForApp(url) {
  const deadline = Date.now() + 30_000;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(`${url}/`, { cache: "no-store" });
      if (response.ok) return;
      lastError = new Error(`status ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error(`admin-web did not respond at ${url}: ${lastError instanceof Error ? lastError.message : String(lastError)}`);
}

function appPath(path) {
  return path;
}

async function resolveSmokeGoatID(baseUrl, token, tenant) {
  if (process.env.GOATOS_SMOKE_GOAT_ID) {
    return process.env.GOATOS_SMOKE_GOAT_ID;
  }
  const response = await fetch(`${baseUrl}/goats/search?limit=1`, {
    headers: { Authorization: `Bearer ${token}`, [TENANT_CONTEXT_HEADER]: tenant },
    cache: "no-store",
  });
  if (!response.ok) {
    throw new Error(`backend smoke goat lookup failed: status ${response.status}`);
  }
  const body = await response.json();
  const goatID = body?.items?.[0]?.goat_id;
  if (typeof goatID !== "string" || goatID.length === 0) {
    throw new Error("backend smoke goat lookup returned no goat_id for passport smoke");
  }
  return goatID;
}

async function resolveSmokeProcurementLoadID(baseUrl, token, tenant) {
  const body = await fetchSmokeJson(`${baseUrl}/procurement/source-entry/loads?limit=1`, token, tenant, "procurement load lookup");
  const loadID = body?.items?.[0]?.load_id;
  return typeof loadID === "string" && loadID.length > 0 ? loadID : null;
}

async function resolveSmokeWorkflowRowID(baseUrl, token, tenant) {
  const body = await fetchSmokeJson(`${baseUrl}/vaccination/action-center?limit=1`, token, tenant, "workflow row lookup");
  const rowID = body?.items?.[0]?.row_id;
  return typeof rowID === "string" && rowID.length > 0 ? rowID : null;
}

async function resolveSmokeCalendarEventID(baseUrl, token, tenant) {
  const body = await fetchSmokeJson(
    `${baseUrl}/calendar/vaccination/events?limit=10&include_drive_summary=true`,
    token,
    tenant,
    "calendar event lookup",
  );
  const events = Array.isArray(body?.items) ? body.items : Array.isArray(body?.events) ? body.events : [];
  const event = events.find((item) => typeof item?.event_id === "string" && item.event_id.length > 0 && item?.drive_summary);
  const eventID = event?.event_id;
  return typeof eventID === "string" && eventID.length > 0 ? eventID : null;
}

async function resolveSmokeVaccinationShedPath(baseUrl, token, tenant) {
  const body = await fetchSmokeJson(`${baseUrl}/vaccination/sheds?limit=1`, token, tenant, "vaccination shed lookup");
  const row = body?.rows?.[0] ?? body?.items?.[0];
  const shedID = row?.shedId ?? row?.shed_id;
  const parkID = row?.parkId ?? row?.park_id;
  if (typeof shedID !== "string" || shedID.length === 0) return null;
  const search = new URLSearchParams();
  if (typeof parkID === "string" && parkID.length > 0) {
    search.set("scope_mode", "park");
    search.set("park", parkID);
  } else {
    search.set("scope_mode", "company");
  }
  return `/vaccination/execution/sheds/${encodeURIComponent(shedID)}?${search.toString()}`;
}

async function fetchSmokeJson(url, token, tenant, label) {
  const response = await fetch(url, {
    headers: { Authorization: `Bearer ${token}`, [TENANT_CONTEXT_HEADER]: tenant },
    cache: "no-store",
  });
  if (!response.ok) {
    throw new Error(`backend smoke ${label} failed: status ${response.status}`);
  }
  return response.json();
}

function assertHealthyHTML(routeName, html, visibleText, token) {
  const forbidden = [
    "Server configuration missing",
    "Bearer authentication failed",
    "Token is valid, but",
    "Backend service is not reachable",
    "Application error",
    "Runtime Error",
    "Trace ",
    "Rendered ",
    "Admin-web contract unavailable",
    "route_not_registered",
    "Forgot password?",
  ];
  for (const marker of forbidden) {
    if (html.includes(marker)) {
      throw new Error(`${routeName} rendered failure marker: ${marker}`);
    }
  }
  const visibleForbidden = failureScreenMarkers.filter((marker) => marker !== "Admin-web contract unavailable");
  for (const marker of visibleForbidden) {
    if (visibleText.includes(marker)) {
      throw new Error(`${routeName} rendered visible failure marker: ${marker}`);
    }
  }
  if (token && html.includes(token)) {
    throw new Error(`${routeName} rendered GOATOS_BEARER_TOKEN into HTML`);
  }
}

async function resolveSmokeToxinSopID(baseUrl, token, tenant) {
  const response = await fetch(`${baseUrl}/admin/sops?code_prefix=procurement.toxin_test&limit=10`, {
    headers: { Authorization: `Bearer ${token}`, [TENANT_CONTEXT_HEADER]: tenant },
  });
  if (!response.ok) throw new Error(`Toxin SOP fixture lookup failed: ${response.status()}`);
  const body = await response.json();
  const sop = body.items?.find((item) => item.code === "procurement.toxin_test");
  if (!sop?.sop_id) throw new Error("Responsive toxin coverage requires the published procurement.toxin_test SOP");
  return sop.sop_id;
}

async function assertRouteLoadedSignal(page, routeName, visibleText) {
  if (routeName === "procurement-toxin-list") {
    await page.getByTestId("toxin-step-1").waitFor({ state: "visible", timeout: 10000 });
    await page.getByTestId("toxin-add-step").waitFor({ state: "visible", timeout: 10000 });
    return { toxin_editor: "list", steps: await page.locator('[data-testid^="toxin-step-"]').count() };
  }
  if (routeName === "procurement-toxin-flow") {
    await page.getByTestId("flow-view").waitFor({ state: "visible", timeout: 10000 });
    await page.getByTestId("flow-node-toxin-step-1").click();
    await page.getByTestId("flow-props").getByTestId("toxin-step-1").waitFor({ state: "visible", timeout: 10000 });
    const clippedTitles = await page.locator(".toxin-flow .studio-node b").evaluateAll((elements) => elements.filter((element) => {
      const range = document.createRange();
      range.selectNodeContents(element);
      const text = range.getBoundingClientRect();
      const box = element.getBoundingClientRect();
      const card = element.closest(".studio-node").getBoundingClientRect();
      return text.top < box.top - 1 || text.bottom > box.bottom + 1 || element.scrollWidth > element.clientWidth + 1
        || box.top < card.top || box.bottom > card.bottom;
    }).map((element) => element.textContent));
    if (clippedTitles.length) throw new Error(`Toxin node titles are clipped: ${clippedTitles.join(", ")}`);
    return { toxin_editor: "flow", node_titles_unclipped: true };
  }
  const normalized = visibleText.replace(/\s+/g, " ").trim();
  if (routeName === "procurement-animal-purchases") {
    const heading = await page.locator("h1").innerText();
    return { has_animal_purchase_review: assertAnimalPurchaseHeading(heading) };
  }
  if (routeName === "work-board" || routeName === "work-board-populated") {
    const laneCounts = {
      todo: extractCountAfter(normalized, "TO DO"),
      in_progress: extractCountAfter(normalized, "IN PROGRESS"),
      in_review: extractCountAfter(normalized, "IN REVIEW"),
      done: extractCountAfter(normalized, "DONE"),
    };
    const hasLaneCounters = Object.values(laneCounts).every((value) => value !== null);
    const hasHealthyEmptyState = normalized.includes("No work on this board for the day.");
    const hasWorkCards = await page.locator(".card[data-filter-row]").count().catch(() => 0);
    const degraded = /Some work couldn't load right now/.test(normalized);
    if ((!hasLaneCounters && !hasWorkCards && !hasHealthyEmptyState) || degraded) {
      throw new Error(`${routeName} did not prove a loaded Work Board state`);
    }
    if (!hasWorkCards && !hasHealthyEmptyState) {
      throw new Error(`${routeName} has no cards but did not render the healthy empty-board copy`);
    }
    if (routeName === "work-board-populated" && hasWorkCards <= 0) {
      throw new Error("work-board-populated requires actual historical workload cards");
    }
    return { lane_counts: laneCounts, work_cards: hasWorkCards, degraded: false, healthy_empty_state: hasHealthyEmptyState };
  }
  if (routeName === "alerts" || routeName === "alerts-populated") {
    const kpis = { total: extractCountAfter(normalized, "ALERTS TODAY"), critical: extractCountAfter(normalized, "CRITICAL"), rules: extractCountAfter(normalized, "RULES CHECKED") };
    if (Object.values(kpis).some((value) => value === null)) {
      throw new Error(`${routeName} did not render the three Alerts KPIs: ${JSON.stringify(kpis)}`);
    }
    if (/could not be loaded/i.test(normalized)) {
      throw new Error(`${routeName} rendered the alerts error state`);
    }
    const rowCount = await page.locator('[data-testid="alerts-row"]').count().catch(() => 0);
    const emptyState = await page.locator('[data-testid="alerts-empty"]').count().catch(() => 0);
    if (rowCount === 0 && emptyState === 0) {
      throw new Error(`${routeName} has no alert rows but did not render the all-clear empty state`);
    }
    if (routeName === "alerts-populated" && rowCount === 0) {
      throw new Error("alerts-populated requires actual alert rows for the populated day");
    }
    return { alert_kpis: kpis, alert_rows: rowCount, empty_state: emptyState > 0 };
  }
  if (routeName === "weighing-weights") {
    if (!/Kids losing weight/i.test(normalized) || !/\bkg\b/i.test(normalized) || !/\bPage\b/i.test(normalized)) {
      throw new Error(`${routeName} did not prove loaded Weighing weights data`);
    }
    return { has_losing_weight_table: true };
  }
  if (routeName === "weighing-analytics") {
    if (!/Pens weighed:/i.test(normalized) || !/\bkg\b/i.test(normalized) || !/\bDaily gain\b/i.test(normalized)) {
      throw new Error(`${routeName} did not prove loaded Weighing analytics data`);
    }
    return { has_weighing_kpis: true, tab: "general" };
  }
  if (routeName === "weighing-analytics-breed" || routeName === "weighing-analytics-breed-wide") {
    if (!/Breed-wise/i.test(normalized) || !/\bkg\b/i.test(normalized) || !/Average weight/i.test(normalized)) {
      throw new Error(`${routeName} did not prove loaded breed-wise Weighing analytics data`);
    }
    return { has_breed_breakdown: true, tab: "breed", selected_window: routeName.endsWith("-wide") };
  }
  if (routeName === "weighing-analytics-birth") {
    if (!/Birth-wise/i.test(normalized) || !/Farm born vs purchased/i.test(normalized) || !/\bg\b/i.test(normalized)) {
      throw new Error(`${routeName} did not prove loaded birth-origin Weighing analytics data`);
    }
    return { has_origin_breakdown: true, tab: "birth" };
  }
  if (routeName === "weighing-analytics-shed") {
    if (!/Pen-wise/i.test(normalized) || !/Elevated vs ground pens/i.test(normalized) || !/\bg\b/i.test(normalized)) {
      throw new Error(`${routeName} did not prove loaded shed-type Weighing analytics data`);
    }
    return { has_shed_type_breakdown: true, tab: "shed" };
  }
  if (routeName === "weighing-analytics-weight") {
    if (!/Weight-wise/i.test(normalized) || !/\bkg\b/i.test(normalized) || !/Average weight/i.test(normalized)) {
      throw new Error(`${routeName} did not prove loaded weight-band Weighing analytics data`);
    }
    return { has_weight_band_breakdown: true, tab: "weight" };
  }
  if (routeName === "weighing-analytics-time") {
    if (!/Weekly growth/i.test(normalized) || !/\bkids\b/i.test(normalized) || !/\bg\b/i.test(normalized)) {
      throw new Error(`${routeName} did not prove loaded time-wise Weighing analytics data`);
    }
    return { has_weekly_growth: true, tab: "time" };
  }
  if (routeName === "weighing-analytics-load") {
    if (!/Purchased weight against/i.test(normalized) || !/Latest weighing/i.test(normalized) || !/At purchase/i.test(normalized)) {
      throw new Error(`${routeName} did not prove loaded load-wise Weighing analytics data`);
    }
    return { has_load_breakdown: true, tab: "load" };
  }
  return {};
}

function extractCountAfter(text, label) {
  const escaped = label.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = text.match(new RegExp(`${escaped}(?:\\s+[^\\d\\s]+)?\\s+(\\d+)`));
  return match ? Number(match[1]) : null;
}

async function assertLayoutHealthy(page, routeName, viewportLabel) {
  // Settle the page at the top BEFORE measuring. A hash route (e.g. /vaccination#execution) scrolls to its
  // anchor, and an in-flight smooth-scroll animation yields a transient negative main.top — a vertical
  // scroll artifact, not a layout defect. Force scroll-behavior to instant, scroll to the top, and let it
  // settle so the measurement reflects the resting layout. This does NOT touch the horizontal overflow or
  // card-clipping checks (they measure the same resting layout) — it only removes the main.top false positive.
  await settleAtTop(page);
  const layout = await page.evaluate((scrollOwnerSelector) => {
    window.scrollTo(0, 0);
    const root = document.documentElement;
    const overflow = root.scrollWidth - root.clientWidth;
    const main = document.querySelector("main")?.getBoundingClientRect();
    const panels = Array.from(document.querySelectorAll("section"))
      .filter(isVisible)
      .filter((element) => element.getBoundingClientRect().width > 120)
      .filter((element) => {
        const rect = element.getBoundingClientRect();
        return rect.left < -1 || rect.right > root.clientWidth + 1;
      })
      .slice(0, 5)
      .map(describeElement);
    const clippedControls = Array.from(document.querySelectorAll("a, button"))
      .filter(isVisible)
      .filter((element) => (element.textContent ?? "").trim().length > 0)
      .filter((element) => (element.scrollWidth > element.clientWidth + 2 || element.scrollHeight > element.clientHeight + 8) && !unclippedAvatarText(element))
      .slice(0, 5)
      .map(describeElement);
    const clippedNavLabels = Array.from(document.querySelectorAll('nav[aria-label^="Mesha"] span'))
      .filter(isVisible)
      .filter((element) => (element.textContent ?? "").trim().length > 0)
      .filter((element) => element.scrollWidth > element.clientWidth + 1)
      .slice(0, 5)
      .map(describeElement);
    const navLabelRects = Array.from(document.querySelectorAll('nav[aria-label^="Mesha"] a span:last-child, nav[aria-label^="Mesha"] button span:last-child'))
      .filter(isVisible)
      .filter((element) => (element.textContent ?? "").trim().length > 0)
      .map((element) => element.getBoundingClientRect());
    const navLabelLefts = navLabelRects.map((rect) => Math.round(rect.left));
    const navLabelSpread = navLabelLefts.length > 1 ? Math.max(...navLabelLefts) - Math.min(...navLabelLefts) : 0;
    // WCAG 2.5.8 (Target Size Minimum) exempts targets rendered INLINE within a sentence. True
    // display:inline prose anchors (e.g. ".lk" cross-references — "…ripples into Protocol Adherence and the
    // Control Tower.") are not standalone tap targets: they report a 0 content box and their wrapped-line
    // rects overlap. Exempt ONLY display:inline anchors from the small-target + overlap checks. Every real
    // control (buttons and .btn/.nav/.tab/.leaf/.lk.small links) renders inline-flex/block and stays checked.
    const interactives = Array.from(document.querySelectorAll('a[href], button:not([disabled]), input:not([type="hidden"]), select, textarea, [role="button"], [tabindex]:not([tabindex="-1"])'))
      .filter(isVisible)
      .filter((element) => !element.closest(".ceo-ai"))
      .filter((element) => !element.closest(".mzai-bubble"))
      .filter((element) => !(element.tagName === "A" && window.getComputedStyle(element).display === "inline"));
    const smallTargets = interactives
      .filter((element) => {
        const rect = element.getBoundingClientRect();
        const min = root.clientWidth < 600 ? 40 : 28;
        if (element instanceof HTMLInputElement && (element.type === "checkbox" || element.type === "radio")) {
          const label = element.closest("label");
          if (label) {
            const labelRect = label.getBoundingClientRect();
            return labelRect.width < min || labelRect.height < min;
          }
        }
        if (root.clientWidth < 600 && reachableStackAvatar(element)) {
          const halo = getComputedStyle(element, '::after');
          if (halo.content !== 'none' && halo.position === 'absolute'
            && ['top', 'right', 'bottom', 'left'].every((side) => halo[side] === '-5px')) return false;
        }
        return rect.width < min || rect.height < min;
      })
      .slice(0, 5)
      .map(describeElement);
    // Intentional Work Board assignee stack: only the designed 8px overlap is allowed,
    // and both independent button centers must still receive pointer hits.
    function reachableStackAvatar(element) {
      if (!element.matches('.wb .avs > button.av, .wb .avs > button.more')) return false;
      // The mobile toolbar intentionally scrolls horizontally. Prove the target
      // can be reached inside that scroll owner, then restore its position.
      const toolbar = element.closest('.wb .tbar');
      const previousScroll = toolbar?.scrollLeft;
      if (toolbar && ['auto', 'scroll'].includes(getComputedStyle(toolbar).overflowX)) {
        const box = toolbar.getBoundingClientRect(), button = element.getBoundingClientRect();
        if (button.left < box.left || button.right > box.right) toolbar.scrollLeft += button.left + button.width / 2 - box.left - box.width / 2;
      }
      const rect = element.getBoundingClientRect();
      const hit = document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2);
      const reachable = rect.width === 30 && (rect.height === 30 || (document.documentElement.clientWidth < 600 && rect.height === 40)) && !element.disabled
        && Boolean(hit && (hit === element || element.contains(hit)));
      if (toolbar) toolbar.scrollLeft = previousScroll;
      return reachable;
    }
    function unclippedAvatarText(element) {
      if (!reachableStackAvatar(element) || document.documentElement.clientWidth >= 600) return false;
      const halo = getComputedStyle(element, '::after');
      if (halo.content === 'none' || halo.position !== 'absolute'
        || !['top', 'right', 'bottom', 'left'].every((side) => halo[side] === '-5px')) return false;
      const box = element.getBoundingClientRect();
      const walker = document.createTreeWalker(element, NodeFilter.SHOW_TEXT);
      let found = false;
      for (let text = walker.nextNode(); text; text = walker.nextNode()) {
        if (!text.textContent.trim()) continue;
        found = true;
        const range = document.createRange();
        range.selectNodeContents(text);
        for (const rect of range.getClientRects()) {
          if (rect.left < box.left + element.clientLeft - 1 || rect.right > box.left + element.clientLeft + element.clientWidth + 1
            || rect.top < box.top + element.clientTop - 1 || rect.bottom > box.top + element.clientTop + element.clientHeight + 1) return false;
        }
      }
      return found;
    }
    // Scroll-canvas children keep their full layout boxes outside the clipped canvas.
    // Compare only painted areas, otherwise off-canvas nodes falsely overlap the
    // properties panel below them on mobile. Actual visible overlaps still fail.
    function clippedInteractiveRect(element) {
      const bounds = element.getBoundingClientRect();
      const rect = { left: bounds.left, right: bounds.right, top: bounds.top, bottom: bounds.bottom };
      for (let parent = element.parentElement; parent; parent = parent.parentElement) {
        const style = getComputedStyle(parent);
        const box = parent.getBoundingClientRect();
        if (/^(auto|scroll|hidden|clip)$/.test(style.overflowX)) {
          rect.left = Math.max(rect.left, box.left + parent.clientLeft);
          rect.right = Math.min(rect.right, box.left + parent.clientLeft + parent.clientWidth);
        }
        if (/^(auto|scroll|hidden|clip)$/.test(style.overflowY)) {
          rect.top = Math.max(rect.top, box.top + parent.clientTop);
          rect.bottom = Math.min(rect.bottom, box.top + parent.clientTop + parent.clientHeight);
        }
      }
      return rect;
    }
    function intentionalAvatarOverlap(first, second, xOverlap, yOverlap) {
      if (first.parentElement !== second.parentElement || !reachableStackAvatar(first) || !reachableStackAvatar(second)) return false;
      const a = first.getBoundingClientRect(), b = second.getBoundingClientRect();
      return xOverlap > 0 && xOverlap <= 8 && a.height === b.height && yOverlap === a.height && Math.abs(a.top - b.top) < 1;
    }
    const overlaps = [];
    for (let i = 0; i < interactives.length; i += 1) {
      for (let j = i + 1; j < interactives.length; j += 1) {
        const first = interactives[i];
        const second = interactives[j];
        if (first.contains(second) || second.contains(first)) continue;
        const a = clippedInteractiveRect(first);
        const b = clippedInteractiveRect(second);
        const xOverlap = Math.max(0, Math.min(a.right, b.right) - Math.max(a.left, b.left));
        const yOverlap = Math.max(0, Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top));
        if (xOverlap > 4 && yOverlap > 4 && !intentionalAvatarOverlap(first, second, xOverlap, yOverlap)) {
          overlaps.push({ first: describeElement(first), second: describeElement(second), xOverlap: Math.round(xOverlap), yOverlap: Math.round(yOverlap) });
        }
        if (overlaps.length >= 5) break;
      }
      if (overlaps.length >= 5) break;
    }
    const truncationTitleProblems = Array.from(document.querySelectorAll("[data-truncate]"))
      .filter(isVisible)
      .filter((element) => element.scrollWidth > element.clientWidth + 1 || element.scrollHeight > element.clientHeight + 1)
      .filter((element) => !hoverTextFor(element))
      .slice(0, 5)
      .map(describeElement);
    const truncationStyleProblems = Array.from(document.querySelectorAll("[data-truncate]"))
      .filter(isVisible)
      .filter((element) => {
        const style = window.getComputedStyle(element);
        const lineClamp = style.getPropertyValue("-webkit-line-clamp");
        return style.overflow !== "hidden" || (style.textOverflow !== "ellipsis" && lineClamp === "none");
      })
      .slice(0, 5)
      .map(describeElement);
    const mobileScrollProblems = root.clientWidth < 600 ? assertMobileWideContentScrolls(root) : [];

    return {
      overflow,
      mainInViewport: !main || (main.left >= -1 && main.right <= root.clientWidth + 1 && main.top >= -1),
      panels,
      clippedControls,
      clippedNavLabels,
      navLabelSpread,
      smallTargets,
      overlaps,
      truncationTitleProblems,
      truncationStyleProblems,
      mobileScrollProblems,
    };

    function isVisible(element) {
      if (element.closest("details:not([open])")) return false;
      const rect = element.getBoundingClientRect();
      const style = window.getComputedStyle(element);
      return rect.width > 0 && rect.height > 0 && style.visibility !== "hidden" && style.display !== "none";
    }

    function describeElement(element) {
      return {
        tag: element.tagName.toLowerCase(),
        className: element.getAttribute("class") || "",
        ariaLabel: element.getAttribute("aria-label") || "",
        text: (element.textContent ?? "").trim().replace(/\s+/g, " ").slice(0, 80),
        width: Math.round(element.getBoundingClientRect().width),
        height: Math.round(element.getBoundingClientRect().height),
        clientWidth: element.clientWidth,
        scrollWidth: element.scrollWidth,
        clientHeight: element.clientHeight,
        scrollHeight: element.scrollHeight,
      };
    }

    function hoverTextFor(element) {
      const own = element.getAttribute("title") || element.getAttribute("aria-label");
      if (own) return own;
      const labelled = element.closest("[title], [aria-label]");
      return labelled?.getAttribute("title") || labelled?.getAttribute("aria-label") || "";
    }

    function assertMobileWideContentScrolls(rootElement) {
      const problems = [];
      const wideTables = Array.from(document.querySelectorAll("table"))
        .filter(isVisible)
        .filter((table) => table.scrollWidth > rootElement.clientWidth + 2);
      for (const table of wideTables) {
        const scroller = table.closest(scrollOwnerSelector);
        if (!(scroller instanceof HTMLElement)) {
          problems.push({ kind: "missing-scroll-owner", table: describeElement(table) });
          continue;
        }
        const style = window.getComputedStyle(scroller);
        const canOverflow = /(auto|scroll)/.test(style.overflowX);
        const hasRoom = scroller.scrollWidth > scroller.clientWidth + 2;
        const before = scroller.scrollLeft;
        scroller.scrollLeft = Math.min(64, scroller.scrollWidth - scroller.clientWidth);
        const moved = scroller.scrollLeft !== before || before > 0;
        scroller.scrollLeft = before;
        const touchAction = style.touchAction;
        const allowsTouchPan = touchAction === "auto" || touchAction === "manipulation" || touchAction.includes("pan-x");
        if (!canOverflow || !hasRoom || !moved || !allowsTouchPan) {
          problems.push({
            kind: "bad-scroll-owner",
            scroller: describeElement(scroller),
            table: describeElement(table),
            overflowX: style.overflowX,
            touchAction,
            hasRoom,
            moved,
          });
        }
        if (problems.length >= 5) break;
      }
      return problems;
    }
  }, wideTableScrollOwnerSelector);

  if (layout.overflow > 2) {
    throw new Error(`${routeName} ${viewportLabel} has horizontal overflow of ${layout.overflow}px`);
  }
  if (!layout.mainInViewport) {
    throw new Error(`${routeName} ${viewportLabel} main content extends outside the viewport`);
  }
  if (layout.panels.length > 0) {
    throw new Error(`${routeName} ${viewportLabel} has cards/panels cut at the viewport edge: ${JSON.stringify(layout.panels)}`);
  }
  if (layout.clippedControls.length > 0) {
    throw new Error(`${routeName} ${viewportLabel} has clipped button/link text: ${JSON.stringify(layout.clippedControls)}`);
  }
  if (viewportLabel === "laptop" && layout.clippedNavLabels.length > 0) {
    throw new Error(`${routeName} ${viewportLabel} has clipped navigation labels: ${JSON.stringify(layout.clippedNavLabels)}`);
  }
  if (viewportLabel === "laptop" && layout.navLabelSpread > 1) {
    throw new Error(`${routeName} ${viewportLabel} has misaligned navigation labels; x spread ${layout.navLabelSpread}px`);
  }
  if (viewportLabel === "mobile" && layout.smallTargets.length > 0) {
    throw new Error(`${routeName} ${viewportLabel} has interactive targets below 40px: ${JSON.stringify(layout.smallTargets)}`);
  }
  if (layout.overlaps.length > 0) {
    throw new Error(`${routeName} ${viewportLabel} has overlapping interactive elements: ${JSON.stringify(layout.overlaps)}`);
  }
  if (layout.truncationTitleProblems.length > 0) {
    throw new Error(`${routeName} ${viewportLabel} has truncated text without hover/full text: ${JSON.stringify(layout.truncationTitleProblems)}`);
  }
  if (layout.truncationStyleProblems.length > 0) {
    throw new Error(`${routeName} ${viewportLabel} has malformed truncation styling: ${JSON.stringify(layout.truncationStyleProblems)}`);
  }
  if (layout.mobileScrollProblems.length > 0) {
    throw new Error(`${routeName} ${viewportLabel} has wide tables that cannot be horizontally scrolled on mobile: ${JSON.stringify(layout.mobileScrollProblems)}`);
  }
}

async function assertMobileWideTableGestures(page, routeName, viewportLabel, screenshotRoot) {
  if (viewportLabel !== "mobile") return;
  const scrollOwners = await page.locator(wideTableScrollOwnerSelector).evaluateAll((elements) =>
    elements
      .map((element, domIndex) => {
        if (!(element instanceof HTMLElement)) return null;
        const table = element.querySelector("table");
        if (!table) return null;
        const rect = element.getBoundingClientRect();
        const style = window.getComputedStyle(element);
        if (rect.width <= 0 || rect.height <= 0 || style.visibility === "hidden" || style.display === "none") return null;
        if (element.scrollWidth <= element.clientWidth + 2) return null;
        return {
          domIndex,
          label: element.getAttribute("aria-label") || table.getAttribute("aria-label") || table.textContent?.trim().replace(/\s+/g, " ").slice(0, 60) || "wide table",
        };
      })
      .filter(Boolean),
  );

  for (const owner of scrollOwners) {
    const locator = page.locator(wideTableScrollOwnerSelector).nth(owner.domIndex);
    await locator.evaluate((element) => {
      element.scrollLeft = 0;
      element.scrollIntoView({ block: "center", inline: "nearest" });
    });
    const box = await locator.boundingBox();
    if (!box) {
      throw new Error(`${routeName} mobile wide table scroll owner ${owner.label} has no bounding box`);
    }
    const before = await locator.evaluate((element) => element.scrollLeft);
    await dispatchTouchDrag(page, box);
    const afterDrag = await locator.evaluate((element) => element.scrollLeft);
    if (afterDrag <= before) {
      throw new Error(`${routeName} mobile wide table did not respond to horizontal drag: ${JSON.stringify(owner)}`);
    }
    await locator.evaluate((element) => {
      element.scrollLeft = element.scrollWidth;
    });
    await locator.screenshot({
      path: join(screenshotRoot, `${viewportLabel}-${routeName}-wide-table-${owner.domIndex}-right.png`),
    });
  }
}

async function dispatchTouchDrag(page, box) {
  const client = await page.context().newCDPSession(page);
  const y = box.y + Math.min(box.height * 0.55, box.height - 8);
  const startX = box.x + box.width * 0.82;
  const endX = box.x + box.width * 0.18;
  const steps = 8;
  await client.send("Input.dispatchTouchEvent", {
    type: "touchStart",
    touchPoints: [{ x: startX, y }],
  });
  for (let step = 1; step <= steps; step += 1) {
    const x = startX + ((endX - startX) * step) / steps;
    await client.send("Input.dispatchTouchEvent", {
      type: "touchMove",
      touchPoints: [{ x, y }],
    });
  }
  await client.send("Input.dispatchTouchEvent", {
    type: "touchEnd",
    touchPoints: [],
  });
  await client.detach();
}

async function settleAtTop(page) {
  await page.waitForFunction(() => Boolean(document.documentElement), null, { timeout: 5_000 });
  await page.evaluate(() => {
    const root = document.documentElement;
    if (root) root.style.scrollBehavior = "auto";
    if (document.body) document.body.style.scrollBehavior = "auto";
    window.scrollTo(0, 0);
  });
  await page.waitForFunction(() => window.scrollX === 0 && window.scrollY === 0, null, { timeout: 1_000 });
}

async function assertA11y(page, routeName, viewportLabel, includeSelector) {
  const builder = new AxeBuilder({ page });
  if (includeSelector) builder.include(includeSelector);
  const results = await analyzeA11yWithNavigationRetry(builder, page);
  const violations = results.violations.filter((violation) => violation.impact === "critical" || violation.impact === "serious");
  if (violations.length === 0) return;
  const summary = violations.slice(0, 5).map((violation) => ({
    id: violation.id,
    impact: violation.impact,
    description: violation.description,
    nodes: violation.nodes.slice(0, 3).map((node) => node.target),
  }));
  throw new Error(`${routeName} ${viewportLabel} has serious/critical accessibility violations: ${JSON.stringify(summary)}`);
}

async function analyzeA11yWithNavigationRetry(builder, page) {
  try {
    return await builder.analyze();
  } catch (error) {
    if (!(error instanceof Error) || !error.message.includes("Execution context was destroyed")) throw error;
    await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
    await settleAtTop(page);
    return await builder.analyze();
  }
}

async function assertTruncationContracts(page, routeName, viewportLabel) {
  void page;
  void routeName;
  void viewportLabel;
}

async function assertPaginationControls(page, routeName, viewportLabel) {
  const minimum = pagerMinimums.get(routeName);
  if (!minimum) return;

  const pagers = page.locator(".pager2");
  const count = await pagers.count();
  if (count === 0) {
    const bodyText = (await page.locator("body").innerText().catch(() => "")).replace(/\s+/g, " ");
    if (/0 rows|0 results|Nothing|No rows|No data/i.test(bodyText)) return;
    if (renderedAllRows(routeName, bodyText)) return;
    throw new Error(`${routeName} ${viewportLabel} expected at least ${minimum} pager2 footer(s), found none`);
  }
  if (count < minimum) {
    throw new Error(`${routeName} ${viewportLabel} expected at least ${minimum} pager2 footer(s) when pagination is rendered, found ${count}`);
  }
  for (let index = 0; index < count; index += 1) {
    const pager = pagers.nth(index);
    const text = (await pager.innerText()).replace(/\s+/g, " ").trim();
    if (!/(?:Prev(?:ious)?|Back)/i.test(text) || !/Next/i.test(text)) {
      throw new Error(`${routeName} ${viewportLabel} pager ${index + 1} is missing Prev/Next controls: ${text}`);
    }
  }

  if (viewportLabel !== "laptop" || process.env.GOATOS_VISUAL_EXERCISE_PAGERS !== "1") return;
  await exerciseFirstPagerRoundTrip(page, routeName);
}

function renderedAllRows(routeName, bodyText) {
  const match = bodyText.match(/\bShowing\b[^:]{0,120}:\s*([\d,]+)\s*\/\s*([\d,]+)/i);
  const salesLoadsMatch = routeName.startsWith("sales-loads")
    ? bodyText.match(/\b([\d,]+)\s*\/\s*([\d,]+)\s+recorded costs only\b/i)
    : null;
  const readout = match ?? salesLoadsMatch;
  if (!readout) return false;
  const shown = Number(readout[1].replace(/,/g, ""));
  const total = Number(readout[2].replace(/,/g, ""));
  return Number.isFinite(shown) && Number.isFinite(total) && total > 0 && shown >= total;
}

async function exerciseFirstPagerRoundTrip(page, routeName) {
  const pager = page.locator(".pager2").first();
  const initialPagerText = normalizePagerText(await pager.innerText());
  const next = pager.locator("a, button").filter({ hasText: /Next/i }).first();
  if ((await next.count()) !== 1) return;
  if (await isDisabledControl(next)) return;

  await next.scrollIntoViewIfNeeded();
  await next.click();
  await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
  await waitForPagerTextChange(page, initialPagerText);
  await waitForPagerControl(page, /Prev(?:ious)?/i, "enabled");

  const previous = page.locator(".pager2").first().locator("a, button").filter({ hasText: /Prev(?:ious)?/i }).first();
  if ((await previous.count()) !== 1 || (await isDisabledControl(previous))) {
    throw new Error(`${routeName} pager Next did not produce an enabled Previous control`);
  }
  await previous.scrollIntoViewIfNeeded();
  await previous.click();
  await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
  await waitForPagerAtFirstPage(page, routeName);
}

async function isDisabledControl(locator) {
  const ariaDisabled = await locator.getAttribute("aria-disabled");
  const disabled = await locator.getAttribute("disabled");
  return ariaDisabled === "true" || disabled !== null;
}

async function waitForPagerControl(page, pattern, state) {
  await page.waitForFunction(
    ({ source, flags, state }) => {
      const re = new RegExp(source, flags);
      const pager = document.querySelector(".pager2");
      if (!pager) return false;
      return Array.from(pager.querySelectorAll("a, button")).some((element) => {
        const text = element.textContent ?? "";
        if (!re.test(text)) return false;
        const disabled =
          element.getAttribute("aria-disabled") === "true" ||
          element.hasAttribute("disabled") ||
          (element instanceof HTMLButtonElement && element.disabled);
        return state === "enabled" ? !disabled : disabled;
      });
    },
    { source: pattern.source, flags: pattern.flags, state },
    { timeout: 5_000 },
  );
}

async function waitForPagerTextChange(page, previousText) {
  await page.waitForFunction(
    (previousText) => {
      const text = (document.querySelector(".pager2")?.textContent ?? "").replace(/\s+/g, " ").trim();
      return text && text !== previousText;
    },
    previousText,
    { timeout: 5_000 },
  );
}

async function waitForPagerAtFirstPage(page, routeName) {
  await page.waitForFunction(
    () => {
      const text = (document.querySelector(".pager2")?.textContent ?? "").replace(/\s+/g, " ").trim();
      return /^1-\d+ of /.test(text) || /\bPage 1\b/.test(text) || /^0 /.test(text) || /^0 results\b/.test(text);
    },
    { timeout: 5_000 },
  ).catch((error) => {
    throw new Error(`${routeName} pager did not return to the first page: ${error instanceof Error ? error.message : String(error)}`);
  });
}

function normalizePagerText(text) {
  return text.replace(/\s+/g, " ").trim();
}

async function assertCoreInteractions(page, routeName, viewportLabel) {
  // The visual smoke gate is not a full mock-fidelity claim, but it must still prove that the core mock
  // controls are not dead. Most checks only open/close overlays; Action Center also submits one seeded
  // row-versioned SOP verification so the acceptance path is proven through the browser.
  if (viewportLabel !== "laptop") {
    if (routeName === "control-tower") {
      await assertMobileSidebarNavigation(page, routeName);
    }
    return;
  }

  if (routeName === "counts-herd") {
    const hasHerdTable = await assertHerdIdentityColumns(page, routeName);
    await openDialogIfPresent(page, page.getByRole("button", { name: "Filters", exact: true }), "Filter — Counts / Herd", "Close filters", routeName);
    await openDialogIfPresent(page, page.getByRole("button", { name: "Register animal", exact: true }), "Register animal", "Close", routeName);
    await openDialogIfPresent(page, page.getByRole("button", { name: "Import sheet", exact: true }), "Import sheet", "Close", routeName);
    if (hasHerdTable) {
      await openAndCloseDrawer(
        page,
        page.locator('section:has-text("Herd") tbody tr .celllink').first(),
        "Animal Passport",
        routeName,
        assertHerdPassportIdentity,
      );
    }
  }

  if (routeName === "action-center") {
    const task = page.locator(".taskboard .task").first();
    if ((await task.count()) === 1) {
      await openAndCloseDrawer(page, task, "ACTION", routeName);
    }
    if (!readOnlySmoke) {
      await submitActionCenterVerification(page, routeName);
    }
  }

  if (routeName === "alerts-populated") {
    // The Configure drawer: open from the top-right button (an <a> only for alerts.configure
    // holders -- the CEO runs this smoke), prove both sections rendered, close via its X.
    const configure = page.locator('a[data-testid="alerts-configure-open"]');
    if ((await configure.count()) !== 1) {
      throw new Error(`${routeName} Configure button is not an enabled link for the smoke principal`);
    }
    await openAndCloseDrawer(page, configure, "Configure alerts", routeName, async (drawer) => {
      const builtin = await drawer.locator('[data-testid="alerts-rule-row"]').count();
      const events = await drawer.locator('[data-testid="alerts-event-rules"]').count();
      const addForm = await drawer.locator('[data-testid="alerts-event-new-add"]').count();
      if (builtin < 2 || events !== 1 || addForm !== 1) {
        throw new Error(`${routeName} Configure drawer incomplete: builtin=${builtin} events=${events} addForm=${addForm}`);
      }
    });
  }

  if (routeName === "calendar") {
    const drawerEvent = page.locator(".agenda .ev.celllink").first();
    const driveEvent = page.locator(".agenda .drivelink").first();
    if ((await drawerEvent.count()) === 1) {
      await openAndCloseDrawer(page, drawerEvent, "CALENDAR EVENT", routeName, assertCalendarTargetIdentity);
    } else if ((await driveEvent.count()) > 0) {
      await openCalendarDriveDetail(page, driveEvent, routeName);
    }
    // Month view + a month-cell (.mev) event open — exercised in-app so the top-bar scope is carried.
    const monthTab = page.getByRole("link", { name: "Month", exact: true });
    if ((await monthTab.count()) === 1) {
      await monthTab.click();
      await page.locator(".mcal").first().waitFor({ state: "visible", timeout: 5_000 });
      const mev = page.locator(".mcal .mev").first();
      if ((await mev.count()) === 1) {
        await openAndCloseDrawer(page, mev, "CALENDAR EVENT", routeName, assertCalendarTargetIdentity);
      }
    }
  }

  if (routeName === "procurement-source-entry") {
    const sourceLoad = page.locator('section:has-text("Supplier warmup") tbody tr .celllink').first();
    if ((await sourceLoad.count()) === 1) {
      await openAndCloseDrawer(page, sourceLoad, "SOURCE LOAD", routeName);
    }
  }

  if (routeName === "workflows") {
    const row = page.locator(".wfcat .wfrow").first();
    const rowCount = await row.count();
    if (rowCount === 1) {
      await row.scrollIntoViewIfNeeded();
      await row.click();
      await page.waitForURL(/workflow=/, { timeout: 5_000 });
      await page.getByRole("link", { name: /Back to list/i }).waitFor({ state: "visible", timeout: 5_000 });
      await page.getByRole("link", { name: /Open workflow detail/i }).first().waitFor({ state: "visible", timeout: 5_000 });
      await page.getByRole("link", { name: /Back to list/i }).click();
      await page.waitForURL((url) => !url.searchParams.has("workflow"), { timeout: 5_000 });
    }
  }

  if (routeName === "vaccination") {
    const sopQuickView = page.getByRole("button", { name: "SOP", exact: true });
    if ((await sopQuickView.count()) === 1) {
      await openAndCloseDialog(page, sopQuickView, "Vaccination Drive SOP", "Close", routeName);
    }
    if ((await page.getByRole("button", { name: "Import sheet", exact: true }).count()) > 0) {
      throw new Error(`${routeName} still exposes the removed Import sheet action`);
    }
    if ((await page.getByRole("button", { name: "New drive", exact: true }).count()) > 0) {
      throw new Error(`${routeName} still exposes the removed New drive action`);
    }

    if ((await page.getByText("Vaccination status matrix", { exact: true }).count()) > 0) {
      throw new Error(`${routeName} still renders the removed vaccination status matrix`);
    }
    if ((await page.getByText("Per-cohort vaccination detail", { exact: true }).count()) > 0) {
      throw new Error(`${routeName} still renders the removed per-cohort vaccination detail`);
    }

    const shedTable = page.locator("table.shed-summary-table").first();
    if ((await shedTable.count()) === 0) {
      return;
    }
    await shedTable.waitFor({ state: "visible", timeout: 10_000 });
    const shedSearch = page.locator('input[name="sheds_q"]');
    if ((await shedSearch.count()) !== 1) {
      throw new Error(`${routeName} expected one server-backed shed search input`);
    }
    const chipGroups = page.locator("section#sheds .chipset");
    if ((await chipGroups.count()) < 2) {
      throw new Error(`${routeName} expected status and capacity chip groups on the shed board`);
    }
    if ((await chipGroups.nth(0).locator("a.chip").count()) < 2 || (await chipGroups.nth(1).locator("a.chip").count()) < 2) {
      throw new Error(`${routeName} shed board status/capacity chips are missing`);
    }

    const firstShedLink = shedTable.locator("tbody tr .celllink").first();
    if ((await firstShedLink.count()) === 0) {
      return;
    }
    await firstShedLink.waitFor({ state: "visible", timeout: 10_000 });
    await firstShedLink.scrollIntoViewIfNeeded();
    await Promise.all([
      page.waitForURL((url) => url.pathname.startsWith("/vaccination/execution/sheds/"), { timeout: 10_000 }),
      firstShedLink.click(),
    ]);
    for (const label of ["Planned sessions", "Vaccine breakdown", "Animals in shed"]) {
      await page.getByText(label, { exact: true }).first().waitFor({ state: "visible", timeout: 10_000 });
    }
    const shedOverviewMetrics = await page.getByText("Shed overview", { exact: true }).first().evaluate((heading) => {
      const card = heading.closest("section.card");
      if (!(card instanceof HTMLElement)) throw new Error("Shed overview heading is not inside a card");
      const grid = card.querySelector(".shed-overview-grid");
      const stats = card.querySelector(".shed-overview-stats");
      const owners = card.querySelector(".shed-overview-owners");
      if (!(grid instanceof HTMLElement) || !(stats instanceof HTMLElement) || !(owners instanceof HTMLElement)) {
        throw new Error("Shed overview card is missing its compact grid layout");
      }
      return {
        cardWidth: Math.round(card.getBoundingClientRect().width),
        gridWidth: Math.round(grid.getBoundingClientRect().width),
        statsWidth: Math.round(stats.getBoundingClientRect().width),
        ownersWidth: Math.round(owners.getBoundingClientRect().width),
        gridHeight: Math.round(grid.getBoundingClientRect().height),
      };
    });
    if (shedOverviewMetrics.gridHeight < 86 || shedOverviewMetrics.ownersWidth < 240) {
      throw new Error(`${routeName} Shed overview renders as a loose/underbuilt summary: ${JSON.stringify(shedOverviewMetrics)}`);
    }
    if (shedOverviewMetrics.statsWidth > shedOverviewMetrics.ownersWidth * 2.25) {
      throw new Error(`${routeName} Shed overview stats consume the card and leave dead space: ${JSON.stringify(shedOverviewMetrics)}`);
    }
    const plannedSessionsMetrics = await page.getByText("Planned sessions", { exact: true }).first().evaluate((heading) => {
      const card = heading.closest("section.card");
      if (!(card instanceof HTMLElement)) throw new Error("Planned sessions heading is not inside a card");
      const body = card.querySelector(".planned-sessions-list");
      const row = card.querySelector(".planned-session-row");
      if (!(body instanceof HTMLElement) || !(row instanceof HTMLElement)) {
        throw new Error("Planned sessions card is missing its purpose-built session list");
      }
      const cardRect = card.getBoundingClientRect();
      const bodyRect = body.getBoundingClientRect();
      const rowRect = row.getBoundingClientRect();
      const bodyStyle = window.getComputedStyle(body);
      return {
        cardHeight: cardRect.height,
        bodyHeight: bodyRect.height,
        rowHeight: rowRect.height,
        overflowY: bodyStyle.overflowY,
      };
    });
    if (plannedSessionsMetrics.cardHeight < 150 || plannedSessionsMetrics.bodyHeight < 100 || plannedSessionsMetrics.rowHeight < 56) {
      throw new Error(`${routeName} Planned sessions renders as a cramped widget: ${JSON.stringify(plannedSessionsMetrics)}`);
    }
    if (plannedSessionsMetrics.overflowY !== "visible") {
      throw new Error(`${routeName} Planned sessions must not become a one-row scroll trap: ${JSON.stringify(plannedSessionsMetrics)}`);
    }
    await gotoWithRetry(page, `${appBaseUrl}${appPath("/vaccination?scope_mode=company")}`);
    await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
  }

  if (routeName === "vaccination-schedule") {
    const overflowToggle = page.locator(".schedule-vaccine-toggle").first();
    if ((await overflowToggle.count()) === 0) {
      return;
    }
    await overflowToggle.waitFor({ state: "visible", timeout: 10_000 });
    const collapsedLabel = (await overflowToggle.innerText()).replace(/\s+/g, " ").trim();
    if (!/^\+\d+ more$/.test(collapsedLabel)) {
      throw new Error(`${routeName} collapsed vaccine overflow label is ${JSON.stringify(collapsedLabel)}; expected "+N more"`);
    }
    if ((await overflowToggle.getAttribute("aria-expanded")) !== "false") {
      throw new Error(`${routeName} vaccine overflow must start collapsed`);
    }

    const before = await overflowToggle.evaluate((element) => {
      const row = element.closest("tr");
      if (!(row instanceof HTMLElement)) throw new Error("vaccine overflow toggle is not inside a table row");
      return { rowHeight: row.getBoundingClientRect().height, url: window.location.href };
    });
    await overflowToggle.click();
    await page.waitForFunction(
      (element) => element instanceof HTMLElement && element.getAttribute("aria-expanded") === "true",
      await overflowToggle.elementHandle(),
      { timeout: 5_000 },
    );

    const expanded = await overflowToggle.evaluate((element) => {
      const row = element.closest("tr");
      const cell = element.closest("td");
      const control = element.closest(".schedule-vaccine-control");
      const expandedChips = control?.querySelector(".schedule-vaccine-expanded-chips");
      if (!(row instanceof HTMLElement) || !(cell instanceof HTMLElement) || !(control instanceof HTMLElement)) {
        throw new Error("vaccine overflow control is not inside the expected schedule cell");
      }
      return {
        cellBackground: getComputedStyle(cell).backgroundColor,
        controlBackground: getComputedStyle(control).backgroundColor,
        expandedChipCount: expandedChips?.querySelectorAll(".vaccine-chip").length ?? 0,
        expandedChipsHidden: expandedChips instanceof HTMLElement ? expandedChips.hidden : true,
        label: element.textContent?.replace(/\s+/g, " ").trim() ?? "",
        rowHeight: row.getBoundingClientRect().height,
        url: window.location.href,
      };
    });
    if (expanded.label !== "Show less") {
      throw new Error(`${routeName} expanded vaccine overflow label is ${JSON.stringify(expanded.label)}; expected "Show less"`);
    }
    if (expanded.expandedChipsHidden || expanded.expandedChipCount < 1) {
      throw new Error(`${routeName} did not reveal its hidden vaccine chips`);
    }
    if (!isTransparentBackground(expanded.cellBackground) || !isTransparentBackground(expanded.controlBackground)) {
      throw new Error(
        `${routeName} paints an expanded-state background (cell=${expanded.cellBackground}, control=${expanded.controlBackground})`,
      );
    }
    if (Math.abs(expanded.rowHeight - before.rowHeight) > 1) {
      throw new Error(`${routeName} vaccine overflow changes row height by ${Math.abs(expanded.rowHeight - before.rowHeight).toFixed(2)}px`);
    }
    if (expanded.url !== before.url) {
      throw new Error(`${routeName} vaccine overflow changed the page URL`);
    }
    await assertLayoutHealthy(page, `${routeName}-expanded`, viewportLabel);
    await assertA11y(page, `${routeName}-expanded`, viewportLabel, ".schedule-vaccine-control");
  }
}

async function exerciseManifestSafeClicks(page, routeName, viewportLabel) {
  if (moduleSafeClicks.length === 0) return;
  if (!readOnlySmoke) {
    throw new Error(`${routeName} ${viewportLabel} refuses manifest safeClicks when read-only smoke is disabled`);
  }
  for (const click of moduleSafeClicks) {
    const locator = manifestClickLocator(page, click);
    if (!locator) continue;
    const count = await locator.count();
    if (count === 0) {
      if (!click.optional) throw new Error(`${routeName} ${viewportLabel} required manifest click target missing: ${JSON.stringify(click)}`);
      continue;
    }
    const target = locator.first();
    if (!(await target.isVisible().catch(() => false))) {
      if (!click.optional) throw new Error(`${routeName} ${viewportLabel} required manifest click target is hidden: ${JSON.stringify(click)}`);
      continue;
    }
    if (/^(close|cancel|delete|approve|reject|submit|save|create|new|verify)$/i.test(String(click.name ?? ""))) {
      if (!click.optional) throw new Error(`${routeName} ${viewportLabel} manifest click target is not read-only safe: ${JSON.stringify(click)}`);
      continue;
    }
    await target.scrollIntoViewIfNeeded().catch(() => {});
    await target.click({ timeout: 5_000 }).catch((error) => {
      if (!click.optional) throw error;
    });
    await page.keyboard.press("Escape").catch(() => {});
    await closeManifestOverlays(page);
  }
}

function manifestClickLocator(page, click) {
  if (click.testId) return page.getByTestId(String(click.testId));
  if (click.css) return page.locator(String(click.css));
  if (click.role && click.name) {
    return page.getByRole(String(click.role), { name: String(click.name), exact: Boolean(click.exact ?? true) });
  }
  if (click.text) return page.getByText(String(click.text), { exact: Boolean(click.exact ?? true) });
  return null;
}

async function closeManifestOverlays(page) {
  for (const close of [
    page.locator('button[aria-label^="Close"], a[aria-label^="Close"]').first(),
    page.getByRole("button", { name: /^Close$/ }).first(),
    page.getByRole("button", { name: /^Close filters$/ }).first(),
  ]) {
    if ((await close.count()) > 0 && (await close.isVisible().catch(() => false))) {
      await close.click({ timeout: 3_000 }).catch(() => {});
    }
  }
}

async function assertMobileSidebarNavigation(page, routeName) {
  const originalUrl = page.url();
  const menu = page.locator("button.hamb").first();
  if ((await menu.count()) !== 1) {
    throw new Error(`${routeName} mobile expected one mobile navigation menu button`);
  }
  await menu.click();
  await page.locator("aside.side.open").waitFor({ state: "visible", timeout: 5_000 });
  await page.waitForFunction(() => {
    const sidebar = document.querySelector("aside.side.open");
    return sidebar instanceof HTMLElement && getComputedStyle(sidebar).transform === "none";
  }, { timeout: 5_000 });
  const salesGroup = page.locator("aside.side.open .ggrp", { hasText: "Sales" }).first();
  if ((await salesGroup.count()) !== 1) {
    throw new Error(`${routeName} mobile expected the Sales sidebar group to be reachable`);
  }
  await salesGroup.click();
  const loadsLeaf = page.locator('aside.side.open a.leaf[href^="/sales/loads"]').first();
  if ((await loadsLeaf.count()) !== 1) {
    throw new Error(`${routeName} mobile expected the Sales / Loads leaf to be reachable after group expansion`);
  }
  await Promise.all([
    page.waitForURL((url) => url.pathname === "/sales/loads", { timeout: 10_000 }),
    loadsLeaf.click(),
  ]);
  await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
  if (await page.locator("aside.side.open").count()) {
    throw new Error(`${routeName} mobile sidebar stayed open after leaf navigation`);
  }
  await gotoWithRetry(page, originalUrl);
  await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
}

function isTransparentBackground(value) {
  return value === "transparent" || value === "rgba(0, 0, 0, 0)";
}

async function openCalendarDriveDetail(page, trigger, routeName) {
  await trigger.first().waitFor({ state: "visible", timeout: 10_000 }).catch(() => undefined);
  const triggerCount = await trigger.count();
  if (triggerCount !== 1) {
    throw new Error(`${routeName} drive detail trigger resolved to ${triggerCount} elements`);
  }
  const href = await trigger.getAttribute("href");
  if (!href) throw new Error(`${routeName} drive detail trigger has no href`);
  const expectedUrl = new URL(href, page.url());
  await trigger.scrollIntoViewIfNeeded();
  await Promise.all([
    page.waitForURL((url) => url.pathname === expectedUrl.pathname && url.search === expectedUrl.search, { timeout: 10_000 }),
    trigger.click(),
  ]);
  if (!page.url().includes("/calendar/drive/")) {
    throw new Error(`${routeName} drive detail did not navigate to /calendar/drive`);
  }
  const rosterHeading = page.getByText("Animal roster", { exact: true });
  if ((await rosterHeading.count()) === 0) {
    await page.goBack({ waitUntil: "domcontentloaded", timeout: 30_000 });
    await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
    return;
  }
  await rosterHeading.waitFor({ state: "visible", timeout: 10_000 });
  const headers = (await page.locator("table thead th").allInnerTexts()).map((h) => h.trim());
  const expected = ["Display ID", "Shed", "Tag 1", "Tag 2"];
  if (headers.slice(0, 4).map(comparableHeader).join("|") !== expected.map(comparableHeader).join("|")) {
    throw new Error(`${routeName} calendar drive detail roster must start with ${expected.join(", ")}; got ${headers.join(", ")}`);
  }
  for (const banned of ["Animal ID 1", "Animal ID 2", "missing ID"]) {
    if ((await page.locator("body").innerText()).includes(banned)) {
      throw new Error(`${routeName} calendar drive detail contains banned identity wording "${banned}"`);
    }
  }
  await page.goBack({ waitUntil: "domcontentloaded", timeout: 30_000 });
  await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
}

async function submitActionCenterVerification(page, routeName) {
  const queueLink = page.locator('a[href*="bucket=verify"]').first();
  const queueLinkCount = await queueLink.count();
  if (queueLinkCount === 0) {
    return;
  }
  if (queueLinkCount !== 1) {
    throw new Error(`${routeName} SOP queue link resolved to ${queueLinkCount} elements`);
  }
  await queueLink.scrollIntoViewIfNeeded();
  await Promise.all([
    page.waitForURL((url) => url.pathname === "/action-center" && url.searchParams.get("bucket") === "verify", { timeout: 10_000 }),
    queueLink.click(),
  ]);
  await page.waitForLoadState("networkidle", { timeout: 10_000 }).catch(() => {});

  const verifyButton = page.locator('form button:not([disabled])').filter({ hasText: /^Verify$/ }).first();
  const verifyCount = await verifyButton.count();
  if (verifyCount !== 1) {
    const emptyQueue = await page.getByText("Nothing awaiting verification.", { exact: false }).count();
    if (verifyCount === 0 && emptyQueue === 1) {
      return;
    }
    const bodyText = await page.locator("body").innerText().catch(() => "");
    throw new Error(`${routeName} expected one enabled SOP Verify button or a coherent empty verification queue, found ${verifyCount}; body=${bodyText.replace(/\s+/g, " ").slice(0, 800)}`);
  }
  await verifyButton.scrollIntoViewIfNeeded();
  await Promise.all([
    page.waitForURL((url) => url.searchParams.get("action_status") === "success", { timeout: 15_000 }),
    verifyButton.click(),
  ]);
  await page.waitForLoadState("networkidle", { timeout: 10_000 }).catch(() => {});
  await page.locator(".note").filter({ hasText: /success|verified|accepted/i }).first().waitFor({ state: "visible", timeout: 10_000 });
}

async function openAndCloseDialog(page, trigger, dialogLabel, closeName, routeName) {
  const triggerCount = await trigger.count();
  if (triggerCount !== 1) {
    throw new Error(`${routeName} trigger for "${dialogLabel}" resolved to ${triggerCount} elements`);
  }
  await trigger.click();
  const dialog = page.locator(`[role="dialog"][aria-label="${cssString(dialogLabel)}"]`);
  await dialog.waitFor({ state: "visible", timeout: 5_000 });
  const close = dialog.locator(`button[aria-label="${cssString(closeName)}"]`);
  const closeCount = await close.count();
  if (closeCount !== 1) {
    throw new Error(`${routeName} close button for "${dialogLabel}" resolved to ${closeCount} elements`);
  }
  const topmost = await close.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    const top = document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2);
    return top === element || element.contains(top);
  });
  if (!topmost) {
    throw new Error(`${routeName} close button for "${dialogLabel}" is covered by another layer`);
  }
  await close.click();
  await dialog.waitFor({ state: "hidden", timeout: 5_000 });
}

async function openDialogIfPresent(page, trigger, dialogLabel, closeName, routeName) {
  if ((await trigger.count()) === 0) return;
  await openAndCloseDialog(page, trigger, dialogLabel, closeName, routeName);
}

async function openAndCloseDrawer(page, trigger, expectedText, routeName, inspectDrawer) {
  await trigger.first().waitFor({ state: "visible", timeout: 10_000 }).catch(() => undefined);
  const triggerCount = await trigger.count();
  if (triggerCount !== 1) {
    throw new Error(`${routeName} drawer trigger for "${expectedText}" resolved to ${triggerCount} elements`);
  }
  const href = await trigger.getAttribute("href").catch(() => null);
  const expectedUrl = href ? new URL(href, page.url()) : null;
  await trigger.scrollIntoViewIfNeeded();
  await Promise.all([
    expectedUrl
      ? page.waitForURL(
          (url) => url.pathname === expectedUrl.pathname && url.search === expectedUrl.search,
          { timeout: 10_000 },
        )
      : Promise.resolve(),
    trigger.click(),
  ]);
  const drawer = page.locator(".drawer.on").first();
  try {
    await drawer.waitFor({ state: "visible", timeout: 10_000 });
  } catch (error) {
    if (!expectedUrl || page.url() !== expectedUrl.toString()) {
      throw error;
    }
    await page.reload({ waitUntil: "domcontentloaded", timeout: 30_000 });
    await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
    try {
      await drawer.waitFor({ state: "visible", timeout: 10_000 });
    } catch (reloadError) {
      const taskCount = await page.locator(".taskboard .task").count().catch(() => -1);
      const bodyText = await page.locator("body").innerText().catch(() => "");
      throw new Error(
        `${routeName} drawer "${expectedText}" did not render after click+reload. url=${page.url()} href=${expectedUrl.toString()} tasks=${taskCount} body=${bodyText
          .replace(/\s+/g, " ")
          .slice(0, 800)}`,
        { cause: reloadError },
      );
    }
  }
  await page.waitForFunction(() => {
    const openDrawer = document.querySelector(".drawer.on");
    return openDrawer instanceof HTMLElement && getComputedStyle(openDrawer).transform === "none";
  });
  await drawer.getByText(expectedText, { exact: false }).first().waitFor({ state: "visible", timeout: 5_000 });
  if (inspectDrawer) {
    await inspectDrawer(drawer, routeName, expectedText);
  }
  const close = drawer.locator('a[aria-label^="Close"], button[aria-label^="Close"]').first();
  const closeCount = await close.count();
  if (closeCount !== 1) {
    throw new Error(`${routeName} close control for drawer "${expectedText}" resolved to ${closeCount} elements`);
  }
  const topmost = await close.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    const top = document.elementFromPoint(rect.left + rect.width / 2, rect.top + rect.height / 2);
    return top === element || element.contains(top);
  });
  if (!topmost) {
    throw new Error(`${routeName} close control for drawer "${expectedText}" is covered by another layer`);
  }
  await close.click();
  await drawer.waitFor({ state: "hidden", timeout: 5_000 });
}

// Herd Register must lead with the Display ID / Tag 1 / Tag 2 identity columns and must never render the
// old "missing ID" chip — missing Tag values render as an em dash only.
async function assertHerdIdentityColumns(page, routeName) {
  const table = page.locator("table.herd-register-table").first();
  if ((await table.count()) === 0) {
    return false;
  }
  const headers = (await table.locator("thead th").allInnerTexts()).map((h) => h.trim());
  const expected = ["Display ID", "Tag 1", "Tag 2"];
  if (headers.slice(0, 3).map(comparableHeader).join("|") !== expected.map(comparableHeader).join("|")) {
    throw new Error(`${routeName} herd table must start with ${expected.join(", ")}; got ${headers.join(", ")}`);
  }
  const bodyText = await table.innerText();
  if (/missing ID/i.test(bodyText)) {
    throw new Error(`${routeName} herd table still renders a "missing ID" chip`);
  }
  return true;
}

// The Animal Passport drawer identity block must show Display ID + Tag 1 + Tag 2 and a G-###### display id,
// and must never render the raw goat UUID as the primary id or the legacy "Animal ID 1/2" / "missing ID".
async function assertHerdPassportIdentity(drawer, routeName, expectedText) {
  const text = await drawer.innerText();
  for (const label of ["Display ID", "Tag 1", "Tag 2"]) {
    if (!text.includes(label)) {
      throw new Error(`${routeName} passport drawer "${expectedText}" is missing identity label "${label}"`);
    }
  }
  for (const banned of ["missing ID", "Animal ID 1", "Animal ID 2"]) {
    if (text.includes(banned)) {
      throw new Error(`${routeName} passport drawer "${expectedText}" contains banned identity wording "${banned}"`);
    }
  }
  const displayId = (await drawer.locator(".gid").first().innerText().catch(() => "")).trim();
  if (!/^G-\d+/.test(displayId)) {
    throw new Error(`${routeName} passport drawer Display ID chip should be a G-###### id; got "${displayId}"`);
  }
}

// The vaccination calendar event drawer must use the same identity vocabulary as herd/passport: any
// eligible-animals target roster leads with Display ID / Tag 1 / Tag 2 and never renders legacy wording.
// A given drive can legitimately resolve to 0 targets in seeded data (projection vs obligation
// rule_id / IST-due-day mismatch), so the roster header check runs only when the roster is present;
// the banned-wording check always runs on the drawer text.
async function assertCalendarTargetIdentity(drawer, routeName, expectedText) {
  const text = await drawer.innerText();
  for (const banned of ["Animal ID 1", "Animal ID 2", "missing ID"]) {
    if (text.includes(banned)) {
      throw new Error(`${routeName} calendar drawer "${expectedText}" contains banned identity wording "${banned}"`);
    }
  }
  const roster = drawer.locator('table:has(th:has-text("Display ID"))');
  if ((await roster.count()) >= 1) {
    const headers = (await roster.first().locator("thead th").allInnerTexts()).map((h) => h.trim());
    const expected = ["Display ID", "Tag 1", "Tag 2"];
    if (headers.slice(0, 3).map(comparableHeader).join("|") !== expected.map(comparableHeader).join("|")) {
      throw new Error(`${routeName} calendar drive-target roster must start with ${expected.join(", ")}; got ${headers.join(", ")}`);
    }
  } else if (process.env.GOATOS_SMOKE_STRICT_CALENDAR_IDENTITY === "1") {
    // Deterministic gate: require a populated drive roster. Run against a seed/fixture whose drive
    // projection has matching generated obligations (in the current seed, drive projections have no
    // generated obligations for their protocol_version, so the roster is always empty — see handoff).
    // Fails loudly instead of silently skipping, so the identity columns are actually validated.
    throw new Error(`${routeName} calendar drawer "${expectedText}" has no eligible-animals roster but GOATOS_SMOKE_STRICT_CALENDAR_IDENTITY=1 requires one`);
  } else {
    console.log(`identity_calendar_roster=skipped_no_targets route=${routeName} event="${expectedText}"`);
  }
}

function cssString(value) {
  return value.replaceAll("\\", "\\\\").replaceAll('"', '\\"');
}

async function gotoWithRetry(page, url) {
  try {
    return await page.goto(url, { waitUntil: "domcontentloaded", timeout: navigationTimeoutMs });
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (!/ERR_ABORTED|Timeout/.test(message)) throw error;
    await page.waitForTimeout(500);
    return page.goto(url, { waitUntil: "domcontentloaded", timeout: navigationTimeoutMs });
  }
}

function comparableHeader(value) {
  return value.trim().replace(/\s+/g, " ").toLowerCase();
}

function parseJsonEnvArray(name) {
  const raw = process.env[name];
  if (!raw) return [];
  const parsed = JSON.parse(raw);
  if (!Array.isArray(parsed)) throw new Error(`${name} must be a JSON array`);
  return parsed;
}

function markObservedModuleText(visibleText) {
  const comparable = visibleText.toLowerCase();
  for (const expected of moduleAssertText) {
    if (comparable.includes(String(expected).toLowerCase())) {
      observedModuleText.add(String(expected));
    }
  }
}

function assertModuleTextObserved() {
  const missing = moduleAssertText.filter((expected) => !observedModuleText.has(String(expected)));
  if (missing.length) {
    throw new Error(`Module journey did not observe required text: ${missing.join(", ")}`);
  }
}

function compareOrUpdateBaseline(screenshotName, screenshotPath) {
  const baselinePath = join(baselineDir, screenshotName);
  if (updateBaseline) {
    mkdirSync(baselineDir, { recursive: true });
    copyFileSync(screenshotPath, baselinePath);
    baselineUpdated += 1;
    return;
  }
  if (!existsSync(baselinePath)) {
    if (requireBaseline) {
      throw new Error(`Missing visual baseline for ${screenshotName} at ${relativeToRepo(baselinePath)}`);
    }
    return;
  }
  const actual = PNG.sync.read(readFileSync(screenshotPath));
  const expected = PNG.sync.read(readFileSync(baselinePath));
  if (actual.width !== expected.width || actual.height !== expected.height) {
    throw new Error(`${screenshotName} dimensions changed: actual ${actual.width}x${actual.height}, baseline ${expected.width}x${expected.height}`);
  }
  const diff = new PNG({ width: actual.width, height: actual.height });
  const diffPixels = pixelmatch(expected.data, actual.data, diff.data, actual.width, actual.height, { threshold: 0.1 });
  const ratio = diffPixels / (actual.width * actual.height);
  baselineCompared += 1;
  if (ratio > maxDiffRatio) {
    const diffPath = join(diffDir, screenshotName);
    writeFileSync(diffPath, PNG.sync.write(diff));
    throw new Error(`${screenshotName} visual diff ${ratio.toFixed(4)} exceeds max ${maxDiffRatio}; diff=${relativeToRepo(diffPath)}`);
  }
}

function trimTrailingSlash(value) {
  return value.replace(/\/+$/, "");
}

function relativeToRepo(path) {
  return path.startsWith(repoRoot) ? path.slice(repoRoot.length + 1) : path;
}

function normalizeRepoPath(path) {
  if (!path) return undefined;
  return isAbsolute(path) ? path : join(repoRoot, path);
}

function parseArgs(argv) {
  const parsed = {
    baselineDir: undefined,
    updateBaseline: false,
    requireBaseline: false,
    maxDiffRatio: undefined,
  };
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === "--baseline-dir") {
      parsed.baselineDir = argv[++index];
    } else if (arg === "--update-baseline") {
      parsed.updateBaseline = true;
    } else if (arg === "--require-baseline") {
      parsed.requireBaseline = true;
    } else if (arg === "--max-diff-ratio") {
      parsed.maxDiffRatio = Number(argv[++index]);
      if (!Number.isFinite(parsed.maxDiffRatio) || parsed.maxDiffRatio < 0 || parsed.maxDiffRatio > 1) {
        throw new Error("--max-diff-ratio must be a number between 0 and 1");
      }
    } else {
      throw new Error(`Unknown argument: ${arg}`);
    }
  }
  return parsed;
}
