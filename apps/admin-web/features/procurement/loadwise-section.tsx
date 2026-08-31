import type { ReactNode } from "react";

import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { Boxes } from "lucide-react";

import { GroupedColumns, type GroupedSeries } from "@/components/grouped-columns";
import { Tag } from "@/components/ui-primitives";
import { copy, optionGroup, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { LoadwiseLoad, LoadwiseSales } from "@/lib/api/procurement";
import type { ApiResult } from "@/lib/api/server";
import { humanDate, inr, inrCompact, num } from "./sales-format";
import type { LoadwisePriorOutcome } from "@/lib/api/procurement";

/**
 * Tooltip line for a count that includes pre-GoatOS history: the copy's label, the count, and the
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
function loadLabel(load: LoadwiseLoad, loadWord: string, none: string): string {
  const vendor = load.vendor_name.trim() === "" ? none : load.vendor_name;
  if (load.load_ref) return `${loadWord} ${load.load_ref} · ${vendor}`;
  return load.purchase_date ? `${vendor} · ${humanDate(load.purchase_date)}` : vendor;
}

export function LoadwiseSection({
  pageContract,
  view,
  tabHref,
  loadwise,
  canRecordCost,
  costHref,
}: {
  pageContract: AdminUiPageContract;
  /** The validated ?view= tab key (an option key of sales_views). */
  view: string;
  /** Link builder for the tab chips, preserving every other selected search param. */
  tabHref: (view: string) => string;
  /** The load-wise read, or null when the Purchased tab is not the one being rendered. */
  loadwise: ApiResult<LoadwiseSales> | null;
  /** Backend-declared record_load_cost capability; without it rows do not open the cost drawer. */
  canRecordCost: boolean;
  /** Link builder for one load's cost drawer (LocalOverlayLink, no RSC request). */
  costHref: (loadId: string) => string;
}) {
  const none = copy(pageContract, "value.none");
  const loadWord = copy(pageContract, "column.load");
  const views = optionGroup(pageContract, "sales_views");
  const columns = tableLabels(pageContract, "sales-loadwise");

  const countSeries: GroupedSeries[] = [
    { key: "purchased", label: copy(pageContract, "chart.series.purchased"), tone: "info" },
    { key: "sold", label: copy(pageContract, "chart.series.sold_count"), tone: "ok" },
    { key: "mortality", label: copy(pageContract, "chart.series.mortality"), tone: "danger" },
    { key: "remaining", label: copy(pageContract, "chart.series.remaining"), tone: "teal" },
  ];
  const valueSeries: GroupedSeries[] = [
    { key: "purchase_value", label: copy(pageContract, "chart.series.purchase_value"), tone: "info" },
    { key: "sold_value", label: copy(pageContract, "chart.series.sold_value"), tone: "ok" },
    { key: "remaining_value", label: copy(pageContract, "chart.series.remaining_value"), tone: "teal" },
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
        <div className="sp" style={{ flex: 1 }} />
        {/* The two tabs. Server-rendered links so the selection survives reload and a shared URL. */}
        <div className="chips" role="group" aria-label={copy(pageContract, "section.loadwise.title")}>
          {views.map((option) => (
            <Link
              key={option.key}
              href={tabHref(option.key)}
              scroll={false}
              className={option.key === view ? "btn sm p" : "btn sm"}
              aria-current={option.key === view ? "true" : undefined}
            >
              {option.label}
            </Link>
          ))}
        </div>
      </div>
      <div className="muted small" style={{ marginTop: 2 }}>
        {copy(pageContract, "section.loadwise.subtitle")}
      </div>

      {view === "from_barn" ? (
        <div className="empty" style={{ marginTop: 12 }}>
          {copy(pageContract, "empty.from_barn")}
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
                <div className="lab">{copy(pageContract, "loadwise.kpi.remaining_value")}</div>
                <div className="val">{summary.remaining_value > 0 ? inrCompact(summary.remaining_value) : none}</div>
                <div className="dl">{copy(pageContract, "loadwise.kpi.remaining_value.hint")}</div>
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
              values: [load.purchase_value ?? null, load.sold_value > 0 ? load.sold_value : null, load.remaining_value ?? null],
              displays: [
                load.purchase_value == null ? copy(pageContract, "value.cost_missing") : inrCompact(load.purchase_value),
                inrCompact(load.sold_value),
                load.remaining_value == null ? none : inrCompact(load.remaining_value),
              ],
              subLabel: `${num(load.sold)} / ${num(load.purchased)} ${copy(pageContract, "loadwise.kpi.sold").toLowerCase()}`,
            }))}
          />

          {/* The reconciliation table. */}
          <div className="twrap" style={{ marginTop: 12 }}>
            <table aria-label={copy(pageContract, "section.loadwise.aria")}>
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
                      {cell(num(load.other_exits), "num")}
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
                        load.remaining_value == null ? (
                          <span className="muted">{none}</span>
                        ) : (
                          <span title={copy(pageContract, `value.price_basis.${load.price_basis}`)}>
                            {inr(Math.round(load.remaining_value))}
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
