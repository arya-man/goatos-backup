import { test } from "node:test";
import assert from "node:assert";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// Pins the fix for the STG incident (2026-08-12): the CEO's oversight filters (module chips,
// capture-date range picker) on /verify rendered for every role that can open the page, including
// RoleVerifier, because /verify is a single role-agnostic component. See
// docs/decisions/role-scoped-ui-is-capability-gated.md.
//
// These are SOURCE-SHAPE assertions, not a rendered-DOM test: the renderer must gate the
// oversight-only chrome on the backend page contract's "oversight_filters" control -- never on a
// hardcoded literal, a role string, or a data shape a hand-crafted payload could satisfy.
const source = readFileSync(
  fileURLToPath(new URL("./verification-review-page.tsx", import.meta.url)),
  "utf8",
);

test("the oversight filters flag is read from the page contract's oversight_filters control", () => {
  assert.match(
    source,
    /const oversightFiltersEnabled = controlEnabled\(pageContract, "oversight_filters", false\)/,
    "oversightFiltersEnabled must come from controlEnabled(pageContract, \"oversight_filters\", ...), not be hardcoded or role-derived",
  );
});

// Two independent gates guard this row and BOTH must survive. `module_filter` is the backend's
// per-principal offer (it is withdrawn from the verifier, whose sidebar already carries one leaf
// per evidence module), and `oversight_filters` is the leadership capability that introduced the
// row's cross-module reach. Asserting only on the pair as a whole would let a later edit drop
// either one silently, so the test names each flag.
test("the module-chip row is gated on oversightFiltersEnabled", () => {
  const chipRow = source.match(/\{[^\n]*modules\.length > 1 \? \(/);
  assert.ok(chipRow, "expected a module chip row conditioned on modules.length > 1");
  assert.match(
    chipRow[0],
    /oversightFiltersEnabled/,
    "the module chip row must require oversightFiltersEnabled in addition to having more than one module option",
  );
  assert.match(
    chipRow[0],
    /moduleFilterOffered/,
    "the module chip row must also honour the backend's module_filter offer, which is withdrawn for the verifier",
  );
});

test("the capture-date range picker is gated on oversightFiltersEnabled", () => {
  const filterRow = source.match(/<div className="vr-frow">([\s\S]*?)\{sheds\.length \? \(/);
  assert.ok(filterRow, "expected the vr-frow filter row to precede the shed filter block");
  assert.match(
    filterRow[1],
    /\{oversightFiltersEnabled \? \(\s*<ActionsDateFilter/,
    "ActionsDateFilter must only render when oversightFiltersEnabled is true",
  );
});

// The verifier's original working-queue filters predate the oversight rollout (status chips:
// commit fe06be1ed, the screen's first commit; shed filter: commit 89b16c0fa, the same commit
// that introduced this screen). They must stay UNGATED so a verifier's queue is unaffected.
test("status chips and the shed filter are not gated behind oversightFiltersEnabled", () => {
  // The oversight analytics section (gated on its own "oversight_analytics" control) may render
  // between the status chip block and the results table heading -- it is optional additive
  // chrome, not part of this filter row, so the regex tolerates it sitting in between.
  const statusBlock = source.match(
    /\{statuses\.length \? \(([\s\S]*?)\) : null\}\s*\n[\s\S]{0,600}?<div className="vr-secthd">/,
  );
  assert.ok(statusBlock, "expected the status chip block ahead of the results table heading");
  assert.ok(
    !/oversightFiltersEnabled/.test(statusBlock[1]),
    "status chips predate the oversight rollout and must render for every role, including RoleVerifier",
  );

  const shedFormMatch = source.match(/id="verification-shed"[\s\S]{0,400}/);
  assert.ok(shedFormMatch, "expected the shed <select> in the filter row");
  assert.ok(
    !/oversightFiltersEnabled/.test(shedFormMatch[0]),
    "the shed filter predates the oversight rollout and must render for every role, including RoleVerifier",
  );
});

// No hardcoded filter chip labels: every visible string this screen shows for the oversight
// filters must be backend copy (pageContract), never a literal the frontend invented -- so a
// hand-edited role cannot resurrect the filters by literal text, and a copy change lands from one
// place (the contract), not two.
test("no hardcoded oversight filter chip labels: module/date/shed copy is backend-owned", () => {
  for (const literal of [
    "All modules",
    "To verify",
    "Accepted",
    "Rejected",
  ]) {
    assert.ok(
      !source.includes(`"${literal}"`) && !source.includes(`'${literal}'`) && !source.includes(`>${literal}<`),
      `filter chip label "${literal}" must come from the page contract via copy(pageContract, ...), not be hardcoded`,
    );
  }
});

// The analytics SECTION is gated on its own contract control, not on oversightFiltersEnabled and
// not on a role string -- the same capability (permissions.VerificationOversee) that gates the
// backend endpoint it reads, so a verifier's page never renders it AND never calls it. The backend
// half is pinned by TestVerifyPageOversightAnalyticsControlIsCapabilityGated (adminui/app) and by
// the VerificationOversee entry for this route in permissions/routes.go.
test("the oversight analytics section is gated on the oversight_analytics control", () => {
  assert.match(
    source,
    /const oversightAnalyticsEnabled = controlEnabled\(pageContract, "oversight_analytics", false\)/,
    'oversightAnalyticsEnabled must come from controlEnabled(pageContract, "oversight_analytics", ...)',
  );
  // The panel wraps the analytics, so BOTH must sit inside the gate: the trigger button is as
  // oversight-only as the numbers behind it.
  assert.match(
    source,
    /\{oversightAnalyticsEnabled \? \(\s*<AnalyticsPanel/,
    "the analytics panel (button + drawer) must only render when oversightAnalyticsEnabled is true",
  );
  const gatedBlock = source.match(/\{oversightAnalyticsEnabled \? \([\s\S]*?\) : null\}/);
  assert.ok(gatedBlock && gatedBlock[0].includes("<OversightAnalytics"), "the analytics data must render inside the gated panel");
});

// Module rows name a module in the operator's words. `module` on an analytics row is the SOURCE
// module code stored on the item ("feed"); the queue's own module vocabulary is keyed by the
// NAVIGATION module ("feed_direction"), so a renderer joining on the code alone misses and prints
// the raw config token -- which the copy firewall bans on a leadership screen. The backend-owned
// module_label must therefore be preferred over both the nav map and the raw code.
test("module backlog rows prefer the backend-owned module_label", () => {
  const analytics = readFileSync(
    fileURLToPath(new URL("./oversight-analytics.tsx", import.meta.url)),
    "utf8",
  );
  assert.match(
    analytics,
    /moduleLabel\?\.trim\(\) \|\| moduleLabels\?\.get\(module\) \|\| module/,
    "label() must resolve backend module_label first, then the nav-vocabulary fallback, then the raw code",
  );
  for (const call of ["label(row.module, row.module_label)", "label(largest.module, largest.module_label)"]) {
    assert.ok(analytics.includes(call), `${call} must pass the row's backend-owned module_label`);
  }
});

// A backlog row links to the queue filtered to THAT module, and the link is built from the backend's
// nav_module key -- never from `module`, the source code the rows are grouped by ("feed"), which the
// queue's nav_module filter does not accept. Getting this wrong is silent: the queue would render
// unfiltered and look like a working link.
test("module backlog rows link to the queue by the backend nav_module key", () => {
  const analytics = readFileSync(
    fileURLToPath(new URL("./oversight-analytics.tsx", import.meta.url)),
    "utf8",
  );
  assert.match(
    analytics,
    /const href = row\.nav_module \? moduleHrefs\?\.get\(row\.nav_module\) : undefined/,
    "the href must be keyed by row.nav_module, not row.module",
  );
  assert.ok(
    !/moduleHrefs\?\.get\(row\.module\)/.test(analytics),
    "row.module is the source module code and is not a queue filter value",
  );
  // A module the registry cannot map must stay a plain card rather than link somewhere that quietly
  // drops the filter.
  assert.match(analytics, /if \(!href\) \{\s*return \(\s*<div key=\{row\.module\} className="vr-omod">/);
  // The page builds those hrefs with the same reset the module chips use: a keyset cursor from the
  // previous filter points into a different sequence.
  assert.match(
    source,
    /moduleHrefs=\{[\s\S]*?hrefWith\(sp, \{ nav_module: option\.key, category: null, \.\.\.RESET_ON_FILTER \}\)/,
    "module hrefs must clear category and RESET_ON_FILTER, exactly as the module chip row does",
  );
});

// The four age buckets partition videos_waiting. Re-deriving the total from anything else (say the
// module backlog rows) would let the legend contradict the KPI above it.
test("the backlog age shape totals the same rows the buckets partition", () => {
  const analytics = readFileSync(
    fileURLToPath(new URL("./oversight-analytics.tsx", import.meta.url)),
    "utf8",
  );
  assert.match(
    analytics,
    /const ageTotal =\s*ageBuckets\.up_to_1_day \+ ageBuckets\.one_to_three_days \+ ageBuckets\.three_to_seven_days \+ ageBuckets\.over_seven_days/,
    "ageTotal must be the sum of the four backend buckets",
  );
});

// Trajectory is arrivals minus verdicts over the SAME zero-filled 14-day series the chart draws. Two
// differently-scoped windows subtracted from each other would produce a number that looks precise
// and means nothing.
test("backlog trajectory is computed over the same series the chart renders", () => {
  const analytics = readFileSync(
    fileURLToPath(new URL("./oversight-analytics.tsx", import.meta.url)),
    "utf8",
  );
  assert.match(analytics, /const arrived14d = dailyVolume\.reduce\(\(total, day\) => total \+ day\.arrived, 0\)/);
  assert.match(analytics, /const verdicts14d = dailyVolume\.reduce\(\(total, day\) => total \+ day\.verdicts, 0\)/);
  assert.match(analytics, /const netChange = arrived14d - verdicts14d/);
});

// ===== Video Log (maintainer decision 2026-08-14) =====
//
// The video log is gated on a DIFFERENT capability from the oversight chrome above:
// permissions.VerificationEvidenceTimeline, which RoleVerifier holds and VerificationOversee is
// not. These pins exist so a later "simplification" onto oversight_analytics -- which would silently
// take the panel away from the verifier -- fails here instead of shipping. The backend halves are
// TestVerifyPageVideoLogControlIsCapabilityGated (adminui/app) and
// TestVerificationVideoLogRouteIsTimelineCapabilityNotOversight (permissions).
test("the video log is gated on its own video_log control, not on oversight", () => {
  assert.match(
    source,
    /const videoLogEnabled = controlEnabled\(pageContract, "video_log", false\)/,
    'videoLogEnabled must come from controlEnabled(pageContract, "video_log", ...)',
  );
  // The trigger button is as gated as the data behind it.
  assert.match(
    source,
    /\{videoLogEnabled \? \(\s*<VideoLogPanel/,
    "the video log panel (button + drawer) must only render when videoLogEnabled is true",
  );
  const gatedBlock = source.match(/\{videoLogEnabled \? \([\s\S]*?\) : null\}/);
  assert.ok(gatedBlock && gatedBlock[0].includes("<VideoLog"), "the video log data must render inside the gated panel");
  // The two panels must stay on separate flags. If the video log ever renders under
  // oversightAnalyticsEnabled, the verifier loses it. Checked against the EXTRACTED analytics block
  // rather than a spanning regex, which would run past that block's end into this one.
  const analyticsBlock = source.match(/\{oversightAnalyticsEnabled \? \([\s\S]*?\) : null\}/);
  assert.ok(analyticsBlock, "the oversight-analytics gated block must still be present");
  assert.ok(
    !analyticsBlock[0].includes("<VideoLog"),
    "the video log must NOT be nested inside the oversight-analytics gate — that would withdraw it from the verifier",
  );
});

test("the video log renders only backend-composed location and label copy", () => {
  const videoLog = readFileSync(fileURLToPath(new URL("./video-log.tsx", import.meta.url)), "utf8");
  // Operational location convention: the composed display is the only string a screen may render.
  // Joining shed_label and partition_label locally is the OL-3/OL-7 defect class.
  assert.ok(
    !/shed_label\s*\+|\$\{[^}]*shed_label[^}]*\}\s*-/.test(videoLog),
    "location display must come from operational_location_display, never a local shed+partition join",
  );
  assert.match(
    videoLog,
    /const display = shed\.operational_location_display/,
    "the summary row must render the backend-composed operational_location_display",
  );
  // A shed key carries "#", so it must be encoded before entering a query value.
  assert.match(
    videoLog,
    /encodeURIComponent\(shed\.shed_key\)/,
    "shed_key contains '#' and must be URL-encoded into the href, or the panel opens with no shed selected",
  );
  // Feed transport writes no subject label on purpose; a placeholder would read as missing data.
  assert.match(
    videoLog,
    /row\.subject_label \? <div className="small">\{row\.subject_label\}<\/div> : null/,
    "an absent subject label must render nothing, never a placeholder",
  );
});

// The day picker defaults to TODAY and expresses that default by ABSENCE (maintainer request
// 2026-08-15). Writing today's date into the URL would freeze a shared link on the day it was
// copied, which is the same trap ActionsDateFilter avoids for the queue's own dates.
test("the video log day filter defaults to today and writes today as an absent param", () => {
  const filter = readFileSync(fileURLToPath(new URL("./video-log-date-filter.tsx", import.meta.url)), "utf8");
  assert.match(
    filter,
    /if \(nextFrom === today\) next\.delete\(VIDEO_LOG_DATE_KEY\)/,
    "selecting today must DELETE the day param, so a bookmark keeps meaning 'today'",
  );
  // A single day, never a range: arrival times would otherwise be ambiguous about their day.
  assert.match(filter, /from=\{selected\}\s*\n\s*to=\{selected\}/, "the video log picker must select ONE day (from === to)");
  // The rendered day comes from the backend response, never a client guess that could drift from
  // the rows below it.
  const videoLog = readFileSync(fileURLToPath(new URL("./video-log.tsx", import.meta.url)), "utf8");
  assert.match(videoLog, /day=\{day\}/, "the picker must show the day the backend actually rendered");
  assert.match(
    videoLog,
    /business_date: day/,
    "the rendered day must be destructured from the backend response",
  );
});

// The panel must SURVIVE its own navigations (reported 2026-08-15: changing the day closed the
// drawer). The trigger opens it with a hash (#vi_video_log=open), which is client-local state, so
// any href or router call that rebuilds only the query string drops it. Every in-panel navigation
// therefore has to carry the panel key in the QUERY.
test("video log navigations keep the panel open", () => {
  const filter = readFileSync(fileURLToPath(new URL("./video-log-date-filter.tsx", import.meta.url)), "utf8");
  assert.match(
    filter,
    /next\.set\(VIDEO_LOG_PANEL_SELECTION_KEY, VIDEO_LOG_PANEL_ID\)/,
    "the day picker must re-assert the panel key, or picking a date closes the drawer",
  );
  // Both the shed drill-down and the back link rebuild the query, so both need it too.
  const shedHref = source.match(/shedHrefTemplate=\{hrefWith\(sp, \{[\s\S]*?\}\)\}/);
  assert.ok(shedHref, "expected the shed href template");
  assert.match(shedHref[0], /VIDEO_LOG_PANEL_SELECTION_KEY\]: VIDEO_LOG_PANEL_ID/, "shed links must keep the panel open");
  const backHref = source.match(/backHref=\{hrefWith\(sp, \{[\s\S]*?\}\)\}/);
  assert.ok(backHref, "expected the back href");
  assert.match(backHref[0], /VIDEO_LOG_PANEL_SELECTION_KEY\]: VIDEO_LOG_PANEL_ID/, "the back link must keep the panel open");
});

// The CSV is built from the SAME payload the table renders, so the file and the screen cannot
// disagree, and it must not execute as a formula when opened in a spreadsheet.
test("the video log CSV is safe and screen-faithful", () => {
  const csv = readFileSync(fileURLToPath(new URL("./video-log-csv-button.tsx", import.meta.url)), "utf8");
  assert.match(csv, /\/\^\[=\+\\-@\]\/\.test\(raw\)/, "CSV cells must be guarded against spreadsheet formula injection");
  assert.match(csv, /replace\(\/"\/g, '""'\)/, "CSV cells must escape embedded quotes per RFC 4180");
  const action = readFileSync(fileURLToPath(new URL("./video-log-export-action.ts", import.meta.url)), "utf8");
  // The export is one row per PROOF: the screen rowspans a work item's proofs and a CSV cannot.
  assert.match(action, /row\.proofs\.map\(\(proof\) => \[/, "the export must emit one row per video");
  // WHOLE DAY, not the current view (maintainer, 2026-08-15).
  assert.match(action, /allSheds: true/, "the export must ask for every shed, not the selected one");
  // Park MUST be in the file: shed names repeat across parks, so location alone renders two
  // different sheds identically — the OL-1 collision the location convention exists to prevent.
  assert.match(action, /row\.park_label \?\? ""/, "each export row must carry its park to disambiguate repeated shed names");
  assert.match(action, /row\.operational_location_display \?\? ""/, "each export row must carry its backend-composed location");
});

// Panel filters (maintainer request 2026-08-15): park, shed and search.
test("the video log filter row is backend-labelled and URL-driven", () => {
  const videoLog = readFileSync(fileURLToPath(new URL("./video-log.tsx", import.meta.url)), "utf8");
  // Park options come from the DAY's own sheds, keyed by park ID. Keying on the label is the OL-1
  // merge one level up — park names are as repeatable as shed names.
  assert.match(videoLog, /const id = shed\.park_id \?\? ""/, "park options must be keyed by park id, never by label");
  // Sheds are grouped by park for the same reason the queue's picker groups them: shed names repeat.
  assert.match(videoLog, /<optgroup key=\{park\} label=\{park\}>/, "the shed picker must group by park");
  // Filtering happens over the ALREADY-FETCHED day, so the option lists keep every park and shed the
  // day holds. Filtering server-side would collapse the options to whatever is already selected.
  assert.match(videoLog, /parkFilter \? sheds\.filter\(/, "park must narrow the fetched day, not re-query it");
  assert.match(videoLog, /needle\s*$/m, "search must be a normalised needle over the fetched rows");
});

test("applying a video log filter keeps the panel open", () => {
  // The filter form rebuilds the query, so it must re-assert the panel key exactly as the day
  // picker and the shed links do — otherwise Apply closes the drawer it was submitted from.
  const form = source.match(/filterHiddenInputs=\{[\s\S]*?<\/>/);
  assert.ok(form, "expected the filter hidden inputs block");
  assert.match(
    form[0],
    /name=\{VIDEO_LOG_PANEL_SELECTION_KEY\} value=\{VIDEO_LOG_PANEL_ID\}/,
    "the filter form must carry the panel key, or Apply closes the drawer",
  );
  const clear = source.match(/clearHref=\{hrefWith\(sp, \{[\s\S]*?\}\)\}/);
  assert.ok(clear && clear[0].includes("VIDEO_LOG_PANEL_SELECTION_KEY]: VIDEO_LOG_PANEL_ID"), "Clear must keep the panel open");
});

// Closing must actually close (reported 2026-08-15: the X did nothing).
//
// closeHref is what the overlay writes when it cannot simply pop history — a deep link, or any URL
// reached by a real navigation. A closeHref that still carries the panel's own selection key writes
// a URL that says "open", and the hook reopens from it immediately. Both panels must strip their
// own key.
test("each panel's closeHref strips its own selection key", () => {
  const analytics = source.match(/closeHref=\{hrefWith\(sp, \{ \[ANALYTICS_PANEL_SELECTION_KEY\]: null \}\)\}/);
  assert.ok(analytics, "the analytics closeHref must delete ANALYTICS_PANEL_SELECTION_KEY");
  const videoLog = source.match(/closeHref=\{hrefWith\(sp, \{[\s\S]*?\}\)\}/g)?.find((m) => m.includes("VIDEO_LOG_PANEL_SELECTION_KEY]: null"));
  assert.ok(videoLog, "the video log closeHref must delete VIDEO_LOG_PANEL_SELECTION_KEY");
});

// The reason the above silently failed: a constant exported from a "use client" module reaches a
// Server Component as a client-reference proxy, not the string — so the CLIENT hook read the URL
// key correctly and opened, while the SERVER-built closeHref deleted a key it never matched. These
// param modules must stay server-safe, or the same defect returns with no error to point at it.
test("panel param modules are server-safe", () => {
  for (const name of ["video-log-params.ts", "analytics-panel-params.ts", "actions-date-params.ts"]) {
    const text = readFileSync(fileURLToPath(new URL(`./${name}`, import.meta.url)), "utf8");
    assert.ok(!/^\s*["']use client["']/m.test(text), `${name} must NOT be a client module — the server page imports its constants`);
  }
  // And the page must import them from those modules, never from the client panels.
  assert.ok(
    !/import \{[^}]*ANALYTICS_PANEL_SELECTION_KEY[^}]*\} from "\.\/analytics-panel"/.test(source),
    "the page must import the analytics panel constants from analytics-panel-params, not the client component",
  );
  assert.ok(
    !/import \{[^}]*VIDEO_LOG_PANEL_SELECTION_KEY[^}]*\} from "\.\/video-log-panel"/.test(source),
    "the page must import the video log constants from video-log-params, not the client component",
  );
});
