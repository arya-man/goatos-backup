import { ChevronLeft, ChevronRight, Search } from "lucide-react";
import Link from "@/components/no-prefetch-link";
import { type AdminUiPageContract, copy, optionLabel } from "@/lib/admin-ui-contract";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { parseScope, scopeHref } from "@/lib/scope";
import { fmtDate as fmtIstDate } from "@/lib/format";
import { getCalendarVaccinationEventDetail, getCalendarDriveTargets } from "./calendar-server";
import { driveSummaryOf, type CalendarDriveTarget } from "./calendar-contract";
import { driveCoverage, driveCoveragePct, driveStatusChips, driveStatusClass } from "./drive-card-metrics";

// Full-screen drive detail (owner-directed replacement for the calendar drive drawer, 2026-07-14).
// New route with no backend page contract yet, so its structural labels (breadcrumb crumbs, roster
// column headers) are local literals — the same documented exception as /verification (see
// context/frontend/admin-web-backend-ui-contract.md, allow-listed in check-ui-contract-literals.mjs).

function targetReason(item: CalendarDriveTarget): string {
  if (item.status === "deferred" && item.defer_reason) return item.defer_reason;
  if (item.exit_reason) return item.exit_reason;
  return "";
}

function hiddenInputs(params: RouteSearchParams, exclude: Set<string>) {
  return Object.entries(params).flatMap(([key, value]) => {
    if (exclude.has(key)) return [];
    const values = Array.isArray(value) ? value : value ? [value] : [];
    return values.map((item, index) => <input key={`${key}-${index}`} type="hidden" name={key} value={item} />);
  });
}

function hrefWithoutKeys(pathname: string, params: RouteSearchParams, keys: Set<string>) {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (keys.has(key)) continue;
    if (Array.isArray(value)) {
      for (const item of value) {
        if (item) next.append(key, item);
      }
    } else if (value) {
      next.set(key, value);
    }
  }
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

