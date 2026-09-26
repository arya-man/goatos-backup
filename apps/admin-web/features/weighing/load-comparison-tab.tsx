import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import Typography from "@mui/material/Typography";
import { EmptyState } from "@/components/app/empty-state";
import { BankingBalanceStatistics } from "@/components/minimal/sections/overview/banking/banking-balance-statistics";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import { pensFromPlacements, withLoadPens } from "@/lib/load-pens";
import type { GrowthSalePrice, ShedWeightsResponse } from "@/lib/api/server";
import { valueHeadMix } from "@/lib/sale-price";
import type { LoadwiseLoad, LoadwiseWeightLoad } from "@/lib/api/procurement";

/**
 * ADG Analytics › Load-wise — purchased weight against the latest weighing, load by load.
 *
 * Two backend-owned reads joined by the farm's own load number, never a third number of this
 * tab's invention: the purchase side is the procurement load ledger (`/procurement/loadwise-sales`,
 * purchase weight over animals bought), the weighing side is the SAME `/weighing/shed-weights`
 * by-load read the Weights page's load chart uses (the load's own animals, each at its latest
 * weight in the period, wherever it was weighed). The growth multiple is the one derived figure — latest ÷ purchase, per animal — and it
 * renders as absent whenever either side is missing, because a load with no tagged shed or no
 * recorded purchase weight has no honest multiple.
 *
 * The parent page fetches both reads (the weighing side over the SELECTED period: "latest
 * weighing" is each load animal's newest weigh inside it, followed through every pen move --
 * maintainer decision 2026-09-24) and passes them here; this component composes no data of its own.
 */

function kg(value: number): string {
  return value.toLocaleString("en-IN", { minimumFractionDigits: 1, maximumFractionDigits: 1 });
}

/** The chart/table heading for one load: its own number, else its source, else its date. */
function loadHeading(load: LoadwiseWeightLoad): string {
  return load.load_ref || load.vendor_name || fmtDate(load.purchase_date ?? undefined) || load.load_id;
}

/**
 * The pens one load's weighed animals sit in, as one line: the backend-composed operational
 * display for each contributing pen with its head count at that pen's latest weigh.
 *
 * The PARK is always named (maintainer, 2026-09-05), never only when a load spans two. A shed name
 * is not unique across parks -- this farm has a "Castro 1" in each -- and this tab is normally read
 * unfiltered, so a bare "Castro 1" leaves the reader guessing which building they are looking at.
 *
 * The names are DATA, composed by the backend (`operational_location_display`) and passed
 * through: this file never joins a shed name to a partition label itself. Empty when the load has
 * no weighed pen, which reads as an absent sub-line rather than an empty separator.
 */
function penList(bucket: { placements?: readonly { park_name: string; operational_location_display: string; animals: number }[] } | undefined): string {
  const placements = bucket?.placements ?? [];
  if (placements.length === 0) return "";
  return placements
    .map((p) => {
      // The park's own short code, backend-resolved; a pen whose park could not be resolved is
      // still named rather than dropped.
      const where = p.park_name ? `${p.park_name} ${p.operational_location_display}` : p.operational_location_display;
      return `${where} · ${p.animals.toLocaleString("en-IN")}`;
    })
    .join(", ");
}

