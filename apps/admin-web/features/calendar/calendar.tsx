import { FilterChip } from "@/components/minimal/list/filter-chip";
import { listOrEmpty } from "@/lib/list-or-empty";
import { copy, actionFeedbackCopy, optionLabel, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { one, hrefWithoutAction, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import { todayIso } from "@/lib/format";
import { PageHeader } from "@/components/app/page-header";
import { AnimatedTabs } from "@/components/minimal/list/animated-tabs";
import { LinkButton } from "@/components/minimal/link-button";
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
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Stack from "@mui/material/Stack";
import ButtonGroup from "@mui/material/ButtonGroup";

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
// backfill), owner filter + workstream tabs above the calendar exactly like the template's user list
// carries `LabelTabs` above the table. Drive rows open /calendar/drive/[eventId]; other rows open
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

  const workstreamTabs = presentation.workstream_tabs.length ? (
    <AnimatedTabs
      variant="underline"
      scrollButtons="auto"
      sx={{ px: { md: 2.5 } }}
      ariaLabel={copy(pageContract, "filter.workstream.aria")}
      value={presentation.workstream_tabs.find((tab) => tab.active)?.key ?? ""}
      items={presentation.workstream_tabs.map((tab) => ({
        value: tab.key,
        label: tab.label,
        href: tab.enabled && !tab.active ? hrefForWorkstreamTab(tab) : undefined,
        disabled: !tab.enabled,
      }))}
    />
  ) : null;

  // Template calendar view (sections/calendar/view/calendar-view): heading row with the page
  // action on the right, the filter result chips, then ONE Card holding CalendarRoot → toolbar →
  // FullCalendar. The upcoming/history switch is the heading action; owner scope rides as the
  // template's filter chips above the card; the workstream strip is the card's first row.
  return (
    <Stack spacing={3}>
      <PageHeader
        className="calendar-page-head"
        title={presentation.page_title || pageContract.title}
        crumbs={[{ label: pageContract.title }]}
        actions={
          <ButtonGroup variant="outlined" color="inherit" aria-label={copy(pageContract, "filter.view.aria", weekTabLabel)}>
            <LinkButton href={weekHref} replace scroll={false} variant={historyMode ? "outlined" : "contained"} color={historyMode ? "inherit" : "primary"} aria-current={historyMode ? undefined : "page"}>
              {weekTabLabel}
            </LinkButton>
            <LinkButton href={historyHref} replace scroll={false} variant={historyMode ? "contained" : "outlined"} color={historyMode ? "primary" : "inherit"} aria-current={historyMode ? "page" : undefined}>
              {historyTabLabel}
            </LinkButton>
          </ButtonGroup>
        }
      />

      {/* Owner scope as the template's filter chips (CalendarFiltersResult row). */}
      <Box role="group" aria-label={copy(pageContract, "filter.owner.aria")} sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
        {presentation.owner_tabs.map((tab) => {
          const on = tab.active || (!presentation.owner_tabs.some((t) => t.active) && tab.key === activeOwnerKey);
          return tab.enabled ? (
            <FilterChip key={tab.key} href={hrefForOwnerTab(tab)} on={on} replace label={tab.label} />
          ) : (
            <FilterChip key={tab.key} on={on} disabled label={tab.label} />
          );
        })}
      </Box>

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
        header={workstreamTabs}
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
