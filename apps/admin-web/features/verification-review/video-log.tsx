import type { ReactNode } from "react";

import Link from "@/components/no-prefetch-link";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { getVerificationVideoLog, type VerificationVideoLogResponse } from "@/lib/api/server";
import { fmtDateTime } from "@/lib/format";
import type { DateRangePickerLabels } from "@/components/date-range-picker";
import { VIDEO_LOG_PARK_KEY, VIDEO_LOG_QUERY_KEY, VIDEO_LOG_SHED_KEY, VIDEO_LOG_SHED_TOKEN } from "./video-log-params";
import { VideoLogDateFilter } from "./video-log-date-filter";
import { VideoLogCsvButton } from "./video-log-csv-button";

/**
 * The VIDEO LOG's contents: for ONE business day, which sheds had proof arrive and at what time
 * (maintainer decision 2026-08-14).
 *
 * Gating: the caller MUST check `controlEnabled(pageContract, "video_log", false)` before rendering
 * this -- the SAME capability (permissions.VerificationEvidenceTimeline) gates both the page
 * contract control and the endpoint read here. This component reads the contract for COPY only,
 * never for its own gating decision; it trusts the caller. That capability is deliberately NOT
 * verification.oversee, so a verifier reaches this panel and still gets no module chips, no
 * capture-date range picker and no analytics drawer.
 *
 * TWO LEVELS, because one flat list of a day's proofs is not bounded -- a vaccination drive raises
 * one item per animal, so a park-day can hold several hundred items. Level 1 is one row per shed.
 * Level 2 is one shed's work in full, reached by picking a shed.
 *
 * Everything visible here is backend-composed: the location display comes from
 * operational_location_display (never shed name + partition joined locally), module and category
 * labels come from the registry, each proof's header comes from the artifact or the registry, and
 * the subject line is the producing module's own words rendered verbatim.
 */
