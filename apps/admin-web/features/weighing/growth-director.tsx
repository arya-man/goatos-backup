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

  const { road_to_sale: road, fair_fight: fairFight, slow_growth: slowGrowth } = result.data;
  const { feed_vs_growth: feedVsGrowth, feed_problems: feedProblems, trust } = result.data;
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

      {/* ---------------- Fair fight + slow growth ---------------- */}
      <section className="grid g2">
        <div className="card wchart" aria-label={gd(pageContract, "fair_fight.title")}>
          <h2 className="h">{gd(pageContract, "fair_fight.title")}</h2>
          <p className="muted small">{gd(pageContract, "fair_fight.caption")}</p>
          {fairFight.cohorts.length === 0 ? (
            <div className="empty">
              <b>{noData}</b>
              <span className="muted small">{gd(pageContract, "fair_fight.empty")}</span>
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
                    label: `${shed.shed_display_name} (${nf(shed.pair_identities)} ${gd(pageContract, "fair_fight.pair_noun")})`,
                    value: shed.median_adg_g_per_day,
                  }))}
                  emptyLabel={gd(pageContract, "fair_fight.empty")}
                  unit="g"
                  chartLabel={`${gd(pageContract, "fair_fight.title")} — ${cohort.breed} ${cohort.sex}`}
                  size="short"
                />
              </div>
            ))
          )}
          <p className="muted small">{gd(pageContract, "fair_fight.note")}</p>
        </div>

        <div className="card" aria-label={gd(pageContract, "slow.title")}>
          <div className="wchart">
            <h2 className="h">{gd(pageContract, "slow.title")}</h2>
            <p className="muted small">{gd(pageContract, "slow.caption")}</p>
          </div>
          {slowGrowth.groups.length === 0 ? (
            <div className="empty">
              <b>{noData}</b>
              <span className="muted small">{gd(pageContract, "slow.empty")}</span>
            </div>
          ) : (
            <div className="tablewrap">
              <table className="tbl">
                <thead>
                  <tr>
                    <th>{gd(pageContract, "slow.col.shed")}</th>
                    <th>{gd(pageContract, "slow.col.breed")}</th>
                    <th>{gd(pageContract, "slow.col.sex")}</th>
                    <th>{gd(pageContract, "slow.col.pairs")}</th>
                    <th>{gd(pageContract, "slow.col.median")}</th>
                    <th>{gd(pageContract, "slow.col.wow")}</th>
                    <th>{gd(pageContract, "slow.col.status")}</th>
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
                        {nf(group.median_adg_g_per_day)} g
                      </td>
                      <td>
                        {group.week_over_week_delta_g === null ? (
                          <span className="muted small">{gd(pageContract, "slow.wow.none")}</span>
                        ) : (
                          <span className={group.week_over_week_delta_g < 0 ? "neg" : undefined}>
                            {group.week_over_week_delta_g > 0 ? "+" : ""}
                            {nf(group.week_over_week_delta_g)} g
                          </span>
                        )}
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
                          {gd(pageContract, `slow.status.${group.status}`)}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <div className="wchart">
            <p className="muted small">{gd(pageContract, "slow.note")}</p>
          </div>
        </div>
      </section>

      {/* ---------------- Feed given vs growth ---------------- */}
      <section className="card" aria-label={gd(pageContract, "feed_growth.title")}>
        <div className="wchart">
          <h2 className="h">
            {gd(pageContract, "feed_growth.title")}{" "}
            <span className="tag t-mut">{gd(pageContract, "feed_growth.estimate")}</span>
          </h2>
          <p className="muted small">{gd(pageContract, "feed_growth.caption")}</p>
        </div>
        {feedVsGrowth.sheds.length === 0 ? (
          <div className="empty">
            <b>{noData}</b>
            <span className="muted small">{gd(pageContract, "feed_growth.empty")}</span>
          </div>
        ) : (
          <div className="tablewrap">
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
                {feedVsGrowth.sheds.map((shed) => (
                  <tr key={shed.location_id}>
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
                      {shed.is_experiment
                        ? gd(pageContract, "feed_growth.experiment")
                        : shed.kg_feed_per_kg_gain === null
                          ? noData
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

      {/* ---------------- Feed sheet problems ---------------- */}
      <section className="card" aria-label={gd(pageContract, "feed_problems.title")}>
        <div className="wchart">
          <h2 className="h">{gd(pageContract, "feed_problems.title")}</h2>
          <p className="muted small">
            {gd(pageContract, "feed_problems.caption")} {nf(feedProblems.blocked_rows_history)}{" "}
            {gd(pageContract, "feed_problems.history")} · {nf(feedProblems.blocked_rows_latest_day)}{" "}
            {gd(pageContract, "feed_problems.latest_day")}.
          </p>
        </div>
        {feedProblems.items.length === 0 ? (
          <div className="empty">
            <b>{noData}</b>
            <span className="muted small">{gd(pageContract, "feed_problems.empty")}</span>
          </div>
        ) : (
          <div className="tablewrap">
            <table className="tbl">
              <thead>
                <tr>
                  <th>{gd(pageContract, "feed_problems.col.shed")}</th>
                  <th>{gd(pageContract, "feed_problems.col.item")}</th>
                  <th>{gd(pageContract, "feed_problems.col.days")}</th>
                  <th>{gd(pageContract, "feed_problems.col.reason")}</th>
                </tr>
              </thead>
              <tbody>
                {/* Keyed with the index because (shed_label, feed_item_label) are display
                    strings with no documented uniqueness — two underlying feed lines can
                    share labels, and collapsing them would hide a real problem. */}
                {feedProblems.items.map((item, index) => (
                  <tr key={`${item.shed_label}-${item.feed_item_label}-${index}`}>
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
          <p className="muted small">{gd(pageContract, "feed_problems.note.zero")}</p>
        </div>
      </section>

      {/* ---------------- Trust panel ---------------- */}
      <section className="card wchart" aria-label={gd(pageContract, "trust.title")}>
        <h2 className="h">{gd(pageContract, "trust.title")}</h2>
        <p className="muted small">{gd(pageContract, "trust.caption")}</p>
        <section className="grid g6 kpi-row" aria-label={gd(pageContract, "trust.title")}>
          <div className="kpi">
            <div className="lab">{gd(pageContract, "trust.scans_matched")}</div>
            <div className="val">{nf(trust.scans_matched)}</div>
            <div className="dl">{gd(pageContract, "trust.scans_matched.sub")}</div>
          </div>
          <div className="kpi">
            <div className="lab">{gd(pageContract, "trust.pairs")}</div>
            <div className="val">{nf(trust.identities_with_pair)}</div>
            <div className="dl">{gd(pageContract, "trust.pairs.sub")}</div>
          </div>
          <div className="kpi">
            <div className="lab">{gd(pageContract, "trust.once_only")}</div>
            <div className="val">{nf(trust.identities_once_only)}</div>
            <div className="dl">{gd(pageContract, "trust.once_only.sub")}</div>
          </div>
          <div className="kpi">
            <div className="lab">{gd(pageContract, "trust.whole_shed")}</div>
            <div className="val">{nf(trust.whole_shed_observations)}</div>
            <div className="dl">{gd(pageContract, "trust.whole_shed.sub")}</div>
          </div>
          <div className="kpi">
            <div className="lab">{gd(pageContract, "trust.pending")}</div>
            <div className="val">{nf(trust.scans_pending_verification)}</div>
            <div className="dl">{gd(pageContract, "trust.pending.sub")}</div>
          </div>
          <div className="kpi">
            <div className="lab">{gd(pageContract, "trust.rework")}</div>
            <div className="val">{nf(trust.scans_rework)}</div>
            <div className="dl">{gd(pageContract, "trust.rework.sub")}</div>
          </div>
        </section>
      </section>
    </>
  );
}
