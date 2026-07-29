import { redirect } from "next/navigation";
import { AlertTriangle, Baby, Beaker, Milk, Warehouse } from "lucide-react";

import { WorklistFilters, type WorklistFilterField } from "@/components/worklist-filters";
import { WorklistPager } from "@/components/worklist-pager";
import { copy, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getMilkPreparation,
  type MilkPreparationRow,
} from "@/lib/api/server";
import { getCensusLocations } from "@/lib/api/herd-locations";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { backendScope, parseScope } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";

const PAGE_PATH = "/counts/milk-preparation";
const DEFAULT_PAGE_SIZE = 10;

function boundedPageValue(raw: string | undefined, fallback: number, allowed: readonly number[]): number {
  const parsed = Number(raw);
  return Number.isInteger(parsed) && allowed.includes(parsed) ? parsed : fallback;
}

function boundedOffset(raw: string | undefined): number {
  const parsed = Number(raw);
  return Number.isInteger(parsed) && parsed >= 0 && parsed <= 5000 ? parsed : 0;
}

function litres(millilitres: number): string {
  return (millilitres / 1000).toLocaleString("en-IN", { maximumFractionDigits: 3 });
}

function hrefWith(searchParams: RouteSearchParams, updates: Record<string, string | null>): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(searchParams)) {
    if (Array.isArray(value)) value.forEach((item) => next.append(key, item));
    else if (value) next.set(key, value);
  }
  for (const [key, value] of Object.entries(updates)) {
    if (value === null) next.delete(key);
    else next.set(key, value);
  }
  const query = next.toString();
  return query ? `${PAGE_PATH}?${query}` : PAGE_PATH;
}

function sessionCell(row: MilkPreparationRow, sessionNo: number, inactive: string, unit: string): string {
  const session = row.sessions.find((item) => item.session_no === sessionNo);
  if (!session?.active) return inactive;
  return `${litres(session.required_ml)} ${unit}`;
}

function verificationTag(row: MilkPreparationRow, pageContract: AdminUiPageContract) {
  if (row.status === "blocked") {
    return { className: "tag t-dng", label: copy(pageContract, "label.blocked"), title: copy(pageContract, "label.missing_shed") };
  }
  switch (row.verification_status) {
    case "pending_verification":
      return { className: "tag t-warn", label: copy(pageContract, "label.pending_verification") };
    case "completed":
      return { className: "tag t-ok", label: copy(pageContract, "label.verified") };
    case "rework":
      return { className: "tag t-dng", label: copy(pageContract, "label.rework"), title: row.rework_reason };
    default:
      return { className: "tag t-mut", label: copy(pageContract, "label.not_submitted") };
  }
}

