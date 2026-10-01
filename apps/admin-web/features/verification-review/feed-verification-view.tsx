"use client";

import Link from "@/components/no-prefetch-link";
import { Tag, type Tone } from "@/components/ui-primitives";
import type { DateRangePickerLabels } from "@/components/date-range-picker";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedPackingVerificationLogResponse, FeedPackingVerificationLogRow } from "@/lib/api/server";
import { fmtDate } from "@/lib/format";
import { FeedVerificationCsvButton } from "./feed-verification-csv-button";
import { FEED_VERIFICATION_PARK_TOKEN } from "./feed-verification-params";
import { VideoLogDateFilter } from "./video-log-date-filter";

const STATUS_TONE: Record<string, Tone> = {
  verified: "ok",
  awaiting_verification: "warn",
  rework: "dng",
  not_done: "mut",
};

/**
 * FEED VERIFICATION's contents (maintainer decisions 2026-09-28): for ONE feed day -- the day the
 * animals eat -- one row per park, pen and session with the planned total, the total the verifier
 * entered on YESTERDAY's packing, the total she entered on TODAY's feeding, and fed minus packed:
 * did what was packed reach the animals.
 *
 * A pure renderer of one already-fetched day. It is a CLIENT module so the drawer can render a day
 * it loaded itself (loadFeedVerificationLogAction) when the panel was opened by its button; the
 * server wrapper in feed-verification.tsx renders the same view for a deep link. It never fetches.
 *
 * A ROW'S FIGURES NEVER REACH THIS PAGE BEFORE ITS FEEDING VERDICT. The server withholds them (the
 * feeding verifier weighs blind, and the packed total would be her answer), so such a row renders
 * "Shown once feeding is verified" -- there is no figure here to hide or to leak.
 *
 * Everything visible is backend-owned: the pen name is `operational_location_display`, the session
 * name comes from the frozen sheet, and every word comes from the page contract.
 */
export type FeedVerificationViewProps = {
  pageContract: AdminUiPageContract;
  /** Park narrowing INSIDE the panel, applied in memory over the day already fetched. */
  parkFilter?: string;
  /** Href with FEED_VERIFICATION_PARK_TOKEN where a park id belongs; keeps the panel open. */
  parkHrefTemplate: string;
  /** Href clearing the in-panel park filter; keeps the panel open. */
  allParksHref: string;
  dateLabels: DateRangePickerLabels;
  basePath: string;
  today: string;
  dateKey: string;
  panelKey: string;
  panelId: string;
};

