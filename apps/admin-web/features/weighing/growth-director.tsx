import { WeightBars } from "./weight-bars";
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

export function GrowthDirectorSection({
  result,
  pageContract,
}: {
  result: ApiResult<GrowthDirectorWeightsResponse>;
  pageContract: AdminUiPageContract;
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
  // `feed_problems`, `trust`, `slow_growth` and `feed_vs_growth` are still served by the
  // backend, but the Feed sheet problems table, the trust-panel KPI row, the Slow-growth
  // watchlist and the Feed given vs growth table were removed from this page — the contract
  // keeps them so the widgets can be restored without a backend change. The watchlist went on
  // 2026-08-15 (maintainer decision): it answered the same question as Fair fight from a
  // narrower angle, ranking a group against a fixed target instead of against the other sheds
  // holding the same kind of kid, and it was the reason that row was split two-up. Fair fight
  // now takes the full width. The Feed given vs growth table went on 2026-08-17 (maintainer
  // decision): mostly "No data available" rows until pens carry a second weigh.
  const noData = copy(pageContract, "empty.no_data.title");

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
              // The backend ranks these sheds fastest-first; the board LISTS them
              // alphabetically (maintainer decision 2026-08-24), like every other shed list on
              // the page, so a reader can find the pen they came here for.
              //
              // The numbers count the LIST, 1..n straight down (maintainer, 2026-08-24): a
              // badge reading "1" three rows down looked like a mistake every time the eye
              // passed it. The standing did not disappear with it — the leader/behind chips
              // and the spread are still computed from the backend's ranked array, by VALUE,
              // so "this pen is the fastest of its cohort" is still on the row that earned it.
              const ranked = cohort.sheds;
              const best = ranked[0];
              const last = ranked[ranked.length - 1];
              const rankByKey = new Map(ranked.map((shed, index) => [shed.operational_key, index]));
              const sheds = [...ranked].sort((a, b) =>
                a.shed_display_name.localeCompare(b.shed_display_name, undefined, { numeric: true }),
              );
              const spread = ranked.length > 1 ? best.median_adg_g_per_day - last.median_adg_g_per_day : null;
              const kids = ranked.reduce((sum, shed) => sum + shed.pair_identities, 0);
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
                      // Standing by KEY (this list is alphabetical); the badge counts the list.
                      const standing = rankByKey.get(shed.operational_key) ?? 0;
                      // The bar is drawn against the cohort's OWN best, so every board reads
                      // "share of the leader" rather than being scaled to a page-wide maximum
                      // that would flatten a close race into identical bars. A non-positive
                      // leader leaves every track empty, which is honest: there is no gain to
                      // take a share of.
                      const share =
                        best.median_adg_g_per_day > 0
                          ? Math.max(0, (shed.median_adg_g_per_day / best.median_adg_g_per_day) * 100)
                          : 0;
                      const isLeader = standing === 0 && ranked.length > 1;
                      const isLast = standing === ranked.length - 1 && ranked.length > 1;
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

    </>
  );
}