export async function MilkPreparationPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const topBarScope = backendScope(parseScope(sp));
  const localParkID = one(sp, "mp_park") ?? "";
  const effectiveParkID = topBarScope.parkId || localParkID || undefined;
  const pageSizeOptions = tablePageSizes(pageContract, "milk-preparation");
  const limit = boundedPageValue(one(sp, "mp_limit"), DEFAULT_PAGE_SIZE, pageSizeOptions);
  const offset = boundedOffset(one(sp, "mp_offset"));

  const [locations, result] = await Promise.all([
    getCensusLocations(),
    getMilkPreparation({ park_id: effectiveParkID, limit, offset }),
  ]);
  if (firstAuthRequiredError(result)) redirect(INTERNAL_LOGIN_PATH);

  const page = result.ok ? result.data : null;
  const rows = page?.items ?? [];
  const summary = page?.summary;
  const cols = tableLabels(pageContract, "milk-preparation");
  const unit = copy(pageContract, "label.litres");
  const inactive = copy(pageContract, "label.inactive_session");
  const filters: WorklistFilterField[] = [{
    kind: "select",
    param: "mp_park",
    label: copy(pageContract, "filter.park_label"),
    value: topBarScope.parkId || localParkID,
    options: locations.parks.map((park) => ({ value: park.id, label: park.name })),
    disabledReason: topBarScope.parkId ? copy(pageContract, "filter.scope_readonly") : undefined,
  }];

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pageContract, "crumb")} / <b>{copy(pageContract, "section.preparation.title")}</b>
          </div>
          <h1>{pageContract.title}</h1>
        </div>
        <div className="sp" style={{ flex: 1 }} />
      </div>

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 16 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>
            <b>{copy(pageContract, "state.preparation_unavailable")}</b>
            <div className="small muted">{copy(pageContract, "state.try_again")}</div>
          </div>
        </div>
      ) : null}

      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.preparation.title")}</h3>
          <span className="small muted">{copy(pageContract, "section.preparation.caption")}</span>
        </div>
        <WorklistFilters basePath={PAGE_PATH} pageParam="mp_offset" fields={filters} pageContract={pageContract} />

        {page ? (
          <div className="note" style={{ marginBottom: 16, fontWeight: 600 }}>
            {copy(pageContract, "label.prepared_for")
              .replace("{preparation_date}", page.preparation_date)
              .replace("{feeding_date}", page.feeding_date)}
          </div>
        ) : null}

        {summary ? (
          <div className="grid g4" aria-label={copy(pageContract, "section.summary.aria")}>
            <div className="kpi">
              <span className="acc" style={{ background: "var(--brand)" }} />
              <div className="lab">{copy(pageContract, "kpi.sheds.label")}</div>
              <div className="val">{summary.shed_count}</div>
              <div className="dl"><span className="muted">{copy(pageContract, "kpi.sheds.sub")}</span></div>
              <Warehouse className="ic kpiic" aria-hidden="true" />
            </div>
            <div className="kpi">
              <span className="acc" style={{ background: "var(--teal)" }} />
              <div className="lab">{copy(pageContract, "kpi.kids.label")}</div>
              <div className="val">{summary.head_count}</div>
              <div className="dl"><span className="muted">{copy(pageContract, "kpi.kids.sub")}</span></div>
              <Baby className="ic kpiic" aria-hidden="true" />
            </div>
            <div className="kpi">
              <span className="acc" style={{ background: "var(--info)" }} />
              <div className="lab">{copy(pageContract, "kpi.milk.label")}</div>
              <div className="val">{litres(summary.total_required_ml)} {unit}</div>
              <div className="dl"><span className="muted">{copy(pageContract, "kpi.milk.sub")}</span></div>
              <Milk className="ic kpiic" aria-hidden="true" />
            </div>
            <div className="kpi">
              <span className="acc" style={{ background: "var(--purple)" }} />
              <div className="lab">{copy(pageContract, "kpi.citric.label")}</div>
              <div className="val">{summary.citric_acid_grams} {copy(pageContract, "label.grams")}</div>
              <div className="dl"><span className="muted">{copy(pageContract, "kpi.citric.sub")}</span></div>
              <Beaker className="ic kpiic" aria-hidden="true" />
            </div>
          </div>
        ) : null}

        {summary ? <div className="note" style={{ marginBottom: 16 }}>{copy(pageContract, "section.summary.note")}</div> : null}

        {summary ? (
          <div className="note" style={{ marginBottom: 16, display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
            <b>{copy(pageContract, "section.verification.label")}</b>
            <span className="tag t-mut">{copy(pageContract, "label.not_submitted")}: {summary.not_submitted_park_count}</span>
            <span className="tag t-warn">{copy(pageContract, "label.pending_verification")}: {summary.pending_verification_park_count}</span>
            <span className="tag t-ok">{copy(pageContract, "label.verified")}: {summary.completed_park_count}</span>
            <span className="tag t-dng">{copy(pageContract, "label.rework")}: {summary.rework_park_count}</span>
          </div>
        ) : null}

        <div className="bd feed-scroll" style={{ padding: 0, overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.preparation.aria")}>
          <table className="feed-table" aria-label={copy(pageContract, "table.preparation.aria")}>
            <thead><tr>{cols.map((col) => <th key={col}>{col}</th>)}</tr></thead>
            <tbody>
              {rows.length === 0 ? (
                <tr><td colSpan={cols.length}><div className="muted small" style={{ padding: "18px 4px", textAlign: "center" }}>{copy(pageContract, "empty.preparation")}</div></td></tr>
              ) : rows.map((row) => {
                const status = verificationTag(row, pageContract);
                return <tr key={`${row.park_id}|${row.shed_id}|${row.management_stage}`}>
                  <td>{row.park_label || copy(pageContract, "label.unassigned_park")}</td>
                  <td>{row.shed_label || copy(pageContract, "label.unassigned_shed")}</td>
                  <td><span className="tag t-info">{row.management_stage}</span></td>
                  <td>{row.head_count}</td>
                  {[1, 2, 3, 4].map((sessionNo) => <td key={sessionNo}>{sessionCell(row, sessionNo, inactive, unit)}</td>)}
                  <td style={{ fontWeight: 650 }}>{litres(row.daily_required_ml)} {unit}</td>
                  <td>
                    <span className={status.className} title={status.title || undefined}>
                      {status.label}
                    </span>
                  </td>
                </tr>
              })}
            </tbody>
          </table>
        </div>

        <WorklistPager
          pageContract={pageContract}
          offset={offset}
          limit={limit}
          rowCount={rows.length}
          hasMore={page?.has_more ?? false}
          noun={copy(pageContract, "table.preparation.noun")}
          pageSizeOptions={pageSizeOptions}
          hrefForOffset={(nextOffset) => hrefWith(sp, { mp_offset: String(nextOffset) })}
          hrefForLimit={(nextLimit) => hrefWith(sp, { mp_limit: String(nextLimit), mp_offset: null })}
        />
      </section>

      <div className="note">{copy(pageContract, "section.preparation.note")}</div>
    </div>
  );
}
