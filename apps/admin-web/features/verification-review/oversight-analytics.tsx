import Link from "@/components/no-prefetch-link";
import { SvgColumnBars } from "@/components/svg-column-bars";
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
 * Anatomy is the mock's console primitives (`card > .hd/.bd`, `.grid .kpi .lab/.val/.dl` with a
 * tone `.stripe`, `.bar > i`), NOT locally invented tiles. The previous build put a bare `.bt`
 * title and inline-styled divs straight inside a padding-less `.card`, so every label sat flush on
 * the card edge with no rhythm, and the two "insight" rows were flat blue `Tag` chips that carried
 * a number with nothing to compare it against. Module backlog is now a ranked bar per module --
 * which module is the bottleneck is the actual question a CEO opens this section to answer.
 *
 * Tone stripes are fixed per metric MEANING (backlog/oldest = warn, throughput = ok, projection and
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
    return <div className="small muted">{copy(pageContract, "oversight_analytics.unavailable")}</div>;
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
  const ageSegments: Array<{ key: string; count: number; tone: string }> = [
    { key: "up_to_1_day", count: ageBuckets.up_to_1_day, tone: "var(--ok)" },
    { key: "one_to_three_days", count: ageBuckets.one_to_three_days, tone: "var(--info)" },
    { key: "three_to_seven_days", count: ageBuckets.three_to_seven_days, tone: "var(--warn)" },
    { key: "over_seven_days", count: ageBuckets.over_seven_days, tone: "var(--danger)" },
  ];

  // Trajectory over the same 14 days the chart draws: what arrived minus what was decided. Positive
  // means the queue grew. Both sums range over the SAME zero-filled date series, so this is a real
  // net change rather than two differently-scoped windows subtracted.
  const arrived14d = dailyVolume.reduce((total, day) => total + day.arrived, 0);
  const verdicts14d = dailyVolume.reduce((total, day) => total + day.verdicts, 0);
  const netChange = arrived14d - verdicts14d;

  return (
    <div className="vr-oversight">
      <div>
        <div className="grid vr-okpis">
          <Kpi
            tone="warn"
            label={copy(pageContract, "oversight_analytics.videos_waiting")}
            value={formatCount(kpis.videos_waiting)}
            detail={
              largest
                ? `${label(largest.module, largest.module_label)} · ${copy(pageContract, "oversight_analytics.largest_backlog")}`
                : undefined
            }
          />
          <Kpi
            tone="warn"
            label={copy(pageContract, "oversight_analytics.oldest_pending")}
            value={formatHours(kpis.oldest_pending_age_hours)}
            detail={copy(pageContract, "oversight_analytics.oldest_hint")}
          />
          <Kpi
            tone="ok"
            label={copy(pageContract, "oversight_analytics.review_speed")}
            value={`${kpis.verdicts_per_active_day_last_7d.toFixed(1)}/day`}
            detail={copy(pageContract, "oversight_analytics.speed_hint")}
          />
          <Kpi
            tone="info"
            label={copy(pageContract, "oversight_analytics.est_days_to_clear")}
            value={kpis.est_days_to_clear_backlog != null ? `${kpis.est_days_to_clear_backlog.toFixed(1)}d` : "—"}
            detail={copy(pageContract, "oversight_analytics.clear_hint")}
          />
          <Kpi
            tone="mut"
            label={copy(pageContract, "oversight_analytics.reject_rate")}
            value={kpis.reject_rate_last_30d != null ? formatRejectRate(kpis.reject_rate_last_30d) : "—"}
            detail={copy(pageContract, "oversight_analytics.reject_hint")}
          />
        </div>

        <div className="vr-osec">
          <div className="vr-osec-hd">{copy(pageContract, "oversight_analytics.pending_by_module")}</div>
          {backlog.length ? (
            <div className="vr-omods">
              {backlog.map((row) => {
                const median = medianByModule.get(row.module);
                // The href comes from the backend's nav_module key, never from the source module
                // code the row is grouped by -- "feed" is not a filter value the queue accepts.
                // A module the registry cannot map stays a plain, non-clickable card rather than
                // linking somewhere that would silently return an unfiltered queue.
                const href = row.nav_module ? moduleHrefs?.get(row.nav_module) : undefined;
                const body = (
                  <>
                    <div className="t">
                      <span className="n">{label(row.module, row.module_label)}</span>
                      <b>{formatCount(row.count)}</b>
                    </div>
                    <div className="bar">
                      <i style={{ width: `${backlogPeak > 0 ? Math.max(3, (row.count / backlogPeak) * 100) : 0}%` }} />
                    </div>
                    <div className="m">
                      {median != null
                        ? `${copy(pageContract, "oversight_analytics.median_review")} ${formatHours(median)}`
                        : copy(pageContract, "oversight_analytics.no_median")}
                    </div>
                  </>
                );
                if (!href) {
                  return (
                    <div key={row.module} className="vr-omod">
                      {body}
                    </div>
                  );
                }
                return (
                  <Link
                    key={row.module}
                    href={href}
                    className="vr-omod vr-omod-link"
                    title={copy(pageContract, "oversight_analytics.open_module_queue")}
                  >
                    {body}
                    <span className="go">{copy(pageContract, "oversight_analytics.open_module_queue")}</span>
                  </Link>
                );
              })}
            </div>
          ) : (
            <div className="small muted">{copy(pageContract, "oversight_analytics.no_backlog")}</div>
          )}
        </div>

        <div className="vr-osec">
          <div className="vr-osec-hd">{copy(pageContract, "oversight_analytics.age_shape")}</div>
          {ageTotal > 0 ? (
            <>
              {/* One stacked bar, oldest on the right: the red tail IS the problem, and its width is
                  the share of the backlog that has waited more than a week. */}
              <div className="vr-agebar" role="img" aria-label={copy(pageContract, "oversight_analytics.age_shape")}>
                {ageSegments
                  .filter((segment) => segment.count > 0)
                  .map((segment) => (
                    <span
                      key={segment.key}
                      style={{ width: `${(segment.count / ageTotal) * 100}%`, background: segment.tone }}
                      title={`${copy(pageContract, `oversight_analytics.age.${segment.key}`)}: ${formatCount(segment.count)}`}
                    />
                  ))}
              </div>
              <div className="vr-agelegend">
                {ageSegments.map((segment) => (
                  <span key={segment.key}>
                    <i style={{ background: segment.tone }} />
                    {copy(pageContract, `oversight_analytics.age.${segment.key}`)}
                    <b>{formatCount(segment.count)}</b>
                  </span>
                ))}
              </div>
            </>
          ) : (
            <div className="small muted">{copy(pageContract, "oversight_analytics.no_backlog")}</div>
          )}
        </div>

        <div className="vr-osec">
          <div className="vr-osec-hd">{copy(pageContract, "oversight_analytics.trend")}</div>
          {/* Verdicts as columns, arrivals as the marker across each column: keeping up looks like
              columns meeting their markers, and falling behind is visible without reading a number. */}
          <SvgColumnBars
            data={dailyVolume.map((day) => ({
              key: day.business_date,
              label: fmtDate(day.business_date),
              value: day.verdicts,
              compareValue: day.arrived,
            }))}
            chartLabel={copy(pageContract, "oversight_analytics.trend")}
            valueNoun={copy(pageContract, "oversight_analytics.trend.verdicts_noun")}
            compareNoun={copy(pageContract, "oversight_analytics.trend.arrived_noun")}
            emptyLabel={copy(pageContract, "oversight_analytics.trend.empty")}
          />
          {/* The chart draws 14 columns with no axis; naming the first and last day is what makes it
              a period rather than an abstract shape. Dates are data, so they are formatted here, not
              copy. */}
          {dailyVolume.length ? (
            <div className="vr-trendaxis">
              <span>{fmtDate(dailyVolume[0].business_date)}</span>
              <span>{fmtDate(dailyVolume[dailyVolume.length - 1].business_date)}</span>
            </div>
          ) : null}
          <div className="vr-trendfoot">
            <span className="k">
              <i style={{ background: "var(--brand)" }} />
              {copy(pageContract, "oversight_analytics.trend.verdicts_noun")} {formatCount(verdicts14d)}
            </span>
            <span className="k">
              <i style={{ background: "var(--amber)" }} />
              {copy(pageContract, "oversight_analytics.trend.arrived_noun")} {formatCount(arrived14d)}
            </span>
            <b className={netChange > 0 ? "bad" : netChange < 0 ? "good" : undefined}>
              {netChange === 0
                ? copy(pageContract, "oversight_analytics.trend.flat")
                : `${copy(
                    pageContract,
                    netChange > 0 ? "oversight_analytics.trend.grew" : "oversight_analytics.trend.shrank",
                  )} ${formatCount(Math.abs(netChange))}`}
            </b>
          </div>
        </div>

        {verifierActivity.length ? (
          <div className="vr-osec">
            <div className="vr-osec-hd">{copy(pageContract, "oversight_analytics.verifier_activity")}</div>
            <div style={{ overflowX: "auto" }}>
              <table data-enh="1" className="vr-table vr-otable">
                <thead>
                  <tr>
                    <th>{copy(pageContract, "oversight_analytics.col.verifier")}</th>
                    <th>{copy(pageContract, "oversight_analytics.col.verdicts")}</th>
                    <th>{copy(pageContract, "oversight_analytics.col.approved")}</th>
                    <th>{copy(pageContract, "oversight_analytics.col.rejected")}</th>
                    <th>{copy(pageContract, "oversight_analytics.col.busiest_day")}</th>
                    <th>{copy(pageContract, "oversight_analytics.col.watch_integrity")}</th>
                  </tr>
                </thead>
                <tbody>
                  {verifierActivity.map((row) => (
                    <tr key={row.verifier_id}>
                      <td>{verifierLabel(row)}</td>
                      <td className="num">{formatCount(row.verdicts)}</td>
                      <td className="num">{formatCount(row.approved)}</td>
                      <td className="num">{formatCount(row.rejected)}</td>
                      <td className="muted">{row.busiest_day || "—"}</td>
                      <td>
                        {/* Was one run-on sentence of three numbers; each is a separate fact about
                            how the videos were actually watched, so each gets its own pill. */}
                        <div className="vr-owatch">
                          <span>
                            <b>{formatCount(row.items_tracked)}</b> {copy(pageContract, "oversight_analytics.tracked")}
                          </span>
                          <span>
                            <b>{formatCount(row.watched_to_end_count)}</b>{" "}
                            {copy(pageContract, "oversight_analytics.watched_full")}
                          </span>
                          <span className={row.verdict_without_play_count > 0 ? "bad" : undefined}>
                            <b>{formatCount(row.verdict_without_play_count)}</b>{" "}
                            {copy(pageContract, "oversight_analytics.no_play")}
                          </span>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}

function Kpi({
  tone,
  label,
  value,
  detail,
}: {
  tone: "ok" | "warn" | "danger" | "info" | "mut";
  label: string;
  value: string;
  detail?: string;
}) {
  return (
    <div className={`kpi ${tone}`}>
      <span className="stripe" />
      <div className="lab">{label}</div>
      <div className="val">{value}</div>
      {detail ? <div className="dl muted">{detail}</div> : null}
    </div>
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
      <span className="muted small mono">{row.verifier_id.slice(0, 8)}</span>
    </span>
  );
}
