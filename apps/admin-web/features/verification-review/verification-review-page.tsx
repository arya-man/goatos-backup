import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { Filter, ShieldCheck } from "lucide-react";

import { Tag } from "@/components/ui-primitives";
import { copy, table, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError, listStaffPositions, listVerificationQueue, type VerificationItemStatus, type VerificationQueueItem } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { fmtDateTime, shortId } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { parseScope } from "@/lib/scope";
import { VerificationReviewDrawer } from "./verification-review-drawer";

const PATHNAME = "/actions";

export async function VerificationReviewPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const status = verificationStatus(one(sp, "status"));
  const category = one(sp, "category")?.trim();
  const scope = parseScope(sp);
  const selectedId = one(sp, "vi_row");

  const [queue, positions] = await Promise.all([
    listVerificationQueue({
      status,
      category,
      businessDate: scope.asOf,
      parkId: scope.parkId,
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
  const typeLabels = new Map(actionTypes.map((option) => [option.category, `${option.module_label} · ${option.label}`]));
  const statusLabels = new Map(statuses.map((option) => [option.status, option.label]));
  const statusLabelRecord = Object.fromEntries(statuses.map((option) => [option.status, option.label]));
  const feedback = { status: one(sp, "va_status"), code: one(sp, "va_code") };
  const columns = tableLabels(pageContract, "verification-actions");
  const tableContract = table(pageContract, "verification-actions");

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

      {statuses.length ? (
        <div className="subtabs" style={{ marginBottom: 12 }}>
          {statuses.map((option) => (
            <Link
              key={option.key}
              href={hrefWith(sp, { status: option.status, vi_row: null, vi_cursor: null, va_status: null, va_code: null })}
              replace
              scroll={false}
              className={status === option.status ? "on" : ""}
            >
              {option.label}
            </Link>
          ))}
        </div>
      ) : null}

      <div className="wftoolbar" style={{ marginBottom: 14 }}>
        <form action={PATHNAME} style={{ display: "contents" }}>
          {hiddenInputs(sp, ["category", "vi_row", "vi_cursor", "va_status", "va_code"])}
          <div className="fld" style={{ width: 260, marginBottom: 0 }}>
            <label htmlFor="verification-action-type">{copy(pageContract, "filter.action_type")}</label>
            <select id="verification-action-type" name="category" defaultValue={category ?? ""}>
              <option value="">{copy(pageContract, "filter.all_action_types")}</option>
              {actionTypes.map((option) => (
                <option key={option.key} value={option.category}>
                  {option.module_label} · {option.label}
                </option>
              ))}
            </select>
          </div>
          <button type="submit" className="btn sm">
            <Filter className="ic" aria-hidden="true" />
            {copy(pageContract, "filter.apply")}
          </button>
        </form>
        <Link href={hrefWith(sp, { category: null, status: null, vi_row: null, vi_cursor: null, va_status: null, va_code: null })} replace scroll={false} className="lk small">
          {copy(pageContract, "filter.clear_all")}
        </Link>
      </div>

      <section className="card" style={{ minWidth: 0 }}>
        <div className="hd">
          <ShieldCheck className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <h3>{tableContract.title}</h3>
        </div>
        <div className="bd" style={{ padding: 0, overflowX: "auto" }} tabIndex={0} role="group">
          <table data-enh="1">
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
                    statusLabel={statusLabels.get(item.status) ?? item.status}
                    searchParams={sp}
                    pageContract={pageContract}
                  />
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>

      {queue.ok && queue.data.next_cursor ? (
        <div className="pager" style={{ marginTop: 12 }}>
          <Link href={hrefWith(sp, { vi_cursor: queue.data.next_cursor, vi_row: null, va_status: null, va_code: null })} className="btn sm" replace scroll={false}>
            {copy(pageContract, "pagination.next")}
          </Link>
        </div>
      ) : null}

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
  statusLabel,
  searchParams,
  pageContract,
}: {
  item: VerificationQueueItem;
  actionTypeLabel: string;
  statusLabel: string;
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const href = hrefWith(searchParams, { vi_row: item.item_id, va_status: null, va_code: null });
  return (
    <tr>
      <td>{actionTypeLabel}</td>
      <td>
        {item.vertical} / {item.module}
      </td>
      <td>
        {item.subject_label?.trim()
          ? item.subject_label
          : `${item.operator_name || (item.operator_id ? shortId(item.operator_id) : "—")} · ${item.shed_label || (item.shed_id ? shortId(item.shed_id) : "—")}`}
      </td>
      <td className="muted" style={{ whiteSpace: "nowrap" }}>
        {fmtDateTime(item.captured_at)}
      </td>
      <td>
        <Tag tone={item.status === "rejected" ? "dng" : item.status === "approved" ? "ok" : "warn"}>{statusLabel}</Tag>
      </td>
      <td>
        <span className="muted small">{item.verdict_reason || "—"}</span>
      </td>
      <td>
        <LocalOverlayLink href={href} className="btn sm" scroll={false}>
          {copy(pageContract, "action.open_details")}
        </LocalOverlayLink>
      </td>
    </tr>
  );
}

function verificationStatus(value: string | undefined): VerificationItemStatus {
  if (value === "approved" || value === "rejected") return value;
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
