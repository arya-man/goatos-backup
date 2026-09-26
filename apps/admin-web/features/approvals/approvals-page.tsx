import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";

import { PageHeader } from "@/components/app/page-header";
import { CourseWidgetSummary } from "@/components/minimal/widgets/course-widget-summary";
import { COURSE_WIDGET_ICONS } from "@/lib/minimal-icons";
import { TablePaginationLinks } from "@/components/minimal/table";
import { AnimatedTabs } from "@/components/minimal/list/animated-tabs";

import { LinkSelect } from "@/components/app/link-select";
import {
  firstAuthRequiredError,
  getAdminWebApproval,
  listAdminWebApprovals,
  type AdminWebApprovalItem,
  type AdminWebApprovalStatus,
} from "@/lib/api/server";
import { getCensusLocations } from "@/lib/api/herd-locations";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { todayIso } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { STATUS_TABS, TYPE_TABS, APPROVALS_COPY as COPY } from "./copy";
import { ApprovalsDrawer } from "./approvals-drawer";
import { ApprovalsDateFilter } from "./approvals-date-filter";
import { ApprovalsQueueTable, approvalsHref } from "./approvals-queue-table";
import { approvalDateRange, approvalSubject, approvalSuccessSentence } from "./approval-display";
import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import Box from "@mui/material/Box";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";

