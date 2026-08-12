import { LocalOverlayLink } from "@/components/local-overlay-link";
import { Syringe } from "lucide-react";
import type { VaccinationOperationsResponse } from "@/lib/api/server";
import { Tag, type Tone } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
import { copy, optionGroup, optionLabel, optionTone, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { scopeHref, type Scope } from "@/lib/scope";
import { VaccinationFilterButton, VisibleTableSearch } from "./vaccination-filter-modal";
import { VaccinationRecordVerifyLocalDrawer } from "./record-verify-drawer";
import { paginateRows, VaccinationTablePager, type VaccinationPageSize } from "./table-pager";
import { one, type RouteSearchParams } from "@/lib/search-params";

// Per-cohort vaccination detail (mock table). Source-backed from /vaccination/operations: one row per
// cohort with headcount, age band, REAL last dose (latest accepted administered_at), next due, and worst
// computed status. No Action Center pivot, no faked last_dose.
function statusTag(pageContract: AdminUiPageContract, workState: string) {
  return <Tag tone={optionTone(pageContract, "work_state_filter_chips", workState) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", workState)}</Tag>;
}

export function VaccinationCohortDetail({
  operations,
  ok,
  scope,
  searchParams,
  pageContract,
}: {
  operations: VaccinationOperationsResponse | null;
  ok: boolean;
  scope: Scope;
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const cohorts = operations?.cohorts ?? [];
  const pageSizeOptions = tablePageSizes(pageContract, "cohort-detail");
  const labels = tableLabels(pageContract, "cohort-detail");
  const paged = paginateRows(cohorts, searchParams, "cohort", 10, pageSizeOptions);
  const selectedId = one(searchParams ?? {}, "cohort_record");
  function pagerHref(page: number): string {
    return scopeHref("/vaccination", scope, {}, { cohort_page: String(page), cohort_limit: String(paged.pageSize) });
  }
  function pageSizeHref(pageSize: VaccinationPageSize): string {
    return scopeHref("/vaccination", scope, {}, { cohort_page: "1", cohort_limit: String(pageSize) });
  }
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <Syringe className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.cohort_detail.title")}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="small muted">{copy(pageContract, "section.cohort_detail.badge")}</span>
      </div>
      {cohorts.length === 0 ? (
        <div className="bd" style={{ display: "flex", alignItems: "center", gap: 12, padding: "14px 16px", flexWrap: "wrap" }}>
          <Syringe className="ic" style={{ width: 18, height: 18, color: "var(--brand)", flexShrink: 0 }} aria-hidden="true" />
          <span className="muted small" style={{ lineHeight: 1.5 }}>
            {ok ? copy(pageContract, "section.cohort_detail.empty") : copy(pageContract, "section.cohort_detail.unavailable")}
          </span>
        </div>
      ) : (
        <div className="bd" style={{ padding: 0 }}>
          <div className="tbar">
            <VisibleTableSearch pageContract={pageContract} label={copy(pageContract, "filter.cohort.search")} />
            <VaccinationFilterButton
              pageContract={pageContract}
              title={copy(pageContract, "filter.cohort.title")}
              searchReason={copy(pageContract, "filter.cohort.reason")}
              filterReason={copy(pageContract, "filter.cohort.filter_reason")}
              rowsLabel={`${cohorts.length} ${copy(pageContract, "pager.rows")} · ${copy(pageContract, "filter.cohort.rows_suffix")}`}
              actionHref={scopeHref("/action-center", scope)}
              actionLabel={copy(pageContract, "action.open_action_center")}
              facets={optionGroup(pageContract, "cohort_detail_facets").map((facet) => facet.label)}
            />
            <span className="muted small">
              {paged.start}-{paged.end} {copy(pageContract, "pager.of")} {cohorts.length} {copy(pageContract, "pager.rows").toLowerCase()}
            </span>
            <span className="muted small">{copy(pageContract, "section.cohort_detail.row_hint")}</span>
          </div>
          <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.cohort_detail.aria")}>
            <table className="vaccination-cohort-detail-table">
              <thead>
                <tr>
                  {labels.map((label) => (
                    <th key={label}>{label}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {paged.items.map((c) => {
                  const href = scopeHref("/vaccination", scope, {}, { cohort_record: cohortRecordId(c) });
                  return (
                    <tr key={`${c.parkId}|${c.shedId}|${c.stage}|${c.partition_label ?? ""}`}>
                      <td>
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>
                          <b>{`${c.stage} · ${c.operational_location_display || c.shedName}`}</b>
                          <div className="muted small">{c.parkName}</div>
                        </LocalOverlayLink>
                      </td>
                      <td className="muted">
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>
                          {c.animals || "—"}
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>
                          <Tag tone="info">{c.ageBand ?? c.stage}</Tag>
                        </LocalOverlayLink>
                      </td>
                      <td className="muted">
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>
                          {c.lastDose ? fmtDate(c.lastDose) : "—"}
                        </LocalOverlayLink>
                      </td>
                      <td className="muted">
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>
                          {c.nextDue ? fmtDate(c.nextDue) : "—"}
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>
                          {statusTag(pageContract, c.workState)}
                        </LocalOverlayLink>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <div className="note" style={{ margin: "12px 14px" }}>
            {copy(pageContract, "section.cohort_detail.note")}
          </div>
          <VaccinationTablePager
            pageContract={pageContract}
            pageSizeOptions={pageSizeOptions}
            page={paged.page}
            pageSize={paged.pageSize}
            total={paged.total}
            start={paged.start}
            end={paged.end}
            noun={copy(pageContract, "label.cohort").toLowerCase()}
            hrefForPage={pagerHref}
            hrefForPageSize={pageSizeHref}
          />
        </div>
      )}
      <VaccinationRecordVerifyLocalDrawer
        records={cohorts.map((cohort) => ({ id: cohortRecordId(cohort), context: { cohort } }))}
        selectionKey="cohort_record"
        initialSelectedId={selectedId}
        scope={scope}
        pageContract={pageContract}
      />
    </section>
  );
}

function cohortRecordId(cohort: VaccinationOperationsResponse["cohorts"][number]): string {
  return `${cohort.parkId}|${cohort.shedId}|${cohort.stage}|${cohort.partition_label ?? ""}`;
}
