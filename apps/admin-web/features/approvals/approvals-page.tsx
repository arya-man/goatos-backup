import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { Gavel } from "lucide-react";

import { Tag, type Tone } from "@/components/ui-primitives";
import {
  firstAuthRequiredError,
  listAdminWebApprovals,
  type AdminWebApprovalItem,
  type AdminWebApprovalRequestType,
  type AdminWebApprovalStatus,
} from "@/lib/api/server";
import { getCensusLocations } from "@/lib/api/herd-locations";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { fmtDateTime, todayIso } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { APPROVALS_COPY as COPY } from "./copy";
import { ApprovalsDrawer } from "./approvals-drawer";
import { ApprovalsDateFilter } from "./approvals-date-filter";
import { approvalDateRange, approvalStatusLabel, approvalSubject, approvalSuccessSentence } from "./approval-display";

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
  const items = queue.ok ? queue.data.items : [];
  const nextCursor = queue.ok ? queue.data.next_cursor ?? "" : "";

  const selectedId = one(sp, "ap_row");
  const feedback = { status: one(sp, "ap_status"), code: one(sp, "ap_code") };
  // A decided row leaves the Pending list on the redirect, so the drawer (which renders only a row
  // still in the list) can never carry the confirmation. It lives at page level instead.
  const successSentence = approvalSuccessSentence(feedback.status, feedback.code);

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            <b>{COPY.title}</b>
          </div>
          <h1>{COPY.title}</h1>
          <div className="sub">{COPY.subtitle}</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
      </div>

      {successSentence ? (
        <div className="alert ok" role="status" style={{ marginBottom: 14 }}>
          <b>{successSentence}</b>
        </div>
      ) : null}

      {queue.ok ? null : (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{COPY.error.queueUnavailable}</b>
          <div className="small" style={{ marginTop: 4 }}>
            {COPY.error.queueUnavailableBody}
          </div>
        </div>
      )}

      <div className="grid g4" style={{ marginBottom: 16 }}>
        <KPI label={COPY.kpi.pendingInView} value={String(countStatus(items, "pending"))} tone="warn" />
        <KPI
          label={COPY.kpi.birthDeathInView}
          value={String(items.filter((i) => i.request_type === "birth" || i.request_type === "death").length)}
          tone="info"
        />
        <KPI label={COPY.kpi.shiftingInView} value={String(items.filter((i) => i.request_type === "shifting").length)} tone="info" />
        <KPI label={COPY.kpi.rowsInView} value={String(items.length)} tone="ok" />
      </div>

      <div className="subtabs" style={{ marginBottom: 12 }}>
        {STATUS_TABS.map((key) => (
          <Link
            key={key}
            href={hrefWith(sp, { status: key, ap_row: null, ap_cursor: null, ap_status: null, ap_code: null })}
            replace
            scroll={false}
            className={status === key ? "on" : ""}
          >
            {COPY.statusTab[key]}
          </Link>
        ))}
      </div>

      <div className="subtabs" style={{ marginBottom: 12 }}>
        {TYPE_TABS.map((key) => (
          <Link
            key={key}
            href={hrefWith(sp, { type: key === "all" ? null : key, ap_row: null, ap_cursor: null, ap_status: null, ap_code: null })}
            replace
            scroll={false}
            className={typeFilter === key ? "on" : ""}
          >
            {COPY.typeTab[key]}
          </Link>
        ))}
      </div>

      {/* Farm filter — the top-level park each request belongs to (Coimbatore / Channapatna). */}
      <div className="subtabs" style={{ marginBottom: 14 }}>
        <Link
          href={hrefWith(sp, { farm: null, ap_row: null, ap_cursor: null, ap_status: null, ap_code: null })}
          replace
          scroll={false}
          className={farmFilter === "" ? "on" : ""}
        >
          {COPY.farmTab.all}
        </Link>
        {farms.map((farm) => (
          <Link
            key={farm.id}
            href={hrefWith(sp, { farm: farm.id, ap_row: null, ap_cursor: null, ap_status: null, ap_code: null })}
            replace
            scroll={false}
            className={farmFilter === farm.id ? "on" : ""}
          >
            {farm.name}
          </Link>
        ))}
      </div>

      <div style={{ marginBottom: 14 }}>
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

      <section className="card" style={{ minWidth: 0 }}>
        <div className="hd">
          <Gavel className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <h3>{COPY.title}</h3>
        </div>
        <div className="bd twrap" style={{ padding: 0 }} tabIndex={0} role="group">
          <table data-enh="1">
            <thead>
              <tr>
                {COPY.table.columns.map((label) => (
                  <th key={label}>{label}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {items.length === 0 ? (
                <tr>
                  <td colSpan={COPY.table.columns.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {queue.ok ? COPY.error.empty : COPY.error.queueUnavailable}
                    </div>
                  </td>
                </tr>
              ) : (
                items.map((item) => (
                  <ApprovalRow key={item.approval_request_id} item={item} searchParams={sp} locationNames={locationNames} />
                ))
              )}
            </tbody>
          </table>
        </div>
        {cursor || nextCursor ? (
          <div className="pager" style={{ padding: "10px 12px", justifyContent: "flex-end", flexWrap: "wrap" }}>
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
        ) : null}
      </section>

      <ApprovalsDrawer items={items} initialSelectedId={selectedId} searchParams={sp} feedback={feedback} locationNames={locationNames} />
    </div>
  );
}

function ApprovalRow({
  item,
  searchParams,
  locationNames,
}: {
  item: AdminWebApprovalItem;
  searchParams: RouteSearchParams;
  locationNames: Record<string, string>;
}) {
  const href = hrefWith(searchParams, { ap_row: item.approval_request_id, ap_status: null, ap_code: null });
  const subject = approvalSubject(item, locationNames);
  return (
    <tr>
      <td>
        <Tag tone={item.request_type === "shifting" ? "info" : "warn"}>{titleCaseType(item.request_type)}</Tag>
      </td>
      <td>{subject}</td>
      <td className="muted">{item.raised_by_name ?? ""}</td>
      <td className="muted" style={{ whiteSpace: "nowrap" }}>
        {fmtDateTime(item.raised_at)}
      </td>
      <td>
        <Tag tone={statusTone(item.status)}>{approvalStatusLabel(item.status)}</Tag>
      </td>
      <td>
        <LocalOverlayLink href={href} className="btn sm" scroll={false}>
          Review
        </LocalOverlayLink>
      </td>
    </tr>
  );
}

function KPI({ label, value, tone }: { label: string; value: string; tone: Tone }) {
  return (
    <div className="kpi">
      <span className="acc" style={{ background: accentForTone(tone) }} aria-hidden="true" />
      <div className="lab">{label}</div>
      <div className="val">{value}</div>
    </div>
  );
}

function titleCaseType(v: string): string {
  return v ? v.charAt(0).toUpperCase() + v.slice(1) : v;
}

function statusTone(status: AdminWebApprovalStatus): Tone {
  if (status === "rejected") return "dng";
  if (status === "approved") return "ok";
  if (status === "cancelled") return "info";
  return "warn";
}

function accentForTone(toneValue: Tone) {
  switch (toneValue) {
    case "ok":
      return "#6fd043";
    case "warn":
      return "#f7c948";
    case "dng":
      return "#ff6b6b";
    case "info":
      return "#5da8ff";
    default:
      return "#7a8b78";
  }
}

function countStatus(items: AdminWebApprovalItem[], status: AdminWebApprovalStatus): number {
  return items.filter((item) => item.status === status).length;
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
