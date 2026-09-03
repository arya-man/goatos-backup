import type { ReactNode } from "react";

import { LocalOverlayLink } from "@/components/local-overlay-link";
import { Boxes } from "lucide-react";

import { GroupedColumns, type GroupedSeries } from "@/components/grouped-columns";
import { Tag } from "@/components/ui-primitives";
import { copy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { LoadwiseLoad, LoadwiseSales } from "@/lib/api/procurement";
import type { ApiResult } from "@/lib/api/server";
import { humanDate, inr, inrCompact, num, signedInr, signedInrCompact } from "./sales-format";
import type { LoadwisePriorOutcome } from "@/lib/api/procurement";

/**
 * Tooltip line for a count that includes pre-system history: the copy's label, the count, and the
 * date range the old records span (e.g. "Sold earlier: 69 · 30 Apr 2026 – 17 Aug 2026").
 */
function priorTitle(pageContract: AdminUiPageContract, key: string, prior: LoadwisePriorOutcome): string {
  const dates =
    prior.first_on && prior.last_on && prior.first_on !== prior.last_on
      ? ` · ${humanDate(prior.first_on)} – ${humanDate(prior.last_on)}`
      : prior.first_on
        ? ` · ${humanDate(prior.first_on)}`
        : "";
  return `${copy(pageContract, key)}: ${num(prior.count)}${dates}`;
}

// The load-wise section of the sales board (maintainer decision 2026-08-31): two tabs — Purchased
// (every procurement load reconciled) and From the barn (farm-born, a backend-owned shell until
// that view is built). A SERVER component: it renders the backend's reconciliation verbatim and
// derives no business number of its own — counts, values, price bases and the summary all arrive
// computed from /procurement/loadwise-sales.

/** "12 Aug" — the chart axis is too narrow for the year, which the tooltip still carries. */
function shortDate(date: string): string {
  const human = humanDate(date);
  const parts = human.split(" ");
  return parts.length === 3 ? `${parts[0]} ${parts[1]}` : human;
}

/**
 * One load's display identity: its farm load NUMBER when known ("Load 131 · Krishnamorrthy"),
 * else the vendor and purchase date. The number prefix arrives from the contract copy.
 */
/**
 * The "weighs now" figure for a load that has NOT sold: matched on the farm's load number, which
 * is the identity the weighing side's shed-to-load tags carry. A load that has sold (any sale
 * weight recorded) gets nothing here on purpose -- its animals have left, and the pens it sat in
 * may now hold another load.
 */
function currentWeightFor(load: LoadwiseLoad, weights: LoadCurrentWeights) {
  if (load.avg_sale_weight_kg != null || !load.load_ref) return null;
  return weights[load.load_ref] ?? null;
}

function loadLabel(load: LoadwiseLoad, loadWord: string, none: string): string {
  const vendor = load.vendor_name.trim() === "" ? none : load.vendor_name;
  if (load.load_ref) return `${loadWord} ${load.load_ref} · ${vendor}`;
  return load.purchase_date ? `${vendor} · ${humanDate(load.purchase_date)}` : vendor;
}

/**
 * What a load's animals weigh NOW, keyed by the farm's load number: the latest weighing of the
 * pens the load was placed into, weighted by head count (the weighing read's by_load). Null when
 * the caller may not read weighing or the read failed; the chart then draws no third bar.
 */
export type LoadCurrentWeights = Record<string, { averageKg: number; animals: number }>;

export function LoadwiseSection({
  pageContract,
  view,
  loadwise,
  canRecordCost,
  costHref,
  currentWeights = null,
}: {
  currentWeights?: LoadCurrentWeights | null;
  pageContract: AdminUiPageContract;
  /** The validated ?view= tab key (an option key of sales_views). The PAGE owns the tab chips. */
  view: string;
  /** The load-wise read, or null when the Purchased tab is not the one being rendered. */
  loadwise: ApiResult<LoadwiseSales> | null;
  /** Backend-declared record_load_cost capability; without it rows do not open the cost drawer. */
  canRecordCost: boolean;
  /** Link builder for one load's cost drawer (LocalOverlayLink, no RSC request). */
  costHref: (loadId: string) => string;
}) {
  const none = copy(pageContract, "value.none");
  const loadWord = copy(pageContract, "column.load");
  const columns = tableLabels(pageContract, "sales-loadwise");

  const countSeries: GroupedSeries[] = [
    { key: "purchased", label: copy(pageContract, "chart.series.purchased"), tone: "info" },
    { key: "sold", label: copy(pageContract, "chart.series.sold_count"), tone: "ok" },
    { key: "mortality", label: copy(pageContract, "chart.series.mortality"), tone: "danger" },
    { key: "remaining", label: copy(pageContract, "chart.series.remaining"), tone: "teal" },
  ];
  // The growth read: how heavy an animal came in against how heavy it went out, and what a
  // kilogram cost against what it fetched. Same load order as the two charts above, so a reader
  // scans one column of loads down the page.
  const weightSeries: GroupedSeries[] = [
    { key: "avg_purchase_weight_kg", label: copy(pageContract, "chart.series.avg_purchase_weight"), tone: "info" },
    { key: "avg_sale_weight_kg", label: copy(pageContract, "chart.series.avg_sale_weight"), tone: "ok" },
    // Third bar (maintainer request 2026-09-03): a load that has sold nothing shows what its
    // animals weigh NOW, so an unsold load is not a lone "bought at" bar with nothing to read it
    // against. Drawn only when the contract enabled the weighing read for this principal.
    ...(currentWeights ? [{ key: "current_avg_weight_kg", label: copy(pageContract, "chart.series.current_avg_weight"), tone: "teal" as const }] : []),
  ];
  const perKgSeries: GroupedSeries[] = [
    { key: "landed_price_per_kg", label: copy(pageContract, "chart.series.landing_price_per_kg"), tone: "info" },
    { key: "sale_price_per_kg", label: copy(pageContract, "chart.series.sale_price_per_kg"), tone: "ok" },
  ];
  // Two clocks with the SAME start (arrival) and MUTUALLY EXCLUSIVE by decision: the finished
  // span for a load that has sold, the days-so-far for one that has not. A load never shows both
  // -- a part-sold load's stragglers can be hundreds of days older than the animals that went,
  // and that bar would set the axis for every other load on the chart.
  const fatteningSeries: GroupedSeries[] = [
    { key: "fattening_days", label: copy(pageContract, "chart.series.fattening_days"), tone: "teal" },
    { key: "days_on_farm_so_far", label: copy(pageContract, "chart.series.days_on_farm_so_far"), tone: "info" },
  ];
  const valueSeries: GroupedSeries[] = [
    { key: "purchase_value", label: copy(pageContract, "chart.series.purchase_value"), tone: "info" },
    { key: "sold_value", label: copy(pageContract, "chart.series.sold_value"), tone: "ok" },
    { key: "profit_loss", label: copy(pageContract, "chart.series.profit_loss"), tone: "teal" },
  ];

  const data = loadwise?.ok ? loadwise.data : null;
  const loads = data?.loads ?? [];
  const summary = data?.summary ?? null;

  // Money cells render the recorded fact or the stated absence — never a fabricated zero.
  const moneyCell = (value: number | null | undefined, missingLabel: string): ReactNode =>
    value == null ? <span className="muted">{missingLabel}</span> : inr(Math.round(value));

  return (
    <section className="card sales-card" aria-label={copy(pageContract, "section.loadwise.aria")}>
      <div className="hd">
        <Boxes className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.loadwise.title")}</h3>
      </div>
      {/* The section's own subtitle is deliberately NOT rendered here: the PAGE header already
          carries that sentence, and repeating it under the card reads as a stutter. */}
      {/* The price every unsold animal is valued at, stated once and plainly: most of the profit
          figures below are stock, so the rate behind them cannot be buried in a tooltip. */}
      {view === "purchased" && data ? (
        <div className="muted small" style={{ marginTop: 2 }}>
          {data.overall_avg_sold_price
            ? `${copy(pageContract, "loadwise.stock_price_note")} ${inr(
                Math.round(data.overall_avg_sold_price),
              )} ${copy(pageContract, "loadwise.stock_price_each")}`
            : copy(pageContract, "loadwise.stock_price_unknown")}
        </div>
      ) : null}

      {view !== "purchased" ? (
        <div className="empty" style={{ marginTop: 12 }}>
          {copy(pageContract, "empty.farm_born")}
        </div>
      ) : loadwise && !loadwise.ok ? (
        <div className="alert" style={{ marginTop: 12 }}>
          <b>{loadwise.error.code ?? loadwise.error.kind}</b>&nbsp;
          {loadwise.error.message || copy(pageContract, "error.load")}
        </div>
      ) : loads.length === 0 ? (
        <div className="empty" style={{ marginTop: 12 }}>
          {copy(pageContract, "empty.loadwise")}
        </div>
      ) : (
        <>
          {/* Summary tiles: the backend's whole-read aggregates, verbatim. */}
          {summary ? (
            <div className="grid g4 kpi-row" style={{ marginTop: 12 }}>
              <div className="kpi">
                <div className="lab">{copy(pageContract, "loadwise.kpi.purchased")}</div>
                <div className="val">{num(summary.purchased)}</div>
                <div className="dl">
                  {num(summary.sold)} {copy(pageContract, "loadwise.kpi.sold").toLowerCase()} ·{" "}
                  {num(summary.mortality)} {copy(pageContract, "loadwise.kpi.mortality").toLowerCase()} ·{" "}
                  {num(summary.remaining)} {copy(pageContract, "loadwise.kpi.remaining").toLowerCase()}
                </div>
              </div>
              <div className="kpi">
                <div className="lab">{copy(pageContract, "loadwise.kpi.purchase_value")}</div>
                <div className="val">{summary.costed_loads > 0 ? inrCompact(summary.purchase_value) : none}</div>
                <div className="dl">
                  {num(summary.costed_loads)} / {num(loads.length)} {copy(pageContract, "loadwise.kpi.purchase_value.hint")}
                </div>
              </div>
              <div className="kpi">
                <div className="lab">{copy(pageContract, "loadwise.kpi.sold_value")}</div>
                <div className="val">{summary.sold_value > 0 ? inrCompact(summary.sold_value) : none}</div>
                <div className="dl">{copy(pageContract, "loadwise.kpi.sold_value.hint")}</div>
              </div>
              <div className="kpi">
                <div className="lab">{copy(pageContract, "loadwise.kpi.profit")}</div>
                {/* Signed and toned: a loss must not read like a profit at a glance. */}
                <div className="val" style={{ color: summary.profit_loss < 0 ? "var(--danger)" : "var(--ok)" }}>
                  {summary.costed_loads > 0 ? signedInrCompact(summary.profit_loss) : none}
                </div>
                <div className="dl">
                  {copy(pageContract, "loadwise.kpi.profit.hint")}
                  {summary.remaining_value > 0 ? (
                    <>
                      {" · "}
                      {copy(pageContract, "value.profit_incl_stock")} {inrCompact(summary.remaining_value)}
                    </>
                  ) : null}
                </div>
              </div>
            </div>
          ) : null}

          {/* Chart 1 — animals per load. */}
          <div className="mt" style={{ marginTop: 10 }}>
            {copy(pageContract, "chart.loadwise_counts.title")}
          </div>
          <GroupedColumns
            series={countSeries}
            chartLabel={copy(pageContract, "chart.loadwise_counts.title")}
            emptyLabel={copy(pageContract, "chart.loadwise_counts.empty")}
            data={loads.map((load) => ({
              key: load.load_id,
              axisLabel: load.load_ref ? load.load_ref : load.purchase_date ? shortDate(load.purchase_date) : none,
              label: loadLabel(load, loadWord, none),
              values: [load.purchased, load.sold, load.mortality, load.remaining],
              displays: [num(load.purchased), num(load.sold), num(load.mortality), num(load.remaining)],
              subLabel: load.vendor_name,
            }))}
          />

          {/* Chart 2 — money per load. The sub-line carries the sold/purchased COUNTS so what is
              left in a load is readable off this chart too. */}
          <div className="mt">{copy(pageContract, "chart.loadwise_value.title")}</div>
          <GroupedColumns
            series={valueSeries}
            chartLabel={copy(pageContract, "chart.loadwise_value.title")}
            emptyLabel={copy(pageContract, "chart.loadwise_value.empty")}
            data={loads.map((load) => ({
              key: load.load_id,
              axisLabel: load.load_ref ? load.load_ref : load.purchase_date ? shortDate(load.purchase_date) : none,
              label: loadLabel(load, loadWord, none),
              values: [
                load.purchase_value ?? null,
                load.sold_value > 0 ? load.sold_value : null,
                // A LOSS has no bar height — a negative cannot be drawn upward, and drawing its
                // magnitude would show a loss as a tall green column. The signed figure is in the
                // tooltip, and the table's coloured cell is where a loss is read.
                load.profit_loss != null && load.profit_loss > 0 ? load.profit_loss : null,
              ],
              displays: [
                load.purchase_value == null ? copy(pageContract, "value.cost_missing") : inrCompact(load.purchase_value),
                inrCompact(load.sold_value),
                load.profit_loss == null
                  ? copy(pageContract, "value.cost_missing")
                  : signedInrCompact(load.profit_loss),
              ],
              subLabel: `${num(load.sold)} / ${num(load.purchased)} ${copy(pageContract, "loadwise.kpi.sold").toLowerCase()}`,
            }))}
          />

          {/* Chart 3 — weight per animal, in against out. The pair only means something when both
              halves exist, and a load that has sold nothing has no sale weight, so its second bar
              is absent rather than zero. */}
          <div className="mt">{copy(pageContract, "chart.loadwise_weight.title")}</div>
          <GroupedColumns
            series={weightSeries}
            chartLabel={copy(pageContract, "chart.loadwise_weight.title")}
            emptyLabel={copy(pageContract, "chart.loadwise_weight.empty")}
            data={loads.map((load) => ({
              key: load.load_id,
              axisLabel: load.load_ref ? load.load_ref : load.purchase_date ? shortDate(load.purchase_date) : none,
              label: loadLabel(load, loadWord, none),
              values: [
                load.avg_purchase_weight_kg ?? null,
                load.avg_sale_weight_kg ?? null,
                ...(currentWeights ? [currentWeightFor(load, currentWeights)?.averageKg ?? null] : []),
              ],
              displays: [
                load.avg_purchase_weight_kg == null
                  ? copy(pageContract, "value.weight_missing")
                  : `${num(load.avg_purchase_weight_kg, 1)} ${copy(pageContract, "value.kg")}`,
                load.avg_sale_weight_kg == null
                  ? copy(pageContract, "value.not_sold_yet")
                  : `${num(load.avg_sale_weight_kg, 1)} ${copy(pageContract, "value.kg")}`,
                ...(currentWeights
                  ? [
                      load.avg_sale_weight_kg != null
                        ? copy(pageContract, "value.sold_no_now")
                        : (() => {
                            const now = currentWeightFor(load, currentWeights);
                            return now == null
                              ? copy(pageContract, "value.not_weighed_yet")
                              : `${num(now.averageKg, 1)} ${copy(pageContract, "value.kg")} · ${num(now.animals)} ${copy(pageContract, "value.weighed_now")}`;
                          })(),
                    ]
                  : []),
              ],
              // The sale average is over the animals actually WEIGHED on the way out, which is
              // fewer than sold on some loads. Saying so here is the difference between a sample
              // and a claim about the whole load.
              subLabel:
                load.sold_weighed_animals != null && load.sold_weighed_animals < load.sold
                  ? `${num(load.sold_weighed_animals)} / ${num(load.sold)} ${copy(pageContract, "value.weighed_out")}`
                  : load.vendor_name,
            }))}
          />

          {/* Chart 4 — what a kilogram cost against what it fetched. */}
          <div className="mt">{copy(pageContract, "chart.loadwise_per_kg.title")}</div>
          <GroupedColumns
            series={perKgSeries}
            chartLabel={copy(pageContract, "chart.loadwise_per_kg.title")}
            emptyLabel={copy(pageContract, "chart.loadwise_per_kg.empty")}
            data={loads.map((load) => ({
              key: load.load_id,
              axisLabel: load.load_ref ? load.load_ref : load.purchase_date ? shortDate(load.purchase_date) : none,
              label: loadLabel(load, loadWord, none),
              values: [load.landed_price_per_kg ?? null, load.sale_price_per_kg ?? null],
              displays: [
                load.landed_price_per_kg == null
                  ? copy(pageContract, "value.cost_missing")
                  : inr(load.landed_price_per_kg, 2),
                load.sale_price_per_kg == null
                  ? copy(pageContract, "value.not_sold_yet")
                  : inr(load.sale_price_per_kg, 2),
              ],
              subLabel: load.vendor_name,
            }))}
          />

          {/* Chart 5 — the fattening clock from arrival, in whichever of its two states the load
              is in: the finished arrival-to-sale span (animal-weighted) once it has sold, and
              until then the days its animals have been here so far. Neither is the load's AGE —
              that clock starts at purchase and is not on this axis. */}
          <div className="mt">{copy(pageContract, "chart.loadwise_fattening.title")}</div>
          <GroupedColumns
            series={fatteningSeries}
            chartLabel={copy(pageContract, "chart.loadwise_fattening.title")}
            emptyLabel={copy(pageContract, "chart.loadwise_fattening.empty")}
            data={loads.map((load) => ({
              key: load.load_id,
              axisLabel: load.load_ref ? load.load_ref : load.purchase_date ? shortDate(load.purchase_date) : none,
              label: loadLabel(load, loadWord, none),
              values: [load.fattening_days ?? null, load.days_on_farm_so_far ?? null],
              displays: [
                load.fattening_days == null
                  ? copy(pageContract, "value.not_sold_yet")
                  : `${num(load.fattening_days)} ${copy(pageContract, "value.days")}`,
                // Absent means the load has sold, or holds nothing — either way its answer is
                // the finished span above. The tooltip still states what is left in the shed, a
                // fact rather than a claim about days, so the row is never simply blank.
                load.days_on_farm_so_far == null
                  ? `${num(load.remaining)} ${copy(pageContract, "value.still_on_farm")}`
                  : `${num(load.days_on_farm_so_far)} ${copy(pageContract, "value.days")} · ${num(
                      load.remaining,
                    )} ${copy(pageContract, "value.still_on_farm")}`,
              ],
              // The clock starts on ARRIVAL, not purchase — stated on the bar so nobody reads it
              // against the purchase date in the row above.
              subLabel: load.arrived_on
                ? `${copy(pageContract, "value.arrived_on")} ${shortDate(load.arrived_on)}`
                : load.vendor_name,
            }))}
          />

          {/* The reconciliation table. */}
          <div className="twrap" style={{ marginTop: 12 }}>
            <table className="loadwise-table" aria-label={copy(pageContract, "section.loadwise.aria")}>
              <thead>
                <tr>
                  {columns.map((label) => (
                    <th key={label}>{label}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {loads.map((load) => {
                  const cell = (value: ReactNode, extra?: string) =>
                    canRecordCost ? (
                      <td className={extra}>
                        <LocalOverlayLink href={costHref(load.load_id)} className="celllink" scroll={false}>
                          {value}
                        </LocalOverlayLink>
                      </td>
                    ) : (
                      <td className={extra}>{value}</td>
                    );
                  return (
                    <tr key={load.load_id}>
                      {cell(<b>{loadLabel(load, loadWord, none)}</b>)}
                      {cell(load.farm ? load.farm : none)}
                      {cell(num(load.purchased), "num")}
                      {cell(
                        load.prior_sold ? (
                          <span title={priorTitle(pageContract, "loadwise.prior.sold", load.prior_sold)}>
                            {num(load.sold)}
                          </span>
                        ) : (
                          num(load.sold)
                        ),
                        "num",
                      )}
                      {cell(
                        load.prior_dead ? (
                          <span title={priorTitle(pageContract, "loadwise.prior.died", load.prior_dead)}>
                            {num(load.mortality)}
                          </span>
                        ) : (
                          num(load.mortality)
                        ),
                        "num",
                      )}
                      {cell(num(load.remaining), "num")}
                      {cell(
                        load.unaccounted === 0 ? (
                          <Tag tone="mut">0</Tag>
                        ) : (
                          <Tag tone="dng">{num(load.unaccounted)}</Tag>
                        ),
                        "num",
                      )}
                      {cell(moneyCell(load.purchase_value, copy(pageContract, "value.cost_missing")), "num")}
                      {/* LANDING PRICE PER LIVE KG (maintainer request 2026-09-01): what one live
                          kilogram cost to land. Absent when the load was never weighed OR never
                          costed -- two different gaps, so the cell says which rather than
                          printing a figure derived from a missing half. Two decimals, because
                          this is the number vendors are compared on and whole rupees would hide
                          the difference between two deals. */}
                      {cell(
                        load.landed_price_per_kg == null ? (
                          <span
                            className="muted"
                            title={copy(
                              pageContract,
                              load.purchase_value == null ? "value.cost_missing" : "value.weight_missing",
                            )}
                          >
                            {copy(
                              pageContract,
                              load.purchase_value == null ? "value.cost_missing" : "value.weight_missing",
                            )}
                          </span>
                        ) : (
                          <span
                            title={`${num(load.purchase_weight_kg ?? 0, 1)} ${copy(pageContract, "value.live_kg")}`}
                          >
                            {inr(load.landed_price_per_kg, 2)}
                          </span>
                        ),
                        "num",
                      )}
                      {cell(
                        load.sold > load.sold_priced ? (
                          <span
                            title={`${num(load.sold - load.sold_priced)} ${copy(pageContract, "value.sold_unpriced")}`}
                          >
                            {inr(Math.round(load.sold_value))}*
                          </span>
                        ) : (
                          inr(Math.round(load.sold_value))
                        ),
                        "num",
                      )}
                      {cell(
                        load.profit_loss == null ? (
                          <span className="muted" title={copy(pageContract, "value.profit_unavailable")}>
                            {copy(pageContract, "value.cost_missing")}
                          </span>
                        ) : (
                          <span
                            title={
                              load.remaining > 0
                                ? `${copy(pageContract, "value.profit_unrealised")} — ${copy(
                                    pageContract,
                                    `value.price_basis.${load.price_basis}`,
                                  )}`
                                : undefined
                            }
                          >
                            <b style={{ color: load.profit_loss < 0 ? "var(--danger)" : "var(--ok)" }}>
                              {signedInr(Math.round(load.profit_loss))}
                            </b>
                            {/* How much of that profit is stock nobody has sold yet. */}
                            {load.remaining > 0 && load.remaining_value != null ? (
                              <span className="muted" style={{ display: "block", fontSize: 11 }}>
                                {copy(pageContract, "value.profit_incl_stock")}{" "}
                                {inr(Math.round(load.remaining_value))}
                              </span>
                            ) : null}
                          </span>
                        ),
                        "num",
                      )}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <div className="muted small" style={{ marginTop: 8, display: "flex", gap: 14, flexWrap: "wrap" }}>
            {canRecordCost ? <span>{copy(pageContract, "loadwise.row_hint")}</span> : null}
            {data && data.total_loads > loads.length ? (
              <span>
                {copy(pageContract, "section.loadwise.showing")}: {num(loads.length)} / {num(data.total_loads)}
              </span>
            ) : null}
          </div>
        </>
      )}
    </section>
  );
}
