import Table from "@mui/material/Table";
import { UrlSuspense } from "@/components/app/url-suspense";
import { KpiRowSkeleton, TableSkeleton } from "@/components/app/skeletons";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import Grid from "@mui/material/Grid";
import IconButton from "@mui/material/IconButton";
import MuiLink from "@mui/material/Link";
import Stack from "@mui/material/Stack";
import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";

import Link from "@/components/no-prefetch-link";
import { EmptyState } from "@/components/app/empty-state";
import { PageHeader } from "@/components/app/page-header";
import { LinkSelect } from "@/components/app/link-select";
import { CourseWidgetSummary } from "@/components/minimal/sections/overview/course/course-widget-summary";
import { COURSE_WIDGET_ICONS } from "@/lib/minimal-icons";
import { TableHeadCustom } from "@/components/minimal/table";
import { Label, type LabelColor } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { AnimatedTabs, TabPanel } from "@/components/minimal/list/animated-tabs";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { control, controlEnabled, copy, optionGroup, table, tableLabels, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { getAlertRuleConfig, listAlerts, type AlertRow, type AlertRuleConfigList, type AlertsPage as AlertsPageData } from "@/lib/api/alerts-server";
import { firstAuthRequiredError, type ApiResult } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { runBounded } from "@/lib/bounded-runner";
import { fmtDate, istDayPlus, todayIso } from "@/lib/format";
import { hrefWithParams, one, type RouteSearchParams } from "@/lib/search-params";
import { AlertsConfigure } from "./alerts-configure";
import { alertsEmptyState, ALERTS_PATH, PARAM_CONFIGURE } from "./alerts-model";

const PARAM_PARK = "park";
const PARAM_DATE = "date";
const PARAM_SEVERITY = "severity";
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

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

  const severityColor = (key: string): LabelColor => (key === "critical" ? "error" : "warning");
  const kpis = [
    { key: "total", title: t("kpi.total"), total, className: "kpi-total", icon: COURSE_WIDGET_ICONS.progress, color: critical > 0 ? ("error" as const) : ("primary" as const) },
    { key: "critical", title: t("kpi.critical"), total: critical, className: "kpi-critical", icon: COURSE_WIDGET_ICONS.certificates, color: "error" as const },
    { key: "rules", title: t("kpi.rules"), total: rulesRun.size, className: undefined, icon: COURSE_WIDGET_ICONS.completed, color: "info" as const },
  ];
  const head = labels.map((label, index) => ({ id: `c${index}`, label, sortable: false }));

  return (
    <Box className="screen on alerts-page">
      <PageHeader
        title={t("title")}
        crumbs={[{ label: t("crumb"), href: "/" }, { label: t("title") }]}
        actions={
          mayConfigure ? (
            <Button
              component={LocalOverlayLink}
              href={configureHref}
              scroll={false}
              variant="contained"
              color="primary"
              startIcon={<Iconify icon="solar:settings-bold" />}
              data-testid="alerts-configure-open"
            >
              {configureControl.label}
            </Button>
          ) : (
            <Box component="span" title={configureControl.disabled_reason ?? undefined}>
              <Button variant="outlined" color="inherit" disabled startIcon={<Iconify icon="solar:settings-bold" />} data-testid="alerts-configure-open">
                {configureControl.label}
              </Button>
            </Box>
          )
        }
      />

      <Stack spacing={3}>
        {/* Template overview/course: CourseWidgetSummary count tiles on a spacing-3 Grid. */}
        {/* KPI tiles + the alert rows read park / day (severity filters rows only); both swap to
            their skeleton on the click (guard: url-keyed-panel), the strip and toolbar stay. */}
        <UrlSuspense searchParams={sp} watch={[PARAM_PARK, PARAM_DATE]} fallback={<KpiRowSkeleton count={3} icon />}>
        <Grid container spacing={3}>
          {kpis.map((kpi) => (
            <Grid key={kpi.key} size={{ xs: 12, sm: 4 }}>
              <CourseWidgetSummary title={kpi.title} total={kpi.total} className={kpi.className} icon={kpi.icon} color={kpi.color} />
            </Grid>
          ))}
        </Grid>
        </UrlSuspense>

        {/* Template order list: severity Tabs with Label counts, then the toolbar row (park, day),
            then the table. Counts are over the UNFILTERED day, so every badge keeps its number when
            one severity is picked. */}
        <Card data-testid="alerts-table" aria-label={alertsTable.title}>
          <AnimatedTabs
            ariaLabel={t("filter.severity")}
            value={severity || "all"}
            sx={{ px: { md: 2.5 } }}
            items={[
              { value: "all", label: t("filter.severity.all"), count: allRows.length, href: href({ [PARAM_SEVERITY]: null }) },
              ...severities.map((option) => ({
                value: option.key,
                label: option.label,
                count: allRows.filter((row) => row.severity === option.key).length,
                href: href({ [PARAM_SEVERITY]: option.key }),
              })),
            ]}
          />

          <Box
            sx={{
              p: 2.5,
              gap: 2,
              display: "flex",
              flexDirection: { xs: "column", md: "row" },
              alignItems: { xs: "stretch", md: "center" },
            }}
          >
            <LinkSelect
              label={t("filter.park")}
              value={chosenPark?.key ?? "all"}
              minWidth={200}
              options={[
                { value: "all", label: t("filter.park.all"), href: href({ [PARAM_PARK]: null }) },
                ...parks.map((park) => ({ value: park.key, label: park.label, href: href({ [PARAM_PARK]: park.key }) })),
              ]}
            />
            <Box role="group" aria-label={t("filter.date")} sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
              <IconButton component={Link} href={href({ [PARAM_DATE]: istDayPlus(businessDate, -1) })} scroll={false} aria-label={t("filter.date.previous")} title={t("filter.date.previous")}>
                <Iconify icon="eva:arrow-ios-back-fill" />
              </IconButton>
              <Button component={Link} href={href({ [PARAM_DATE]: null })} scroll={false} variant="outlined" color="inherit" sx={{ minWidth: 120 }}>
                {isToday ? t("filter.date.today") : fmtDate(businessDate)}
              </Button>
              <IconButton component={Link} href={href({ [PARAM_DATE]: istDayPlus(businessDate, 1) })} scroll={false} aria-label={t("filter.date.next")} title={t("filter.date.next")}>
                <Iconify icon="eva:arrow-ios-forward-fill" />
              </IconButton>
            </Box>
          </Box>

          {/* Park / severity / date all re-fetch the list. Keyed on the three of them together, the
              body cross-fades instead of snapping. */}
          <UrlSuspense searchParams={sp} watch={[PARAM_PARK, PARAM_SEVERITY, PARAM_DATE]} fallback={<TableSkeleton bare header={false} pager={false} columns={head.length || 6} rows={8} />}>
          <TabPanel tabKey={`${chosenPark?.key ?? "all"}|${severity || "all"}|${businessDate}`}>
            <Stack spacing={2} sx={{ px: 2.5, pb: allFailed || partial || skipped.length > 0 || emptyState ? 2.5 : 0 }}>
              {allFailed ? (
                <Alert severity="error" role="status">
                  {t("state.error")} <Link href={href({})}>{t("action.retry")}</Link>
                </Alert>
              ) : null}

              {partial ? (
                <Alert severity="error" role="status">
                  {t("state.partial")} {[...failedParks, ...Array.from(degraded).map((key) => ruleLabels[key] ?? "")].filter(Boolean).join(", ")}. <Link href={href({})}>{t("action.retry")}</Link>
                </Alert>
              ) : null}

              {skipped.length > 0 ? (
                <Alert severity="info" role="status" data-testid="alerts-skipped">
                  {skipped.map((row) => `${row.label}: ${row.reason}`).join(" ")}
                </Alert>
              ) : null}

              {emptyState ? <EmptyState title={<span data-testid="alerts-empty">{t(emptyState)}</span>} /> : null}
            </Stack>

            {rows.length > 0 ? (
              <Scrollbar>
                <Table sx={{ minWidth: 960 }}>
                  <TableHeadCustom headCells={head} />
                  <TableBody>
                    {rows.map((row) => (
                      <TableRow hover key={row.key} data-testid="alerts-row" data-severity={row.severity}>
                        <TableCell>
                          <Label variant="soft" color={severityColor(row.severity)}>
                            {t(`severity.${row.severity}`)}
                          </Label>
                        </TableCell>
                        <TableCell sx={{ maxWidth: 360, typography: "subtitle2" }}>{row.title}</TableCell>
                        <TableCell>{row.park_label}</TableCell>
                        <TableCell>{row.operational_location_display || <Box component="span" sx={{ color: "text.disabled" }}>—</Box>}</TableCell>
                        <TableCell sx={{ maxWidth: 420, color: "text.secondary" }}>
                          {row.detail}
                          {row.href ? (
                            <>
                              {" "}
                              <MuiLink component={Link} href={row.href} className="alerts-open-link" underline="hover" sx={{ fontWeight: "fontWeightSemiBold" }}>
                                {t("action.open")}
                              </MuiLink>
                            </>
                          ) : null}
                        </TableCell>
                        <TableCell sx={{ color: "text.secondary" }}>{row.rule_label}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </Scrollbar>
            ) : null}
          </TabPanel>
          </UrlSuspense>
        </Card>
      </Stack>

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
    </Box>
  );
}
