import { Fragment } from "react";
import { redirect } from "next/navigation";
import { AlertTriangle } from "lucide-react";

import { copy, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedConfigExperiment } from "@/lib/api/server";
import {
  firstAuthRequiredError,
  listFeedConfigExperiment,
  listFeedConfigFeedItems,
  listFeedConfigRationGroups,
  listFeedConfigRationRates,
  listFeedConfigSchedule,
  listFeedConfigSessionTemplates,
  listFeedConfigShedFactors,
  listFeedConfigShedTags,
  type ApiResult,
} from "@/lib/api/server";
import { getCensusLocations } from "@/lib/api/herd-locations";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import type { RouteSearchParams } from "@/lib/search-params";
import { FeedFilters, type FeedFilterField } from "./feed-filters";
import { FeedPager } from "./feed-pager";
import { FeedFaroView } from "./feed-faro-view";
import { isConfiguredZero } from "./feed-quantity";
import { feedHref, feedLimit, feedOffset, resolveFeedScope } from "./feed-scope";
import {
  saveExperimentCell,
  saveFeedItem,
  saveRationRate,
  saveSchedule,
  saveShedFactor,
  setExperimentShedStatus,
} from "./feed-config-actions";
import {
  ExperimentCellEditor,
  ExperimentCellAdder,
  ExperimentShedEnroller,
  ExperimentShedSwitch,
  FeedItemCreator,
  RationRateEditor,
  ScheduleEditor,
  ShedFactorEditor,
} from "./feed-config-editor";

// Feed -> Feed Config. The authored input the daily generation reads: the ration grid, the per-shed
// factors, the park's session split, and the dispatch clock.
//
// EDITS ARE NON-DESTRUCTIVE AND EFFECTIVE-DATED. Saving a rate does not overwrite the old one — the
// backend CLOSES the row that was in force and opens a new one, so every past feed sheet stays
// explainable. The UI therefore talks about rows being "in force" or "superseded" rather than
// implying a destructive overwrite, and shows each row's effective window.
//
// A CLEARED FIELD IS NOT ZERO. The full reasoning lives in feed-config-actions.ts (server side) and
// feed-config-editor.tsx (input side); the short version is that an absent rate means the
// combination is UNCONFIGURED and every shed resolving to it is BLOCKED and will not be fed, while
// an authored 0 means "feed nothing of this item" and is correct for milk-fed kids. The grid marks
// an authored zero explicitly so it can never be mistaken for a missing one.
//
// NO KPI CARDS. Every list endpoint here returns `has_more` and no total, because counting the
// filtered set on each request is compute-on-read. A headline figure would have to be computed from
// the visible page, and a page subtotal shown as an authored-rate total is a false statement.

const PAGE_PATH = "/feed/config";
const DEFAULT_PAGE_SIZE = 10;
const SECONDARY_PAGE_SIZE = 25;
// The experiment section groups its rows by shed, so its page must hold whole sheds rather than a
// screenful of cells: at 5 items per shed, 100 rows is 20 sheds. It is still a bounded page (the
// backend caps at 200 and reports has_more), not a drain — the live parks author 17 sheds each.
const EXPERIMENT_PAGE_SIZE = 100;
// Both parks at once: 35 pens x 5 items = 175 rows today, so a 100-row page would cut the second
// park in half. 200 is the contract's largest declared option and the backend's own cap.
const EXPERIMENT_ALL_PARKS_PAGE_SIZE = 200;
// The authored feed vocabulary (ration groups, shed tags, feed items) backs the FILTER dropdowns,
// so the whole catalog must arrive in one bounded page — a screenful-sized limit would silently
// truncate it (there are already 31 shed tags, past SECONDARY_PAGE_SIZE) and reintroduce the very
// "filter only shows what's on the current grid page" bug this replaces. Bounded config, not a herd
// scan: the backend caps these catalogs and reports has_more.
const VOCAB_PAGE_SIZE = 200;

/** In-force (`valid_to` absent) vs superseded by a later edit. */
function EffectiveWindow({
  validFrom,
  validTo,
  pageContract,
}: {
  validFrom: string;
  validTo?: string | null;
  pageContract: AdminUiPageContract;
}) {
  const open = !validTo;
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 3 }}>
      <span
        className={open ? "tag t-ok" : "tag t-mut"}
        title={copy(pageContract, open ? "label.effective_open_note" : "label.effective_closed_note")}
      >
        {copy(pageContract, open ? "label.effective_open" : "label.effective_closed")}
      </span>
      <span className="muted" style={{ fontSize: 11 }}>
        {validFrom}
        {validTo ? ` · ${validTo}` : ""}
      </span>
    </div>
  );
}

/**
 * One experiment PEN: its arm, its informational head count, its authored cells, and whether it is
 * currently on the experiment workflow.
 *
 * A pen, not a shed. A partitioned shed authors one cell per pen and each pen carries its own arm
 * and head count -- Mandela 1 holds ten. Grouping by shed collapsed all ten into one row whose arm
 * and count came from whichever pen happened to be first, and rendered the pens' cells as ten
 * indistinguishable "Dry Masoor Bhusa" lines differing only by a number.
 */
type ExperimentShedGroup = {
  shedId: string;
  parkId: string;
  /** Backend-supplied park name. Rendered as its own column because the list may span both parks. */
  parkName: string;
  /** The pen's HUMAN label; empty for an undivided shed. Echoed back on every write. */
  partitionLabel: string;
  /** Server-composed "Mandela 1 - Part 3". Shown verbatim -- never rejoined here. */
  locationDisplay: string;
  /** The experiment ARM. Taken from the pen's rows, which the writer keeps consistent. */
  category: string;
  /** INFORMATIONAL population. Null means not recorded — never rendered or sent as 0. */
  headCount: number | null;
  /**
   * True when ANY of the shed's rows is active, which is exactly ExperimentPlanner.Applies' rule.
   * The whole-shed status write keeps the rows in step, so a mixed shed is not a state this UI can
   * create; deriving it this way rather than reading row[0] means a legacy mixed shed still reports
   * the workflow that would actually feed it.
   */
  active: boolean;
  rows: FeedConfigExperiment[];
};

