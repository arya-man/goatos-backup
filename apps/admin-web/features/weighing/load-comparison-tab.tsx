import { Scale } from "lucide-react";

import { GroupedBars, type BarGroup, type GroupedBar } from "./grouped-bars";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import type { ShedWeightsResponse } from "@/lib/api/server";
import type { LoadwiseLoad } from "@/lib/api/procurement";

/**
 * ADG Analytics › Load-wise — purchased weight against the latest weighing, load by load.
 *
 * Two backend-owned reads joined by the farm's own load number, never a third number of this
 * tab's invention: the purchase side is the procurement load ledger (`/procurement/loadwise-sales`,
 * purchase weight over animals bought), the weighing side is the SAME `/weighing/shed-weights`
 * by-load read the Weights page's load chart uses (weighted mean at each tagged shed's latest
 * weigh). The growth multiple is the one derived figure — latest ÷ purchase, per animal — and it
 * renders as absent whenever either side is missing, because a load with no tagged shed or no
 * recorded purchase weight has no honest multiple.
 *
 * The parent page fetches both reads (the weighing side over an ALL-TIME window — "latest
 * weighing" means the newest weigh that exists, not the newest inside the page's selected
 * period) and passes them here; this component composes no data of its own.
 */

function kg(value: number): string {
  return value.toLocaleString("en-IN", { minimumFractionDigits: 1, maximumFractionDigits: 1 });
}

/** The chart/table heading for one load: its own number, else its source, else its date. */
function loadHeading(load: LoadwiseLoad): string {
  return load.load_ref || load.vendor_name || fmtDate(load.purchase_date ?? undefined) || load.load_id;
}

