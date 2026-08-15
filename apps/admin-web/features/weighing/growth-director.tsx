import { WeightBars } from "./weight-bars";
import { SegmentedLinks } from "./segmented-links";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ApiResult, GrowthDirectorWeightsResponse } from "@/lib/api/server";

// Growth Director — the analytics block UNDER the live Weights dashboard.
//
// Server component, no client JS. Every visible string resolves through the
// weighing-weights page contract: the `growth_director.*` vocabulary is
// BACKEND-OWNED (adminui service copy map) and this component renders that
// vocabulary 1:1 — COPY_FALLBACKS carries the same keys only as the offline
// fallback. Every widget states its own denominator, because the whole block
// stands on partial data (only kids weighed twice have a gain, only
// tag-matched kids have a breed/sex) and hiding that would let a thin sample
// read as a herd-wide fact.
//
// The block renders BACKEND aggregates verbatim — no client-side math beyond
// choosing what to show. Ratios like "kg of feed per kg gained" arrive
// computed; recomputing here from a row slice is the capped-rollup
// anti-pattern the page header warns about. There is deliberately no
// roster-count denominator anywhere: weighing is free-flow and has no roster
// (that table was dropped), so every denominator below counts scans and kids
// actually seen.

const nf = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 1 });

function gd(pageContract: AdminUiPageContract, key: string): string {
  return copy(pageContract, `growth_director.${key}`);
}

// FEED_FILTER_PARAM is URL state, not component state: this whole block is a server component,
// so a filter has to survive a reload and be shareable as a link. Same reason the metric toggles
// above it are links.
const FEED_FILTER_PARAM = "fg_show";
const FEED_FILTERS = ["all", "measured", "trial"] as const;
type FeedFilter = (typeof FEED_FILTERS)[number];

/** An unrecognised value falls back to "all" rather than showing nothing for a typo'd URL. */
function readFeedFilter(searchParams?: Record<string, string | string[] | undefined>): FeedFilter {
  const raw = searchParams?.[FEED_FILTER_PARAM];
  const value = Array.isArray(raw) ? raw[0] : raw;
  return (FEED_FILTERS as readonly string[]).includes(value ?? "") ? (value as FeedFilter) : "all";
}

/**
 * Preserves every other parameter — park scope, the date window, each chart's own metric toggle —
 * so switching this filter cannot silently reset the rest of the page.
 */
function feedFilterHref(
  pagePath: string,
  searchParams: Record<string, string | string[] | undefined> | undefined,
  next: FeedFilter,
): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(searchParams ?? {})) {
    if (key === FEED_FILTER_PARAM) continue;
    if (Array.isArray(value)) value.forEach((item) => params.append(key, item));
    else if (value) params.set(key, value);
  }
  // "all" is the default, so it stays OUT of the URL rather than pinning a redundant parameter.
  if (next !== "all") params.set(FEED_FILTER_PARAM, next);
  const query = params.toString();
  return query ? `${pagePath}?${query}` : pagePath;
}

