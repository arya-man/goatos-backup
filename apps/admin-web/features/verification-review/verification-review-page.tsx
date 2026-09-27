// A GET form through next/form: Apply is a soft navigation (the page stays on screen), not a document reload.
import { UrlSuspense } from "@/components/app/url-suspense";
import { StatStripSkeleton, TableSkeleton } from "@/components/app/skeletons";
import Form from "next/form";
import { Label } from "@/components/minimal/label";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { PageHeader } from "@/components/app/page-header";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";

import { controlEnabled, copy, table, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError, getAdminWebBootstrap, listVerificationQueue, type VerificationItemStatus, type VerificationQueueItem } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { fmtDateTime, humanizeDurationMs, todayIso } from "@/lib/format";
import { all, one, type RouteSearchParams } from "@/lib/search-params";
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
import { ModuleFilter } from "./module-filter";
import { SubcategoryFilter } from "./subcategory-filter";
import { VideoLogPanel } from "./video-log-panel";
import { VrFormSelect } from "./vr-form-select";
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
import { EmptyContent } from "@/components/minimal/empty-content";
import Box from "@mui/material/Box";
import { LinkButton } from "@/components/minimal/link-button";
import Card from "@mui/material/Card";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import { VerificationQueueTelemetry } from "./verification-queue-telemetry";
import { ToxinReviewScreen, toxinTabLabel } from "./toxin-review-section";
import Alert from "@mui/material/Alert";
import Avatar from "@mui/material/Avatar";
import Badge from "@mui/material/Badge";
import Button from "@mui/material/Button";
import Divider from "@mui/material/Divider";
import { TemplateTabs } from "@/components/app/template-tabs";
import { Iconify, type IconifyName } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TablePaginationLinks } from "@/components/minimal/table";
import { fPercent } from "@/components/minimal/_shared/format-number";
import { InvoiceAnalytic } from "@/components/minimal/sections/invoice/invoice-analytic";
import { VrQueueHead } from "./vr-queue-head";
import { QUEUE_LIMIT } from "./verification-layout";

const PATHNAME = "/verify";

