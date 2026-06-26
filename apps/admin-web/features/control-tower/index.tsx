import Link from "next/link";
import { redirect } from "next/navigation";
import { AlertTriangle, CheckCircle2, MapPin, ShieldCheck, X } from "lucide-react";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, getVaccinationControlTower } from "@/lib/api/server";
import type { ControlTowerAlert, ProcessIntegritySeverity } from "@/lib/api/server";
import { SEVERITY_META, SEVERITY_RANK, WORK_STATE_META } from "@/features/process-integrity";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import { Tag } from "@/components/ui-primitives";

// Severity tint for the alert-band icon chip.
const SEVERITY_FILL: Record<ProcessIntegritySeverity, { bg: string; fg: string }> = {
  broken: { bg: "var(--dangerx)", fg: "var(--danger)" },
  at_risk: { bg: "var(--warnx)", fg: "var(--warn)" },
  watch: { bg: "var(--infox)", fg: "var(--info)" },
  ok: { bg: "var(--okx)", fg: "var(--brand-d)" },
};

type Tone4 = "ok" | "warn" | "dng" | "info" | "mut";
const accentVar: Record<Tone4, string> = {
  ok: "var(--brand)",
  warn: "var(--amber)",
  dng: "var(--danger)",
  info: "var(--info)",
  mut: "var(--line)",
};

function Kpi({ label, value, sub, tone, icon }: { label: string; value: React.ReactNode; sub?: string; tone: Tone4; icon?: React.ReactNode }) {
  return (
    <div className="kpi">
      <span className="acc" style={{ background: accentVar[tone] }} />
      <div className="lab">
        {icon}
        {label}
      </div>
      <div className="val">{value}</div>
      {sub ? <div className="dl muted">{sub}</div> : null}
    </div>
  );
}

function fmtInt(n: number): string {
  return n.toLocaleString("en-IN");
}
function ownerOf(alert: ControlTowerAlert): string {
  return alert.owner?.operator_name ?? alert.owner?.park_head_name ?? "owner: unassigned";
}

