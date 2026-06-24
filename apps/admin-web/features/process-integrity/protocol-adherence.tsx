import Link from "next/link";
import { Syringe } from "lucide-react";
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
  const { parkId } = backendScope(scope);

  const result = await getVaccinationAdherence({
    parkId,
    severity: severityFilter === "all" ? undefined : severityFilter,
    limit: 200,
  });

  const summary = result.ok ? result.data.summary : null;
  const rows: AdherenceRow[] = result.ok ? result.data.rows : [];

  // Filter links preserve the full top-bar scope (scopeHref) + the page severity filter.
  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref("/protocol-adherence", scope, {}, { severity: severityFilter, ...overrides });
  }

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
        <Link href={hrefWith({ severity: "all" })} className={`chip${severityFilter === "all" ? " on" : ""}`}>
          All severity
        </Link>
        {SEVERITY_ORDER.map((s) => (
          <Link key={s} href={hrefWith({ severity: s })} className={`chip${severityFilter === s ? " on" : ""}`}>
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
                rows.map((row) => (
                  <tr key={row.row_id}>
                    <td>
                      <b>{row.expected}</b>
                    </td>
                    <td className="muted">{row.actual}</td>
                    <td>
                      <Tag tone={WORK_STATE_META[row.work_state].tone}>{row.gap}</Tag>
                    </td>
                    <td>
                      <Tag tone={SEVERITY_META[row.severity].tone}>{SEVERITY_META[row.severity].label}</Tag>
                    </td>
                    <td className="muted">{ownerOf(row)}</td>
                    <td>
                      <Link href={`/workflows/${encodeURIComponent(row.row_id)}`} className="lk small">
                        {row.next_action}
                      </Link>
                    </td>
                    <td>
                      <EvidenceCell evidence={row.evidence} />
                    </td>
                  </tr>
                ))
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
    </div>
  );
}
