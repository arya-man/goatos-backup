import Link from "next/link";
import { redirect } from "next/navigation";
import { AlertTriangle, CheckCircle2, MapPin, ShieldCheck, X } from "lucide-react";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, getVaccinationControlTower } from "@/lib/api/server";
import type { ControlTowerAlert, ProcessIntegritySeverity, WorkState } from "@/lib/api/server";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import { Tag } from "@/components/ui-primitives";
import { copy, optionGroup, optionLabel, optionTone, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { VaccinationFilterButton, VisibleTableSearch, paginateRows, VaccinationTablePager, type VaccinationPageSize } from "@/features/preventive-care-vaccination";
import { SEVERITY_ORDER, WORK_STATE_ORDER, type Tone } from "@/features/process-integrity/process-integrity";

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
function ownerOf(alert: ControlTowerAlert, unassignedLabel: string): string {
  return alert.owner?.operator_name ?? alert.owner?.park_head_name ?? unassignedLabel;
}

function contractTone(pageContract: AdminUiPageContract, groupId: string, key: string): Tone {
  return optionTone(pageContract, groupId, key) as Tone;
}

export async function ControlTowerPage({ searchParams, pageContract }: { searchParams?: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const sp = searchParams ?? {};
  // Control Tower honors the top-bar scope via the shared contract (park is the backend-safe UUID).
  const scope = parseScope(sp);
  const { parkId, asOf } = backendScope(scope);
  const result = await getVaccinationControlTower({ parkId, asOf, limit: 200 });
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const summary = result.ok ? result.data.summary : null;
  const severityOptions = optionGroup(pageContract, "severity_chips");
  const workStateOptions = optionGroup(pageContract, "work_state_filter_chips");
  const severityRank = new Map(severityOptions.map((option, index) => [option.key, index]));
  // Most-broken first.
  const alerts: ControlTowerAlert[] = result.ok
    ? [...result.data.alerts].sort((a, b) => (severityRank.get(a.severity) ?? 999) - (severityRank.get(b.severity) ?? 999))
    : [];
  const severityParam = one(sp, "ct_severity");
  const severityFilter = (severityOptions.some((option) => option.key === severityParam) ? severityParam : "all") as ProcessIntegritySeverity | "all";
  const stateParam = one(sp, "ct_state");
  const stateFilter = (workStateOptions.some((option) => option.key === stateParam) ? stateParam : "all") as WorkState | "all";
  const filteredAlerts = alerts.filter(
    (alert) =>
      (severityFilter === "all" || alert.severity === severityFilter) &&
      (stateFilter === "all" || alert.work_state === stateFilter),
  );
  const alertStateSet = new Set(alerts.map((alert) => alert.work_state));
  const visibleWorkStates = WORK_STATE_ORDER.filter((state) => alertStateSet.has(state) || stateFilter === state);

  const processTone: Tone4 = !summary ? "mut" : !summary.process_intact ? (summary.critical_count > 0 ? "dng" : "warn") : "ok";
  const processLabel = !summary
    ? "n/a"
    : summary.process_intact
      ? copy(pageContract, "label.process_intact")
      : summary.critical_count > 0
        ? copy(pageContract, "label.process_not_intact")
        : copy(pageContract, "label.process_at_risk");
  const band = filteredAlerts.slice(0, 5);
  const openGapLabels = tableLabels(pageContract, "open-gaps");
  const pageSizeOptions = tablePageSizes(pageContract, "open-gaps");
  const paged = paginateRows(filteredAlerts, sp, "ct", 10, pageSizeOptions);
  const ownerUnassignedLabel = copy(pageContract, "label.owner_unassigned");
  const selectedAlertId = one(sp, "ct_alert");
  const selectedAlert = selectedAlertId ? alerts.find((alert) => alert.row_id === selectedAlertId) : undefined;

  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref("/", scope, {}, {
      ct_severity: severityFilter,
      ct_state: stateFilter,
      ct_page: String(paged.page),
      ct_limit: String(paged.pageSize),
      ...overrides,
    });
  }
  function pagerHref(page: number): string {
    return hrefWith({ ct_page: String(page) });
  }
  function pageSizeHref(pageSize: VaccinationPageSize): string {
    return hrefWith({ ct_page: "1", ct_limit: String(pageSize) });
  }

  const alertDrawerHref = (alert: ControlTowerAlert) => hrefWith({ ct_alert: alert.row_id });
  const closeDrawerHref = hrefWith({ ct_alert: undefined });
  const workflowRecordHref = (alert: ControlTowerAlert) =>
    scopeHref(`/workflows/${encodeURIComponent(alert.row_id)}`, scope, {}, { from: "control-tower" });
  const actionCenterHref = (alert: ControlTowerAlert) => scopeHref("/action-center", scope, {}, { ac_row: alert.row_id });

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <h1>{pageContract.title}</h1>
          <div className="sub">{pageContract.subtitle}</div>
        </div>
      </div>

      {/* Process-integrity KPIs only — no census/count totals (Counts is a separate vertical). */}
      <div className="grid g4" style={{ marginBottom: 16 }}>
        <Kpi
          label={copy(pageContract, "kpi.process")}
          value={processLabel}
          sub={summary ? pageContract.subtitle : copy(pageContract, "state.unavailable")}
          tone={processTone}
          icon={processTone === "ok" ? <CheckCircle2 className="ic" /> : <AlertTriangle className="ic" />}
        />
        <Kpi label={copy(pageContract, "kpi.critical")} value={summary ? fmtInt(summary.critical_count) : "n/a"} sub={copy(pageContract, "label.process_not_intact")} tone={summary && summary.critical_count > 0 ? "dng" : "mut"} />
        <Kpi label={copy(pageContract, "kpi.open_gaps")} value={summary ? fmtInt(summary.warning_count) : "n/a"} sub={copy(pageContract, "label.process_at_risk")} tone={summary && summary.warning_count > 0 ? "warn" : "mut"} />
        <Kpi
          label={copy(pageContract, "kpi.evidence")}
          value={summary ? fmtInt(summary.verification_backlog) : "n/a"}
          sub={openGapLabels[4]}
          tone={summary && summary.verification_backlog > 0 ? "info" : "mut"}
          icon={<ShieldCheck className="ic" />}
        />
      </div>

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 16 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
      ) : null}

      <div className="chipset" style={{ marginBottom: 8 }}>
        <Link href={hrefWith({ ct_severity: "all", ct_page: "1" })} replace scroll={false} className={`chip${severityFilter === "all" ? " on" : ""}`}>
          {copy(pageContract, "label.all_severity")}
        </Link>
        {SEVERITY_ORDER.map((severity) => (
          <Link
            key={severity}
            href={hrefWith({ ct_severity: severity, ct_page: "1" })}
            replace
            scroll={false}
            className={`chip${severityFilter === severity ? " on" : ""}`}
          >
            {optionLabel(pageContract, "severity_chips", severity)}
          </Link>
        ))}
      </div>

      <div className="chipset" style={{ marginBottom: 16 }}>
        <Link href={hrefWith({ ct_state: "all", ct_page: "1" })} replace scroll={false} className={`chip${stateFilter === "all" ? " on" : ""}`}>
          {copy(pageContract, "label.all_states")}
        </Link>
        {visibleWorkStates.map((state) => (
          <Link
            key={state}
            href={hrefWith({ ct_state: state, ct_page: "1" })}
            replace
            scroll={false}
            className={`chip${stateFilter === state ? " on" : ""}`}
          >
            {optionLabel(pageContract, "work_state_filter_chips", state)}
          </Link>
        ))}
      </div>

      {/* Config / SOP authority gap — server-counted (config_or_sop_blockers). */}
      {summary && summary.config_or_sop_blockers > 0 ? (
        <div className="alert warn" style={{ marginBottom: 16 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>
            <b>
              {summary.config_or_sop_blockers} {copy(pageContract, summary.config_or_sop_blockers === 1 ? "alert.config_sop.singular" : "alert.config_sop.plural")} {copy(pageContract, "alert.config_sop.action_required")}
            </b>{" "}
            {copy(pageContract, "alert.config_sop.body_prefix")}{" "}
            <Link href="/config?category=vaccination" className="lk">
              {copy(pageContract, "alert.config_sop.config_label")}
            </Link>{" "}
            {copy(pageContract, "alert.config_sop.joiner")}{" "}
            <Link href="/sops" className="lk">
              {copy(pageContract, "alert.config_sop.sops_label")}
            </Link>
            {copy(pageContract, "alert.config_sop.body_suffix")}
          </div>
        </div>
      ) : null}

      {/* Critical alert band — top broken / at-risk vaccination process only. */}
      <section className="card" style={{ marginBottom: 16, borderColor: "color-mix(in srgb,var(--danger) 28%,var(--line))" }}>
        <div className="hd">
          <AlertTriangle className="ic" style={{ color: "var(--danger)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.critical_alerts.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          <Tag tone={summary && summary.critical_count > 0 ? "dng" : summary && summary.warning_count > 0 ? "warn" : "mut"}>
            {summary ? summary.critical_count : 0} {copy(pageContract, "label.critical")} · {summary ? summary.warning_count : 0} {copy(pageContract, "label.at_risk")}
          </Tag>
        </div>
        <div className="bd feed">
          {band.length === 0 ? (
            <div className="fitem" style={{ cursor: "default" }}>
              <span className="fic" style={{ background: "var(--okx)", color: "var(--brand-d)" }}>
                <CheckCircle2 className="ic" />
              </span>
              <div className="tx">
                <b>{result.ok ? copy(pageContract, "empty.critical_ok_title") : copy(pageContract, "empty.critical_unavailable")}</b>
                <div className="mt">
                  {result.ok
                    ? copy(pageContract, "empty.critical_ok_body")
                    : copy(pageContract, "empty.resolve_error")}
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
                  aria-label={`${copy(pageContract, "action.open_alert_for")} ${alert.title}`}
                >
                  <span className="fic" style={{ background: fill.bg, color: fill.fg }}>
                    <MapPin className="ic" />
                  </span>
                  <div className="tx">
                    <b>{alert.title}</b>
                    <div className="mt">
                      {alert.detail} · {ownerOf(alert, ownerUnassignedLabel)} → {alert.next_action}
                    </div>
                  </div>
                  <Tag tone={contractTone(pageContract, "severity_chips", alert.severity)}>{optionLabel(pageContract, "severity_chips", alert.severity)}</Tag>
                </Link>
              );
            })
          )}
        </div>
      </section>

      {/* Open gaps table — every alert row, with owner + next action. */}
      <section className="card" style={{ marginBottom: 16 }} data-filter-scope>
        <div className="hd">
          <AlertTriangle className="ic" style={{ color: "var(--amber)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.open_gaps.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          {summary && summary.owner_missing_count > 0 ? <Tag tone="dng">{summary.owner_missing_count} {copy(pageContract, "label.owner_missing")}</Tag> : null}
        </div>
        <div className="tbar">
          <VisibleTableSearch pageContract={pageContract} label={copy(pageContract, "filter.search_label")} />
          <VaccinationFilterButton
            pageContract={pageContract}
            title={copy(pageContract, "filter.drawer.title")}
            searchReason={copy(pageContract, "filter.search_reason")}
            filterReason={copy(pageContract, "filter.reason")}
            rowsLabel={`${paged.start}-${paged.end} of ${filteredAlerts.length} ${copy(pageContract, "filter.rows_suffix")}`}
            actionHref={scopeHref("/action-center", scope)}
            actionLabel={copy(pageContract, "action.open_action_center")}
            facets={openGapLabels}
          />
          <span className="muted small">
            {paged.start}-{paged.end} of {filteredAlerts.length} {copy(pageContract, "table.open_gaps.noun")}s
          </span>
          <span className="muted small">{copy(pageContract, "filter.click_row")}</span>
        </div>
        {filteredAlerts.length === 0 ? (
          <div className="bd">
            <p className="muted small" style={{ margin: 0, lineHeight: 1.6 }}>
              {result.ok ? (alerts.length === 0 ? copy(pageContract, "empty.open_gaps_detail") : copy(pageContract, "empty.open_gaps_filtered")) : copy(pageContract, "empty.open_gaps_unavailable")}
            </p>
          </div>
        ) : (
          <div style={{ overflowX: "auto", padding: 0 }} tabIndex={0} role="group" aria-label={copy(pageContract, "table.open_gaps.aria")}>
            <table>
              <thead>
                <tr>
                  {openGapLabels.map((label) => (
                    <th key={label}>{label}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {paged.items.map((alert) => (
                  <tr key={alert.row_id} data-filter-row>
                    <td>
                      <Link href={alertDrawerHref(alert)} className="celllink" scroll={false}>
                        <Tag tone={contractTone(pageContract, "work_state_filter_chips", alert.work_state)}>{optionLabel(pageContract, "work_state_filter_chips", alert.work_state)}</Tag>
                      </Link>
                    </td>
                    <td>
                      <Link href={alertDrawerHref(alert)} className="celllink" scroll={false}>
                        <Tag tone={contractTone(pageContract, "severity_chips", alert.severity)}>{optionLabel(pageContract, "severity_chips", alert.severity)}</Tag>
                      </Link>
                    </td>
                    <td className="muted">
                      <Link href={alertDrawerHref(alert)} className="celllink" scroll={false}>
                        {alert.detail}
                      </Link>
                    </td>
                    <td className="muted">
                      <Link href={alertDrawerHref(alert)} className="celllink" scroll={false}>
                        {ownerOf(alert, ownerUnassignedLabel)}
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
        <VaccinationTablePager
          pageContract={pageContract}
          pageSizeOptions={pageSizeOptions}
          page={paged.page}
          pageSize={paged.pageSize}
          total={paged.total}
          start={paged.start}
          end={paged.end}
          noun={copy(pageContract, "table.open_gaps.noun")}
          hrefForPage={pagerHref}
          hrefForPageSize={pageSizeHref}
        />
        <div className="bd" style={{ paddingTop: 12, display: "flex", gap: 14, flexWrap: "wrap" }}>
          <Link href={scopeHref("/action-center", scope)} className="lk small">
            {copy(pageContract, "link.action_center")}
          </Link>
          <Link href={scopeHref("/protocol-adherence", scope)} className="lk small">
            {copy(pageContract, "link.protocol_adherence")}
          </Link>
          <Link href={scopeHref("/workflows", scope)} className="lk small">
            {copy(pageContract, "link.workflows")}
          </Link>
          <Link href={scopeHref("/vaccination", scope)} className="lk small">
            {copy(pageContract, "link.vaccination_ops")}
          </Link>
          <Link href={`${scopeHref("/vaccination", scope)}#execution`} className="lk small">
            {copy(pageContract, "link.park_shed_execution")}
          </Link>
        </div>
      </section>
      {selectedAlert ? (
        <ControlTowerAlertDrawer
          alert={selectedAlert}
          pageContract={pageContract}
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
  pageContract,
  closeHref,
  actionCenterHref,
  workflowHref,
  adherenceHref,
  vaccinationHref,
}: {
  alert: ControlTowerAlert;
  pageContract: AdminUiPageContract;
  closeHref: string;
  actionCenterHref: string;
  workflowHref: string;
  adherenceHref: string;
  vaccinationHref: string;
}) {
  const fill = SEVERITY_FILL[alert.severity];
  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label={copy(pageContract, "drawer.alert.close_label")} scroll={false} />
      <aside className="drawer on" aria-label={copy(pageContract, "drawer.alert.aria")}>
        <div className="dh">
          <span className="fic" style={{ background: fill.bg, color: fill.fg }}>
            <AlertTriangle className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{pageContract.title}</div>
            <h2>{alert.title}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label={copy(pageContract, "drawer.alert.close_label")} scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          <div className="metagrid">
            <div>
              <div className="k">{copy(pageContract, "label.gap")}</div>
              <div className="v">
                <Tag tone={contractTone(pageContract, "work_state_filter_chips", alert.work_state)}>{optionLabel(pageContract, "work_state_filter_chips", alert.work_state)}</Tag>
              </div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "label.severity")}</div>
              <div className="v">
                <Tag tone={contractTone(pageContract, "severity_chips", alert.severity)}>{optionLabel(pageContract, "severity_chips", alert.severity)}</Tag>
              </div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "label.detail")}</div>
              <div className="v">{alert.detail}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "label.owner")}</div>
              <div className="v">{ownerOf(alert, copy(pageContract, "label.owner_unassigned"))}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "label.next_action")}</div>
              <div className="v">{alert.next_action}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "label.evidence")}</div>
              <div className="v">{alert.evidence_link || copy(pageContract, "label.not_ready")}</div>
            </div>
          </div>
          <div className="note" style={{ marginTop: 14 }}>
            {copy(pageContract, "drawer.alert.guidance")}
          </div>
        </div>
        <div className="df">
          <Link href={actionCenterHref} className="btn p">
            {copy(pageContract, "action.open_action_center")}
          </Link>
          <Link href={workflowHref} className="btn">
            {copy(pageContract, "action.open_workflow")}
          </Link>
          <Link href={adherenceHref} className="btn">
            {copy(pageContract, "action.open_adherence")}
          </Link>
          <Link href={vaccinationHref} className="btn">
            {copy(pageContract, "action.open_vaccination")}
          </Link>
        </div>
      </aside>
    </>
  );
}