export function GrowthDirectorSection({
  result,
  pageContract,
  searchParams,
  pagePath,
}: {
  result: ApiResult<GrowthDirectorWeightsResponse>;
  pageContract: AdminUiPageContract;
  searchParams?: Record<string, string | string[] | undefined>;
  pagePath: string;
}) {
  // A Growth Director failure must never take the Weights page down: the live
  // dashboard above is independent and stays useful without this block.
  if (!result.ok) {
    return (
      <section className="card wchart" aria-label={gd(pageContract, "section.aria")}>
        <h2 className="h">{gd(pageContract, "error.title")}</h2>
        <p className="muted small">{gd(pageContract, "error.body")}</p>
      </section>
    );
  }

  const { road_to_sale: road, fair_fight: fairFight } = result.data;
  // `feed_problems`, `trust` and `slow_growth` are still served by the backend, but the Feed
  // sheet problems table, the trust-panel KPI row and the Slow-growth watchlist were removed
  // from this page — the contract keeps them so the widgets can be restored without a backend
  // change. The watchlist went on 2026-08-15 (maintainer decision): it answered the same
  // question as Fair fight from a narrower angle, ranking a group against a fixed target
  // instead of against the other sheds holding the same kind of kid, and it was the reason
  // that row was split two-up. Fair fight now takes the full width.
  const { feed_vs_growth: feedVsGrowth } = result.data;
  const noData = copy(pageContract, "empty.no_data.title");

  // Filtering is a VIEW over the backend's rows, never a recomputation: no total, ratio or median
  // is derived from the visible slice. `adg_g_per_day` present is exactly "this pen has a second
  // weigh", which is the distinction the 78-vs-27 split turns on.
  const feedFilter = readFeedFilter(searchParams);
  const feedRows = feedVsGrowth.sheds.filter((shed) => {
    if (feedFilter === "measured") return shed.adg_g_per_day != null;
    if (feedFilter === "trial") return shed.is_experiment;
    return true;
  });

  return (
    <>
      <h2 className="h" aria-label={gd(pageContract, "section.aria")}>
        {gd(pageContract, "section.title")}
      </h2>

      {/* ---------------- Road to sale weight ---------------- */}
      <section className="card wchart" aria-label={gd(pageContract, "road.title")}>
        <h2 className="h">{gd(pageContract, "road.title")}</h2>
        <p className="muted small">{gd(pageContract, "road.caption")}</p>
        <section className="grid g3 kpi-row" aria-label={gd(pageContract, "road.title")}>
          <div className="kpi">
            <div className="val">{nf(road.total_identities)}</div>
            <div className="dl">{gd(pageContract, "road.identities.sub")}</div>
          </div>
          <div className="kpi">
            <div className="val">{nf(road.matched_identities)}</div>
            <div className="dl">{gd(pageContract, "road.matched.sub")}</div>
          </div>
          <div className="kpi">
            <div className="val">{nf(road.movement.moved_up)}</div>
            <div className="dl">{gd(pageContract, "road.moved_up")}</div>
          </div>
          <div className="kpi">
            <div className="val">{nf(road.movement.held)}</div>
            <div className="dl">{gd(pageContract, "road.held")}</div>
          </div>
          <div className="kpi">
            <div className={road.movement.moved_down > 0 ? "val dn" : "val"}>
              {nf(road.movement.moved_down)}
            </div>
            <div className="dl">{gd(pageContract, "road.moved_down")}</div>
          </div>
        </section>
        <WeightBars
          data={road.bands.map((band) => ({
            key: band.band,
            label: band.band,
            value: band.identity_count,
          }))}
          emptyLabel={copy(pageContract, "empty.no_data.body")}
          unit={gd(pageContract, "fair_fight.pair_noun")}
          chartLabel={gd(pageContract, "road.title")}
          size="short"
        />
        <p className="muted small">
          {gd(pageContract, "road.note.pairs")} {gd(pageContract, "road.note.unmatched")}
        </p>
        <p className="muted small">{gd(pageContract, "period.note")}</p>
      </section>

      {/* ---------------- Fair fight ---------------- */}
      {/* FULL WIDTH, and it earns it. This was the left half of a two-up row whose right half
          was the Slow-growth watchlist; that card is gone (maintainer decision 2026-08-15) and
          its width came here rather than to whitespace.
          Each cohort renders as STANDINGS, because that is what the data already is: the
          backend returns a cohort's sheds ordered by median gain DESC (growth_cohorts.go
          `ORDER BY breed, sex, median_adg_g_day DESC`), so position IS the rank and nothing is
          sorted or ranked on the client. The spread between first and last is the line that
          decides whether a cohort is worth a walk -- sheds within a few grams of each other are
          not a shed problem, however low the whole cohort sits. */}
      <section className="card wchart ffcard" aria-label={gd(pageContract, "fair_fight.title")}>
        <h2 className="h">{gd(pageContract, "fair_fight.title")}</h2>
        <p className="muted small">{gd(pageContract, "fair_fight.caption")}</p>
        {fairFight.cohorts.length === 0 ? (
          <div className="empty">
            <b>{noData}</b>
            <span className="muted small">{gd(pageContract, "fair_fight.empty")}</span>
          </div>
        ) : (
          <div className="ffboard">
            {fairFight.cohorts.map((cohort) => {
              // Read off the ENDS of the backend's own ordering rather than recomputing a
              // min/max: taking the extremes from a list the server already ranked keeps one
              // definition of "best" on both sides. A one-shed cohort cannot happen (the query
              // requires two), but the guard keeps the arithmetic honest if that ever changes.
              const sheds = cohort.sheds;
              const best = sheds[0];
              const last = sheds[sheds.length - 1];
              const spread = sheds.length > 1 ? best.median_adg_g_per_day - last.median_adg_g_per_day : null;
              const kids = sheds.reduce((sum, shed) => sum + shed.pair_identities, 0);
              return (
                <div className="ffmatch" key={`${cohort.breed}-${cohort.sex}`}>
                  <div className="ffhead">
                    <b className="ffcohort">
                      {cohort.breed} · {cohort.sex}
                    </b>
                    <span className="muted small">
                      {nf(sheds.length)} {gd(pageContract, "fair_fight.shed_noun")} · {nf(kids)}{" "}
                      {gd(pageContract, "fair_fight.pair_noun")}
                    </span>
                  </div>
                  <ol className="ffstand" aria-label={`${gd(pageContract, "fair_fight.title")} — ${cohort.breed} ${cohort.sex}`}>
                    {sheds.map((shed, index) => {
                      // The bar is drawn against the cohort's OWN best, so every board reads
                      // "share of the leader" rather than being scaled to a page-wide maximum
                      // that would flatten a close race into identical bars. A non-positive
                      // leader leaves every track empty, which is honest: there is no gain to
                      // take a share of.
                      const share =
                        best.median_adg_g_per_day > 0
                          ? Math.max(0, (shed.median_adg_g_per_day / best.median_adg_g_per_day) * 100)
                          : 0;
                      const isLeader = index === 0 && sheds.length > 1;
                      const isLast = index === sheds.length - 1 && sheds.length > 1;
                      return (
                        <li className={`ffrow${isLeader ? " ffwin" : ""}`} key={shed.operational_key}>
                          <span className="ffrank" aria-label={gd(pageContract, "fair_fight.rank_label")}>
                            {index + 1}
                          </span>
                          {/* The name gets a LINE OF ITS OWN, because a shed's identity here is
                              park + shed + pen — thirty-odd characters — and that does not fit
                              beside a bar. Squeezed into a column it truncated after the park,
                              naming the farm and hiding the one thing the row is about. `title`
                              still carries the full string for a name that outruns even a line. */}
                          <span className="ffshed" title={shed.shed_display_name}>
                            {shed.shed_display_name}
                          </span>
                          <span className={`ffval${shed.median_adg_g_per_day < 0 ? " neg" : ""}`}>
                            {nf(shed.median_adg_g_per_day)} g
                          </span>
                          <span className="fftrack">
                            <i
                              className={shed.median_adg_g_per_day < 0 ? "neg" : undefined}
                              style={{ width: `${Math.max(share, 0.6)}%` }}
                            />
                          </span>
                          {/* n and the standing chip share one cell so the chip never takes a
                              column off every name — including the middle rows that carry no
                              chip — and the kid count stays visible on the leader and last rows,
                              which are exactly the two an operator checks the sample size of. */}
                          <span className="ffmeta">
                            <span className="muted ffn">
                              {nf(shed.pair_identities)} {gd(pageContract, "fair_fight.pair_noun")}
                            </span>
                            {isLeader ? (
                              <span className="tag t-ok ffchip">{gd(pageContract, "fair_fight.leader")}</span>
                            ) : isLast ? (
                              <span className="tag t-mut ffchip">{gd(pageContract, "fair_fight.behind")}</span>
                            ) : null}
                          </span>
                        </li>
                      );
                    })}
                  </ol>
                  {spread === null ? null : (
                    <p className="ffspread muted small">
                      {gd(pageContract, "fair_fight.spread")} <b>{nf(spread)} g</b>
                    </p>
                  )}
                </div>
              );
            })}
          </div>
        )}
        <p className="muted small">{gd(pageContract, "fair_fight.note")}</p>
      </section>

      {/* ---------------- Feed given vs growth ---------------- */}
      <section className="card" aria-label={gd(pageContract, "feed_growth.title")}>
        <div className="wchart">
          <h2 className="h">
            {gd(pageContract, "feed_growth.title")}{" "}
            <span className="tag t-mut">{gd(pageContract, "feed_growth.estimate")}</span>
            {/* The filter earns its place because this table became ONE ROW PER PEN: 105 rows
                where it used to be 18 sheds, and 78 of them have no second weigh yet, so the
                27 rows a reader can act on were buried. Links, not client state — the section
                is a server component and a filter must survive a reload and paste as a URL. */}
            <SegmentedLinks
              current={feedFilter}
              ariaLabel={gd(pageContract, "feed_growth.filter.aria")}
              options={FEED_FILTERS.map((option) => ({
                value: option,
                label: gd(pageContract, `feed_growth.filter.${option}`),
                href: feedFilterHref(pagePath, searchParams, option),
              }))}
            />
          </h2>
          <p className="muted small">
            {gd(pageContract, "feed_growth.caption")}{" "}
            <b>
              {nf(feedRows.length)} {gd(pageContract, "feed_growth.showing")}
            </b>
          </p>
        </div>
        {feedRows.length === 0 ? (
          <div className="empty">
            <b>{noData}</b>
            {/* A filter that matched nothing is a different statement from "this park fed
                nothing", and saying the wrong one sends a reader looking for a data problem
                that does not exist. */}
            <span className="muted small">
              {gd(pageContract, feedVsGrowth.sheds.length === 0 ? "feed_growth.empty" : "feed_growth.filter.empty")}
            </span>
          </div>
        ) : (
          <div
            className="tablewrap"
            tabIndex={0}
            role="group"
            aria-label={gd(pageContract, "feed_growth.title")}
          >
            <table className="tbl">
              <thead>
                <tr>
                  <th>{gd(pageContract, "feed_growth.col.shed")}</th>
                  <th>{gd(pageContract, "feed_growth.col.feed")}</th>
                  <th>{gd(pageContract, "feed_growth.col.gain")}</th>
                  <th>{gd(pageContract, "feed_growth.col.ratio")}</th>
                </tr>
              </thead>
              <tbody>
                {feedRows.map((shed) => (
                  // Keyed by location AND pen. The row grain is one PEN, so a partitioned shed
                  // returns up to ten rows under ONE location_id -- keying on location_id alone
                  // gave nine children the same key, which React treats as duplicated/omitted
                  // children rather than a warning. Same pair the gain chart above already keys on.
                  <tr key={`${shed.location_id}|${shed.partition_label ?? ""}`}>
                    <td>
                      {shed.shed_display_name}{" "}
                      <span className={shed.basis === "per_animal" ? "tag t-info" : "tag t-mut"}>
                        {gd(pageContract, `feed_growth.basis.${shed.basis}`)}
                      </span>{" "}
                      {shed.is_experiment ? (
                        <span className="tag t-info">
                          {gd(pageContract, "feed_growth.experiment")}
                        </span>
                      ) : null}
                    </td>
                    <td>
                      {shed.feed_g_per_head_per_day === null
                        ? noData
                        : `${nf(shed.feed_g_per_head_per_day)} ${gd(pageContract, "feed_growth.feed_unit")}`}
                    </td>
                    <td>
                      {shed.adg_g_per_day === null
                        ? gd(pageContract, "feed_growth.no_gain")
                        : `${nf(shed.adg_g_per_day)} g`}
                    </td>
                    <td>
                      {shed.kg_feed_per_kg_gain === null
                        ? shed.is_experiment
                          ? gd(pageContract, "feed_growth.experiment")
                          : noData
                        : nf(shed.kg_feed_per_kg_gain)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <div className="wchart">
          <p className="muted small">
            {gd(pageContract, "feed_growth.note")}{" "}
            {gd(pageContract, "feed_growth.experiment.note")}
          </p>
        </div>
      </section>

    </>
  );
}
