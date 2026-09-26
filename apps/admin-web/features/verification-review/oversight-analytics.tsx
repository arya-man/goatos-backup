import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Stack from "@mui/material/Stack";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Link from "@/components/no-prefetch-link";
import { Label } from "@/components/minimal/label";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { EmptyContent } from "@/components/minimal/empty-content";
import { TableHeadCustom } from "@/components/minimal/table";
import { EcommerceSalesOverview, type EcommerceSalesOverviewItem } from "@/components/minimal/sections/overview/e-commerce/ecommerce-sales-overview";
import { AnalyticsWebsiteVisits } from "@/components/minimal/sections/overview/analytics/analytics-website-visits";
import { OversightKpis, type OversightKpi } from "./oversight-kpis";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { getVerificationOversightAnalytics } from "@/lib/api/server";
import { fmtDate } from "@/lib/format";

/**
 * CEO/PC-Director-only aggregate analytics rendered ABOVE the /verify queue table (KPI strip,
 * pending backlog by module, per-verifier last-14-day activity + watch integrity). Server-computed
 * only: no client mega-fetch, no page-local recompute.
 *
 * Gating: the caller MUST check `controlEnabled(pageContract, "oversight_analytics", false)`
 * before rendering this component (see verification-review-page.tsx) -- the SAME capability
 * (permissions.VerificationOversee) gates both the page contract control and the backend endpoint
 * this component reads, so a verifier's build never even calls the endpoint. This component reads
 * the contract for copy only, never for its own gating decision -- it trusts the caller.
 *
 * Anatomy is the template overview (Minimal sections/overview): BankingWidgetSummary KPI tiles,
 * EcommerceSalesOverview progress cards for the module backlog (ranked -- which module is the
 * bottleneck is the question a CEO opens this for) and the backlog age shape, AnalyticsWebsiteVisits
 * for the 14-day verdicts-vs-arrivals columns, and a CardHeader + Scrollbar table for verifiers.
 *
 * Tones are fixed per metric MEANING (backlog/oldest = warn, throughput = ok, projection and
 * reject rate = info/neutral). They are deliberately NOT threshold-driven: a colour that flips at
 * "72h" would read as a review SLA, and no such business rule has been decided.
 */
