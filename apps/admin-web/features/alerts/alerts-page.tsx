import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";
import { BellRing, Settings2, ShieldCheck, TriangleAlert } from "lucide-react";

import Link from "@/components/no-prefetch-link";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader, { cardHeaderClasses } from "@mui/material/CardHeader";
import { EmptyState } from "@/components/app/empty-state";
import { PageHeader } from "@/components/app/page-header";
import { KpiCard, KpiGrid } from "@/components/minimal/widgets";
import { AnimatedTabs, TabPanel } from "@/components/minimal/list/animated-tabs";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { Tag, type Tone } from "@/components/ui-primitives";
import { control, controlEnabled, copy, optionGroup, table, tableLabels, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { getAlertRuleConfig, listAlerts, type AlertRow, type AlertRuleConfigList, type AlertsPage as AlertsPageData } from "@/lib/api/alerts-server";
import { firstAuthRequiredError, type ApiResult } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { runBounded } from "@/lib/bounded-runner";
import { istDayPlus, todayIso } from "@/lib/format";
import { hrefWithParams, one, type RouteSearchParams } from "@/lib/search-params";
import { AlertsConfigure } from "./alerts-configure";
import { alertsEmptyState, ALERTS_PATH, PARAM_CONFIGURE } from "./alerts-model";
import Alert from "@mui/material/Alert";

const PARAM_PARK = "park";
const PARAM_DATE = "date";
const PARAM_SEVERITY = "severity";
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

function severityTone(severity: string): Tone {
  return severity === "critical" ? "dng" : "warn";
}

/**
 * /alerts (maintainer decision 2026-09-16): what is off today, directly below the Work Board.
 *
 * Every alert is backend-derived and backend-worded; this page composes reads and renders. A read
 * is bounded to ONE park (every rule reads a park-day), so "all parks" is one request per park the
 * caller may see, merged here -- the counts are summed from each park's whole-scope counts, never
 * the page's row length. The Configure button top-right follows the compiled `configure_alerts`
 * control (alerts.configure, ticked per person on /people): a reader sees it disabled with the
 * backend's reason, and the config read behind the drawer is only fired when the control is on.
 */
export async function AlertsPage({ searchParams, pageContract }: { searchParams?: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const sp = searchParams ?? {};
  const t = (key: string) => copy(pageContract, key);
  const parks: AdminUiOption[] = optionGroup(pageContract, "alerts_parks");
  const severities: AdminUiOption[] = optionGroup(pageContract, "alert_severities");
  const requestedPark = one(sp, PARAM_PARK);
  const chosenPark = requestedPark ? parks.find((option) => option.key === requestedPark) : undefined;
  const activeParks = chosenPark ? [chosenPark] : parks;
  const requestedDate = one(sp, PARAM_DATE);
  const businessDate = requestedDate && DATE_RE.test(requestedDate) ? requestedDate : todayIso();
  const requestedSeverity = one(sp, PARAM_SEVERITY);
  const severity = severities.some((option) => option.key === requestedSeverity) ? requestedSeverity : "";
  const mayConfigure = controlEnabled(pageContract, "configure_alerts", false);
  const configureControl = control(pageContract, "configure_alerts");

  const [reads, config] = await Promise.all([
    runBounded(activeParks, 2, async (park) => ({ park, result: await listAlerts({ park: park.key, businessDate }) })),
    mayConfigure ? getAlertRuleConfig() : Promise.resolve<ApiResult<AlertRuleConfigList> | null>(null),
  ]);
  const authError = firstAuthRequiredError(...reads.map((read) => read.result), ...(config ? [config] : []));
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const okReads = reads.filter((read) => read.result.ok);
  const failedParks = reads.filter((read) => !read.result.ok).map((read) => read.park.label);
  const pages: AlertsPageData[] = okReads.map((read) => (read.result as { ok: true; data: AlertsPageData }).data);
  const allRows: AlertRow[] = pages.flatMap((page) => page.rows);
  const rows = severity ? allRows.filter((row) => row.severity === severity) : allRows;
  // Whole-scope counts summed from each park's backend counts, not the filtered page.
  const total = pages.reduce((sum, page) => sum + page.total, 0);
  const critical = pages.reduce((sum, page) => sum + page.critical, 0);
  const rulesRun = new Set(pages.flatMap((page) => page.rules_run));
  const degraded = new Set(pages.flatMap((page) => page.degraded ?? []));
  // Rules the backend left out for this date (a live-figure rule on a past day), de-duplicated
  // across parks; the note repeats the backend's reason verbatim.
  const skipped = Array.from(new Map(pages.flatMap((page) => page.skipped ?? []).map((row) => [row.key, row])).values());
  const allFailed = reads.length > 0 && okReads.length === 0;
  const partial = !allFailed && (failedParks.length > 0 || degraded.size > 0);
  // Rule names for the KPI and the degraded note: from the config read when the caller may make
  // it, otherwise from the rows themselves, which carry their rule's label. A viewer never fires
  // the config read, and the rule keys are config vocabulary that must not be shown in their place.
  const ruleLabels: Record<string, string> = Object.fromEntries(allRows.map((row) => [row.rule_key, row.rule_label]));
  if (config?.ok) for (const rule of listOrEmpty(config.data.rules)) ruleLabels[rule.key] = rule.label;
  const alertsTable = table(pageContract, "alerts");
  const labels = tableLabels(pageContract, "alerts");
  const isToday = businessDate === todayIso();
  const emptyState = alertsEmptyState({
    visibleRows: rows.length,
    totalRows: allRows.length,
    allFailed,
    incomplete: partial || skipped.length > 0 || reads.length === 0,
    rulesRun: rulesRun.size,
  });

  const href = (overrides: Record<string, string | null | undefined>) => hrefWithParams(ALERTS_PATH, sp, overrides);
  const configureHref = href({ [PARAM_CONFIGURE]: "1" });
  const closeHref = href({ [PARAM_CONFIGURE]: undefined });

  return (
    <div className="kit-enter screen on alerts-page">
      <div>
        <PageHeader
          title={t("title")}
          crumbs={[{ label: t("crumb"), href: "/" }, { label: t("title") }]}
          actions={
            mayConfigure ? (
              <LocalOverlayLink href={configureHref} scroll={false} className="btn primary" data-testid="alerts-configure-open">
                <Settings2 className="ic" aria-hidden="true" /> {configureControl.label}
              </LocalOverlayLink>
            ) : (
              <button type="button" className="btn" disabled aria-disabled title={configureControl.disabled_reason ?? undefined} data-testid="alerts-configure-open">
                <Settings2 className="ic" aria-hidden="true" /> {configureControl.label}
              </button>
            )
          }
        />
      </div>

      <KpiGrid min={220}>
        <KpiCard
          label={t("kpi.total")}
          value={total}
          tone={critical > 0 ? "error" : "primary"}
          icon={<BellRing />}
          className="kpi-total"
        />
        <KpiCard label={t("kpi.critical")} value={critical} tone="error" icon={<TriangleAlert />} className="kpi-critical" />
        <KpiCard
          label={t("kpi.rules")}
          value={rulesRun.size}
          tone="info"
          icon={<ShieldCheck />}
        />
      </KpiGrid>

      <Card className="kit-tablecard" data-testid="alerts-table">
        <CardHeader
          title={alertsTable.title}
          sx={{
            pt: 2.5,
            px: 3,
            pb: 2,
            alignItems: "center",
            flexWrap: "wrap",
            rowGap: 1.5,
            [`& .${cardHeaderClasses.action}`]: { m: 0, flex: { xs: "1 1 100%", sm: "0 1 auto" }, minWidth: 0, maxWidth: "100%" },
          }}
          action={
            <Box sx={{ display: "flex", alignItems: "center", flexWrap: "wrap", gap: 1.5 }}>
              <AnimatedTabs
                variant="pill"
                ariaLabel={t("filter.park")}
                value={chosenPark?.key ?? "all"}
                items={[
                  { value: "all", label: t("filter.park.all"), href: href({ [PARAM_PARK]: null }) },
                  ...parks.map((park) => ({ value: park.key, label: park.label, href: href({ [PARAM_PARK]: park.key }) })),
                ]}
              />
              <AnimatedTabs
                variant="pill"
                ariaLabel={t("filter.severity")}
                value={severity || "all"}
                items={[
                  { value: "all", label: t("filter.severity.all"), count: allRows.length, href: href({ [PARAM_SEVERITY]: null }) },
                  ...severities.map((option) => ({
                    value: option.key,
                    label: option.label,
                    // Counted over the UNFILTERED day, so every badge keeps its number when one severity is picked.
                    count: allRows.filter((row) => row.severity === option.key).length,
                    href: href({ [PARAM_SEVERITY]: option.key }),
                  })),
                ]}
              />
              <AnimatedTabs
                variant="pill"
                ariaLabel={t("filter.date")}
                value="day"
                items={[
                  { value: "prev", label: "\u2039", href: href({ [PARAM_DATE]: istDayPlus(businessDate, -1) }) },
                  { value: "day", label: isToday ? t("filter.date.today") : businessDate, href: href({ [PARAM_DATE]: null }) },
                  { value: "next", label: "\u203a", href: href({ [PARAM_DATE]: istDayPlus(businessDate, 1) }) },
                ]}
              />
            </Box>
          }
        />

        {/* Park / severity / date all re-fetch the list. Keyed on the three of them together, the
            body cross-fades instead of snapping, which is what makes a filter feel applied rather
            than the page feel reloaded. */}
        <TabPanel tabKey={`${chosenPark?.key ?? "all"}|${severity || "all"}|${businessDate}`}>
        {allFailed ? (
          <Alert severity="error" style={{ margin: 12 }} role="status">
            {t("state.error")} <Link href={href({})}>{t("action.retry")}</Link>
          </Alert>
        ) : null}

        {partial ? (
          <Alert severity="error" style={{ margin: 12 }} role="status">
            {t("state.partial")} {[...failedParks, ...Array.from(degraded).map((key) => ruleLabels[key] ?? "")].filter(Boolean).join(", ")}. <Link href={href({})}>{t("action.retry")}</Link>
          </Alert>
        ) : null}

        {skipped.length > 0 ? (
          <div className="note muted small" style={{ margin: "0 12px 8px" }} role="status" data-testid="alerts-skipped">
            {skipped.map((row) => `${row.label}: ${row.reason}`).join(" ")}
          </div>
        ) : null}

        {emptyState ? (
          <EmptyState icon={<ShieldCheck className="ic" aria-hidden="true" />} title={<span data-testid="alerts-empty">{t(emptyState)}</span>} />
        ) : null}

        {rows.length > 0 ? (
          <div className="tablewrap" style={{ overflowX: "auto" }}>
            <Table className="tbl">
              <TableHead>
                <TableRow>
                  {labels.map((label) => (
                    <TableCell component="th" key={label}>{label}</TableCell>
                  ))}
                </TableRow>
              </TableHead>
              <TableBody>
                {rows.map((row) => (
                  <TableRow key={row.key} data-testid="alerts-row" data-severity={row.severity}>
                    <TableCell>
                      <Tag tone={severityTone(row.severity)}>{t(`severity.${row.severity}`)}</Tag>
                    </TableCell>
                    <TableCell style={{ maxWidth: 360, whiteSpace: "normal" }}>
                      <b>{row.title}</b>
                    </TableCell>
                    <TableCell>{row.park_label}</TableCell>
                    <TableCell>{row.operational_location_display || <span className="muted">—</span>}</TableCell>
                    <TableCell style={{ maxWidth: 420, whiteSpace: "normal" }} className="small">
                      {row.detail}
                      {row.href ? (
                        <>
                          {" "}
                          <Link href={row.href} className="small alerts-open-link">
                            {t("action.open")}
                          </Link>
                        </>
                      ) : null}
                    </TableCell>
                    <TableCell className="small muted">{row.rule_label}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        ) : null}
        </TabPanel>
      </Card>

      {mayConfigure ? (
        <AlertsConfigure
          pageContract={pageContract}
          rules={config?.ok ? config.data.rules : null}
          eventRules={config?.ok ? listOrEmpty(config.data.event_rules) : []}
          eventKinds={config?.ok ? listOrEmpty(config.data.event_kinds) : []}
          initialOpen={one(sp, PARAM_CONFIGURE) === "1"}
          closeHref={closeHref}
        />
      ) : null}
    </div>
  );
}