/**
 * Regroups one bounded page of flat experiment cells into per-PEN groups, preserving the backend's
 * (shed, partition, feed item) order.
 *
 * Keyed on shed_id + partition, never on shed_id alone and never on a NAME. Shed names repeat
 * across parks (two Castro, two Gandhi, two Yashoda), and one shed holds many pens -- keying on
 * either one merges rows that describe different ground locations.
 *
 * This is NOT a read-time rollup presented as business truth: it re-shapes rows already fetched for
 * display and computes no total. Every number rendered is the backend's own authored value.
 */
function groupExperimentRowsByShed(rows: FeedConfigExperiment[]): ExperimentShedGroup[] {
  const byPen = new Map<string, ExperimentShedGroup>();
  for (const row of rows) {
    const partitionLabel = row.partition_label ?? "";
    const key = `${row.shed_id}#${partitionLabel.trim().toLowerCase()}`;
    const existing = byPen.get(key);
    if (!existing) {
      byPen.set(key, {
        shedId: row.shed_id,
        parkId: row.park_id,
        parkName: row.park_name,
        partitionLabel,
        locationDisplay: row.operational_location_display,
        category: row.experiment_category,
        headCount: row.head_count ?? null,
        active: row.status === "active",
        rows: [row],
      });
      continue;
    }
    existing.rows.push(row);
    existing.active = existing.active || row.status === "active";
    // `??` and not `||`: a recorded 0 is a real head count and must not be replaced by a later row's
    // value just because it is falsy.
    existing.headCount = existing.headCount ?? row.head_count ?? null;
  }
  return Array.from(byPen.values());
}

/**
 * The feed-config join key, mirroring Postgres `feed_config_norm`: trim, casefold, collapse runs of
 * whitespace/underscore/hyphen to one underscore.
 *
 * Used ONLY to decide which catalog items a pen may still be offered. The database's generated
 * `feed_item_key` remains the authority on identity — if this ever drifts, the worst outcome is that
 * the Add control offers an item the pen already has, and the upsert then corrects that cell instead
 * of inserting a duplicate (the natural key forbids one). It is not used to write, compare
 * quantities, or decide what a shed is fed.
 */
function normalizeFeedItemKey(label: string): string {
  return label.trim().toLowerCase().replace(/[\s_-]+/g, "_");
}

/** The feed items this pen already has an authored cell for, as normalized keys. */
function authoredItemKeys(pen: ExperimentShedGroup): Set<string> {
  return new Set(pen.rows.map((row) => normalizeFeedItemKey(row.feed_item)));
}

function SectionError({
  result,
  titleKey,
  pageContract,
}: {
  result: ApiResult<unknown> | null;
  titleKey: string;
  pageContract: AdminUiPageContract;
}) {
  if (!result || result.ok) return null;
  return (
    <div className="alert" style={{ marginBottom: 16 }}>
      <AlertTriangle className="ic" aria-hidden="true" />
      <div>
        <b>{copy(pageContract, titleKey)}</b>
        <div className="small muted">
          {result.error.code ?? result.error.kind}&nbsp;{result.error.message}
        </div>
      </div>
    </div>
  );
}

