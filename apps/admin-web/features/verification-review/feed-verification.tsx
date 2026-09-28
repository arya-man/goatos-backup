import Link from "@/components/no-prefetch-link";
import { Tag, type Tone } from "@/components/ui-primitives";
import type { DateRangePickerLabels } from "@/components/date-range-picker";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { getFeedPackingVerificationLog, type FeedPackingVerificationLogBag } from "@/lib/api/server";
import { fmtDate, fmtDateTime } from "@/lib/format";
import { FeedVerificationCsvButton } from "./feed-verification-csv-button";
import { VideoLogDateFilter } from "./video-log-date-filter";

/** Placeholder the page puts in the park href template; only URL-safe characters, so it survives. */
export const FEED_VERIFICATION_PARK_TOKEN = "__FVPARK__";

const STATUS_TONE: Record<string, Tone> = {
  verified: "ok",
  awaiting_verification: "warn",
  rework: "dng",
  not_packed: "mut",
};

/**
 * FEED VERIFICATION's contents (maintainer decision 2026-09-28): for ONE feed day -- the day the
 * animals eat -- every bag packed the day before, per park, pen and session, with each feed item's
 * planned quantity beside the weight the verifier entered.
 *
 * Gating: the caller MUST check `controlEnabled(pageContract, "feed_verification", false)` first;
 * the same capability (permissions.VerificationFeedPackingLog) gates the endpoint read here.
 *
 * THE PLAN OF AN UNDECIDED BAG NEVER REACHES THIS PAGE. The server withholds it (the verifier still
 * weighs those bags blind), so a bag awaiting verification or sent back renders one row naming its
 * feeds with "Shown once verified" -- there is no figure here to hide or to leak.
 *
 * Everything visible is backend-owned: the pen name is `operational_location_display`, the session
 * and feed names come from the frozen sheet, and every word comes from the page contract.
 */
