import Link from "@/components/no-prefetch-link";
import { redirect } from "next/navigation";
import { Filter, ShieldCheck } from "lucide-react";

import { Tag, type Tone } from "@/components/ui-primitives";
import { firstAuthRequiredError, listStaffPositions, listVerificationQueue, type VerificationItemStatus, type VerificationQueueItem } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { fmtDateTime, shortId } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope } from "@/lib/scope";
import { VERIFICATION_REVIEW_COPY as COPY } from "./copy";
import { VerificationReviewDrawer } from "./verification-review-drawer";

const PATHNAME = "/verification";
const STATUS_TABS: VerificationItemStatus[] = ["rejected", "approved", "pending"];
// Only vaccination_proof is registered in the type registry today (backend/internal/bootstrap/api.go
// RegisterCategory call, context/architecture/verification-module-design.md §2.3 "plug-and-play
// registry"). This is a display default, not a hardcoded business rule — the category filter still
// reads whatever categories are actually present on the fetched page.
const KNOWN_CATEGORY_FALLBACK = "vaccination_proof";

export async function VerificationReviewPage({ searchParams }: { searchParams?: RouteSearchParams }) {
  const sp = searchParams ?? {};
  const status = STATUS_TABS.find((s) => s === one(sp, "status")) ?? "rejected";
  const category = one(sp, "category")?.trim();
  const scope = parseScope(sp);
  const { parkId } = backendScope(scope);

  const queue = await listVerificationQueue({ status, category, limit: 20, cursor: one(sp, "vi_cursor") });
  const authError = firstAuthRequiredError(queue);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const allItems = queue.ok ? queue.data.items : [];
  // No park_id query param on /verification/queue yet (TODO in lib/api/server.ts) — narrow the
  // already-fetched bounded page (max 20 rows) client-side by the top-bar park scope instead of a
  // second unbounded fetch.
  const items = parkId ? allItems.filter((item) => !item.park_id || item.park_id === parkId) : allItems;

  const selectedId = one(sp, "vi_row");
  const selected = items.find((item) => item.item_id === selectedId);

  // One bounded roster read per drawer open (not per row) so "Re-assign" offers real staff
  // positions scoped to the selected item's park. See features/verification-review/copy.ts for why
  // names are not resolved (would require a getStaffPositionProfile call per row — N+1).
  const positions =
    selected?.park_id
      ? await listStaffPositions({ scope_type: "center", scope_id: selected.park_id, status: "active", limit: 50 })
      : null;

  const returnTo = hrefWith(sp, {});
  const feedback = { status: one(sp, "va_status"), code: one(sp, "va_code") };

  const categories = Array.from(new Set(allItems.map((item) => item.category))).sort();

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            {COPY.crumb} / <b>{COPY.title}</b>
          </div>
          <h1>{COPY.title}</h1>
          <div className="sub">{COPY.subtitle}</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
      </div>

      {queue.ok ? null : (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{COPY.error.queueUnavailable}</b>
          <div className="small" style={{ marginTop: 4 }}>
            {COPY.error.queueUnavailableBody}
          </div>
          <div className="small muted" style={{ marginTop: 4 }}>
            {queue.error.code ?? queue.error.kind} · {queue.error.message}
          </div>
        </div>
      )}

      <div className="grid g4" style={{ marginBottom: 16 }}>
        <KPI label={COPY.kpi.rejectedInView} value={String(countStatus(items, "rejected"))} tone="dng" />
        <KPI label={COPY.kpi.approvedInView} value={String(countStatus(items, "approved"))} tone="ok" />
        <KPI label={COPY.kpi.noTaskHandle} value={String(items.filter((item) => !item.source.task_id).length)} tone="warn" />
        <KPI label={COPY.kpi.rowsInView} value={String(items.length)} tone="info" />
      </div>

      <div className="subtabs" style={{ marginBottom: 12 }}>
        {STATUS_TABS.map((key) => (
          <Link key={key} href={hrefWith(sp, { status: key, vi_row: null, vi_cursor: null, va_status: null, va_code: null })} replace scroll={false} className={status === key ? "on" : ""}>
            {COPY.statusTab[key]}
          </Link>
        ))}
      </div>

      <div className="wftoolbar" style={{ marginBottom: 14 }}>
        <form action={PATHNAME} style={{ display: "contents" }}>
          {hiddenInputs(sp, ["category", "vi_row", "va_status", "va_code"])}
          <div className="fld" style={{ width: 220, marginBottom: 0 }}>
            <label htmlFor="verification-category">{COPY.filter.categoryLabel}</label>
            <select id="verification-category" name="category" defaultValue={category ?? ""}>
              <option value="">{COPY.filter.categoryAll}</option>
              {(categories.length ? categories : [KNOWN_CATEGORY_FALLBACK]).map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
          </div>
          <button type="submit" className="btn sm">
            <Filter className="ic" aria-hidden="true" />
            {COPY.filter.categoryLabel}
          </button>
        </form>
        <Link href={PATHNAME} replace scroll={false} className="lk small">
          {COPY.filter.clearAll}
        </Link>
      </div>

      <section className="card" style={{ minWidth: 0 }}>
        <div className="hd">
          <ShieldCheck className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" />
          <h3>{COPY.title}</h3>
        </div>
        <div className="bd" style={{ padding: 0, overflowX: "auto" }} tabIndex={0} role="group">
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
                items.map((item) => <QueueRow key={item.item_id} item={item} searchParams={sp} />)
              )}
            </tbody>
          </table>
        </div>
      </section>

      {selected ? (
        <VerificationReviewDrawer item={selected} positions={positions?.ok ? positions.data : null} searchParams={sp} returnTo={returnTo} feedback={feedback} />
      ) : null}
    </div>
  );
}

function QueueRow({ item, searchParams }: { item: VerificationQueueItem; searchParams: RouteSearchParams }) {
  const href = hrefWith(searchParams, { vi_row: item.item_id, va_status: null, va_code: null });
  return (
    <tr>
      <td>{item.category}</td>
      <td>
        {item.vertical} / {item.module}
      </td>
      <td>
        {item.operator_id ? shortId(item.operator_id) : "—"} · {item.shed_id ? shortId(item.shed_id) : "—"}
      </td>
      <td className="muted" style={{ whiteSpace: "nowrap" }}>
        {fmtDateTime(item.captured_at)}
      </td>
      <td>
        <Tag tone={item.status === "rejected" ? "dng" : item.status === "approved" ? "ok" : "warn"}>{item.status}</Tag>
      </td>
      <td>
        <span className="muted small">{item.verdict_reason || "—"}</span>
      </td>
      <td>
        <Link href={href} className="btn sm" scroll={false}>
          Review
        </Link>
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

function countStatus(items: VerificationQueueItem[], status: VerificationItemStatus): number {
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

function hiddenInputs(params: RouteSearchParams, exclude: string[]) {
  return Object.entries(params).flatMap(([key, value]) => {
    if (exclude.includes(key)) return [];
    if (Array.isArray(value)) {
      return value.filter(Boolean).map((item) => <input key={`${key}:${item}`} type="hidden" name={key} value={item} />);
    }
    return value ? [<input key={key} type="hidden" name={key} value={value} />] : [];
  });
}
