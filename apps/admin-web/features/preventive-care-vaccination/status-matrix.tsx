import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { Syringe } from "lucide-react";
import type { VaccinationOperationsResponse, VaccinationOperationsCell } from "@/lib/api/server";
import type { Tone } from "@/features/process-integrity";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
import { copy, optionGroup, optionLabel, optionTone, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { scopeHref, type Scope } from "@/lib/scope";
import { VaccinationFilterButton, VisibleTableSearch } from "./vaccination-filter-modal";
import { VaccinationRecordVerifyLocalDrawer, type VaccinationRecordSelection } from "./record-verify-drawer";
import { paginateRows, VaccinationTablePager, type VaccinationPageSize } from "./table-pager";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { sortVaccinationProtocols, vaccinationProtocolDisplayName } from "./vaccine-display";

// Matrix chips use the mock's short operational language. The backend work_state stays canonical; this is only
// display copy for the cohort x vaccine table.
function matrixCellMeta(pageContract: AdminUiPageContract, cell: VaccinationOperationsCell): { label: string; tone: Tone } {
  const label = cell.workState === "due" ? dueLabel(pageContract, cell.nextDue) : optionLabel(pageContract, "matrix_states", cell.workState);
  return { label, tone: optionTone(pageContract, "matrix_states", cell.workState) as Tone };
}

function dueLabel(pageContract: AdminUiPageContract, nextDue: string | undefined): string {
  const base = optionLabel(pageContract, "matrix_states", "due");
  if (!nextDue) return base;
  const due = new Date(nextDue);
  if (Number.isNaN(due.getTime())) return base;
  const now = new Date();
  const dayMs = 24 * 60 * 60 * 1000;
  const days = Math.ceil((startOfDay(due).getTime() - startOfDay(now).getTime()) / dayMs);
  return days > 0 ? `${base} ${days}${copy(pageContract, "label.days_suffix")}` : base;
}

function startOfDay(value: Date): Date {
  return new Date(value.getFullYear(), value.getMonth(), value.getDate());
}

function LegendSwatch({ varName, label }: { varName: string; label: string }) {
  return (
    <span>
      <i className="sw" style={{ background: `var(${varName})` }} aria-hidden="true" />
      {label}
    </span>
  );
}

export function VaccinationStatusMatrix({
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
  const protocols = sortVaccinationProtocols(operations?.protocols ?? []);
  const cohorts = operations?.cohorts ?? [];
  const pageSizeOptions = tablePageSizes(pageContract, "status-matrix");
  const labels = tableLabels(pageContract, "status-matrix");
  const paged = paginateRows(cohorts, searchParams, "matrix", 10, pageSizeOptions);
  const empty = protocols.length === 0 || cohorts.length === 0;
  const selectedId = one(searchParams ?? {}, "vacc_record");
  const records: VaccinationRecordSelection[] = [];
  for (const cohort of cohorts) {
    for (const protocol of protocols) {
      const cell = cohort.cells.find((candidate) => candidate.protocolId === protocol.protocolId);
      if (cell) {
        records.push({
          id: matrixRecordId(cohort, protocol.protocolId),
          context: { cohort: { ...cohort, partitionLabel: cohort.partitionLabel ?? undefined }, protocol, cell },
        });
      }
    }
  }
  function pagerHref(page: number): string {
    return scopeHref("/vaccination", scope, {}, { matrix_page: String(page), matrix_limit: String(paged.pageSize) });
  }
  function pageSizeHref(pageSize: VaccinationPageSize): string {
    return scopeHref("/vaccination", scope, {}, { matrix_page: "1", matrix_limit: String(pageSize) });
  }

  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <Syringe className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.status_matrix.title")}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="legend">
          <LegendSwatch varName="--brand" label={copy(pageContract, "label.up_to_date")} />
          <LegendSwatch varName="--amber" label={copy(pageContract, "label.due_soon")} />
          <LegendSwatch varName="--danger" label={copy(pageContract, "label.overdue")} />
        </span>
      </div>

      {empty ? (
        <div className="bd" style={{ display: "flex", alignItems: "center", gap: 12, padding: "14px 16px", flexWrap: "wrap" }}>
          <Syringe className="ic" style={{ width: 18, height: 18, color: "var(--brand)", flexShrink: 0 }} aria-hidden="true" />
          <div style={{ minWidth: 0, flex: 1 }}>
            <b style={{ fontSize: 14 }}>
              {ok ? copy(pageContract, "section.status_matrix.empty_title") : copy(pageContract, "section.status_matrix.unavailable_title")}
            </b>
            <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
              {ok ? copy(pageContract, "section.status_matrix.empty_body") : copy(pageContract, "section.status_matrix.unavailable_body")}
            </span>
          </div>
          {ok ? (
            <div style={{ display: "flex", gap: 8, flexShrink: 0 }}>
              <Link href={scopeHref("/sops", scope)} className="btn sm">
                {copy(pageContract, "action.open_sop_library")}
              </Link>
            </div>
          ) : null}
        </div>
      ) : (
        <div className="bd" style={{ padding: 0 }}>
          <div className="tbar">
            <VisibleTableSearch pageContract={pageContract} label={copy(pageContract, "filter.status_matrix.search")} />
            <VaccinationFilterButton
              pageContract={pageContract}
              title={copy(pageContract, "filter.status_matrix.title")}
              searchReason={copy(pageContract, "filter.status_matrix.reason")}
              filterReason={copy(pageContract, "filter.status_matrix.filter_reason")}
              rowsLabel={`${cohorts.length} ${copy(pageContract, "pager.rows")} · ${copy(pageContract, "filter.status_matrix.rows_suffix")}`}
              actionHref={scopeHref("/action-center", scope)}
              actionLabel={copy(pageContract, "action.open_action_center")}
              facets={optionGroup(pageContract, "status_matrix_facets").map((facet) => facet.label)}
            />
            <span className="muted small">
              {paged.start}-{paged.end} {copy(pageContract, "pager.of")} {cohorts.length} {copy(pageContract, "pager.rows").toLowerCase()}
            </span>
            <span className="muted small">{copy(pageContract, "section.status_matrix.row_hint")}</span>
          </div>
          <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.status_matrix.aria")}>
            <table className="vaccination-status-matrix-table">
              <thead>
                <tr>
                  <th>{labels[0]}</th>
                  {protocols.map((p) => (
                    <th key={p.protocolId}>{vaccinationProtocolDisplayName(p)}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {paged.items.map((c) => {
                  const byProtocol = new Map<string, VaccinationOperationsCell>();
                  for (const cell of c.cells) byProtocol.set(cell.protocolId, cell);
                  const firstProtocol = protocols.find((protocol) => byProtocol.has(protocol.protocolId));
                  const cohortHref = firstProtocol
                    ? scopeHref("/vaccination", scope, {}, { vacc_record: matrixRecordId(c, firstProtocol.protocolId) })
                    : null;
                  return (
                    <tr key={`${c.parkId}|${c.shedId}|${c.stage}`}>
                      <td>
                        {cohortHref ? (
                          <LocalOverlayLink href={cohortHref} className="celllink" scroll={false} title={copy(pageContract, "section.status_matrix.row_hint")}>
                            <b>{`${c.stage} · ${c.operationalLocationDisplay || c.shedName}`}</b>
                            <div className="muted small">{c.parkName}</div>
                          </LocalOverlayLink>
                        ) : (
                          <>
                            <b>{`${c.stage} · ${c.operationalLocationDisplay || c.shedName}`}</b>
                            <div className="muted small">{c.parkName}</div>
                          </>
                        )}
                      </td>
                      {protocols.map((p) => {
                        const cell = byProtocol.get(p.protocolId);
                        if (!cell) {
                          return (
                            <td key={p.protocolId}>
                              <Tag tone="mut">—</Tag>
                            </td>
                          );
                        }
                        const meta = matrixCellMeta(pageContract, cell);
                        const protocolLabel = vaccinationProtocolDisplayName(p);
                        const title = cell.lastDose ? `${protocolLabel} — ${meta.label} · ${copy(pageContract, "label.last_dose")} ${fmtDate(cell.lastDose)}` : `${protocolLabel} — ${meta.label}`;
                        return (
                          <td key={p.protocolId}>
                            <LocalOverlayLink
                              href={scopeHref("/vaccination", scope, {}, { vacc_record: matrixRecordId(c, p.protocolId) })}
                              className="celllink"
                              title={title}
                              scroll={false}
                            >
                              <Tag tone={meta.tone}>{meta.label}</Tag>
                            </LocalOverlayLink>
                          </td>
                        );
                      })}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <div className="note" style={{ margin: "12px 14px" }}>
            {copy(pageContract, "section.status_matrix.note")}{" "}
            <Link href={scopeHref("/protocol-adherence", scope)} className="lk">
              {copy(pageContract, "action.open_protocol_adherence")}
            </Link>
            <Link href={scopeHref("/action-center", scope)} className="lk">
              {copy(pageContract, "action.open_action_center")}
            </Link>
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
        records={records}
        selectionKey="vacc_record"
        initialSelectedId={selectedId}
        scope={scope}
        pageContract={pageContract}
      />
    </section>
  );
}

function matrixRecordId(cohort: VaccinationOperationsResponse["cohorts"][number], protocolId: string): string {
  return `${cohort.parkId}|${cohort.shedId}|${cohort.stage}|${protocolId}`;
}
