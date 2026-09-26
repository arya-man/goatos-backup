import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { AlertTriangle, CheckCircle2, MapPin, ShieldCheck } from "lucide-react";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, getVaccinationControlTower } from "@/lib/api/server";
import type { ControlTowerAlert, ProcessIntegritySeverity, WorkState } from "@/lib/api/server";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import { Tag } from "@/components/ui-primitives";
import type { KitTone } from "@/lib/tone";
import { PageHeader } from "@/components/app/page-header";
import { KpiCard, KpiGrid } from "@/components/minimal/widgets";
import { AnimatedTabs, TabPanel } from "@/components/minimal/list/animated-tabs";
import { copy, optionGroup, optionLabel, optionTone, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { VaccinationFilterButton, VaccinationTablePager, type VaccinationPageSize } from "@/features/preventive-care-vaccination";
import { SEVERITY_ORDER, WORK_STATE_ORDER, type Tone } from "@/features/process-integrity";
import { ControlTowerLocalDrawer, type ControlTowerDrawerRecord } from "./control-tower-local-drawer";
import Alert from "@mui/material/Alert";

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

const CT_TONE: Record<Tone4, KitTone> = { ok: "success", warn: "warning", dng: "error", info: "info", mut: "neutral" };

function Kpi({ label, value, sub, tone, icon }: { label: string; value: React.ReactNode; sub?: string; tone: Tone4; icon?: React.ReactNode }) {
  return <KpiCard label={label} value={value} tone={CT_TONE[tone]} icon={icon} hint={sub} />;
}

function fmtInt(n: number): string {
  return n.toLocaleString("en-IN");
}

function ownerOf(alert: ControlTowerAlert, unassignedLabel: string): string {
  return alert.owner?.operator_name ?? alert.owner?.park_head_name ?? unassignedLabel;
}

function driveCapacityLabel(alert: ControlTowerAlert): string | null {
  switch (alert.drive_capacity_state) {
    case "over_cap_required": {
      const animals = alert.drive_animals_assigned ?? alert.drive_animals_required ?? 0;
      const slots = (alert.drive_available_operators ?? 0) * (alert.drive_operator_cap ?? 0);
      return `${animals.toLocaleString("en-IN")} animals over ${slots.toLocaleString("en-IN")} slots`;
    }
    case "medical_defer":
      return alert.drive_medical_defer_reason ? `medical defer: ${alert.drive_medical_defer_reason}` : "medical defer";
    case "terminal_animal_closed":
      return alert.drive_medical_defer_reason ? `terminal closed: ${alert.drive_medical_defer_reason}` : "terminal animal closed";
    default:
      return null;
  }
}

function contractTone(pageContract: AdminUiPageContract, groupId: string, key: string): Tone {
  return optionTone(pageContract, groupId, key) as Tone;
}

export async function ControlTowerPage({ searchParams, pageContract }: { searchParams?: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const sp = searchParams ?? {};
  // Control Tower honors the top-bar scope via the shared contract (park is the backend-safe UUID).
  const scope = parseScope(sp);
  const { parkId, asOf } = backendScope(scope);
  const severityOptions = optionGroup(pageContract, "severity_chips");
  const workStateOptions = optionGroup(pageContract, "work_state_filter_chips");
  const severityParam = one(sp, "ct_severity");
  const severityFilter = (severityOptions.some((option) => option.key === severityParam) ? severityParam : "all") as ProcessIntegritySeverity | "all";
  const stateParam = one(sp, "ct_state");
  const stateFilter = (workStateOptions.some((option) => option.key === stateParam) ? stateParam : "all") as WorkState | "all";
  const openGapLabels = tableLabels(pageContract, "open-gaps");
  const pageSizeOptions = tablePageSizes(pageContract, "open-gaps");
  const CT_PATH = "/";
  const requestedPageSize = (pageSizeOptions.find((size) => size === boundedInt(one(sp, "ct_limit"), 10, 1, 100)) ?? 10) as VaccinationPageSize;
  const ctCursor = one(sp, "ct_cursor");
  const ctCursorStack = sp.ct_cursor_stack;
  const ctPage = boundedInt(one(sp, "ct_page"), 1, 1, 1000000);
  const result = await getVaccinationControlTower({
    parkId,
    asOf,
    workState: stateFilter === "all" ? undefined : stateFilter,
    severity: severityFilter === "all" ? undefined : severityFilter,
    limit: requestedPageSize,
    cursor: ctCursor,
  });
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const summary = result.ok ? result.data.summary : null;
  const severityRank = new Map(severityOptions.map((option, index) => [option.key, index]));
  // Most-broken first. The backend already applied filters/page bounds.
  const alerts: ControlTowerAlert[] = result.ok
    ? [...result.data.alerts].sort((a, b) => (severityRank.get(a.severity) ?? 999) - (severityRank.get(b.severity) ?? 999))
    : [];
  const visibleWorkStates = WORK_STATE_ORDER;
  // Counts are honest only in the unfiltered view: `alerts` is one server-filtered page,
  // so counting it under an active filter would report the filter back to itself.
  const severityCounts = new Map<string, number>();
  const stateCounts = new Map<string, number>();
  if (severityFilter === "all" && stateFilter === "all") {
    for (const alert of alerts) {
      severityCounts.set(alert.severity, (severityCounts.get(alert.severity) ?? 0) + 1);
      const ws = alert.work_state;
      if (ws) stateCounts.set(ws, (stateCounts.get(ws) ?? 0) + 1);
    }
  }

  const processTone: Tone4 = !summary ? "mut" : !summary.process_intact ? (summary.critical_count > 0 ? "dng" : "warn") : "ok";
  const processLabel = !summary
    ? "n/a"
    : summary.process_intact
      ? copy(pageContract, "label.process_intact")
      : summary.critical_count > 0
        ? copy(pageContract, "label.process_not_intact")
        : copy(pageContract, "label.process_at_risk");
  const hasAlertFilters = severityFilter !== "all" || stateFilter !== "all";
  const band = alerts.slice(0, 5);
  const totalCount = result.ok ? result.data.total_count : 0;
  const nextCursor = result.ok ? result.data.next_cursor : undefined;
  const start = totalCount === 0 ? 0 : (ctPage - 1) * requestedPageSize + 1;
  const end = totalCount === 0 ? 0 : Math.min(totalCount, start + alerts.length - 1);
  const paged = { items: alerts, page: ctPage, pageSize: requestedPageSize, total: totalCount, start, end };
  const nextHref = nextCursor ? hrefWithPagedCursor(CT_PATH, sp, "ct_cursor", nextCursor, "ct_page", "ct_cursor_stack") : null;
  const prevHref = hrefPreviousPagedCursor(CT_PATH, sp, "ct_cursor", "ct_page", "ct_cursor_stack");
  if (result.ok && ctPage > 1 && !ctCursor && !ctCursorStack) {
    redirect(scopeHref(CT_PATH, scope, {}, {
      lens: "control-tower",
      ct_severity: severityFilter,
      ct_state: stateFilter,
      ct_page: "1",
      ct_limit: String(requestedPageSize),
    }));
  }
  const ownerUnassignedLabel = copy(pageContract, "label.owner_unassigned");
  const initialSelectedAlertId = one(sp, "ct_alert");

  // Filter/page-size changes reset to page 1 and drop the cursor stack (keyset restart).
  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref(CT_PATH, scope, {}, {
      lens: "control-tower",
      ct_severity: severityFilter,
      ct_state: stateFilter,
      ct_page: String(paged.page),
      ct_limit: String(paged.pageSize),
      ct_cursor: undefined,
      ct_cursor_stack: undefined,
      ...overrides,
    });
  }
  function pagerHref(page: number): string {
    if (page > paged.page) return nextHref ?? hrefWith({});
    if (page < paged.page) return prevHref ?? hrefWith({});
    return hrefWith({ ct_page: String(page) });
  }
  function pageSizeHref(pageSize: VaccinationPageSize): string {
    return hrefWith({ ct_page: "1", ct_limit: String(pageSize), ct_cursor: undefined, ct_cursor_stack: undefined });
  }

  const closeDrawerHref = hrefWith({ ct_alert: undefined });
  const alertDrawerHref = (alert: ControlTowerAlert) => `${closeDrawerHref}#ct_alert=${encodeURIComponent(alert.row_id)}`;
  const workflowRecordHref = (alert: ControlTowerAlert) =>
    scopeHref(`/workflows/${encodeURIComponent(alert.row_id)}`, scope, {}, { from: "control-tower" });
  const actionCenterHref = (alert: ControlTowerAlert) => scopeHref("/action-center", scope, {}, { ac_row: alert.row_id });
  const drawerRecords: ControlTowerDrawerRecord[] = alerts.map((alert) => ({
    alert,
    actionCenterHref: actionCenterHref(alert),
    workflowHref: workflowRecordHref(alert),
    adherenceHref: scopeHref("/protocol-adherence", scope),
    vaccinationHref: scopeHref("/vaccination", scope),
  }));

  return (
    <div className="screen on">
      <PageHeader title={pageContract.title} crumbs={[{ label: pageContract.title }]} />

      {/* Process-integrity KPIs only — no census/count totals (Counts is a separate vertical). */}
      <KpiGrid min={210}>
        <Kpi
          label={copy(pageContract, "kpi.process")}
          value={processLabel}
          sub={summary ? pageContract.subtitle : copy(pageContract, "state.unavailable")}
          tone={processTone}
          icon={processTone === "ok" ? <CheckCircle2 /> : <AlertTriangle />}
        />
        <Kpi label={copy(pageContract, "kpi.critical")} value={summary ? fmtInt(summary.critical_count) : "n/a"} sub={copy(pageContract, "label.process_not_intact")} tone={summary && summary.critical_count > 0 ? "dng" : "mut"} />
        <Kpi label={copy(pageContract, "kpi.open_gaps")} value={summary ? fmtInt(summary.warning_count) : "n/a"} sub={copy(pageContract, "label.process_at_risk")} tone={summary && summary.warning_count > 0 ? "warn" : "mut"} />
        <Kpi
          label={copy(pageContract, "kpi.evidence")}
          value={summary ? fmtInt(summary.verification_backlog) : "n/a"}
          sub={openGapLabels[4]}
          tone={summary && summary.verification_backlog > 0 ? "info" : "mut"}
          icon={<ShieldCheck />}
        />
      </KpiGrid>

      {!result.ok ? (
        <Alert severity="error" style={{ marginBottom: 16 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </Alert>
      ) : null}

      <div style={{ marginBottom: 8 }}>
        <AnimatedTabs
          variant="pill"
          ariaLabel={copy(pageContract, "label.all_severity")}
          value={severityFilter}
          items={[
            { value: "all", label: copy(pageContract, "label.all_severity"), href: hrefWith({ ct_severity: "all", ct_page: "1" }) },
            ...SEVERITY_ORDER.map((severity) => ({
              value: severity,
              label: optionLabel(pageContract, "severity_chips", severity),
              count: severityCounts.get(severity),
              href: hrefWith({ ct_severity: severity, ct_page: "1" }),
            })),
          ]}
        />
      </div>

      <div style={{ marginBottom: 16 }}>
        <AnimatedTabs
          variant="pill"
          ariaLabel={copy(pageContract, "label.all_states")}
          value={stateFilter}
          items={[
            { value: "all", label: copy(pageContract, "label.all_states"), href: hrefWith({ ct_state: "all", ct_page: "1" }) },
            ...visibleWorkStates.map((state) => ({
              value: state,
              label: optionLabel(pageContract, "work_state_filter_chips", state),
              count: stateCounts.get(state),
              href: hrefWith({ ct_state: state, ct_page: "1" }),
            })),
          ]}
        />
      </div>

      {/* Config / SOP authority gap — server-counted (config_or_sop_blockers). */}
      {summary && summary.config_or_sop_blockers > 0 ? (
        <Alert severity="warning" style={{ marginBottom: 16 }}><div>
            <b>
              {summary.config_or_sop_blockers} {copy(pageContract, summary.config_or_sop_blockers === 1 ? "alert.config_sop.singular" : "alert.config_sop.plural")} {copy(pageContract, "alert.config_sop.action_required")}
            </b>{" "}
            {/* One link, one destination. Two links used to render here, for the
                Config screen and the vaccination SOP screen -- both now point at the
                plan console, so it named two screens that no longer exist and sent
                you to the same place twice. */}
            {copy(pageContract, "alert.config_sop.body_prefix")}{" "}
            <Link href="/vaccination/plan" className="lk">
              {copy(pageContract, "alert.config_sop.config_label")}
            </Link>
            {copy(pageContract, "alert.config_sop.body_suffix")}
          </div>
        </Alert>
      ) : null}

      <TabPanel tabKey={`${severityFilter}|${stateFilter}`}>
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
                <b>
                  {result.ok
                    ? hasAlertFilters
                      ? copy(pageContract, "empty.open_gaps_filtered")
                      : copy(pageContract, "empty.critical_ok_title")
                    : copy(pageContract, "empty.critical_unavailable")}
                </b>
                <div className="mt">
                  {result.ok
                    ? hasAlertFilters
                      ? copy(pageContract, "filter.reason")
                      : copy(pageContract, "empty.critical_ok_body")
                    : copy(pageContract, "empty.resolve_error")}
                </div>
              </div>
            </div>
          ) : (
            band.map((alert) => {
              const fill = SEVERITY_FILL[alert.severity];
              const capacityLabel = driveCapacityLabel(alert);
              return (
                <LocalOverlayLink
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
                    {capacityLabel ? <div className="mt">{capacityLabel}</div> : null}
                  </div>
                  <Tag tone={contractTone(pageContract, "severity_chips", alert.severity)}>{optionLabel(pageContract, "severity_chips", alert.severity)}</Tag>
                </LocalOverlayLink>
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
        </div>
        <div className="tbar">
          <VaccinationFilterButton
            pageContract={pageContract}
            title={copy(pageContract, "filter.drawer.title")}
            searchReason={copy(pageContract, "filter.search_reason")}
            filterReason={copy(pageContract, "filter.reason")}
            rowsLabel={`${paged.start}-${paged.end} of ${paged.total} ${copy(pageContract, "filter.rows_suffix")}`}
            actionHref={scopeHref("/action-center", scope)}
            actionLabel={copy(pageContract, "action.open_action_center")}
            facets={openGapLabels}
          />
          <span className="muted small">
            {paged.start}-{paged.end} of {paged.total} {copy(pageContract, "table.open_gaps.noun")}s
          </span>
        </div>
        {paged.total === 0 ? (
          <div className="bd">
            <p className="muted small" style={{ margin: 0, lineHeight: 1.6 }}>
              {result.ok ? (hasAlertFilters ? copy(pageContract, "empty.open_gaps_filtered") : copy(pageContract, "empty.open_gaps_detail")) : copy(pageContract, "empty.open_gaps_unavailable")}
            </p>
          </div>
        ) : (
          <div style={{ overflowX: "auto", padding: 0 }} tabIndex={0} role="group" aria-label={copy(pageContract, "table.open_gaps.aria")}>
            <Table className="control-tower-gaps-table">
              <TableHead>
                <TableRow>
                  {openGapLabels.map((label) => (
                    <TableCell component="th" key={label}>{label}</TableCell>
                  ))}
                </TableRow>
              </TableHead>
              <TableBody>
                {paged.items.map((alert) => {
                  const capacityLabel = driveCapacityLabel(alert);
                  return (
                  <TableRow key={alert.row_id} data-filter-row>
                    <TableCell>
                      <LocalOverlayLink href={alertDrawerHref(alert)} className="celllink" scroll={false}>
                        <Tag tone={contractTone(pageContract, "work_state_filter_chips", alert.work_state)}>{optionLabel(pageContract, "work_state_filter_chips", alert.work_state)}</Tag>
                      </LocalOverlayLink>
                    </TableCell>
                    <TableCell>
                      <LocalOverlayLink href={alertDrawerHref(alert)} className="celllink" scroll={false}>
                        <Tag tone={contractTone(pageContract, "severity_chips", alert.severity)}>{optionLabel(pageContract, "severity_chips", alert.severity)}</Tag>
                      </LocalOverlayLink>
                    </TableCell>
                    <TableCell className="muted">
                      <LocalOverlayLink href={alertDrawerHref(alert)} className="celllink" scroll={false}>
                        {alert.detail}
                        {capacityLabel ? <span className="mt">{capacityLabel}</span> : null}
                      </LocalOverlayLink>
                    </TableCell>
                    <TableCell className="muted">
                      <LocalOverlayLink href={alertDrawerHref(alert)} className="celllink" scroll={false}>
                        {ownerOf(alert, ownerUnassignedLabel)}
                      </LocalOverlayLink>
                    </TableCell>
                    <TableCell>
                      <LocalOverlayLink href={alertDrawerHref(alert)} className="celllink" scroll={false}>
                        <span className="lk small">{alert.next_action} →</span>
                      </LocalOverlayLink>
                    </TableCell>
                  </TableRow>
                  );
                })}
              </TableBody>
            </Table>
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
      </TabPanel>
      <ControlTowerLocalDrawer
        records={drawerRecords}
        pageContract={pageContract}
        initialSelectedAlertId={initialSelectedAlertId}
        closeHref={closeDrawerHref}
      />
    </div>
  );
}