export async function FeedVerification({
  pageContract,
  feedDay,
  parkId,
  parkFilter,
  parkHrefTemplate,
  allParksHref,
  dateLabels,
  basePath,
  today,
  dateKey,
  panelKey,
  panelId,
}: {
  pageContract: AdminUiPageContract;
  /** The feed day to show, YYYY-MM-DD; absent means today. */
  feedDay?: string;
  /** The page's own top-bar park scope, forwarded to the read. */
  parkId?: string;
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
}) {
  const t = (key: string) => copy(pageContract, `feed_verification.${key}`);
  const result = await getFeedPackingVerificationLog({ feedDay, parkId });
  if (!result.ok) {
    return <div className="small muted">{t("unavailable")}</div>;
  }
  const { feed_day: day, packing_day: packingDay, bags: allBags } = result.data;

  // Park options come from the day itself, so a park with nothing directed or packed is never
  // offered. Filtering in memory keeps the option list whole (a server-side filter would collapse
  // it to whatever is already picked) and the panel on ONE request.
  const parks = dedupeParks(allBags);
  const bags = parkFilter ? allBags.filter((bag) => bag.park_id === parkFilter) : allBags;
  const totals = parkFilter ? tally(bags) : result.data.totals;

  return (
    <div className="vr-videolog vr-feedverify">
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
        <FeedVerificationCsvButton
          label={t("download")}
          filename={`feed-verification-${day}.csv`}
          rows={csvRows(pageContract, day, packingDay, bags)}
        />
      </div>

      {parks.length > 1 ? (
        <div className="fv-parks" role="group" aria-label={t("filter.park")}>
          <Link href={allParksHref} replace scroll={false} className={`chip${parkFilter ? "" : " on"}`}>
            {t("filter.all_parks")}
          </Link>
          {parks.map((park) => (
            <Link
              key={park.id}
              href={parkHrefTemplate.replace(FEED_VERIFICATION_PARK_TOKEN, encodeURIComponent(park.id))}
              replace
              scroll={false}
              className={`chip${parkFilter === park.id ? " on" : ""}`}
            >
              {park.label}
            </Link>
          ))}
        </div>
      ) : null}

      {bags.length === 0 ? (
        <div className="small muted vl-empty">{t("empty_day")}</div>
      ) : (
        <>
          <div className="fv-kpis">
            <Kpi label={t("kpi.bags")} value={String(totals.bags)} />
            <Kpi label={t("kpi.verified")} value={String(totals.verified)} />
            <Kpi label={t("kpi.pending")} value={String(totals.awaiting_verification + totals.rework)} />
            <Kpi label={t("kpi.planned")} value={kgOrDash(totals.planned_kg)} />
            <Kpi label={t("kpi.entered")} value={kgOrDash(totals.entered_kg)} />
          </div>

          {/* A wide table scrolls inside its own wrapper, never the page (phone-390 rule). */}
          <div className="fv-tablewrap">
            <table className="tbl vl-tbl fv-table">
              <thead>
                <tr>
                  <th className="fv-desk">{t("col.park")}</th>
                  <th>{t("col.pen")}</th>
                  <th className="fv-desk">{t("col.session")}</th>
                  <th>{t("col.feed")}</th>
                  <th className="num">{t("col.planned")}</th>
                  <th className="num">{t("col.verified")}</th>
                  <th className="num">{t("col.difference")}</th>
                  <th className="fv-desk">{t("col.status")}</th>
                </tr>
              </thead>
              <tbody>
                {bags.map((bag) => (
                  <BagRows key={bagKey(bag)} bag={bag} t={t} />
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  );
}

function BagRows({ bag, t }: { bag: FeedPackingVerificationLogBag; t: (key: string) => string }) {
  const statusCell = (
    <td rowSpan={rowSpanOf(bag)} className="fv-status fv-desk">
      <Tag tone={STATUS_TONE[bag.status] ?? "mut"}>{t(`status.${bag.status}`)}</Tag>
      {bag.status === "verified" && (bag.verified_by_name || bag.verified_at) ? (
        <div className="small muted">
          {[bag.verified_by_name, bag.verified_at ? fmtDateTime(bag.verified_at) : ""].filter(Boolean).join(" · ")}
        </div>
      ) : null}
    </td>
  );
  const placeCells = (
    <>
      <td rowSpan={rowSpanOf(bag)} className="fv-park fv-desk">{bag.park_label}</td>
      <td rowSpan={rowSpanOf(bag)} className="fv-pen">
        <b>{bag.operational_location_display}</b>
        {/* Phone only: park, session and status fold in here so the feed and its three numbers fit
            a 390px screen without panning. The desktop columns carry the same facts. */}
        <div className="small muted fv-phone">
          {bag.park_label} · {sessionName(bag)}
        </div>
        <div className="fv-phone">
          <Tag tone={STATUS_TONE[bag.status] ?? "mut"}>{t(`status.${bag.status}`)}</Tag>
        </div>
        {bag.workflow === "experiment" ? (
          <div>
            <Tag tone="pur">{t("experiment")}</Tag>
          </div>
        ) : null}
      </td>
      <td rowSpan={rowSpanOf(bag)} className="fv-desk">{sessionName(bag)}</td>
    </>
  );

  // An undecided bag: which feeds are in it, and no quantity at all -- the server sent none.
  if (bag.status !== "verified") {
    const feeds = bag.items.map((item) => item.feed_item_label).filter(Boolean).join(", ");
    return (
      <tr className="fv-bag-first">
        {placeCells}
        <td>
          <div className="fv-feeds">{feeds || <span className="muted">{t("not_on_sheet")}</span>}</div>
        </td>
        <td colSpan={3} className="small muted fv-hidden-plan">
          {bag.status === "not_packed" ? "—" : t("plan_after_verdict")}
        </td>
        {statusCell}
      </tr>
    );
  }

  const items = bag.items.length > 0 ? bag.items : [];
  if (items.length === 0) {
    return (
      <tr className="fv-bag-first">
        {placeCells}
        <td className="muted">{t("not_on_sheet")}</td>
        <td className="num">—</td>
        <td className="num">—</td>
        <td className="num">—</td>
        {statusCell}
      </tr>
    );
  }
  return (
    <>
      {items.map((item, index) => (
        <tr key={item.feed_item_key} className={index === 0 ? "fv-bag-first" : undefined}>
          {index === 0 ? placeCells : null}
          <td>
            <span className="fv-feeds">{item.feed_item_label}</span>
            {item.variance_acknowledged ? (
              <>
                {" "}
                <Tag tone="info">{t("rechecked")}</Tag>
              </>
            ) : null}
          </td>
          <td className="num">{kgOrDash(item.planned_kg)}</td>
          <td className="num">{kgOrDash(item.entered_kg)}</td>
          <td className={`num ${diffClass(item.difference_kg)}`}>{signedKg(item.difference_kg)}</td>
          {index === 0 ? statusCell : null}
        </tr>
      ))}
      {items.length > 1 ? (
        <tr className="fv-bag-total">
          <td className="small muted">{t("bag_total")}</td>
          <td className="num">{kgOrDash(bag.planned_total_kg)}</td>
          <td className="num">{kgOrDash(bag.entered_total_kg)}</td>
          <td className={`num ${diffClass(diffOf(bag.entered_total_kg, bag.planned_total_kg))}`}>
            {signedKg(diffOf(bag.entered_total_kg, bag.planned_total_kg))}
          </td>
        </tr>
      ) : null}
    </>
  );
}

function Kpi({ label, value }: { label: string; value: string }) {
  return (
    <div className="fv-kpi">
      <div className="small muted">{label}</div>
      <div className="fv-kpi-v">{value}</div>
    </div>
  );
}

/** Rows a bag occupies: one per item (+ a total row) when verified, else one. */
function rowSpanOf(bag: FeedPackingVerificationLogBag): number {
  if (bag.status !== "verified" || bag.items.length === 0) return 1;
  return bag.items.length + (bag.items.length > 1 ? 1 : 0);
}

function bagKey(bag: FeedPackingVerificationLogBag): string {
  return `${bag.park_id}|${bag.shed_id}|${bag.partition_label}|${bag.session_no}|${bag.workflow}`;
}

function sessionName(bag: FeedPackingVerificationLogBag): string {
  return bag.session_label || `${bag.session_no}`;
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
  if (!value || !Number.isFinite(n) || n === 0) return "";
  return n > 0 ? "fv-over" : "fv-under";
}

/** Bag-level difference from the two server totals, which already range over the same items. */
function diffOf(entered: string, planned: string): string {
  if (!entered || !planned) return "";
  const d = Number(entered) - Number(planned);
  return Number.isFinite(d) ? d.toFixed(3) : "";
}

function dedupeParks(bags: FeedPackingVerificationLogBag[]): Array<{ id: string; label: string }> {
  const seen = new Map<string, string>();
  for (const bag of bags) if (!seen.has(bag.park_id)) seen.set(bag.park_id, bag.park_label);
  // The server already orders parks by code (CBE before CPT); insertion order preserves it.
  return [...seen].map(([id, label]) => ({ id, label }));
}

/** Totals for an in-panel park filter, over exactly the bags shown. */
function tally(bags: FeedPackingVerificationLogBag[]) {
  const sum = (pick: (bag: FeedPackingVerificationLogBag) => string) => {
    const parts = bags.map(pick).filter(Boolean);
    return parts.length ? parts.reduce((acc, v) => acc + Number(v), 0).toFixed(3) : "";
  };
  return {
    bags: bags.length,
    verified: bags.filter((b) => b.status === "verified").length,
    awaiting_verification: bags.filter((b) => b.status === "awaiting_verification").length,
    rework: bags.filter((b) => b.status === "rework").length,
    not_packed: bags.filter((b) => b.status === "not_packed").length,
    planned_kg: sum((b) => b.planned_total_kg),
    entered_kg: sum((b) => b.entered_total_kg),
  };
}

/** One CSV row per feed item (one per bag when undecided), in the screen's own words. */
function csvRows(
  pageContract: AdminUiPageContract,
  day: string,
  packingDay: string,
  bags: FeedPackingVerificationLogBag[],
): string[][] {
  const t = (key: string) => copy(pageContract, `feed_verification.${key}`);
  const header = [
    t("col.feed_day"),
    t("col.packing_day"),
    t("col.park"),
    t("col.pen"),
    t("col.session"),
    t("col.feed"),
    t("col.planned"),
    t("col.verified"),
    t("col.difference"),
    t("col.status"),
    t("col.verified_by"),
    t("col.verified_at"),
  ];
  const rows: string[][] = [header];
  for (const bag of bags) {
    const base = [fmtDate(day), fmtDate(packingDay), bag.park_label, bag.operational_location_display, sessionName(bag)];
    const tail = [t(`status.${bag.status}`), bag.verified_by_name, bag.verified_at ? fmtDateTime(bag.verified_at) : ""];
    if (bag.status !== "verified" || bag.items.length === 0) {
      const feeds = bag.items.map((item) => item.feed_item_label).join(", ");
      rows.push([...base, feeds, "", "", "", ...tail]);
      continue;
    }
    for (const item of bag.items) {
      rows.push([...base, item.feed_item_label, item.planned_kg, item.entered_kg, item.difference_kg, ...tail]);
    }
  }
  return rows;
}
