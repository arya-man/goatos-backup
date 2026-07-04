import Link from "next/link";
import { Syringe, X } from "lucide-react";
import { getVaccinationAdherence } from "@/lib/api/server";
import type { AdherenceRow, ProcessIntegrityEvidence, ProcessIntegritySeverity, WorkState } from "@/lib/api/server";
import { copy, optionalCopy, optionGroup, optionLabel, optionTone, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import { SEVERITY_ORDER, WORK_STATE_ORDER, type Tone } from "./process-integrity";
import { ClipText, Tag } from "@/components/ui-primitives";
import { VaccinationFilterButton, VaccinationTablePager, type VaccinationPageSize } from "@/features/preventive-care-vaccination";

type Tone4 = "ok" | "warn" | "dng" | "info" | "mut";
const accentVar: Record<Tone4, string> = {
  ok: "var(--ok)",
  warn: "var(--amber)",
  dng: "var(--danger)",
  info: "var(--info)",
  mut: "var(--line)",
};

function Kpi({ label, value, sub, tone = "mut" }: { label: string; value: React.ReactNode; sub?: string; tone?: Tone4 }) {
  return (
    <div className="kpi">
      <span className="acc" style={{ background: accentVar[tone] }} />
      <div className="lab">{label}</div>
      <div className="val">{value}</div>
      {sub ? <div className="dl muted">{sub}</div> : null}
    </div>
  );
}

function ownerOf(pageContract: AdminUiPageContract, row: AdherenceRow): string {
  return row.owner?.operator_name ?? row.owner?.park_head_name ?? copy(pageContract, "label.unassigned");
}

function adherenceSubtitle(pageContract: AdminUiPageContract): string {
  return pageContract.subtitle.replace("evidence, owner, and next action", "evidence, owner chain, and next action");
}

function adherenceLedgerLabels(pageContract: AdminUiPageContract): string[] {
  const labels = [...tableLabels(pageContract, "adherence-ledger")];
  if ((labels[4] || "").toLowerCase() === "owner") {
    labels[4] = `${labels[4]} chain`.toUpperCase();
  }
  return labels;
}

function copyOr(pageContract: AdminUiPageContract, key: string, fallback: string): string {
  return optionalCopy(pageContract, key) ?? fallback;
}

function backendPage(
  sp: RouteSearchParams,
  prefix: string,
  pageSizeOptions: readonly number[],
  fallbackPageSize: VaccinationPageSize,
): { page: number; pageSize: VaccinationPageSize; offset: number } {
  const requestedSize = Number(one(sp, `${prefix}_limit`));
  const pageSize = pageSizeOptions.includes(requestedSize) ? requestedSize : fallbackPageSize;
  const requestedPage = Number(one(sp, `${prefix}_page`));
  const page = Number.isInteger(requestedPage) && requestedPage > 0 ? requestedPage : 1;
  return { page, pageSize, offset: (page - 1) * pageSize };
}

function pageResult<T>(items: T[], total: number, page: number, pageSize: VaccinationPageSize) {
  const start = total === 0 ? 0 : (page - 1) * pageSize + 1;
  const end = total === 0 ? 0 : Math.min(total, (page - 1) * pageSize + items.length);
  return { items, page, pageSize, total, start, end };
}

function gapLabel(pageContract: AdminUiPageContract, row: AdherenceRow): string {
  switch (row.gap) {
    case "owner_missing":
      return copyOr(pageContract, "gap.owner_missing", "owner chain missing");
    case "proof_missing":
      return copyOr(pageContract, "gap.proof_missing", "proof missing");
    case "verification_pending":
      return copyOr(pageContract, "gap.verification_pending", "verification pending");
    case "deferred_explained":
      return copyOr(pageContract, "gap.deferred_explained", "deferred / explained");
    case "proof_rejected":
      return copyOr(pageContract, "gap.proof_rejected", "proof rejected");
    case "missed":
      return copyOr(pageContract, "gap.missed", "missed");
    case "blocked":
      return copyOr(pageContract, "gap.blocked", "blocked");
    case "overdue":
      return copyOr(pageContract, "gap.overdue", "overdue");
    default:
      return row.gap.replaceAll("_", " ");
  }
}

function EvidenceCell({ evidence, pageContract }: { evidence: ProcessIntegrityEvidence; pageContract: AdminUiPageContract }) {
  if (evidence.latest_rejection_reason) {
    return <Tag tone="dng" title={evidence.latest_rejection_reason}>{copy(pageContract, "label.rejected")}</Tag>;
  }
  if (evidence.evidence_count > 0) {
    return (
      <Tag tone="ok" title={evidence.audit_ref ?? undefined}>
        {evidence.evidence_count} {copy(pageContract, evidence.evidence_count === 1 ? "label.proof_singular" : "label.proof_plural")}
      </Tag>
    );
  }
  return <span className="muted">—</span>;
}

export async function ProtocolAdherencePage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const severityFilter = (SEVERITY_ORDER.find((s) => s === one(sp, "severity")) ?? "all") as ProcessIntegritySeverity | "all";
  const workStateOptions = optionGroup(pageContract, "work_state_filter_chips");
  const workStateParam = one(sp, "state");
  const workStateFilter = (workStateOptions.some((option) => option.key === workStateParam) ? workStateParam : "all") as WorkState | "all";
  const scope = parseScope(sp);
  const { parkId, asOf } = backendScope(scope);
  const pageSizeOptions = tablePageSizes(pageContract, "adherence-ledger");
  const requestedPage = backendPage(sp, "adh", pageSizeOptions, 10);

  const result = await getVaccinationAdherence({
    parkId,
    asOf,
    workState: workStateFilter === "all" ? undefined : workStateFilter,
    severity: severityFilter === "all" ? undefined : severityFilter,
    limit: requestedPage.pageSize,
    offset: requestedPage.offset,
  });

  const summary = result.ok ? result.data.summary : null;
  const rows: AdherenceRow[] = result.ok ? result.data.rows : [];
  const hasLedgerFilters = severityFilter !== "all" || workStateFilter !== "all";
  const paged = pageResult(rows, result.ok ? result.data.total_count : 0, requestedPage.page, requestedPage.pageSize);
  const ledgerLabels = adherenceLedgerLabels(pageContract);
  const selectedRowId = one(sp, "adh_row");
  const selectedRow = selectedRowId ? rows.find((row) => row.row_id === selectedRowId) : undefined;

  // Filter links preserve the full top-bar scope (scopeHref) + the page severity filter.
  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref("/protocol-adherence", scope, {}, {
      severity: severityFilter,
      state: workStateFilter,
      adh_page: String(paged.page),
      adh_limit: String(paged.pageSize),
      ...overrides,
    });
  }
  function pagerHref(page: number): string {
    return hrefWith({ adh_page: String(page) });
  }
  function pageSizeHref(pageSize: VaccinationPageSize): string {
    return hrefWith({ adh_page: "1", adh_limit: String(pageSize) });
  }
  const closeDrawerHref = hrefWith({ adh_row: undefined });
  const rowDrawerHref = (row: AdherenceRow) => hrefWith({ adh_row: row.row_id });
  const workflowHref = (row: AdherenceRow) =>
    scopeHref(`/workflows/${encodeURIComponent(row.row_id)}`, scope, {}, { from: "protocol-adherence" });

  return (
    <div className="screen on">
	      <div className="phead">
	        <div>
	          <h1>{pageContract.title}</h1>
	          <div className="sub">{adherenceSubtitle(pageContract)}</div>
	        </div>
	      </div>

      {/* Mock KPI row, from the real adherence summary. Park/date scope lives in the top bar only. */}
      <div className="grid g4" style={{ marginBottom: 14 }}>
        <Kpi
	          label={copy(pageContract, "label.overall_adherence")}
	          value={summary ? `${Math.round(summary.adherence_percent)}%` : "n/a"}
	          sub={copy(pageContract, "label.on_time_correct")}
	          tone={summary ? (summary.adherence_percent >= 90 ? "ok" : summary.adherence_percent >= 70 ? "warn" : "dng") : "mut"}
	        />
	        <Kpi label={copy(pageContract, "label.open_process_gaps")} value={summary ? summary.open_gap_count : "n/a"} sub={copy(pageContract, "label.across_rules")} tone={summary && summary.open_gap_count > 0 ? "warn" : "mut"} />
	        <Kpi label={copy(pageContract, "label.deferred_explained")} value={summary ? summary.deferred_count : "n/a"} sub={copy(pageContract, "label.deferred_scope")} tone="mut" />
	        <Kpi label={copy(pageContract, "label.on_track")} value={summary ? summary.process_intact_count : "n/a"} sub={summary ? `${summary.completed_count}/${summary.expected_count} ${copy(pageContract, "label.done_suffix")}` : copy(pageContract, "label.obligations")} tone="ok" />
      </div>

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
      ) : null}

      {/* Severity + work-state filters (server-side). */}
	      <div className="chipset" style={{ marginBottom: 14 }}>
	        <Link href={hrefWith({ severity: "all", adh_page: "1" })} replace scroll={false} className={`chip${severityFilter === "all" ? " on" : ""}`}>
	          {copy(pageContract, "label.all_severity")}
	        </Link>
	        {SEVERITY_ORDER.map((s) => (
	          <Link key={s} href={hrefWith({ severity: s, adh_page: "1" })} replace scroll={false} className={`chip${severityFilter === s ? " on" : ""}`}>
	            {optionLabel(pageContract, "severity_chips", s)}
	          </Link>
	        ))}
      </div>
      <div className="chipset" style={{ marginBottom: 14 }}>
        <Link href={hrefWith({ state: "all", adh_page: "1" })} replace scroll={false} className={`chip${workStateFilter === "all" ? " on" : ""}`}>
          {copy(pageContract, "label.all_states")}
        </Link>
        {WORK_STATE_ORDER.map((state) => (
          <Link key={state} href={hrefWith({ state, adh_page: "1" })} replace scroll={false} className={`chip${workStateFilter === state ? " on" : ""}`}>
            {optionLabel(pageContract, "work_state_filter_chips", state)}
          </Link>
        ))}
      </div>

      <section className="card">
        <div className="hd">
          <Syringe className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
	          <h3>{copy(pageContract, "section.ledger.title")}</h3>
	          {summary ? <Tag tone={summary.adherence_percent >= 90 ? "ok" : "warn"}>{Math.round(summary.adherence_percent)}% adherence</Tag> : null}
	          <div className="sp" style={{ flex: 1 }} />
	          <span className="muted small">{copy(pageContract, "section.ledger.note")}</span>
	        </div>
	        <div className="tbar">
	          <VaccinationFilterButton
	            pageContract={pageContract}
	            title={copy(pageContract, "filter.drawer.title")}
	            searchReason={copy(pageContract, "filter.search_reason")}
	            filterReason={copy(pageContract, "filter.reason")}
	            rowsLabel={`${paged.start}-${paged.end} of ${paged.total} rows · ${copy(pageContract, "filter.rows_suffix")}`}
	            actionHref={scopeHref("/action-center", scope)}
	            actionLabel={copy(pageContract, "action.open_action_center")}
	            facets={ledgerLabels}
	          />
          <span className="muted small">
            {paged.start}-{paged.end} of {paged.total} rows
          </span>
	          <span className="muted small">{copy(pageContract, "filter.click_row")}</span>
	        </div>
	        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.ledger.aria")}>
          <table className="table-fixed adherence-table">
            <colgroup>
              <col style={{ width: "36%" }} />
              <col style={{ width: "12%" }} />
              <col style={{ width: "12%" }} />
              <col style={{ width: "9%" }} />
              <col style={{ width: "10%" }} />
              <col style={{ width: "13%" }} />
              <col style={{ width: "8%" }} />
            </colgroup>
            <thead>
              <tr>
	                {ledgerLabels.map((c) => (
	                  <th key={c}>{c}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {paged.total === 0 ? (
                <tr>
	                  <td colSpan={ledgerLabels.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
	                      {result.ok
	                        ? hasLedgerFilters
	                          ? copy(pageContract, "empty.ledger_filtered")
	                          : copy(pageContract, "empty.ledger_detail")
	                        : copy(pageContract, "empty.unavailable")}
                    </div>
                  </td>
                </tr>
              ) : (
                paged.items.map((row) => {
                  const href = rowDrawerHref(row);
                  return (
                    <tr key={row.row_id}>
                      <td>
                        <Link href={href} className="celllink" scroll={false} title={row.expected}>
                          <ClipText title={row.expected} className="strong">
                            {row.expected}
                          </ClipText>
                        </Link>
                      </td>
                      <td className="muted">
                        <Link href={href} className="celllink" scroll={false} title={row.actual}>
                          <ClipText title={row.actual}>{row.actual}</ClipText>
                        </Link>
                      </td>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>
	                          <Tag tone={optionTone(pageContract, "work_state_filter_chips", row.work_state) as Tone}>{gapLabel(pageContract, row)}</Tag>
                        </Link>
                      </td>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>
	                          <Tag tone={optionTone(pageContract, "severity_chips", row.severity) as Tone}>{optionLabel(pageContract, "severity_chips", row.severity)}</Tag>
                        </Link>
                      </td>
                      <td className="muted">
	                        <Link href={href} className="celllink" scroll={false} title={ownerOf(pageContract, row)}>
	                          <ClipText title={ownerOf(pageContract, row)}>{ownerOf(pageContract, row)}</ClipText>
                        </Link>
                      </td>
                      <td>
                        <Link href={href} className="celllink" scroll={false} title={row.next_action}>
                          <ClipText title={row.next_action} className="lk small">
                            {row.next_action} →
                          </ClipText>
                        </Link>
                      </td>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>
	                          <EvidenceCell evidence={row.evidence} pageContract={pageContract} />
                        </Link>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
        <VaccinationTablePager
          pageContract={pageContract}
          pageSizeOptions={pageSizeOptions}
          page={paged.page}
          pageSize={paged.pageSize}
          total={paged.total}
          start={paged.start}
          end={paged.end}
	          noun={copy(pageContract, "table.ledger.noun")}
          hrefForPage={pagerHref}
          hrefForPageSize={pageSizeHref}
        />
      </section>

      <div className="note" style={{ marginTop: 14 }}>
	        {copy(pageContract, "note.computation")}{" "}
	        <Link href="/config?category=vaccination" className="lk">
	          {copy(pageContract, "action.open_config")}
	        </Link>{" "}
	        {copy(pageContract, "note.computation.joiner")}{" "}
	        <Link href="/sops" className="lk">
	          {copy(pageContract, "action.open_sops")}
	        </Link>
	        {copy(pageContract, "note.computation.tail")}
      </div>

      {selectedRow ? (
        <AdherenceRecordDrawer
          row={selectedRow}
          closeHref={closeDrawerHref}
          workflowHref={workflowHref(selectedRow)}
	          actionCenterHref={scopeHref("/action-center", scope, {}, { ac_row: selectedRow.row_id })}
	          pageContract={pageContract}
	        />
      ) : null}
    </div>
  );
}

// Row-click RECORD drawer (mock #recordDrawer anatomy): the adherence row's Expected → Actual → Gap →
// Severity → Owner → Next action → Evidence as a 2-col metagrid, with links out to the act surfaces.
// Adherence is a computed read projection — there is no Edit/Status/Void here; the real actions live in
// the Workflow record and Action Center, which this drawer links to.
function AdherenceRecordDrawer({
  row,
  closeHref,
  workflowHref,
  actionCenterHref,
  pageContract,
}: {
  row: AdherenceRow;
  closeHref: string;
  workflowHref: string;
  actionCenterHref: string;
  pageContract: AdminUiPageContract;
}) {
  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label={copy(pageContract, "drawer.record.close_label")} scroll={false} />
      <aside className="drawer on" aria-label={copy(pageContract, "drawer.record.aria")}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <Syringe className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "drawer.record.eyebrow")}</div>
            <h2>{row.expected}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label={copy(pageContract, "drawer.record.close_label")} scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          <div className="metagrid">
            <div>
              <div className="k">{adherenceLedgerLabels(pageContract)[0]}</div>
              <div className="v">{row.expected}</div>
            </div>
            <div>
              <div className="k">{adherenceLedgerLabels(pageContract)[1]}</div>
              <div className="v">{row.actual}</div>
            </div>
            <div>
              <div className="k">{adherenceLedgerLabels(pageContract)[2]}</div>
              <div className="v">
                <Tag tone={optionTone(pageContract, "work_state_filter_chips", row.work_state) as Tone}>{gapLabel(pageContract, row)}</Tag>
              </div>
            </div>
            <div>
              <div className="k">{adherenceLedgerLabels(pageContract)[3]}</div>
              <div className="v">
                <Tag tone={optionTone(pageContract, "severity_chips", row.severity) as Tone}>{optionLabel(pageContract, "severity_chips", row.severity)}</Tag>
              </div>
            </div>
            <div>
              <div className="k">{adherenceLedgerLabels(pageContract)[4]}</div>
              <div className="v">{ownerOf(pageContract, row)}</div>
            </div>
            <div>
              <div className="k">{adherenceLedgerLabels(pageContract)[5]}</div>
              <div className="v">{row.next_action}</div>
            </div>
            <div>
              <div className="k">{adherenceLedgerLabels(pageContract)[6]}</div>
              <div className="v">
                <EvidenceCell evidence={row.evidence} pageContract={pageContract} />
              </div>
            </div>
          </div>
          <div className="note" style={{ marginTop: 14 }}>{copy(pageContract, "drawer.record.note")}</div>
        </div>
        <div className="df">
          <Link href={workflowHref} className="btn p">
            {row.next_action}
          </Link>
          <Link href={actionCenterHref} className="btn">
            {copy(pageContract, "action.open_action_center")}
          </Link>
          <Link href={workflowHref} className="btn">
            {copy(pageContract, "action.workflow_record")}
          </Link>
          <Link href={closeHref} replace className="btn" scroll={false}>
            {copy(pageContract, "action.close")}
          </Link>
        </div>
      </aside>
    </>
  );
}