export async function OversightAnalytics({
  pageContract,
  moduleLabels,
  moduleHrefs,
}: {
  pageContract: AdminUiPageContract;
  /** Backend-owned module labels (queue filter_options.modules), keyed by module key. */
  moduleLabels?: Map<string, string>;
  /**
   * Queue hrefs keyed by NAV module key, built by the page from the live search params so a backlog
   * row lands on the queue already filtered to that module. Plain data, not a callback: this element
   * is handed to a client component as children, and a function prop would not survive that.
   */
  moduleHrefs?: Map<string, string>;
}) {
  const result = await getVerificationOversightAnalytics();
  if (!result.ok) {
    return <EmptyContent title={copy(pageContract, "oversight_analytics.unavailable")} sx={{ py: 5 }} />;
  }
  const {
    kpis,
    pending_age_buckets: ageBuckets,
    daily_volume_last_14d: dailyVolume,
    pending_by_module: pendingByModule,
    verifier_activity: verifierActivity,
  } = result.data;

  // Backend-owned label first: `module` is the SOURCE module code stored on the item ("feed"),
  // while the queue's module vocabulary is keyed by the nav module ("feed_direction"), so joining on
  // the code alone misses and prints the raw key. The nav map is a rollout fallback for a backend
  // that predates module_label; the raw code is the last resort so a row never disappears.
  const label = (module: string, moduleLabel?: string) =>
    moduleLabel?.trim() || moduleLabels?.get(module) || module;
  const medianByModule = new Map(kpis.per_module_median_review_latency_hours.map((row) => [row.module, row.median_hours]));
  // Ranked by backlog: the widest bar IS the bottleneck. Sorting here is presentation over a
  // whole-filter aggregate the backend already computed -- never a page-local recount.
  const backlog = [...pendingByModule].sort((a, b) => b.count - a.count);
  const largest = backlog[0];
  const backlogPeak = largest?.count ?? 0;

  // Backlog SHAPE. The buckets are disjoint and the backend computes them in the same statement as
  // videos_waiting, so this total is that headline number -- not a second opinion about it.
  const ageTotal =
    ageBuckets.up_to_1_day + ageBuckets.one_to_three_days + ageBuckets.three_to_seven_days + ageBuckets.over_seven_days;
  const ageSegments: Array<{ key: string; count: number; color: "success" | "info" | "warning" | "error" }> = [
    { key: "up_to_1_day", count: ageBuckets.up_to_1_day, color: "success" },
    { key: "one_to_three_days", count: ageBuckets.one_to_three_days, color: "info" },
    { key: "three_to_seven_days", count: ageBuckets.three_to_seven_days, color: "warning" },
    { key: "over_seven_days", count: ageBuckets.over_seven_days, color: "error" },
  ];

  // Trajectory over the same 14 days the chart draws: what arrived minus what was decided. Positive
  // means the queue grew. Both sums range over the SAME zero-filled date series, so this is a real
  // net change rather than two differently-scoped windows subtracted.
  const arrived14d = dailyVolume.reduce((total, day) => total + day.arrived, 0);
  const verdicts14d = dailyVolume.reduce((total, day) => total + day.verdicts, 0);
  const netChange = arrived14d - verdicts14d;

  // Tone per metric MEANING (backlog/oldest = warning, throughput = success, projection = info,
  // reject rate = neutral) -- deliberately not threshold-driven: no review SLA has been decided.
  const kpiItems: OversightKpi[] = [
    {
      key: "videos_waiting",
      title: copy(pageContract, "oversight_analytics.videos_waiting"),
      total: formatCount(kpis.videos_waiting),
      hint: largest ? `${label(largest.module, largest.module_label)} · ${copy(pageContract, "oversight_analytics.largest_backlog")}` : undefined,
      color: "warning",
      icon: "solar:inbox-in-bold-duotone",
    },
    {
      key: "oldest_pending",
      title: copy(pageContract, "oversight_analytics.oldest_pending"),
      total: formatHours(kpis.oldest_pending_age_hours),
      hint: copy(pageContract, "oversight_analytics.oldest_hint"),
      color: "warning",
      icon: "solar:sort-by-time-bold-duotone",
    },
    {
      key: "review_speed",
      title: copy(pageContract, "oversight_analytics.review_speed"),
      total: `${kpis.verdicts_per_active_day_last_7d.toFixed(1)}/day`,
      hint: copy(pageContract, "oversight_analytics.speed_hint"),
      color: "success",
      icon: "solar:file-check-bold-duotone",
    },
    {
      key: "est_days_to_clear",
      title: copy(pageContract, "oversight_analytics.est_days_to_clear"),
      total: kpis.est_days_to_clear_backlog != null ? `${kpis.est_days_to_clear_backlog.toFixed(1)}d` : "—",
      hint: copy(pageContract, "oversight_analytics.clear_hint"),
      color: "info",
      icon: "solar:calendar-date-bold",
    },
    {
      key: "reject_rate",
      title: copy(pageContract, "oversight_analytics.reject_rate"),
      total: kpis.reject_rate_last_30d != null ? formatRejectRate(kpis.reject_rate_last_30d) : "—",
      hint: copy(pageContract, "oversight_analytics.reject_hint"),
      color: "secondary",
      icon: "solar:file-corrupted-bold-duotone",
    },
  ];

  const backlogRows: EcommerceSalesOverviewItem[] = backlog.map((row) => {
    const median = medianByModule.get(row.module);
    const name = label(row.module, row.module_label);
    // The href comes from the backend's nav_module key, never from the source module code the row
    // is grouped by -- "feed" is not a filter value the queue accepts. A module the registry cannot
    // map stays plain text rather than linking somewhere that would return an unfiltered queue.
    const href = row.nav_module ? moduleHrefs?.get(row.nav_module) : undefined;
    return {
      key: row.module,
      label: href ? (
        <Link href={href} title={copy(pageContract, "oversight_analytics.open_module_queue")} style={{ color: "inherit" }}>
          {name}
        </Link>
      ) : (
        name
      ),
      value: backlogPeak > 0 ? (row.count / backlogPeak) * 100 : 0,
      display: formatCount(row.count),
      color: row === largest ? "warning" : "primary",
      caption:
        median != null
          ? `${copy(pageContract, "oversight_analytics.median_review")} ${formatHours(median)}`
          : copy(pageContract, "oversight_analytics.no_median"),
    };
  });

  const ageRows: EcommerceSalesOverviewItem[] = ageSegments.map((segment) => ({
    key: segment.key,
    label: copy(pageContract, `oversight_analytics.age.${segment.key}`),
    value: ageTotal > 0 ? (segment.count / ageTotal) * 100 : 0,
    display: formatCount(segment.count),
    color: segment.color,
  }));

  const trendSubheader = `${copy(pageContract, "oversight_analytics.trend.verdicts_noun")} ${formatCount(verdicts14d)} · ${copy(pageContract, "oversight_analytics.trend.arrived_noun")} ${formatCount(arrived14d)} · ${
    netChange === 0
      ? copy(pageContract, "oversight_analytics.trend.flat")
      : `${copy(pageContract, netChange > 0 ? "oversight_analytics.trend.grew" : "oversight_analytics.trend.shrank")} ${formatCount(Math.abs(netChange))}`
  }`;

  // Template overview composition inside the drawer: KPI tiles on the Grid, then the chart cards
  // stacked (EcommerceSalesOverview progress lists, AnalyticsWebsiteVisits paired columns), then
  // the per-verifier table card (EcommerceBestSalesman anatomy: CardHeader + Scrollbar table).
  return (
    <Stack spacing={3}>
      <OversightKpis items={kpiItems} />

      {backlog.length ? (
        <EcommerceSalesOverview title={copy(pageContract, "oversight_analytics.pending_by_module")} data={backlogRows} />
      ) : (
        <Card>
          <CardHeader title={copy(pageContract, "oversight_analytics.pending_by_module")} />
          <EmptyContent title={copy(pageContract, "oversight_analytics.no_backlog")} sx={{ py: 5 }} />
        </Card>
      )}

      {ageTotal > 0 ? (
        <EcommerceSalesOverview title={copy(pageContract, "oversight_analytics.age_shape")} data={ageRows} />
      ) : (
        <Card>
          <CardHeader title={copy(pageContract, "oversight_analytics.age_shape")} />
          <EmptyContent title={copy(pageContract, "oversight_analytics.no_backlog")} sx={{ py: 5 }} />
        </Card>
      )}

      {/* Verdicts beside arrivals, one pair of columns per day on one shared scale: keeping up looks
          like pairs of equal height, and falling behind is visible without reading a number. */}
      {dailyVolume.length ? (
        <AnalyticsWebsiteVisits
          title={copy(pageContract, "oversight_analytics.trend")}
          subheader={trendSubheader}
          chart={{
            categories: dailyVolume.map((day) => fmtDate(day.business_date)),
            series: [
              { name: copy(pageContract, "oversight_analytics.trend.verdicts_noun"), data: dailyVolume.map((day) => day.verdicts) },
              { name: copy(pageContract, "oversight_analytics.trend.arrived_noun"), data: dailyVolume.map((day) => day.arrived) },
            ],
          }}
        />
      ) : (
        <Card>
          <CardHeader title={copy(pageContract, "oversight_analytics.trend")} />
          <EmptyContent title={copy(pageContract, "oversight_analytics.trend.empty")} sx={{ py: 5 }} />
        </Card>
      )}

      {verifierActivity.length ? (
        <Card>
          <CardHeader title={copy(pageContract, "oversight_analytics.verifier_activity")} sx={{ mb: 3 }} />
          <Scrollbar>
            <Table sx={{ minWidth: 640 }}>
              <TableHeadCustom
                headCells={[
                  { id: "verifier", label: copy(pageContract, "oversight_analytics.col.verifier") },
                  { id: "verdicts", label: copy(pageContract, "oversight_analytics.col.verdicts"), align: "right" },
                  { id: "approved", label: copy(pageContract, "oversight_analytics.col.approved"), align: "right" },
                  { id: "rejected", label: copy(pageContract, "oversight_analytics.col.rejected"), align: "right" },
                  { id: "busiest_day", label: copy(pageContract, "oversight_analytics.col.busiest_day") },
                  { id: "watch_integrity", label: copy(pageContract, "oversight_analytics.col.watch_integrity") },
                ]}
              />
              <TableBody>
                {verifierActivity.map((row) => (
                  <TableRow key={row.verifier_id} hover>
                    <TableCell>{verifierLabel(row)}</TableCell>
                    <TableCell align="right">{formatCount(row.verdicts)}</TableCell>
                    <TableCell align="right">{formatCount(row.approved)}</TableCell>
                    <TableCell align="right">{formatCount(row.rejected)}</TableCell>
                    <TableCell sx={{ color: "text.secondary" }}>{row.busiest_day || "—"}</TableCell>
                    <TableCell>
                      {/* Each is a separate fact about how the videos were actually watched, so each
                          gets its own soft Label. */}
                      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.5 }}>
                        <Label variant="soft">
                          {formatCount(row.items_tracked)} {copy(pageContract, "oversight_analytics.tracked")}
                        </Label>
                        <Label variant="soft" color="success">
                          {formatCount(row.watched_to_end_count)} {copy(pageContract, "oversight_analytics.watched_full")}
                        </Label>
                        <Label variant="soft" color={row.verdict_without_play_count > 0 ? "error" : "default"}>
                          {formatCount(row.verdict_without_play_count)} {copy(pageContract, "oversight_analytics.no_play")}
                        </Label>
                      </Box>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Scrollbar>
        </Card>
      ) : null}
    </Stack>
  );
}

function formatCount(value: number): string {
  return value.toLocaleString("en-IN");
}

function formatHours(hours: number | undefined | null): string {
  if (hours == null) return "—";
  if (hours < 24) return `${hours.toFixed(1)}h`;
  return `${(hours / 24).toFixed(1)}d`;
}

// "1 in 8 rejected" reads CEO-plain (per the copy rule in the task brief); a bare percentage does
// not. Rounds to the nearest whole ratio; falls back to a percentage when the rate is too small
// for a clean "1 in N" phrase.
function formatRejectRate(rate: number): string {
  if (rate <= 0) return "0%";
  const oneInN = Math.round(1 / rate);
  if (oneInN >= 2 && oneInN <= 100) return `1 in ${oneInN} rejected`;
  return `${Math.round(rate * 100)}% rejected`;
}

// verifierLabel keeps a raw UUID out of a leadership-facing table. A verdict whose actor has no
// workforce record is not a reviewer at all -- on staging these are backfill/automation writes that
// stamp a batch of items in the same second -- and printing its id beside a real name reads as a
// second person reviewing videos. Name it for what it is and keep the id as a short trailing
// reference so it stays traceable.
function verifierLabel(row: { verifier_id: string; verifier_name?: string | null }): React.ReactNode {
  if (row.verifier_name?.trim()) return row.verifier_name;
  return (
    <span>
      Automated / unassigned account{" "}
      <Box component="span" sx={{ color: "text.disabled", typography: "caption", fontFamily: "monospace" }}>
        {row.verifier_id.slice(0, 8)}
      </Box>
    </span>
  );
}
