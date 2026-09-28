import { LinkFiltersResult, type LinkFilterChip } from "@/components/app/link-filters-result";
import { listOrEmpty } from "@/lib/list-or-empty";
import { copy, actionFeedbackCopy, optionLabel, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { one, hrefWithoutAction, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import { todayIso } from "@/lib/format";
import { PageHeader } from "@/components/app/page-header";
import {
  fallbackCalendarPresentation,
  ownerMetaFromPresentation,
  presentationQueryToSearch,
  type CalendarEvent,
  type CalendarOwnerFilter,
  type CalendarOwnerPresentationTab,
  type CalendarPresentationTab,
} from "./calendar-contract";
import { getCalendarVaccinationEvents, getCalendarVaccinationEventDetail, getCalendarDriveTargets } from "./calendar-server";
import { CalendarEventDrawer, type CalendarDrawerLoadResult } from "./calendar-event-drawer";
import { historyWindow, monthWindow } from "./calendar-window";
import { toFullCalendarEvents } from "./calendar-fullcalendar-events";
import { CalendarFullView, type CalendarViewOption } from "./calendar-full-view";
import type { CalendarFiltersModel } from "./calendar-filters";
import Alert from "@mui/material/Alert";
import Stack from "@mui/material/Stack";

const PATH = "/calendar";

// Aggregated vaccination-drive placeholders without a `drive_summary` are backend rollups the
// FullCalendar grid cannot usefully render (no progress, no click target); filter them BEFORE the
// adapter so they never become empty event pills. Same rule the old week/history grids applied.
function isRenderableCalendarEvent(event: CalendarEvent): boolean {
  return !(event.aggregated && event.event_type === "vaccination_drive" && !event.drive_summary);
}

async function loadCalendarDrawer(
  eventId: string,
  includeTargets: boolean,
  targetsCursor?: string,
): Promise<CalendarDrawerLoadResult> {
  "use server";
  const normalizedId = eventId.trim();
  if (!normalizedId || normalizedId.length > 256) {
    return {
      eventId: normalizedId,
      detail: null,
      detailError: "calendar_event_id_invalid",
      targets: null,
      targetsError: null,
      targetsNextCursor: null,
    };
  }
  const cursor = targetsCursor && targetsCursor.length <= 512 ? targetsCursor : undefined;
  const [detail, targets] = await Promise.all([
    getCalendarVaccinationEventDetail(normalizedId),
    includeTargets ? getCalendarDriveTargets(normalizedId, { cursor, limit: 10 }) : Promise.resolve(null),
  ]);
  return {
    eventId: normalizedId,
    detail: detail.ok ? detail.data : null,
    detailError: detail.ok ? null : detail.error.message,
    targets: targets?.ok ? targets.data.items : null,
    targetsError: targets && !targets.ok ? targets.error.message : null,
    targetsNextCursor: targets?.ok ? targets.data.next_cursor ?? null : null,
  };
}

// VaccinationCalendarPage — /calendar on the template's Minimal calendar app (FullCalendar 6.1.20
// on CalendarRoot). Read-only, one page for both "week" and "history" work (history = the completed
// backfill); window, owner and workstream are the template filters drawer behind the toolbar icon
// (applied ones show as CalendarFiltersResult chips). Drive rows open /calendar/drive/[eventId]; other rows open
// the shared CalendarEventDrawer via the #calendar_event=<id> fragment.
export async function VaccinationCalendarPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const { parkId, asOf } = backendScope(scope);
  const requestedStatus = one(sp, "status") === "completed" ? "completed" : undefined;
  const historyMode = requestedStatus === "completed";
  const requestedOwnerKey = (one(sp, "owner_key") || "all") as CalendarOwnerFilter;
  const selectedEventId = one(sp, "event");
  const targetsCursor = one(sp, "targets_cursor");
  const today = todayIso();
  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");

  const anchorKey = asOf ? asOf.slice(0, 10) : today;
  // FullCalendar month view already covers the visible calendar grid; monthWindow() gives the full
  // month around the anchor. History mode uses the 45-day historyWindow. Both fits inside the API's
  // 45-day inclusive window cap.
  const window = historyMode ? historyWindow(anchorKey) : monthWindow(anchorKey);
  const [list, detail, targets] = await Promise.all([
    getCalendarVaccinationEvents({
      parkId,
      ownerKey: requestedOwnerKey,
      status: requestedStatus,
      dateFrom: window.dateFrom,
      dateTo: window.dateTo,
      limit: 200,
    }),
    selectedEventId ? getCalendarVaccinationEventDetail(selectedEventId) : Promise.resolve(null),
    selectedEventId && !historyMode ? getCalendarDriveTargets(selectedEventId, { cursor: targetsCursor, limit: 10 }) : Promise.resolve(null),
  ]);

  const events = list.ok ? listOrEmpty(list.data.items) : [];
  if (list.ok && !list.data.presentation) {
    throw new Error(copy(pageContract, "error.presentation_missing"));
  }
  const presentation = list.ok && list.data.presentation ? list.data.presentation : fallbackCalendarPresentation(pageContract, requestedOwnerKey);
  const ownerMeta = ownerMetaFromPresentation(presentation);
  const activeOwnerKey = presentation.active_owner_key as CalendarOwnerFilter;

  const weekTabLabel = presentation.view_tabs.find((tab) => tab.key === "week")?.label || optionLabel(pageContract, "calendar_view_tabs", "week");
  const historyTabLabel = presentation.view_tabs.find((tab) => tab.key === "history")?.label || optionLabel(pageContract, "calendar_view_tabs", "history");

  // FullCalendar toolbar view options: labels come from the page contract's calendar_view_tabs so
  // the toolbar carries no local literals. Icons map 1:1 to the template's four calendar views.
  const viewOptions: CalendarViewOption[] = [
    { value: "dayGridMonth", label: optionLabel(pageContract, "calendar_view_tabs", "month"), icon: "mingcute:calendar-month-line" },
    { value: "timeGridWeek", label: optionLabel(pageContract, "calendar_view_tabs", "week"), icon: "mingcute:calendar-week-line" },
    { value: "timeGridDay", label: copy(pageContract, "calendar.view.day", "Day"), icon: "mingcute:calendar-day-line" },
    { value: "listWeek", label: copy(pageContract, "calendar.view.agenda", "Agenda"), icon: "custom:calendar-agenda-outline" },
  ];

  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref(PATH, scope, {}, {
      owner_key: activeOwnerKey === "all" ? undefined : activeOwnerKey,
      status: requestedStatus,
      ...overrides,
    });
  }

  const hrefForOwnerTab = (tab: CalendarOwnerPresentationTab) =>
    hrefWith({
      ...presentationQueryToSearch(tab.query),
      event: undefined,
      targets_cursor: undefined,
      targets_page: undefined,
      targets_cursor_stack: undefined,
    });
  const hrefForWorkstreamTab = (tab: CalendarPresentationTab) =>
    hrefWith({
      ...presentationQueryToSearch(tab.query),
      event: undefined,
      targets_cursor: undefined,
      targets_page: undefined,
      targets_cursor_stack: undefined,
    });
  const closeHref = hrefWith({ event: undefined, targets_cursor: undefined, targets_page: undefined, targets_cursor_stack: undefined });
  const weekHref = hrefWith({
    status: undefined,
    event: undefined,
    targets_cursor: undefined,
    targets_page: undefined,
    targets_cursor_stack: undefined,
  });
  const historyHref = hrefWith({
    status: "completed",
    event: undefined,
    targets_cursor: undefined,
    targets_page: undefined,
    targets_cursor_stack: undefined,
  });

  const sel = detail && detail.ok ? detail.data : null;
  const initialDrawerData: CalendarDrawerLoadResult | null = sel
    ? {
        eventId: sel.event.event_id,
        detail: sel,
        detailError: null,
        targets: targets?.ok ? targets.data.items : null,
        targetsError: targets && !targets.ok ? targets.error.message : null,
        targetsNextCursor: targets?.ok ? targets.data.next_cursor ?? null : null,
      }
    : null;

  const renderableEvents = events.filter(isRenderableCalendarEvent);
  const fcEvents = toFullCalendarEvents(renderableEvents, ownerMeta);

  // Template calendar view (sections/calendar/view/calendar-view): heading row, the filter result
  // chips only when a filter is applied, then ONE Card holding CalendarRoot -> toolbar -> FullCalendar.
  // The upcoming / history window, the owner lane and the workstream are the template filters
  // drawer behind the toolbar's filter icon (TR1-#25): no segmented toggle in the heading, no chip
  // row above the card and no tab row inside it. The template's "Add event" has no Mesha action
  // (drives are planned on /vaccination/plan), so the heading carries none.
  const ownerTabOn = (tab: CalendarOwnerPresentationTab) =>
    tab.active || (!presentation.owner_tabs.some((t) => t.active) && tab.key === activeOwnerKey);
  const defaultOwnerTab = presentation.owner_tabs.find((tab) => tab.key === "all") ?? presentation.owner_tabs[0];
  const activeOwnerTab = presentation.owner_tabs.find(ownerTabOn);
  const defaultWorkstreamTab = presentation.workstream_tabs[0];
  const enabledOwnerTabs = presentation.owner_tabs.filter((tab) => tab.enabled);
  const enabledWorkstreamTabs = presentation.workstream_tabs.filter((tab) => tab.enabled);
  const activeWorkstreamTab = presentation.workstream_tabs.find((tab) => tab.active);
  const ownerFiltered = Boolean(activeOwnerTab && defaultOwnerTab && activeOwnerTab.key !== defaultOwnerTab.key);
  const workstreamFiltered = Boolean(activeWorkstreamTab && defaultWorkstreamTab && activeWorkstreamTab.key !== defaultWorkstreamTab.key);
  const resetHref = scopeHref(PATH, scope);
  const filterChips: LinkFilterChip[] = [
    ...(historyMode ? [{ id: "window", label: `${copy(pageContract, "filter.view.label", "View")}:`, value: historyTabLabel, href: weekHref }] : []),
    ...(ownerFiltered && activeOwnerTab && defaultOwnerTab
      ? [{ id: "owner", label: `${copy(pageContract, "filter.owner.aria")}:`, value: activeOwnerTab.label, href: hrefForOwnerTab(defaultOwnerTab) }]
      : []),
    ...(workstreamFiltered && activeWorkstreamTab && defaultWorkstreamTab
      ? [{ id: "workstream", label: `${copy(pageContract, "filter.workstream.aria")}:`, value: activeWorkstreamTab.label, href: hrefForWorkstreamTab(defaultWorkstreamTab) }]
      : []),
  ];
  const filters: CalendarFiltersModel = {
    title: copy(pageContract, "action.filters", "Filters"),
    closeLabel: copy(pageContract, "action.close", "Close"),
    openLabel: copy(pageContract, "action.filters", "Filters"),
    canReset: filterChips.length > 0,
    resetHref,
    groups: [
      {
        id: "window",
        label: copy(pageContract, "filter.view.label", "View"),
        options: [
          { key: "week", label: weekTabLabel, href: weekHref, active: !historyMode },
          { key: "history", label: historyTabLabel, href: historyHref, active: historyMode },
        ],
      },
      // Only the tabs the contract ENABLES are offered (a disabled one is a dead control; guard:
      // no-disabled-contract-tabs, widened from /people to every feature). A group with fewer than
      // two live options is not a choice, so it is dropped.
      ...(enabledOwnerTabs.length > 1
        ? [{
            id: "owner",
            label: copy(pageContract, "filter.owner.aria"),
            options: enabledOwnerTabs.map((tab) => ({
              key: tab.key,
              label: tab.label,
              href: hrefForOwnerTab(tab),
              active: ownerTabOn(tab),
            })),
          }]
        : []),
      ...(enabledWorkstreamTabs.length > 1
        ? [{
            id: "workstream",
            label: copy(pageContract, "filter.workstream.aria"),
            options: enabledWorkstreamTabs.map((tab) => ({
              key: tab.key,
              label: tab.label,
              href: hrefForWorkstreamTab(tab),
              active: tab.active,
            })),
          }]
        : []),
    ],
  };

  return (
    <Stack spacing={3}>
      <PageHeader
        className="calendar-page-head"
        title={presentation.page_title || pageContract.title}
        crumbs={[{ label: pageContract.title }]}
      />

      {/* Template CalendarFiltersResult: only when a filter is applied; a chip removes its filter. */}
      <LinkFiltersResult totalResults={renderableEvents.length} chips={filterChips} resetHref={resetHref} placement="page" />

      {actionStatus ? (
        actionStatus === "success" ? (
          <Alert severity="success">
            <b>{copy(pageContract, "action.success_tag")}</b>&nbsp;{actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </Alert>
        ) : (
          <Alert severity="error">
            <b>{copy(pageContract, "action.failed_title")}</b>&nbsp;{actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </Alert>
        )
      ) : null}

      {!list.ok ? (
        <Alert severity="error">
          <b>{list.error.code ?? list.error.kind}</b>&nbsp;{list.error.message}
        </Alert>
      ) : null}
      {selectedEventId && detail && !detail.ok ? (
        <Alert severity="error">
          <b>{detail.error.code ?? detail.error.kind}</b>&nbsp;{detail.error.message}
        </Alert>
      ) : null}

      <CalendarFullView
        filters={filters}
        events={fcEvents}
        initialAsOf={anchorKey}
        scope={scope}
        ownerKey={activeOwnerKey}
        status={requestedStatus}
        viewOptions={viewOptions}
        noEventsText={copy(pageContract, "calendar.week.empty", presentation.week.empty_message || "No drives in this window")}
        toolbarCopy={{
          today: copy(pageContract, "calendar.toolbar.today", "Today"),
          previous: copy(pageContract, "action.previous"),
          next: copy(pageContract, "action.next"),
          viewGroupAria: copy(pageContract, "calendar.toolbar.view_group_aria", "Calendar view"),
        }}
      />

      <CalendarEventDrawer
        initialData={initialDrawerData}
        initialSelectedEventId={selectedEventId}
        loadDrawer={loadCalendarDrawer}
        includeTargets={!historyMode}
        closeHref={closeHref}
        returnTo={hrefWithoutAction(PATH, sp)}
        scope={scope}
        presentation={presentation}
        ownerMeta={ownerMeta}
        pageContract={pageContract}
      />
    </Stack>
  );
}