export function FeedVerificationView({
  pageContract,
  log,
  parkFilter,
  parkHrefTemplate,
  allParksHref,
  dateLabels,
  basePath,
  today,
  dateKey,
  panelKey,
  panelId,
}: FeedVerificationViewProps & { log: FeedPackingVerificationLogResponse }) {
  const t = (key: string) => copy(pageContract, `feed_verification.${key}`);
  const { feed_day: day, packing_day: packingDay, rows: allRows } = log;

  // Park options come from the day itself; filtering in memory keeps the option list whole and the
  // panel on ONE request.
  const parks = dedupeParks(allRows);
  const rows = parkFilter ? allRows.filter((row) => row.park_id === parkFilter) : allRows;
  const totals = parkFilter ? tally(rows) : log.totals;
  const selectedPark = parkFilter ? parks.find((park) => park.id === parkFilter) : undefined;

  return (
    <div className="vr-videolog vr-feedverify">
      {/* THE SELECTION BAR: the feed day and the park, pinned to the top of the drawer while the
          table scrolls. Everything below -- the totals, the table and the CSV -- is drawn from this
          selection and nothing else, so what is chosen here is always in view. */}
      <div className="fv-top">
        <div className="vl-head">
          <VideoLogDateFilter
            labels={dateLabels}
            basePath={basePath}
            day={day}
            today={today}
            dateKey={dateKey}
            panelKey={panelKey}
            panelId={panelId}
          />
          <span className="small muted fv-packed">
            {t("packed_on")} <b>{fmtDate(packingDay)}</b>
          </span>
          <span className="sp" style={{ flex: 1 }} />
          {rows.length > 0 ? (
            <FeedVerificationCsvButton
              label={t("download")}
              filename={`feed-verification-${day}${selectedPark ? `-${slug(selectedPark.label)}` : ""}.csv`}
              rows={csvRows(pageContract, day, packingDay, rows)}
            />
          ) : null}
        </div>

        {/* Shown whenever there is a park to choose OR a park is already chosen: a park picked on
            one day may hold nothing on the next, and the reader must still have "All parks" to get
            back, never a dead end. */}
        {parks.length > 1 || parkFilter ? (
          <div className="fv-parks" role="group" aria-label={t("filter.park")}>
            <Link href={allParksHref} replace scroll={false} className={parkFilter ? "" : "on"}>
              {t("filter.all_parks")}
            </Link>
            {parks.map((park) => (
              <Link
                key={park.id}
                href={parkHrefTemplate.replace(FEED_VERIFICATION_PARK_TOKEN, encodeURIComponent(park.id))}
                replace
                scroll={false}
                className={parkFilter === park.id ? "on" : ""}
              >
                {park.label}
              </Link>
            ))}
          </div>
        ) : null}
      </div>

      {rows.length === 0 ? (
        <div className="small muted vl-empty">{t("empty_day")}</div>
      ) : (
        <>
          {/* How much of the day there is to compare: counts on one slim line ... */}
          <div className="fv-counts">
            <Count label={t("kpi.rows")} value={totals.rows} />
            <Count label={t("kpi.compared")} value={totals.compared} />
            <Count label={t("kpi.awaiting_feeding")} value={totals.awaiting_feeding} />
          </div>
          {/* ... and the four kilogram totals as tiles, in the same order as the table's columns.
              They range over the CHECKED rows only, so the four always describe the same bags. */}
          <div className="fv-kpis">
            <Kpi label={t("kpi.planned")} value={kgOrDash(totals.planned_kg)} />
            <Kpi label={t("kpi.packed")} value={kgOrDash(totals.packed_kg)} />
            <Kpi label={t("kpi.fed")} value={kgOrDash(totals.fed_kg)} />
            <Kpi label={t("kpi.difference")} value={signedKg(totals.difference_kg)} tone={diffClass(totals.difference_kg)} />
          </div>

          {/* A wide table scrolls inside its own wrapper, never the page (phone-390 rule). */}
          <div className="fv-tablewrap">
            <table className="tbl vl-tbl fv-table">
              <thead>
                <tr>
                  <th className="fv-desk">{t("col.park")}</th>
                  <th>{t("col.pen")}</th>
                  <th className="fv-desk">{t("col.session")}</th>
                  <th className="num">{t("col.planned")}</th>
                  <th className="num">{t("col.packed")}</th>
                  <th className="num">{t("col.fed")}</th>
                  <th className="num">{t("col.difference")}</th>
                  <th className="fv-desk">{t("col.packing")}</th>
                  <th className="fv-desk">{t("col.feeding")}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <Row key={rowKey(row)} row={row} t={t} />
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  );
}

function Row({ row, t }: { row: FeedPackingVerificationLogRow; t: (key: string) => string }) {
  const packing = <Tag tone={STATUS_TONE[row.packing_status] ?? "mut"}>{t(`status.${row.packing_status}`)}</Tag>;
  const feeding = <Tag tone={STATUS_TONE[row.feeding_status] ?? "mut"}>{t(`status.${row.feeding_status}`)}</Tag>;
  const figuresShown = row.feeding_status === "verified";
  return (
    <tr>
      <td className="fv-park fv-desk">{row.park_label}</td>
      <td className="fv-pen">
        <b>{row.operational_location_display}</b>
        {row.workflow === "experiment" ? (
          <div>
            <Tag tone="pur">{t("experiment")}</Tag>
          </div>
        ) : null}
        {/* Phone only: park, session and both statuses fold in here so the three totals fit a
            390px screen without panning. The desktop columns carry the same facts. */}
        <div className="small muted fv-phone">
          {row.park_label} · {sessionName(row)}
        </div>
        <div className="fv-phone fv-phone-tags">
          <span className="small muted">{t("col.packing")}</span> {packing}
          <span className="small muted">{t("col.feeding")}</span> {feeding}
        </div>
      </td>
      <td className="fv-desk">{sessionName(row)}</td>
      {figuresShown ? (
        <>
          <td className="num">{kgOrDash(row.planned_kg)}</td>
          <td className="num">{kgOrDash(row.packed_kg)}</td>
          <td className="num">{kgOrDash(row.fed_kg)}</td>
          <td className={`num ${diffClass(row.difference_kg)}`}>{signedKg(row.difference_kg)}</td>
        </>
      ) : row.feeding_status === "not_done" && row.packing_status === "not_done" ? (
        // Nothing packed or fed yet: one dash per figure, aligned like the figures themselves, so
        // the row reads "no numbers yet" rather than "numbers missing".
        <>
          <td className="num muted">—</td>
          <td className="num muted">—</td>
          <td className="num muted">—</td>
          <td className="num muted">—</td>
        </>
      ) : (
        <td colSpan={4} className="small muted fv-hidden-plan">
          {t("after_feeding_verdict")}
        </td>
      )}
      <td className="fv-status fv-desk">{packing}</td>
      <td className="fv-status fv-desk">{feeding}</td>
    </tr>
  );
}

function Count({ label, value }: { label: string; value: number }) {
  return (
    <span className="fv-count">
      <span className="small muted">{label}</span> <b>{value}</b>
    </span>
  );
}

function Kpi({ label, value, tone }: { label: string; value: string; tone?: string }) {
  return (
    <div className="fv-kpi">
      <div className="small muted">{label}</div>
      <div className={`fv-kpi-v ${tone ?? ""}`}>{value}</div>
    </div>
  );
}

/** A file-name-safe form of a park label. */
function slug(label: string): string {
  return label.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
}

function rowKey(row: FeedPackingVerificationLogRow): string {
  return `${row.park_id}|${row.shed_id}|${row.partition_label}|${row.session_no}|${row.workflow}`;
}

function sessionName(row: FeedPackingVerificationLogRow): string {
  return row.session_label || `${row.session_no}`;
}

function kgOrDash(value: string): string {
  if (!value) return "—";
  const n = Number(value);
  if (!Number.isFinite(n)) return value;
  return n.toLocaleString("en-IN", { maximumFractionDigits: 3 });
}

function signedKg(value: string): string {
  if (!value) return "—";
  const n = Number(value);
  if (!Number.isFinite(n)) return value;
  const shown = Math.abs(n).toLocaleString("en-IN", { maximumFractionDigits: 3 });
  return n > 0 ? `+${shown}` : n < 0 ? `−${shown}` : "0";
}

function diffClass(value: string): string {
  const n = Number(value);
  if (!value || !Number.isFinite(n)) return "";
  if (n === 0) return "fv-same";
  return n > 0 ? "fv-over" : "fv-under";
}

function dedupeParks(rows: FeedPackingVerificationLogRow[]): Array<{ id: string; label: string }> {
  const seen = new Map<string, string>();
  for (const row of rows) if (!seen.has(row.park_id)) seen.set(row.park_id, row.park_label);
  // The server already orders parks by code (CBE before CPT); insertion order preserves it.
  return [...seen].map(([id, label]) => ({ id, label }));
}

/** Totals for an in-panel park filter, over exactly the rows shown -- compared rows only for kg. */
function tally(rows: FeedPackingVerificationLogRow[]) {
  const compared = rows.filter((r) => r.packed_kg && r.fed_kg);
  const sum = (pick: (r: FeedPackingVerificationLogRow) => string) =>
    compared.length ? compared.reduce((acc, r) => acc + Number(pick(r)), 0).toFixed(3) : "";
  return {
    rows: rows.length,
    compared: compared.length,
    awaiting_packing: rows.filter((r) => r.packing_status === "awaiting_verification").length,
    awaiting_feeding: rows.filter((r) => r.feeding_status === "awaiting_verification").length,
    planned_kg: sum((r) => r.planned_kg),
    packed_kg: sum((r) => r.packed_kg),
    fed_kg: sum((r) => r.fed_kg),
    difference_kg: sum((r) => r.difference_kg),
  };
}

/** One CSV row per pen-session, in the screen's own words. */
function csvRows(
  pageContract: AdminUiPageContract,
  day: string,
  packingDay: string,
  rows: FeedPackingVerificationLogRow[],
): string[][] {
  const t = (key: string) => copy(pageContract, `feed_verification.${key}`);
  const out: string[][] = [
    [
      t("col.feed_day"),
      t("col.packing_day"),
      t("col.park"),
      t("col.pen"),
      t("col.session"),
      t("col.planned"),
      t("col.packed"),
      t("col.fed"),
      t("col.difference"),
      t("col.packing"),
      t("col.feeding"),
    ],
  ];
  for (const row of rows) {
    out.push([
      fmtDate(day),
      fmtDate(packingDay),
      row.park_label,
      row.operational_location_display,
      sessionName(row),
      row.planned_kg,
      row.packed_kg,
      row.fed_kg,
      row.difference_kg,
      t(`status.${row.packing_status}`),
      t(`status.${row.feeding_status}`),
    ]);
  }
  return out;
}
