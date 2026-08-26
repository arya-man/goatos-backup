import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { Filter, PlayCircle } from "lucide-react";

import { Tag } from "@/components/ui-primitives";
import { controlEnabled, copy, table, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError, listVerificationQueue, type VerificationItemStatus, type VerificationQueueItem } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { fmtDateTime, humanizeDurationMs, todayIso } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { parseScope } from "@/lib/scope";
import { ActionsDateFilter } from "./actions-date-filter";
// Server-safe module on purpose: a constant imported across the "use client" boundary arrives as a
// client-reference proxy, not the string, and every date selection silently fell back to today.
import { DATE_FROM_PARAM, DATE_TO_PARAM } from "./actions-date-params";
import { AnalyticsPanel } from "./analytics-panel";
// Server-safe module on purpose: see analytics-panel-params.ts — importing these across the
// "use client" boundary made closeHref unable to strip its own key, so the drawer would not close.
import { ANALYTICS_PANEL_ID, ANALYTICS_PANEL_SELECTION_KEY } from "./analytics-panel-params";
import { OversightAnalytics } from "./oversight-analytics";
import { Randomization } from "./randomization";
import { RandomizationPanel } from "./randomization-panel";
// Server-safe module on purpose: see randomization-panel-params.ts.
import { RANDOMIZATION_PANEL_ID, RANDOMIZATION_PANEL_SELECTION_KEY } from "./randomization-panel-params";
import { VideoLogPanel } from "./video-log-panel";
// Server-safe module on purpose: a constant imported across the "use client" boundary arrives as a
// client-reference proxy, not the string, so vl_date/vl_shed silently never matched.
import {
  VIDEO_LOG_DATE_KEY,
  VIDEO_LOG_PANEL_ID,
  VIDEO_LOG_PANEL_SELECTION_KEY,
  VIDEO_LOG_PARK_KEY,
  VIDEO_LOG_QUERY_KEY,
  VIDEO_LOG_SHED_KEY,
  VIDEO_LOG_SHED_TOKEN,
} from "./video-log-params";
import { VideoLog } from "./video-log";
import { VerificationReviewDrawer } from "./verification-review-drawer";
import { VerificationQueueTelemetry } from "./verification-queue-telemetry";
import { ToxinReviewScreen, toxinTabLabel } from "./toxin-review-section";

const PATHNAME = "/verify";

// Changing a filter invalidates the open row, the keyset cursor, its back-trail, and any verdict
// feedback banner: all four describe the queue as it was BEFORE the change. Carrying a cursor
// across a filter change is the worst of them — cursors are keyset positions in one filtered
// sequence, so reusing one lands on an unrelated slice of the new queue.
const RESET_ON_FILTER = { vi_row: null, vi_cursor: null, vi_trail: null, vi_open_first: null, va_status: null, va_code: null };


