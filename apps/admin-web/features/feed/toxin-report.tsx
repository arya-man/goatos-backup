import {
  control,
  controlEnabled,
  copy,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import {
  loadToxinReport,
  type ToxinReport,
  type ToxinReportLoad,
} from "@/lib/api/server";
import { fmtDate } from "@/lib/format";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { Tag, type Tone } from "@/components/ui-primitives";
import NoPrefetchLink from "@/components/no-prefetch-link";
import { FeedFaroView } from "./feed-faro-view";

// Feed -> Toxin. The leadership read of the aflatoxin strip test that every purchased feed load
// goes through before it is fed.
//
// THE GRAIN IS THE LOAD, not the test. A delivery whose first strip came back void is retested,
// so it carries two or three task rows; it is still ONE delivery and must read as one row here.
// The backend does that collapse (DISTINCT ON the load, latest round) — this page never groups,
// never sums rows into a KPI, and never derives a status of its own.
//
// Every visible string arrives composed on the payload or the page contract and is rendered
// verbatim: chip labels, result chips, KPI notes, the banner, the empty line. The page owns
// layout only.

const TONES: Record<string, Tone> = {
  ok: "ok",
  warn: "warn",
  danger: "dng",
  info: "info",
  muted: "mut",
};

function tone(value: string): Tone {
  return TONES[value] ?? "mut";
}

function toxinHref(
  searchParams: RouteSearchParams,
  next: { filter?: string; cursor?: string },
): string {
  const params = new URLSearchParams();
  const filter = next.filter ?? one(searchParams, "filter") ?? "";
  if (filter && filter !== "all") params.set("filter", filter);
  if (next.cursor) params.set("cursor", next.cursor);
  const query = params.toString();
  return query ? `/feed/toxin?${query}` : "/feed/toxin";
}

export async function ToxinReportPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  // Capability half of the role-scoped-UI rule: the endpoint enforces ToxinRead independently,
  // and this stops a principal without it from seeing a shell that only ever errors.
  if (!controlEnabled(pageContract, "toxin_report", false)) {
    return (
      <section className="card">
        <h2 className="h">{copy(pageContract, "toxin_report.title")}</h2>
        <p className="muted small">
          {control(pageContract, "toxin_report").disabled_reason}
        </p>
      </section>
    );
  }

  const filter = one(searchParams, "filter") ?? "all";
  const cursor = one(searchParams, "cursor") ?? "";
  const result = await loadToxinReport({ filter, cursor, limit: 20 });

  if (!result.ok) {
    return (
      <section className="card">
        <h2 className="h">{copy(pageContract, "toxin_report.title")}</h2>
        <p className="muted small">{copy(pageContract, "error.unavailable")}</p>
      </section>
    );
  }
  const report = result.data;

  return (
    <>
      <FeedFaroView
        routeId="toxin-reports"
        flaggedLoads={report.summary.needs_attention}
      />
      <p className="muted small" style={{ margin: "0 0 14px" }}>
        {copy(pageContract, "banner.basis")}
      </p>

      {report.summary.alert_message ? (
        <div className="alert" role="status">
          <div style={{ flex: 1 }}>
            <b>{report.summary.alert_message}</b>
          </div>
        </div>
      ) : null}

      <ToxinKpis report={report} pageContract={pageContract} />
      <ToxinCharts report={report} pageContract={pageContract} />
      <ToxinLoadsTable
        report={report}
        pageContract={pageContract}
        searchParams={searchParams}
        filter={filter}
      />
    </>
  );
}

function ToxinKpis({
  report,
  pageContract,
}: {
  report: ToxinReport;
  pageContract: AdminUiPageContract;
}) {
  const s = report.summary;
  const cards = [
    {
      value: s.loads_received,
      label: copy(pageContract, "kpi.received.label"),
      note: s.loads_received_note,
    },
    {
      value: s.loads_tested,
      label: copy(pageContract, "kpi.tested.label"),
      note: s.loads_tested_note,
    },
    {
      value: s.needs_attention,
      label: copy(pageContract, "kpi.attention.label"),
      note: s.needs_attention_note,
    },
    {
      value: s.waiting,
      label: copy(pageContract, "kpi.waiting.label"),
      note: s.waiting_note,
    },
  ];
  return (
    <section className="grid g4 kpi-row" aria-label={s.window_label}>
      {cards.map((card) => (
        <div className="kpi card" key={card.label}>
          <div className="val">{card.value}</div>
          <div className="dl">{card.label}</div>
          <div className="muted small">{card.note}</div>
        </div>
      ))}
    </section>
  );
}

