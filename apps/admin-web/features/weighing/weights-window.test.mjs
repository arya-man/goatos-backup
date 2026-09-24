import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./weights.tsx", import.meta.url), "utf8");
const analyticsSource = readFileSync(new URL("./weights-analytics.tsx", import.meta.url), "utf8");
const analyticsRouteSource = readFileSync(
  new URL("../../app/(admin)/weighing/analytics/page.tsx", import.meta.url),
  "utf8",
);
const analyticsTabLoadingSource = readFileSync(
  new URL("./weights-analytics-tab-loading.tsx", import.meta.url),
  "utf8",
);
const landingSource = readFileSync(new URL("./landing-window.ts", import.meta.url), "utf8");
const landingConstantsSource = readFileSync(
  new URL("./landing-window-constants.ts", import.meta.url),
  "utf8",
);
const segmentedLinksSource = readFileSync(new URL("../../components/segmented-links.tsx", import.meta.url), "utf8");
const serverSource = readFileSync(new URL("../../lib/api/server.ts", import.meta.url), "utf8");
const contract = readFileSync(
  new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url),
  "utf8",
);

test("admin weighing read coalescer covers the browser route sweep", () => {
	assert.match(serverSource, /const SHORT_READ_CACHE_TTL_MS = 0;/);
	assert.match(serverSource, /getShedWeights[\s\S]*cachedShortRead\(/);
	assert.match(serverSource, /getWeightDemographics[\s\S]*cachedShortRead\(/);
	assert.match(serverSource, /getWeighingGrowth[\s\S]*cachedShortRead\(/);
	assert.match(serverSource, /auth=\$\{authCacheFingerprint\(config\.bearerToken\)\}/);
	assert.match(serverSource, /createHash\("sha256"\)\.update\(token\)\.digest\("base64url"\)\.slice\(0, 16\)/);
	assert.match(serverSource, /new ShortReadCache\(SHORT_READ_CACHE_TTL_MS\)/);
});

test("weighing routes keep a local loading boundary instead of the global app fallback", () => {
  const loadingUrl = new URL("../../app/(admin)/weighing/loading.tsx", import.meta.url);
  assert.equal(existsSync(loadingUrl), true, "weighing must not fall back to app/loading.tsx");
  const loadingSource = readFileSync(loadingUrl, "utf8");
  assert.match(loadingSource, /aria-label="Weighing analytics loading"/);
  assert.match(loadingSource, /key=\{`\$\{width\}-\$\{index\}`\}/);
  assert.doesNotMatch(loadingSource, /Loading Mesha admin data/);
});

test("the period control is a calendar, not a fixed-window select", () => {
  assert.match(source, /kind: "daterange"/);
  // The two presets and the vocabulary behind them are GONE, not left unused: a stale option group
  // reads to the next author as a control that still exists somewhere.
  assert.doesNotMatch(source, /weighing_period/);
  assert.doesNotMatch(source, /periodDays/);
  assert.doesNotMatch(contract, /ID: "weighing_period"/);
  assert.doesNotMatch(contract, /"filter\.period\.4w"/);
  assert.doesNotMatch(contract, /"filter\.period\.12w"/);
});

test("the page lands on the database-configured default start (seed: 2026-08-03) through the latest weighing when no period is selected", () => {
  assert.match(landingConstantsSource, /export const DEFAULT_WINDOW_FROM = "2026-08-03";/);
  assert.match(landingConstantsSource, /export const LATEST_LUMP_LOOKBACK_DAYS = 400;/);
  assert.match(landingConstantsSource, /export const WINDOW_MIN_DATE = "2026-07-05";/);
  assert.match(landingSource, /export async function landingWindow/);
  assert.match(landingSource, /getWeighingDates\(\{\s*\n\s*park_id: parkID \|\| undefined,\s*\n\s*\.\.\.lookback,/);
  assert.match(landingSource, /sex: sexFilter \|\| undefined,/);
  assert.match(landingSource, /origin: originFilter \|\| undefined,/);
  assert.match(landingSource, /weighing_category: weighingCategoryFilter \|\| undefined,/);
  assert.doesNotMatch(landingSource, /dates\[dates\.length - 2\]/);
  // The database calendar row supplies the SERVED setting; the constant is its fallback.
  assert.match(landingSource, /const from = settings\?\.defaultFrom \?\? DEFAULT_WINDOW_FROM;/);
  assert.match(landingSource, /return windowThroughLatest\(today, result\.data\.latest_weighing_date \?\? "", settings\);/);
  // THE END IS NOT A LUMP DATE. On 25 Aug 2026 the farm scanned 199 kids across 17 sheds and weighed
  // no shed whole, so that day never entered lump_weighing_dates and a window closing on the later
  // lump date shut a day early -- dropping all 199 from the KPIs while the period label read as
  // though nothing was missing.
  assert.match(landingConstantsSource, /latest >= from/);

  // Clamped, like every other window this file resolves: a future end is never rendered.
  assert.match(landingConstantsSource, /latest <= today/);
  // The date is BACKEND-owned. A max taken across the returned rows would be the page deriving
  // business truth from its own rows, which is the rollup-from-a-slice shape this repo bans.
  assert.doesNotMatch(landingSource, /Math\.max\([^)]*last_weighed_date/);
  assert.match(landingSource, /return \{ from: from > today \? today : from, to: today \};/);
  // istDayPlus is pure calendar arithmetic on an already-resolved IST day. Re-entering a timezone
  // here (or hardcoding +05:30) is what the shared helper exists to prevent.
  assert.match(landingSource, /import \{ istDayPlus \} from "@\/lib\/format";/);
  assert.doesNotMatch(landingSource, /5\.5 \* 60/);
});


test("weights analytics starts independent cold-load reads together", () => {
  assert.match(analyticsSource, /const landingWindowPromise = landingWindow\(/);
  assert.match(analyticsSource, /const \[assumptions, window\] = await Promise\.all\(\[getGrowthAssumptions\(\), landingWindowPromise\]\);/);
  assert.doesNotMatch(analyticsSource, /const assumptions = await getGrowthAssumptions\(\);[\s\S]*const window = await landingWindow\(/);
});

test("weights analytics time-wise uses the same selected/default period as every tab", () => {
  assert.doesNotMatch(analyticsSource, /TREND_WEEKS/);
  assert.doesNotMatch(analyticsSource, /trendWindow/);
  assert.doesNotMatch(analyticsSource, /tab === "time" \?[^:]+: window/);
  assert.match(analyticsSource, /const readWindow = window;/);
  // The ONE deliberate exception is the Load-wise tab (maintainer request 2026-09-03): a purchase
  // load is bought whole, so that tab's shed read carries park-only scope over an all-time window
  // and its caption says so. Every other tab still reads the page's own scope and window.
  assert.match(analyticsSource, /const shedParams = \{\s*\n\s*\.\.\.\(wantsLoads \? \{ park_id: parkFilter \|\| undefined, from: LOAD_TAB_ALL_TIME_FROM, to: today \} : \{ \.\.\.scope, \.\.\.readWindow \}\),/);
  assert.match(analyticsSource, /include_loads: wantsLoads/);
  assert.match(analyticsSource, /include_dates: false/);
  assert.match(analyticsSource, /getShedWeights\(shedParams\)/);
  assert.match(analyticsSource, /getWeighingGrowth\(\{\s*\n\s*\.\.\.scope,\s*\n\s*\.\.\.readWindow,\s*\n\s*sections: growthSections,\s*\n\s*\.\.\.\(tab === "time" \? \{ bucket: gainBucket, \.\.\.penScope \} : \{\}\),/);
  assert.match(analyticsSource, /getWeightDemographics\(\{\s*\n\s*\.\.\.scope,\s*\n\s*\.\.\.readWindow,\s*\n\s*sections: demographicsSections,\s*\n\s*band_edges_kg: bandEdgesParam\(assumptionRows\),\s*\n\s*\.\.\.\(tab === "time" \? \{ bucket: gainBucket, \.\.\.penScope \} : \{\}\),/);
  assert.doesNotMatch(contract, /last 12 weeks/);
  assert.doesNotMatch(contract, /those 12 weeks/);
  assert.doesNotMatch(contract, /not moved by the period filter/);
});

test("analytics tab changes expose a visible pending state", () => {
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  assert.doesNotMatch(analyticsSource, /pendingLabel=/);
  assert.doesNotMatch(segmentedLinksSource, /pendingLabel\?: string/);
  assert.match(segmentedLinksSource, /metricseg metricseg-pending/);
  assert.doesNotMatch(segmentedLinksSource, /metricseg-status/);
  assert.match(segmentedLinksSource, /metricseg:navigate/);
  assert.doesNotMatch(segmentedLinksSource, /router\.prefetch/);
  assert.doesNotMatch(segmentedLinksSource, /from "next\/link"/);
  assert.match(segmentedLinksSource, /aria-busy=\{isPending\}/);
  assert.match(analyticsSource, /<WeightsAnalyticsTabLoading[\s\S]*currentTab=\{tab\}[\s\S]*tabLabels=\{/);
  assert.match(analyticsSource, /className="wt-tab-live"/);
  assert.match(analyticsTabLoadingSource, /window\.addEventListener\("metricseg:navigate"/);
  assert.match(analyticsTabLoadingSource, /classList\.add\("wt-tab-switching"\)/);
  assert.match(analyticsTabLoadingSource, /wt-tab-skeleton/);
  assert.match(css, /\.metricseg-pending\{/);
  assert.match(css, /\.wt-tab-switching \.wt-tab-live\{display:none\}/);
  assert.match(css, /\.wt-tab-skeleton\{/);
  assert.doesNotMatch(css, /\.metricseg-status\{/);
});

test("weights analytics sends the weighing mode through every tab read", () => {
  assert.doesNotMatch(analyticsSource, /WEIGHING_FILTER_TABS/);
  assert.match(analyticsSource, /function weighingModeFilter\(raw: string \| undefined\): string/);
  assert.match(analyticsSource, /const modeFilter = weighingModeFilter\(one\(params, "weighing"\)\);/);
  assert.match(analyticsSource, /const weighingCategoryFilter = modeFilter !== "all" \? modeFilter : "";/);
  assert.match(analyticsSource, /landingWindow\(\s*\n\s*params,\s*\n\s*today,\s*\n\s*parkFilter,\s*\n\s*sexFilter,\s*\n\s*originFilter,\s*\n\s*weighingCategoryFilter,\s*\n\s*windowSettings,\s*\n\s*\)/);
  assert.match(analyticsSource, /weighing_category: weighingCategoryFilter \|\| undefined/);
  // The shed read routes through shedParams so the Load-wise tab can drop the page filters a
  // whole load cannot honour; on every other tab shedParams IS { ...scope, ...readWindow }.
  assert.match(analyticsSource, /getShedWeights\(shedParams\)/);
  assert.match(analyticsSource, /getWeighingGrowth\(\{\s*\n\s*\.\.\.scope,\s*\n\s*\.\.\.readWindow,\s*\n\s*sections: growthSections,\s*\n\s*\.\.\.\(tab === "time" \? \{ bucket: gainBucket, \.\.\.penScope \} : \{\}\),/);
  assert.match(analyticsSource, /getWeightDemographics\(\{\s*\n\s*\.\.\.scope,\s*\n\s*\.\.\.readWindow,\s*\n\s*sections: demographicsSections,\s*\n\s*band_edges_kg: bandEdgesParam\(assumptionRows\),\s*\n\s*\.\.\.\(tab === "time" \? \{ bucket: gainBucket, \.\.\.penScope \} : \{\}\),/);
  assert.match(analyticsSource, /weighingCategory=\{modeFilter !== "all" \? modeFilter : undefined\}/);
});

test("weights analytics tab links preserve the resolved weighing window", () => {
  assert.match(analyticsSource, /\[WINDOW_FROM_PARAM\]: window\.from/);
  assert.match(analyticsSource, /\[WINDOW_TO_PARAM\]: window\.to/);
  assert.match(analyticsSource, /\[TAB_PARAM\]: name === "general" \? null : name/);
});

test("weights analytics direct route renders without a client-router canonical redirect", () => {
  assert.match(analyticsRouteSource, /const \[params, contract\] = await Promise\.all\(\[searchParams, getAdminWebBootstrap\(\)\]\);/);
  assert.match(analyticsRouteSource, /assertContractAvailable\(contract\);/);
  assert.match(analyticsRouteSource, /const pageContract = contract\.data\.pages\.find\(\(item\) => item\.route_id === "weighing-analytics"\);/);
  assert.match(analyticsRouteSource, /<WeighingWeightsAnalyticsPage[\s\S]*searchParams=\{params\}[\s\S]*pageContract=\{pageContract\}/);
  assert.doesNotMatch(analyticsRouteSource, /redirectToCanonicalWindow/);
  assert.doesNotMatch(analyticsRouteSource, /hrefWithWindow/);
  assert.doesNotMatch(analyticsRouteSource, /redirect\(hrefWithWindow/);
  assert.doesNotMatch(analyticsRouteSource, /requireAdminWebPageContract/);
  assert.doesNotMatch(analyticsRouteSource, /Promise\.all\(\[searchParams, requireAdminWebPageContract/);
});

test("weights analytics fails selected tabs instead of rendering API failures as empty data", () => {
  assert.match(analyticsSource, /if \(growth && !growth\.ok\) \{\s*return <WeightsAnalyticsLoadError pageContract=\{pageContract\} \/>;\s*\}/);
  assert.match(analyticsSource, /if \(demographics && !demographics\.ok\) \{\s*return <WeightsAnalyticsLoadError pageContract=\{pageContract\} \/>;\s*\}/);
  assert.match(analyticsSource, /function WeightsAnalyticsLoadError/);
  assert.doesNotMatch(analyticsSource, /perParkResults/);
  assert.doesNotMatch(analyticsSource, /function mustHaveData/);
  // A failed growth read takes the tab down above; the per-park cards read that same response and
  // must never fall back to an empty list that reads as "no park grew".
  assert.match(analyticsSource, /growth\?\.ok \? \(growth\.data\.by_park \?\? \[\]\) : \[\]/);
  assert.match(analyticsSource, /const demo = demographics\?\.ok \? demographics\.data : null;/);
});

test("weights page sends the weighing mode through every backend read", () => {
  assert.match(source, /function weighingModeFilter\(raw: string \| undefined\): string/);
  assert.match(source, /const modeFilter = weighingModeFilter\(one\(params, "weighing"\)\);/);
  assert.match(source, /const weighingCategoryFilter = modeFilter !== "all" \? modeFilter : "";/);
  assert.match(source, /landingWindow\(\s*\n\s*params,\s*\n\s*today,\s*\n\s*parkFilter,\s*\n\s*sexFilter,\s*\n\s*originFilter,\s*\n\s*weighingCategoryFilter,\s*\n\s*windowSettings,\s*\n\s*\)/);
  assert.match(source, /weighing_category: weighingCategoryFilter \|\| undefined/);
  assert.match(source, /getShedWeights\(\{\s*\n\s*\.\.\.scope,\s*\n\s*\.\.\.window,/);
  assert.match(source, /getWeighingGrowth\(\{ \.\.\.scope, \.\.\.window, sections: "headline,shed_leaderboard,losing_animals" \}\)/);
  assert.match(source, /sections: "composition,dimensions,gain_thresholds"/);
  assert.doesNotMatch(source, /sections: "composition,dimensions,origin,shed_type,weight_bands,weekly_gain,gain_thresholds"/);
  assert.match(source, /getGrowthDirector\(\{ \.\.\.scope, \.\.\.window, sections: "road_to_sale,fair_fight" \}\)/);
  assert.doesNotMatch(source, /getWeighingGrowth\(\{ \.\.\.scope, \.\.\.window, park_id: park\.park_id \}\)/);
});

test("weights analytics tabs request only the growth sections they render", () => {
  assert.match(analyticsSource, /tab === "general" \? "headline,shed_leaderboard,by_park" : tab === "time" \? "weekly_gain" : ""/);
  assert.match(analyticsSource, /const wantsGrowth = growthSections !== "";/);
  assert.match(analyticsSource, /getWeighingGrowth\(\{\s*\n\s*\.\.\.scope,\s*\n\s*\.\.\.readWindow,\s*\n\s*sections: growthSections,\s*\n\s*\.\.\.\(tab === "time" \? \{ bucket: gainBucket, \.\.\.penScope \} : \{\}\),/);
  assert.doesNotMatch(analyticsSource, /sectioned-aggregate-reads:allow/);
  assert.doesNotMatch(analyticsSource, /getWeighingGrowth\(\{ \.\.\.scope, \.\.\.readWindow \}\)/);
});

test("weights analytics tabs request only the demographics sections they render", () => {
  assert.match(analyticsSource, /tab === "breed"[\s\S]*\? "dimensions"/);
  assert.match(analyticsSource, /tab === "birth"[\s\S]*\? "origin"/);
  assert.match(analyticsSource, /tab === "shed"[\s\S]*\? "shed_type"/);
  assert.match(analyticsSource, /tab === "weight"[\s\S]*\? "weight_bands"/);
  assert.match(analyticsSource, /tab === "time"[\s\S]*\? "weekly_gain"/);
  assert.match(analyticsSource, /getWeightDemographics\(\{\s*\n\s*\.\.\.scope,\s*\n\s*\.\.\.readWindow,\s*\n\s*sections: demographicsSections,\s*\n\s*band_edges_kg: bandEdgesParam\(assumptionRows\),\s*\n\s*\.\.\.\(tab === "time" \? \{ bucket: gainBucket, \.\.\.penScope \} : \{\}\),/);
  assert.doesNotMatch(analyticsSource, /getWeightDemographics\(\{ \.\.\.scope, \.\.\.readWindow \}\)/);
});

test("weights page requests every growth section it renders", () => {
  assert.match(source, /growth\.data\.headline\.average_adg_g_per_day/);
  assert.match(source, /growth\.data\.headline\.headline_animals/);
  assert.match(source, /growth\.data\.shed_leaderboard/);
  assert.match(source, /growth\.data\.losing_animals/);
  assert.match(source, /sections: "headline,shed_leaderboard,losing_animals"/);
});

test("weights reads use the longer live-latency backend timeout", () => {
  assert.match(serverSource, /const DEFAULT_BACKEND_GET_TIMEOUT_MS = 8000;/);
  assert.match(serverSource, /const WEIGHING_BACKEND_GET_TIMEOUT_MS = 15000;/);
  assert.match(serverSource, /pathname\.startsWith\("\/weighing\/"\)/);
  assert.match(serverSource, /pathname\.startsWith\("\/growth-director\/"\)/);
  assert.match(serverSource, /setTimeout\(\(\) => controller\.abort\(\), backendGetTimeoutMs\(url\.pathname\)\)/);
});

test("the default window is passed as NAMED fields, never spread", () => {
  // `{...defaultWindow(today)}` spreads `{from, to}` — the same two keys the SELECTED window uses —
  // and would silently overwrite the resolved latest-two-weighings window. It typechecks and
  // renders; only the data is wrong.
  assert.match(source, /defaultFrom: defaultWindow\(today, windowSettings\)\.from,\s*\n\s*defaultTo: defaultWindow\(today, windowSettings\)\.to,/);
  assert.doesNotMatch(landingSource, /\.\.\.defaultWindow\(/);
});

test("a hand-edited window falls back instead of taking the page down", () => {
  // Inverted, malformed or absent parameters land on the default; a future end clamps to today,
  // because a weigh cannot have happened tomorrow.
  assert.match(landingSource, /rawFrom <= rawTo/);
  assert.match(landingSource, /rawFrom > today \? today : rawFrom/);
  assert.match(landingSource, /if \(rawFrom \|\| rawTo\) return defaultWindow\(today, settings\);/);
  assert.doesNotMatch(landingSource, /ADMIN_WEB_FAST_SIDEBAR_WINDOWS/);
  assert.match(landingSource, /return defaultWindow\(today, settings\);/);
});

test("the headline row is five cards, and the gain figure is stated once", () => {
  // The sixth card printed the SAME number, denominator and sub-line as the "All parks — daily
  // gain" card in the row below it. Its copy keys are deleted too, so the duplicate cannot be
  // reinstated by pasting the markup back.
  assert.match(source, /className="grid g5 kpi-row"/);
  assert.doesNotMatch(source, /className="grid g6 kpi-row"/);
  assert.doesNotMatch(source, /"kpi\.gain\.label"/);
  assert.doesNotMatch(contract, /"kpi\.gain\.label":/);
  assert.doesNotMatch(contract, /"kpi\.gain\.sub":/);
  // g5 needs its own responsive ladder; without it the row falls back to one column.
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  assert.match(analyticsSource, /className="wt-general-metrics"/);
  assert.match(css, /\.wt-general-metrics\{display:flex;flex-direction:column;gap:18px\}/);
  assert.match(css, /\.g5\{grid-template-columns:repeat\(5,minmax\(0,1fr\)\)\}/);
  assert.match(css, /@media\(max-width:1400px\)\{\.g5\{/);
  assert.match(css, /@media\(max-width:640px\)\{\.g5\{grid-template-columns:1fr\}\}/);
});

test("daily gain survives a park-scoped page", () => {
  // The gain row used to render ONLY when the page showed more than one park, because the deleted
  // headline card carried the figure in every other scope. Keeping that condition would have
  // removed the growth number entirely from a park-scoped page — the one thing this screen is for.
  assert.doesNotMatch(source, /perParkGain\.length > 0 \?/);
  // Its first card names the CURRENT scope, so "All parks" is only shown when it really is all.
  assert.match(source, /\{selectedParkName \|\| copy\(pageContract, "kpi\.park_gain\.all"\)\}/);
  // Never the raw park id: that would put an internal identifier in front of a CEO.
  assert.match(source, /parks\.find\(\(park\) => park\.park_id === parkFilter\)\?\.name \?\? ""/);
});

test("the analytics General tab reads its per-park gain from the growth response", () => {
  // The regression this pins: the per-park read was emptied to a literal `[]` while every line of
  // card markup below it survived, so the page kept its "CBE — daily gain" / "CPT — daily gain"
  // renderer and had nothing to render. Asserting the markup is therefore not enough -- the DATA
  // is the half that went missing, so the data is what this test names.
  assert.match(analyticsSource, /growth\.data\.by_park \?\? \[\]/);
  assert.match(analyticsSource, /gain: park\.average_adg_g_per_day \?\? null/);
  assert.match(analyticsSource, /animals: park\.headline_animals/);
  // ...and it comes off the request the page ALREADY makes. The extra per-park round trips were
  // removed for page speed; re-adding them is what this half of the pin forbids.
  assert.doesNotMatch(analyticsSource, /perParkResults/);
  assert.doesNotMatch(analyticsSource, /getWeighingGrowth\(\{ \.\.\.scope, \.\.\.readWindow, park_id: park\.park_id \}\)/);
  // A park narrowed to by the reader shows no card: the headline above already is that park.
  assert.match(analyticsSource, /tab === "general" && parkFilter === ""/);
});

test("shed lists and gain chart only show sheds weighed in the selected window", () => {
  assert.match(source, /const weighedRows = rows\.filter\(\(row\) => row\.animals_weighed > 0\);/);
  assert.match(source, /modeFilter === "all" \? weighedRows : weighedRows\.filter/);
  assert.match(source, /const visibleRowKeys = new Set\(visibleRows\.map\(\(row\) => shedKey\(row\.location_id, row\.partition_label\)\)\);/);
  assert.match(source, /shed\.adg_animals > 0 && visibleRowKeys\.has\(shedKey\(shed\.location_id, shed\.partition_label\)\)/);
  // A scanned pen's gain is the MEAN of its kids' own gains (maintainer decision 2026-09-24), the
  // headline's statistic -- never the leaderboard's median of per-leg rates.
  assert.doesNotMatch(source, /shed\.median_adg_g_per_day/);
  // A shed with ONE weigh has no daily gain, so it is not plotted in the gain chart at all.
  // It used to be — with a zero-length bar labelled in KILOGRAMS beside real g/day bars, which
  // put "Castro 1 · 34.4 kg" in a daily-gain chart. Two measures on one axis is the defect;
  // these three assertions are what pinned it, so they now pin its absence.
  assert.doesNotMatch(source, /const singleWeighRows/);
  assert.match(source, /const gainChartData = \[\.\.\.perAnimalGainRows, \.\.\.shedAverageGainRows\]\.sort\(/);
  // No hand-written kg label survives anywhere in this page's chart data. The Weight view
  // still shows every shed's average — it carries `unit: "kg"` on the SERIES, so the unit is
  // declared once for the whole chart and cannot leak into the gain chart beside it.
  assert.equal(source.match(/valueLabel: `\$\{kg\(row\.average_weight_kg\)\} kg`/g)?.length ?? 0, 0);
  assert.match(source, /unit: "kg"/);
});

test("weighed shed rows render breed and sex composition chips from the backend contract", () => {
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  assert.match(source, /demo\?\.shed_composition \?\? \[\]/);
  assert.match(source, /shedLabelWithComposition/);
  // Three, not four: the gain chart no longer builds a row for a shed with one weigh.
  assert.equal(source.match(/label: shedLabelWithComposition\(/g)?.length, 3);
  assert.match(source, /replaceAll\(" · ", " - "\)/);
  // The chart's per-cohort suffix carries the resident COUNT (maintainer ask
  // 2026-08-31), from the chip's own backend `animals` figure — same number the
  // sheds table chips have always shown.
  assert.match(source, /× \$\{chip\.animals\.toLocaleString\("en-IN"\)\}/);
  assert.doesNotMatch(source, /shed avg/);
  assert.match(source, /className="wcomp-chips"/);
  assert.match(source, /className="wcomp-chip"/);
  assert.match(source, /compositionLabel\(chip, pageContract\)/);
  assert.match(source, /copy\(pageContract, "composition\.unknown_breed"\)/);
  assert.match(source, /copy\(pageContract, "composition\.unknown_sex"\)/);
  assert.match(contract, /"composition\.unknown_breed":/);
  assert.match(contract, /"composition\.unknown_sex":/);
  assert.doesNotMatch(source, /composition\.source === "scanned_tags"/);
  assert.match(css, /\.wcomp-chips\{/);
  assert.match(css, /\.wcomp-chip\{/);
});

test("small shed charts do not reserve the tall empty panel height", () => {
  assert.match(source, /size: gainChartData\.length <= 8 \? \("short" as const\) : \("tall" as const\)/);
  assert.match(source, /size: chartData\.length <= 8 \? \("short" as const\) : \("tall" as const\)/);
  assert.match(source, /<ShedMetricChart/);
});

test("both shed-chart metrics use ONE order, so the toggle only changes the bars", () => {
  // The gain view has been alphabetical since 2026-08-22 so an operator can find a pen by
  // name. The weight view was heaviest-first, so switching metric reshuffled every row on a
  // control the reader expects to change only the measure. Both now sort by the PEN NAME with
  // numeric collation ("Castro 2" before "Castro 10") -- the composed label carried the pen's
  // breed/sex composition too, and since 2026-09-01 the table shows that as its own columns,
  // so keying the order off the label would tie A→Z to a string the table no longer prints.
  // Counted, not matched once: the two call sites are formatted differently (one wraps),
  // so this asserts BOTH series carry the same comparator rather than that one exists.
  //
  // Since 2026-09-16 the A→Z runs INSIDE a park cluster: rows are grouped CBE first, then
  // CPT, in the order the backend served the page's park vocabulary (`parkOrder`), and the
  // pen-name comparator is the within-cluster tie-break on both series.
  const alphabetical = /a\.shedName\.localeCompare\(b\.shedName, undefined, \{ numeric: true \}\)/g;
  assert.equal((source.match(alphabetical) ?? []).length, 2, "both chart series must sort A→Z by shed name");
  const clustered = /byParkThen\(parkOrder, \(row\) => row\.park_name, \(a, b\) =>\s*a\.shedName\.localeCompare/g;
  assert.equal((source.match(clustered) ?? []).length, 2, "both chart series must cluster by the served park order first");
  assert.match(source, /const parkOrder = parks\.map\(\(park\) => park\.name\)/);
  assert.match(source, /const gainChartData = \[[\s\S]{0,120}\.sort\(/);
  assert.match(source, /const chartData = visibleRows[\s\S]{0,1800}\.sort\(\s*byParkThen\(parkOrder/);
  // The weight ranking must not come back: it is the specific behaviour being replaced.
  assert.doesNotMatch(source, /sort\(\(a, b\) => b\.average_weight_kg - a\.average_weight_kg\)[\s\S]{0,400}chartData/);
});

test("the shed table reads breed, gender and count as columns, not out of the pen name", () => {
  // Maintainer request 2026-09-01. The composition used to ride inside the shed label, where a
  // mixed pen ran past a hundred characters; it is three columns now and the shed cell is the
  // pen name alone. Chips under the name -- the Sheds table's shape -- were tried and turned
  // down, so this also pins that they do not come back. The CHART still draws the composed
  // label, which is what keeps the two views naming the same pen.
  const client = readFileSync(new URL("./metric-chart.tsx", import.meta.url), "utf8");
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  for (const key of ["breed", "sex", "count"]) {
    assert.match(source, new RegExp(`copy\\(pageContract, "table\\.shed_gain\\.${key}"\\)`));
    assert.match(contract, new RegExp(`"table\\.shed_gain\\.${key}":`));
    assert.match(client, new RegExp(`columns\\.${key}`));
  }
  // The pen name alone in the shed cell, and the composed label still fed to the bars.
  assert.match(client, /row\.shedName \?\? row\.label/);
  assert.doesNotMatch(client, /wcomp-chip/);
  // The gain stays on the ROW: a shed average is never repeated per cohort, which would read
  // as a per-breed figure nobody measured.
  assert.doesNotMatch(client, /cohorts[\s\S]{0,200}wsg-val/);
  assert.match(css, /table\.wsgtable th\.wsg-sex,/);
  assert.match(css, /table\.wsgtable \.wsg-line\{/);
});

test("gender is written once for a single-sex pen and every line for a mixed one", () => {
  // Maintainer request 2026-09-01: repeating "male" five times down an all-male pen is noise,
  // and it buried the mixed pens among identical columns. All-or-nothing, so a collapsed cell
  // can only ever mean "this pen is all of this sex" -- one differing cohort brings every line
  // back, and the lines still pair by index with breed and count.
  const client = readFileSync(new URL("./metric-chart.tsx", import.meta.url), "utf8");
  const fn = client.match(/function sexLines\([\s\S]*?\n\}/);
  assert.ok(fn, "sexLines must exist");
  // The signature carries TypeScript types; the BODY is plain JS, so the real rule runs here
  // rather than a copy of it re-derived in the test.
  const js = fn[0].replace(/function sexLines\([^)]*\)\s*:[^{]*\{/, "function sexLines(cohorts) {");
  const sexLines = new Function(`${js}; return sexLines;`)();
  const c = (...sexes) => sexes.map((sex) => ({ breed: "b", sex, animals: 1 }));
  assert.deepEqual(sexLines(c("male", "male", "male")), ["male"], "all-male pen writes it once");
  assert.deepEqual(sexLines(c("male", "female", "male")), ["male", "female", "male"], "one differing cohort brings every line back");
  assert.deepEqual(sexLines(c("female")), ["female"], "a single cohort is already one line");
  assert.deepEqual(sexLines(undefined), [], "a pen with no composition has no lines");
  // Breed and count must NOT collapse: two cohorts really can share a breed, and each carries
  // its own head count.
  assert.match(client, /\(row\.cohorts \?\? \[\]\)\.map\(\(cohort, index\) => \([\s\S]{0,120}cohort\.breed/);
  assert.match(client, /\(row\.cohorts \?\? \[\]\)\.map\(\(cohort, index\) => \([\s\S]{0,160}cohort\.animals/);
});

test("the shed gain card offers no Table/Chart switch", () => {
  // Retired in the frontend (maintainer request 2026-09-01): the figures ARE the card, so the
  // control was one more thing to read past. The backend copy key is left in place and
  // `?shed_view=chart` is still honoured, so a link shared before this still renders -- nothing
  // on the page produces one any more. The gain-thresholds card keeps its OWN switch, which is
  // why this asserts on the shed card's props rather than on SegmentedLinks page-wide.
  const client = readFileSync(new URL("./metric-chart.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(client, /SegmentedLinks/);
  assert.doesNotMatch(source, /viewOptions=\{/);
  assert.doesNotMatch(source, /section\.shed_gain\.view_aria/);
  // The metric toggle is untouched -- Daily gain / Weight is a different control.
  assert.match(client, /<MetricToggle current=\{metric\}/);
});

test("chart metric switches are local state, not route reloads", () => {
  const client = readFileSync(new URL("./metric-chart.tsx", import.meta.url), "utf8");
  assert.match(client, /"use client"/);
  assert.match(client, /useState<Metric>/);
  assert.match(client, /type="button"/);
  assert.match(source, /series=\{\{\s*adg:/);
  assert.doesNotMatch(source, /hrefWith\(params, \{ \[param\]: option \}\)/);
  assert.doesNotMatch(source, /breed_metric"\} current/);
  assert.doesNotMatch(source, /shed_metric"\} current/);
});

test("full-width shed chart labels fit without overlapping rows", () => {
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  assert.match(css, /\.wcols \.wbar\{[^}]*min-height:86px/);
  assert.match(css, /@media\(max-width:1200px\)\{\.wcols \.wbar\{[^}]*min-height:92px/);
  assert.match(css, /\.wbar \.wbl-text\{[^}]*display:-webkit-box/);
  assert.match(css, /\.wbar \.wbl-text\{[^}]*-webkit-line-clamp:2/);
  assert.match(css, /\.wcols \.wbar \.wbl-text\{[^}]*display:-webkit-box/);
  assert.match(css, /\.wcols \.wbar \.wbl-text\{[^}]*-webkit-line-clamp:3/);
  assert.match(css, /\.wcols \.wbar \.wbl-text\{[^}]*white-space:normal/);
  assert.match(css, /\.wcols \.wbar \.wbl-text\{[^}]*overflow:hidden/);
  assert.match(css, /\.wcols \.wbar \.wbl-text\{[^}]*text-overflow:ellipsis/);
  assert.match(css, /\.wcols \.wbar \.wbl-text\{[^}]*overflow-wrap:anywhere/);
  assert.doesNotMatch(css, /\.wcols \.wbar \.wbl\{[^}]*overflow:visible/);
  assert.match(css, /\.wbar\{[^}]*grid-template-columns:minmax\(88px,clamp\(160px,36%,260px\)\) minmax\(72px,1fr\) 72px/);
  assert.match(css, /\.wbar\{[^}]*min-height:34px/);
  assert.match(css, /\.wbar \.wbl\{[^}]*min-width:0/);
  assert.match(css, /\.wbar \.wbt\{[^}]*min-width:0/);
  assert.match(css, /@media\(max-width:900px\)\{\.wbar\{[^}]*minmax\(0,112px\)/);
  assert.match(css, /@media\(max-width:520px\)\{\s*\.wgrouped \.wbar\{[^}]*grid-template-columns:minmax\(0,1fr\) auto/);
  assert.match(css, /@media\(max-width:520px\)\{[\s\S]*\.wgrouped \.wbar \.wbt\{[^}]*grid-column:1\/-1/);
  assert.match(css, /@media\(max-width:520px\)\{[\s\S]*\.wgrouped \.wbar \.wbv\{[^}]*grid-column:2/);
});

test("the two table cards are inset without losing their full-bleed tables", () => {
  const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
  assert.match(source, /className="card wtable" aria-label=\{copy\(pageContract, "section\.sheds\.aria"\)\}/);
  assert.match(source, /className="card wtable" aria-label=\{copy\(pageContract, "section\.losing\.aria"\)\}/);
  // Vertical padding on the CARD, horizontal on its children — never on the card itself, which
  // would inset the table away from its own header rule and row separators.
  assert.match(css, /\.wtable\{padding:14px 0\}/);
  // `.twrap` joined `.tablewrap` in the exclusion when the loads ledger took this card treatment
  // (2026-09-21): it is the same wrapper one class over and owns its own horizontal scroll, so
  // insetting it would pull that table away from its own header rule.
  assert.match(css, /\.wtable > :not\(\.tablewrap\):not\(\.twrap\)\{padding-left:16px;padding-right:16px\}/);
  // The outer columns match the card's own 17px text edge; a cell padding, so the rules still reach
  // the frame.
  assert.match(css, /\.wtable table\.tbl th:first-child,\s*\n\.wtable table\.tbl td:first-child\{padding-left:16px\}/);
});

test("every visible string on the weighing calendar is backend-contract copy", () => {
  for (const key of [
    "filter.period.label",
    "filter.period.today",
    "filter.period.single",
    "filter.period.range",
    "filter.period.aria",
    "filter.period.previous_month",
    "filter.period.next_month",
    "filter.period.range_start_hint",
    "filter.period.range_end_hint",
    "filter.period.range_separator",
    "filter.period.lump_marker_hint",
  ]) {
    const escaped = key.replace(/\./g, "\\.");
    assert.match(source, new RegExp(`copy\\(pageContract, "${escaped}"\\)`), `page: ${key}`);
    assert.match(contract, new RegExp(`"${escaped}":`), `contract: ${key}`);
  }
});

test("the weighing calendar marks backend-reported lump-sum weigh dates", () => {
  assert.match(source, /markerHint: copy\(pageContract, "filter\.period\.lump_marker_hint"\)/);
  assert.match(source, /markerFetchPath: `\/api\/weighing\/lump-markers/);
  assert.doesNotMatch(source, /markerDates: lumpWeighingDates/);
  assert.match(contract, /"filter\.period\.lump_marker_hint":/);
});

test("lump marker proxy reads a calendar window independently of the report range", () => {
  const route = readFileSync(
    new URL("../../app/api/weighing/lump-markers/route.ts", import.meta.url),
    "utf8",
  );
  assert.match(route, /url\.searchParams\.get\("from"\)/);
  assert.match(route, /url\.searchParams\.get\("to"\)/);
  assert.match(route, /url\.searchParams\.get\("sex"\)/);
  assert.match(route, /url\.searchParams\.get\("origin"\)/);
  assert.match(route, /url\.searchParams\.get\("weighing"\)/);
  assert.match(route, /getWeighingDates\(\{\s*\n\s*park_id: parkID \|\| undefined,\s*\n\s*from,\s*\n\s*to,/);
  assert.match(route, /sex: sex === "male" \|\| sex === "female" \? sex : undefined,/);
  assert.match(route, /origin: origin === "farm_born" \|\| origin === "purchased" \? origin : undefined,/);
  assert.match(route, /weighing_category:\s*\n\s*weighing === "individual_animal" \|\| weighing === "per_shed_partition" \? weighing : undefined,/);
  assert.match(route, /dates: result\.data\.lump_weighing_dates/);
});

test("both Weights pages and the export drawer take the window from the page contract, never the constants", () => {
  // Database-only calendar configuration supplies the default start and earliest day
  // through the existing page contract's copy; no settings UI is added or changed.
  for (const src of [source, analyticsSource]) {
    assert.match(src, /const windowSettings = weightsWindowSettings\(pageContract\.copy, today\);/);
    assert.match(src, /minDate: windowSettings\.earliestDate,/);
    assert.doesNotMatch(src, /WINDOW_MIN_DATE/);
  }
  const exportSource = readFileSync(new URL("./weights-export.tsx", import.meta.url), "utf8");
  assert.match(exportSource, /minDate=\{weightsWindowSettings\(pageContract\.copy, today\)\.earliestDate\}/);
  assert.doesNotMatch(exportSource, /WINDOW_MIN_DATE/);
  // The resolver: fixed date, rolling days (never before the earliest day), and the seed fallback.
  assert.match(landingConstantsSource, /if \(mode === "rolling_days"\)/);
  assert.match(landingConstantsSource, /if \(defaultFrom < earliestDate\) defaultFrom = earliestDate;/);
});