export async function VideoLog({
  pageContract,
  businessDate,
  parkId,
  selectedShedKey,
  parkFilter,
  query,
  filterAction,
  filterHiddenInputs,
  clearHref,
  shedHrefTemplate,
  backHref,
  queueHrefs,
  dateLabels,
  basePath,
  today,
}: {
  pageContract: AdminUiPageContract;
  /** Calendar copy, from the page contract's existing filter.date.* keys. */
  dateLabels: DateRangePickerLabels;
  /** Route the day picker rewrites, so the panel's own URL contract stays on /verify. */
  basePath: string;
  /** Today's business day (Asia/Kolkata), resolved on the server — the picker's default. */
  today: string;
  /** The day to show, YYYY-MM-DD. */
  businessDate?: string;
  /** Optional park filter, mirrored from the page's own scope. */
  parkId?: string;
  /** The shed whose detail is open, as the backend's composite shed_key. */
  selectedShedKey?: string;
  /** Park narrowing INSIDE the panel; empty means every park in the page's scope. */
  parkFilter?: string;
  /** Free-text filter over the day's rows. */
  query?: string;
  /** Route the filter form submits to. */
  filterAction: string;
  /** Hidden inputs that carry the rest of the URL through the filter submit. */
  filterHiddenInputs: ReactNode;
  /** Href that drops every panel filter but keeps the panel open. */
  clearHref: string;
  /**
   * Href containing VIDEO_LOG_SHED_TOKEN where a shed key belongs. Only the page knows the live
   * search params and only this component knows which sheds the day holds, so the page hands over
   * a template and this substitutes each key. Plain data, not a callback: this element is handed
   * to a client component as children, and a function prop would not survive that.
   */
  shedHrefTemplate?: string;
  /** Href that clears the shed selection and returns to the day summary. */
  backHref: string;
  /** Queue hrefs keyed by NAV module key, so a row can link to its own module's queue. */
  queueHrefs?: Map<string, string>;
}) {
  const result = await getVerificationVideoLog({ businessDate, parkId, shedId: selectedShedKey });
  if (!result.ok) {
    return <div className="small muted">{copy(pageContract, "video_log.unavailable")}</div>;
  }
  const { business_date: day, sheds, rows, rows_truncated: truncated, selected_shed_id: selected } = result.data;
  const selectedShed = selected ? sheds.find((shed) => shed.shed_key === selected) : undefined;

  // Park and search narrow the day IN MEMORY, over rows the single fetch already returned.
  //
  // Deliberately not extra query params on the read: the option vocabulary has to list every park
  // and shed the day actually holds, and filtering server-side would collapse those lists to
  // whatever is already selected — the classic filter that erases its own options. A day is at most
  // a few dozen shed rows, so this costs nothing and keeps the panel on ONE request.
  const needle = (query ?? "").trim().toLowerCase();
  const parkFiltered = parkFilter ? sheds.filter((shed) => shed.park_id === parkFilter) : sheds;
  const visibleSheds = needle
    ? parkFiltered.filter((shed) => shedMatches(shed, needle))
    : parkFiltered;
  const visibleRows = needle ? rows.filter((row) => rowMatches(row, needle)) : rows;

  // Park options come from the day itself, so a park with no arrivals is never offered. Labels are
  // the backend's own; this composes none of them.
  const parkOptions = dedupeParks(sheds);

  return (
    <div className="vr-videolog">
      {/* The day is a CALENDAR FILTER, defaulting to today (maintainer request 2026-08-15). Today is
          expressed by an ABSENT vl_date so a shared link keeps meaning "today" rather than freezing
          on the day it was copied. `day` comes back from the backend, so the picker always shows the
          day actually rendered — never a client guess that could drift from the data below it. */}
      {/* The picker carries its own backend-owned field label, so there is no separate label
          element beside it — an earlier build rendered both and printed the day label twice, the
          second time with the queue's capture-date wording, which is a different fact. */}
      <div className="vl-head">
        <VideoLogDateFilter labels={dateLabels} basePath={basePath} day={day} today={today} />
        <span className="sp" style={{ flex: 1 }} />
        {/* ONE button, and it always exports the WHOLE DAY — every shed, one row per video — not
            whichever level happens to be on screen (maintainer, 2026-08-15). The rows are fetched
            on click, so opening the panel never pays for a file most viewers do not ask for. */}
        <VideoLogCsvButton
          label={copy(pageContract, "video_log.download")}
          filename={csvFilename(day)}
          businessDate={day}
          // The PANEL's park filter wins over the page scope: the file must match the day the
          // reader is looking at, not a wider one they narrowed away from.
          parkId={parkFilter || parkId}
          headers={csvHeaders(pageContract)}
          awaitingLabel={copy(pageContract, "video_log.awaiting_upload_one")}
          truncatedNote={copy(pageContract, "video_log.export_truncated")}
        />
      </div>

      <VideoLogFilters
        pageContract={pageContract}
        action={filterAction}
        hidden={filterHiddenInputs}
        clearHref={clearHref}
        parkFilter={parkFilter}
        parkOptions={parkOptions}
        sheds={parkFiltered}
        selectedShedKey={selected ?? ""}
        query={query ?? ""}
      />

      {selected ? (
        <ShedDetail
          pageContract={pageContract}
          day={day}
          shedDisplay={selectedShed?.operational_location_display ?? ""}
          rows={visibleRows}
          truncated={truncated}
          backHref={backHref}
          queueHrefs={queueHrefs}
        />
      ) : (
        <DaySummary pageContract={pageContract} sheds={visibleSheds} shedHrefTemplate={shedHrefTemplate} day={day} />
      )}
    </div>
  );
}


/**
 * The panel's own filter row: park, shed, and free-text search.
 *
 * A FORM + Apply, matching the queue's shed filter directly below the drawer, rather than a new
 * live-filtering paradigm on the same screen. Every value lives in the URL, so a filtered view is
 * shareable and Back works.
 *
 * The PARK control narrows within the top bar's scope; it does not replace it. The Scope Chrome
 * Rule keeps park scope in the top bar, and this panel deliberately does not touch that — it lets a
 * reader look at one park's arrivals without re-scoping the queue behind the drawer.
 */