export async function ControlTowerPage({ searchParams }: { searchParams?: RouteSearchParams }) {
  const sp = searchParams ?? {};
  // Control Tower honors the top-bar scope via the shared contract (park is the backend-safe UUID).
  const scope = parseScope(sp);
  const { parkId, asOf } = backendScope(scope);
  const result = await getVaccinationControlTower({ parkId, asOf, limit: 200 });
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const summary = result.ok ? result.data.summary : null;
  // Most-broken first.
  const alerts: ControlTowerAlert[] = result.ok
    ? [...result.data.alerts].sort((a, b) => SEVERITY_RANK[b.severity] - SEVERITY_RANK[a.severity])
    : [];

  const processTone: Tone4 = !summary ? "mut" : !summary.process_intact ? (summary.critical_count > 0 ? "dng" : "warn") : "ok";
  const processLabel = !summary ? "n/a" : summary.process_intact ? "Intact" : summary.critical_count > 0 ? "Not intact" : "At risk";
  const band = alerts.slice(0, 5);
  const selectedAlertId = one(sp, "ct_alert");
  const selectedAlert = selectedAlertId ? alerts.find((alert) => alert.row_id === selectedAlertId) : undefined;

  const alertDrawerHref = (alert: ControlTowerAlert) => scopeHref("/", scope, {}, { ct_alert: alert.row_id });
  const closeDrawerHref = scopeHref("/", scope);
  const workflowRecordHref = (alert: ControlTowerAlert) =>
    scopeHref(`/workflows/${encodeURIComponent(alert.row_id)}`, scope, {}, { from: "control-tower" });
  const actionCenterHref = (alert: ControlTowerAlert) => scopeHref("/action-center", scope, {}, { ac_row: alert.row_id });

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <h1>Control Tower</h1>
          <div className="sub">
            <b>Watch</b> — vaccination process integrity: gaps, owners, and next actions only.
          </div>
        </div>
      </div>

      {/* Process-integrity KPIs only — no census/count totals (Counts is a separate vertical). */}
      <div className="grid g4" style={{ marginBottom: 16 }}>
        <Kpi
          label="Process status"
          value={processLabel}
          sub={summary ? "vaccination process" : "control tower unavailable"}
          tone={processTone}
          icon={processTone === "ok" ? <CheckCircle2 className="ic" /> : <AlertTriangle className="ic" />}
        />
        <Kpi label="Critical gaps" value={summary ? fmtInt(summary.critical_count) : "n/a"} sub="broken process" tone={summary && summary.critical_count > 0 ? "dng" : "mut"} />
        <Kpi label="At-risk gaps" value={summary ? fmtInt(summary.warning_count) : "n/a"} sub="watch + at risk" tone={summary && summary.warning_count > 0 ? "warn" : "mut"} />
        <Kpi
          label="Verification backlog"
          value={summary ? fmtInt(summary.verification_backlog) : "n/a"}
          sub="proof awaiting review"
          tone={summary && summary.verification_backlog > 0 ? "info" : "mut"}
          icon={<ShieldCheck className="ic" />}
        />
      </div>

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 16 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
      ) : null}

      {/* Config / SOP authority gap — server-counted (config_or_sop_blockers). */}
      {summary && summary.config_or_sop_blockers > 0 ? (
        <div className="alert warn" style={{ marginBottom: 16 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>
            <b>
              {summary.config_or_sop_blockers} config / SOP blocker{summary.config_or_sop_blockers === 1 ? "" : "s"} — action
              required.
            </b>{" "}
            Vaccination obligations and proof need a published protocol + SOP. Resolve in{" "}
            <Link href="/config?category=vaccination" className="lk">
              Config
            </Link>{" "}
            and{" "}
            <Link href="/sops" className="lk">
              SOP Library
            </Link>
            .
          </div>
        </div>
      ) : null}

      {/* Critical alert band — top broken / at-risk vaccination process only. */}
      <section className="card" style={{ marginBottom: 16, borderColor: "color-mix(in srgb,var(--danger) 28%,var(--line))" }}>
        <div className="hd">
          <AlertTriangle className="ic" style={{ color: "var(--danger)" }} aria-hidden="true" />
          <h3>Critical vaccination alerts — broken or at-risk process</h3>
          <div className="sp" style={{ flex: 1 }} />
          <Tag tone={summary && summary.critical_count > 0 ? "dng" : summary && summary.warning_count > 0 ? "warn" : "mut"}>
            {summary ? summary.critical_count : 0} critical · {summary ? summary.warning_count : 0} at risk
          </Tag>
        </div>
        <div className="bd feed">
          {band.length === 0 ? (
            <div className="fitem" style={{ cursor: "default" }}>
              <span className="fic" style={{ background: "var(--okx)", color: "var(--brand-d)" }}>
                <CheckCircle2 className="ic" />
              </span>
              <div className="tx">
                <b>{result.ok ? "No broken or at-risk vaccination process" : "Vaccination process status unavailable"}</b>
                <div className="mt">
                  {result.ok
                    ? "Every vaccination obligation is on track. Open gaps appear here the moment severity rises."
                    : "Resolve the error above, then reload."}
                </div>
              </div>
            </div>
          ) : (
            band.map((alert) => {
              const fill = SEVERITY_FILL[alert.severity];
              return (
                <Link
                  key={alert.row_id}
                  href={alertDrawerHref(alert)}
                  className="fitem"
                  scroll={false}
                  aria-label={`Open Control Tower alert for ${alert.title}`}
                >
                  <span className="fic" style={{ background: fill.bg, color: fill.fg }}>
                    <MapPin className="ic" />
                  </span>
                  <div className="tx">
                    <b>{alert.title}</b>
                    <div className="mt">
                      {alert.detail} · {ownerOf(alert)} → {alert.next_action}
                    </div>
                  </div>
                  <Tag tone={SEVERITY_META[alert.severity].tone}>{SEVERITY_META[alert.severity].label}</Tag>
                </Link>
              );
            })
          )}
        </div>
      </section>

      {/* Open gaps table — every alert row, with owner + next action. */}
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <AlertTriangle className="ic" style={{ color: "var(--amber)" }} aria-hidden="true" />
          <h3>Open vaccination gaps — gap, severity, owner, next action</h3>
          <div className="sp" style={{ flex: 1 }} />
          {summary && summary.owner_missing_count > 0 ? <Tag tone="dng">{summary.owner_missing_count} owner-missing</Tag> : null}
        </div>
        {alerts.length === 0 ? (
          <div className="bd">
            <p className="muted small" style={{ margin: 0, lineHeight: 1.6 }}>
              {result.ok
                ? "No open gaps — every vaccination obligation is on track, or none has been generated yet. Publish a source-backed protocol in Config and a vaccination SOP to start generating obligations."
                : "Open gaps are unavailable until the Control Tower service responds."}
            </p>
          </div>
        ) : (
          <div style={{ overflowX: "auto", padding: 0 }} tabIndex={0} role="group" aria-label="Open vaccination gaps">
            <table>
              <thead>
                <tr>
                  <th>Gap</th>
                  <th>Severity</th>
                  <th>Detail</th>
                  <th>Owner</th>
                  <th>Next action</th>
                </tr>
              </thead>
              <tbody>
                {alerts.map((alert) => (
                  <tr key={alert.row_id}>
                    <td>
                      <Link href={alertDrawerHref(alert)} className="celllink" scroll={false}>
                        <Tag tone={WORK_STATE_META[alert.work_state].tone}>{WORK_STATE_META[alert.work_state].label}</Tag>
                      </Link>
                    </td>
                    <td>
                      <Link href={alertDrawerHref(alert)} className="celllink" scroll={false}>
                        <Tag tone={SEVERITY_META[alert.severity].tone}>{SEVERITY_META[alert.severity].label}</Tag>
                      </Link>
                    </td>
                    <td className="muted">
                      <Link href={alertDrawerHref(alert)} className="celllink" scroll={false}>
                        {alert.detail}
                      </Link>
                    </td>
                    <td className="muted">
                      <Link href={alertDrawerHref(alert)} className="celllink" scroll={false}>
                        {ownerOf(alert)}
                      </Link>
                    </td>
                    <td>
                      <Link href={alertDrawerHref(alert)} className="celllink" scroll={false}>
                        <span className="lk small">{alert.next_action} →</span>
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <div className="bd" style={{ paddingTop: 12, display: "flex", gap: 14, flexWrap: "wrap" }}>
          <Link href={scopeHref("/action-center", scope)} className="lk small">
            Action Center →
          </Link>
          <Link href={scopeHref("/protocol-adherence", scope)} className="lk small">
            Protocol Adherence ledger →
          </Link>
          <Link href={scopeHref("/workflows", scope)} className="lk small">
            Workflows →
          </Link>
          <Link href={scopeHref("/vaccination", scope)} className="lk small">
            PHC · Vaccination ops →
          </Link>
          <Link href={`${scopeHref("/vaccination", scope)}#execution`} className="lk small">
            Park/shed execution →
          </Link>
        </div>
      </section>
      {selectedAlert ? (
        <ControlTowerAlertDrawer
          alert={selectedAlert}
          closeHref={closeDrawerHref}
          actionCenterHref={actionCenterHref(selectedAlert)}
          workflowHref={workflowRecordHref(selectedAlert)}
          adherenceHref={scopeHref("/protocol-adherence", scope)}
          vaccinationHref={scopeHref("/vaccination", scope)}
        />
      ) : null}
    </div>
  );
}

function ControlTowerAlertDrawer({
  alert,
  closeHref,
  actionCenterHref,
  workflowHref,
  adherenceHref,
  vaccinationHref,
}: {
  alert: ControlTowerAlert;
  closeHref: string;
  actionCenterHref: string;
  workflowHref: string;
  adherenceHref: string;
  vaccinationHref: string;
}) {
  const fill = SEVERITY_FILL[alert.severity];
  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label="Close Control Tower alert" scroll={false} />
      <aside className="drawer on" aria-label="Control Tower alert">
        <div className="dh">
          <span className="fic" style={{ background: fill.bg, color: fill.fg }}>
            <AlertTriangle className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">CONTROL TOWER</div>
            <h2>{alert.title}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label="Close Control Tower alert" scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          <div className="metagrid">
            <div>
              <div className="k">Gap</div>
              <div className="v">
                <Tag tone={WORK_STATE_META[alert.work_state].tone}>{WORK_STATE_META[alert.work_state].label}</Tag>
              </div>
            </div>
            <div>
              <div className="k">Severity</div>
              <div className="v">
                <Tag tone={SEVERITY_META[alert.severity].tone}>{SEVERITY_META[alert.severity].label}</Tag>
              </div>
            </div>
            <div>
              <div className="k">Detail</div>
              <div className="v">{alert.detail}</div>
            </div>
            <div>
              <div className="k">Owner</div>
              <div className="v">{ownerOf(alert)}</div>
            </div>
            <div>
              <div className="k">Next action</div>
              <div className="v">{alert.next_action}</div>
            </div>
            <div>
              <div className="k">Evidence</div>
              <div className="v">{alert.evidence_link || "not ready"}</div>
            </div>
          </div>
          <div className="note" style={{ marginTop: 14 }}>
            Control Tower is the watch surface. Open the Action Center drawer to act on this obligation, or open the
            Workflow record to inspect the full chain.
          </div>
        </div>
        <div className="df">
          <Link href={actionCenterHref} className="btn p">
            Action Center
          </Link>
          <Link href={workflowHref} className="btn">
            Workflow
          </Link>
          <Link href={adherenceHref} className="btn">
            Adherence
          </Link>
          <Link href={vaccinationHref} className="btn">
            Vaccination
          </Link>
        </div>
      </aside>
    </>
  );
}
