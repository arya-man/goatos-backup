import { redirect } from "next/navigation";

import Link from "@/components/no-prefetch-link";
import { LinkPending } from "@/components/link-pending";
import { ChartHover } from "@/components/chart-hover";
import {
  SeriesLegend,
  SeriesLines,
  seriesColorVar,
  type LineSeries,
} from "@/components/svg-series";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getSalesExecutiveAnalytics } from "@/lib/api/procurement-server";
import type {
  SalesExecutiveActivity,
  SalesExecutiveAnalytics,
} from "@/lib/api/procurement";
import { fmtDate, fmtDateTime } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { countKey, inr, num } from "./sales-format";
import { SalesPageHeader, hrefWithQuery } from "./sales-chrome";
import { salesErrorText } from "./sales-error";

const PAGE_PATH = "/sales/executive-analytics";

function queryInt(value: string | undefined): number | undefined {
  if (value === undefined || value.trim() === "") return undefined;
  const trimmed = value.trim();
  if (!/^-?\d+$/.test(trimmed)) return Number.NaN;
  const parsed = Number.parseInt(trimmed, 10);
  return Number.isFinite(parsed) ? parsed : Number.NaN;
}

/** Fills a backend copy template's `{name}` slots; the sentence itself stays backend-owned. */
function fill(template: string, values: Record<string, string>): string {
  return template.replace(
    /\{(\w+)\}/g,
    (match, key: string) => values[key] ?? match,
  );
}

/**
 * Sales executive analytics (maintainer request 2026-10-02): what the sales desk is DOING --
 * vendors added and changed, market and buyer calls, sales and payments recorded -- by person and
 * by day. Read-only by contract. Every figure is the backend's (the procurement sales-desk read);
 * every word is the page contract's; vendor, city and buyer names are the farm's own data.
 */
/** Back / Next for one paged list. Only that list's offset moves; the period and the other list stay. */
function Pager({
  pageContract,
  sp,
  param,
  offset,
  size,
  total,
  noun,
}: {
  pageContract: AdminUiPageContract;
  sp: RouteSearchParams;
  param: "activity_offset" | "vendor_offset";
  offset: number;
  size: number;
  total: number;
  noun: string;
}) {
  if (total <= size) return null;
  const href = (next: number) =>
    hrefWithQuery(PAGE_PATH, sp, { [param]: next > 0 ? String(next) : null });
  const pageNumber = Math.floor(offset / size) + 1;
  const pageCount = Math.max(1, Math.ceil(total / size));
  return (
    <div className="pager2">
      <span className="muted">
        {copy(pageContract, "pager.page")} {num(pageNumber)}{" "}
        {copy(pageContract, "pager.of")} {num(pageCount)} · {num(total)} {noun}
      </span>
      {offset > 0 ? (
        <Link
          href={href(Math.max(0, offset - size))}
          scroll={false}
          className="btn"
        >
          {copy(pageContract, "action.prev_page")}
          <LinkPending />
        </Link>
      ) : (
        <span className="btn" aria-disabled="true">
          {copy(pageContract, "action.prev_page")}
        </span>
      )}
      {offset + size < total ? (
        <Link href={href(offset + size)} scroll={false} className="btn">
          {copy(pageContract, "action.next_page")}
          <LinkPending />
        </Link>
      ) : (
        <span className="btn" aria-disabled="true">
          {copy(pageContract, "action.next_page")}
        </span>
      )}
    </div>
  );
}