export async function FeedConfigPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};

  const locations = await getCensusLocations();
  const scope = resolveFeedScope(sp, "fc_park", "fc_date", locations.parks);
  const rationGroupFilter = (sp.fc_group as string | undefined) || "";
  const shedTagFilter = (sp.fc_tag as string | undefined) || "";
  const feedItemFilter = (sp.fc_item as string | undefined) || "";

  const gridPageSizes = tablePageSizes(pageContract, "ration-grid");
  const gridLimit = feedLimit(sp, "fc_limit", gridPageSizes, DEFAULT_PAGE_SIZE);
  const gridOffset = feedOffset(sp, "fc_offset");
  // The experiment section paginates on its OWN params. It shares the page with the ration grid but
  // not the grid's cursor: one park holds ~17 pens x 5 items here against thousands of grid rows, so
  // a shared offset would scroll one section by the other's page. EXPERIMENT_PAGE_SIZE is the
  // default rather than the only choice — a pen can now hold as many cells as the catalog has items,
  // so the row count grows with the vocabulary and the reader needs a bigger page available.
  const experimentPageSizes = tablePageSizes(pageContract, "experiment-config");
  // ALL PARKS is a real mode for this ONE section. Nobody picked a park (the top bar is company-wide
  // and the page carries no park param), and unlike the ration grid an experiment cell knows its own
  // park -- so the honest answer to "show me all parks" is every authored pen in the tenant, not one
  // park's silently. The other three sections stay park-scoped because they are park-OWNED.
  const experimentAllParks = scope.parkSource === "fallback";
  // A cross-park page holds both parks' pens (175 rows today against one park's 90), so the default
  // page size steps up to the contract's largest option in that mode. Still bounded, still paged.
  const experimentDefaultSize = experimentAllParks ? EXPERIMENT_ALL_PARKS_PAGE_SIZE : EXPERIMENT_PAGE_SIZE;
  const experimentLimit = feedLimit(sp, "fc_exp_limit", experimentPageSizes, experimentDefaultSize);
  const experimentOffset = feedOffset(sp, "fc_exp_offset");

  // Six independent authored surfaces, fetched concurrently — no serial await, and no draining of
  // any of them: each is one bounded page.
  const [
    ratesResult,
    factorsResult,
    sessionsResult,
    scheduleResult,
    experimentResult,
    feedItemsResult,
    rationGroupsResult,
    shedTagsResult,
  ] = await Promise.all([
    scope.parkId
      ? listFeedConfigRationRates({
          park_id: scope.parkId,
          ration_group: rationGroupFilter || undefined,
          shed_tag: shedTagFilter || undefined,
          feed_item: feedItemFilter || undefined,
          limit: gridLimit,
          offset: gridOffset,
        })
      : Promise.resolve(null),
    scope.parkId ? listFeedConfigShedFactors({ park_id: scope.parkId, limit: SECONDARY_PAGE_SIZE }) : Promise.resolve(null),
    scope.parkId ? listFeedConfigSessionTemplates({ park_id: scope.parkId, limit: SECONDARY_PAGE_SIZE }) : Promise.resolve(null),
    scope.parkId ? listFeedConfigSchedule({ park_id: scope.parkId, limit: SECONDARY_PAGE_SIZE }) : Promise.resolve(null),
    // No `status` filter: retired rows must stay visible so a withdrawn shed's authored quantities
    // can be seen and restored, and so an accidental withdrawal is not invisible on the screen that
    // owns the decision. One bounded page — the live parks author 17 sheds x 5 items each.
    scope.parkId
      ? listFeedConfigExperiment(
          experimentAllParks
            // park_id omitted entirely -- the backend reads that as "every park".
            ? { limit: experimentLimit, offset: experimentOffset }
            : { park_id: scope.parkId, limit: experimentLimit, offset: experimentOffset },
        )
      : Promise.resolve(null),
    // The tenant's feed vocabulary, for the enrol control's item picker. It comes from the catalog
    // endpoint rather than a local list: feed items are live module-owned data, and hardcoding them
    // here would break the moment a workbook adds one (as RGS/Vijay Concentrate just did).
    listFeedConfigFeedItems({ limit: VOCAB_PAGE_SIZE }),
    // The ration-group and shed-tag vocabularies for the FILTER dropdowns. Like feed-items above,
    // these are the tenant's whole authored catalog (tenant-wide, not park-scoped), so a filter can
    // offer an option the current — paginated, already-filtered — grid page doesn't happen to show.
    listFeedConfigRationGroups({ limit: VOCAB_PAGE_SIZE }),
    listFeedConfigShedTags({ limit: VOCAB_PAGE_SIZE }),
  ]);

  const authError = firstAuthRequiredError(
    ratesResult,
    factorsResult,
    sessionsResult,
    scheduleResult,
    experimentResult,
    feedItemsResult,
    rationGroupsResult,
    shedTagsResult,
  );
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const rates = ratesResult && ratesResult.ok ? ratesResult.data : null;
  const factors = factorsResult && factorsResult.ok ? factorsResult.data : null;
  const sessions = sessionsResult && sessionsResult.ok ? sessionsResult.data : null;
  const schedule = scheduleResult && scheduleResult.ok ? scheduleResult.data : null;
  const experiment = experimentResult && experimentResult.ok ? experimentResult.data : null;
  const feedItems = feedItemsResult && feedItemsResult.ok ? feedItemsResult.data : null;
  const rationGroups = rationGroupsResult && rationGroupsResult.ok ? rationGroupsResult.data : null;
  const shedTags = shedTagsResult && shedTagsResult.ok ? shedTagsResult.data : null;

  // Experiment rows arrive flat (one per shed x feed item) and are grouped by shed for display,
  // because the SHED is the unit of every decision in this section: the arm, the head count and the
  // workflow switch all describe a shed, not a cell. Grouping is a pure regroup of ONE bounded page
  // — no second fetch, no accumulation across pages, and the shed's own rows are the only input.
  const experimentRows = experiment?.items ?? [];
  const experimentSheds = groupExperimentRowsByShed(experimentRows);
  // Sheds in this park that have no experiment rows at all — the candidates the enrol control offers.
  // A shed with only RETIRED rows is deliberately NOT a candidate: it already has authored
  // quantities, so it is restored through its own row group rather than re-enrolled from scratch.
  const experimentShedIds = new Set(experimentRows.map((row) => row.shed_id));
  const candidateSheds = locations.sheds
    .filter((shed) => shed.parentId === scope.parkId && !experimentShedIds.has(shed.id))
    .map((shed) => ({ id: shed.id, name: shed.name }));
  const catalogItems = (feedItems?.items ?? []).map((item) => item.feed_item);

  const gridCols = tableLabels(pageContract, "ration-grid");
  const factorCols = tableLabels(pageContract, "shed-factors");
  const sessionCols = tableLabels(pageContract, "session-template");
  const scheduleCols = tableLabels(pageContract, "schedule-config");
  const experimentCols = tableLabels(pageContract, "experiment-config");
  const feedItemCols = tableLabels(pageContract, "feed-items");

  const shedNameById = new Map(locations.sheds.map((shed) => [shed.id, shed.name]));
  // The park being read, by name. Live data from the locations master — never composed from a code
  // or an id, and blank only when the master returned no parks at all.
  const parkName = locations.parks.find((park) => park.id === scope.parkId)?.name ?? "";
  const hasGridFilter = Boolean(rationGroupFilter || shedTagFilter || feedItemFilter);

  const gridRows = rates?.items ?? [];

  // Filter options are the tenant's authored catalog — the FULL vocabulary — not the current grid
  // page. Deriving them from gridRows showed only the values on the visible (paginated,
  // already-filtered) rows, so a park's other ration groups / shed tags / feed items were
  // unreachable in the filter. Shed tags and feed items carry a backend display_order, which is
  // authoritative and preserved rather than re-sorted alphabetically. A ration group's label is not
  // unique (Beetal and Sirohi both map to "Beetal/Sirohi"), so it is de-duplicated by label. If a
  // catalog read failed, fall back to the grid-derived set so the filter never goes empty.
  const uniqueSorted = (values: string[]) => Array.from(new Set(values)).sort();
  const dedupe = (values: string[]) => Array.from(new Set(values));
  const toOptions = (values: string[]) => values.map((value) => ({ value, label: value }));
  const byDisplayOrder = <T extends { display_order: number }>(rows: readonly T[]) =>
    [...rows].sort((a, b) => a.display_order - b.display_order);

  const rationGroupOptions = rationGroups
    ? toOptions(uniqueSorted(rationGroups.items.map((group) => group.ration_group)))
    : toOptions(uniqueSorted(gridRows.map((row) => row.ration_group)));
  const shedTagOptions = shedTags
    ? toOptions(dedupe(byDisplayOrder(shedTags.items).map((tag) => tag.shed_tag)))
    : toOptions(uniqueSorted(gridRows.map((row) => row.shed_tag)));
  const feedItemOptions = feedItems
    ? toOptions(dedupe(byDisplayOrder(feedItems.items).map((item) => item.feed_item)))
    : toOptions(uniqueSorted(gridRows.map((row) => row.feed_item)));

  const filterFields: FeedFilterField[] = [
    {
      kind: "select",
      param: "fc_park",
      label: copy(pageContract, "filter.park_label"),
      value: scope.parkId,
      allowAll: false,
      disabledReason: scope.parkLockedByTopBar ? copy(pageContract, "filter.scope_readonly") : undefined,
      options: locations.parks.map((park) => ({ value: park.id, label: park.name })),
    },
    {
      kind: "select",
      param: "fc_group",
      label: copy(pageContract, "filter.ration_group_label"),
      value: rationGroupFilter,
      options: rationGroupOptions,
    },
    {
      kind: "select",
      param: "fc_tag",
      label: copy(pageContract, "filter.shed_tag_label"),
      value: shedTagFilter,
      options: shedTagOptions,
    },
    {
      kind: "select",
      param: "fc_item",
      label: copy(pageContract, "filter.feed_item_label"),
      value: feedItemFilter,
      options: feedItemOptions,
    },
  ];

  // The park's session splits must add up to a whole day; a park where they do not would under- or
  // over-feed every shed in it, so that is surfaced rather than left for someone to spot by eye.
  const activeSessions = (sessions?.items ?? []).filter((session) => session.status === "active");
  const splitTotalMilli = activeSessions.reduce(
    (total, session) => total + Math.round(Number(session.split_fraction) * 10000),
    0,
  );
  const splitMismatch = activeSessions.length > 0 && splitTotalMilli !== 10000;

  return (
    <div className="screen on">
      <FeedFaroView routeId={pageContract.route_id} parkId={scope.parkId} />

      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pageContract, "crumb")} / <b>{copy(pageContract, "section.ration_grid.title")}</b>
          </div>
          <h1>{pageContract.title}</h1>
        </div>
        <div className="sp" style={{ flex: 1 }} />
      </div>

      {/* Nobody chose this park — the top bar is company-wide and the page has no park param, so the
          first park in the locations master was read. Said out loud because the alternative is a
          screen that shows one park's sheds while the top bar reads company-wide, which is how a
          whole park goes missing with no on-screen sign. Not an error: this page is single-park by
          construction, so it is a notice rather than the .alert used for the split mismatch. */}
      {scope.parkSource === "fallback" && parkName ? (
        <div className="note" style={{ marginBottom: 16 }}>
          {copy(pageContract, "notice.park_scope_fallback")} <b>{parkName}</b>
        </div>
      ) : null}

      <SectionError result={ratesResult} titleKey="state.ration_grid_unavailable" pageContract={pageContract} />

      {/* ---------------------------------------------------------------- ration grid (editable) */}
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.ration_grid.title")}</h3>
          <span className="small muted">{copy(pageContract, "section.ration_grid.caption")}</span>
        </div>
        <FeedFilters
          basePath={PAGE_PATH}
          pageParam="fc_offset"
          fields={filterFields}
          pageContract={pageContract}
        />

        <div
          className="bd feed-scroll"
          style={{ padding: 0, overflowX: "auto" }}
          tabIndex={0}
          role="group"
          aria-label={copy(pageContract, "section.ration_grid.aria")}
        >
          <table className="feed-table" aria-label={copy(pageContract, "table.ration_grid.aria")}>
            <thead>
              <tr>
                {gridCols.map((col) => (
                  <th key={col}>{col}</th>
                ))}
                <th>{copy(pageContract, "action.edit_rate")}</th>
              </tr>
            </thead>
            <tbody>
              {gridRows.length === 0 ? (
                <tr>
                  <td colSpan={gridCols.length + 1}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {!ratesResult || ratesResult.ok
                        ? hasGridFilter
                          ? copy(pageContract, "empty.ration_grid_filtered")
                          : copy(pageContract, "empty.ration_grid")
                        : copy(pageContract, "state.ration_grid_unavailable")}
                    </div>
                  </td>
                </tr>
              ) : (
                gridRows.map((row) => {
                  // An authored 0 is real configuration. It is tagged so it can never be read as the
                  // missing-rate state, which has the opposite consequence (the shed goes unfed).
                  const authoredZero = isConfiguredZero(row.grams_per_head);
                  return (
                    <tr key={row.ration_rate_id}>
                      {/* No park cell. Every /feed-config/* read requires park_id and filters on
                          it, and all four sections on this page share ONE Park filter — so a park
                          column would repeat the same value on every row. */}
                      <td>{row.ration_group}</td>
                      <td className="muted">{row.shed_tag}</td>
                      <td>{row.feed_item}</td>
                      <td>
                        <span style={{ display: "inline-flex", alignItems: "center", gap: 6, whiteSpace: "nowrap" }}>
                          <span
                            style={{
                              fontVariantNumeric: "tabular-nums",
                              fontWeight: authoredZero ? 500 : 700,
                              color: authoredZero ? "var(--muted)" : "var(--brand-d)",
                            }}
                          >
                            {row.grams_per_head}
                          </span>
                          {authoredZero ? (
                            <span className="tag t-info" title={copy(pageContract, "label.configured_zero_note")}>
                              {copy(pageContract, "label.configured_zero")}
                            </span>
                          ) : null}
                        </span>
                      </td>
                      <td colSpan={2}>
                        <EffectiveWindow
                          validFrom={row.valid_from}
                          validTo={row.valid_to}
                          pageContract={pageContract}
                        />
                      </td>
                      <td>
                        <RationRateEditor
                          pageContract={pageContract}
                          action={saveRationRate}
                          parkId={row.park_id}
                          rationGroup={row.ration_group}
                          shedTag={row.shed_tag}
                          feedItem={row.feed_item}
                          gramsPerHead={row.grams_per_head}
                        />
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>

        <FeedPager
          pageContract={pageContract}
          offset={gridOffset}
          limit={gridLimit}
          rowCount={gridRows.length}
          hasMore={rates?.has_more ?? false}
          noun={copy(pageContract, "table.ration_grid.noun")}
          pageSizeOptions={gridPageSizes}
          hrefForOffset={(next) => feedHref(PAGE_PATH, sp, "fc_offset", String(next))}
          hrefForLimit={(next) => feedHref(PAGE_PATH, sp, "fc_limit", String(next))}
        />
      </section>

      <div className="note" style={{ marginBottom: 16 }}>{copy(pageContract, "section.ration_grid.note")}</div>
      <div className="note" style={{ marginBottom: 16 }}>{copy(pageContract, "label.park_scoped_note")}</div>
      <div className="note" style={{ marginBottom: 16 }}>{copy(pageContract, "label.kid_group_note")}</div>
      {/* Spelled out rather than implied: this is the difference between an unconfigured cell and an
          authored zero, and it is the one thing an author on this screen must not get wrong. */}
      <div className="note" style={{ marginBottom: 16 }}>{copy(pageContract, "label.blocked_note")}</div>

      {/* ------------------------------------------------------------------- feed items (catalog) */}
      {/* The vocabulary the grid above is indexed by, directly under it. Two things separate this
          section from every other one on the page, and both are stated in its copy rather than left
          to be inferred:

          It is TENANT-wide, not park-scoped — the Park filter does not narrow it, because
          feed_item_catalog is keyed on (tenant, item) and both parks author against one list.

          And adding an item authors NO quantity. The new name becomes selectable on the grid above,
          the shed factors and the experiment sheds; every combination using it stays unconfigured —
          and therefore blocked — until a rate is authored. That is why this section sits next to
          the ration grid rather than replacing any part of it. */}
      <SectionError result={feedItemsResult} titleKey="state.feed_items_unavailable" pageContract={pageContract} />
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.feed_items.title")}</h3>
          <span className="small muted">{copy(pageContract, "section.feed_items.caption")}</span>
          <div className="sp" style={{ flex: 1 }} />
          {/* In the section header, next to the list it changes — the same placement as the
              experiment enroller, for the same reason. */}
          <FeedItemCreator pageContract={pageContract} action={saveFeedItem} />
        </div>
        <div
          className="bd feed-scroll"
          style={{ padding: 0, overflowX: "auto" }}
          tabIndex={0}
          role="group"
          aria-label={copy(pageContract, "section.feed_items.aria")}
        >
          <table className="feed-table" aria-label={copy(pageContract, "table.feed_items.aria")}>
            <thead>
              <tr>
                {feedItemCols.map((col) => (
                  <th key={col}>{col}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {(feedItems?.items ?? []).length === 0 ? (
                <tr>
                  <td colSpan={feedItemCols.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {!feedItemsResult || feedItemsResult.ok
                        ? copy(pageContract, "empty.feed_items")
                        : copy(pageContract, "state.feed_items_unavailable")}
                    </div>
                  </td>
                </tr>
              ) : (
                (feedItems?.items ?? []).map((row) => (
                  <tr key={row.feed_item_id}>
                    <td>{row.feed_item}</td>
                    {/* An unset attribute renders as the contract's placeholder, never as 0 and
                        never as a blank cell. Both of those would read as a measured value: a 0
                        claims someone measured none, and an empty cell reads as one too on a table
                        whose other columns are numbers. */}
                    <td className="muted" style={{ fontVariantNumeric: "tabular-nums" }}>
                      {row.energy_kcal_per_kg ?? copy(pageContract, "label.placeholder")}
                    </td>
                    <td className="muted" style={{ fontVariantNumeric: "tabular-nums" }}>
                      {row.dry_matter_factor ?? copy(pageContract, "label.placeholder")}
                    </td>
                    <td className="muted" style={{ fontVariantNumeric: "tabular-nums" }}>
                      {row.wastage_factor ?? copy(pageContract, "label.placeholder")}
                    </td>
                    <td className="muted" style={{ fontVariantNumeric: "tabular-nums" }}>
                      {row.display_order}
                    </td>
                    <td>
                      <span className={row.status === "active" ? "tag t-ok" : "tag t-mut"}>{row.status}</span>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>
      <div className="note" style={{ marginBottom: 16 }}>{copy(pageContract, "section.feed_items.note")}</div>
      <div className="note" style={{ marginBottom: 16 }}>{copy(pageContract, "label.feed_item_attributes_note")}</div>

      {/* ---------------------------------------------------------------- shed factors (editable) */}
      <SectionError result={factorsResult} titleKey="state.shed_factors_unavailable" pageContract={pageContract} />
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.shed_factors.title")}</h3>
          <span className="small muted">{copy(pageContract, "section.shed_factors.caption")}</span>
        </div>
        <div
          className="bd feed-scroll"
          style={{ padding: 0, overflowX: "auto" }}
          tabIndex={0}
          role="group"
          aria-label={copy(pageContract, "section.shed_factors.aria")}
        >
          <table className="feed-table" aria-label={copy(pageContract, "table.shed_factors.aria")}>
            <thead>
              <tr>
                {factorCols.map((col) => (
                  <th key={col}>{col}</th>
                ))}
                <th>{copy(pageContract, "action.edit_shed_factor")}</th>
              </tr>
            </thead>
            <tbody>
              {(factors?.items ?? []).length === 0 ? (
                <tr>
                  <td colSpan={factorCols.length + 1}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {/* Only AUTHORED factors are returned. An empty table means every shed is
                          treated as 1.0 — a real, safe default, not a missing read. */}
                      {!factorsResult || factorsResult.ok
                        ? copy(pageContract, "empty.shed_factors")
                        : copy(pageContract, "state.shed_factors_unavailable")}
                    </div>
                  </td>
                </tr>
              ) : (
                (factors?.items ?? []).map((row) => (
                  <tr key={row.shed_factor_id}>
                    <td>{shedNameById.get(row.shed_id) ?? row.shed_id}</td>
                    <td>{row.feed_item}</td>
                    <td style={{ fontVariantNumeric: "tabular-nums", fontWeight: 700 }}>{row.multiplier}</td>
                    <td colSpan={2}>
                      <EffectiveWindow validFrom={row.valid_from} validTo={row.valid_to} pageContract={pageContract} />
                    </td>
                    <td>
                      <ShedFactorEditor
                        pageContract={pageContract}
                        action={saveShedFactor}
                        parkId={row.park_id}
                        shedId={row.shed_id}
                        feedItem={row.feed_item}
                        multiplier={row.multiplier}
                      />
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>
      <div className="note" style={{ marginBottom: 16 }}>{copy(pageContract, "section.shed_factors.note")}</div>

      {/* ------------------------------------------------------------ experiment sheds (editable) */}
      {/* Its OWN section, deliberately separated from the ration grid above rather than mixed into
          it. The two hold numbers with incompatible units: a grid cell is a per-head RATE that gets
          multiplied by a projected head count, and an experiment cell is an ABSOLUTE shed total that
          must never be multiplied by anything. Putting them in one table would place both under a
          single "quantity" heading on a screen whose output is a feeding instruction.

          Rows are grouped by SHED because the shed is the unit of every decision here — the arm, the
          head count, and the workflow switch all describe a shed rather than a cell. */}
      <SectionError result={experimentResult} titleKey="state.experiment_unavailable" pageContract={pageContract} />
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.experiment.title")}</h3>
          {/* The park, on the section itself. Every row in this table belongs to ONE park and the
              table deliberately carries no park column (see the ration grid above), so without this
              chip the screen never states which park's sheds these are — and when the top bar reads
              company-wide it silently shows one. The NAME is live data from the locations master;
              the LABEL it is titled with stays backend-owned. */}
          {parkName ? (
            <span className="tag t-mut" title={copy(pageContract, "filter.park_label")}>
              {parkName}
            </span>
          ) : null}
          <span className="small muted">{copy(pageContract, "section.experiment.caption")}</span>
          <div className="sp" style={{ flex: 1 }} />
          {/* Enrolment lives in the section header, next to the list it changes. It authors the
              shed's first quantity, which IS what moves it onto the experiment workflow. */}
          {scope.parkId ? (
            <ExperimentShedEnroller
              pageContract={pageContract}
              action={saveExperimentCell}
              parkId={scope.parkId}
              sheds={candidateSheds}
              feedItems={catalogItems}
            />
          ) : null}
        </div>

        <div
          className="bd feed-scroll"
          style={{ padding: 0, overflowX: "auto" }}
          tabIndex={0}
          role="group"
          aria-label={copy(pageContract, "section.experiment.aria")}
        >
          <table className="feed-table" aria-label={copy(pageContract, "table.experiment.aria")}>
            <thead>
              <tr>
                {/* Headers wrap in THIS table only. "Head count (informational)" is a deliberately
                    long header — the parenthetical is what stops anyone multiplying by it — and held
                    on one line it reserved a column far wider than the two-digit counts under it.
                    Wrapping the header costs a line of height and keeps the whole table inside its
                    container, matching the four tables above. The DATA cells stay nowrap. */}
                {experimentCols.map((col) => (
                  <th key={col} style={{ whiteSpace: "normal" }}>
                    {col}
                  </th>
                ))}
                <th style={{ whiteSpace: "normal" }}>{copy(pageContract, "action.edit_experiment_cell")}</th>
              </tr>
            </thead>
            <tbody>
              {experimentSheds.length === 0 ? (
                <tr>
                  <td colSpan={experimentCols.length + 1}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {/* An empty table is a real, meaningful state: this park runs no experiments
                          and every shed in it is fed from the ration grid. It is not a failed read. */}
                      {!experimentResult || experimentResult.ok
                        ? copy(pageContract, "empty.experiment")
                        : copy(pageContract, "state.experiment_unavailable")}
                    </div>
                  </td>
                </tr>
              ) : (
                experimentSheds.map((shed) => {
                  // The backend composes this ("Mandela 1 - Part 3"); it is rendered verbatim rather
                  // than rejoined here, so this screen reads identically to the direction sheet and
                  // the mobile app. shedNameById is only the degraded fallback for a row whose shed
                  // could not be resolved -- it holds no partition and cannot tell pens apart.
                  const shedName =
                    shed.locationDisplay || shedNameById.get(shed.shedId) || shed.shedId;
                  return (
                    // Fragment.key, not a key on the first <tr>: a pen contributes SEVERAL sibling
                    // rows, so the fragment is the list item React reconciles and the key belongs on
                    // it. Keying only the inner rows leaves the fragment itself unkeyed.
                    //
                    // The key carries the PARTITION as well as the shed: a partitioned shed yields
                    // one group per pen, so shed_id alone gave ten siblings the SAME key -- React
                    // then reconciles them onto each other and an edit to one pen can paint another
                    // pen's row.
                    <Fragment key={`${shed.shedId}#${shed.partitionLabel}`}>
                      {/* One header row per PEN carrying its arm, head count and the workflow
                          switch, then one row per authored feed item beneath it. */}
                      <tr>
                        {/* Park, as its own column. The section can now span BOTH parks, and the shed
                            name cannot disambiguate them -- Castro, Gandhi and Yashoda each exist in
                            both. Muted because in a single-park view it repeats the header chip; it
                            is the cross-park view that needs it. */}
                        <td className="muted">{shed.parkName}</td>
                        <td>
                          <b>{shedName}</b>
                        </td>
                        {/* The ONLY wrapping cell in this table. Arms are descriptive prose from the
                            workbook and run long ("Mixed (9 Goat F, 1 Sheep F, 5 Goat M) NEW -
                            warmup 20:80"); left nowrap they pushed the table 218px past its
                            container, putting the shed's workflow switch behind a horizontal scroll
                            while the page's other four tables all fit. This is not failure mode 4b:
                            that bans wrapping SHORT business values (RFIDs, dates, "female") which
                            shred into vertical character columns. A sentence-shaped label wrapping
                            to two lines is the correct rendering for it. */}
                        <td
                          title={copy(pageContract, "label.experiment_category_note")}
                          style={{ maxWidth: 200, whiteSpace: "normal", overflowWrap: "break-word" }}
                        >
                          {shed.category}
                        </td>
                        <td
                          className="muted"
                          style={{ fontVariantNumeric: "tabular-nums" }}
                          title={copy(pageContract, "label.experiment_head_count_note")}
                        >
                          {/* `?? placeholder` and never `?? 0`: an unrecorded population must not be
                              printed as an empty shed. */}
                          {shed.headCount ?? copy(pageContract, "label.placeholder")}
                        </td>
                        {/* Spans feed_item + absolute_kg + status. Those three are per-CELL values
                            and are blank on a shed header row, so the span reads as a shed-level
                            banner rather than as a value of any one column — and it reaches the
                            status column, which is the one it is the heading for. Spanning also
                            keeps the label from being clipped: the workflow chip is far wider than
                            the per-row status chips the status column is sized for, and squeezing
                            it into that column alone truncated it mid-word. */}
                        <td colSpan={3}>
                          <span
                            className={shed.active ? "tag t-pur" : "tag t-mut"}
                            title={copy(
                              pageContract,
                              shed.active ? "label.experiment_active_note" : "label.experiment_retired_note",
                            )}
                          >
                            {copy(pageContract, shed.active ? "label.experiment_active" : "label.experiment_retired")}
                          </span>
                        </td>
                        <td>
                          <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
                            {/* The switch offers the OPPOSITE of the current state, so the button
                                always names the change it makes rather than the state it is in. */}
                            <ExperimentShedSwitch
                              pageContract={pageContract}
                              action={setExperimentShedStatus}
                              parkId={shed.parkId}
                              shedId={shed.shedId}
                              shedName={shedName}
                              targetStatus={shed.active ? "retired" : "active"}
                            />
                            {/* Adding a feed item is a PEN-level act, so it sits on the pen's own
                                header row beside the switch — not on a cell row, which is scoped to
                                one item that already exists. */}
                            <ExperimentCellAdder
                              pageContract={pageContract}
                              action={saveExperimentCell}
                              parkId={shed.parkId}
                              shedId={shed.shedId}
                              partitionLabel={shed.partitionLabel}
                              experimentCategory={shed.category}
                              headCount={shed.headCount}
                              availableItems={catalogItems.filter(
                                (item) => !authoredItemKeys(shed).has(normalizeFeedItemKey(item)),
                              )}
                            />
                          </div>
                        </td>
                      </tr>
                      {shed.rows.map((row) => {
                        // An authored 0 kg is real configuration here exactly as it is on the grid:
                        // an arm that deliberately gets none of an item. It is tagged so it cannot
                        // be read as an unauthored cell.
                        const authoredZero = isConfiguredZero(row.absolute_kg);
                        return (
                          <tr key={row.experiment_config_id}>
                            <td />
                            <td />
                            <td />
                            <td />
                            <td>{row.feed_item}</td>
                            <td title={copy(pageContract, "label.experiment_absolute_kg_note")}>
                              <span style={{ display: "inline-flex", alignItems: "center", gap: 6, whiteSpace: "nowrap" }}>
                                <span
                                  style={{
                                    fontVariantNumeric: "tabular-nums",
                                    fontWeight: authoredZero ? 500 : 700,
                                    color: authoredZero ? "var(--muted)" : "var(--brand-d)",
                                  }}
                                >
                                  {row.absolute_kg}
                                </span>
                                {authoredZero ? (
                                  <span className="tag t-info" title={copy(pageContract, "label.configured_zero_note")}>
                                    {copy(pageContract, "label.configured_zero")}
                                  </span>
                                ) : null}
                              </span>
                            </td>
                            <td>
                              <span className={row.status === "active" ? "tag t-ok" : "tag t-mut"}>{row.status}</span>
                            </td>
                            <td>
                              <ExperimentCellEditor
                                pageContract={pageContract}
                                action={saveExperimentCell}
                                parkId={row.park_id}
                                shedId={row.shed_id}
                                partitionLabel={row.partition_label ?? ""}
                                feedItem={row.feed_item}
                                experimentCategory={row.experiment_category}
                                absoluteKg={row.absolute_kg}
                                headCount={row.head_count}
                              />
                            </td>
                          </tr>
                        );
                      })}
                    </Fragment>
                  );
                })
              )}
            </tbody>
          </table>
        </div>

        {/* The experiment section paginates too. It had no pager while the ration grid above did, so
            a park whose pens hold more cells than one page silently lost the overflow — and because
            an experiment pen that is missing from this screen is still FED, a truncated list reads
            as "these are all the experiment sheds" when it is not. rowCount is the flat cell count,
            which is what the backend paged; the pens above are a grouping of those same rows. */}
        <FeedPager
          pageContract={pageContract}
          offset={experimentOffset}
          limit={experimentLimit}
          rowCount={experimentRows.length}
          hasMore={experiment?.has_more ?? false}
          noun={copy(pageContract, "table.experiment.noun")}
          pageSizeOptions={experimentPageSizes}
          hrefForOffset={(next) => feedHref(PAGE_PATH, sp, "fc_exp_offset", String(next))}
          hrefForLimit={(next) => feedHref(PAGE_PATH, sp, "fc_exp_limit", String(next))}
        />
      </section>
      <div className="note" style={{ marginBottom: 16 }}>{copy(pageContract, "section.experiment.note")}</div>
      {/* Spelled out rather than implied, for the same reason the blocked-vs-zero note is above:
          this is the one thing an author on this section must not get wrong. */}
      <div className="note" style={{ marginBottom: 16 }}>{copy(pageContract, "section.experiment.switch_note")}</div>
      <div className="note" style={{ marginBottom: 16 }}>{copy(pageContract, "label.experiment_not_dated_note")}</div>

      {/* -------------------------------------------------- session template (read-only: no writer) */}
      <SectionError result={sessionsResult} titleKey="state.session_template_unavailable" pageContract={pageContract} />
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.session_template.title")}</h3>
          <span className="small muted">{copy(pageContract, "section.session_template.caption")}</span>
        </div>
        <div
          className="bd feed-scroll"
          style={{ padding: 0, overflowX: "auto" }}
          tabIndex={0}
          role="group"
          aria-label={copy(pageContract, "section.session_template.aria")}
        >
          <table className="feed-table" aria-label={copy(pageContract, "table.session_template.aria")}>
            <thead>
              <tr>
                {sessionCols.map((col) => (
                  <th key={col}>{col}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {(sessions?.items ?? []).length === 0 ? (
                <tr>
                  <td colSpan={sessionCols.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {!sessionsResult || sessionsResult.ok
                        ? copy(pageContract, "empty.session_template")
                        : copy(pageContract, "state.session_template_unavailable")}
                    </div>
                  </td>
                </tr>
              ) : (
                (sessions?.items ?? []).map((row) => (
                  <tr key={row.session_template_id}>
                    <td style={{ fontVariantNumeric: "tabular-nums" }}>{row.session_no}</td>
                    <td>{row.session_label}</td>
                    <td
                      style={{ fontVariantNumeric: "tabular-nums", fontWeight: 700 }}
                      title={copy(pageContract, "label.session_split_note")}
                    >
                      {row.split_fraction}
                    </td>
                    <td>
                      <span className={row.status === "active" ? "tag t-ok" : "tag t-mut"}>{row.status}</span>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>
      {splitMismatch ? (
        <div className="alert" style={{ marginBottom: 16 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>{copy(pageContract, "state.split_mismatch")}</div>
        </div>
      ) : null}
      <div className="note" style={{ marginBottom: 16 }}>{copy(pageContract, "section.session_template.note")}</div>

      {/* -------------------------------------------------------------- feeding schedule (editable) */}
      <SectionError result={scheduleResult} titleKey="state.schedule_unavailable" pageContract={pageContract} />
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.schedule.title")}</h3>
          <span className="small muted">{copy(pageContract, "section.schedule.caption")}</span>
        </div>
        <div
          className="bd feed-scroll"
          style={{ padding: 0, overflowX: "auto" }}
          tabIndex={0}
          role="group"
          aria-label={copy(pageContract, "section.schedule.aria")}
        >
          <table className="feed-table" aria-label={copy(pageContract, "table.schedule.aria")}>
            <thead>
              <tr>
                {scheduleCols.map((col) => (
                  <th key={col}>{col}</th>
                ))}
                <th>{copy(pageContract, "action.edit_schedule")}</th>
              </tr>
            </thead>
            <tbody>
              {(schedule?.items ?? []).length === 0 ? (
                <tr>
                  <td colSpan={scheduleCols.length + 1}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {!scheduleResult || scheduleResult.ok
                        ? copy(pageContract, "empty.schedule")
                        : copy(pageContract, "state.schedule_unavailable")}
                    </div>
                  </td>
                </tr>
              ) : (
                (schedule?.items ?? []).map((row) => (
                  <tr key={row.schedule_config_id}>
                    <td>
                      <span className={row.workflow === "experiment" ? "tag t-pur" : "tag t-ok"}>
                        {copy(
                          pageContract,
                          row.workflow === "experiment" ? "label.workflow_experiment" : "label.workflow_normal",
                        )}
                      </span>
                    </td>
                    {/* Each cell carries the contract's explanation of what its time DOES. These
                        three moments sit hours apart on the same afternoon, so without it the pair
                        14:00 / 15:45 reads as a feeding window rather than issue-and-cutoff. */}
                    <td
                      style={{ fontVariantNumeric: "tabular-nums" }}
                      title={copy(pageContract, "label.direction_time_note")}
                    >
                      {row.direction_time}
                    </td>
                    <td
                      style={{ fontVariantNumeric: "tabular-nums" }}
                      title={copy(pageContract, "label.correction_time_note")}
                    >
                      {row.correction_time}
                    </td>
                    {/* An omitted transport time means the park declared NO cutoff — that is UNKNOWN,
                        and rendering it as a blank cell would read as "no deadline". The contract's
                        placeholder is used so the gap is visible as a gap. */}
                    <td
                      className="muted"
                      style={{ fontVariantNumeric: "tabular-nums" }}
                      title={copy(pageContract, "label.transport_time_note")}
                    >
                      {row.transport_time ?? copy(pageContract, "label.placeholder")}
                    </td>
                    <td>
                      <EffectiveWindow validFrom={row.valid_from} validTo={row.valid_to} pageContract={pageContract} />
                    </td>
                    <td>
                      <ScheduleEditor
                        pageContract={pageContract}
                        action={saveSchedule}
                        parkId={row.park_id}
                        workflow={row.workflow}
                        directionTime={row.direction_time}
                        correctionTime={row.correction_time}
                        transportTime={row.transport_time}
                      />
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>
      <div className="note">{copy(pageContract, "section.schedule.note")}</div>
    </div>
  );
}
