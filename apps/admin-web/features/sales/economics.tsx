import Link from "@/components/no-prefetch-link";
import { redirect } from "next/navigation";

import { Tag } from "@/components/ui-primitives";
import { BreedEconomicsBars, type BreedBarDatum } from "./breed-economics-bars";
import { copy, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, getSalesEconomics, type BusinessEconomicsResponse } from "@/lib/api/server";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { humanDate, inr, num } from "@/features/procurement/sales-format";

const PAGE_PATH = "/sales/economics";
const WINDOW_DAYS = [30, 90, 180] as const;
const DEFAULT_WINDOW = 90;

type Pulse = BusinessEconomicsResponse["pulse"];
type ShedRow = BusinessEconomicsResponse["sheds"][number];
type BreedRow = BusinessEconomicsResponse["breeds"][number];
type BandRow = BusinessEconomicsResponse["bands"][number];

function hrefWithQuery(sp: RouteSearchParams, patch: Record<string, string | null>): string {
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

/** Today's business date in Asia/Kolkata, shifted back `daysBack` days (inclusive-window start). */
function istDateDaysBack(daysBack: number): string {
  const nowIST = new Date(Date.now() + 5.5 * 60 * 60 * 1000);
  nowIST.setUTCDate(nowIST.getUTCDate() - daysBack);
  return nowIST.toISOString().slice(0, 10);
}

function signalTone(signal: string): "ok" | "dng" | "mut" {
  if (signal === "earning") return "ok";
  if (signal === "burning") return "dng";
  return "mut";
}

/** Renders a nullable rupee figure; null NEVER renders as ₹0 — it renders as the none marker. */
function money(value: number | null | undefined, none: string, suffix = ""): string {
  if (value === null || value === undefined) return none;
  return suffix ? `${inr(value)} ${suffix}` : inr(value);
}

function gramsPerDay(value: number | null | undefined, none: string, suffix: string): string {
  if (value === null || value === undefined) return none;
  return `${num(value)} ${suffix}`;
}

function Pager({ page, pageCount, hrefFor }: { page: number; pageCount: number; hrefFor: (page: number) => string }) {
  if (pageCount <= 1) return null;
  return (
    <div className="row" style={{ gap: 8, alignItems: "center", padding: "8px 2px" }}>
      {page > 1 ? (
        <Link className="btn small" href={hrefFor(page - 1)}>
          ‹
        </Link>
      ) : null}
      <span className="muted small">
        {page} / {pageCount}
      </span>
      {page < pageCount ? (
        <Link className="btn small" href={hrefFor(page + 1)}>
          ›
        </Link>
      ) : null}
    </div>
  );
}

export async function EconomicsPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;
  const park = one(sp, "park") ?? "";
  const windowDays = boundedInt(one(sp, "days"), DEFAULT_WINDOW, WINDOW_DAYS[0], WINDOW_DAYS[WINDOW_DAYS.length - 1]);

  const result = await getSalesEconomics({
    park_id: park || undefined,
    // The backend defaults to its own 90-day window; a chosen chip pins the start
    // explicitly so 30/180 work the same way.
    from: istDateDaysBack(windowDays - 1),
  });
  if (firstAuthRequiredError(result)) redirect(INTERNAL_LOGIN_PATH);

  const none = copy(pageContract, "value.none");
  const gday = copy(pageContract, "value.g_per_day_suffix");
  const perDay = copy(pageContract, "value.per_day_suffix");
  const perKg = copy(pageContract, "value.per_kg_suffix");

  const data: BusinessEconomicsResponse | null = result.ok ? result.data : null;
  const pulse: Pulse | null = data ? data.pulse : null;
  const parks = data ? data.parks : [];

  const shedColumns = tableLabels(pageContract, "economics-sheds");
  const shedPageSize = tablePageSizes(pageContract, "economics-sheds")[1] ?? 25;

  const sheds: ShedRow[] = data ? data.sheds : [];
  const breeds: BreedRow[] = data ? data.breeds : [];
  const bands: BandRow[] = data ? data.bands : [];

  const shedPageCount = Math.max(1, Math.ceil(sheds.length / shedPageSize));
  const shedPage = Math.min(boundedInt(one(sp, "spage"), 1, 1, shedPageCount), shedPageCount);
  const shedRows = sheds.slice((shedPage - 1) * shedPageSize, shedPage * shedPageSize);

  const priceBasisHint = pulse ? copy(pageContract, `kpi.realized_price.${pulse.price_basis}`) : "";

  // Feed as the fill inside the return as the track, on ONE shared scale: a fill
  // that nearly covers its track is a breed barely paying for itself.
  const breedChart: BreedBarDatum[] = breeds
    .filter((b) => b.feed_cost_per_day_rupees !== null && b.feed_cost_per_day_rupees !== undefined)
    .map((b) => ({
      key: b.breed,
      label: b.breed,
      feed: b.feed_cost_per_day_rupees ?? 0,
      value: b.value_added_per_day_rupees ?? null,
      feedDisplay: money(b.feed_cost_per_day_rupees, none),
      valueDisplay: money(b.value_added_per_day_rupees, none),
      losing: (b.net_per_day_rupees ?? 0) < 0,
    }));

  return (
    <div className="screen on">
      <div className="phead" style={{ marginTop: 12, alignItems: "flex-end", paddingBottom: 6 }}>
        <div>
          <div className="crumb">
            <b>{copy(pageContract, "crumb")}</b> · {pageContract.title}
          </div>
          <h1>{pageContract.title}</h1>
          <div className="sub">{pageContract.subtitle}</div>
        </div>
      </div>

      {/* Scope + window chips. Park narrows animal and feed figures; deal figures stay
          both-farms, which the honesty strip below states. */}
      <div className="row" style={{ gap: 8, flexWrap: "wrap", margin: "4px 0 14px" }}>
        <Link className={`btn small${park === "" ? " primary" : ""}`} href={hrefWithQuery(sp, { park: null, spage: null })}>
          {copy(pageContract, "filter.park.all")}
        </Link>
        {parks.map((p) => (
          <Link
            key={p.park_id}
            className={`btn small${park === p.park_id ? " primary" : ""}`}
            href={hrefWithQuery(sp, { park: p.park_id, spage: null })}
          >
            {p.name}
          </Link>
        ))}
        <span className="sp" style={{ flex: 1 }} />
        <span className="muted small" style={{ alignSelf: "center" }}>
          {copy(pageContract, "filter.window")}:
        </span>
        {WINDOW_DAYS.map((d) => (
          <Link
            key={d}
            className={`btn small${windowDays === d ? " primary" : ""}`}
            href={hrefWithQuery(sp, { days: String(d), spage: null })}
          >
            {d}d
          </Link>
        ))}
      </div>

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message || copy(pageContract, "error.load")}
        </div>
      ) : null}

      {data && pulse ? (
        <>
          {/* 1 — pulse tiles. The first three cover ONE shared set of animals, so the
              third really is the first two subtracted; the whole-herd feed figure is a
              separate labelled line below. */}
          <section className="grid g4 kpi-row" aria-label={copy(pageContract, "section.pulse.aria")}>
            <div className="kpi">
              <div className="lab">{copy(pageContract, "kpi.feed_burn")}</div>
              <div className="val">{money(pulse.feed_cost_per_day_rupees, none, perDay)}</div>
              <div className="dl">{copy(pageContract, "kpi.feed_burn.hint")}</div>
            </div>
            <div className="kpi">
              <div className="lab">{copy(pageContract, "kpi.value_added")}</div>
              <div className="val">{money(pulse.value_added_per_day_rupees, none, perDay)}</div>
              <div className="dl">{copy(pageContract, "kpi.value_added.hint")}</div>
            </div>
            <div className="kpi">
              <div className="lab">{copy(pageContract, "kpi.net")}</div>
              <div className="val">{money(pulse.net_per_day_rupees, none, perDay)}</div>
              <div className="dl">
                {pulse.net_per_day_rupees === null || pulse.net_per_day_rupees === undefined
                  ? copy(pageContract, "kpi.cost_per_kg_gain.hint")
                  : copy(pageContract, pulse.net_per_day_rupees >= 0 ? "kpi.net.earning" : "kpi.net.burning")}
              </div>
            </div>
            <div className="kpi">
              <div className="lab">{copy(pageContract, "kpi.realized_price")}</div>
              <div className="val">{money(pulse.realized_price_per_kg, none, perKg)}</div>
              <div className="dl">{priceBasisHint}</div>
            </div>
          </section>

          {/* The shared denominator, said once, directly under the tiles it governs. */}
          <p className="muted small" style={{ margin: "8px 0 2px" }}>
            {num(pulse.priced_animals)} {copy(pageContract, "kpi.comparable")}
            {" · "}
            {copy(pageContract, "kpi.cost_per_kg_gain")}: {money(pulse.median_cost_per_kg_gain, none, perKg)}
          </p>

          {/* Whole-herd feed spend on its LATEST sheet day — a real day's rate, with its
              population, its date and the feed it could not price all named, so it is
              never read as the tiles' companion. */}
          <p className="muted small" style={{ margin: "2px 0 2px" }}>
            {copy(pageContract, "farm.line")}
            {pulse.farm_feed_day ? ` (${humanDate(pulse.farm_feed_day)})` : ""}:{" "}
            {money(pulse.farm_feed_cost_per_day_rupees, none)}
            {" · "}
            {num(pulse.farm_animals)} {copy(pageContract, "farm.animals")}
            {pulse.farm_unpriced_kg ? (
              <>
                {" · "}
                {num(pulse.farm_unpriced_kg)} {copy(pageContract, "farm.unpriced")}
              </>
            ) : null}
            {". "}
            {copy(pageContract, "farm.caution")}
          </p>

          <p className="muted small" style={{ margin: "2px 0 12px" }}>
            {copy(pageContract, "period.covering")}: {humanDate(data.period.start)} – {humanDate(data.period.end)}
            {" · "}
            {copy(pageContract, "kpi.sold")}: {num(pulse.animals_sold)} {copy(pageContract, "kpi.animals")} ·{" "}
            {num(pulse.closed_deals)} {copy(pageContract, "kpi.deals")} · {inr(pulse.sold_revenue_rupees)} (
            {copy(pageContract, "kpi.sold.detail")})
          </p>

          {/* Honesty strip: what the figures cover and what is missing from them. */}
          <section className="card" style={{ margin: "10px 0 14px" }} aria-label={copy(pageContract, "trust.title")}>
            <div className="bd">
              <p className="muted small" style={{ margin: 0 }}>
                <b>{copy(pageContract, "trust.title")}:</b> {num(pulse.weighed_identities)}{" "}
                {copy(pageContract, "trust.weighed")} · {num(pulse.paired_animals)} {copy(pageContract, "trust.paired")} ·{" "}
                {num(pulse.priced_animals)} {copy(pageContract, "trust.costed")}
                {pulse.unpriced_feed_items > 0 ? (
                  <>
                    {" · "}
                    {num(pulse.unpriced_feed_items)} {copy(pageContract, "trust.unpriced_items")}
                  </>
                ) : null}
              </p>
              <p className="muted small" style={{ margin: "6px 0 0" }}>
                {copy(pageContract, "trust.estimate")} {copy(pageContract, "trust.deals_scope")}
              </p>
            </div>
          </section>

          {/* 2 — breed by breed: the buy/keep question, chart first. */}
          <section className="card" style={{ marginBottom: 14 }} aria-label={copy(pageContract, "section.breeds.title")}>
            <div className="hd">
              <h3>{copy(pageContract, "section.breeds.title")}</h3>
              <div className="sub">{copy(pageContract, "section.breeds.subtitle")}</div>
            </div>
            <div className="bd">
              <BreedEconomicsBars
                data={breedChart}
                chartLabel={copy(pageContract, "section.breeds.title")}
                feedNoun={copy(pageContract, "chart.breeds.value")}
                valueNoun={copy(pageContract, "chart.breeds.compare")}
                emptyLabel={copy(pageContract, "empty.breeds")}
              />
              {breeds.length > 0 ? (
                <div className="tbl-wrap" style={{ overflowX: "auto", marginTop: 10 }}>
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th>{copy(pageContract, "column.breed")}</th>
                        <th>{copy(pageContract, "column.animals")}</th>
                        <th>{copy(pageContract, "column.adg")}</th>
                        <th>{copy(pageContract, "column.feed_cost_day")}</th>
                        <th>{copy(pageContract, "column.value_per_day")}</th>
                        <th>{copy(pageContract, "column.net_per_day")}</th>
                        <th>{copy(pageContract, "column.cost_per_kg_gain")}</th>
                        <th>{copy(pageContract, "column.signal")}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {breeds.map((breed) => (
                        <tr key={breed.breed}>
                          <td>
                            <b>{breed.breed}</b>
                          </td>
                          <td>{num(breed.animals)}</td>
                          <td>{gramsPerDay(breed.adg_g_per_day, none, gday)}</td>
                          <td>{money(breed.feed_cost_per_day_rupees, none)}</td>
                          <td>{money(breed.value_added_per_day_rupees, none)}</td>
                          <td>{money(breed.net_per_day_rupees, none)}</td>
                          <td>{money(breed.cost_per_kg_gain_rupees, none, perKg)}</td>
                          <td>
                            <Tag tone={signalTone(breed.signal)}>{copy(pageContract, `signal.${breed.signal}`)}</Tag>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : null}
            </div>
          </section>

          {/* 3 — shed by shed. */}
          <section className="card" style={{ marginBottom: 14 }} aria-label={copy(pageContract, "section.animals.title")}>
            <div className="hd">
              <h3>{copy(pageContract, "section.animals.title")}</h3>
              <div className="sub">{copy(pageContract, "section.animals.subtitle")}</div>
            </div>
            <div className="bd">
              {sheds.length === 0 ? (
                <div className="muted small" style={{ padding: "14px 2px", textAlign: "center" }}>
                  {copy(pageContract, "empty.animals")}
                </div>
              ) : (
                <>
                  <div className="tbl-wrap" style={{ overflowX: "auto" }}>
                    <table className="tbl">
                      <thead>
                        <tr>
                          {shedColumns.map((label) => (
                            <th key={label}>{label}</th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {shedRows.map((shed) => (
                          <tr key={`${shed.location_id}-${shed.partition_label}`}>
                            <td>
                              <b>{shed.shed_display}</b>
                            </td>
                            <td>{num(shed.animals)}</td>
                            <td>{gramsPerDay(shed.adg_g_per_day, none, gday)}</td>
                            <td>{money(shed.feed_cost_per_day_rupees, none)}</td>
                            <td>{money(shed.value_added_per_day_rupees, none)}</td>
                            <td>{money(shed.net_per_day_rupees, none)}</td>
                            <td>{money(shed.cost_per_kg_gain_rupees, none, perKg)}</td>
                            <td>
                              <Tag tone={signalTone(shed.signal)}>{copy(pageContract, `signal.${shed.signal}`)}</Tag>
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                  <div className="row" style={{ alignItems: "center", gap: 12 }}>
                    <Pager
                      page={shedPage}
                      pageCount={shedPageCount}
                      hrefFor={(page) => hrefWithQuery(sp, { spage: page === 1 ? null : String(page) })}
                    />
                    <span className="muted small">{copy(pageContract, "animals.capped")}</span>
                  </div>
                </>
              )}
            </div>
          </section>

          {/* 4 — break-even bands. */}
          <section className="card" style={{ marginBottom: 14 }} aria-label={copy(pageContract, "section.bands.title")}>
            <div className="hd">
              <h3>{copy(pageContract, "section.bands.title")}</h3>
              <div className="sub">{copy(pageContract, "section.bands.subtitle")}</div>
            </div>
            <div className="bd">
              {bands.every((b) => b.animals === 0) ? (
                <div className="muted small" style={{ padding: "14px 2px", textAlign: "center" }}>
                  {copy(pageContract, "empty.bands")}
                </div>
              ) : (
                <div className="tbl-wrap" style={{ overflowX: "auto" }}>
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th>{copy(pageContract, "value.kg_suffix")}</th>
                        <th>{copy(pageContract, "bands.animals")}</th>
                        <th>{copy(pageContract, "bands.gain")}</th>
                        <th>{copy(pageContract, "bands.cost")}</th>
                        <th>{copy(pageContract, "bands.value")}</th>
                        <th>{copy(pageContract, "bands.net")}</th>
                        <th />
                      </tr>
                    </thead>
                    <tbody>
                      {bands.map((band) => (
                        <tr key={band.band}>
                          <td>
                            <b>{band.band}</b>
                          </td>
                          <td>{num(band.animals)}</td>
                          <td>{gramsPerDay(band.median_adg_g_per_day, none, gday)}</td>
                          <td>{money(band.feed_cost_per_day_rupees, none)}</td>
                          <td>{money(band.value_added_per_day_rupees, none)}</td>
                          <td>{money(band.net_per_day_rupees, none)}</td>
                          <td>
                            {band.net_per_day_rupees === null || band.net_per_day_rupees === undefined ? (
                              band.animals > 0 ? (
                                <Tag tone="mut">{copy(pageContract, "bands.unknown")}</Tag>
                              ) : null
                            ) : band.sell_signal ? (
                              <Tag tone="dng">{copy(pageContract, "bands.sell")}</Tag>
                            ) : (
                              <Tag tone="ok">{copy(pageContract, "bands.keep")}</Tag>
                            )}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </div>
          </section>
        </>
      ) : null}
    </div>
  );
}
