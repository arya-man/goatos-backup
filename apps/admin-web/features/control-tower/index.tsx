import Table from "@mui/material/Table";
import { UrlSuspense } from "@/components/app/url-suspense";
import { PanelSkeleton } from "@/components/app/panel-skeleton";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import TableBody from "@mui/material/TableBody";
import TableContainer from "@mui/material/TableContainer";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { varAlpha } from "minimal-shared/utils";
import Avatar from "@mui/material/Avatar";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import MuiLink from "@mui/material/Link";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import { TableHeadCustom } from "@/components/app/table";
import { Iconify, type IconifyName } from "@/components/minimal/iconify";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, getVaccinationControlTower } from "@/lib/api/server";
import type { ControlTowerAlert, ProcessIntegritySeverity, WorkState } from "@/lib/api/server";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import { Tag } from "@/components/ui-primitives";
import type { KitTone } from "@/lib/tone";
import { PageHeader } from "@/components/app/page-header";
import { KpiGrid } from "@/components/app/kpi-grid";
import { KpiWidget, kpiColor } from "@/components/app/kpi-widget";
import { TemplateTabs, TabPanel } from "@/components/app/template-tabs";
import { copy, optionGroup, optionLabel, optionTone, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { VaccinationFilterButton, VaccinationTablePager, type VaccinationPageSize } from "@/features/preventive-care-vaccination";
import { SEVERITY_ORDER, WORK_STATE_ORDER, type Tone } from "@/features/process-integrity";
import { ControlTowerLocalDrawer, type ControlTowerDrawerRecord } from "./control-tower-local-drawer";
import Alert from "@mui/material/Alert";

// Severity colour for the alert-band icon chip (template soft avatar: tinted main channel).
const SEVERITY_COLOR: Record<ProcessIntegritySeverity, "error" | "warning" | "info" | "success"> = {
  broken: "error",
  at_risk: "warning",
  watch: "info",
  ok: "success",
};

const bandRowSx = { px: 3, py: 1.5, gap: 2, display: "flex", alignItems: "center", minHeight: "calc(var(--sp-5) + var(--sp-half))" } as const;

function BandIcon({ severity, icon }: { severity: ProcessIntegritySeverity; icon: IconifyName }) {
  const color = SEVERITY_COLOR[severity];
  return (
    <Avatar variant="rounded" aria-hidden="true" sx={{ width: 40, height: 40, flexShrink: 0, bgcolor: varAlpha(`var(--palette-${color}-mainChannel)`, 0.16), color: `${color}.main` }}>
      <Iconify icon={icon} width={22} />
    </Avatar>
  );
}

type Tone4 = "ok" | "warn" | "dng" | "info" | "mut";
const CT_TONE: Record<Tone4, KitTone> = { ok: "success", warn: "warning", dng: "error", info: "info", mut: "neutral" };


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

  const cellLinkSx = { color: "inherit", display: "block", textDecoration: "none", minHeight: "calc(var(--sp-5) + var(--sp-half))", py: 0.5 } as const;
  const bandTone = summary && summary.critical_count > 0 ? "dng" : summary && summary.warning_count > 0 ? "warn" : "mut";

  return (
    <Box sx={{ display: "grid", gridTemplateColumns: "minmax(0, 1fr)", gap: "var(--sp-3)", alignContent: "start" }}>
      <PageHeader title={pageContract.title} crumbs={[{ label: pageContract.title }]} />

      <Stack spacing={3}>
      {/* Process-integrity KPIs only — no census/count totals (Counts is a separate vertical). */}
      <KpiGrid min={210}>
        {/* The process state is a word, not a count: it rides in the template widget title. */}
        <KpiWidget
          title={`${copy(pageContract, "kpi.process")}: ${processLabel}`}
          total={null}
          caption={summary ? pageContract.subtitle : copy(pageContract, "state.unavailable")}
          color={kpiColor(CT_TONE[processTone])}
          icon={processTone === "ok" ? "completed" : "progress"}
        />
        <KpiWidget title={copy(pageContract, "kpi.critical")} total={summary?.critical_count} caption={copy(pageContract, "label.process_not_intact")} color={kpiColor(CT_TONE[summary && summary.critical_count > 0 ? "dng" : "mut"])} />
        <KpiWidget title={copy(pageContract, "kpi.open_gaps")} total={summary?.warning_count} caption={copy(pageContract, "label.process_at_risk")} color={kpiColor(CT_TONE[summary && summary.warning_count > 0 ? "warn" : "mut"])} />
        <KpiWidget
          title={copy(pageContract, "kpi.evidence")}
          total={summary?.verification_backlog}
          caption={openGapLabels[4]}
          color={kpiColor(CT_TONE[summary && summary.verification_backlog > 0 ? "info" : "mut"])}
          icon="certificates"
        />
      </KpiGrid>

      {!result.ok ? (
        <Alert severity="error">
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </Alert>
      ) : null}

      <Stack spacing={1}>
        <TemplateTabs
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
        <TemplateTabs
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
      </Stack>

      {/* Config / SOP authority gap — server-counted (config_or_sop_blockers). */}
      {summary && summary.config_or_sop_blockers > 0 ? (
        <Alert severity="warning">
          <div>
            <b>
              {summary.config_or_sop_blockers} {copy(pageContract, summary.config_or_sop_blockers === 1 ? "alert.config_sop.singular" : "alert.config_sop.plural")} {copy(pageContract, "alert.config_sop.action_required")}
            </b>{" "}
            {/* One link, one destination. Two links used to render here, for the
                Config screen and the vaccination SOP screen -- both now point at the
                plan console, so it named two screens that no longer exist and sent
                you to the same place twice. */}
            {copy(pageContract, "alert.config_sop.body_prefix")}{" "}
            <MuiLink component={Link} href="/vaccination/plan" underline="hover" sx={{ fontWeight: "fontWeightSemiBold" }}>
              {copy(pageContract, "alert.config_sop.config_label")}
            </MuiLink>
            {copy(pageContract, "alert.config_sop.body_suffix")}
          </div>
        </Alert>
      ) : null}

      {/* The alert panels (guard: url-keyed-panel): a severity / state / page click swaps them to
          their skeleton at once; header and pill strips stay on screen. */}
      <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} ignore={["ct_alert"]} fallback={<PanelSkeleton charts={1} table={10} />}>
      <TabPanel tabKey={`${severityFilter}|${stateFilter}`}>
      <Stack spacing={3}>
      {/* Critical alert band — top broken / at-risk vaccination process only (template news-list card). */}
      <Card component="section">
        <CardHeader
          avatar={<Iconify icon="solar:danger-triangle-bold" width={24} sx={{ color: "error.main" }} aria-hidden="true" />}
          title={copy(pageContract, "section.critical_alerts.title")}
          action={
            <Tag tone={bandTone}>
              {summary ? summary.critical_count : 0} {copy(pageContract, "label.critical")} · {summary ? summary.warning_count : 0} {copy(pageContract, "label.at_risk")}
            </Tag>
          }
          sx={{ mb: 1, "& .MuiCardHeader-action": { alignSelf: "center", m: 0 } }}
        />
        <Box sx={{ pb: 1 }}>
          {band.length === 0 ? (
            <Box sx={bandRowSx}>
              <BandIcon severity="ok" icon="solar:check-circle-bold" />
              <Box sx={{ minWidth: 0, flex: 1 }}>
                <Typography variant="subtitle2">
                  {result.ok
                    ? hasAlertFilters
                      ? copy(pageContract, "empty.open_gaps_filtered")
                      : copy(pageContract, "empty.critical_ok_title")
                    : copy(pageContract, "empty.critical_unavailable")}
                </Typography>
                <Typography variant="body2" sx={{ color: "text.secondary" }}>
                  {result.ok
                    ? hasAlertFilters
                      ? copy(pageContract, "filter.reason")
                      : copy(pageContract, "empty.critical_ok_body")
                    : copy(pageContract, "empty.resolve_error")}
                </Typography>
              </Box>
            </Box>
          ) : (
            band.map((alert) => {
              const capacityLabel = driveCapacityLabel(alert);
              return (
                <Box
                  component={LocalOverlayLink}
                  key={alert.row_id}
                  href={alertDrawerHref(alert)}
                  scroll={false}
                  aria-label={`${copy(pageContract, "action.open_alert_for")} ${alert.title}`}
                  sx={{ ...bandRowSx, color: "inherit", textDecoration: "none", "&:hover": { bgcolor: "action.hover" } }}
                >
                  <BandIcon severity={alert.severity} icon="mingcute:location-fill" />
                  <Box sx={{ minWidth: 0, flex: 1 }}>
                    <Typography variant="subtitle2" noWrap>
                      {alert.title}
                    </Typography>
                    <Typography variant="body2" sx={{ color: "text.secondary" }}>
                      {alert.detail} · {ownerOf(alert, ownerUnassignedLabel)} → {alert.next_action}
                    </Typography>
                    {capacityLabel ? (
                      <Typography variant="body2" sx={{ color: "text.secondary" }}>
                        {capacityLabel}
                      </Typography>
                    ) : null}
                  </Box>
                  <Tag tone={contractTone(pageContract, "severity_chips", alert.severity)}>{optionLabel(pageContract, "severity_chips", alert.severity)}</Tag>
                </Box>
              );
            })
          )}
        </Box>
      </Card>

      {/* Open gaps table — every alert row, with owner + next action (template list anatomy). */}
      <Card component="section" data-filter-scope>
        <CardHeader
          avatar={<Iconify icon="solar:danger-triangle-bold" width={24} sx={{ color: "warning.main" }} aria-hidden="true" />}
          title={copy(pageContract, "section.open_gaps.title")}
        />
        <Box sx={{ p: 2.5, gap: 2, display: "flex", alignItems: "center", flexWrap: "wrap" }}>
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
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            {paged.start}-{paged.end} of {paged.total} {copy(pageContract, "table.open_gaps.noun")}s
          </Typography>
        </Box>
        {paged.total === 0 ? (
          <Typography variant="body2" sx={{ color: "text.secondary", px: 2.5, pb: 2.5 }}>
            {result.ok ? (hasAlertFilters ? copy(pageContract, "empty.open_gaps_filtered") : copy(pageContract, "empty.open_gaps_detail")) : copy(pageContract, "empty.open_gaps_unavailable")}
          </Typography>
        ) : (
          <TableContainer tabIndex={0} role="group" aria-label={copy(pageContract, "table.open_gaps.aria")}>
            <Table sx={{ minWidth: 920, "& td:nth-of-type(-n+2), & th:nth-of-type(-n+2)": { minWidth: 132 } }}>
              <TableHeadCustom headCells={openGapLabels.map((label, index) => ({ id: `c${index}`, label }))} />
              <TableBody>
                {paged.items.map((alert) => {
                  const capacityLabel = driveCapacityLabel(alert);
                  return (
                  <TableRow hover key={alert.row_id} data-filter-row>
                    <TableCell>
                      <Box component={LocalOverlayLink} href={alertDrawerHref(alert)} scroll={false} sx={cellLinkSx}>
                        <Tag tone={contractTone(pageContract, "work_state_filter_chips", alert.work_state)}>{optionLabel(pageContract, "work_state_filter_chips", alert.work_state)}</Tag>
                      </Box>
                    </TableCell>
                    <TableCell>
                      <Box component={LocalOverlayLink} href={alertDrawerHref(alert)} scroll={false} sx={cellLinkSx}>
                        <Tag tone={contractTone(pageContract, "severity_chips", alert.severity)}>{optionLabel(pageContract, "severity_chips", alert.severity)}</Tag>
                      </Box>
                    </TableCell>
                    <TableCell sx={{ color: "text.secondary" }}>
                      <Box component={LocalOverlayLink} href={alertDrawerHref(alert)} scroll={false} sx={cellLinkSx}>
                        {alert.detail}
                        {capacityLabel ? (
                          <Box component="span" sx={{ display: "block", typography: "caption" }}>
                            {capacityLabel}
                          </Box>
                        ) : null}
                      </Box>
                    </TableCell>
                    <TableCell sx={{ color: "text.secondary" }}>
                      <Box component={LocalOverlayLink} href={alertDrawerHref(alert)} scroll={false} sx={cellLinkSx}>
                        {ownerOf(alert, ownerUnassignedLabel)}
                      </Box>
                    </TableCell>
                    <TableCell>
                      <Box component={LocalOverlayLink} href={alertDrawerHref(alert)} scroll={false} sx={{ ...cellLinkSx, color: "info.main", typography: "subtitle2" }}>
                        {alert.next_action} →
                      </Box>
                    </TableCell>
                  </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </TableContainer>
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
        <Box sx={{ px: 2.5, py: 1.5, display: "flex", columnGap: 2, flexWrap: "wrap" }}>
          {[
            { href: scopeHref("/action-center", scope), label: copy(pageContract, "link.action_center") },
            { href: scopeHref("/protocol-adherence", scope), label: copy(pageContract, "link.protocol_adherence") },
            { href: scopeHref("/workflows", scope), label: copy(pageContract, "link.workflows") },
            { href: scopeHref("/vaccination", scope), label: copy(pageContract, "link.vaccination_ops") },
            { href: `${scopeHref("/vaccination", scope)}#execution`, label: copy(pageContract, "link.park_shed_execution") },
          ].map((item) => (
            <MuiLink key={item.href} component={Link} href={item.href} underline="hover" variant="subtitle2" sx={{ minHeight: "calc(var(--sp-5) + var(--sp-half))", display: "inline-flex", alignItems: "center" }}>
              {item.label}
            </MuiLink>
          ))}
        </Box>
      </Card>
      </Stack>
      </TabPanel>
      </UrlSuspense>
      </Stack>
      <ControlTowerLocalDrawer
        records={drawerRecords}
        pageContract={pageContract}
        initialSelectedAlertId={initialSelectedAlertId}
        closeHref={closeDrawerHref}
      />
    </Box>
  );
}
