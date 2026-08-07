import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { Filter, PlayCircle } from "lucide-react";

import { Tag } from "@/components/ui-primitives";
import { controlEnabled, copy, table, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError, listVerificationQueue, type VerificationItemStatus, type VerificationQueueItem } from "@/lib/api/server";
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
  const trail = decodeTrail(one(sp, "vi_trail"));

  // ONE read on the critical path. The staff roster the re-assign picker offers used to be fetched
  // here too -- listStaffPositions with limit 500, awaited alongside the queue on every load -- for
  // a control that lives inside the drawer, is only reachable after a row is opened, and is only
  // usable on a rejected item with a linked SOP task. It now loads lazily, per park, when the
  // drawer opens (loadReassignPositionsAction), so the queue paints without waiting on it.
  const queue = await listVerificationQueue({
    status,
    category,
    businessDate: scope.asOf,
    parkId: scope.parkId,
    shedId,
    limit: 20,
    cursor: one(sp, "vi_cursor"),
  });
  const authError = firstAuthRequiredError(queue);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const items = queue.ok ? queue.data.items : [];
  const actionTypes = queue.ok ? queue.data.filter_options.action_types : [];
  const statuses = queue.ok ? queue.data.filter_options.statuses : [];
  const sheds = queue.ok ? queue.data.filter_options.sheds : [];
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

      <VerificationQueueTelemetry
        category={category}
        parkId={scope.parkId}
        shedId={shedId}
        status={status}
        enabled={controlEnabled(pageContract, "record_verdict", false)}
      >
        <section className="card vr-board" style={{ minWidth: 0 }}>
        <div className="bt">{copy(pageContract, "board.title")}</div>

        {/* The action-type select was REMOVED (maintainer decision 2026-08-07). The left nav
            already scopes this screen -- every leaf sets ?category= -- so the dropdown was a
            second, competing scope control for a choice the verifier had just made in the sidebar.
            It was also wrong: its defaultValue never matched the URL category, so it sat on an
            unrelated action type on every category the nav could reach.

            Shed is now the only filter, so the whole row is conditional on there being sheds to
            choose between: without this, a module with no shed options (Birth, Death) rendered an
            Apply/Clear pair with nothing to apply. */}
        {sheds.length ? (
          <div className="vr-frow">
            <form action={PATHNAME} style={{ display: "contents" }}>
              {hiddenInputs(sp, ["category", "shed_id", "vi_row", "vi_cursor", "vi_trail", "va_status", "va_code"])}
              {/* Carries the sidebar's scope through the submit; without it, filtering by shed
                  would silently widen the queue back to every module. */}
              {category ? <input type="hidden" name="category" value={category} /> : null}
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
              <button type="submit" className="btn sm">
                <Filter className="ic" aria-hidden="true" />
                {copy(pageContract, "filter.apply")}
              </button>
            </form>
            {/* Deliberately does NOT clear `category`: that is the sidebar's selection, not a
                filter the verifier set here. Clearing it stranded her on every module's queue at
                once while the nav still highlighted the one she had picked. */}
            <Link
              href={hrefWith(sp, { shed_id: null, status: null, vi_row: null, vi_cursor: null, vi_trail: null, va_status: null, va_code: null })}
              replace
              scroll={false}
              className="lk small"
            >
              {copy(pageContract, "filter.clear_all")}
            </Link>
          </div>
        ) : null}

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
        initialSelectedId={selectedId}
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

function decodeTrail(raw: string | undefined): string[] {
  if (!raw) return [];
  return raw.split("~").map((entry) => {
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
  return bounded.map((entry) => encodeURIComponent(entry)).join("~");
}
