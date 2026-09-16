import { redirect } from "next/navigation";
import { BellRing, Settings2, ShieldCheck } from "lucide-react";

import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { Tag, type Tone } from "@/components/ui-primitives";
import { control, controlEnabled, copy, optionGroup, table, tableLabels, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { getAlertRuleConfig, listAlerts, type AlertRow, type AlertRuleConfigList, type AlertsPage as AlertsPageData } from "@/lib/api/alerts-server";
import { firstAuthRequiredError, type ApiResult } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { istDayPlus, todayIso } from "@/lib/format";
import { hrefWithParams, one, type RouteSearchParams } from "@/lib/search-params";
import { AlertsConfigure } from "./alerts-configure";
import { ALERTS_PATH, PARAM_CONFIGURE } from "./alerts-model";

const PARAM_PARK = "park";
const PARAM_DATE = "date";
const PARAM_SEVERITY = "severity";
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

function severityTone(severity: string): Tone {
  return severity === "critical" ? "dng" : "warn";
}

async function runBounded<T, R>(items: T[], limit: number, fn: (item: T) => Promise<R>): Promise<R[]> {
  const out = new Array<R>(items.length);
  let next = 0;
  const workers = Math.max(1, Math.min(limit, items.length));
  await Promise.all(
    Array.from({ length: workers }, async () => {
      for (;;) {
        const index = next;
        next += 1;
        if (index >= items.length) return;
        // serial-await: bounded worker pool; parallelism is the caller's limit.
        out[index] = await fn(items[index]!);
      }
    }),
  );
  return out;
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
  const allFailed = reads.length > 0 && okReads.length === 0;
  const partial = !allFailed && (failedParks.length > 0 || degraded.size > 0);
  // Rule names for the KPI and the degraded note: from the config read when the caller may make
  // it, otherwise from the rows themselves, which carry their rule's label. A viewer never fires
  // the config read, and the rule keys are config vocabulary that must not be shown in their place.
  const ruleLabels: Record<string, string> = Object.fromEntries(allRows.map((row) => [row.rule_key, row.rule_label]));
  if (config?.ok) for (const rule of config.data.rules) ruleLabels[rule.key] = rule.label;
  const alertsTable = table(pageContract, "alerts");
  const labels = tableLabels(pageContract, "alerts");
  const isToday = businessDate === todayIso();

  const href = (overrides: Record<string, string | null | undefined>) => hrefWithParams(ALERTS_PATH, sp, overrides);
  const configureHref = href({ [PARAM_CONFIGURE]: "1" });
  const closeHref = href({ [PARAM_CONFIGURE]: undefined });

  return (
    <div className="screen on alerts-page">
      <div className="phead">
        <div>
          <div className="crumb">
            <b>{t("crumb")}</b>
          </div>
          <h1>{t("title")}</h1>
          <div className="sub">{t("subtitle")}</div>
        </div>
        <span className="sp" style={{ flex: 1 }} />
        {mayConfigure ? (
          <LocalOverlayLink href={configureHref} scroll={false} className="btn primary" data-testid="alerts-configure-open">
            <Settings2 className="ic" aria-hidden="true" /> {configureControl.label}
          </LocalOverlayLink>
        ) : (
          <button type="button" className="btn" disabled aria-disabled title={configureControl.disabled_reason ?? undefined} data-testid="alerts-configure-open">
            <Settings2 className="ic" aria-hidden="true" /> {configureControl.label}
          </button>
        )}
      </div>

      <div className="grid g3" style={{ marginBottom: 14 }}>
        <div className="kpi">
          <span className="acc" style={{ background: critical > 0 ? "var(--danger)" : "var(--brand)" }} />
          <div className="lab">
            <BellRing className="ic" aria-hidden="true" /> {t("kpi.total")}
          </div>
          <div className="val" data-testid="alerts-kpi-total">{total}</div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: "var(--danger)" }} />
          <div className="lab">{t("kpi.critical")}</div>
          <div className="val" data-testid="alerts-kpi-critical">{critical}</div>
        </div>
        <div className="kpi">
          <span className="acc" style={{ background: "var(--info)" }} />
          <div className="lab">
            <ShieldCheck className="ic" aria-hidden="true" /> {t("kpi.rules")}
          </div>
          <div className="val">{rulesRun.size}</div>
          <div className="dl">{Array.from(rulesRun).map((key) => ruleLabels[key] ?? "").filter(Boolean).join(" · ")}</div>
        </div>
      </div>

      <section className="card" data-testid="alerts-table">
        <div className="hd" style={{ flexWrap: "wrap" }}>
          <h3 style={{ marginRight: "auto" }}>{alertsTable.title}</h3>
          <div className="subtabs" aria-label={t("filter.park")}>
            <Link href={href({ [PARAM_PARK]: null })} replace scroll={false} className={chosenPark ? "" : "on"}>
              {t("filter.park.all")}
            </Link>
            {parks.map((park) => (
              <Link key={park.key} href={href({ [PARAM_PARK]: park.key })} replace scroll={false} className={chosenPark?.key === park.key ? "on" : ""}>
                {park.label}
              </Link>
            ))}
          </div>
          <div className="subtabs" aria-label={t("filter.severity")}>
            <Link href={href({ [PARAM_SEVERITY]: null })} replace scroll={false} className={severity ? "" : "on"}>
              {t("filter.severity.all")}
            </Link>
            {severities.map((option) => (
              <Link key={option.key} href={href({ [PARAM_SEVERITY]: option.key })} replace scroll={false} className={severity === option.key ? "on" : ""}>
                {option.label}
              </Link>
            ))}
          </div>
          <div className="subtabs" aria-label={t("filter.date")}>
            <Link href={href({ [PARAM_DATE]: istDayPlus(businessDate, -1) })} replace scroll={false} aria-label="Previous day">
              ‹
            </Link>
            <Link href={href({ [PARAM_DATE]: null })} replace scroll={false} className={isToday ? "on" : ""}>
              {isToday ? t("filter.date.today") : businessDate}
            </Link>
            <Link href={href({ [PARAM_DATE]: istDayPlus(businessDate, 1) })} replace scroll={false} aria-label="Next day">
              ›
            </Link>
          </div>
        </div>

        {allFailed ? (
          <div className="alert" style={{ margin: 12 }} role="status">
            {t("state.error")} <Link href={href({})}>{t("action.retry")}</Link>
          </div>
        ) : null}

        {partial ? (
          <div className="alert" style={{ margin: 12 }} role="status">
            {t("state.partial")} {[...failedParks, ...Array.from(degraded).map((key) => ruleLabels[key] ?? "")].filter(Boolean).join(", ")}. <Link href={href({})}>{t("action.retry")}</Link>
          </div>
        ) : null}

        {!allFailed && rows.length === 0 ? (
          <div className="empty" style={{ padding: 28 }}>
            <ShieldCheck size={22} aria-hidden="true" />
            <div className="small muted" style={{ marginTop: 8 }} data-testid="alerts-empty">
              {rulesRun.size === 0 ? t("state.empty.no_rules") : t("state.empty")}
            </div>
          </div>
        ) : null}

        {rows.length > 0 ? (
          <div className="tablewrap" style={{ overflowX: "auto" }}>
            <table className="tbl">
              <thead>
                <tr>
                  {labels.map((label) => (
                    <th key={label}>{label}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr key={row.key} data-testid="alerts-row" data-severity={row.severity}>
                    <td>
                      <Tag tone={severityTone(row.severity)}>{t(`severity.${row.severity}`)}</Tag>
                    </td>
                    <td style={{ maxWidth: 360, whiteSpace: "normal" }}>
                      <b>{row.title}</b>
                    </td>
                    <td>{row.park_label}</td>
                    <td>{row.operational_location_display || <span className="muted">—</span>}</td>
                    <td style={{ maxWidth: 420, whiteSpace: "normal" }} className="small">
                      {row.detail}
                      {row.href ? (
                        <>
                          {" "}
                          <Link href={row.href} className="small">
                            {t("action.open")}
                          </Link>
                        </>
                      ) : null}
                    </td>
                    <td className="small muted">{row.rule_label}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </section>

      {mayConfigure ? (
        <AlertsConfigure
          pageContract={pageContract}
          rules={config?.ok ? config.data.rules : null}
          initialOpen={one(sp, PARAM_CONFIGURE) === "1"}
          closeHref={closeHref}
        />
      ) : null}
    </div>
  );
}
