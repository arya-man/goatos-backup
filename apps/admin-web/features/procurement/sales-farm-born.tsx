import { redirect } from "next/navigation";

import Link from "@/components/no-prefetch-link";
import { WorklistFilters, type WorklistFilterField } from "@/components/worklist-filters";
import { copy, optionGroup, table, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getFarmBornSales } from "@/lib/api/procurement-server";
import type { FarmBornBucket, FarmBornSales } from "@/lib/api/procurement";
import { todayIso } from "@/lib/format";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { humanDate, inr, num } from "./sales-format";
import { SalesPageHeader } from "./sales-chrome";
import { FarmBornSoldTable } from "./farm-born-sold-table";

const PAGE_PATH = "/sales/farm-born";
/** Only used when an older backend contract carries no sold table; the contract page size wins. */
const FALLBACK_LIMIT = 25;
const MAX_OFFSET = 10000;
/** The By pen card pages its rows: a farm has dozens of pens and the card sat 1,600px tall. */
const PEN_PAGE_SIZE = 10;
const PEN_OFFSET_PARAM = "pen_offset";

/** Fills a backend copy template's `{name}` slots; the sentence itself stays backend-owned. */
function fill(template: string, values: Record<string, string>): string {
  return template.replace(/\{(\w+)\}/g, (match, key: string) => values[key] ?? match);
}

/**
 * One calendar month before an ISO day, the way the backend's DefaultFarmBornWindow counts it
 * (Go's AddDate(0, -1, 0) and Date.UTC both normalise "31 Feb" forward to 3 Mar), so the picker
 * recognises the backend's own default window and clears the parameters for it.
 */