function VideoLogFilters({
  pageContract,
  action,
  hidden,
  clearHref,
  parkFilter,
  parkOptions,
  sheds,
  selectedShedKey,
  query,
}: {
  pageContract: AdminUiPageContract;
  action: string;
  hidden: ReactNode;
  clearHref: string;
  parkFilter?: string;
  parkOptions: Array<{ id: string; label: string }>;
  sheds: VideoLogSheds;
  selectedShedKey: string;
  query: string;
}) {
  // Sheds grouped by park for the same reason the queue's picker groups them: a shed NAME is not
  // unique across the farm, so an ungrouped list shows the reader two identical-looking options.
  const groups = sheds.reduce<Array<[string, VideoLogSheds]>>((acc, shed) => {
    const park = shed.park_label ?? "";
    const existing = acc.find(([label]) => label === park);
    if (existing) existing[1].push(shed);
    else acc.push([park, [shed]]);
    return acc;
  }, []);

  return (
    <form action={action} className="vl-frow">
      {hidden}
      <div className="vr-fld fld" style={{ marginBottom: 0 }}>
        <label htmlFor="vl-park">{copy(pageContract, "video_log.filter.park")}</label>
        <select id="vl-park" name={VIDEO_LOG_PARK_KEY} className="vr-selbtn" defaultValue={parkFilter ?? ""}>
          <option value="">{copy(pageContract, "video_log.filter.all_parks")}</option>
          {parkOptions.map((park) => (
            <option key={park.id} value={park.id}>
              {park.label}
            </option>
          ))}
        </select>
      </div>
      <div className="vr-fld fld" style={{ marginBottom: 0 }}>
        <label htmlFor="vl-shed">{copy(pageContract, "video_log.filter.shed")}</label>
        {/* Picking a shed here opens that shed's videos — the same destination as clicking its row,
            which is the point: on a 74-shed day, scrolling to find one pen is the slow path. */}
        <select id="vl-shed" name={VIDEO_LOG_SHED_KEY} className="vr-selbtn" defaultValue={selectedShedKey}>
          <option value="">{copy(pageContract, "video_log.filter.all_sheds")}</option>
          {groups.map(([park, group]) =>
            park ? (
              <optgroup key={park} label={park}>
                {group.map((shed) => (
                  <option key={shed.shed_key} value={shed.shed_key}>
                    {shed.operational_location_display}
                  </option>
                ))}
              </optgroup>
            ) : (
              group.map((shed) => (
                <option key={shed.shed_key} value={shed.shed_key}>
                  {shed.operational_location_display}
                </option>
              ))
            ),
          )}
        </select>
      </div>
      <div className="vr-fld fld vl-search" style={{ marginBottom: 0 }}>
        <label htmlFor="vl-q">{copy(pageContract, "video_log.filter.search")}</label>
        <input
          id="vl-q"
          name={VIDEO_LOG_QUERY_KEY}
          className="vr-selbtn"
          type="search"
          defaultValue={query}
          placeholder={copy(pageContract, "video_log.filter.search_hint")}
        />
      </div>
      {/* Grouped so the pair wraps TOGETHER: at the drawer's width the three fields fill the first
          line and Clear was landing alone on a second one, reading as an unrelated control. */}
      <div className="vl-fbtns">
        <button type="submit" className="btn sm">
          {copy(pageContract, "video_log.filter.apply")}
        </button>
        <Link href={clearHref} className="btn sm">
          {copy(pageContract, "video_log.filter.clear")}
        </Link>
      </div>
    </form>
  );
}

/**
 * Park options for the day, in the order the backend returned its sheds.
 *
 * Keyed by park ID, never by label: park NAMES are as repeatable as shed names, and keying on the
 * label is the OL-1 merge one level up. A shed whose park the backend could not resolve is skipped
 * rather than filed under a guess.
 */
function dedupeParks(sheds: VideoLogSheds): Array<{ id: string; label: string }> {
  const byId = new Map<string, string>();
  for (const shed of sheds) {
    const id = shed.park_id ?? "";
    const label = shed.park_label ?? "";
    if (!id || !label || byId.has(id)) continue;
    byId.set(id, label);
  }
  return [...byId].map(([id, label]) => ({ id, label }));
}

/** Search over a summary row: its location and the modules that contributed. */
function shedMatches(shed: VideoLogSheds[number], needle: string): boolean {
  return [shed.operational_location_display, shed.park_label ?? "", ...shed.modules]
    .some((field) => field.toLowerCase().includes(needle));
}

/**
 * Search over a detail row: everything a reader can see on it — the work, the animal or session the
 * producer named, who captured it, and each video's own label.
 */
function rowMatches(row: VideoLogRows[number], needle: string): boolean {
  const fields = [
    row.category_label ?? "",
    row.module_label ?? "",
    row.subject_label ?? "",
    row.operator_name ?? "",
    row.operational_location_display ?? "",
    ...row.proofs.map((proof) => proof.label ?? ""),
  ];
  return fields.some((field) => field.toLowerCase().includes(needle));
}