// Changing a filter invalidates the open row, the keyset cursor, its back-trail, and any verdict
// feedback banner: all four describe the queue as it was BEFORE the change. Carrying a cursor
// across a filter change is the worst of them — cursors are keyset positions in one filtered
// sequence, so reusing one lands on an unrelated slice of the new queue.
const RESET_ON_FILTER = { vi_row: null, vi_cursor: null, vi_trail: null, vi_open_first: null, vi_play: null, va_status: null, va_code: null, va_fields: null, va_entries: null };


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
  const selectedCategories = all(sp, "category").map((value) => value.trim()).filter(Boolean);
  const category = selectedCategories.length === 1 ? selectedCategories[0] : undefined;
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
  const sort = verificationSort(one(sp, "sort"));
  const nextSort = sort === "captured_at_asc" ? "captured_at_desc" : "captured_at_asc";

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
    categories: selectedCategories.length > 1 ? selectedCategories : undefined,
    navModule,
    // One day collapses to the backend's single `business_date`; a span uses the range pair. Both
    // are the same inclusive Asia/Kolkata capture-date scope, and sending both at once is a 400
    // (invalid_date_scope), so this is an either/or, never a merge.
    ...(dateRange.from === dateRange.to
      ? { businessDate: dateRange.from }
      : { businessDateFrom: dateRange.from, businessDateTo: dateRange.to }),
    parkId: scope.parkId,
    shedId,
    limit: QUEUE_LIMIT,
    cursor: one(sp, "vi_cursor"),
    sort,
  });
  const authError = firstAuthRequiredError(queue);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const items = queue.ok ? listOrEmpty(queue.data.items) : [];
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
  const selectedModuleLabel = modules.find((option) => option.key === selectedModuleKey)?.label ?? selectedModuleKey;
  const selectedModuleActionTypes = selectedModuleKey
    ? actionTypes
        .filter((option) => option.module_key === selectedModuleKey)
        .map((option) => ({ ...option, label: childActionTypeLabel(option.label, selectedModuleLabel) }))
        .filter((option) => option.label)
    : [];
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
  // va_fields / va_entries ride only the 500 g confirm bounce (maintainer decision 2026-09-09): the
  // flagged entry keys with their direction codes, and the readings she typed, so the remounted
  // drawer can show the warning under the right box with her numbers still in it.
  const feedback = { status: one(sp, "va_status"), code: one(sp, "va_code"), fields: one(sp, "va_fields"), entries: one(sp, "va_entries") };
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
  const scopeParkLabel = videoLogEnabled && scope.parkId ? await bootstrapParkLabel(scope.parkId) : undefined;
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
  const statusTotal = statusCounts.pending + statusCounts.approved + statusCounts.rejected;
  // The statusless option is the backend's "All" tab (status=all is forwarded verbatim).
  const allStatusOption = statuses.find((option) => !option.status);

  return (
    <div className="screen on">
        <PageHeader
          title={pageContract.title}
          crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]}
          actions={
            // One row of same-size template Buttons (FJ1-P1-6); wraps on a phone.
            <Stack direction="row" spacing={1.5} useFlexGap sx={{ flexWrap: "wrap" }}>
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
                  // The page's park scope is the default park filter inside the panel (filters carry
                  // into drawers and downloads); the CSV already follows parkFilter || parkId.
                  parkFilter={one(sp, VIDEO_LOG_PARK_KEY) || scope.parkId || undefined}
                  scopeParkLabel={scopeParkLabel}
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
            </Stack>
          }
        />

      {queue.ok ? null : (
        <Alert severity="error">
          <b>{copy(pageContract, "state.queue_unavailable")}</b>
          <Typography variant="body2" sx={{ mt: 0.5 }}>
            {copy(pageContract, "state.queue_unavailable_body")}
          </Typography>
          <Typography variant="body2" sx={{ mt: 0.5, color: "text.secondary" }}>
            {queue.error.code ?? queue.error.kind} · {queue.error.message}
          </Typography>
        </Alert>
      )}

      {/* Template InvoiceListView summary card: one InvoiceAnalytic per status, dashed dividers,
          scrolling sideways inside its own Scrollbar on a phone. The counts are the backend's
          whole-filter aggregate (filter_options.counts), never a count of the page on screen. */}
      {/* Status counts + queue rows swap to their skeleton on a tab / module / date / shed / sort /
          page change (guard: url-keyed-panel); the tabs and the filter toolbar stay on screen. The
          drawer / panel params are not watched: opening one never blanks the queue. */}
      <UrlSuspense searchParams={sp} watch={QUEUE_WATCH} fallback={<Box sx={{ mb: { xs: 3, md: 5 } }}><StatStripSkeleton count={4} meta /></Box>}>
      {queue.ok && statusOptionsWithStatus.length ? (
        <Card>
          <Scrollbar sx={{ minHeight: 108 }}>
            <Stack divider={<Divider orientation="vertical" flexItem sx={{ borderStyle: "dashed" }} />} sx={{ py: 2, flexDirection: "row" }}>
              {allStatusOption ? (
                <InvoiceAnalytic
                  title={allStatusOption.label}
                  total={statusTotal}
                  percent={100}
                  icon="solar:bill-list-bold-duotone"
                  color="info.main"
                  caption={fPercent(100)}
                />
              ) : null}
              {statusOptionsWithStatus.map((option) => (
                <InvoiceAnalytic
                  key={option.key}
                  title={option.label}
                  total={statusCounts[option.status] ?? 0}
                  percent={statusTotal ? ((statusCounts[option.status] ?? 0) / statusTotal) * 100 : 0}
                  icon={STATUS_ICON[option.status] ?? "solar:file-bold-duotone"}
                  color={`${STATUS_COLOR[option.status] ?? "info"}.main`}
                  caption={fPercent(statusTotal ? ((statusCounts[option.status] ?? 0) / statusTotal) * 100 : 0)}
                />
              ))}
            </Stack>
          </Scrollbar>
        </Card>
      ) : null}
      </UrlSuspense>

      <VerificationQueueTelemetry
        category={category}
        parkId={scope.parkId}
        shedId={shedId}
        status={status}
        enabled={controlEnabled(pageContract, "record_verdict", false)}
      >
        {/* Template InvoiceListView list card: status Tabs with Label counts, the toolbar row, the
            table bleeding to the card edge, the pagination footer. */}
        <Card className="vr-board" aria-label={copy(pageContract, "board.title")} sx={{ minWidth: 0 }}>
          {statuses.length ? (
            // Status tabs predate the oversight rollout and render for every role, verifier included.
            <TemplateTabs
              ariaLabel={copy(pageContract, "board.title")}
              value={status}
              sx={{ px: { md: 2.5 } }}
              items={statuses.map((option) => ({
                value: option.status ?? "all",
                label: option.label,
                count: option.status ? (statusCounts[option.status] ?? 0) : statusTotal,
                href: hrefWith(sp, { status: option.status ?? "all", vi_row: null, vi_cursor: null, vi_trail: null, va_status: null, va_code: null, va_fields: null, va_entries: null }),
              }))}
            />
          ) : null}

          {/* Template list toolbar (InvoiceTableToolbar anatomy): p 2.5, one row from md, a column
              on a phone. Module and capture date navigate on change; subcategories and shed ride the
              form's Apply. */}
          <Form action={PATHNAME} prefetch={false} className="vr-filter-form">
          <Box
            sx={{
              p: 2.5,
              gap: 2,
              display: "flex",
              pr: { xs: 2.5, md: 1 },
              flexWrap: { md: "wrap" },
              flexDirection: { xs: "column", md: "row" },
              alignItems: { xs: "stretch", md: "center" },
            }}
          >
            {/* vd_from / vd_to are NOT excluded: Apply must preserve the selected capture date.
                category is rendered by the subcategory select so multi-select stays real. */}
            {hiddenInputs(sp, ["category", "shed_id", "vi_row", "vi_cursor", "vi_trail", "va_status", "va_code", "va_fields", "va_entries"])}

            {/* Module picker — the same grouping the phone's verifier drawer uses. Vocabulary, labels
                and order are the registry's (filter_options.modules); selection is read from
                filter_options.module_key so a nav leaf's ?category= selects its module too. Each
                choice clears `category` (a wider selection than one page; a sibling module's category
                would be a 400 module_category_conflict). Offered only when the backend offers it:
                the verifier lens withdraws `module_filter`, and it defaults on for an older backend. */}
            {moduleFilterOffered && oversightFiltersEnabled && modules.length > 1 ? (
              <ModuleFilter
                allLabel={copy(pageContract, "filter.all_modules")}
                allHref={hrefWith(sp, { nav_module: null, category: null, ...RESET_ON_FILTER })}
                ariaLabel={copy(pageContract, "filter.module")}
                selectedModuleKey={selectedModuleKey ?? ""}
                modules={modules.map((option) => ({
                  key: option.key,
                  label: option.label,
                  href: hrefWith(sp, { nav_module: option.key, category: null, ...RESET_ON_FILTER }),
                }))}
                toxinOption={toxinTabEnabled ? { label: toxinTabLabel(pageContract), href: hrefWith(sp, { toxin: "1", nav_module: null, category: null, ...RESET_ON_FILTER }) } : undefined}
              />
            ) : null}

            {moduleFilterOffered && oversightFiltersEnabled && selectedModuleActionTypes.length > 1 ? (
              <SubcategoryFilter
                ariaLabel={`${copy(pageContract, "filter.module")} ${selectedModuleLabel}`}
                label={`${selectedModuleLabel} subcategories`}
                options={selectedModuleActionTypes}
                selectedCategories={selectedCategories}
              />
            ) : null}

            <>
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
                  {/* Grouped by park: a shed NAME is not unique across the farm (Castro, Gandhi,
                      Godel 1/2, Mandela 1/2 and Yashoda exist in BOTH parks). The park comes from the
                      option's own park_label and is never folded into the shed's display; options
                      with no park stay in a plain list ABOVE the groups. */}
                  <VrFormSelect
                    id="verification-shed"
                    name="shed_id"
                    label={copy(pageContract, "filter.shed")}
                    defaultValue={shedId ?? ""}
                    options={[
                      { value: "", label: copy(pageContract, "filter.all_sheds") },
                      ...shedsWithoutPark.map((option) => ({
                        value: option.id,
                        label: option.operational_location_display || option.label,
                      })),
                      ...shedsByPark.flatMap(([parkLabel, parkSheds]) =>
                        parkSheds.map((option) => ({
                          value: option.id,
                          label: option.operational_location_display || option.label,
                          group: parkLabel,
                        })),
                      ),
                    ]}
                  />
                  <Button type="submit" variant="contained" color="primary" size="large" startIcon={<Iconify icon="solar:list-bold" />} sx={{ flexShrink: 0 }}>
                    {copy(pageContract, "filter.apply")}
                  </Button>
                  {/* Deliberately does NOT clear `category` (the sidebar's selection). It DOES clear
                      the date pair, which returns the board to its default recent window. */}
                  <LinkButton
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
                    color="error"
                    size="large"
                    startIcon={<Iconify icon="solar:trash-bin-trash-bold" />}
                    sx={{ flexShrink: 0 }}
                  >
                    {copy(pageContract, "filter.clear_all")}
                  </LinkButton>
                </>
              ) : selectedModuleActionTypes.length > 1 ? (
                <Button type="submit" variant="contained" color="primary" size="large" startIcon={<Iconify icon="solar:list-bold" />} sx={{ flexShrink: 0 }}>
                  {copy(pageContract, "filter.apply")}
                </Button>
              ) : null}
            </>
          </Box>
          </Form>

          <Box className="vr-results-zone" aria-live="polite" aria-busy="false" sx={{ position: "relative" }}>
            <div className="vr-results-loading" aria-hidden="true">
              <span />
              <span />
              <span />
              <span />
            </div>

            <UrlSuspense searchParams={sp} watch={QUEUE_WATCH} fallback={<TableSkeleton bare header={false} columns={columns.length || 6} rows={10} />}>
            <Scrollbar>
              <Table className="vr-table" aria-label={tableContract.title} sx={{ minWidth: 960 }}>
                <VrQueueHead
                  orderBy="captured_at"
                  order={sort === "captured_at_desc" ? "desc" : "asc"}
                  sortHref={hrefWith(sp, { sort: nextSort, ...RESET_ON_FILTER })}
                  headCells={columns.map((label, index) => ({
                    id: index === 2 ? "captured_at" : `col-${index}`,
                    label,
                    sortable: index === 2,
                    sortLabel: index === 2 ? `Sort by ${label} ${nextSort === "captured_at_desc" ? "newest first" : "oldest first"}` : undefined,
                  }))}
                />
                <TableBody>
                  {items.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={columns.length}>
                        {/* Template TableNoData: EmptyContent straight in the table body. */}
                        <EmptyContent
                          filled
                          role="status"
                          title={queue.ok ? copy(pageContract, "state.empty") : copy(pageContract, "state.queue_unavailable")}
                          sx={{ py: 10 }}
                        />
                      </TableCell>
                    </TableRow>
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
                </TableBody>
              </Table>
            </Scrollbar>

            {/* Keyset pagination. The queue read is cursor-based (OFFSET is banned on this path), so
                there is no page number to jump to. `vi_trail` carries the cursors already consumed,
                newest last: Next pushes the cursor that produced the CURRENT page, Previous pops it
                and re-reads with the one beneath -- each direction is a real indexed keyset read.
                Every label stays backend-owned (pagination.previous / position / next). */}
            {queue.ok && (queue.data.next_cursor || trail.length) ? (
              <TablePaginationLinks
                className="pager"
                page={trail.length}
                rowsPerPage={20}
                count={-1}
                replace
                rangeLabel={`${copy(pageContract, "pagination.position")} ${trail.length + 1}`}
                prevLabel={copy(pageContract, "pagination.previous")}
                nextLabel={copy(pageContract, "pagination.next")}
                prevHref={
                  trail.length
                    ? hrefWith(sp, {
                        vi_cursor: trail[trail.length - 1] || null,
                        vi_trail: encodeTrail(trail.slice(0, -1)),
                        vi_row: null,
                        va_status: null,
                        va_code: null,
                        va_fields: null,
                        va_entries: null,
                      })
                    : null
                }
                nextHref={
                  queue.data.next_cursor
                    ? hrefWith(sp, {
                        vi_cursor: queue.data.next_cursor,
                        // The cursor that produced THIS page becomes the way back to it. "" is a real
                        // trail entry (the first page has no cursor) and must survive the round trip.
                        vi_trail: encodeTrail([...trail, one(sp, "vi_cursor") ?? ""]),
                        vi_row: null,
                        va_status: null,
                        va_code: null,
                        va_fields: null,
                        va_entries: null,
                      })
                    : null
                }
              />
            ) : null}
            </UrlSuspense>
          </Box>
        </Card>
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

const STATUS_COLOR: Record<string, "warning" | "success" | "error"> = { pending: "warning", approved: "success", rejected: "error" };
const STATUS_ICON: Record<string, IconifyName> = {
  pending: "solar:sort-by-time-bold-duotone",
  approved: "solar:file-check-bold-duotone",
  rejected: "solar:file-corrupted-bold-duotone",
};

// The whole row opens the review overlay AND resolves the first proof immediately: this is the
// verifier's explicit tap on that evidence row, not an automatic list-preview load. Keeping this
// intent on every cell avoids the two-click "open drawer, then open video" trap.
const ROW_LINK_STYLE = { display: "block", color: "inherit", textDecoration: "none" } as const;

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
  void pageContract;
  const playHref = hrefWith(searchParams, { vi_row: item.item_id, vi_play: "1", va_status: null, va_code: null, va_fields: null, va_entries: null });
  const leadMedia = item.media.find((media) => media.thumbnail_url) ?? item.media[0];
  const cell = (children: React.ReactNode) => (
    <LocalOverlayLink href={playHref} scroll={false} style={ROW_LINK_STYLE}>
      {children}
    </LocalOverlayLink>
  );
  const muted = { color: "text.secondary", whiteSpace: "nowrap" } as const;
  return (
    // Template InvoiceTableRow: hover row, Avatar + ListItemText lead cell, soft status Label.
    <TableRow hover>
      <TableCell>
        {cell(
          <Box sx={{ gap: 2, display: "flex", alignItems: "center" }}>
            <Badge badgeContent={item.media.length > 1 ? item.media.length : 0} color="default" overlap="rectangular">
              <Avatar variant="rounded" src={leadMedia?.thumbnail_url || undefined} alt="" slotProps={{ img: { loading: "lazy", decoding: "async" } }} sx={{ bgcolor: "background.neutral", color: "text.secondary" }}>
                <Iconify icon="solar:play-circle-bold" />
              </Avatar>
            </Badge>
            <Box component="span" sx={{ typography: "body2" }}>
              {actionTypeLabel}
            </Box>
          </Box>,
        )}
      </TableCell>
      <TableCell>{cell(subjectCell(item))}</TableCell>
      <TableCell sx={muted}>{cell(fmtDateTime(item.captured_at))}</TableCell>
      <TableCell sx={muted}>{cell(inQueueCell(item))}</TableCell>
      <TableCell sx={muted}>{cell(item.verified_at ? fmtDateTime(item.verified_at) : "—")}</TableCell>
      <TableCell sx={muted}>{cell(reviewTookCell(item))}</TableCell>
      <TableCell>
        {cell(
          <Label variant="soft" color={STATUS_COLOR[item.status] ?? "default"}>
            {statusLabels[item.status] || item.status}
          </Label>,
        )}
      </TableCell>
      <TableCell sx={{ color: "text.secondary", typography: "body2" }}>{cell(item.verdict_reason || "—")}</TableCell>
      <TableCell sx={muted}>{cell(watchCell(item))}</TableCell>
    </TableRow>
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
        <Label key={`tag-${segment}`} variant="soft" title={tag[1]}>
          {tag[1]}
        </Label>,
      );
      continue;
    }
    if (COUNT_SEGMENT.test(segment)) {
      chips.push(<Label key={`count-${segment}`} variant="soft" color="info">{segment}</Label>);
      continue;
    }
    if (WEIGHT_SEGMENT.test(segment)) {
      chips.push(<Label key={`kg-${segment}`} variant="soft" color="secondary">{segment}</Label>);
      continue;
    }
    rest.push(segment);
  }

  // The headline prefers the label's own descriptive segment when it carries MORE than the shed
  // name ("Godel 1 - Part 2" beats "Godel 1"); otherwise the resolved location leads and the
  // remaining segments ("Whole pen", "Session 2") drop to the meta line.
  const descriptive = rest.find((segment) => location && segment.startsWith(location)) || "";
  const headline = descriptive || location || rest[0] || item.subject_label?.trim() || "—";
  const meta = rest.filter((segment) => segment !== headline && segment !== descriptive);
  if (item.operator_name) meta.push(item.operator_name);

  // Template ListItemText anatomy: body2 primary, the typed chips + context as the muted secondary line.
  return (
    <Box sx={{ minWidth: 0 }}>
      <Box component="span" title={item.subject_label || headline} sx={{ display: "block", typography: "body2", overflowWrap: "anywhere" }}>
        {headline}
      </Box>
      {chips.length || meta.length ? (
        <Box component="span" sx={{ mt: 0.5, display: "flex", flexWrap: "wrap", alignItems: "center", gap: 0.5, typography: "caption", color: "text.disabled" }}>
          {chips}
          {meta.length ? <span>{meta.join(" · ")}</span> : null}
        </Box>
      ) : null}
    </Box>
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
  if (item.status !== "pending") return "—";
  const capturedMs = Date.parse(item.captured_at);
  if (Number.isNaN(capturedMs)) return "—";
  const ageMs = Date.now() - capturedMs;
  const tone = ageMs >= IN_QUEUE_RED_MS ? "error" : ageMs >= IN_QUEUE_AMBER_MS ? "warning" : undefined;
  return tone ? (
    <Label variant="soft" color={tone}>
      {humanizeDurationMs(ageMs)}
    </Label>
  ) : (
    humanizeDurationMs(ageMs)
  );
}

