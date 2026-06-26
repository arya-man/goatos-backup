import Link from "next/link";
import { Syringe } from "lucide-react";
import type { VaccinationOperationsResponse } from "@/lib/api/server";
import { WORK_STATE_META, type Tone } from "@/features/process-integrity";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
import { scopeHref, type Scope } from "@/lib/scope";
import { VaccinationFilterButton, VisibleTableSearch } from "./vaccination-filter-modal";
import { VaccinationRecordVerifyDrawer } from "./record-verify-drawer";
import { VaccinationTablePager } from "./table-pager";
import { one, type RouteSearchParams } from "@/lib/search-params";

// Per-cohort vaccination detail (mock table). Source-backed from /vaccination/operations: one row per
// cohort with headcount, age band, REAL last dose (latest accepted administered_at), next due, and worst
// computed status. No Action Center pivot, no faked last_dose.
function statusTag(workState: string) {
  if (workState === "completed") return <Tag tone="ok">on schedule</Tag>;
  const meta = WORK_STATE_META[workState as keyof typeof WORK_STATE_META] as { label: string; tone: Tone } | undefined;
  return <Tag tone={meta?.tone ?? "mut"}>{meta?.label ?? workState}</Tag>;
}

export function VaccinationCohortDetail({
  operations,
  ok,
  scope,
  searchParams,
}: {
  operations: VaccinationOperationsResponse | null;
  ok: boolean;
  scope: Scope;
  searchParams?: RouteSearchParams;
}) {
  const cohorts = operations?.cohorts ?? [];
  const selectedId = one(searchParams ?? {}, "cohort_record");
  const selected = selectedId ? cohorts.find((cohort) => cohortRecordId(cohort) === selectedId) : undefined;
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd">
        <Syringe className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>Per-cohort vaccination detail</h3>
        <div className="sp" style={{ flex: 1 }} />
        <span className="small muted">animals · age band · last dose · next due</span>
      </div>
      {cohorts.length === 0 ? (
        <div className="bd" style={{ display: "flex", alignItems: "center", gap: 12, padding: "14px 16px", flexWrap: "wrap" }}>
          <Syringe className="ic" style={{ width: 18, height: 18, color: "var(--brand)", flexShrink: 0 }} aria-hidden="true" />
          <span className="muted small" style={{ lineHeight: 1.5 }}>
            {ok
              ? "No cohorts with vaccination obligations yet. Rows appear per park/shed cohort once a protocol is published and drives generate."
              : "Cohort detail is unavailable until the service responds; resolve the error above and reload."}
          </span>
        </div>
      ) : (
        <div className="bd" style={{ padding: 0 }}>
          <div className="tbar">
            <VisibleTableSearch label="Search cohort vaccination rows" />
            <VaccinationFilterButton
              title="Filter — Per-cohort vaccination detail"
              searchReason="Search cohort, age band, dose, due date, status..."
              filterReason="Use visible-row search and quick facets; click a status to open the cohort record."
              rowsLabel={`${cohorts.length} rows · animals, dose history, next due`}
              actionHref={scopeHref("/action-center", scope)}
              actionLabel="Open Action Center"
              facets={["Cohort", "Age band", "Last dose", "Next due", "Status"]}
            />
            <span className="muted small">{cohorts.length} rows</span>
            <span className="muted small">click a row → cohort record</span>
          </div>
          <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Per-cohort vaccination detail">
            <table>
              <thead>
                <tr>
                  <th>Cohort</th>
                  <th>Animals</th>
                  <th>Age band</th>
                  <th>Last dose</th>
                  <th>Next due</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {cohorts.map((c) => {
                  const href = scopeHref("/vaccination", scope, {}, { cohort_record: cohortRecordId(c) });
                  return (
                    <tr key={`${c.parkId}|${c.shedId}|${c.stage}`}>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>
                          <b>{`${c.stage} · ${c.shedName}`}</b>
                          <div className="muted small">{c.parkName}</div>
                        </Link>
                      </td>
                      <td className="muted">
                        <Link href={href} className="celllink" scroll={false}>
                          {c.animals || "—"}
                        </Link>
                      </td>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>
                          <Tag tone="info">{c.ageBand ?? c.stage}</Tag>
                        </Link>
                      </td>
                      <td className="muted">
                        <Link href={href} className="celllink" scroll={false}>
                          {c.lastDose ? fmtDate(c.lastDose) : "—"}
                        </Link>
                      </td>
                      <td className="muted">
                        <Link href={href} className="celllink" scroll={false}>
                          {c.nextDue ? fmtDate(c.nextDue) : "—"}
                        </Link>
                      </td>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>
                          {statusTag(c.workState)}
                        </Link>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <div className="note" style={{ margin: "12px 14px" }}>
            Status is keyed on the cohort&apos;s open obligations + interval. <b>Last dose</b> is the latest accepted
            administered dose for the cohort; animal counts drive dose quantities and FEFO stock reserves on verify.
          </div>
          <VaccinationTablePager rows={cohorts.length} noun="cohort" />
        </div>
      )}
      {selected ? <VaccinationRecordVerifyDrawer context={{ cohort: selected }} scope={scope} /> : null}
    </section>
  );
}

function cohortRecordId(cohort: VaccinationOperationsResponse["cohorts"][number]): string {
  return `${cohort.parkId}|${cohort.shedId}|${cohort.stage}`;
}