export function LoadComparisonTab({
  pageContract,
  loads,
  weights,
}: {
  pageContract: AdminUiPageContract;
  /** The purchase ledger rows; null when that read failed. */
  loads: LoadwiseLoad[] | null;
  /** The by-load weighing read (all-time window); null when that read failed. */
  weights: ShedWeightsResponse | null;
}) {
  // The purchase ledger is the row set: without it there is nothing to compare, so its failure
  // is the tab's failure. The weighing side degrades instead — the purchase figures still
  // render, with a band naming the missing half.
  if (loads === null) {
    return (
      <section className="card">
        <p className="muted">{copy(pageContract, "error.load.loads_unavailable")}</p>
      </section>
    );
  }
  const weighingDown = weights === null;
  const byLoad = new Map((weights?.by_load ?? []).map((bucket) => [bucket.load_ref, bucket]));

  const none = copy(pageContract, "load.no_figure");
  const suffix = copy(pageContract, "load.multiple.suffix");
  const unit = copy(pageContract, "unit.kg_per_head");

  type Row = {
    load: LoadwiseLoad;
    heading: string;
    purchasedAvg: number | null;
    latestAvg: number | null;
    multiple: number | null;
  };
  const rows: Row[] = loads
    .map((load) => {
      const bucket = load.load_ref ? byLoad.get(load.load_ref) : undefined;
      const purchasedAvg = load.avg_purchase_weight_kg ?? null;
      const latestAvg = bucket ? bucket.average_weight_kg : null;
      const multiple =
        purchasedAvg !== null && purchasedAvg > 0 && latestAvg !== null ? latestAvg / purchasedAvg : null;
      return { load, heading: loadHeading(load), purchasedAvg, latestAvg, multiple };
    })
    // ONLY loads with a latest weighing (maintainer request 2026-09-03): this tab compares, and a
    // load nobody has weighed since it arrived has nothing to compare. The note above the chart
    // says so. When the weighing read is down every load is kept, so the purchase half still
    // renders under its own "weighing unavailable" band rather than the tab going blank.
    .filter((row) => weighingDown || row.latestAvg !== null);

  // VALUE (maintainer request 2026-09-03). Purchased value is the load's LANDED cost; current
  // stock value is the animals still on farm at their latest average weight, priced at the
  // maintainer's assumed live-weight rate per species (backend-owned figures, printed on the
  // chart); gain is the difference. A sold-out load has no stock and gets no value bars.
  const rateSheep = Number(copy(pageContract, "load.rate.sheep_per_kg"));
  const rateGoat = Number(copy(pageContract, "load.rate.goat_per_kg"));
  const rupees = copy(pageContract, "unit.rupees");
  const valueGroups: BarGroup[] = rows.map((row) => {
    const bars: GroupedBar[] = [];
    const purchaseValue = row.load.purchase_value ?? null;
    const stockAnimals = row.load.remaining_sheep + row.load.remaining_goats;
    const stockValue =
      row.latestAvg !== null && stockAnimals > 0
        ? row.latestAvg * (row.load.remaining_sheep * rateSheep + row.load.remaining_goats * rateGoat)
        : null;
    if (purchaseValue !== null) {
      bars.push({ key: `${row.load.load_id}-pv`, label: copy(pageContract, "legend.load.purchase_value"), value: Math.round(purchaseValue), seriesKey: "purchase_value" });
    }
    if (stockValue !== null) {
      bars.push({ key: `${row.load.load_id}-sv`, label: copy(pageContract, "legend.load.stock_value"), value: Math.round(stockValue), seriesKey: "stock_value" });
    }
    if (purchaseValue !== null && stockValue !== null) {
      bars.push({ key: `${row.load.load_id}-gain`, label: copy(pageContract, "legend.load.gain"), value: Math.round(stockValue - purchaseValue), seriesKey: "gain" });
    }
    const subheading =
      stockAnimals === 0
        ? copy(pageContract, "load.value.sold_out")
        : purchaseValue === null
          ? copy(pageContract, "load.value.no_cost")
          : `${stockAnimals.toLocaleString("en-IN")} × ${kg(row.latestAvg ?? 0)} ${unit}`;
    return { key: `${row.load.load_id}-value`, heading: row.heading, subheading, bars };
  });

  const groups: BarGroup[] = rows.map((row) => {
    const bars: GroupedBar[] = [];
    if (row.purchasedAvg !== null) {
      bars.push({
        key: `${row.load.load_id}-purchased`,
        label: copy(pageContract, "legend.load.purchased"),
        value: Number(row.purchasedAvg.toFixed(1)),
        seriesKey: "purchased",
      });
    }
    if (row.latestAvg !== null) {
      bars.push({
        key: `${row.load.load_id}-latest`,
        label: copy(pageContract, "legend.load.latest"),
        value: Number(row.latestAvg.toFixed(1)),
        seriesKey: "latest",
      });
    }
    return {
      key: row.load.load_id,
      heading: row.heading,
      subheading: row.multiple !== null ? `${row.multiple.toFixed(1)}${suffix}` : undefined,
      bars,
    };
  });

  if (loads.length === 0) {
    return (
      <section className="card">
        <p className="muted small">{copy(pageContract, "empty.load.body")}</p>
      </section>
    );
  }

  return (
    <>
      {weighingDown ? (
        <section className="card" role="alert">
          <p className="muted">{copy(pageContract, "error.load.weighing_unavailable")}</p>
        </section>
      ) : null}

      <section className="card wchart" aria-label={copy(pageContract, "section.load.aria")}>
        <h2 className="h">
          <Scale className="ic" size={15} aria-hidden /> {copy(pageContract, "section.load.title")}
        </h2>
        <p className="muted small">{copy(pageContract, "section.load.caption")}</p>
        <p className="muted small">{copy(pageContract, "note.load.filters")}</p>
        <p className="muted small">{copy(pageContract, "note.load.weighed_only")}</p>
        <GroupedBars
          groups={groups}
          // Both sides ARE the same measure (kg per animal) over two moments, so they share
          // one scale — the entire comparison is the difference in bar length.
          series={[
            { key: "purchased", scaleKey: "kg", label: copy(pageContract, "legend.load.purchased"), unit: "kg", fractionDigits: 1 },
            { key: "latest", scaleKey: "kg", label: copy(pageContract, "legend.load.latest"), unit: "kg", fractionDigits: 1 },
          ]}
          emptyLabel={copy(pageContract, "empty.load.body")}
          chartLabel={copy(pageContract, "section.load.aria")}
        />
      </section>

      {/* VALUE. Three bars per load on ONE rupee scale -- purchased value, current stock value,
          and the gain between them -- so a loss draws below the baseline. The assumed rates are
          printed here, beside the chart, because a figure priced on an assumption must show it. */}
      <section className="card wchart" aria-label={copy(pageContract, "section.load_value.aria")}>
        <h2 className="h">
          <Scale className="ic" size={15} aria-hidden /> {copy(pageContract, "section.load_value.title")}
        </h2>
        <p className="muted small">{copy(pageContract, "section.load_value.caption")}</p>
        <p className="muted small">{copy(pageContract, "note.load.rates")}</p>
        <GroupedBars
          groups={valueGroups}
          series={[
            { key: "purchase_value", scaleKey: "inr", label: copy(pageContract, "legend.load.purchase_value"), unit: rupees, fractionDigits: 0 },
            { key: "stock_value", scaleKey: "inr", label: copy(pageContract, "legend.load.stock_value"), unit: rupees, fractionDigits: 0 },
            { key: "gain", scaleKey: "inr", label: copy(pageContract, "legend.load.gain"), unit: rupees, fractionDigits: 0 },
          ]}
          emptyLabel={copy(pageContract, "empty.load.body")}
          chartLabel={copy(pageContract, "section.load_value.aria")}
        />
      </section>

      <section className="card" style={{ marginTop: 12 }}>
        <h2 className="h">{copy(pageContract, "table.loads.title")}</h2>
        <p className="muted small">{copy(pageContract, "note.load.denominator")}</p>
        <div className="twrap" style={{ marginTop: 8 }}>
          <table className="loadwise-table" aria-label={copy(pageContract, "table.loads.title")}>
            <thead>
              <tr>
                <th>{copy(pageContract, "table.loads.load")}</th>
                <th>{copy(pageContract, "table.loads.vendor")}</th>
                <th className="num">{copy(pageContract, "table.loads.animals")}</th>
                <th className="num">{`${copy(pageContract, "table.loads.purchased_avg")} (${unit})`}</th>
                <th className="num">{`${copy(pageContract, "table.loads.latest_avg")} (${unit})`}</th>
                <th className="num">{copy(pageContract, "table.loads.multiple")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.load.load_id}>
                  <td>
                    <b>{row.heading}</b>
                  </td>
                  <td>{row.load.vendor_name || none}</td>
                  <td className="num">{row.load.purchased.toLocaleString("en-IN")}</td>
                  <td className="num">{row.purchasedAvg !== null ? kg(row.purchasedAvg) : none}</td>
                  <td className="num">{row.latestAvg !== null ? kg(row.latestAvg) : none}</td>
                  <td className="num">
                    {row.multiple !== null ? <b>{`${row.multiple.toFixed(1)}${suffix}`}</b> : none}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </>
  );
}
