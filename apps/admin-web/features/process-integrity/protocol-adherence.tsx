import Link from "next/link";
import { Syringe, X } from "lucide-react";
import { getVaccinationAdherence } from "@/lib/api/server";
import type { AdherenceRow, ProcessIntegrityEvidence, ProcessIntegritySeverity } from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import { SEVERITY_META, SEVERITY_ORDER, WORK_STATE_META } from "./process-integrity";
import { Tag } from "@/components/ui-primitives";

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

const ADHERENCE_COLS = ["Expected", "Actual", "Gap", "Severity", "Owner", "Next action", "Evidence"];

function ownerOf(row: AdherenceRow): string {
  return row.owner?.operator_name ?? row.owner?.park_head_name ?? "unassigned";
}

function EvidenceCell({ evidence }: { evidence: ProcessIntegrityEvidence }) {
  if (evidence.latest_rejection_reason) {
    return <Tag tone="dng" title={evidence.latest_rejection_reason}>rejected</Tag>;
  }
  if (evidence.evidence_count > 0) {
    return (
      <Tag tone="ok" title={evidence.audit_ref ?? undefined}>
        {evidence.evidence_count} proof{evidence.evidence_count === 1 ? "" : "s"}
      </Tag>
    );
  }
  return <span className="muted">—</span>;
}