const PATHNAME = "/approvals";
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export async function ApprovalsPage({ searchParams }: { searchParams?: RouteSearchParams }) {
  const sp = searchParams ?? {};
  const status = STATUS_TABS.find((s) => s === one(sp, "status")) ?? "pending";
  const typeFilter = TYPE_TABS.find((t) => t === one(sp, "type")) ?? "all";
  // A farm id reaches the server only when it is a uuid; anything else is treated as "all farms"
  // rather than turned into a 400 by a hand-edited URL.
  const rawFarm = one(sp, "farm")?.trim() ?? "";
  const farmFilter = UUID_RE.test(rawFarm) ? rawFarm : "";
  const cursor = one(sp, "ap_cursor");
  // The calendar filter reaches the server only as a well-formed, ordered pair of YYYY-MM-DD days;
  // a hand-edited URL degrades to "any date" rather than a 400 page.
  const dateRange = approvalDateRange(one(sp, "raised_from"), one(sp, "raised_to"));

  const [queue, locations] = await Promise.all([
    listAdminWebApprovals({
      status,
      page_size: 20,
      cursor,
      request_type: typeFilter === "all" ? undefined : typeFilter,
      park_id: farmFilter || undefined,
      raised_from: dateRange.from || undefined,
      raised_to: dateRange.to || undefined,
    }),
    // Park/shed NAMES so the list + drawer render human-readable farm/shed text instead of UUIDs.
    // These are backend-owned canonical location names, resolved id -> name in the renderer.
    getCensusLocations(),
  ]);
  const authError = firstAuthRequiredError(queue);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const locationNames: Record<string, string> = {};
  for (const loc of [...locations.parks, ...locations.sheds]) locationNames[loc.id] = loc.name;
  // "Farm" in the product = the top-level park (Coimbatore / Channapatna); sheds sit under it and
  // shed moves stay within one park. These are the Farm filter options.
  const farms = locations.parks.map((p) => ({ id: p.id, name: p.name }));

  // Type and farm are applied by the server (and bound into its cursor), so every page is already
  // narrowed; the page never filters a fetched page client-side, which used to hide older rows.
  const items = queue.ok ? listOrEmpty(queue.data.items) : [];
  const nextCursor = queue.ok ? queue.data.next_cursor ?? "" : "";
  const subjects = Object.fromEntries(items.map((item) => [item.approval_request_id, approvalSubject(item, locationNames)]));

  const selectedId = one(sp, "ap_row");
  // A link to a request that is not on this page (older than the first 20, or under another tab
  // or filter -- a Work Board row, a bookmark) still opens its drawer: that one request is read on
  // its own, under the same authority as the list (maintainer 2026-09-25). It opens the drawer only;
  // the table keeps showing the page the reader is on.
  // After a decision the redirect still names the decided row; its confirmation is the page-level
  // banner, so that row is NOT re-read into a reopened drawer.
  const justDecided = one(sp, "ap_status") === "success";
  const onPage = !selectedId || justDecided || items.some((item) => item.approval_request_id === selectedId);
  const linked = !onPage && UUID_RE.test(selectedId ?? "") ? await getAdminWebApproval(selectedId as string) : null;
  const drawerItems = linked && linked.ok ? [...items, linked.data] : items;
  const feedback = { status: one(sp, "ap_status"), code: one(sp, "ap_code") };
  // A decided row leaves the Pending list on the redirect, so the drawer (which renders only a row
  // still in the list) can never carry the confirmation. It lives at page level instead.
  const successSentence = approvalSuccessSentence(feedback.status, feedback.code);

  const clearRow = { ap_row: null, ap_status: null, ap_code: null } as const;
  const kpis = [
    { key: "pending", title: COPY.kpi.pendingInView, total: countStatus(items, "pending"), icon: COURSE_WIDGET_ICONS.progress, color: "warning" as const },
    { key: "birth-death", title: COPY.kpi.birthDeathInView, total: items.filter((i) => i.request_type === "birth" || i.request_type === "death").length, icon: COURSE_WIDGET_ICONS.completed, color: "info" as const },
    { key: "shifting", title: COPY.kpi.shiftingInView, total: items.filter((i) => i.request_type === "shifting").length, icon: COURSE_WIDGET_ICONS.certificates, color: "secondary" as const },
    { key: "rows", title: COPY.kpi.rowsInView, total: items.length, icon: COURSE_WIDGET_ICONS.completed, color: "primary" as const },
  ];

  return (
    <Box className="screen on">
      <PageHeader title={COPY.title} crumbs={[{ label: COPY.title }]} />

      <Stack spacing={3}>
        {successSentence ? (
          <Alert severity="success" role="status">
            <b>{successSentence}</b>
          </Alert>
        ) : null}

        {queue.ok ? null : (
          <Alert severity="error">
            <AlertTitle>{COPY.error.queueUnavailable}</AlertTitle>
            {COPY.error.queueUnavailableBody}
          </Alert>
        )}

        {/* A deck of zeros is a wall, not a reading: the tiles render only once the view has rows.
            Template overview/course: CourseWidgetSummary count tiles on a spacing-3 Grid. */}
        {items.length > 0 ? (
          <Grid container spacing={3}>
            {kpis.map((kpi) => (
              <Grid key={kpi.key} size={{ xs: 12, sm: 6, md: 3 }}>
                <CourseWidgetSummary title={kpi.title} total={kpi.total} icon={kpi.icon} color={kpi.color} />
              </Grid>
            ))}
          </Grid>
        ) : null}

        {/* Template order list: the request-type Tabs are the card's first row, the status, farm and
            date filters the toolbar row under them. The type tabs carry no counts: the server applies
            the type filter, so the fetched page cannot count other types. */}
        <ApprovalsQueueTable
          items={items}
          ok={queue.ok}
          searchParams={sp}
          subjects={subjects}
          tabs={
            <AnimatedTabs
              ariaLabel="Request type"
              value={typeFilter}
              sx={{ px: { md: 2.5 } }}
              items={TYPE_TABS.map((key) => ({
                value: key,
                label: COPY.typeTab[key],
                href: hrefWith(sp, { type: key === "all" ? null : key, ap_cursor: null, ...clearRow }),
              }))}
            />
          }
          toolbar={
            <Box
              sx={{
                p: 2.5,
                gap: 2,
                display: "flex",
                pr: { xs: 2.5, md: 1 },
                flexDirection: { xs: "column", md: "row" },
                alignItems: { xs: "stretch", md: "center" },
                "& > *": { width: { xs: 1, md: "auto" } },
              }}
            >
              <LinkSelect
                label={COPY.filter.status}
                value={status}
                minWidth={160}
                options={STATUS_TABS.map((key) => ({
                  value: key,
                  label: COPY.statusTab[key],
                  href: hrefWith(sp, { status: key, ap_cursor: null, ...clearRow }),
                }))}
              />
              {/* Farm filter: the top-level park each request belongs to (Coimbatore / Channapatna). */}
              <LinkSelect
                label={COPY.filter.farm}
                value={farmFilter === "" ? "__all" : farmFilter}
                minWidth={200}
                options={[
                  { value: "__all", label: COPY.farmTab.all, href: hrefWith(sp, { farm: null, ap_cursor: null, ...clearRow }) },
                  ...farms.map((farm) => ({
                    value: farm.id,
                    label: farm.name,
                    href: hrefWith(sp, { farm: farm.id, ap_cursor: null, ...clearRow }),
                  })),
                ]}
              />
              <ApprovalsDateFilter
                labels={{
                  field: COPY.dateFilter.field,
                  today: COPY.dateFilter.today,
                  single: COPY.dateFilter.single,
                  range: COPY.dateFilter.range,
                  aria: COPY.dateFilter.aria,
                  previousMonth: COPY.dateFilter.previousMonth,
                  nextMonth: COPY.dateFilter.nextMonth,
                  rangeStartHint: COPY.dateFilter.rangeStartHint,
                  rangeEndHint: COPY.dateFilter.rangeEndHint,
                  rangeSeparator: COPY.dateFilter.rangeSeparator,
                }}
                from={dateRange.from}
                to={dateRange.to}
                today={todayIso()}
                anyLabel={COPY.dateFilter.any}
                clearLabel={COPY.dateFilter.clear}
                basePath={PATHNAME}
              />
            </Box>
          }
          footer={
            cursor || nextCursor ? (
              // Template TablePaginationCustom footer; the queue pages by cursor, so there is no total
              // (count -1) and "first" returns to the newest page.
              <TablePaginationLinks
                page={cursor ? 1 : 0}
                rowsPerPage={20}
                count={-1}
                rangeLabel={`${COPY.kpi.rowsInView}: ${items.length}`}
                prevHref={cursor ? hrefWith(sp, { ap_cursor: null, ...clearRow }) : null}
                nextHref={nextCursor ? hrefWith(sp, { ap_cursor: nextCursor, ...clearRow }) : null}
                prevLabel={COPY.pager.first}
                nextLabel={COPY.pager.next}
                replace
              />
            ) : null
          }
        />
      </Stack>

      <ApprovalsDrawer items={drawerItems} initialSelectedId={selectedId} searchParams={sp} feedback={feedback} locationNames={locationNames} />
    </Box>
  );
}

function countStatus(items: AdminWebApprovalItem[], status: AdminWebApprovalStatus): number {
  return items.filter((item) => item.status === status).length;
}

function hrefWith(params: RouteSearchParams, updates: Record<string, string | null | undefined>): string {
  return approvalsHref(params, updates);
}