export function LoadComparisonTab({
  pageContract,
  loads,
  weights,
  valueLoads = null,
  valueChartReason = "",
  salePrices,
}: {
  pageContract: AdminUiPageContract;
  /** The UNPRICED purchase rows (loadwise-weights); null when that read failed. */
  loads: LoadwiseWeightLoad[] | null;
  /** The by-load weighing read (the selected period); null when that read failed. */
  weights: ShedWeightsResponse | null;
  /** The assumed live-weight sale prices (growth_sale_price_assumptions); null when unread. */
  salePrices: GrowthSalePrice[] | null;
  /**
   * The PRICED rows (loadwise-sales), fetched only when the contract enabled the value chart
   * for this principal; null otherwise. Matched to `loads` by load_id.
   */
  valueLoads?: LoadwiseLoad[] | null;
  /** The backend's reason when the value chart is withheld; "" when it is enabled. */
  valueChartReason?: string;
}) {
  // The purchase ledger is the row set: without it there is nothing to compare, so its failure
  // is the tab's failure. The weighing side degrades instead — the purchase figures still
  // render, with a band naming the missing half.
  if (loads === null) {
    return (
      <Alert severity="error" variant="outlined">
        {copy(pageContract, "error.load.loads_unavailable")}
      </Alert>
    );
  }
  const weighingDown = weights === null;
  const byLoad = new Map((weights?.by_load ?? []).map((bucket) => [bucket.load_ref, bucket]));

  const none = copy(pageContract, "load.no_figure");
  const suffix = copy(pageContract, "load.multiple.suffix");
  const unit = copy(pageContract, "unit.kg_per_head");

  type Row = {
    load: LoadwiseWeightLoad;
    heading: string;
    purchasedAvg: number | null;
    latestAvg: number | null;
    multiple: number | null;
    /** Where this load's weighed animals actually are, with head counts — the TABLE's cell. */
    pens: string;
    /**
     * The CHART heading: the load's name with its pens in a bracket beside it (maintainer
     * request 2026-09-22, replacing the sub-line these two charts used to carry). One shape
     * across every load chart on the dashboard, composed by the shared helper.
     *
     * The head counts stay in the table and out of the bracket: the axis is one column wide,
     * and a heading is read at a glance while a cell is read deliberately.
     */
    chartHeading: string;
  };
  const rows: Row[] = loads
    .map((load) => {
      const bucket = load.load_ref ? byLoad.get(load.load_ref) : undefined;
      const purchasedAvg = load.avg_purchase_weight_kg ?? null;
      const latestAvg = bucket ? bucket.average_weight_kg : null;
      const multiple =
        purchasedAvg !== null && purchasedAvg > 0 && latestAvg !== null ? latestAvg / purchasedAvg : null;
      const heading = loadHeading(load);
      return {
        load,
        heading,
        chartHeading: withLoadPens(heading, pensFromPlacements(bucket?.placements)),
        purchasedAvg,
        latestAvg,
        multiple,
        pens: penList(bucket),
      };
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
  const priced = new Map((valueLoads ?? []).map((load) => [load.load_id, load]));
  // The rates are DATA (growth_sale_price_assumptions, maintainer decision 2026-09-07; edited
  // from the Weighing SOP Assumptions drawer since 2026-09-19), never page copy. Since 2026-09-24
  // each remaining animal is priced at its own (species, stage, sex) price, falling back to its
  // species' all-stages price; a load holding an animal with no price at all is not valued.
  const prices = salePrices ?? [];
  const defaults = prices.filter((price) => price.management_stage === "");
  const overrideCount = prices.length - defaults.length;
  const rupees = copy(pageContract, "unit.rupees");
  const ratesNote =
    defaults.length === 0
      ? copy(pageContract, "note.load.rates.missing")
      : `${copy(pageContract, "note.load.rates.prefix")} ${defaults
          .map((price) => `${price.species} ${rupees}${price.price_per_kg_inr.toLocaleString("en-IN")}`)
          .join(", ")}${overrideCount > 0 ? ` · ${overrideCount} ${copy(pageContract, "note.load.rates.overrides")}` : ""}`;
  const valueRows = rows.map((row) => {
    const purchaseValue = priced.get(row.load.load_id)?.purchase_value ?? null;
    const stockAnimals = row.load.remaining;
    const stockValue =
      row.latestAvg !== null && stockAnimals > 0 ? valueHeadMix(prices, row.load.remaining_mix ?? [], row.latestAvg) : null;
    const basis =
      stockAnimals === 0
        ? copy(pageContract, "load.value.sold_out")
        : purchaseValue === null
          ? copy(pageContract, "load.value.no_cost")
          : `${stockAnimals.toLocaleString("en-IN")} × ${kg(row.latestAvg ?? 0)} ${unit}`;
    // WHERE the money is standing, on the same terms as the weight chart above (maintainer request
    // 2026-09-21: name the pen on every graph). It rides the category in a bracket (2026-09-22),
    // the shape every other load chart on the dashboard uses. A SOLD-OUT load is named WITHOUT its
    // pens: it has no animals standing anywhere, so naming them would point at an empty pen.
    return {
      heading: stockAnimals === 0 ? row.heading : row.chartHeading,
      purchaseValue: purchaseValue === null ? null : Math.round(purchaseValue),
      stockValue: stockValue === null ? null : Math.round(stockValue),
      gain: purchaseValue !== null && stockValue !== null ? Math.round(stockValue - purchaseValue) : null,
      basis,
    };
  });

  if (loads.length === 0) {
    return (
      <Card>
        <EmptyState title={copy(pageContract, "empty.load.body")} />
      </Card>
    );
  }
  // Loads exist but the weighed-only filter kept none: say THAT, not "no purchased loads". The
  // weighing-down case never lands here, because the filter keeps every load when the weighing
  // read failed and the band below names the missing half instead.
  if (rows.length === 0) {
    return (
      <Card>
        <EmptyState title={copy(pageContract, "empty.load.unweighed.body")} description={copy(pageContract, "note.load.weighed_only")} />
      </Card>
    );
  }

  const loadCategories = rows.map((row) => row.chartHeading);
  const rupeeUnit = `${rupees}·`;

  return (
    <Grid container spacing={3}>
      {weighingDown ? (
        <Grid size={12}>
          <Alert severity="warning" variant="outlined">
            {copy(pageContract, "error.load.weighing_unavailable")}
          </Alert>
        </Grid>
      ) : null}

      {/* Both sides ARE the same measure (kg per animal) over two moments, so they share one scale:
          the entire comparison is the difference in bar length. The growth multiple rides each
          load's tooltip; WHERE the load is now rides its category in a bracket. */}
      <Grid size={12}>
        <BankingBalanceStatistics
          aria-label={copy(pageContract, "section.load.aria")}
          title={copy(pageContract, "section.load.title")}
          subheader={copy(pageContract, "section.load.caption")}
          empty={<EmptyState title={copy(pageContract, "empty.load.unweighed.body")} />}
          chart={{
            series: [
              {
                name: copy(pageContract, "section.load.title"),
                categories: loadCategories,
                unit: "kg",
                digits: 1,
                data: [
                  {
                    name: copy(pageContract, "legend.load.purchased"),
                    data: rows.map((row) => (row.purchasedAvg === null ? null : Number(row.purchasedAvg.toFixed(1)))),
                  },
                  {
                    name: copy(pageContract, "legend.load.latest"),
                    data: rows.map((row) => (row.latestAvg === null ? null : Number(row.latestAvg.toFixed(1)))),
                    notes: rows.map((row) => (row.multiple === null ? null : `${row.multiple.toFixed(1)}${suffix}`)),
                  },
                ],
              },
            ],
          }}
        />
      </Grid>

      {/* VALUE. Three bars per load on ONE rupee scale -- purchased value, current stock value,
          and the gain between them -- so a loss draws below the baseline. The assumed rates are
          printed beside the chart, because a figure priced on an assumption must show it.
          GATED: the money comes from the sales-only read, so a principal the contract withholds
          it from (the Growth Director on weighing access alone) sees the backend's reason. */}
      <Grid size={12}>
        <BankingBalanceStatistics
          aria-label={copy(pageContract, "section.load_value.aria")}
          title={copy(pageContract, "section.load_value.title")}
          subheader={copy(pageContract, "section.load_value.caption")}
          empty={
            <EmptyState
              title={
                valueChartReason !== ""
                  ? valueChartReason
                  : valueLoads === null
                    ? copy(pageContract, "error.load.loads_unavailable")
                    : copy(pageContract, "empty.load.unweighed.body")
              }
            />
          }
          chart={{
            series: [
              {
                name: copy(pageContract, "section.load_value.title"),
                categories: valueChartReason !== "" || valueLoads === null ? [] : valueRows.map((row) => row.heading),
                unit: rupeeUnit,
                data: [
                  { name: copy(pageContract, "legend.load.purchase_value"), data: valueRows.map((row) => row.purchaseValue) },
                  {
                    name: copy(pageContract, "legend.load.stock_value"),
                    data: valueRows.map((row) => row.stockValue),
                    notes: valueRows.map((row) => row.basis),
                  },
                  { name: copy(pageContract, "legend.load.gain"), data: valueRows.map((row) => row.gain) },
                ],
              },
            ],
          }}
        >
          <Typography variant="body2" sx={{ px: 3, pb: 3, color: "text.secondary" }}>
            {ratesNote}
          </Typography>
        </BankingBalanceStatistics>
      </Grid>

      <Grid size={12}>
        <Card>
          <CardHeader title={copy(pageContract, "table.loads.title")} subheader={copy(pageContract, "note.load.denominator")} sx={{ mb: 3 }} />
          <Box sx={{ overflowX: "auto" }} tabIndex={0} role="region" aria-label={copy(pageContract, "table.loads.title")}>
            <Table aria-label={copy(pageContract, "table.loads.title")}>
              <TableHead>
                <TableRow>
                  <TableCell component="th">{copy(pageContract, "table.loads.load")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "table.loads.vendor")}</TableCell>
                  <TableCell component="th" align="right">{copy(pageContract, "table.loads.animals")}</TableCell>
                  <TableCell component="th" align="right">{`${copy(pageContract, "table.loads.purchased_avg")} (${unit})`}</TableCell>
                  <TableCell component="th" align="right">{`${copy(pageContract, "table.loads.latest_avg")} (${unit})`}</TableCell>
                  <TableCell component="th" align="right">{copy(pageContract, "table.loads.multiple")}</TableCell>
                  <TableCell component="th">{copy(pageContract, "table.loads.pens")}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {rows.map((row) => (
                  <TableRow key={row.load.load_id} hover>
                    <TableCell sx={{ typography: "subtitle2" }}>{row.heading}</TableCell>
                    <TableCell>{row.load.vendor_name || none}</TableCell>
                    <TableCell align="right">{row.load.purchased.toLocaleString("en-IN")}</TableCell>
                    <TableCell align="right">{row.purchasedAvg !== null ? kg(row.purchasedAvg) : none}</TableCell>
                    <TableCell align="right">{row.latestAvg !== null ? kg(row.latestAvg) : none}</TableCell>
                    <TableCell align="right" sx={{ typography: "subtitle2" }}>
                      {row.multiple !== null ? `${row.multiple.toFixed(1)}${suffix}` : none}
                    </TableCell>
                    {/* The SAME line both charts carry, so the three surfaces on this tab name one
                        load's pens identically. A load with no weighed pen reads as absent. */}
                    <TableCell>{row.pens || none}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Box>
          <Typography variant="body2" sx={{ p: 3, color: "text.secondary" }}>
            {copy(pageContract, "note.load.filters")}
          </Typography>
        </Card>
      </Grid>
    </Grid>
  );
}