export async function ProtocolAdherencePage({ searchParams }: { searchParams?: RouteSearchParams }) {
  const sp = searchParams ?? {};
  const severityFilter = (SEVERITY_ORDER.find((s) => s === one(sp, "severity")) ?? "all") as ProcessIntegritySeverity | "all";
  const scope = parseScope(sp);
  const { parkId, asOf } = backendScope(scope);

  const result = await getVaccinationAdherence({
    parkId,
    asOf,
    severity: severityFilter === "all" ? undefined : severityFilter,
    limit: 200,
  });

  const summary = result.ok ? result.data.summary : null;
  const rows: AdherenceRow[] = result.ok ? result.data.rows : [];
  const selectedRowId = one(sp, "adh_row");
  const selectedRow = selectedRowId ? rows.find((row) => row.row_id === selectedRowId) : undefined;

  // Filter links preserve the full top-bar scope (scopeHref) + the page severity filter.
  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref("/protocol-adherence", scope, {}, { severity: severityFilter, ...overrides });
  }
  const closeDrawerHref = hrefWith({ adh_row: undefined });
  const rowDrawerHref = (row: AdherenceRow) => hrefWith({ adh_row: row.row_id });
  const workflowHref = (row: AdherenceRow) =>
    scopeHref(`/workflows/${encodeURIComponent(row.row_id)}`, scope, {}, { from: "protocol-adherence" });

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <h1>Protocol Adherence</h1>
          <div className="sub">
            <b>Is the agreed process being followed?</b> Expected → Actual → Gap → Severity → Owner → Next → Evidence.
            On-track obligations are summarized, not listed; gaps and deferred/explained rows surface here.
          </div>
        </div>
      </div>

      {/* Mock KPI row, from the real adherence summary. Park/date scope lives in the top bar only. */}
      <div className="grid g4" style={{ marginBottom: 14 }}>
        <Kpi
          label="Overall adherence"
          value={summary ? `${Math.round(summary.adherence_percent)}%` : "n/a"}
          sub="on-time + correct"
          tone={summary ? (summary.adherence_percent >= 90 ? "ok" : summary.adherence_percent >= 70 ? "warn" : "dng") : "mut"}
        />
        <Kpi label="Open process gaps" value={summary ? summary.open_gap_count : "n/a"} sub="across vaccination rules" tone={summary && summary.open_gap_count > 0 ? "warn" : "mut"} />
        <Kpi label="Deferred / explained" value={summary ? summary.deferred_count : "n/a"} sub="ICU / quarantine / sick" tone="mut" />
        <Kpi label="On-track (no action)" value={summary ? summary.process_intact_count : "n/a"} sub={summary ? `${summary.completed_count}/${summary.expected_count} done` : "obligations"} tone="ok" />
      </div>

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
      ) : null}

      {/* Severity filter (server-side). */}
      <div className="chipset" style={{ marginBottom: 14 }}>
        <Link href={hrefWith({ severity: "all" })} replace scroll={false} className={`chip${severityFilter === "all" ? " on" : ""}`}>
          All severity
        </Link>
        {SEVERITY_ORDER.map((s) => (
          <Link key={s} href={hrefWith({ severity: s })} replace scroll={false} className={`chip${severityFilter === s ? " on" : ""}`}>
            {SEVERITY_META[s].label}
          </Link>
        ))}
      </div>

      <section className="card">
        <div className="hd">
          <Syringe className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>Vaccination</h3>
          {summary ? <Tag tone={summary.adherence_percent >= 90 ? "ok" : "warn"}>{Math.round(summary.adherence_percent)}% adherence</Tag> : null}
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">expected vs actual + SOP proof</span>
        </div>
        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Vaccination adherence ledger">
          <table>
            <thead>
              <tr>
                {ADHERENCE_COLS.map((c) => (
                  <th key={c}>{c}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 ? (
                <tr>
                  <td colSpan={ADHERENCE_COLS.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {result.ok
                        ? "No open vaccination adherence gaps for this scope — every obligation is on track, deferred/explained, or none has been generated yet."
                        : "Adherence rows are unavailable until the service responds."}
                    </div>
                  </td>
                </tr>
              ) : (
                rows.map((row) => {
                  const href = rowDrawerHref(row);
                  return (
                    <tr key={row.row_id}>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>
                          <b>{row.expected}</b>
                        </Link>
                      </td>
                      <td className="muted">
                        <Link href={href} className="celllink" scroll={false}>
                          {row.actual}
                        </Link>
                      </td>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>
                          <Tag tone={WORK_STATE_META[row.work_state].tone}>{row.gap}</Tag>
                        </Link>
                      </td>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>
                          <Tag tone={SEVERITY_META[row.severity].tone}>{SEVERITY_META[row.severity].label}</Tag>
                        </Link>
                      </td>
                      <td className="muted">
                        <Link href={href} className="celllink" scroll={false}>
                          {ownerOf(row)}
                        </Link>
                      </td>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>
                          <span className="lk small">{row.next_action} →</span>
                        </Link>
                      </td>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>
                          <EvidenceCell evidence={row.evidence} />
                        </Link>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </section>

      <div className="note" style={{ marginTop: 14 }}>
        Adherence is computed from <b>published</b> rules vs actual SOP submission + proof. Set the process in{" "}
        <Link href="/config?category=vaccination" className="lk">
          Config — Protocol Rules
        </Link>{" "}
        and{" "}
        <Link href="/sops" className="lk">
          SOP Library
        </Link>
        ; deferred/explained rows stay visible here instead of silently disappearing.
      </div>

      {selectedRow ? (
        <AdherenceRecordDrawer
          row={selectedRow}
          closeHref={closeDrawerHref}
          workflowHref={workflowHref(selectedRow)}
          actionCenterHref={scopeHref("/action-center", scope, {}, { ac_row: selectedRow.row_id })}
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
}: {
  row: AdherenceRow;
  closeHref: string;
  workflowHref: string;
  actionCenterHref: string;
}) {
  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label="Close adherence record" scroll={false} />
      <aside className="drawer on" aria-label="Protocol adherence record">
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <Syringe className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">RECORD</div>
            <h2>{row.expected}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label="Close adherence record" scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          <div className="metagrid">
            <div>
              <div className="k">Expected</div>
              <div className="v">{row.expected}</div>
            </div>
            <div>
              <div className="k">Actual</div>
              <div className="v">{row.actual}</div>
            </div>
            <div>
              <div className="k">Gap</div>
              <div className="v">
                <Tag tone={WORK_STATE_META[row.work_state].tone}>{row.gap}</Tag>
              </div>
            </div>
            <div>
              <div className="k">Severity</div>
              <div className="v">
                <Tag tone={SEVERITY_META[row.severity].tone}>{SEVERITY_META[row.severity].label}</Tag>
              </div>
            </div>
            <div>
              <div className="k">Owner</div>
              <div className="v">{ownerOf(row)}</div>
            </div>
            <div>
              <div className="k">Next action</div>
              <div className="v">{row.next_action}</div>
            </div>
            <div>
              <div className="k">Evidence</div>
              <div className="v">
                <EvidenceCell evidence={row.evidence} />
              </div>
            </div>
          </div>
          <div className="note" style={{ marginTop: 14 }}>
            Read view — adherence is computed from published rules vs actual SOP submission + proof. Act on the real
            obligation from the Workflow record or the Action Center.
          </div>
        </div>
        <div className="df">
          <Link href={workflowHref} className="btn p">
            {row.next_action}
          </Link>
          <Link href={actionCenterHref} className="btn">
            Action Center
          </Link>
          <Link href={workflowHref} className="btn">
            Workflow record
          </Link>
          <Link href={closeHref} replace className="btn" scroll={false}>
            Close
          </Link>
        </div>
      </aside>
    </>
  );
}
