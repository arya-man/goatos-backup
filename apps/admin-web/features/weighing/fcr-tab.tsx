import { CalendarRange, Scale, Sprout, Warehouse, Wheat } from "lucide-react";

import { WorklistPager } from "@/components/worklist-pager";
import { FCRPensTable } from "./fcr-pens-table";
import { cohortWord } from "./fcr-labels";
import { GroupedBars, type BarGroup, type GroupedBar } from "./grouped-bars";
import { WeightBars } from "./weight-bars";
import { copy, table, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import type { GrowthFCRGroup, GrowthFCRResponse, GrowthFCRPen } from "@/lib/api/server";

/**
 * ADG Analytics › FCR (maintainer request 2026-09-07) — feed conversion ratio: kilograms of feed
 * directed to a pen per kilogram the pen gained over the same days. Lower is better.
 *
 * ONE backend read (`/growth-director/fcr`) carries every figure on this tab: the KPI strip, each
 * pen, every cut and the weekly series are computed by the backend over the whole filter, and this
 * component divides nothing. It does not even compute a percentage: weighing has no expected
 * denominator, and a ratio this tab might invent from two served figures would disagree with the
 * one the backend already published.
 *
 * Absence is load-bearing. A pen weighed once, a pen whose sheet has no rows, and a pen that did
 * not gain each have NO honest ratio; they stay in the table with their status and out of every
 * chart, and the caption says so.
 */

const num = (value: number, digits = 1) =>
  value.toLocaleString("en-IN", { minimumFractionDigits: digits, maximumFractionDigits: digits });

function KPI({ label, value, unit, sub, tone }: { label: string; value: string; unit?: string; sub?: React.ReactNode; tone?: "ok" | "warn" }) {
  return (
    <div className="kpi">
      <div className="lab">{label}</div>
      <div className="val">
        {value} {unit ? <small className="fcr-kpi-unit" style={{ color: "var(--muted)" }}>{unit}</small> : null}
      </div>
      {sub ? <div className={`dl ${tone === "warn" ? "warn" : "muted"}`}>{sub}</div> : null}
    </div>
  );
}

/** "1 pen", "12 pens": the count and the right word, both from the page contract. */
function penCount(pageContract: AdminUiPageContract, n: number): string {
  return `${n.toLocaleString("en-IN")} ${copy(pageContract, n === 1 ? "value.fcr.pen" : "value.fcr.pens")}`;
}

function groupLabel(pageContract: AdminUiPageContract, group: GrowthFCRGroup, id: string): string {
  // "Mixed" names a different fact on each card: several breeds, both sexes, or bought and born.
  if (group.key === "mixed" && id === "breed") return copy(pageContract, "label.fcr.mixed_breed");
  if (group.key === "mixed" && id === "sex") return copy(pageContract, "label.fcr.mixed_sex");
  switch (group.key) {
    case "male":
      return copy(pageContract, "view.sex.male");
    case "female":
      return copy(pageContract, "view.sex.female");
    case "mixed":
      return copy(pageContract, "label.fcr.mixed");
    case "unknown":
      return copy(pageContract, "label.fcr.unknown");
    case "farm_born":
      return copy(pageContract, "label.fcr.farm_born");
    case "procured_no_load":
      return copy(pageContract, "label.fcr.procured_no_load");
    case "procured_load":
      return copy(pageContract, "label.fcr.procured_load");
    default:
      return group.label;
  }
}

/** One bar per group, on one FCR scale, with the pen count beside it. */
function groupBars(pageContract: AdminUiPageContract, groups: GrowthFCRGroup[], id: string): BarGroup[] {
  return groups
    .filter((group) => group.fcr != null)
    .map((group) => ({
      key: group.key,
      heading: groupLabel(pageContract, group, id),
      bars: [
        {
          key: `${group.key}-fcr`,
          label: copy(pageContract, "series.fcr"),
          value: Number((group.fcr as number).toFixed(2)),
          seriesKey: "fcr",
          noteLabel: penCount(pageContract, group.pens),
        } satisfies GroupedBar,
      ],
    }));
}

function GroupCard({
  pageContract,
  id,
  groups,
  icon,
  rupee,
}: {
  pageContract: AdminUiPageContract;
  id: string;
  groups: GrowthFCRGroup[];
  icon: React.ReactNode;
  rupee: string;
}) {
  const caption = copy(pageContract, `section.fcr.${id}.caption`, "");
  const safeGroups = groups ?? [];
  // Two series on two scales: the ratio, and the money the group made over its feed. A group
  // whose gain is unpriced or whose feed is unpriced shows the ratio alone.
  const bars = groupBars(pageContract, safeGroups, id).map((group) => {
    const src = safeGroups.find((g) => g.key === group.key);
    if (!src || src.margin_inr == null) return group;
    return {
      ...group,
      bars: [
        ...group.bars,
        { key: `${group.key}-margin`, label: copy(pageContract, "legend.fcr.margin"), value: Math.round(src.margin_inr), seriesKey: "margin" } satisfies GroupedBar,
      ],
    };
  });
  return (
    <section className="card wchart" aria-label={copy(pageContract, `section.fcr.${id}.aria`)}>
      <h2 className="h">
        {icon} {copy(pageContract, `section.fcr.${id}.title`)}
      </h2>
      {caption ? <p className="muted small">{caption}</p> : null}
      <GroupedBars
        groups={bars}
        series={[
          { key: "fcr", label: copy(pageContract, "series.fcr"), unit: copy(pageContract, "unit.fcr"), fractionDigits: 2 },
          { key: "margin", scaleKey: "inr", label: copy(pageContract, "legend.fcr.margin"), unit: rupee, fractionDigits: 0 },
        ]}
        emptyLabel={copy(pageContract, "empty.fcr.body")}
        chartLabel={copy(pageContract, `section.fcr.${id}.aria`)}
      />
    </section>
  );
}

export function FCRTab({
  pageContract,
  fcr,
  pager,
}: {
  pageContract: AdminUiPageContract;
  fcr: GrowthFCRResponse | null;
  /** The page's shared offset/limit query params, so the pens table pages like the shed table does. */
  pager: { offset: number; limit: number; pageSizeOptions: readonly number[]; hrefForOffset: (offset: number) => string; hrefForLimit: (limit: number) => string };
}) {
  if (fcr === null) {
    return (
      <section className="card" role="alert">
        <p className="muted">{copy(pageContract, "error.fcr.body")}</p>
      </section>
    );
  }
  const none = copy(pageContract, "kpi.fcr.no_value");
  const rupee = copy(pageContract, "unit.fcr.rupees");
  const priceDefaults = fcr.sale_prices.filter((price) => price.management_stage === "");
  const priceOverrides = fcr.sale_prices.length - priceDefaults.length;
  const s = fcr.summary;
  const money = (value: number | null | undefined) => (value == null ? none : `${rupee}${num(value, 0)}`);
  const visiblePens = fcr.pens.slice(pager.offset, pager.offset + pager.limit);

  const tableLabels = {
    ariaLabel: copy(pageContract, "table.fcr.title"),
    noValue: none,
    mixedBreed: copy(pageContract, "label.fcr.mixed_breed"),
    mixedSex: copy(pageContract, "label.fcr.mixed_sex"),
    unknown: copy(pageContract, "label.fcr.unknown"),
    male: copy(pageContract, "view.sex.male"),
    female: copy(pageContract, "view.sex.female"),
    wholePen: copy(pageContract, "label.fcr.whole_pen"),
    scanned: copy(pageContract, "label.fcr.scanned"),
    status: {
      ok: copy(pageContract, "table.fcr.status.ok"),
      blocked: copy(pageContract, "table.fcr.status.blocked"),
      weighed_once: copy(pageContract, "table.fcr.status.weighed_once"),
      no_feed: copy(pageContract, "table.fcr.status.no_feed"),
      no_gain: copy(pageContract, "table.fcr.status.no_gain"),
    },
    unpriced: copy(pageContract, "table.fcr.unpriced"),
    rupee,
    empty: <span className="muted small">{copy(pageContract, "empty.fcr.body")}</span>,
  };

  // Pens with a ratio as horizontal bars on one FCR scale, in the contract's own order: park
  // clusters (CBE, then CPT) and pens A→Z inside each, the backend's order for every All-parks surface.
  const penBars = fcr.pens
    .filter((pen): pen is GrowthFCRPen & { fcr: number } => pen.fcr != null)
    .map((pen) => ({
      key: `${pen.location_id}|${pen.partition_label}`,
      label: pen.operational_location_display,
      value: Number(pen.fcr.toFixed(2)),
      valueLabel: num(pen.fcr, 2),
      // The same words the table's cohort column uses, so a chip never shows a raw register key.
      modeLabel: `${cohortWord(pen.breed, tableLabels, "breed")} · ${cohortWord(pen.sex, tableLabels, "sex")}`,
      modeTone: (pen.breed === "mixed" ? "info" : "mut") as "info" | "mut",
    }));

  // Money by pen: gain value, feed cost and money made on ONE rupee scale, so a loss draws below
  // the baseline. Pens whose gain is unpriced (no species price) or whose feed bill is unpriced
  // show only the half they have.
  const moneyGroups: BarGroup[] = fcr.pens
    .filter((pen) => pen.fcr != null && (pen.gain_value_inr != null || pen.feed_cost_inr != null))
    .map((pen) => {
      const bars: GroupedBar[] = [];
      if (pen.gain_value_inr != null) bars.push({ key: `${pen.location_id}|${pen.partition_label}-gv`, label: copy(pageContract, "legend.fcr.gain_value"), value: Math.round(pen.gain_value_inr), seriesKey: "gain_value" });
      if (pen.feed_cost_inr != null) bars.push({ key: `${pen.location_id}|${pen.partition_label}-fc`, label: copy(pageContract, "legend.fcr.feed_cost"), value: Math.round(pen.feed_cost_inr), seriesKey: "feed_cost" });
      if (pen.margin_inr != null) bars.push({ key: `${pen.location_id}|${pen.partition_label}-m`, label: copy(pageContract, "legend.fcr.margin"), value: Math.round(pen.margin_inr), seriesKey: "margin" });
      return { key: `${pen.location_id}|${pen.partition_label}`, heading: pen.operational_location_display, bars };
    });

  const weekBars = fcr.weekly
    .filter((week) => week.fcr != null)
    .map((week) => ({
      key: week.week_start,
      label: fmtDate(week.week_start),
      value: Number((week.fcr as number).toFixed(2)),
      valueLabel: num(week.fcr as number, 2),
      modeLabel: penCount(pageContract, week.pens),
      modeTone: "mut" as const,
    }));

  const fcrTable = table(pageContract, "fcr-pens");

  return (
    <>
      <div className="grid g5 kpi-row">
        <KPI
          label={copy(pageContract, "kpi.fcr.farm.label")}
          value={s.fcr == null ? none : num(s.fcr, 2)}
          unit={copy(pageContract, "kpi.fcr.farm.unit")}
          sub={`${penCount(pageContract, s.pens_with_fcr)} · ${s.animals.toLocaleString("en-IN")} ${copy(pageContract, "value.fcr.kids")}`}
        />
        <KPI label={copy(pageContract, "kpi.fcr.gain_value.label")} value={money(s.gain_value_inr)} sub={`${num(s.gain_kg, 0)} kg · ${copy(pageContract, "kpi.fcr.gain_value.sub")}`} />
        <KPI label={copy(pageContract, "kpi.fcr.feed_cost.label")} value={money(s.feed_cost_inr)} sub={`${num(s.feed_kg, 0)} kg · ${money(s.feed_cost_per_kg_gain_inr)} ${copy(pageContract, "kpi.fcr.cost_gain.label").toLowerCase()}`} />
        <KPI
          label={copy(pageContract, "kpi.fcr.margin.label")}
          value={money(s.margin_inr)}
          sub={s.margin_inr != null && s.margin_inr < 0 ? copy(pageContract, "kpi.fcr.margin.loss") : copy(pageContract, "kpi.fcr.margin.sub")}
          tone={s.margin_inr != null && s.margin_inr < 0 ? "warn" : undefined}
        />
        <KPI label={copy(pageContract, "kpi.fcr.break_even.label")} value={s.break_even_fcr == null ? none : num(s.break_even_fcr, 1)} sub={copy(pageContract, "kpi.fcr.break_even.sub")} />
      </div>

      {/* The price the gain is valued at: maintainer-edited DATA, printed beside the figures it
          prices, with who set it and when, because a figure priced on an assumption must show it. */}
      <section className="card" style={{ padding: "10px 16px" }}>
        <p className="muted small" style={{ margin: 0 }}>
          {priceDefaults.length === 0 ? (
            copy(pageContract, "fcr.price.missing")
          ) : (
            <>
              {copy(pageContract, "fcr.price.prefix")}{" "}
              {priceDefaults.map((price, i) => (
                <span key={price.species}>
                  {i > 0 ? " · " : ""}
                  <b>
                    {rupee}
                    {num(price.price_per_kg_inr, 0)}
                  </b>{" "}
                  {copy(pageContract, "fcr.price.per_kg")} ({price.species}) — {copy(pageContract, "fcr.price.set")} {fmtDate(price.effective_from)}
                  {price.set_by ? ` ${copy(pageContract, "fcr.price.by")} ${price.set_by}` : ""}
                </span>
              ))}
              {/* Stage x sex prices (maintainer decision 2026-09-24) sit on top of these defaults; each
                  animal in a pen is valued at its own, so the caption says how many are in force. */}
              {priceOverrides > 0 ? ` · ${priceOverrides} ${copy(pageContract, "fcr.price.overrides")}` : ""}
              {" · "}
              {copy(pageContract, "fcr.price.shared")}
            </>
          )}
        </p>
      </section>

      <section className="card wchart" aria-label={copy(pageContract, "section.fcr.pens.aria")}>
        <h2 className="h">
          <Wheat className="ic" size={15} aria-hidden /> {copy(pageContract, "section.fcr.pens.title")}
        </h2>
        <p className="muted small">{copy(pageContract, "section.fcr.pens.caption")}</p>
        <WeightBars
          data={penBars}
          emptyLabel={copy(pageContract, "empty.fcr.body")}
          unit={copy(pageContract, "unit.fcr")}
          chartLabel={copy(pageContract, "section.fcr.pens.aria")}
          size="tall"
          wide
          // The caption promises a dashed break-even line; it is drawn on every pen's track, so a pen
          // past it reads as losing money without comparing two numbers.
          reference={s.break_even_fcr != null ? { value: s.break_even_fcr, label: copy(pageContract, "label.fcr.break_even") } : undefined}
        />
        {s.break_even_fcr != null ? (
          <p className="muted small">
            {copy(pageContract, "label.fcr.break_even")}: <b>{num(s.break_even_fcr, 1)}</b> {copy(pageContract, "unit.fcr")}
          </p>
        ) : null}
        <p className="muted small">{copy(pageContract, "note.fcr.excluded")}</p>
      </section>

      <section className="card wchart" aria-label={copy(pageContract, "section.fcr.money.aria")}>
        <h2 className="h">
          <Scale className="ic" size={15} aria-hidden /> {copy(pageContract, "section.fcr.money.title")}
        </h2>
        <p className="muted small">{copy(pageContract, "section.fcr.money.caption")}</p>
        <GroupedBars
          groups={moneyGroups}
          series={[
            { key: "gain_value", scaleKey: "inr", label: copy(pageContract, "legend.fcr.gain_value"), unit: rupee, fractionDigits: 0 },
            { key: "feed_cost", scaleKey: "inr", label: copy(pageContract, "legend.fcr.feed_cost"), unit: rupee, fractionDigits: 0 },
            { key: "margin", scaleKey: "inr", label: copy(pageContract, "legend.fcr.margin"), unit: rupee, fractionDigits: 0 },
          ]}
          emptyLabel={copy(pageContract, "empty.fcr.body")}
          chartLabel={copy(pageContract, "section.fcr.money.aria")}
        />
      </section>

      <div className="grid g2">
        <GroupCard pageContract={pageContract} id="breed" groups={fcr.estimated_by_breed ?? fcr.by_breed} icon={<Sprout className="ic" size={15} aria-hidden />} rupee={rupee} />
        <div style={{ display: "grid", gap: 14, alignSelf: "start" }}>
          <GroupCard pageContract={pageContract} id="sex" groups={fcr.by_sex} icon={<Sprout className="ic" size={15} aria-hidden />} rupee={rupee} />
          <section className="card wchart" aria-label={copy(pageContract, "section.fcr.weekly.aria")}>
            <h2 className="h">
              <CalendarRange className="ic" size={15} aria-hidden /> {copy(pageContract, "section.fcr.weekly.title")}
            </h2>
            <p className="muted small">{copy(pageContract, "section.fcr.weekly.caption")}</p>
            <WeightBars
              data={weekBars}
              emptyLabel={copy(pageContract, "empty.fcr.body")}
              unit={copy(pageContract, "unit.fcr")}
              chartLabel={copy(pageContract, "section.fcr.weekly.aria")}
              size="bands"
              wide
            />
          </section>
        </div>
      </div>

      <div className="grid g2">
        <GroupCard pageContract={pageContract} id="band" groups={fcr.by_weight_band} icon={<Scale className="ic" size={15} aria-hidden />} rupee={rupee} />
        <GroupCard pageContract={pageContract} id="park" groups={fcr.by_park} icon={<Warehouse className="ic" size={15} aria-hidden />} rupee={rupee} />
      </div>
      <GroupCard pageContract={pageContract} id="origin" groups={fcr.by_origin} icon={<Warehouse className="ic" size={15} aria-hidden />} rupee={rupee} />

      <section className="card wtable" aria-label={copy(pageContract, "table.fcr.aria")}>
        <h2 className="h">{copy(pageContract, "table.fcr.title")}</h2>
        <p className="muted small">{copy(pageContract, "table.fcr.caption")}</p>
        {/* Paged on the page's shared offset/limit, exactly like the shed table on the Pen-wise tab:
            the rows are one read, the window is a query param, and the pager is the shared one. */}
        <div className="tablewrap" tabIndex={0} role="group" aria-label={copy(pageContract, "table.fcr.aria")}>
          <FCRPensTable contract={fcrTable} rows={visiblePens} labels={tableLabels} />
        </div>
        <WorklistPager
          pageContract={pageContract}
          offset={pager.offset}
          limit={pager.limit}
          rowCount={visiblePens.length}
          hasMore={pager.offset + visiblePens.length < fcr.pens.length}
          noun={copy(pageContract, "pager.noun")}
          pageSizeOptions={pager.pageSizeOptions}
          hrefForOffset={pager.hrefForOffset}
          hrefForLimit={pager.hrefForLimit}
        />
        <p className="muted small">{copy(pageContract, "note.fcr.basis")}</p>
        <p className="muted small">{copy(pageContract, "note.fcr.filters")}</p>
      </section>
    </>
  );
}
