import { LocalOverlayLink } from "@/components/local-overlay-link";
import { LocalOverlayDrawer, type LocalOverlayDrawerItem } from "@/components/local-overlay-drawer";
import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDateTime, todayIso } from "@/lib/format";
import type { FeedAnalyticsExecutionResponse } from "@/lib/api/server";
import type { RouteSearchParams } from "@/lib/search-params";
import { FeedFilters, type FeedFilterField } from "./feed-filters";
import { FeedPager } from "./feed-pager";

// Feed direction completion — WHO fed, WHO did not, and what they filmed (maintainer decision
// 2026-08-26: operators were skipping proof uploads and no screen could show it). One row per
// PEN-SESSION of the chosen feed day, which is the grain the completion and its three videos live
// at; a shed-level row would let one pen's video read as the whole shed's work.
//
// Everything on screen is backend-owned: the day, the page, the four counts, the filter vocabulary,
// the status wording and the location string all arrive in the payload. This file renders them. The
// one thing it derives is the "n of 3" video tally, a count of the payload's own fixed three slots.
//
// Paging is SERVER-SIDE, like the mismatch table above it: the rows array IS the page. Slicing a
// whole park-day client-side would ship two hundred pen-sessions and their proofs to draw ten.

type CompletionRow = FeedAnalyticsExecutionResponse["distribution_completions"][number];
type ProofSlot = CompletionRow["proofs"][number];
type CompletionTotals = FeedAnalyticsExecutionResponse["completion_totals"];

/** The four backend buckets, in worst-first order — the reader is here for what went wrong. */
const STATUS_ORDER = ["not_started", "rework", "pending_verification", "completed"] as const;
type CompletionStatus = (typeof STATUS_ORDER)[number];

const STATUS_TONE: Record<CompletionStatus, Tone> = {
  not_started: "dng",
  rework: "warn",
  pending_verification: "info",
  completed: "ok",
};

const STATUS_COPY_KEY: Record<CompletionStatus, string> = {
  not_started: "completion.status.not_started",
  rework: "completion.status.rework",
  pending_verification: "completion.status.await",
  completed: "completion.status.completed",
};

const STATUS_KPI_KEY: Record<CompletionStatus, string> = {
  not_started: "completion.kpi.not_started",
  rework: "completion.kpi.rework",
  pending_verification: "completion.kpi.await",
  completed: "completion.kpi.completed",
};

/** Slot label keys, keyed by the backend's own field_key vocabulary. */
const SLOT_COPY_KEY: Record<string, string> = {
  feed_distribution_feed_weight_photo: "completion.slot.weight",
  feed_distribution_video: "completion.slot.feed",
  feed_distribution_water_video: "completion.slot.water",
};

function isStatus(raw: string): raw is CompletionStatus {
  return (STATUS_ORDER as readonly string[]).includes(raw);
}

function totalFor(totals: CompletionTotals, status: CompletionStatus): number {
  return totals[status] ?? 0;
}

/**
 * An upload/verdict instant in the farm's own clock, through the shared IST formatter.
 *
 * DATE AND TIME, not time alone: a pen fed at 21:40 can be submitted after midnight, and a verdict
 * lands whenever the verifier gets to it — a bare "12:05" against a row labelled with yesterday's
 * feed day would silently misdate both. Null stays null so the caller renders the contract's em
 * dash rather than the epoch, which would read as a real upload in 1970.
 */
function istInstant(iso: string | null | undefined): string | null {
  if (!iso) return null;
  const formatted = fmtDateTime(iso);
  return formatted === "" ? null : formatted;
}

/** Stable identity of a pen-session: the completion's own natural key, minus the tenant. */
function rowId(row: CompletionRow): string {
  return [row.park_id, row.shed_id, row.partition_label ?? "", row.session_no, row.workflow].join("|");
}

function recordedCount(row: CompletionRow): number {
  return row.proofs.filter((slot) => slot.proof_ref !== "").length;
}