function ToxinCharts({
  report,
  pageContract,
}: {
  report: ToxinReport;
  pageContract: AdminUiPageContract;
}) {
  const maxReceived = report.weeks.reduce(
    (max, week) => Math.max(max, week.received),
    0,
  );
  const mixTotal = report.outcome_mix.reduce(
    (sum, slice) => sum + slice.count,
    0,
  );
  const maxVendorShare = report.vendors.reduce(
    (max, vendor) =>
      Math.max(max, vendor.loads > 0 ? vendor.flagged / vendor.loads : 0),
    0,
  );

  return (
    <section className="grid g3" style={{ marginTop: 14 }}>
      <div className="card" style={{ padding: "13px 15px" }}>
        <h3 style={{ margin: "0 0 1px", fontSize: 13 }}>
          {copy(pageContract, "chart.weeks.title")}
        </h3>
        <div className="muted small" style={{ marginBottom: 10 }}>
          {copy(pageContract, "chart.weeks.caption")}
        </div>
        <div
          style={{
            display: "flex",
            alignItems: "flex-end",
            gap: 10,
            height: 120,
          }}
        >
          {report.weeks.map((week) => {
            // Nested, not stacked: the GREY bar is the week's loads and the GREEN fill inside it
            // is how many of those were tested. Positioning the two against each other renders
            // both at full height and says nothing.
            const barHeight =
              maxReceived > 0
                ? Math.max(2, Math.round((week.received / maxReceived) * 92))
                : 0;
            const testedShare =
              week.received > 0
                ? Math.round((week.tested / week.received) * 100)
                : 0;
            return (
              <div
                key={week.week_start}
                style={{ flex: 1, textAlign: "center", minWidth: 0 }}
              >
                <div
                  style={{
                    height: 92,
                    display: "flex",
                    alignItems: "flex-end",
                  }}
                >
                  <div
                    title={`${week.tested} / ${week.received}`}
                    style={{
                      width: "100%",
                      height: `${barHeight}px`,
                      background: "var(--line2)",
                      borderRadius: 4,
                      display: "flex",
                      alignItems: "flex-end",
                      overflow: "hidden",
                    }}
                  >
                    <div
                      style={{
                        width: "100%",
                        height: `${testedShare}%`,
                        background: "var(--brand)",
                      }}
                    />
                  </div>
                </div>
                <div className="muted small" style={{ marginTop: 6 }}>
                  {week.label}
                </div>
                <div
                  className="small"
                  style={{
                    fontVariantNumeric: "tabular-nums",
                    fontWeight: 650,
                  }}
                >
                  {week.tested}/{week.received}
                </div>
              </div>
            );
          })}
          {report.weeks.length === 0 ? (
            <div className="muted small">{report.empty_message}</div>
          ) : null}
        </div>
      </div>

      <div className="card" style={{ padding: "13px 15px" }}>
        <h3 style={{ margin: "0 0 1px", fontSize: 13 }}>
          {copy(pageContract, "chart.mix.title")}
        </h3>
        <div className="muted small" style={{ marginBottom: 10 }}>
          {copy(pageContract, "chart.mix.caption")}
        </div>
        <ul
          style={{
            listStyle: "none",
            margin: 0,
            padding: 0,
            display: "grid",
            gap: 9,
          }}
        >
          {report.outcome_mix.map((slice) => (
            <li
              key={slice.key}
              style={{ display: "flex", alignItems: "center", gap: 9 }}
            >
              <Tag tone={tone(slice.tone)}>{slice.label}</Tag>
              <span style={{ flex: 1 }} />
              <b style={{ fontVariantNumeric: "tabular-nums" }}>
                {slice.count}
              </b>
              <span
                className="muted small"
                style={{ width: 44, textAlign: "right" }}
              >
                {mixTotal > 0
                  ? `${Math.round((slice.count / mixTotal) * 100)}%`
                  : "—"}
              </span>
            </li>
          ))}
        </ul>
      </div>

      <div className="card" style={{ padding: "13px 15px" }}>
        <h3 style={{ margin: "0 0 1px", fontSize: 13 }}>
          {copy(pageContract, "chart.vendors.title")}
        </h3>
        <div className="muted small" style={{ marginBottom: 10 }}>
          {copy(pageContract, "chart.vendors.caption")}
        </div>
        {report.vendors.length === 0 ||
        report.vendors.every((v) => v.flagged === 0) ? (
          <div className="muted small" style={{ padding: "10px 0" }}>
            {copy(pageContract, "chart.vendors.empty")}
          </div>
        ) : (
          <div style={{ display: "grid", gap: 11 }}>
            {report.vendors.map((vendor) => {
              const share =
                vendor.loads > 0 ? vendor.flagged / vendor.loads : 0;
              const width =
                maxVendorShare > 0
                  ? Math.max(2, Math.round((share / maxVendorShare) * 100))
                  : 2;
              const fill =
                vendor.tone === "danger"
                  ? "var(--danger)"
                  : vendor.tone === "warn"
                    ? "var(--amber)"
                    : "var(--brand)";
              return (
                <div key={vendor.vendor}>
                  <div
                    style={{
                      display: "flex",
                      gap: 8,
                      fontSize: 12.5,
                      marginBottom: 5,
                    }}
                  >
                    <b
                      style={{
                        flex: 1,
                        minWidth: 0,
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                      }}
                    >
                      {vendor.vendor}
                    </b>
                    <span
                      className="muted"
                      style={{ fontVariantNumeric: "tabular-nums" }}
                    >
                      {vendor.share_label}
                    </span>
                  </div>
                  <div className="bar">
                    <i style={{ width: `${width}%`, background: fill }} />
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </section>
  );
}

function ToxinLoadsTable({
  report,
  pageContract,
  searchParams,
  filter,
}: {
  report: ToxinReport;
  pageContract: AdminUiPageContract;
  searchParams: RouteSearchParams;
  filter: string;
}) {
  return (
    <section className="card" style={{ marginTop: 14 }}>
      <div className="tbar">
        <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
          {report.filters.map((chip) => (
            <NoPrefetchLink
              key={chip.key}
              href={toxinHref(searchParams, { filter: chip.key })}
              className={chip.selected ? "chip on" : "chip"}
              aria-current={chip.selected ? "true" : undefined}
            >
              {chip.label} <span className="cbq">{chip.count}</span>
            </NoPrefetchLink>
          ))}
        </div>
      </div>
      <div style={{ overflowX: "auto" }}>
        <table>
          <thead>
            <tr>
              <th>{copy(pageContract, "col.load")}</th>
              <th>{copy(pageContract, "col.supplier")}</th>
              <th>{copy(pageContract, "col.arrived")}</th>
              <th style={{ textAlign: "right" }}>
                {copy(pageContract, "col.quantity")}
              </th>
              <th>{copy(pageContract, "col.tested_by")}</th>
              <th>{copy(pageContract, "col.result")}</th>
              <th style={{ textAlign: "right" }}>
                {copy(pageContract, "col.turnaround")}
              </th>
            </tr>
          </thead>
          <tbody>
            {report.loads.length === 0 ? (
              <tr>
                <td
                  colSpan={7}
                  className="muted small"
                  style={{ padding: 26, textAlign: "center" }}
                >
                  {report.empty_message}
                </td>
              </tr>
            ) : (
              report.loads.map((load) => (
                <ToxinLoadRow key={load.feed_purchase_id} load={load} />
              ))
            )}
          </tbody>
        </table>
      </div>
      {report.next_cursor ? (
        <div className="tfoot">
          <span style={{ flex: 1 }} />
          <NoPrefetchLink
            className="btn sm"
            href={toxinHref(searchParams, {
              filter,
              cursor: report.next_cursor,
            })}
          >
            {copy(pageContract, "pager.next")}
          </NoPrefetchLink>
        </div>
      ) : null}
    </section>
  );
}

function ToxinLoadRow({ load }: { load: ToxinReportLoad }) {
  return (
    <tr>
      <td>
        <b style={{ display: "block", fontWeight: 650 }}>
          {load.feed_item_label}
        </b>
        <span className="muted small">{load.batch_label}</span>
      </td>
      <td>{load.vendor}</td>
      <td style={{ fontVariantNumeric: "tabular-nums" }}>
        {fmtDate(load.purchase_date)}
      </td>
      <td style={{ textAlign: "right", fontVariantNumeric: "tabular-nums" }}>
        {load.quantity_label}
      </td>
      <td className={load.tested_by_name ? undefined : "muted"}>
        {load.tested_by_label}
      </td>
      <td>
        <Tag tone={tone(load.result_tone)}>{load.result_label}</Tag>
      </td>
      <td
        className="muted"
        style={{ textAlign: "right", fontVariantNumeric: "tabular-nums" }}
      >
        {load.turnaround_label}
      </td>
    </tr>
  );
}