// reviewTookCell renders the elapsed time between capture and verdict for a decided item; absent
// for a still-pending item, which has not been reviewed yet.
function reviewTookCell(item: VerificationQueueItem): React.ReactNode {
  if (!item.verified_at) return "—";
  const capturedMs = Date.parse(item.captured_at);
  const verifiedMs = Date.parse(item.verified_at);
  if (Number.isNaN(capturedMs) || Number.isNaN(verifiedMs)) return "—";
  return humanizeDurationMs(verifiedMs - capturedMs);
}

// watchCell renders the queue row's watch-telemetry summary: "not opened" when the verifier never
// opened the item, "N%" when a video position/duration pair was reported, "—" when the item was
// opened but no duration telemetry exists, or when telemetry is unavailable for this deployment
// (item.watch absent -- see domain.ItemWatchState's doc comment).
function watchCell(item: VerificationQueueItem): React.ReactNode {
  const watch = item.watch;
  if (!watch) return "—";
  if (!watch.opened) return "not opened";
  if (watch.percent_watched === undefined || watch.percent_watched === null) return "—";
  return `${watch.percent_watched}%`;
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

function verificationSort(value: string | undefined): "captured_at_asc" | "captured_at_desc" {
  if (value === "captured_at_desc") return "captured_at_desc";
  return "captured_at_asc";
}

function childActionTypeLabel(label: string, moduleLabel: string): string {
  const cleanLabel = label.trim();
  const cleanModuleLabel = moduleLabel.trim();
  if (!cleanLabel || !cleanModuleLabel) return cleanLabel;
  if (cleanLabel.toLocaleLowerCase() === cleanModuleLabel.toLocaleLowerCase()) return "";
  const prefix = `${cleanModuleLabel} `;
  if (cleanLabel.toLocaleLowerCase().startsWith(prefix.toLocaleLowerCase())) {
    return cleanLabel.slice(prefix.length).trim();
  }
  return cleanLabel;
}

async function bootstrapParkLabel(parkId: string): Promise<string | undefined> {
  const bootstrap = await getAdminWebBootstrap();
  if (!bootstrap.ok) return undefined;
  const option = bootstrap.data.top_bar.park_selector.options.find((item) => item.key === parkId);
  return option ? option.title || option.label : undefined;
}

function hrefWith(params: RouteSearchParams, updates: Record<string, string | string[] | null | undefined>): string {
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
    else if (Array.isArray(value)) {
      next.delete(key);
      for (const item of value) if (item) next.append(key, item);
    }
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

/** The params the queue read takes (never the drawer / Analytics / Video log / Randomization panels' own). */
const QUEUE_WATCH = ["status", "category", "nav_module", "shed_id", "park", "scope_mode", DATE_FROM_PARAM, DATE_TO_PARAM, "vi_cursor", "vi_trail", "sort", "toxin"] as const;
