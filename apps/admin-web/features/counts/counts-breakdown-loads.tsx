import { GroupedColumns, type GroupedSeries } from "@/components/grouped-columns";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { CountsBreakdownResponse } from "@/lib/api/server";
import { fmtDate } from "@/lib/format";
import { withLoadPens, type LoadPen } from "@/lib/load-pens";

// Counts Breakdown -> Purchased loads: the last card on the page (maintainer request 2026-09-18).
// One column group per purchased load — animals bought, still on farm under the current filters,
// male and female — repeated as a table under the chart. No tag column (maintainer request
// 2026-09-18: the per-animal identifiers were noise here; a load with 58 animals listed 58 chips).
// A SERVER component that renders the backend's own per-load
// figures verbatim: `on_farm` is the backend's sum of its `stages` and of its `sexes`, and nothing
// here re-sums either.

type LoadRow = CountsBreakdownResponse["loads"][number];
type Point = LoadRow["stages"][number];

function countFor(points: Point[], key: string): number {
  return points.find((point) => point.key === key)?.count ?? 0;
}

export function CountsBreakdownLoads({
  loads,
  pageContract,
  genderLabels,
}: {
  loads: LoadRow[];
  pageContract: AdminUiPageContract;
  /** Backend gender vocabulary (key -> label), so the male/female series never invent copy. */
  genderLabels: Map<string, string>;
}) {
  const loadWord = copy(pageContract, "label.load");
  const unnumbered = copy(pageContract, "label.load_unnumbered");
  const maleKey = "male";
  const femaleKey = "female";

  const series: GroupedSeries[] = [
    // Slots of the chart ramp in its fixed order (mesha-theme.css `--chart-1..4`), so the four bars
    // of one load separate for every reader. `teal` and `warn` are the SAME slot, which is what put
    // two of these bars in one colour before.
    { key: "purchased", label: copy(pageContract, "chart.series.load_purchased"), tone: "info" },
    { key: "on_farm", label: copy(pageContract, "chart.series.load_on_farm"), tone: "ok" },
    { key: "male", label: genderLabels.get(maleKey) ?? copy(pageContract, "chart.series.load_male"), tone: "danger" },
    { key: "female", label: genderLabels.get(femaleKey) ?? copy(pageContract, "chart.series.load_female"), tone: "warn" },
  ];

  const loadTitle = (load: LoadRow) => (load.load_ref ? `${loadWord} ${load.load_ref}` : unnumbered);
  const loadLabel = (load: LoadRow) => {
    const vendor = load.vendor_name.trim();
    return vendor ? `${loadTitle(load)} · ${vendor}` : loadTitle(load);
  };

  // The pens beside the load name (maintainer request 2026-09-22). These are the HERD REGISTER's
  // pens -- where the load's filtered live animals sit NOW -- which is the same key set on_farm
  // and the stage/sex splits on this very chart are rolled over, so the bracket and the bars
  // agree. The Weights and ADG load charts name the pens a load was WEIGHED in, a different
  // question; the two are not meant to match.
  const pensOf = (load: LoadRow): LoadPen[] =>
    (load.pens ?? []).map((pen) => ({
      park: pen.park_name,
      pen: pen.operational_location_display,
      animals: pen.animals,
    }));

  const columns = [
    "column.load",
    "column.load_vendor",
    "column.load_bought_on",
    "column.load_purchased",
    "column.load_on_farm",
    "column.load_male",
    "column.load_female",
  ] as const;

  return (
    <section className="card" aria-label={copy(pageContract, "section.loads.aria")} style={{ marginTop: 16 }}>
      <div className="hd">
        <h3>{copy(pageContract, "section.loads.title")}</h3>
        <span className="small muted">{copy(pageContract, "section.loads.caption")}</span>
      </div>
      <div className="bd">
        <GroupedColumns
          series={series}
          chartLabel={copy(pageContract, "chart.loads.aria")}
          emptyLabel={copy(pageContract, "chart.loads.empty")}
          data={loads.map((load) => {
            const male = countFor(load.sexes, maleKey);
            const female = countFor(load.sexes, femaleKey);
            return {
              key: load.load_id,
              axisLabel: withLoadPens(
                load.load_ref ? load.load_ref : load.purchase_date ? fmtDate(load.purchase_date) : unnumbered,
                pensOf(load),
              ),
              label: withLoadPens(loadLabel(load), pensOf(load), Number.POSITIVE_INFINITY),
              values: [load.purchased, load.on_farm, male, female],
              displays: [String(load.purchased), String(load.on_farm), String(male), String(female)],
              subLabel: load.vendor_name,
            };
          })}
        />
      </div>
      {loads.length > 0 ? (
        <div className="bd" style={{ padding: 0, overflowX: "auto" }}>
          <table className="tbl" aria-label={copy(pageContract, "table.loads.aria")}>
            <thead>
              <tr>
                {columns.map((key) => (
                  <th
                    key={key}
                    style={key.endsWith("purchased") || key.endsWith("on_farm") || key.endsWith("male") ? { textAlign: "right" } : undefined}
                  >
                    {copy(pageContract, key)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {loads.map((load) => (
                <tr key={load.load_id}>
                  <td>
                    <b>{loadTitle(load)}</b>
                  </td>
                  <td className="muted">{load.vendor_name || "—"}</td>
                  <td className="muted">{fmtDate(load.purchase_date || undefined)}</td>
                  <td style={{ textAlign: "right" }}>{load.purchased}</td>
                  <td style={{ textAlign: "right" }}>
                    <b>{load.on_farm}</b>
                  </td>
                  <td style={{ textAlign: "right" }}>{countFor(load.sexes, maleKey)}</td>
                  <td style={{ textAlign: "right" }}>{countFor(load.sexes, femaleKey)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </section>
  );
}
