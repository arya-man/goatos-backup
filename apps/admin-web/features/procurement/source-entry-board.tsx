import Link from "next/link";
import { redirect } from "next/navigation";
import { ArrowRight, PackageSearch, Truck } from "lucide-react";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { listProcurementLoads } from "@/lib/api/procurement-server";
import type { ProcurementLoad, ProcurementLoadStatus } from "@/lib/api/procurement";
import { boundedInt, hrefPreviousCursor, hrefWithCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { fmtDate, shortId } from "@/lib/format";
import { Tag } from "@/components/ui-primitives";
import { PROC_LOAD_STATUS_META, PROC_LOAD_STATUS_ORDER } from "./work-state";
import { NewLoadForm } from "./load-forms";
import { ProcurementPager } from "./pager";

const LOADS_COLS = ["Load", "Source party", "Holding / source", "Expected", "Purchase", "Planned dispatch", "Status", "Updated"];

// The canonical source-entry journey, shown as IA so users learn that a goat journey starts at
// purchase/source and that park arrival is only one gate near the end. Explanatory copy, not data.
const JOURNEY_STAGES = ["Purchase / source", "Holding warmup", "Source health SOP", "Pre-dispatch", "Transit", "Arrival gate", "Accepted intake"];

export async function SourceEntryBoardPage({ searchParams }: { searchParams?: RouteSearchParams }) {
  const sp = searchParams ?? {};
  const pathname = "/procurement/source-entry";
  const statusFilter = (PROC_LOAD_STATUS_ORDER.find((s) => s === one(sp, "status")) ?? "all") as ProcurementLoadStatus | "all";
  const cursor = one(sp, "cursor");
  const page = boundedInt(one(sp, "page"), 1, 1, 1_000_000);
  const PAGE_SIZE = 200;
  const actionStatus = one(sp, "action_status");
  const actionMessage = one(sp, "action_message");

  const result = await listProcurementLoads({
    status: statusFilter === "all" ? undefined : statusFilter,
    limit: PAGE_SIZE,
    cursor,
  });
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  // Sort the returned page by the canonical stage order so the board reads source-side -> intake.
  const loads: ProcurementLoad[] = result.ok
    ? [...result.data.items].sort((a, b) => PROC_LOAD_STATUS_ORDER.indexOf(a.status) - PROC_LOAD_STATUS_ORDER.indexOf(b.status))
    : [];
  const nextCursor = result.ok ? result.data.next_cursor ?? null : null;
  const nextHref = hrefWithCursor(pathname, sp, nextCursor);
  const prevHref = hrefPreviousCursor(pathname, sp);

  // Status filter resets the cursor/page (a new filter starts a fresh first page).
  function statusHref(status: ProcurementLoadStatus | "all"): string {
    return status === "all" ? pathname : `${pathname}?status=${status}`;
  }

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            <b>Procurement</b> · Source entry
          </div>
          <h1>Source Entry Board</h1>
          <div className="sub">
            <b>The goat journey starts at purchase/source</b> — not at park arrival. Loads move through supplier holding,
            source health SOP, and a pre-dispatch decision before any truck. Source warmup of <b>45–70 days is normal</b>.
            Rejected or unresolved goats stay procurement history; only accepted-intake goats flow to PHC / Parks.
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
      </div>

      {/* Journey IA — the full source-entry chain, so park arrival reads as one late gate, not the start. */}
      <div className="fchipsbar" style={{ marginBottom: 14, flexWrap: "wrap" }} aria-label="Source-entry journey">
        <Truck className="ic" style={{ width: 14, color: "var(--brand-d)" }} aria-hidden="true" />
        {JOURNEY_STAGES.map((stage, i) => (
          <span key={stage} style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
            <span className="muted small">{stage}</span>
            {i < JOURNEY_STAGES.length - 1 ? <ArrowRight className="ic" style={{ width: 12, opacity: 0.5 }} aria-hidden="true" /> : null}
          </span>
        ))}
      </div>

      {actionStatus ? (
        actionStatus === "success" ? (
          <div className="note" style={{ marginBottom: 14 }}>
            <Tag tone="ok">done</Tag> {actionMessage ?? "Action completed."}
          </div>
        ) : (
          <div className="alert" style={{ marginBottom: 14 }}>
            <b>Action failed</b>&nbsp;{actionMessage ?? actionStatus}
          </div>
        )
      ) : null}

      <NewLoadForm returnTo={pathname} />

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
      ) : null}

      {/* Status filter (server-side ?status). Park/date scope stays in the top bar; this is a page control. */}
      <div className="chipset" style={{ marginBottom: 14 }}>
        <Link href={statusHref("all")} className={`chip${statusFilter === "all" ? " on" : ""}`}>
          All states
        </Link>
        {PROC_LOAD_STATUS_ORDER.map((s) => (
          <Link key={s} href={statusHref(s)} className={`chip${statusFilter === s ? " on" : ""}`}>
            {PROC_LOAD_STATUS_META[s].label}
          </Link>
        ))}
      </div>

      <section className="card">
        <div className="hd">
          <PackageSearch className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>Loads</h3>
          <Tag tone={loads.length ? "info" : "mut"}>{loads.length}</Tag>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">purchase → holding → pre-dispatch → transit → arrival</span>
        </div>
        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Procurement loads">
          <table>
            <thead>
              <tr>
                {LOADS_COLS.map((c) => (
                  <th key={c}>{c}</th>
                ))}
                <th aria-label="Open" />
              </tr>
            </thead>
            <tbody>
              {loads.length === 0 ? (
                <tr>
                  <td colSpan={LOADS_COLS.length + 1}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {result.ok
                        ? statusFilter === "all"
                          ? "No source-entry loads for this scope. Loads appear here once a purchase/source load is created in the procurement backend."
                          : `No loads in “${PROC_LOAD_STATUS_META[statusFilter as ProcurementLoadStatus].label}” for this scope.`
                        : "Loads are unavailable until the procurement service responds."}
                    </div>
                  </td>
                </tr>
              ) : (
                loads.map((load) => {
                  const href = `/procurement/source-entry/loads/${encodeURIComponent(load.load_id)}`;
                  return (
                    <tr key={load.load_id}>
                      <td>
                        <Link href={href} className="gid">
                          {shortId(load.load_id)}
                        </Link>
                      </td>
                      <td className="muted">{shortId(load.source_party_id)}</td>
                      <td className="muted">{load.source_location_id ? shortId(load.source_location_id) : "—"}</td>
                      <td>{load.expected_count}</td>
                      <td className="muted">{fmtDate(load.purchase_date ?? undefined)}</td>
                      <td className="muted">{fmtDate(load.planned_dispatch_at ?? undefined)}</td>
                      <td>
                        <Tag tone={PROC_LOAD_STATUS_META[load.status].tone}>{PROC_LOAD_STATUS_META[load.status].label}</Tag>
                      </td>
                      <td className="muted">{fmtDate(load.updated_at)}</td>
                      <td>
                        <Link href={href} className="lk small" style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
                          Detail
                          <ArrowRight className="ic" style={{ width: 13, flexShrink: 0 }} aria-hidden="true" />
                        </Link>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
        {loads.length > 0 || page > 1 ? (
          <ProcurementPager prevHref={prevHref} nextHref={nextHref} page={page} count={loads.length} noun="load" />
        ) : null}
      </section>
    </div>
  );
}
