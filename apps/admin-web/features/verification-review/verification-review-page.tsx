import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { Filter, PlayCircle } from "lucide-react";

import { Tag } from "@/components/ui-primitives";
import { copy, table, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError, listStaffPositions, listVerificationQueue, type VerificationItemStatus, type VerificationQueueItem } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { fmtDateTime } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { parseScope } from "@/lib/scope";
import { VerificationReviewDrawer } from "./verification-review-drawer";
import { VerificationQueueTelemetry } from "./verification-queue-telemetry";

const PATHNAME = "/actions";


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
  const shedId = one(sp, "shed_id")?.trim();
  const scope = parseScope(sp);
  const selectedId = one(sp, "vi_row");

  const [queue, positions] = await Promise.all([
    listVerificationQueue({
      status,
      category,
      businessDate: scope.asOf,
      parkId: scope.parkId,
      shedId,
      limit: 20,
      cursor: one(sp, "vi_cursor"),
    }),
    listStaffPositions({ scope_type: "center", status: "active", limit: 500 }),
  ]);
  const authError = firstAuthRequiredError(queue);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const items = queue.ok ? queue.data.items : [];
  const actionTypes = queue.ok ? queue.data.filter_options.action_types : [];
  const statuses = queue.ok ? queue.data.filter_options.statuses : [];
  const sheds = queue.ok ? queue.data.filter_options.sheds : [];
  const typeLabels = new Map(actionTypes.map((option) => [option.category, `${option.module_label} · ${option.label}`]));
  // Row/drawer labels map an ITEM's status to its display label, so the statusless "All" tab is
  // excluded — it is a filter tab, never a status a row can be in.
  const statusOptionsWithStatus = statuses.filter(
    (option): option is typeof option & { status: VerificationItemStatus } => Boolean(option.status),
  );
  const statusLabelRecord = Object.fromEntries(statusOptionsWithStatus.map((option) => [option.status, option.label]));
  const feedback = { status: one(sp, "va_status"), code: one(sp, "va_code") };
  const columns = tableLabels(pageContract, "verification-actions");
  const tableContract = table(pageContract, "verification-actions");

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

      <VerificationQueueTelemetry category={category} parkId={scope.parkId} shedId={shedId} status={status}>
        <section className="card vr-board" style={{ minWidth: 0 }}>
        <div className="bt">{copy(pageContract, "board.title")}</div>

        <div className="vr-frow">
          <form action={PATHNAME} style={{ display: "contents" }}>
            {hiddenInputs(sp, ["category", "shed_id", "vi_row", "vi_cursor", "va_status", "va_code"])}
            <div className="vr-fld fld" style={{ marginBottom: 0 }}>
              <label htmlFor="verification-action-type">{copy(pageContract, "filter.action_type")}</label>
              <select id="verification-action-type" name="category" className="vr-selbtn" defaultValue={category ?? ""}>
                <option value="">{copy(pageContract, "filter.all_action_types")}</option>
                {actionTypes.map((option) => (
                  <option key={option.key} value={option.category}>
                    {option.module_label} · {option.label}
                  </option>
                ))}
              </select>
            </div>
            {sheds.length ? (
              <div className="vr-fld fld" style={{ marginBottom: 0 }}>
                <label htmlFor="verification-shed">{copy(pageContract, "filter.shed")}</label>
                <select id="verification-shed" name="shed_id" className="vr-selbtn" defaultValue={shedId ?? ""}>
                  <option value="">{copy(pageContract, "filter.all_sheds")}</option>
                  {sheds.map((option) => (
                    <option key={option.id} value={option.id}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
            ) : null}
            <button type="submit" className="btn sm">
              <Filter className="ic" aria-hidden="true" />
              {copy(pageContract, "filter.apply")}
            </button>
          </form>
          <Link
            href={hrefWith(sp, { category: null, shed_id: null, status: null, vi_row: null, vi_cursor: null, va_status: null, va_code: null })}
            replace
            scroll={false}
            className="lk small"
          >
            {copy(pageContract, "filter.clear_all")}
          </Link>
        </div>

        {statuses.length ? (
          <div className="vr-legend">
            {statusOptionsWithStatus.map((option) => (
              <Link
                key={option.key}
                href={hrefWith(sp, { status: option.status, vi_row: null, vi_cursor: null, va_status: null, va_code: null })}
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
                    actionTypeLabel={typeLabels.get(item.category) ?? item.category}
                    searchParams={sp}
                    pageContract={pageContract}
                    statusLabels={statusLabelRecord}
                  />
                ))
              )}
            </tbody>
          </table>
        </div>

        {queue.ok && queue.data.next_cursor ? (
          <div className="pager" style={{ marginTop: 12 }}>
            <Link href={hrefWith(sp, { vi_cursor: queue.data.next_cursor, vi_row: null, va_status: null, va_code: null })} className="btn sm" replace scroll={false}>
              {copy(pageContract, "pagination.next")}
            </Link>
          </div>
        ) : null}
        </section>
      </VerificationQueueTelemetry>

      <VerificationReviewDrawer
        items={items}
        initialSelectedId={selectedId}
        positions={positions.ok ? positions.data : null}
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
        {cell(<>{item.vertical} / {item.module}</>)}
      </td>
      <td>
        {cell(item.subject_label?.trim()
          ? item.subject_label
          : `${item.operator_name || "—"} · ${item.shed_label || "—"}`)}
      </td>
      <td className="muted" style={{ whiteSpace: "nowrap" }}>
        {cell(fmtDateTime(item.captured_at))}
      </td>
      <td>
        {cell(<Tag tone={item.status === "rejected" ? "dng" : item.status === "approved" ? "ok" : "warn"}>{statusLabels[item.status] || item.status}</Tag>)}
      </td>
      <td>
        {cell(<span className="muted small">{item.verdict_reason || "—"}</span>)}
      </td>
    </tr>
  );
}

/**
 * The selected status tab.
 *
 * `?status=all` is forwarded VERBATIM: the backend needs it explicitly, because an absent status
 * defaults to pending (the landing tab) rather than to "everything". An unknown value falls back to
 * that same landing tab.
 */
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