export async function VerificationReviewPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  // "all" = every status together; anything else is a single-status tab.
  const status = verificationStatus(one(sp, "status"));
  const category = one(sp, "category")?.trim();
  // The module filter (maintainer request 2026-08-11): the same Vaccination / Weighing / Feed /
  // Counts / Milk grouping the phone's verifier drawer uses. It is a MODULE, not an action type --
  // Feed alone covers distribution, packing and transport -- so it is sent as `nav_module` and the
  // backend expands it into that module's category set. The action-type SELECT removed on
  // 2026-08-07 is deliberately not coming back: this row shows the current selection, whereas that
  // control had a defaultValue that never matched the URL and sat on an unrelated action type.
  const navModule = one(sp, "nav_module")?.trim();
  const shedId = one(sp, "shed_id")?.trim();
  const scope = parseScope(sp);
  // The capture-date filter (maintainer decision 2026-08-12). The board LANDS ON TODAY: absent
  // params mean today, so the default view is one business day and a bookmark keeps meaning
  // "today" rather than freezing on the day it was taken. Anything older is reached by picking a
  // past day or a span, which is why the picker exists at all — before it, this screen showed the
  // whole pending backlog and had no way to narrow it.
  const today = todayIso();
  const dateRange = parseDateRange(sp, today);
  const trail = decodeTrail(one(sp, "vi_trail"));

  // The TOXIN review tab (maintainer decision 2026-08-25). Gated on the backend-declared
  // toxin_tab control (permissions.ToxinVerdict — CEO/CXO only; the verifier never holds it and
  // never sees the chip). When active, the toxin screen replaces the verification queue entirely:
  // toxin is deliberately NOT a verification category, so its rows never mix into this table.
  const toxinTabEnabled = controlEnabled(pageContract, "toxin_tab", false);
  const toxinActive = toxinTabEnabled && one(sp, "toxin") === "1";
  if (toxinActive) {
    return <ToxinReviewScreen searchParams={sp} pageContract={pageContract} />;
  }

  // ONE read on the critical path. The staff roster the re-assign picker offers used to be fetched
  // here too -- listStaffPositions with limit 500, awaited alongside the queue on every load -- for
  // a control that lives inside the drawer, is only reachable after a row is opened, and is only
  // usable on a rejected item with a linked SOP task. It now loads lazily, per park, when the
  // drawer opens (loadReassignPositionsAction), so the queue paints without waiting on it.
  const queue = await listVerificationQueue({
    status,
    category,
    navModule,
    // One day collapses to the backend's single `business_date`; a span uses the range pair. Both
    // are the same inclusive Asia/Kolkata capture-date scope, and sending both at once is a 400
    // (invalid_date_scope), so this is an either/or, never a merge.
    ...(dateRange.from === dateRange.to
      ? { businessDate: dateRange.from }
      : { businessDateFrom: dateRange.from, businessDateTo: dateRange.to }),
    parkId: scope.parkId,
    shedId,
    limit: 20,
    cursor: one(sp, "vi_cursor"),
  });
  const authError = firstAuthRequiredError(queue);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const items = queue.ok ? queue.data.items : [];
  const selectedId = one(sp, "vi_row") ?? (one(sp, "vi_open_first") === "1" ? items[0]?.item_id : undefined);
  // `?? []` is not defensive noise: admin-web and the API deploy separately, so a browser can hit a
  // backend one release behind that has no `modules` in its filter options. The contract declares
  // the field required, which means the generated type asserts it is there — the renderer must
  // still not throw on the older payload; it simply shows no module row.
  const modules = (queue.ok ? queue.data.filter_options.modules : []) ?? [];
  // module_key is the backend's own answer to "which module is this queue showing" — it resolves
  // from ?nav_module AND from a single ?category, so the chip row reflects a nav-leaf selection
  // without the frontend re-deriving which module owns that category.
  const selectedModuleKey = (queue.ok ? queue.data.filter_options.module_key : undefined) ?? navModule ?? "";
  // Whether the module chip row is offered at all. Withdrawn for the verifier lens, whose sidebar
  // already lists the same modules; defaults to offered so leadership — and an older backend that
  // declares no such control — keeps it.
  const moduleFilterOffered = controlEnabled(pageContract, "module_filter", true);
  const actionTypes = queue.ok ? queue.data.filter_options.action_types : [];
  const statuses = queue.ok ? queue.data.filter_options.statuses : [];
  const sheds = queue.ok ? queue.data.filter_options.sheds : [];
  // Park grouping for the shed picker. The backend already returns the options park-first, so this
  // preserves arrival order instead of re-sorting: the park order and the shed order inside it are
  // the backend's, and re-sorting here would be a second opinion about a list it already ordered.
  // An option carrying no park keeps its place in a plain list rendered before the groups — the
  // backend omits the park only when it genuinely cannot say which one, and dropping such a shed
  // would hide a filter that still works.
  const shedsWithoutPark = sheds.filter((option) => !option.park_label);
  const shedsByPark = sheds.reduce<Array<[string, typeof sheds]>>((groups, option) => {
    if (!option.park_label) return groups;
    const existing = groups.find(([parkLabel]) => parkLabel === option.park_label);
    if (existing) existing[1].push(option);
    else groups.push([option.park_label, [option]]);
    return groups;
  }, []);
  // "Vaccination · Vaccination" and "Weighing · Weighing" were what this produced for every row in
  // those two modules: the category registry gives a module label and a page label, and for a
  // module with a single page they are the same word (bootstrap/api.go). Joining them
  // unconditionally turned the column into a stutter that carried no information. Join only when
  // the page actually narrows the module ("Feed · Feed Packing", "Counts · Birth").
  const typeLabels = new Map(
    actionTypes.map((option) => [
      option.category,
      option.label === option.module_label ? option.label : `${option.module_label} · ${option.label}`,
    ]),
  );
  // Row/drawer labels map an ITEM's status to its display label, so the statusless "All" tab is
  // excluded — it is a filter tab, never a status a row can be in.
  const statusOptionsWithStatus = statuses.filter(
    (option): option is typeof option & { status: VerificationItemStatus } => Boolean(option.status),
  );
  const statusLabelRecord = Object.fromEntries(statusOptionsWithStatus.map((option) => [option.status, option.label]));
  const feedback = { status: one(sp, "va_status"), code: one(sp, "va_code") };
  const columns = tableLabels(pageContract, "verification-actions");
  const tableContract = table(pageContract, "verification-actions");
  // Gates the CROSS-MODULE oversight chrome (module chips, capture-date range picker):
  // permissions.VerificationOversee, backend/internal/permissions/permissions.go. These filters
  // were built for the CEO's oversight view but rendered for every role that can open /verify,
  // including RoleVerifier, before this fix (STG incident, 2026-08-12) -- because /verify is one
  // role-agnostic component. This is the UI half of the gate; the backend independently ignores
  // nav_module/business_date_from/business_date_to for callers without the capability and blanks
  // filter_options.modules, so a hand-edited URL cannot reach the oversight query shape even if
  // this control were somehow bypassed. See docs/decisions/role-scoped-ui-is-capability-gated.md.
  //
  // Status chips and the shed filter are NOT gated here: both predate the oversight rollout (the
  // verifier's original working-queue screen, commit 89b16c0fa / fe06be1ed) and stay available to
  // every role that can open this page.
  const oversightFiltersEnabled = controlEnabled(pageContract, "oversight_filters", false);
  // Gates the CAPTURE-DATE RANGE picker. SPLIT OUT of oversightFiltersEnabled (maintainer decision
  // 2026-08-17) and held by the verifier as well as leadership: the 2026-08-12 incident was about
  // CROSS-MODULE chrome, and a date range crosses no module boundary -- it narrows the caller's own
  // queue to the days she is working. Without it her board is pinned to a date she cannot change,
  // which on real data is an empty screen sitting on top of a full backlog. The module chips above
  // stay on the oversight capability. Backend half: permissions.VerificationFilterByCaptureDate +
  // ports.ListQueueParams.CaptureDateFilterEnabled.
  const captureDateFilterEnabled = controlEnabled(pageContract, "capture_date_filter", false);
  // Gates the CEO/PC-Director-only analytics section rendered ABOVE the queue table (KPI strip,
  // pending-by-module, per-verifier activity + watch integrity). Same capability
  // (permissions.VerificationOversee) as oversightFiltersEnabled above, but a DISTINCT contract
  // control -- see compileVerificationReviewControls's oversight_analytics doc comment for why
  // this is not folded into oversight_filters.
  const oversightAnalyticsEnabled = controlEnabled(pageContract, "oversight_analytics", false);
  // Gates the VIDEO LOG panel (button + drawer). A DIFFERENT capability from the two above:
  // permissions.VerificationEvidenceTimeline, which the verifier holds and VerificationOversee is
  // not. Keeping it a distinct control is what lets her have this panel without the oversight
  // chrome. See compileVerificationReviewControls's video_log doc comment.
  const videoLogEnabled = controlEnabled(pageContract, "video_log", false);
  // Gates the CEO-only RANDOMIZATION section: per module, the share of proof the verifier must
  // review (maintainer decision 2026-08-26). Its own control, on permissions.VerificationSampling
  // -- NARROWER than the oversight capability above, which the PC Director also holds. See
  // compileVerificationReviewControls's randomization doc comment.
  const randomizationEnabled = controlEnabled(pageContract, "randomization", false);

  // The mock's dot-legend pills (mock/verifier-web-mock.html .legend/.lg) need a live count per
  // status for the CURRENT feature+scope. This is the backend's own whole-filter aggregate
  // (domain.QueueStatusCounts, computed by one indexed GROUP BY over verification_items for the
  // SAME scope as the queue page — see backend/internal/verification/adapters/postgres/
  // repository.go) — NEVER derived by fetching a page and grouping it here. A page-derived count
  // was tried and reverted: it silently undercounts once the in-scope backlog exceeds the fetched
  // page, which is exactly the banned "capped read-time rollup presented as truth" pattern
  // (docs/decisions/scale-anti-patterns.md).
  const statusCounts: Record<string, number> = queue.ok
    ? { pending: queue.data.filter_options.counts.pending, approved: queue.data.filter_options.counts.approved, rejected: queue.data.filter_options.counts.rejected }
    : { pending: 0, approved: 0, rejected: 0 };
  const legendDotColor: Record<string, string> = { pending: "var(--warn)", approved: "var(--ok)", rejected: "var(--danger)" };

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pageContract, "crumb")} / <b>{pageContract.title}</b>
          </div>
          <h1>{pageContract.title}</h1>
          <div className="sub">{pageContract.subtitle}</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        {/* Oversight analytics live behind a right-side panel, not stacked above the queue: the
            queue is the working surface. Shown ONLY when the oversight_analytics contract control
            is enabled -- the same capability (permissions.VerificationOversee) that gates the
            endpoint the panel's contents read -- so a verifier gets neither the button nor the data.

            Rollout fallback only for module names: the analytics rows now carry their own
            backend-owned module_label. This map (filter_options.modules, the same vocabulary the
            chip row below renders) keeps the backlog rows readable against a backend that predates
            that field. */}
        {oversightAnalyticsEnabled ? (
          <AnalyticsPanel
            pageContract={pageContract}
            // MUST drop the panel's own key. closeHref is what the overlay writes when it cannot
            // simply pop history, so a href that still carries vi_analytics=open closes the drawer
            // and immediately reopens it from the URL.
            closeHref={hrefWith(sp, { [ANALYTICS_PANEL_SELECTION_KEY]: null })}
            initialOpen={one(sp, ANALYTICS_PANEL_SELECTION_KEY) === ANALYTICS_PANEL_ID}
          >
            <OversightAnalytics
              pageContract={pageContract}
              moduleLabels={new Map(modules.map((option) => [option.key, option.label]))}
              // A backlog row is a question ("729 waiting in Feed") whose answer is the queue itself,
              // so each row links to that queue exactly as the module chip row does -- same
              // nav_module key, same RESET_ON_FILTER (a cursor from the previous filter points into a
              // different sequence), same category clear. Built here because only the page has the
              // live search params; passed as plain data because the panel is a client component.
              moduleHrefs={
                new Map(
                  modules.map((option) => [
                    option.key,
                    hrefWith(sp, { nav_module: option.key, category: null, ...RESET_ON_FILTER }),
                  ]),
                )
              }
            />
          </AnalyticsPanel>
        ) : null}
        {/* The VIDEO LOG: one business day, per shed, when each proof arrived (maintainer decision
            2026-08-14). A SECOND panel beside Analytics, not a tab inside it, because the two are
            gated on DIFFERENT capabilities: this follows permissions.VerificationEvidenceTimeline,
            which the VERIFIER holds, while Analytics follows VerificationOversee, which she does
            not. Folding them together would have handed her the oversight chrome that the
            2026-08-12 STG incident deliberately took away. */}
        {videoLogEnabled ? (
          <VideoLogPanel
            pageContract={pageContract}
            // MUST drop the panel key, and the panel's own filters with it.
            //
            // Every in-panel navigation (day, shed, Apply) re-asserts vi_video_log=open in the QUERY
            // so the drawer survives it. That made a closeHref which preserved the whole query
            // unable to close anything: the overlay wrote a URL that still said open and the hook
            // reopened from it. Dropping the filters too means the next open starts on the day
            // summary rather than silently restoring a shed the reader had already left.
            closeHref={hrefWith(sp, {
              [VIDEO_LOG_PANEL_SELECTION_KEY]: null,
              [VIDEO_LOG_SHED_KEY]: null,
              [VIDEO_LOG_PARK_KEY]: null,
              [VIDEO_LOG_QUERY_KEY]: null,
              [VIDEO_LOG_DATE_KEY]: null,
            })}
            initialOpen={one(sp, VIDEO_LOG_PANEL_SELECTION_KEY) === VIDEO_LOG_PANEL_ID}
          >
            <VideoLog
              pageContract={pageContract}
              // The panel's own day, independent of the queue's capture-date filter: the queue may
              // be showing a range or the whole backlog, but a video log is always ONE day.
              businessDate={one(sp, VIDEO_LOG_DATE_KEY) || undefined}
              parkId={scope.parkId || undefined}
              selectedShedKey={one(sp, VIDEO_LOG_SHED_KEY) || undefined}
              parkFilter={one(sp, VIDEO_LOG_PARK_KEY) || undefined}
              query={one(sp, VIDEO_LOG_QUERY_KEY) || undefined}
              filterAction={PATHNAME}
              // The filter form REPLACES the panel's own three params and keeps everything else --
              // including the selected day and the panel key, without which Apply would close the
              // drawer it was submitted from.
              filterHiddenInputs={
                <>
                  {hiddenInputs(sp, [VIDEO_LOG_PARK_KEY, VIDEO_LOG_SHED_KEY, VIDEO_LOG_QUERY_KEY])}
                  <input type="hidden" name={VIDEO_LOG_PANEL_SELECTION_KEY} value={VIDEO_LOG_PANEL_ID} />
                </>
              }
              clearHref={hrefWith(sp, {
                [VIDEO_LOG_PARK_KEY]: null,
                [VIDEO_LOG_SHED_KEY]: null,
                [VIDEO_LOG_QUERY_KEY]: null,
                [VIDEO_LOG_PANEL_SELECTION_KEY]: VIDEO_LOG_PANEL_ID,
              })}
              basePath={PATHNAME}
              today={today}
              // The calendar's MECHANICS copy is shared with the queue's date filter — one
              // vocabulary for one calendar. Only the FIELD label differs, and it must: the queue
              // filters on capture date, while this picks the day whose arrivals are listed, so
              // reusing "Capture date" here labelled the control with the wrong fact.
              dateLabels={{
                field: copy(pageContract, "video_log.day"),
                today: copy(pageContract, "filter.date.today"),
                single: copy(pageContract, "filter.date.single"),
                range: copy(pageContract, "filter.date.range"),
                aria: copy(pageContract, "filter.date.aria"),
                previousMonth: copy(pageContract, "filter.date.previous_month"),
                nextMonth: copy(pageContract, "filter.date.next_month"),
                rangeStartHint: copy(pageContract, "filter.date.range_start_hint"),
                rangeEndHint: copy(pageContract, "filter.date.range_end_hint"),
                rangeSeparator: copy(pageContract, "filter.date.range_separator"),
              }}
              // An href TEMPLATE rather than a per-shed map: only the page knows the live search
              // params, but only the component knows which sheds the day actually holds (they come
              // from its own fetch). The component substitutes each shed's key into the token. A
              // callback would be the obvious alternative and does not survive being passed as
              // children of a client component.
              // Both hrefs carry the panel key so the drawer SURVIVES the navigation. The trigger
              // opens this panel with a hash (#vi_video_log=open) and a query-only href drops it,
              // which closed the drawer on every shed click and every date change.
              shedHrefTemplate={hrefWith(sp, {
                [VIDEO_LOG_SHED_KEY]: VIDEO_LOG_SHED_TOKEN,
                [VIDEO_LOG_PANEL_SELECTION_KEY]: VIDEO_LOG_PANEL_ID,
              })}
              backHref={hrefWith(sp, {
                [VIDEO_LOG_SHED_KEY]: null,
                [VIDEO_LOG_PANEL_SELECTION_KEY]: VIDEO_LOG_PANEL_ID,
              })}
              queueHrefs={
                new Map(
                  modules.map((option) => [
                    option.key,
                    hrefWith(sp, { nav_module: option.key, category: null, ...RESET_ON_FILTER }),
                  ]),
                )
              }
            />
          </VideoLogPanel>
        ) : null}
        {/* RANDOMIZATION: how much of each module's proof the verifier is required to watch
            (maintainer decision 2026-08-26). A THIRD panel, not a tab inside Analytics, because it
            is gated on a THIRD capability: permissions.VerificationSampling is CEO-only, while
            Analytics follows VerificationOversee, which the PC Director also holds. Folding them
            together would hand a director the control over how deeply his own department's work is
            checked. */}
        {randomizationEnabled ? (
          <RandomizationPanel
            pageContract={pageContract}
            // MUST drop the panel's own key: closeHref is what the overlay writes when it cannot
            // pop history, and a href that still says open closes the drawer and immediately
            // reopens it from the URL.
            closeHref={hrefWith(sp, { [RANDOMIZATION_PANEL_SELECTION_KEY]: null })}
            initialOpen={one(sp, RANDOMIZATION_PANEL_SELECTION_KEY) === RANDOMIZATION_PANEL_ID}
          >
            <Randomization
              pageContract={pageContract}
              // Saving a share redirects back here, so the return URL re-asserts the panel key in
              // the QUERY -- otherwise the CEO would be dropped back on the queue with the drawer
              // shut after every change.
              returnTo={hrefWith(sp, { [RANDOMIZATION_PANEL_SELECTION_KEY]: RANDOMIZATION_PANEL_ID })}
            />
          </RandomizationPanel>
        ) : null}
      </div>

      {queue.ok ? null : (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{copy(pageContract, "state.queue_unavailable")}</b>
          <div className="small" style={{ marginTop: 4 }}>
            {copy(pageContract, "state.queue_unavailable_body")}
          </div>
          <div className="small muted" style={{ marginTop: 4 }}>
            {queue.error.code ?? queue.error.kind} · {queue.error.message}
          </div>
        </div>
      )}

      <VerificationQueueTelemetry
        category={category}
        parkId={scope.parkId}
        shedId={shedId}
        status={status}
        enabled={controlEnabled(pageContract, "record_verdict", false)}
      >
        <section className="card vr-board" style={{ minWidth: 0 }}>
        <div className="bt">{copy(pageContract, "board.title")}</div>

        {/* Module filter — the same grouping the phone's verifier drawer uses (Vaccination,
            Weighing, Feed, Counts, Milk, Health). The vocabulary, the labels and the order are the
            registry's (filter_options.modules); nothing here is derived from the rows on screen, so
            a module with an empty queue stays selectable instead of vanishing.

            Selection is read from filter_options.module_key, NOT from ?nav_module, so a nav leaf
            that scopes the screen with ?category= lights up its own module chip too — the two ways
            in cannot disagree about what is selected.

            Each chip clears `category`: it is a WIDER selection than one page, and leaving a
            sibling module's category behind would ask the backend for a contradiction it answers
            400 (module_category_conflict).

            Whether the row is OFFERED is the backend's call, not this renderer's: the verifier lens
            puts one sidebar leaf per evidence module in front of exactly the principal who would
            otherwise see the same choice twice, so it withdraws `module_filter` for her and leaves
            it enabled for leadership, whose single nav item makes this row their only module
            picker. Defaulting to true keeps a backend one release behind — which declares no such
            control — showing the row. */}
        {moduleFilterOffered && oversightFiltersEnabled && modules.length > 1 ? (
          <div className="vr-legend" role="group" aria-label={copy(pageContract, "filter.module")}>
            <Link
              href={hrefWith(sp, { nav_module: null, category: null, ...RESET_ON_FILTER })}
              replace
              scroll={false}
              className={`vr-lg${selectedModuleKey ? "" : " on"}`}
            >
              {copy(pageContract, "filter.all_modules")}
            </Link>
            {modules.map((option) => (
              <Link
                key={option.key}
                href={hrefWith(sp, { nav_module: option.key, category: null, ...RESET_ON_FILTER })}
                replace
                scroll={false}
                className={`vr-lg${selectedModuleKey === option.key ? " on" : ""}`}
              >
                {option.label}
              </Link>
            ))}
          </div>
        ) : null}

        {/* The TOXIN chip — offered ONLY when the backend contract enables toxin_tab (CEO/CXO,
            permissions.ToxinVerdict). A ?toxin=1 toggle: selecting it swaps this whole board for
            the toxin review screen above. Styled as a .vr-lg chip so it sits in the same chip
            vocabulary as the module row, in its own row because it is a different surface, not a
            module of this queue. */}
        {toxinTabEnabled ? (
          <div className="vr-legend" role="group" aria-label={toxinTabLabel(pageContract)}>
            <Link
              href={hrefWith(sp, { toxin: "1", ...RESET_ON_FILTER })}
              replace
              scroll={false}
              className="vr-lg"
            >
              {toxinTabLabel(pageContract)}
            </Link>
          </div>
        ) : null}

        {/* The action-type select was REMOVED (maintainer decision 2026-08-07). The left nav
            already scopes this screen -- every leaf sets ?category= -- so the dropdown was a
            second, competing scope control for a choice the verifier had just made in the sidebar.
            It was also wrong: its defaultValue never matched the URL category, so it sat on an
            unrelated action type on every category the nav could reach.

            Shed is now the only filter, so the whole row is conditional on there being sheds to
            choose between: without this, a module with no shed options (Birth, Death) rendered an
            Apply/Clear pair with nothing to apply. */}
        {/* The filter row always renders now, because the capture-date picker always applies —
            unlike Shed, which is conditional on the selected module having sheds to choose
            between (Birth and Death have none, and an Apply button with nothing to apply is
            worse than no row). */}
        <div className="vr-frow">
          {captureDateFilterEnabled ? (
            <ActionsDateFilter
              basePath={PATHNAME}
              from={dateRange.from}
              to={dateRange.to}
              today={today}
              defaultFrom={businessDaysBefore(today, DEFAULT_QUEUE_WINDOW_DAYS)}
              labels={{
                field: copy(pageContract, "filter.date"),
                today: copy(pageContract, "filter.date.today"),
                single: copy(pageContract, "filter.date.single"),
                range: copy(pageContract, "filter.date.range"),
                aria: copy(pageContract, "filter.date.aria"),
                previousMonth: copy(pageContract, "filter.date.previous_month"),
                nextMonth: copy(pageContract, "filter.date.next_month"),
                rangeStartHint: copy(pageContract, "filter.date.range_start_hint"),
                rangeEndHint: copy(pageContract, "filter.date.range_end_hint"),
                rangeSeparator: copy(pageContract, "filter.date.range_separator"),
              }}
            />
          ) : null}
          {sheds.length ? (
            <>
            <form action={PATHNAME} style={{ display: "contents" }}>
              {/* vd_from / vd_to are NOT excluded: the shed submit must preserve the selected
                  capture date, or applying a shed filter would silently reset the board to today. */}
              {hiddenInputs(sp, ["category", "shed_id", "vi_row", "vi_cursor", "vi_trail", "va_status", "va_code"])}
              {/* Carries the sidebar's scope through the submit; without it, filtering by shed
                  would silently widen the queue back to every module. */}
              {category ? <input type="hidden" name="category" value={category} /> : null}
              <div className="vr-fld fld" style={{ marginBottom: 0 }}>
                <label htmlFor="verification-shed">{copy(pageContract, "filter.shed")}</label>
                {/* Grouped by park, because a shed NAME is not unique across the farm: Castro,
                    Gandhi, Godel 1, Godel 2, Mandela 1, Mandela 2 and Yashoda each exist in BOTH
                    parks, so nine of the sixty-seven options on a real STG day were exact duplicate
                    labels sitting next to each other. The value was always the right shed — the id
                    is a UUID — but a reader could not tell which one she was picking, and the park
                    holding more pens read as the only park present.

                    The park comes from the option's own park_label; it is NOT concatenated into the
                    shed's display, which belongs to oploc. Options with no park (the backend sends
                    none when an option's rows disagree) stay in a plain ungrouped list ABOVE the
                    groups rather than being dropped or filed under a guess. */}
                <select id="verification-shed" name="shed_id" className="vr-selbtn" defaultValue={shedId ?? ""}>
                  <option value="">{copy(pageContract, "filter.all_sheds")}</option>
                  {shedsWithoutPark.map((option) => (
                    <option key={option.id} value={option.id}>
                      {option.operational_location_display || option.label}
                    </option>
                  ))}
                  {shedsByPark.map(([parkLabel, parkSheds]) => (
                    <optgroup key={parkLabel} label={parkLabel}>
                      {parkSheds.map((option) => (
                        <option key={option.id} value={option.id}>
                          {option.operational_location_display || option.label}
                        </option>
                      ))}
                    </optgroup>
                  ))}
                </select>
              </div>
              <button type="submit" className="btn sm">
                <Filter className="ic" aria-hidden="true" />
                {copy(pageContract, "filter.apply")}
              </button>
            </form>
            {/* Deliberately does NOT clear `category`: that is the sidebar's selection, not a
                filter the verifier set here. Clearing it stranded her on every module's queue at
                once while the nav still highlighted the one she had picked. It DOES clear the
                date pair, which returns the board to its default recent window. */}
            <Link
              href={hrefWith(sp, {
                shed_id: null,
                status: null,
                nav_module: null,
                [DATE_FROM_PARAM]: null,
                [DATE_TO_PARAM]: null,
                ...RESET_ON_FILTER,
              })}
              replace
              scroll={false}
              className="lk small"
            >
              {copy(pageContract, "filter.clear_all")}
            </Link>
            </>
          ) : null}
        </div>

        {statuses.length ? (
          <div className="vr-legend">
            {statusOptionsWithStatus.map((option) => (
              <Link
                key={option.key}
                href={hrefWith(sp, { status: option.status, vi_row: null, vi_cursor: null, vi_trail: null, va_status: null, va_code: null })}
                replace
                scroll={false}
                className={`vr-lg${status === option.status ? " on" : ""}`}
              >
                <i style={{ background: legendDotColor[option.status] }} />
                {option.label}
                <span className="n">{statusCounts[option.status] ?? 0}</span>
              </Link>
            ))}
          </div>
        ) : null}

        <div className="vr-secthd">
          <h2>{tableContract.title}</h2>
          <span className="hint">{copy(pageContract, "table.hint")}</span>
        </div>

        <div style={{ overflowX: "auto" }} tabIndex={0} role="group">
          <table data-enh="1" className="vr-table">
            <thead>
              <tr>
                {columns.map((label) => (
                  <th key={label}>{label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.length === 0 ? (
                <tr>
                  <td colSpan={columns.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {queue.ok ? copy(pageContract, "state.empty") : copy(pageContract, "state.queue_unavailable")}
                    </div>
                  </td>
                </tr>
              ) : (
                items.map((item) => (
                  <QueueRow
                    key={item.item_id}
                    item={item}
                    actionTypeLabel={String(typeLabels.get(item.category) ?? item.category)}
                    searchParams={sp}
                    pageContract={pageContract}
                    statusLabels={statusLabelRecord}
                  />
                ))
              )}
            </tbody>
          </table>
        </div>

        {/* Keyset pagination. The queue read is cursor-based (OFFSET is banned on this path), so
            there is no page number to jump to and no way to read backwards from a cursor alone.
            `vi_trail` carries the cursors already consumed, newest last: Next pushes the cursor
            that produced the CURRENT page, Previous pops it and re-reads with the one beneath. Each
            direction is therefore a real indexed keyset read.

            Before this the pager was a lone forward link: no way back without the browser button,
            and no indication of where in the backlog the verifier was -- 33 pending items at 20 a
            page, with nothing saying which 20 these were. Every label here stays backend-owned
            (pagination.previous / pagination.position / pagination.next). */}
        {queue.ok && (queue.data.next_cursor || trail.length) ? (
          <div className="pager" style={{ marginTop: 12, display: "flex", alignItems: "center", gap: 10 }}>
            {trail.length ? (
              <Link
                href={hrefWith(sp, {
                  vi_cursor: trail[trail.length - 1] || null,
                  vi_trail: encodeTrail(trail.slice(0, -1)),
                  vi_row: null,
                  va_status: null,
                  va_code: null,
                })}
                className="btn sm"
                replace
                scroll={false}
              >
                {copy(pageContract, "pagination.previous")}
              </Link>
            ) : null}
            <span className="small muted">
              {copy(pageContract, "pagination.position")} {trail.length + 1}
            </span>
            {queue.data.next_cursor ? (
              <Link
                href={hrefWith(sp, {
                  vi_cursor: queue.data.next_cursor,
                  // The cursor that produced THIS page becomes the way back to it. "" is a real
                  // trail entry (the first page has no cursor) and must survive the round trip.
                  vi_trail: encodeTrail([...trail, one(sp, "vi_cursor") ?? ""]),
                  vi_row: null,
                  va_status: null,
                  va_code: null,
                })}
                className="btn sm"
                replace
                scroll={false}
              >
                {copy(pageContract, "pagination.next")}
              </Link>
            ) : null}
          </div>
        ) : null}
        </section>
      </VerificationQueueTelemetry>

      <VerificationReviewDrawer
        items={items}
        initialSelectedId={selectedId ?? undefined}
        nextCursor={queue.ok ? (queue.data.next_cursor ?? undefined) : undefined}
        nextTrail={queue.ok && Boolean(queue.data.next_cursor) ? (encodeTrail([...trail, one(sp, "vi_cursor") ?? ""]) ?? undefined) : undefined}
        actionTypeLabels={Object.fromEntries(typeLabels)}
        searchParams={sp}
        feedback={feedback}
        pageContract={pageContract}
        statusLabels={statusLabelRecord}
      />
    </div>
  );
}

function QueueRow({
  item,
  actionTypeLabel,
  searchParams,
  pageContract,
  statusLabels,
}: {
  item: VerificationQueueItem;
  actionTypeLabel: string;
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
  statusLabels: Record<string, string>;
}) {
  const href = hrefWith(searchParams, { vi_row: item.item_id, va_status: null, va_code: null });
  // The whole row opens the review overlay (mock: no Details column). Each cell wraps its content in
  // the same LocalOverlayLink, so it stays a client-local overlay (no route navigation) and remains
  // keyboard-reachable per cell instead of relying on a row onClick that a11y cannot follow.
  const cell = (children: React.ReactNode) => (
    <LocalOverlayLink href={href} className="vr-rowlink" scroll={false}>
      {children}
    </LocalOverlayLink>
  );
  return (
    <tr className="vr-row">
      <td>
        {cell(<div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <span className="vr-thumb" aria-hidden="true">
            <PlayCircle className="ic" />
            {item.media.length > 1 ? <span className="n">{item.media.length}</span> : null}
          </span>
          {actionTypeLabel}
        </div>)}
      </td>
      <td>
        {cell(subjectCell(item))}
      </td>
      <td className="muted" style={{ whiteSpace: "nowrap" }}>
        {cell(fmtDateTime(item.captured_at))}
      </td>
      <td className="muted" style={{ whiteSpace: "nowrap" }}>
        {cell(inQueueCell(item))}
      </td>
      <td className="muted" style={{ whiteSpace: "nowrap" }}>
        {cell(item.verified_at ? fmtDateTime(item.verified_at) : <span className="small">—</span>)}
      </td>
      <td className="muted" style={{ whiteSpace: "nowrap" }}>
        {cell(reviewTookCell(item))}
      </td>
      <td>
        {cell(<Tag tone={item.status === "rejected" ? "dng" : item.status === "approved" ? "ok" : "warn"}>{statusLabels[item.status] || item.status}</Tag>)}
      </td>
      <td>
        {cell(<span className="muted small">{item.verdict_reason || "—"}</span>)}
      </td>
      <td className="muted" style={{ whiteSpace: "nowrap" }}>
        {cell(watchCell(item))}
      </td>
    </tr>
  );
}

// subjectCell renders the Subject column as a headline plus typed chips instead of the raw
// "·"-joined subject_label. The label's segments carry different kinds of fact depending on the
// module -- "Godel 1 - Part 2", "31 goats", "Tag 901007000503938", "732.0 kg", "Session 2" -- and
// reading them as one grey sentence forced the reviewer to parse every row. The operational
// location leads (it is what the reviewer is looking at), quantities become chips that scan
// vertically down the column, and the operator trails as context.
const COUNT_SEGMENT = /^\d[\d,]*\s+(goats?|animals?|kids?)$/i;
const WEIGHT_SEGMENT = /^[\d.,]+\s*kg$/i;
const TAG_SEGMENT = /^tag\s+(\S+)$/i;

function subjectCell(item: VerificationQueueItem): React.ReactNode {
  const location = (item.operational_location_display || item.shed_label || "").trim();
  const segments = (item.subject_label || "")
    .split("·")
    .map((part) => part.trim())
    .filter(Boolean);

  const chips: React.ReactNode[] = [];
  const rest: string[] = [];
  for (const segment of segments) {
    const tag = TAG_SEGMENT.exec(segment);
    if (tag) {
      chips.push(
        <span key={`tag-${segment}`} className="chip tag" title={tag[1]}>
          {tag[1]}
        </span>,
      );
      continue;
    }
    if (COUNT_SEGMENT.test(segment)) {
      chips.push(<span key={`count-${segment}`} className="chip count">{segment}</span>);
      continue;
    }
    if (WEIGHT_SEGMENT.test(segment)) {
      chips.push(<span key={`kg-${segment}`} className="chip kg">{segment}</span>);
      continue;
    }
    rest.push(segment);
  }

  // The headline prefers the label's own descriptive segment when it carries MORE than the shed
  // name ("Godel 1 - Part 2" beats "Godel 1"); otherwise the resolved location leads and the
  // remaining segments ("Whole shed", "Session 2") drop to the meta line.
  const descriptive = rest.find((segment) => location && segment.startsWith(location)) || "";
  const headline = descriptive || location || rest[0] || item.subject_label?.trim() || "—";
  const meta = rest.filter((segment) => segment !== headline && segment !== descriptive);
  if (item.operator_name) meta.push(item.operator_name);

  return (
    <div className="vr-subj">
      <span className="t" title={item.subject_label || headline}>{headline}</span>
      {chips.length || meta.length ? (
        <span className="m">
          {chips}
          {meta.length ? <span>{meta.join(" · ")}</span> : null}
        </span>
      ) : null}
    </div>
  );
}

// IN_QUEUE_AMBER_MS / IN_QUEUE_RED_MS are the "In queue" column's age thresholds for a still-
// pending item: amber past 4 hours, red past 7 days. Reviewed/decided items never age, so this
// only applies to status === "pending".
const IN_QUEUE_AMBER_MS = 4 * 60 * 60 * 1000;
const IN_QUEUE_RED_MS = 7 * 24 * 60 * 60 * 1000;

// inQueueCell renders the age since captured_at for a still-pending item, colored amber past 4h
// and red past 7d; decided items show "—" (their age in the pending queue no longer matters --
// "Review took" answers the equivalent question for them).
function inQueueCell(item: VerificationQueueItem): React.ReactNode {
  if (item.status !== "pending") return <span className="small">—</span>;
  const capturedMs = Date.parse(item.captured_at);
  if (Number.isNaN(capturedMs)) return <span className="small">—</span>;
  const ageMs = Date.now() - capturedMs;
  const tone = ageMs >= IN_QUEUE_RED_MS ? "dng" : ageMs >= IN_QUEUE_AMBER_MS ? "warn" : undefined;
  return <span className={tone ? `small ${tone === "dng" ? "vr-age-red" : "vr-age-amber"}` : "small"}>{humanizeDurationMs(ageMs)}</span>;
}

// reviewTookCell renders the elapsed time between capture and verdict for a decided item; absent
// for a still-pending item, which has not been reviewed yet.
function reviewTookCell(item: VerificationQueueItem): React.ReactNode {
  if (!item.verified_at) return <span className="small">—</span>;
  const capturedMs = Date.parse(item.captured_at);
  const verifiedMs = Date.parse(item.verified_at);
  if (Number.isNaN(capturedMs) || Number.isNaN(verifiedMs)) return <span className="small">—</span>;
  return <span className="small">{humanizeDurationMs(verifiedMs - capturedMs)}</span>;
}

// watchCell renders the queue row's watch-telemetry summary: "not opened" when the verifier never
// opened the item, "N%" when a video position/duration pair was reported, "—" when the item was
// opened but no duration telemetry exists, or when telemetry is unavailable for this deployment
// (item.watch absent -- see domain.ItemWatchState's doc comment).
function watchCell(item: VerificationQueueItem): React.ReactNode {
  const watch = item.watch;
  if (!watch) return <span className="small">—</span>;
  if (!watch.opened) return <span className="small">not opened</span>;
  if (watch.percent_watched === undefined || watch.percent_watched === null) return <span className="small">—</span>;
  return <span className="small">{watch.percent_watched}%</span>;
}

/**
 * The selected status tab.
 *
 * `?status=all` is forwarded VERBATIM: the backend needs it explicitly, because an absent status
 * defaults to pending (the landing tab) rather than to "everything". An unknown value falls back to
 * that same landing tab.
 */
const BUSINESS_DAY = /^\d{4}-\d{2}-\d{2}$/;

/**
 * The selected inclusive capture-date range, both ends "YYYY-MM-DD".
 *
 * A single day is expressed as from === to, so the whole screen carries ONE date concept and the
 * query builder decides at the last moment whether that collapses to the backend's `business_date`
 * or opens into the range pair.
 *
 * Defaults, in order:
 *  - `vd_from` + `vd_to`, the picker's own params;
 *  - `as_of`, so links minted while the shell still had a top-bar date picker keep working;
 *  - today.
 *
 * Malformed or out-of-order input falls back rather than throwing: a hand-edited URL must not take
 * the board down, and the backend re-validates the same bounds anyway. A future end is clamped to
 * today for the same reason — the backend answers 400 future_business_date, and a 400 is a worse
 * answer to a stale bookmark than today's board.
 */
function parseDateRange(sp: RouteSearchParams, today: string): { from: string; to: string } {
  const rawFrom = one(sp, DATE_FROM_PARAM)?.trim();
  const rawTo = one(sp, DATE_TO_PARAM)?.trim();
  if (rawFrom && rawTo && BUSINESS_DAY.test(rawFrom) && BUSINESS_DAY.test(rawTo) && rawFrom <= rawTo) {
    return { from: rawFrom > today ? today : rawFrom, to: rawTo > today ? today : rawTo };
  }
  const asOf = one(sp, "as_of")?.trim();
  if (asOf && BUSINESS_DAY.test(asOf) && asOf <= today) return { from: asOf, to: asOf };
  // Nothing named: open on the recent WINDOW, not on today alone -- see DEFAULT_QUEUE_WINDOW_DAYS.
  return { from: businessDaysBefore(today, DEFAULT_QUEUE_WINDOW_DAYS), to: today };
}

/**
 * How far back the board looks when the URL names no date at all.
 *
 * TODAY IS THE WRONG DEFAULT FOR A WORKING QUEUE (maintainer decision 2026-08-17). Proof arrives on
 * the day it is captured and is reviewed later, so a queue pinned to today shows an empty board
 * sitting on top of a full backlog -- observed on real data: 402 pending weighing proofs captured
 * across the previous twelve days, and a board reading "No actions to review". The verifier had no
 * control to change the date either, which is what made it a dead end rather than a wrong default.
 *
 * The backend already says this in ports.ListQueueParams.IsVerifierQueueRead: a verifier queue read
 * "does NOT clamp to today -- it returns the full pending backlog ordered oldest-first". This page
 * was overriding that with a today-to-today range of its own.
 *
 * A WINDOW rather than "no filter": the range still bounds the query (the read is keyset-paged and
 * date-bounded, and an unbounded scan is exactly what the scale rules forbid), it is simply wide
 * enough to hold work that is actually outstanding. Two weeks matches the per-verifier activity
 * window the oversight analytics already report on.
 */
const DEFAULT_QUEUE_WINDOW_DAYS = 14;

/** businessDaysBefore subtracts whole days from a YYYY-MM-DD business date, in date space only --
 *  no clock, no zone arithmetic, so it cannot drift across the IST business-day boundary. */
function businessDaysBefore(day: string, days: number): string {
  const parsed = new Date(`${day}T00:00:00Z`);
  if (Number.isNaN(parsed.getTime())) return day;
  parsed.setUTCDate(parsed.getUTCDate() - days);
  return parsed.toISOString().slice(0, 10);
}

function verificationStatus(value: string | undefined): VerificationItemStatus | "all" {
  if (value === "all" || value === "approved" || value === "rejected") return value;
  return "pending";
}

function hrefWith(params: RouteSearchParams, updates: Record<string, string | null | undefined>): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (Array.isArray(value)) {
      for (const item of value) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  for (const [key, value] of Object.entries(updates)) {
    if (value === null || value === undefined || value === "") next.delete(key);
    else next.set(key, value);
  }
  const qs = next.toString();
  return qs ? `${PATHNAME}?${qs}` : PATHNAME;
}

function hiddenInputs(params: RouteSearchParams, exclude: string[]) {
  return Object.entries(params).flatMap(([key, value]) => {
    if (exclude.includes(key)) return [];
    if (Array.isArray(value)) {
      return value.filter(Boolean).map((item) => <input key={`${key}:${item}`} type="hidden" name={key} value={item} />);
    }
    return value ? [<input key={key} type="hidden" name={key} value={value} />] : [];
  });
}

/**
 * The cursor trail behind the current page, oldest first.
 *
 * Keyset pagination can only read FORWARD from a cursor, so "previous" is served by remembering
 * the cursors already consumed rather than by an OFFSET jump. Entries are URL-encoded and joined
 * with "~", a character the base64url cursors the backend issues never contain, so a cursor can
 * never be split in half by the delimiter.
 *
 * The empty string is a legitimate entry: it is the first page, which is read with no cursor at
 * all. Dropping it would make Previous skip page one.
 *
 * Bounded at MAX_TRAIL: a verifier walking a long backlog must not grow an unbounded URL. Past
 * that the oldest entries are dropped, so Previous still walks back through the recent pages and
 * simply cannot reach the very first one -- the status pills reset the queue for that.
 */
const MAX_TRAIL = 40;

/**
 * The first page is read with NO cursor, so its trail entry is the empty string. That entry cannot
 * be written to the URL as-is: hrefWith deletes any param whose value is empty, so a one-entry
 * trail ["\u0022\u0022"] encoded to "" and was dropped -- page two then rendered with no trail, no
 * Previous link, and no position at all. FIRST_PAGE is the on-the-wire stand-in for that entry.
 * "-" is safe: cursors are base64url and never equal it.
 */
const FIRST_PAGE = "-";

function decodeTrail(raw: string | undefined): string[] {
  if (!raw) return [];
  return raw.split("~").map((entry) => {
    if (entry === FIRST_PAGE) return "";
    try {
      return decodeURIComponent(entry);
    } catch {
      // A hand-edited or truncated URL must not throw the whole page; treat it as the first page.
      return "";
    }
  });
}

function encodeTrail(trail: string[]): string | null {
  const bounded = trail.slice(-MAX_TRAIL);
  if (!bounded.length) return null;
  return bounded.map((entry) => (entry ? encodeURIComponent(entry) : FIRST_PAGE)).join("~");
}