export async function VaccinationDriveDetail({
  eventId,
  searchParams,
  pageContract,
}: {
  eventId: string;
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const scope = parseScope(sp);
  const backHref = scopeHref("/calendar", scope);
  const cursor = one(sp, "cursor");
  const targetSearch = (one(sp, "q") ?? "").trim();
  const page = boundedInt(one(sp, "page"), 1, 1, 1000000);
  const detailPath = `/calendar/drive/${eventId}`;

  const [detail, targets] = await Promise.all([
    getCalendarVaccinationEventDetail(eventId),
    getCalendarDriveTargets(eventId, { cursor, limit: 25, q: targetSearch || undefined }),
  ]);

  if (!detail.ok) {
    return (
      <div className="screen on">
        <div className="navback">
          <Link href={backHref} className="nbback">
            <ChevronLeft className="ic" /> Back
          </Link>
        </div>
        <div className="alert">
          <b>{detail.error.code ?? detail.error.kind}</b>&nbsp;{detail.error.message}
        </div>
      </div>
    );
  }

  const event = detail.data.event;
  const summary = driveSummaryOf(event);
  const crumb = (
    <div className="navback">
      <Link href={backHref} className="nbback">
        <ChevronLeft className="ic" /> Back
      </Link>
      <div className="nbtrail">
        <Link href={scopeHref("/vaccination", scope)} className="nbc">{copy(pageContract, "calendar.breadcrumb.vaccination")}</Link>
        <span className="nbsep">/</span>
        <Link href={backHref} className="nbc">{copy(pageContract, "calendar.breadcrumb.calendar")}</Link>
        <span className="nbsep">/</span>
        <span className="nbc cur">{summary ? `${summary.park_name} · ${fmtIstDate(summary.due_date)}` : event.title}</span>
      </div>
    </div>
  );

  if (!summary) {
    return (
      <div className="screen on">
        {crumb}
        <div className="note">{copy(pageContract, "calendar.drive.summary_pending")}</div>
      </div>
    );
  }

  const coverage = driveCoverage(summary.completed_animals, summary.total_animals, summary.completed_count, summary.total_count);
  const pct = driveCoveragePct(coverage.completed, coverage.total);
  const chips = driveStatusChips(summary);
  const ringRadius = 29;
  const ringCircumference = 2 * Math.PI * ringRadius;
  const ringOffset = ringCircumference * (1 - pct / 100);

  const rosterItems = targets && targets.ok ? targets.data.items : [];
  const nextCursor = targets && targets.ok ? targets.data.next_cursor : undefined;
  const targetsError = targets && !targets.ok ? targets.error : null;
  const nextHref = nextCursor ? hrefWithPagedCursor(detailPath, sp, "cursor", nextCursor, "page", "cursor_stack") : null;
  const prevHref = hrefPreviousPagedCursor(detailPath, sp, "cursor", "page", "cursor_stack");
  const clearSearchHref = hrefWithoutKeys(detailPath, sp, new Set(["q", "cursor", "page", "cursor_stack"]));

  return (
    <div className="screen on">
      {crumb}
      <div className="card">
        <div className="hd"><h3>{event.title} · {summary.park_name}</h3></div>
        <div className="bd">
          <div className="ddhero">
            <svg className="dring" viewBox="0 0 70 70" width="92" height="92" aria-hidden="true">
              <circle className="rbg" cx="35" cy="35" r={ringRadius} />
              <circle className="rfg" cx="35" cy="35" r={ringRadius} strokeDasharray={ringCircumference.toFixed(1)} strokeDashoffset={ringOffset.toFixed(1)} transform="rotate(-90 35 35)" />
              <text x="35" y="35" className="rtx" textAnchor="middle" dominantBaseline="central">{pct}%</text>
            </svg>
            <div>
              <div style={{ fontSize: 22, fontWeight: 700 }}>
                <span className="mono">{coverage.completed}</span>{" "}
                <span style={{ fontSize: 15, color: "var(--muted)" }}>
                  {copy(pageContract, "calendar.drive.of")} {coverage.total} {copy(pageContract, coverage.usesAnimals ? "calendar.drive.animals" : "calendar.drive.doses")}
                </span>
              </div>
              <div className="metric" style={{ marginTop: 6 }}>
                <span><b style={{ color: "var(--ink)" }}>{summary.sheds_completed}</b> {copy(pageContract, "calendar.drive.of")} {summary.shed_count} {copy(pageContract, "calendar.drive.sheds_done_suffix")} · {copy(pageContract, "calendar.drive.owner")} {summary.owner_label}</span>
              </div>
              {summary.vaccine_labels.length ? (
                <div className="vchips">
                  {summary.vaccine_labels.map((label) => (<span key={label} className="tag t-mut">{label}</span>))}
                </div>
              ) : null}
            </div>
          </div>
          {chips.length ? (
            <div className="chips" style={{ marginTop: 14, gap: 14 }}>
              {chips.map((chip) => (
                <span key={chip.key} className={`sc ${driveStatusClass(chip.key)}`}>
                  <span className={`d c-${driveStatusClass(chip.key)}`} />
                  {chip.count} {optionLabel(pageContract, "calendar_status", chip.key).toLowerCase()}
                </span>
              ))}
            </div>
          ) : null}
        </div>
      </div>

      <div className="card">
        <div className="bd">
          <div className="lt">{copy(pageContract, "calendar.drive.animal_roster")}</div>
          <div style={{ display: "flex", alignItems: "center", gap: 12, justifyContent: "space-between", flexWrap: "wrap", margin: "0 0 14px" }}>
            <form action={detailPath} style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 320, flex: "1 1 420px" }}>
              {hiddenInputs(sp, new Set(["q", "cursor", "page", "cursor_stack"]))}
              <label className="searchbox" style={{ flex: "1 1 280px", display: "flex", alignItems: "center", gap: 8 }}>
                <Search className="ic" style={{ width: 15 }} aria-hidden="true" />
                <input
                  name="q"
                  defaultValue={targetSearch}
                  placeholder={copy(pageContract, "calendar.drive.search_placeholder")}
                  style={{ width: "100%", background: "transparent", border: 0, outline: 0, color: "inherit" }}
                />
              </label>
              <button type="submit" className="btn btn-primary">{copy(pageContract, "calendar.drive.search_action")}</button>
              {targetSearch ? <Link href={clearSearchHref} className="btn">{copy(pageContract, "calendar.drive.clear_search")}</Link> : null}
            </form>
            <div className="sub" style={{ whiteSpace: "nowrap" }}>
              {copy(pageContract, "calendar.drive.page_label")} {page} · {rosterItems.length} {copy(pageContract, "calendar.drive.rows_label")}
            </div>
          </div>
          {targetsError ? (
            <div style={{ padding: 16, textAlign: "center", color: "var(--danger)" }}>
              <b>{targetsError.code ?? targetsError.kind}</b>&nbsp;{targetsError.message}
            </div>
          ) : (
            <>
              <div style={{ overflowX: "auto" }}>
                <table className="rostertbl">
                  <thead><tr><th>{copy(pageContract, "calendar.drive.display_id_header")}</th><th>{copy(pageContract, "calendar.drive.shed_header")}</th><th>{copy(pageContract, "calendar.drive.tag_1_header")}</th><th>{copy(pageContract, "calendar.drive.tag_2_header")}</th><th>{copy(pageContract, "calendar.drive.stage_header")}</th><th>{copy(pageContract, "calendar.drive.lifecycle_header")}</th><th>{copy(pageContract, "calendar.drive.health_header")}</th><th>{copy(pageContract, "calendar.drive.reason_header")}</th><th>{copy(pageContract, "calendar.drive.status_header")}</th></tr></thead>
                  <tbody className="mono">
                    {rosterItems.length ? rosterItems.map((item) => (
                      <tr key={item.animal_id}>
                        <td>{item.display_id || "—"}</td>
                        <td>{item.shed_name || "—"}</td>
                        <td>{item.animal_identifier_1 || "—"}</td>
                        <td>{item.animal_identifier_2 || "—"}</td>
                        <td>{item.stage || "—"}</td>
                        <td>{item.lifecycle_status || "—"}</td>
                        <td>{item.health_status || "—"}</td>
                        <td>{targetReason(item) || "—"}</td>
                        <td>{optionLabel(pageContract, "calendar_status", item.status).toLowerCase() || item.status}</td>
                      </tr>
                    )) : (
                      <tr><td colSpan={9} style={{ padding: 10, textAlign: "center", color: "var(--muted)" }}>{copy(pageContract, "calendar.drive.no_animals")}</td></tr>
                    )}
                  </tbody>
                </table>
              </div>
              <div style={{ padding: 12, display: "flex", alignItems: "center", justifyContent: "flex-end", gap: 10, borderTop: "1px solid var(--border)" }}>
                {prevHref ? (
                  <Link href={prevHref} className="btn">
                    <ChevronLeft className="ic" /> {copy(pageContract, "calendar.drive.previous_page")}
                  </Link>
                ) : (
                  <span className="btn" aria-disabled="true" style={{ opacity: 0.45, pointerEvents: "none" }}>
                    <ChevronLeft className="ic" /> {copy(pageContract, "calendar.drive.previous_page")}
                  </span>
                )}
                <span className="sub">{copy(pageContract, "calendar.drive.page_label")} {page}</span>
                {nextHref ? (
                  <Link href={nextHref} className="btn btn-primary">
                    {copy(pageContract, "calendar.drive.next_page")} <ChevronRight className="ic" />
                  </Link>
                ) : (
                  <span className="btn" aria-disabled="true" style={{ opacity: 0.45, pointerEvents: "none" }}>
                    {copy(pageContract, "calendar.drive.next_page")} <ChevronRight className="ic" />
                  </span>
                )}
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