function monthBefore(day: string): string {
  const [year, month, date] = day.split("-").map(Number);
  const shifted = new Date(Date.UTC(year, month - 2, date));
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${shifted.getUTCFullYear()}-${pad(shifted.getUTCMonth() + 1)}-${pad(shifted.getUTCDate())}`;
}

function hrefWith(sp: RouteSearchParams, patch: Record<string, string | null>): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(sp)) {
    const single = Array.isArray(value) ? value[0] : value;
    if (single) query.set(key, single);
  }
  for (const [key, value] of Object.entries(patch)) {
    if (value === null || value === "") query.delete(key);
    else query.set(key, value);
  }
  const qs = query.toString();
  return qs ? `${PAGE_PATH}?${qs}` : PAGE_PATH;
}

/** A requested page size is honoured only when the contract offers it. */
function resolveLimit(raw: string | undefined, offered: readonly number[], fallback: number): number {
  const parsed = raw ? Number.parseInt(raw, 10) : Number.NaN;
  if (Number.isFinite(parsed) && offered.includes(parsed)) return parsed;
  return fallback;
}

/**
 * One breakdown card: the same animals split one way, on farm today beside sold in the period.
 * Every figure is the backend's; the share is the row's sold over the whole-filter sold, which
 * the payload guarantees the rows sum to.
 */
function BreakdownCard({
  title,
  rows,
  totalSold,
  pageContract,
  pager,
}: {
  title: string;
  rows: FarmBornBucket[];
  totalSold: number;
  pageContract: AdminUiPageContract;
  /**
   * Optional page window over `rows`. The rows are the WHOLE breakdown (so the share column is
   * still over the whole-filter sold count); only the slice shown moves. Server-rendered links,
   * so the page survives a reload and a shared URL.
   */
  pager?: { offset: number; limit: number; noun: string; href: (offset: number) => string };
}) {
  // A filter change can leave a pen offset past the end of a now-shorter list (the bar resets the
  // ledger offset, not this one); an out-of-range page falls back to the first rather than an
  // empty card.
  if (pager && pager.offset >= rows.length) pager = { ...pager, offset: 0 };
  const shown = pager ? rows.slice(pager.offset, pager.offset + pager.limit) : rows;
  const pageCount = pager ? Math.max(1, Math.ceil(rows.length / pager.limit)) : 1;
  const pageNumber = pager ? Math.floor(pager.offset / pager.limit) + 1 : 1;
  return (
    <section className="card" aria-label={title}>
      <div className="hd">
        <h3>{title}</h3>
        {pager && pageCount > 1 ? (
          <>
            <div className="sp" style={{ flex: 1 }} />
            <span className="muted small">
              {copy(pageContract, "pager.page")} {pageNumber} {copy(pageContract, "pager.of")} {pageCount} ·{" "}
              {num(rows.length)} {pager.noun}
            </span>
          </>
        ) : null}
      </div>
      <div className="twrap" tabIndex={0} role="region" aria-label={title}>
        <table className="tbl">
          <thead>
            <tr>
              <th />
              <th className="num">{copy(pageContract, "column.on_farm")}</th>
              <th className="num">{copy(pageContract, "column.sold")}</th>
              <th className="num">{copy(pageContract, "column.share_pct")}</th>
              <th className="num">{copy(pageContract, "column.revenue")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 ? (
              <tr>
                <td colSpan={5}>
                  <div className="empty">{copy(pageContract, "empty.breakdown")}</div>
                </td>
              </tr>
            ) : (
              shown.map((row) => (
                <tr key={row.key}>
                  <td>
                    <b>{row.label}</b>
                    {row.detail ? <div className="muted small">{row.detail}</div> : null}
                  </td>
                  <td className="num">{num(row.on_farm)}</td>
                  <td className="num">
                    <b>{num(row.sold)}</b>
                  </td>
                  <td className="num">{totalSold > 0 ? `${num((row.sold / totalSold) * 100, 0)}%` : "—"}</td>
                  <td className="num">{row.sold_priced > 0 ? inr(row.revenue) : "—"}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
      {pager && pageCount > 1 ? (
        <div className="pager2">
          <span className="muted">
            {copy(pageContract, "pager.page")} {pageNumber} {copy(pageContract, "pager.of")} {pageCount}
          </span>
          {pager.offset > 0 ? (
            <Link href={pager.href(Math.max(0, pager.offset - pager.limit))} scroll={false} className="btn">
              {copy(pageContract, "action.previous")}
            </Link>
          ) : (
            <span className="btn" aria-disabled="true">
              {copy(pageContract, "action.previous")}
            </span>
          )}
          {pager.offset + pager.limit < rows.length ? (
            <Link href={pager.href(pager.offset + pager.limit)} scroll={false} className="btn">
              {copy(pageContract, "action.next")}
            </Link>
          ) : (
            <span className="btn" aria-disabled="true">
              {copy(pageContract, "action.next")}
            </span>
          )}
        </div>
      ) : null}
    </section>
  );
}

function FarmBornSections({
  data,
  pageContract,
  pageHref,
  penOffset,
  penHref,
}: {
  data: FarmBornSales;
  pageContract: AdminUiPageContract;
  pageHref: (offset: number) => string;
  penOffset: number;
  penHref: (offset: number) => string;
}) {
  const s = data.summary;
  const unpriced = s.sold - s.sold_priced;
  const pageNumber = Math.floor(data.offset / data.limit) + 1;
  const pageCount = Math.max(1, Math.ceil(data.total_sold / data.limit));
  const sexLabel = (key: string) =>
    optionGroup(pageContract, "farm_born_sexes").find((option) => option.key === key)?.label ?? key;

  return (
    <>
      <section className="grid g4 kpi-row sales-kpi-row" aria-label={copy(pageContract, "section.headline.aria")}>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.on_farm")}</div>
          <div className="val">{num(s.on_farm)}</div>
          <div className="dl">{copy(pageContract, "kpi.on_farm.detail")}</div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.sold")}</div>
          <div className="val">{num(s.sold)}</div>
          <div className="dl">
            {humanDate(s.from)} {copy(pageContract, "filter.period.range_separator")} {humanDate(s.to)}
          </div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.revenue")}</div>
          <div className="val">{inr(s.revenue)}</div>
          <div className="dl">
            {unpriced > 0
              ? fill(copy(pageContract, "kpi.revenue.unpriced"), { count: num(unpriced) })
              : copy(pageContract, "kpi.revenue.detail")}
          </div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.avg_price")}</div>
          <div className="val">{s.sold_priced > 0 ? inr(s.avg_price) : "—"}</div>
          <div className="dl">
            {num(s.sold_priced)} {copy(pageContract, "kpi.avg_price.detail")}
          </div>
        </div>
      </section>

      <div className="phead" style={{ marginTop: 18, paddingBottom: 4 }}>
        <div>
          <h2 style={{ margin: 0 }}>{copy(pageContract, "section.breakdowns.title")}</h2>
          <div className="sub">{copy(pageContract, "section.breakdowns.subtitle")}</div>
        </div>
      </div>
      {/* Breed on the left; Sex and Stage stacked on the right (both short). Pen gets its own
          full-width card below: a farm has dozens of pens, and beside a two-row sex table it left
          the right column mostly blank. */}
      <div className="grid g2">
        <BreakdownCard title={copy(pageContract, "section.by_breed.title")} rows={data.by_breed} totalSold={s.sold} pageContract={pageContract} />
        <div style={{ display: "grid", gap: 14, alignContent: "start" }}>
          <BreakdownCard
            title={copy(pageContract, "section.by_sex.title")}
            rows={data.by_sex.map((row) => ({ ...row, label: sexLabel(row.key) === row.key ? row.label : sexLabel(row.key) }))}
            totalSold={s.sold}
            pageContract={pageContract}
          />
          <BreakdownCard title={copy(pageContract, "section.by_stage.title")} rows={data.by_stage} totalSold={s.sold} pageContract={pageContract} />
        </div>
      </div>
      <div style={{ marginTop: 14 }}>
        <BreakdownCard
          title={copy(pageContract, "section.by_pen.title")}
          rows={data.by_pen}
          totalSold={s.sold}
          pageContract={pageContract}
          pager={{ offset: penOffset, limit: PEN_PAGE_SIZE, noun: copy(pageContract, "pager.pens"), href: penHref }}
        />
      </div>

      <section className="card" aria-label={copy(pageContract, "section.sold.aria")} style={{ marginTop: 14 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.sold.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">{copy(pageContract, "section.sold.subtitle")}</span>
        </div>
        <div className="twrap" tabIndex={0} role="region" aria-label={copy(pageContract, "section.sold.aria")}>
          <FarmBornSoldTable
            contract={table(pageContract, "sales-farm-born-sold")}
            rows={data.sold}
            labels={{
              ariaLabel: copy(pageContract, "section.sold.aria"),
              notRecorded: copy(pageContract, "value.not_recorded"),
              noDeal: copy(pageContract, "value.no_deal"),
              male: sexLabel("male"),
              female: sexLabel("female"),
              empty: <div className="empty">{copy(pageContract, "empty.sold")}</div>,
            }}
          />
        </div>
        {pageCount > 1 ? (
          <div className="pager2">
            <span className="muted">
              {copy(pageContract, "pager.page")} {pageNumber} {copy(pageContract, "pager.of")} {pageCount} ·{" "}
              {num(data.total_sold)} {copy(pageContract, "pager.noun")}
            </span>
            {data.offset > 0 ? (
              <Link href={pageHref(Math.max(0, data.offset - data.limit))} scroll={false} className="btn">
                {copy(pageContract, "action.previous")}
              </Link>
            ) : (
              <span className="btn" aria-disabled="true">
                {copy(pageContract, "action.previous")}
              </span>
            )}
            {data.offset + data.limit < data.total_sold ? (
              <Link href={pageHref(data.offset + data.limit)} scroll={false} className="btn">
                {copy(pageContract, "action.next")}
              </Link>
            ) : (
              <span className="btn" aria-disabled="true">
                {copy(pageContract, "action.next")}
              </span>
            )}
          </div>
        ) : null}
      </section>
    </>
  );
}

/**
 * Farm born (maintainer request 2026-09-18): the animals the farm did NOT buy on a load -- the
 * counterpart of Load wise. How many are on the farm today, how many sold in the chosen period,
 * which breed / sex / stage / pen the sold ones came from, and what they earned, behind a filter
 * bar (period, park, pen, species, breed, sex, stage) that governs the whole page.
 *
 * The period binds the SOLD side only (maintainer decision, same day): the on-farm count is
 * today's whatever the period. Read-only by contract, the /sales/sold shape: the page declares
 * no write control, so nothing here opens a form. Every figure and every label is the backend's.
 */
export async function SalesFarmBornPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;
  const today = todayIso();
  const defaultFrom = monthBefore(today);
  const defaultTo = today;

  // Every filter value is validated against the SERVED vocabulary before it is trusted: sex and
  // species against the contract's option groups here, the park / pen / breed / stage against the
  // payload's own options once it arrives (an unknown value simply matches nothing, which the
  // backend reports honestly as zero rows). The page reads ONE origin -- animals recorded as born
  // here (maintainer instruction 2026-09-18: no origin control) -- so the backend's default
  // applies and no URL parameter can widen it.
  const sexOptions = optionGroup(pageContract, "farm_born_sexes");
  const rawSex = one(sp, "sex") ?? "";
  const sex = sexOptions.some((option) => option.key === rawSex) ? rawSex : "";
  const speciesOptions = optionGroup(pageContract, "farm_born_species");
  const rawSpecies = one(sp, "species") ?? "";
  const species = speciesOptions.some((option) => option.key === rawSpecies) ? rawSpecies : "";
  const park = one(sp, "park") ?? "";
  const pen = one(sp, "pen") ?? "";
  const breed = one(sp, "breed") ?? "";
  const stage = one(sp, "stage") ?? "";
  const from = one(sp, "from") ?? "";
  const to = one(sp, "to") ?? "";

  const pageSizes = tablePageSizes(pageContract, "sales-farm-born-sold");
  const defaultLimit = pageSizes[0] ?? FALLBACK_LIMIT;
  const limit = resolveLimit(one(sp, "limit"), pageSizes, defaultLimit);
  const offset = boundedInt(one(sp, "offset"), 0, 0, MAX_OFFSET);
  const penOffset = boundedInt(one(sp, PEN_OFFSET_PARAM), 0, 0, MAX_OFFSET);

  const result = await getFarmBornSales({
    from: from || undefined,
    to: to || undefined,
    park_id: park || undefined,
    pen: pen || undefined,
    species: species || undefined,
    breed: breed || undefined,
    sex: sex || undefined,
    stage: stage || undefined,
    limit,
    offset,
  });
  if (firstAuthRequiredError(result)) redirect(INTERNAL_LOGIN_PATH);

  const options = result.ok ? result.data.options : null;
  // The pen list follows the park select: a pen belongs to one park, so with a park chosen only
  // its pens are offered, and a park change clears a pen that would no longer be in the list.
  const penOptions = (options?.pens ?? []).filter((option) => !park || option.park_id === park);
  // Pen names repeat across parks (Gandhi 1, Castro 2 exist in both), so on the All-parks view
  // each pen option also names its park; with a park chosen the name alone is unambiguous.
  const parkLabel = new Map((options?.parks ?? []).map((option) => [option.key, option.label]));
  const penOptionLabel = (option: { label: string; park_id?: string }) =>
    park || !option.park_id ? option.label : `${option.label} · ${parkLabel.get(option.park_id) ?? ""}`;
  const served = result.ok ? result.data.summary : null;

  const filterFields: WorklistFilterField[] = [
    {
      kind: "daterange",
      param: "from",
      toParam: "to",
      label: copy(pageContract, "filter.period.label"),
      // The window the BACKEND applied, so the control shows what is on screen.
      from: served?.from ?? (from || defaultFrom),
      to: served?.to ?? (to || defaultTo),
      today,
      defaultFrom,
      defaultTo,
      labels: {
        // The bar's own label says what the dates bind ("Sold between"); the picker's inner field
        // name stays the short word so the control does not read the sentence twice.
        field: copy(pageContract, "filter.period.field"),
        today: copy(pageContract, "filter.period.today"),
        single: copy(pageContract, "filter.period.single"),
        range: copy(pageContract, "filter.period.range"),
        aria: copy(pageContract, "filter.period.aria"),
        previousMonth: copy(pageContract, "filter.period.previous_month"),
        nextMonth: copy(pageContract, "filter.period.next_month"),
        rangeStartHint: copy(pageContract, "filter.period.range_start_hint"),
        rangeEndHint: copy(pageContract, "filter.period.range_end_hint"),
        rangeSeparator: copy(pageContract, "filter.period.range_separator"),
      },
    },
    {
      kind: "select",
      param: "park",
      label: copy(pageContract, "filter.park.label"),
      value: park,
      allowAll: true,
      clears: ["pen", PEN_OFFSET_PARAM],
      options: (options?.parks ?? []).map((option) => ({ value: option.key, label: option.label })),
    },
    {
      kind: "select",
      param: "pen",
      label: copy(pageContract, "filter.pen.label"),
      value: pen,
      allowAll: true,
      clears: [PEN_OFFSET_PARAM],
      options: penOptions.map((option) => ({ value: option.key, label: penOptionLabel(option) })),
    },
    {
      kind: "select",
      param: "species",
      label: copy(pageContract, "filter.species.label"),
      value: species,
      allowAll: true,
      clears: [PEN_OFFSET_PARAM],
      options: speciesOptions.map((option) => ({ value: option.key, label: option.label })),
    },
    {
      kind: "select",
      param: "breed",
      label: copy(pageContract, "filter.breed.label"),
      value: breed,
      allowAll: true,
      clears: [PEN_OFFSET_PARAM],
      options: (options?.breeds ?? []).map((option) => ({ value: option.key, label: option.label })),
    },
    {
      kind: "select",
      param: "sex",
      label: copy(pageContract, "filter.sex.label"),
      value: sex,
      allowAll: true,
      clears: [PEN_OFFSET_PARAM],
      options: sexOptions.map((option) => ({ value: option.key, label: option.label })),
    },
    {
      kind: "select",
      param: "stage",
      label: copy(pageContract, "filter.stage.label"),
      value: stage,
      allowAll: true,
      clears: [PEN_OFFSET_PARAM],
      options: (options?.stages ?? []).map((option) => ({ value: option.key, label: option.label })),
    },
  ];

  const pageHref = (nextOffset: number) =>
    hrefWith(sp, {
      offset: nextOffset > 0 ? String(nextOffset) : null,
      limit: limit === defaultLimit ? null : String(limit),
    });
  const penHref = (nextOffset: number) => hrefWith(sp, { [PEN_OFFSET_PARAM]: nextOffset > 0 ? String(nextOffset) : null });

  return (
    <div className="screen on sales-farm-born-page">
      <SalesPageHeader pageContract={pageContract} />

      <WorklistFilters basePath={PAGE_PATH} pageParam="offset" fields={filterFields} pageContract={pageContract}>
        {!result.ok ? (
          <div className="alert" style={{ marginBottom: 14 }}>
            <b>{result.error.code ?? result.error.kind}</b>&nbsp;
            {result.error.message || copy(pageContract, "error.load")}
          </div>
        ) : (
          <FarmBornSections data={result.data} pageContract={pageContract} pageHref={pageHref} penOffset={penOffset} penHref={penHref} />
        )}
      </WorklistFilters>
    </div>
  );
}