export function FeedCompletionTable({
  data,
  searchParams,
  basePath,
  pageContract,
  filters,
  parkScopeLocked,
}: {
  data: FeedAnalyticsExecutionResponse;
  searchParams: RouteSearchParams;
  basePath: string;
  pageContract: AdminUiPageContract;
  filters: {
    park: string;
    shed: string;
    status: string;
    selectedRowId: string;
    offset: number;
    limit: number;
    pageSizes: number[];
  };
  /** True when the top bar already fixes the park, which disables this table's own park select. */
  parkScopeLocked: boolean;
}) {
  const fc = (key: string) => copy(pageContract, key);
  // The rows ARE the page; the totals and the options describe the whole day.
  const rows = data.distribution_completions ?? [];
  const totals = data.completion_totals;
  const options = data.completion_filter_options ?? [];
  const dayHasRows =
    totals.not_started + totals.pending_verification + totals.rework + totals.completed > 0;

  const parkOptions = dedupe(options.map((o) => ({ value: o.park_id, label: o.park_label })));
  // Shed options follow the park selection, because a shed list spanning both farms offers sheds the
  // current filter can never show. Keyed by shed_id, never by name: Castro, Gandhi, Godel 1, Mandela
  // 1/2 and Yashoda each exist in BOTH farms, so name-keying would merge two real sheds into one
  // option that then filters to the wrong animals. Keying them apart is only half of it — with both
  // farms in scope the list still PRINTS "Castro" twice and the reader cannot tell which is which,
  // so a label shared by more than one shed carries its farm and a unique one stays clean.
  const shedOptions = disambiguateByPark(
    options.filter((o) => filters.park === "" || o.park_id === filters.park),
  );

  const fields: FeedFilterField[] = [
    {
      kind: "date",
      param: "fdc_day",
      label: fc("completion.date.label"),
      value: data.completion_day,
      today: todayIso(),
      labels: {
        field: fc("completion.date.label"),
        today: fc("filter.date.today"),
        single: fc("filter.date.single"),
        range: fc("filter.date.range"),
        aria: fc("completion.date.aria"),
        previousMonth: fc("filter.date.previous_month"),
        nextMonth: fc("filter.date.next_month"),
        rangeStartHint: fc("filter.date.range_start_hint"),
        rangeEndHint: fc("filter.date.range_end_hint"),
        rangeSeparator: fc("filter.date.range_separator"),
      },
    },
    {
      kind: "select",
      param: "fdc_park",
      label: fc("filter.park_label"),
      value: filters.park,
      allowAll: true,
      // Changing farm invalidates the shed pick: a shed of the other farm matches nothing and the
      // reader cannot tell why the table went empty.
      clears: ["fdc_shed"],
      disabledReason: parkScopeLocked ? fc("filter.scope_readonly") : undefined,
      options: parkOptions,
    },
    {
      kind: "select",
      param: "fdc_shed",
      label: fc("completion.filter.shed"),
      value: filters.shed,
      allowAll: true,
      options: shedOptions,
    },
    {
      kind: "select",
      param: "fdc_status",
      label: fc("completion.filter.status"),
      value: filters.status,
      allowAll: true,
      options: STATUS_ORDER.map((status) => ({ value: status, label: fc(STATUS_COPY_KEY[status]) })),
    },
  ];

  return (
    <section className="card" aria-label={fc("completion.title")}>
      <div className="hd">
        <h3>{fc("completion.title")}</h3>
        <span className="small muted">{fc("completion.hint")}</span>
      </div>

      <FeedFilters basePath={basePath} pageParam="fdc_offset" fields={fields} pageContract={pageContract} />

      {!dayHasRows ? (
        <p className="muted small">{fc("completion.empty")}</p>
      ) : (
        <>
          <div className="grid g4 kpi-row" style={{ marginBottom: 10 }}>
            {STATUS_ORDER.map((status) => {
              const count = totalFor(totals, status);
              return (
                <div className="kpi card" key={status}>
                  <div
                    className="val"
                    style={status === "not_started" && count > 0 ? { color: "var(--danger)" } : undefined}
                  >
                    {count}
                  </div>
                  <div className="dl">{fc(STATUS_KPI_KEY[status])}</div>
                  <div className="muted small">{fc("completion.kpi.sub")}</div>
                </div>
              );
            })}
          </div>

          {rows.length === 0 ? (
            <p className="muted small">{fc("completion.empty_filtered")}</p>
          ) : (
            <div className="tablewrap" tabIndex={0} role="group" aria-label={fc("completion.title")}>
              <table className="tbl">
                <thead>
                  <tr>
                    <th>{fc("col.completion.park")}</th>
                    <th>{fc("col.completion.pen")}</th>
                    <th>{fc("col.completion.session")}</th>
                    <th>{fc("col.completion.status")}</th>
                    <th>{fc("col.completion.videos")}</th>
                    <th>{fc("col.completion.who")}</th>
                    <th>{fc("col.completion.when")}</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row) => {
                    const id = rowId(row);
                    return (
                      <tr key={id}>
                        <td>{row.park_label}</td>
                        <td>
                          {/* Backend-composed location, rendered verbatim: "Godel 1 - Part 3". */}
                          <LocalOverlayLink
                            href={hrefWith(basePath, searchParams, { fdc_row: id })}
                            title={fc("completion.action.details")}
                            scroll={false}
                          >
                            {row.operational_location_display}
                          </LocalOverlayLink>
                        </td>
                        <td>{row.session_label || String(row.session_no)}</td>
                        <td>
                          <Tag tone={isStatus(row.status) ? STATUS_TONE[row.status] : "mut"}>
                            {isStatus(row.status) ? fc(STATUS_COPY_KEY[row.status]) : row.status}
                          </Tag>
                        </td>
                        <td>{fc("completion.videos.count").replace("{done}", String(recordedCount(row)))}</td>
                        <td>{row.submitted_by_name || fc("drawer.completion.none")}</td>
                        <td>{istInstant(row.submitted_at) ?? fc("drawer.completion.none")}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}

          {rows.length > 0 ? (
            <FeedPager
              pageContract={pageContract}
              offset={filters.offset}
              limit={filters.limit}
              rowCount={rows.length}
              hasMore={data.distribution_completions_has_more}
              noun={fc("completion.pager.noun")}
              pageSizeOptions={filters.pageSizes}
              hrefForOffset={(next) => hrefWith(basePath, searchParams, { fdc_offset: String(next) })}
              // A page-size change returns to the FIRST page: keeping a 40-row offset while
              // switching to ten rows lands the reader mid-result with no way to tell where.
              hrefForLimit={(next) =>
                hrefWith(basePath, searchParams, { fdc_limit: String(next), fdc_offset: undefined })
              }
            />
          ) : null}
        </>
      )}

      <LocalOverlayDrawer
        items={rows.map((row) => drawerItem(row, pageContract))}
        selectionKey="fdc_row"
        initialSelectedId={filters.selectedRowId}
        closeHref={hrefWith(basePath, searchParams, { fdc_row: undefined })}
        ariaLabel={fc("drawer.completion.aria")}
        closeLabel={fc("drawer.completion.close_label")}
      />
    </section>
  );
}

/**
 * The row detail: the three videos with the time each was uploaded and by whom.
 *
 * Built from the LIST payload, so opening a row is pure client state — no fetch, no navigation, no
 * page-level loading (the same-page overlay rule). The three slots always render, including the ones
 * nobody shot: a missing video is the point of the screen and must be visible as a labelled gap
 * rather than as an absent line. Each line names ITS OWN uploader, because the three proofs may be
 * shot by three different people on three phones.
 */
function drawerItem(row: CompletionRow, pageContract: AdminUiPageContract): LocalOverlayDrawerItem {
  const fc = (key: string) => copy(pageContract, key);
  const dash = fc("drawer.completion.none");
  const status = isStatus(row.status) ? fc(STATUS_COPY_KEY[row.status]) : row.status;
  return {
    id: rowId(row),
    eyebrow: fc("drawer.completion.eyebrow"),
    title: row.operational_location_display,
    body: (
      <>
        <div className="metagrid">
          <div>
            <div className="k">{fc("drawer.completion.farm")}</div>
            <div className="v">{row.park_label}</div>
          </div>
          <div>
            <div className="k">{fc("drawer.completion.session")}</div>
            <div className="v">{row.session_label || String(row.session_no)}</div>
          </div>
          <div>
            <div className="k">{fc("drawer.completion.status")}</div>
            <div className="v">{status}</div>
          </div>
          <div>
            <div className="k">{fc("drawer.completion.submitted_by")}</div>
            <div className="v">{row.submitted_by_name || dash}</div>
          </div>
          <div>
            <div className="k">{fc("drawer.completion.submitted_at")}</div>
            <div className="v">{istInstant(row.submitted_at) ?? dash}</div>
          </div>
          <div>
            <div className="k">{fc("drawer.completion.verified_by")}</div>
            <div className="v">{row.verified_by_name || dash}</div>
          </div>
          <div>
            <div className="k">{fc("drawer.completion.verified_at")}</div>
            <div className="v">{istInstant(row.verified_at) ?? dash}</div>
          </div>
        </div>

        {row.rework_reason ? (
          <p className="muted small" style={{ marginTop: 10 }}>
            {`${fc("drawer.completion.rework")}: ${row.rework_reason}`}
          </p>
        ) : null}

        <h4 className="h" style={{ marginTop: 14 }}>
          {fc("drawer.completion.videos")}
        </h4>
        <div className="tablewrap">
          <table className="tbl">
            <thead>
              <tr>
                <th>{fc("drawer.completion.col.video")}</th>
                <th>{fc("drawer.completion.col.when")}</th>
                <th>{fc("drawer.completion.col.who")}</th>
              </tr>
            </thead>
            <tbody>
              {row.proofs.map((slot: ProofSlot) => {
                const uploaded = istInstant(slot.uploaded_at);
                return (
                  <tr key={slot.field_key}>
                    <td>{fc(SLOT_COPY_KEY[slot.field_key] ?? "completion.slot.missing")}</td>
                    <td>
                      {slot.proof_ref === "" ? (
                        <Tag tone="dng">{fc("completion.slot.missing")}</Tag>
                      ) : (
                        uploaded ?? dash
                      )}
                    </td>
                    <td>
                      {slot.proof_ref === "" ? dash : slot.uploaded_by_name || fc("completion.slot.no_name")}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </>
    ),
  };
}

/** Preserves every other param, so paging or opening a row never resets the day or the filters. */
function hrefWith(
  basePath: string,
  sp: RouteSearchParams | undefined,
  next: Record<string, string | undefined>,
): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(sp ?? {})) {
    if (key in next) continue;
    if (Array.isArray(value)) value.forEach((item) => params.append(key, item));
    else if (value) params.set(key, value);
  }
  for (const [key, value] of Object.entries(next)) {
    if (value) params.set(key, value);
  }
  const query = params.toString();
  return query ? `${basePath}?${query}` : basePath;
}

/** First label wins per value, sorted for a stable dropdown. */
function dedupe(options: { value: string; label: string }[]): { value: string; label: string }[] {
  const seen = new Map<string, string>();
  for (const option of options) {
    if (option.value !== "" && !seen.has(option.value)) seen.set(option.value, option.label);
  }
  return [...seen.entries()]
    .map(([value, label]) => ({ value, label }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

/** Shed options, with the farm appended ONLY to labels that would otherwise appear twice. */
function disambiguateByPark(
  options: { park_id: string; park_label: string; shed_id: string; shed_label: string }[],
): { value: string; label: string }[] {
  const byId = new Map<string, { label: string; park: string }>();
  for (const option of options) {
    if (option.shed_id !== "" && !byId.has(option.shed_id)) {
      byId.set(option.shed_id, { label: option.shed_label, park: option.park_label });
    }
  }
  const labelCounts = new Map<string, number>();
  for (const { label } of byId.values()) {
    labelCounts.set(label, (labelCounts.get(label) ?? 0) + 1);
  }
  return [...byId.entries()]
    .map(([value, { label, park }]) => ({
      value,
      label: (labelCounts.get(label) ?? 0) > 1 ? `${label} · ${park}` : label,
    }))
    .sort((a, b) => a.label.localeCompare(b.label));
}
