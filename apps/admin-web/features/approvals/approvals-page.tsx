import Link from "@/components/no-prefetch-link";
import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";
import { ArrowLeftRight, Clock3, HeartPulse, ListChecks } from "lucide-react";

import { PageHeader } from "@/components/app/page-header";
import { KpiCard, KpiGrid } from "@/components/minimal/widgets";
import { AnimatedTabs } from "@/components/minimal/list/animated-tabs";
import { reviewQueueStyles as rq } from "@/components/review-queue/review-queue-ui";

import { LinkSelect } from "@/components/app/link-select";
import {
  firstAuthRequiredError,
  getAdminWebApproval,
  listAdminWebApprovals,
  type AdminWebApprovalItem,
  type AdminWebApprovalRequestType,
  type AdminWebApprovalStatus,
} from "@/lib/api/server";
import { getCensusLocations } from "@/lib/api/herd-locations";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { todayIso } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { APPROVALS_COPY as COPY } from "./copy";
import { ApprovalsDrawer } from "./approvals-drawer";
import { ApprovalsDateFilter } from "./approvals-date-filter";
import { ApprovalsQueueTable, approvalsHref } from "./approvals-queue-table";
import { approvalDateRange, approvalSubject, approvalSuccessSentence } from "./approval-display";
import ap from "./approvals.module.css";
import Alert from "@mui/material/Alert";

const PATHNAME = "/approvals";
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const STATUS_TABS: AdminWebApprovalStatus[] = ["pending", "approved", "rejected"];
const TYPE_TABS: Array<"all" | AdminWebApprovalRequestType> = ["all", "birth", "death", "shifting"];

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

  return (
    <div className={`screen on ${rq.root}`}>
      <PageHeader title={COPY.title} crumbs={[{ label: COPY.title }]} />

      {successSentence ? (
        <Alert severity="success" role="status" style={{ marginBottom: "var(--sp-1h)" }}>
          <b>{successSentence}</b>
        </Alert>
      ) : null}

      {queue.ok ? null : (
        <Alert severity="error" style={{ marginBottom: 14 }}>
          <b>{COPY.error.queueUnavailable}</b>
          <div className="small" style={{ marginTop: 4 }}>
            {COPY.error.queueUnavailableBody}
          </div>
        </Alert>
      )}

      {/* A deck of zeros is a wall, not a reading: the tiles render only once the view has rows. */}
      {items.length > 0 ? (
      <div className={rq.kpis}>
        <KpiGrid min={200}>
          <KpiCard label={COPY.kpi.pendingInView} value={countStatus(items, "pending")} tone="warning" icon={<Clock3 />} />
          <KpiCard
            label={COPY.kpi.birthDeathInView}
            value={items.filter((i) => i.request_type === "birth" || i.request_type === "death").length}
            tone="info"
            icon={<HeartPulse />}
          />
          <KpiCard label={COPY.kpi.shiftingInView} value={items.filter((i) => i.request_type === "shifting").length} tone="violet" icon={<ArrowLeftRight />} />
          <KpiCard label={COPY.kpi.rowsInView} value={items.length} tone="success" icon={<ListChecks />} />
        </KpiGrid>
      </div>
      ) : null}

      {/* One toolbar row, MUI list style: the request-type tabs, then the status, farm and date
          filters on the same line (they wrap under the tabs on a phone). The type tabs carry no
          counts: the server applies the type filter, so the fetched page cannot count other types. */}
      <div className={`${rq.tabs} ${ap.toolbar}`}>
        <AnimatedTabs
          className={`kit-count-tabs ${ap.typeTabs}`}
          ariaLabel="Request type"
          value={typeFilter}
          items={TYPE_TABS.map((key) => ({
            value: key,
            label: COPY.typeTab[key],
            href: hrefWith(sp, { type: key === "all" ? null : key, ap_row: null, ap_cursor: null, ap_status: null, ap_code: null }),
          }))}
        />
        <div className={ap.filters}>
          <LinkSelect
            label={COPY.filter.status}
            value={status}
            minWidth={140}
            options={STATUS_TABS.map((key) => ({
              value: key,
              label: COPY.statusTab[key],
              href: hrefWith(sp, { status: key, ap_row: null, ap_cursor: null, ap_status: null, ap_code: null }),
            }))}
          />
          {/* Farm filter: the top-level park each request belongs to (Coimbatore / Channapatna). */}
          <LinkSelect
            label={COPY.filter.farm}
            value={farmFilter === "" ? "__all" : farmFilter}
            minWidth={160}
            options={[
              { value: "__all", label: COPY.farmTab.all, href: hrefWith(sp, { farm: null, ap_row: null, ap_cursor: null, ap_status: null, ap_code: null }) },
              ...farms.map((farm) => ({
                value: farm.id,
                label: farm.name,
                href: hrefWith(sp, { farm: farm.id, ap_row: null, ap_cursor: null, ap_status: null, ap_code: null }),
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
        </div>
      </div>

      <ApprovalsQueueTable
        items={items}
        ok={queue.ok}
        searchParams={sp}
        subjects={subjects}
        footer={
          cursor || nextCursor ? (
            <div className="pager" style={{ padding: "var(--sp-1) var(--sp-1h)", justifyContent: "flex-end", flexWrap: "wrap" }}>
              {cursor ? (
                <Link href={hrefWith(sp, { ap_cursor: null, ap_row: null, ap_status: null, ap_code: null })} replace scroll={false} className="btn sm">
                  {COPY.pager.first}
                </Link>
              ) : null}
              {nextCursor ? (
                <Link href={hrefWith(sp, { ap_cursor: nextCursor, ap_row: null, ap_status: null, ap_code: null })} replace scroll={false} className="btn sm">
                  {COPY.pager.next}
                </Link>
              ) : null}
            </div>
          ) : null
        }
      />

      <ApprovalsDrawer items={drawerItems} initialSelectedId={selectedId} searchParams={sp} feedback={feedback} locationNames={locationNames} />
    </div>
  );
}

function countStatus(items: AdminWebApprovalItem[], status: AdminWebApprovalStatus): number {
  return items.filter((item) => item.status === status).length;
}

function hrefWith(params: RouteSearchParams, updates: Record<string, string | null | undefined>): string {
  return approvalsHref(params, updates);
}