function DaySummary({
  pageContract,
  sheds,
  shedHrefTemplate,
  day,
}: {
  pageContract: AdminUiPageContract;
  sheds: VideoLogSheds;
  shedHrefTemplate?: string;
  day: string;
}) {
  if (sheds.length === 0) {
    return <div className="small muted vl-empty">{copy(pageContract, "video_log.empty_day")}</div>;
  }
  return (
    <table className="tbl vl-tbl">
      <thead>
        <tr>
          <th>{copy(pageContract, "video_log.col.shed")}</th>
          <th>{copy(pageContract, "video_log.col.videos")}</th>
          <th>{copy(pageContract, "video_log.col.first_last")}</th>
        </tr>
      </thead>
      <tbody>
        {sheds.map((shed) => {
          // shed_key carries a "#" separator, so it MUST be encoded before it goes into a query
          // value -- unencoded it would truncate the URL into a fragment and the panel would open
          // with no shed selected.
          const href = shedHrefTemplate
            ? shedHrefTemplate.replace(VIDEO_LOG_SHED_TOKEN, encodeURIComponent(shed.shed_key))
            : undefined;
          // The composed display is the ONLY location string rendered. A shed whose day is only
          // weighing/shifting/birth/death carries no partition (those producers do not record one),
          // so it correctly shows its bare shed name rather than a dangling separator.
          const display = shed.operational_location_display;
          return (
            <tr key={shed.shed_key}>
              <td>
                {href ? (
                  <Link href={href} className="lnk">
                    {display}
                  </Link>
                ) : (
                  display
                )}
                {shed.modules.length > 0 ? <div className="small muted">{shed.modules.join(" · ")}</div> : null}
                {/* The per-video times live one level down, and the shed name alone did not say so
                    — it read as a plain label, so the drill-down was undiscoverable. This is the
                    affordance, in backend-owned copy. */}
                {href ? (
                  <Link href={href} className="small vl-drill">
                    {copy(pageContract, "video_log.view_videos")} →
                  </Link>
                ) : null}
              </td>
              <td>
                <span className="val">{shed.proof_count}</span>{" "}
                <span className="small muted">{copy(pageContract, "video_log.videos_count")}</span>
                <div className="small muted">
                  {shed.item_count} {copy(pageContract, "video_log.items_count")}
                </div>
                {/* Registered but not received. Counted INSIDE proof_count, so this is a
                    breakdown of the number above it, never a second total beside it. */}
                {shed.awaiting_upload_count > 0 ? (
                  <div className="small warn">
                    {shed.awaiting_upload_count} {copy(pageContract, "video_log.awaiting_upload")}
                  </div>
                ) : null}
              </td>
              <td>
                <ArrivalRange first={shed.first_upload_at} last={shed.last_upload_at} day={day} />
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

function ShedDetail({
  pageContract,
  day,
  shedDisplay,
  rows,
  truncated,
  backHref,
  queueHrefs,
}: {
  pageContract: AdminUiPageContract;
  day: string;
  shedDisplay: string;
  rows: VideoLogRows;
  truncated: boolean;
  backHref: string;
  queueHrefs?: Map<string, string>;
}) {
  return (
    <>
      <div className="vl-crumb">
        <Link href={backHref} className="lnk">
          ← {copy(pageContract, "video_log.back_to_sheds")}
        </Link>
        {shedDisplay ? <span className="val">{shedDisplay}</span> : null}
      </div>

      {rows.length === 0 ? (
        <div className="small muted vl-empty">{copy(pageContract, "video_log.empty_shed")}</div>
      ) : (
        <table className="tbl vl-tbl">
          <thead>
            <tr>
              <th>{copy(pageContract, "video_log.col.work")}</th>
              <th>{copy(pageContract, "video_log.col.video")}</th>
              <th>{copy(pageContract, "video_log.col.uploaded")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => {
              const href = row.nav_module ? queueHrefs?.get(row.nav_module) : undefined;
              const moduleLabel = row.module_label?.trim() || "";
              const categoryLabel = row.category_label?.trim() || "";
              return row.proofs.map((proof, index) => (
                // One table row per PROOF, with the work described only on its first row: a feed
                // distribution item is three arrivals at three different times, and collapsing them
                // onto one line would hide exactly the times this panel exists to show.
                <tr key={proof.proof_id}>
                  {index === 0 ? (
                    <td rowSpan={row.proofs.length}>
                      <div className="val">
                        {href ? (
                          <Link href={href} className="lnk">
                            {categoryLabel || moduleLabel}
                          </Link>
                        ) : (
                          categoryLabel || moduleLabel
                        )}
                      </div>
                      {/* The producing module's own words, verbatim. Legitimately EMPTY for feed
                          transport, whose producer writes no label because the shed header already
                          names it -- render nothing rather than a placeholder that looks broken. */}
                      {row.subject_label ? <div className="small">{row.subject_label}</div> : null}
                      {row.operator_name ? <div className="small muted">{row.operator_name}</div> : null}
                      <div className="small muted">
                        {/* Neutral tone on purpose: grain is a FACT about the work, not a status.
                            A coloured chip here would read as a warning about the row. */}
                        <Tag tone="mut">{copy(pageContract, `video_log.grain.${row.grain}`)}</Tag>
                      </div>
                    </td>
                  ) : null}
                  {/* The backend-owned proof label already names the medium where it matters, so a
                      locally-appended media-kind suffix was both a hardcoded visible literal and a
                      duplicate of what the label already says. */}
                  <td>{proof.label}</td>
                  <td>
                    <ArrivalTime uploadedAt={proof.uploaded_at} day={day} pageContract={pageContract} />
                  </td>
                </tr>
              ));
            })}
          </tbody>
        </table>
      )}

      {/* No silent caps: a truncated shed says so rather than reading as a complete day. */}
      {truncated ? <div className="small warn vl-note">{copy(pageContract, "video_log.truncated")}</div> : null}
    </>
  );
}

/**
 * Renders one proof's arrival time.
 *
 * Shows HH:MM when the video landed on the business day it belongs to, and the full date-time when
 * it landed LATER -- a video shot in the evening and uploaded next morning is a real and important
 * case, and a bare "07:12" would read as same-day. Both come from the single IST formatter in
 * lib/format, so no timezone arithmetic is re-derived here.
 */
function ArrivalTime({
  uploadedAt,
  day,
  pageContract,
}: {
  uploadedAt?: string;
  day: string;
  pageContract: AdminUiPageContract;
}) {
  if (!uploadedAt) {
    return <span className="small warn">{copy(pageContract, "video_log.awaiting_upload_one")}</span>;
  }
  const formatted = fmtDateTime(uploadedAt);
  const [datePart, timePart] = formatted.split(" ");
  if (!timePart) return <span className="val">{formatted}</span>;
  if (datePart === day) return <span className="val">{timePart}</span>;
  return (
    <span className="val">
      {timePart}
      <span className="small muted"> · {copy(pageContract, "video_log.arrived_later")} {datePart}</span>
    </span>
  );
}

/**
 * The bracket around a shed's arrivals for the day.
 *
 * An end that landed on a DIFFERENT calendar date carries that date, because this range genuinely
 * can cross midnight: the day is cut on when the WORK was recorded (the item's own anchor) while
 * these are UPLOAD times, so a proof filmed late can arrive the next morning — and one filmed
 * before its work was stamped can arrive the day before. Rendering both ends as bare clock times
 * made exactly that case read as going backwards in time ("15:18 – 11:32" on a real shed).
 */
function ArrivalRange({ first, last, day }: { first?: string; last?: string; day: string }) {
  if (!first && !last) return <span className="muted">—</span>;
  return (
    <span className="val">
      <ArrivalStamp iso={first} day={day} />
      {" – "}
      <ArrivalStamp iso={last} day={day} />
    </span>
  );
}

/** HH:MM on the business day; the date is added the moment the arrival falls outside it. */
function ArrivalStamp({ iso, day }: { iso?: string; day: string }) {
  if (!iso) return <>—</>;
  const formatted = fmtDateTime(iso);
  const [datePart, timePart] = formatted.split(" ");
  if (!timePart) return <>{formatted}</>;
  if (datePart === day) return <>{timePart}</>;
  return (
    <>
      {timePart}
      <span className="small muted"> {datePart}</span>
    </>
  );
}

type VideoLogSheds = VerificationVideoLogResponse["sheds"];
type VideoLogRows = VerificationVideoLogResponse["rows"];

/** `video-log-2026-08-12.csv`, or `video-log-2026-08-12-Godel 1 - Part 3.csv` for one shed. */
function csvFilename(day: string, shedDisplay?: string): string {
  const shed = shedDisplay?.trim();
  // Only characters a filesystem genuinely rejects are replaced; the shed's real name is worth
  // keeping in the filename, since that is how a reader tells two exports apart.
  const suffix = shed ? `-${shed.replace(/[\\/:*?"<>|]/g, "-")}` : "";
  return `video-log-${day}${suffix}.csv`;
}

/**
 * The export's column headers, in the screen's own backend-owned words.
 *
 * Passed into the server action rather than built there so the file is labelled exactly as the
 * table is — a reader opening this a week later should not have to map columns back to a screen.
 */
function csvHeaders(pageContract: AdminUiPageContract): string[] {
  return [
    copy(pageContract, "video_log.day"),
    copy(pageContract, "video_log.col.park"),
    copy(pageContract, "video_log.col.shed"),
    copy(pageContract, "video_log.col.work"),
    copy(pageContract, "video_log.col.video"),
    copy(pageContract, "video_log.col.uploaded"),
  ];
}