function Sections({
  data,
  pageContract,
  sp,
}: {
  data: SalesExecutiveAnalytics;
  pageContract: AdminUiPageContract;
  sp: RouteSearchParams;
}) {
  const c = data.current;
  const p = data.previous;
  const days = String(data.days);
  const unknown = copy(pageContract, "value.unknown_person");
  const vsPrevious = (previous: number) =>
    fill(copy(pageContract, "kpi.vs_previous"), {
      previous: num(previous),
      days,
    });

  // One line per kind of work, one point per day (7 days) or per week (30 / 90 days), so the
  // rise and fall of each reads at a glance. Every point is a backend bucket, never re-binned here.
  const series: LineSeries[] = (
    [
      ["series.vendors_added", "vendors_added"],
      ["series.vendors_edited", "vendors_edited"],
      ["series.calls", "calls"],
      ["series.sales", "sales"],
    ] as const
  ).map(([labelKey, field], index) => ({
    label: copy(pageContract, labelKey),
    colorVar: seriesColorVar(index),
    points: data.daily.map((d) => d[field]),
  }));
  const weekly = data.trend_grain === "week";
  const trendTitle = copy(
    pageContract,
    weekly ? "section.trend.title.week" : "section.trend.title",
  );

  const activityText = (a: SalesExecutiveActivity): string => {
    const subject = a.subject || copy(pageContract, "activity.unnamed");
    return fill(copy(pageContract, `activity.${a.kind}`), {
      subject,
      category: a.category ?? "",
    });
  };
  const activityDetail = (a: SalesExecutiveActivity): string => {
    switch (a.kind) {
      case "sale_recorded": {
        const parts: string[] = [];
        if (a.animals > 0) {
          parts.push(
            fill(
              copy(
                pageContract,
                countKey(a.animals, "value.animal", "value.animals"),
              ),
              {
                count: num(a.animals),
              },
            ),
          );
        }
        if (a.amount > 0) parts.push(inr(a.amount));
        return parts.join(" · ");
      }
      case "payment_recorded":
        return a.amount > 0 ? inr(a.amount) : "";
      case "market_call":
        return a.animals > 0
          ? fill(
              copy(
                pageContract,
                countKey(a.animals, "value.price", "value.prices"),
              ),
              {
                count: num(a.animals),
              },
            )
          : "";
      case "vendor_added":
      case "vendor_edited":
      case "lead_call":
        return a.category ?? "";
      default:
        return "";
    }
  };

  return (
    <>
      <section
        className="grid g4 kpi-row sales-kpi-row"
        aria-label={copy(pageContract, "section.headline.aria")}
      >
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.vendors_added")}</div>
          <div className="val">{num(c.vendors_added)}</div>
          <div className="dl">{vsPrevious(p.vendors_added)}</div>
          <div className="dl">
            {fill(copy(pageContract, "kpi.vendors.register"), {
              total: num(data.register_vendors),
            })}
          </div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.vendors_edited")}</div>
          <div className="val">{num(c.vendors_edited)}</div>
          <div className="dl">{vsPrevious(p.vendors_edited)}</div>
          <div className="dl">{copy(pageContract, "hint.edits")}</div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.calls")}</div>
          <div className="val">{num(c.calls)}</div>
          <div className="dl">
            {fill(copy(pageContract, "kpi.calls.detail"), {
              market: num(c.market_calls),
              leads: num(c.lead_calls),
            })}
          </div>
          <div className="dl">{vsPrevious(p.calls)}</div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "kpi.sales")}</div>
          <div className="val">{num(c.sales_recorded)}</div>
          <div className="dl">
            {fill(copy(pageContract, "kpi.sales.detail"), {
              value: inr(c.sales_value),
              payments: num(c.payments_recorded),
            })}
          </div>
          <div className="dl">{vsPrevious(p.sales_recorded)}</div>
        </div>
      </section>

      <section className="card" aria-label={trendTitle}>
        <div className="hd">
          <h3>{trendTitle}</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">
            {fmtDate(data.period_from)} – {fmtDate(data.period_to)}
          </span>
        </div>
        <div className="bd">
          <p className="muted small">
            {copy(
              pageContract,
              weekly ? "section.trend.subtitle.week" : "section.trend.subtitle",
            )}
          </p>
          <ChartHover>
            <SeriesLines
              series={series}
              dayLabels={data.daily.map((d) => d.date)}
              valueNoun=""
              chartLabel={trendTitle}
              emptyLabel={copy(pageContract, "empty.trend")}
            />
          </ChartHover>
          <SeriesLegend
            entries={series.map((s) => ({
              label: s.label,
              colorVar: s.colorVar,
            }))}
          />
        </div>
      </section>

      <section
        className="card"
        aria-label={copy(pageContract, "section.people.title")}
      >
        <div className="hd">
          <h3>{copy(pageContract, "section.people.title")}</h3>
        </div>
        <div className="bd">
          <p className="muted small">
            {copy(pageContract, "section.people.subtitle")}
          </p>
          <div
            className="twrap"
            tabIndex={0}
            role="region"
            aria-label={copy(pageContract, "section.people.title")}
          >
            {data.people.length === 0 ? (
              <div className="empty">{copy(pageContract, "empty.people")}</div>
            ) : (
              <table className="tbl">
                <thead>
                  <tr>
                    <th>{copy(pageContract, "column.person")}</th>
                    <th className="num">
                      {copy(pageContract, "column.vendors_added")}
                    </th>
                    <th className="num">
                      {copy(pageContract, "column.vendors_edited")}
                    </th>
                    <th className="num">
                      {copy(pageContract, "column.calls")}
                    </th>
                    <th className="num">
                      {copy(pageContract, "column.sales")}
                    </th>
                    <th className="num">
                      {copy(pageContract, "column.payments")}
                    </th>
                    <th className="num">
                      {copy(pageContract, "column.status_changes")}
                    </th>
                    <th className="num">
                      {copy(pageContract, "column.active_days")}
                    </th>
                    <th>{copy(pageContract, "column.last_active")}</th>
                  </tr>
                </thead>
                <tbody>
                  {data.people.map((person) => (
                    <tr key={person.actor_id}>
                      <td>
                        <b>{person.name || unknown}</b>
                      </td>
                      <td className="num">
                        {num(person.counts.vendors_added)}
                      </td>
                      <td className="num">
                        {num(person.counts.vendors_edited)}
                      </td>
                      <td className="num">{num(person.counts.calls)}</td>
                      <td className="num">
                        {num(person.counts.sales_recorded)}
                        {person.counts.sales_value > 0 ? (
                          <div className="muted small">
                            {inr(person.counts.sales_value)}
                          </div>
                        ) : null}
                      </td>
                      <td className="num">
                        {num(person.counts.payments_recorded)}
                        {person.counts.payments_value > 0 ? (
                          <div className="muted small">
                            {inr(person.counts.payments_value)}
                          </div>
                        ) : null}
                      </td>
                      <td className="num">
                        {num(person.counts.deal_status_changes)}
                      </td>
                      <td className="num">{num(person.active_days)}</td>
                      <td>{fmtDateTime(person.last_active_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      </section>

      <section className="grid g2 sales-exec-pair">
        <section
          className="card sales-exec-list"
          aria-label={copy(pageContract, "section.vendors.title")}
        >
          <div className="hd">
            <h3>{copy(pageContract, "section.vendors.title")}</h3>
          </div>
          <div className="bd">
            <p className="muted small">
              {copy(pageContract, "section.vendors.subtitle")}
            </p>
            <div
              className="twrap sales-exec-scroll"
              tabIndex={0}
              role="region"
              aria-label={copy(pageContract, "section.vendors.title")}
            >
              {data.register_vendors === 0 ? (
                <div className="empty">
                  {copy(pageContract, "empty.vendors")}
                </div>
              ) : data.latest_vendors.length === 0 ? null : (
                <table className="tbl">
                  <thead>
                    <tr>
                      <th>{copy(pageContract, "column.vendor")}</th>
                      <th>{copy(pageContract, "column.added_by")}</th>
                      <th>{copy(pageContract, "column.added_on")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.latest_vendors.map((v) => (
                      <tr key={v.vendor_id}>
                        <td>
                          <b>{v.business_name}</b>
                          <div className="muted small">
                            {[v.category, v.place].filter(Boolean).join(" · ")}
                          </div>
                        </td>
                        <td>
                          {v.added_by_known
                            ? v.added_by_name || unknown
                            : copy(pageContract, "value.imported")}
                        </td>
                        <td>{fmtDate(v.added_at)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
            <Pager
              pageContract={pageContract}
              sp={sp}
              param="vendor_offset"
              offset={data.vendor_offset}
              size={data.page_size}
              total={data.register_vendors}
              noun={copy(pageContract, "pager.vendors")}
            />
          </div>
        </section>

        <section
          className="card sales-exec-list"
          aria-label={copy(pageContract, "section.recent.title")}
        >
          <div className="hd">
            <h3>{copy(pageContract, "section.recent.title")}</h3>
          </div>
          <div className="bd">
            <p className="muted small">
              {copy(pageContract, "section.recent.subtitle")}
            </p>
            <div
              className="sales-exec-scroll"
              tabIndex={0}
              role="region"
              aria-label={copy(pageContract, "section.recent.title")}
            >
              {data.recent_total === 0 ? (
                <div className="empty">
                  {copy(pageContract, "empty.recent")}
                </div>
              ) : data.recent.length === 0 ? null : (
                <ul className="sales-exec-feed">
                  {data.recent.map((a, index) => {
                    const detail = activityDetail(a);
                    return (
                      <li key={`${a.at}-${a.kind}-${index}`}>
                        <div>
                          <b>{a.actor_name || unknown}</b> · {activityText(a)}
                        </div>
                        <div className="muted small">
                          {fmtDateTime(a.at)}
                          {detail ? ` · ${detail}` : ""}
                        </div>
                      </li>
                    );
                  })}
                </ul>
              )}
            </div>
            <Pager
              pageContract={pageContract}
              sp={sp}
              param="activity_offset"
              offset={data.activity_offset}
              size={data.page_size}
              total={data.recent_total}
              noun={copy(pageContract, "pager.activities")}
            />
          </div>
        </section>
      </section>
    </>
  );
}

function PeriodChips({
  pageContract,
  sp,
  days,
  options,
}: {
  pageContract: AdminUiPageContract;
  sp: RouteSearchParams;
  days: number;
  options: readonly number[];
}) {
  return (
    <div
      className="chips"
      role="group"
      aria-label={copy(pageContract, "filter.period")}
      style={{ marginBottom: 14 }}
    >
      <span className="muted small" style={{ marginRight: 6 }}>
        {copy(pageContract, "filter.period")}
      </span>
      {options.map((option) => (
        <Link
          key={option}
          href={hrefWithQuery(PAGE_PATH, sp, {
            days: String(option),
            activity_offset: null,
            vendor_offset: null,
          })}
          scroll={false}
          className={option === days ? "btn sm p" : "btn sm"}
          aria-current={option === days ? "true" : undefined}
        >
          {fill(copy(pageContract, "filter.period.days"), {
            days: num(option),
          })}
          <LinkPending />
        </Link>
      ))}
    </div>
  );
}

/** The periods the backend offers; used only to render the chips before a failed load. */
const FALLBACK_PERIODS = [7, 30, 90] as const;

export async function SalesExecutiveAnalyticsPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;
  const days = queryInt(one(sp, "days"));
  const activityOffset = queryInt(one(sp, "activity_offset"));
  const vendorOffset = queryInt(one(sp, "vendor_offset"));
  const result = await getSalesExecutiveAnalytics({
    days,
    activityOffset,
    vendorOffset,
  });
  if (firstAuthRequiredError(result)) redirect(INTERNAL_LOGIN_PATH);

  return (
    <div className="screen on">
      <SalesPageHeader pageContract={pageContract} />

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          {salesErrorText(result.error, copy(pageContract, "error.load"))}
        </div>
      ) : null}

      <PeriodChips
        pageContract={pageContract}
        sp={sp}
        days={result.ok ? result.data.days : (days ?? 30)}
        options={result.ok ? result.data.days_options : FALLBACK_PERIODS}
      />

      {result.ok ? (
        <Sections data={result.data} pageContract={pageContract} sp={sp} />
      ) : null}
    </div>
  );
}
