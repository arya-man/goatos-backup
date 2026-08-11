import { WeightBars } from "./weight-bars";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ApiResult, GrowthDirectorWeightsResponse } from "@/lib/api/server";

// Growth Director — the analytics block UNDER the live Weights dashboard.
//
// Server component, no client JS. Every visible string resolves through the page
// contract (`growth_director.*` keys); every widget states its own denominator,
// because the whole block stands on partial data (only kids weighed twice have a
// gain, only tag-matched kids have a breed/sex) and hiding that would let a thin
// sample read as a herd-wide fact.
//
// The block renders BACKEND aggregates verbatim — no client-side math beyond
// choosing what to show. Ratios like "kg of feed per kg gained" arrive computed;
// recomputing here from a row slice is the capped-rollup anti-pattern the page
// header warns about. There is deliberately no roster-count denominator
// anywhere: weighing is free-flow and has no roster (that table was dropped),
// so every denominator below counts scans and kids actually seen.

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
      <section className="card wchart" aria-label={gd(pageContract, "aria")}>
        <h2 className="h">{gd(pageContract, "title")}</h2>
        <p className="muted small">{copy(pageContract, "error.load.body")}</p>
      </section>
    );
  }

  const { road_to_sale: road, fair_fight: fairFight, slow_growth: slowGrowth } = result.data;
  const { feed_vs_growth: feedVsGrowth, feed_problems: feedProblems, trust } = result.data;

  return (
    <>
      {/* ---------------- Road to sale weight ---------------- */}
      <section className="card wchart" aria-label={gd(pageContract, "road.aria")}>
        <h2 className="h">{gd(pageContract, "road.title")}</h2>
        <p className="muted small">
          {gd(pageContract, "road.caption")} {nf(road.matched_identities)}{" "}
          {gd(pageContract, "road.caption_matched")} {nf(road.total_identities)}{" "}
          {gd(pageContract, "road.caption_identities")}
        </p>
        <section className="grid g3 kpi-row" aria-label={gd(pageContract, "road.movement_aria")}>
          <div className="kpi">
            <div className="lab">{gd(pageContract, "road.moved_up")}</div>
            <div className="val">{nf(road.movement.moved_up)}</div>
            <div className="dl">{gd(pageContract, "road.moved_up_dl")}</div>
          </div>
          <div className="kpi">
            <div className="lab">{gd(pageContract, "road.held")}</div>
            <div className="val">{nf(road.movement.held)}</div>
            <div className="dl">{gd(pageContract, "road.held_dl")}</div>
          </div>
          <div className="kpi">
            <div className="lab">{gd(pageContract, "road.moved_down")}</div>
            <div className="val">{nf(road.movement.moved_down)}</div>
            <div className="dl">{gd(pageContract, "road.moved_down_dl")}</div>
          </div>
        </section>
        <WeightBars
          data={road.bands.map((band) => ({
            key: band.band,
            label: band.band,
            value: band.identity_count,
          }))}
          emptyLabel={gd(pageContract, "road.empty")}
          unit={gd(pageContract, "road.unit")}
          chartLabel={gd(pageContract, "road.aria")}
          size="short"
        />
        <p className="muted small">
          {gd(pageContract, "road.footnote_pairs")} {nf(road.movement.pair_identities)}{" "}
          {gd(pageContract, "road.footnote_of")} {nf(road.total_identities)}{" "}
          {gd(pageContract, "road.footnote_tail")}
        </p>
      </section>

      {/* ---------------- Fair fight + slow growth ---------------- */}
      <section className="grid g2">
        <div className="card wchart" aria-label={gd(pageContract, "fair_fight.aria")}>
          <h2 className="h">{gd(pageContract, "fair_fight.title")}</h2>
          <p className="muted small">{gd(pageContract, "fair_fight.caption")}</p>
          {fairFight.cohorts.length === 0 ? (
            <div className="empty">
              <b>{gd(pageContract, "fair_fight.empty_title")}</b>
              <span className="muted small">{gd(pageContract, "fair_fight.empty_body")}</span>
            </div>
          ) : (
            fairFight.cohorts.map((cohort) => (
              <div key={`${cohort.breed}-${cohort.sex}`}>
                <p className="muted small">
                  <b>
                    {cohort.breed} · {cohort.sex}
                  </b>
                </p>
                <WeightBars
                  data={cohort.sheds.map((shed) => ({
                    key: shed.location_id,
                    label: `${shed.shed_display_name} (${nf(shed.pair_identities)})`,
                    value: shed.median_adg_g_per_day,
                  }))}
                  emptyLabel={gd(pageContract, "fair_fight.empty_body")}
                  unit={gd(pageContract, "unit_g")}
                  chartLabel={gd(pageContract, "fair_fight.aria")}
                  size="short"
                />
              </div>
            ))
          )}
        </div>

        <div className="card" aria-label={gd(pageContract, "watchlist.aria")}>
          <div className="wchart">
            <h2 className="h">{gd(pageContract, "watchlist.title")}</h2>
            <p className="muted small">{gd(pageContract, "watchlist.caption")}</p>
          </div>
          {slowGrowth.groups.length === 0 ? (
            <div className="empty">
              <b>{gd(pageContract, "watchlist.empty_title")}</b>
              <span className="muted small">{gd(pageContract, "watchlist.empty_body")}</span>
            </div>
          ) : (
            <div className="tablewrap">
              <table className="tbl">
                <thead>
                  <tr>
                    <th>{gd(pageContract, "watchlist.th_shed")}</th>
                    <th>{gd(pageContract, "watchlist.th_breed")}</th>
                    <th>{gd(pageContract, "watchlist.th_sex")}</th>
                    <th>{gd(pageContract, "watchlist.th_pairs")}</th>
                    <th>{gd(pageContract, "watchlist.th_gain")}</th>
                    <th>{gd(pageContract, "watchlist.th_status")}</th>
                  </tr>
                </thead>
                <tbody>
                  {slowGrowth.groups.map((group) => (
                    <tr key={`${group.location_id}-${group.breed}-${group.sex}`}>
                      <td>{group.shed_display_name}</td>
                      <td>{group.breed}</td>
                      <td>{group.sex}</td>
                      <td>{nf(group.pair_identities)}</td>
                      <td className={group.median_adg_g_per_day < 0 ? "neg" : undefined}>
                        {nf(group.median_adg_g_per_day)} {gd(pageContract, "unit_g")}
                      </td>
                      <td>
                        <span
                          className={
                            group.status === "losing"
                              ? "tag t-dng"
                              : group.status === "below_target"
                                ? "tag t-mut"
                                : "tag t-info"
                          }
                        >
                          {gd(pageContract, `watchlist.status.${group.status}`)}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <div className="wchart">
            <p className="muted small">
              {gd(pageContract, "watchlist.target_note")} {nf(slowGrowth.target_g_per_day)}{" "}
              {gd(pageContract, "watchlist.target_note_tail")}
            </p>
          </div>
        </div>
      </section>

      {/* ---------------- Feed given vs growth ---------------- */}
      <section className="card" aria-label={gd(pageContract, "feed.aria")}>
        <div className="wchart">
          <h2 className="h">{gd(pageContract, "feed.title")}</h2>
          <p className="muted small">{gd(pageContract, "feed.caption")}</p>
        </div>
        {feedVsGrowth.sheds.length === 0 ? (
          <div className="empty">
            <b>{gd(pageContract, "feed.empty_title")}</b>
            <span className="muted small">{gd(pageContract, "feed.empty_body")}</span>
          </div>
        ) : (
          <div className="tablewrap">
            <table className="tbl">
              <thead>
                <tr>
                  <th>{gd(pageContract, "feed.th_shed")}</th>
                  <th>{gd(pageContract, "feed.th_feed")}</th>
                  <th>{gd(pageContract, "feed.th_gain")}</th>
                  <th>{gd(pageContract, "feed.th_ratio")}</th>
                  <th>{gd(pageContract, "feed.th_basis")}</th>
                </tr>
              </thead>
              <tbody>
                {feedVsGrowth.sheds.map((shed) => (
                  <tr key={shed.location_id}>
                    <td>{shed.shed_display_name}</td>
                    <td>
                      {shed.feed_g_per_head_per_day === null
                        ? copy(pageContract, "empty.no_data.title")
                        : `${nf(shed.feed_g_per_head_per_day)} ${gd(pageContract, "unit_g")}`}
                    </td>
                    <td>
                      {shed.adg_g_per_day === null
                        ? gd(pageContract, "feed.needs_second_weigh")
                        : `${nf(shed.adg_g_per_day)} ${gd(pageContract, "unit_g")}`}
                    </td>
                    <td>
                      {shed.is_experiment
                        ? gd(pageContract, "feed.trial")
                        : shed.kg_feed_per_kg_gain === null
                          ? copy(pageContract, "empty.no_data.title")
                          : nf(shed.kg_feed_per_kg_gain)}
                    </td>
                    <td>
                      <span className={shed.basis === "per_animal" ? "tag t-info" : "tag t-mut"}>
                        {gd(pageContract, `feed.basis.${shed.basis}`)}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <div className="wchart">
          <p className="muted small">{gd(pageContract, "feed.footnote")}</p>
        </div>
      </section>

      {/* ---------------- Feed sheet problems ---------------- */}
      <section className="card" aria-label={gd(pageContract, "problems.aria")}>
        <div className="wchart">
          <h2 className="h">{gd(pageContract, "problems.title")}</h2>
          <p className="muted small">
            {nf(feedProblems.blocked_rows_history)} {gd(pageContract, "problems.caption_history")}{" "}
            {nf(feedProblems.blocked_rows_latest_day)} {gd(pageContract, "problems.caption_latest")}
          </p>
        </div>
        {feedProblems.items.length === 0 ? (
          <div className="empty">
            <b>{gd(pageContract, "problems.empty_title")}</b>
            <span className="muted small">{gd(pageContract, "problems.empty_body")}</span>
          </div>
        ) : (
          <div className="tablewrap">
            <table className="tbl">
              <thead>
                <tr>
                  <th>{gd(pageContract, "problems.th_shed")}</th>
                  <th>{gd(pageContract, "problems.th_item")}</th>
                  <th>{gd(pageContract, "problems.th_days")}</th>
                  <th>{gd(pageContract, "problems.th_reason")}</th>
                </tr>
              </thead>
              <tbody>
                {feedProblems.items.map((item) => (
                  <tr key={`${item.shed_label}-${item.feed_item_label}`}>
                    <td>{item.shed_label}</td>
                    <td>{item.feed_item_label}</td>
                    <td>{nf(item.blocked_days)}</td>
                    <td>
                      <span className="tag t-dng">{item.latest_reason_code}</span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <div className="wchart">
          <p className="muted small">
            {gd(pageContract, "problems.zero_note_head")} {nf(feedProblems.authored_zero_history)}{" "}
            {gd(pageContract, "problems.zero_note_tail")}
          </p>
        </div>
      </section>

      {/* ---------------- Trust panel ---------------- */}
      <section className="card wchart" aria-label={gd(pageContract, "trust.aria")}>
        <h2 className="h">{gd(pageContract, "trust.title")}</h2>
        <p className="muted small">{gd(pageContract, "trust.caption")}</p>
        <section className="grid g6 kpi-row" aria-label={gd(pageContract, "trust.aria")}>
          <div className="kpi">
            <div className="lab">{gd(pageContract, "trust.matched")}</div>
            <div className="val">{nf(trust.scans_matched)}</div>
            <div className="dl">
              {gd(pageContract, "trust.matched_dl")} {nf(trust.scans_total)}{" "}
              {gd(pageContract, "trust.matched_dl_tail")} {nf(trust.scans_unmatched)}{" "}
              {gd(pageContract, "trust.unmatched_dl_tail")}
            </div>
          </div>
          <div className="kpi">
            <div className="lab">{gd(pageContract, "trust.pairs")}</div>
            <div className="val">{nf(trust.identities_with_pair)}</div>
            <div className="dl">{gd(pageContract, "trust.pairs_dl")}</div>
          </div>
          <div className="kpi">
            <div className="lab">{gd(pageContract, "trust.once")}</div>
            <div className="val">{nf(trust.identities_once_only)}</div>
            <div className="dl">{gd(pageContract, "trust.once_dl")}</div>
          </div>
          <div className="kpi">
            <div className="lab">{gd(pageContract, "trust.whole_shed")}</div>
            <div className="val">{nf(trust.whole_shed_observations)}</div>
            <div className="dl">{gd(pageContract, "trust.whole_shed_dl")}</div>
          </div>
          <div className="kpi">
            <div className="lab">{gd(pageContract, "trust.pending")}</div>
            <div className="val">{nf(trust.scans_pending_verification)}</div>
            <div className="dl">{gd(pageContract, "trust.pending_dl")}</div>
          </div>
          <div className="kpi">
            <div className="lab">{gd(pageContract, "trust.rework")}</div>
            <div className="val">{nf(trust.scans_rework)}</div>
            <div className="dl">{gd(pageContract, "trust.rework_dl")}</div>
          </div>
        </section>
      </section>
    </>
  );
}
