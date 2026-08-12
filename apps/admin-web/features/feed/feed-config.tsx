import { Fragment } from "react";
import { redirect } from "next/navigation";
import { AlertTriangle } from "lucide-react";

import { copy, optionGroup, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedConfigExperiment } from "@/lib/api/server";
import {
  firstAuthRequiredError,
  listFeedConfigExperiment,
  listFeedConfigFeedItems,
  listFeedConfigRationGroups,
  listFeedConfigRationRates,
  listFeedConfigSchedule,
  listFeedConfigSessionTemplates,
  listFeedConfigShedTags,
  type ApiResult,
} from "@/lib/api/server";
import { getCensusLocations, listAllFeedConfigPens } from "@/lib/api/herd-locations";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { all, one, type RouteSearchParams } from "@/lib/search-params";
import { FeedFilters, type FeedFilterField } from "./feed-filters";
import { FeedPager } from "./feed-pager";
import { FeedFaroView } from "./feed-faro-view";
import { isConfiguredZero } from "./feed-quantity";
import { RationRateValue } from "./feed-rate-optimistic";
import { feedHref, feedLimit, feedOffset, resolveFeedScope } from "./feed-scope";
import {
  enrolExperimentPen,
  saveExperimentCell,
  saveFeedItem,
  saveRationRate,
  saveSchedule,
  setExperimentShedStatus,
  setFeedItemStatus,
} from "./feed-config-actions";
import {
  ExperimentCellEditor,
  ExperimentCellAdder,
  ExperimentPenEnroller,
  ExperimentShedSwitch,
  FeedItemCreator,
  FeedItemStatusSwitch,
  RationRateEditor,
  ScheduleEditor,
} from "./feed-config-editor";
import { experimentEnrollerScopeKey } from "./experiment-enroller-scope";

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
// Experiment pages count complete pens, not their individual feed-item cells.
const EXPERIMENT_PAGE_SIZE = 10;
const EXPERIMENT_ALL_PARKS_PAGE_SIZE = 25;
// The authored feed vocabulary (ration groups, shed tags, feed items) backs the FILTER dropdowns,
// so the whole catalog must arrive in one bounded page — a screenful-sized limit would silently
// truncate it (there are already 31 shed tags, past SECONDARY_PAGE_SIZE) and reintroduce the very
// "filter only shows what's on the current grid page" bug this replaces. Bounded config, not a herd
// scan: the backend caps these catalogs and reports has_more.
const VOCAB_PAGE_SIZE = 200;


/**
 * The comparison operators the feed-config reads accept. Mirrors the `grams_op` / `kg_op` enum in
 * the OpenAPI contract, and the `feed_grams_compare` option group the controls are labelled from.
 */
type CompareOp = "gt" | "gte" | "eq" | "lte" | "lt" | "neq";

/**
 * Narrows a URL-supplied operator, PASSING AN UNRECOGNISED ONE THROUGH rather than dropping it.
 *
 * Dropping would silently widen the result: a hand-edited `?fc_grams_op=roughly` would quietly show
 * the whole grid to someone who asked to narrow it, with nothing on screen saying the filter was
 * ignored. Forwarded, the backend rejects it and the section renders its error band — which is the
 * honest outcome, and the one the surface-API-errors rule requires.
 *
 * Blank is different and IS dropped: no operator means no filter, which is a real state the
 * controls produce every time an operator clears one.
 */
function asCompareOp(raw: string): CompareOp | undefined {
  return raw === "" ? undefined : (raw as CompareOp);
}

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
  /** Backend-supplied park name. Shown as its own column because the list may span both parks. */
  parkName: string | null | undefined;
  /** The pen's HUMAN label; empty for an undivided shed. Echoed back on every write. */
  partitionLabel: string;
  /** Server-composed "Mandela 1 - Part 3". Shown verbatim -- never rejoined here. */
  locationDisplay: string | null | undefined;
  /** The experiment ARM. Taken from the pen's rows, which the writer keeps consistent. */
  category: string;
  /** INFORMATIONAL population. Null means not recorded — never rendered or sent as 0. */
  headCount: number | null;
  /**
   * True when ANY of the pen's rows is active, which is exactly ExperimentPlanner.Applies' rule.
   * The complete-pen status write keeps the rows in step, so a mixed pen is not a state this UI can
   * create; deriving it this way rather than reading row[0] means a legacy mixed shed still reports
   * the workflow that would actually feed it.
   */
  active: boolean;
  rows: FeedConfigExperiment[];
};

type FeedConfigPenOption = {
  park_id: string;
  shed_id: string;
  partition_label?: string | null;
  operational_location_display: string;
  has_experiment_config: boolean;
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
  // No ration-group filter. BREED is the one cohort control on this bar (maintainer decision
  // 2026-08-11): two controls over the same column read as a duplicate, and the group is already the
  // grid's own first column, so a reader can see what a row is without a second dropdown to say it.
  //
  // The consequence, stated so it is not rediscovered as a defect: `Kid` is a ration group with NO
  // breed — kids of every breed collapse to it by age band and never touch the breed map
  // (feeddirection/domain.ConfigSnapshot.RationGroupByBreedKey is adult-breeds-only) — so those rates
  // are no longer reachable from this bar and are found by paging or by the shed-tag/feed-item
  // filters. `fc_group` is not read either: a control that is gone must not keep narrowing the grid
  // from a stale bookmarked URL with nothing on screen saying so.
  const breedFilter = one(sp, "fc_breed") || "";
  const shedTagFilter = one(sp, "fc_tag") || "";
  // A SET, read with `all` because the parameter repeats. `one` would keep the first pick and
  // silently drop the rest, showing a narrower grid than the chips say is applied.
  const feedItemFilter = all(sp, "fc_item");
  const gramsOpFilter = one(sp, "fc_grams_op") || "";
  const gramsValueFilter = one(sp, "fc_grams_value") || "";

  // Experiment section filters, on their OWN params. The two sections page independently (see the
  // offsets below) and must filter independently for the same reason: they hold different row
  // counts and incompatible units, so one section's narrowing must not silently reshape the other.
  // The experiment section's OWN park and shed narrowing, on its own params.
  //
  // Additive: blank leaves the section's scope exactly as it was — every park when nobody chose one
  // on the page, that park otherwise. Picking a park here narrows THIS section only, which is the
  // point: this is the one read on the screen that can span both parks (a cell carries its own park,
  // unlike a rate or a dispatch clock), so it is also the only one that needs a park control of its
  // own to cut a 150-row two-park list down.
  const experimentParkFilter = one(sp, "fc_exp_park") || "";
  // ONE VALUE carrying both halves of a pen: "<shed_id>|<partition label>". Split on the FIRST
  // separator only — a shed id is a UUID and contains none, so the remainder is the label verbatim
  // however it is punctuated. Two URL params would let a reader hand-edit one half and filter by a
  // pen that does not exist.
  const experimentPenFilter = one(sp, "fc_exp_shed") || "";
  const penSeparator = experimentPenFilter.indexOf("|");
  const experimentShedFilter =
    penSeparator === -1 ? experimentPenFilter : experimentPenFilter.slice(0, penSeparator);
  const experimentPartitionFilter =
    penSeparator === -1 ? "" : experimentPenFilter.slice(penSeparator + 1);
  const experimentItemFilter = all(sp, "fc_exp_item");
  const experimentArmFilter = one(sp, "fc_exp_arm") || "";
  const experimentStatusFilter = one(sp, "fc_exp_status") || "";
  const experimentKgOpFilter = one(sp, "fc_exp_kg_op") || "";
  const experimentKgValueFilter = one(sp, "fc_exp_kg_value") || "";

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
  // The park this section actually reads: its own filter when set, otherwise the page's rule.
  const experimentParkId = experimentParkFilter || (scope.parkSource === "fallback" ? "" : scope.parkId);
  const experimentAllParks = experimentParkId === "";
  // A cross-park page holds both parks' pens (175 rows today against one park's 90), so the default
  // page size steps up to the contract's largest option in that mode. Still bounded, still paged.
  const experimentDefaultSize = experimentAllParks ? EXPERIMENT_ALL_PARKS_PAGE_SIZE : EXPERIMENT_PAGE_SIZE;
  const experimentLimit = feedLimit(sp, "fc_exp_limit", experimentPageSizes, experimentDefaultSize);
  const experimentOffset = feedOffset(sp, "fc_exp_offset");

  // Six independent authored surfaces, fetched concurrently — no serial await, and no draining of
  // any of them: each is one bounded page.
  const [
    ratesResult,
    sessionsResult,
    scheduleResult,
    experimentResult,
    pensResult,
    feedItemsResult,
    rationGroupsResult,
    shedTagsResult,
  ] = await Promise.all([
    scope.parkId
      ? listFeedConfigRationRates({
          park_id: scope.parkId,
          breed: breedFilter || undefined,
          shed_tag: shedTagFilter || undefined,
          feed_item: feedItemFilter,
          // Both halves or neither: the backend rejects a lone half rather than defaulting it, so a
          // partly-filled control sends nothing at all and the grid stays unfiltered until the pair
          // is complete.
          grams_op: asCompareOp(gramsOpFilter),
          grams_value: gramsValueFilter || undefined,
          limit: gridLimit,
          offset: gridOffset,
        })
      : Promise.resolve(null),
    scope.parkId ? listFeedConfigSessionTemplates({ park_id: scope.parkId, limit: SECONDARY_PAGE_SIZE }) : Promise.resolve(null),
    scope.parkId ? listFeedConfigSchedule({ park_id: scope.parkId, limit: SECONDARY_PAGE_SIZE }) : Promise.resolve(null),
    // No `status` filter: retired rows must stay visible so a withdrawn shed's authored quantities
    // can be seen and restored, and so an accidental withdrawal is not invisible on the screen that
    // owns the decision. One bounded page — the live parks author 17 sheds x 5 items each.
    scope.parkId
      ? listFeedConfigExperiment({
          // park_id omitted entirely in all-parks mode -- the backend reads that as "every park".
          ...(experimentAllParks ? {} : { park_id: experimentParkId }),
          // Backend-supported since this endpoint shipped; it was simply never exposed.
          ...(experimentShedFilter ? { shed_id: experimentShedFilter } : {}),
          ...(experimentPartitionFilter ? { partition_label: experimentPartitionFilter } : {}),
          limit: experimentLimit,
          offset: experimentOffset,
          feed_item: experimentItemFilter,
          experiment_category: experimentArmFilter || undefined,
          status: experimentStatusFilter === "active" || experimentStatusFilter === "retired"
            ? experimentStatusFilter
            : undefined,
          kg_op: asCompareOp(experimentKgOpFilter),
          kg_value: experimentKgValueFilter || undefined,
        })
      : Promise.resolve(null),
    // The PEN CATALOG, for the enrol control's candidate list.
    //
    // Read from locations/shed_partitions, NOT derived from the experiment cells above, and that is
    // the whole point of the endpoint: the cell list is paginated and shed-incomplete, so deriving
    // candidates from it made a pen configured on another page look unconfigured, and made a NEW pen
    // of an already-enrolled shed unreachable entirely. It follows the same all-parks rule as the
    // cell read so the two sections agree about what is in scope.
    experimentAllParks || experimentParkId
      ? listAllFeedConfigPens(
          experimentAllParks
            // park_id omitted entirely -- the backend reads that as "every park", the same rule the
            // experiment read above follows so the table and its enroller agree about scope.
            ? {}
            : { park_id: experimentParkId },
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
    sessionsResult,
    scheduleResult,
    experimentResult,
    pensResult,
    feedItemsResult,
    rationGroupsResult,
    shedTagsResult,
  );
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const rates = ratesResult && ratesResult.ok ? ratesResult.data : null;
  const sessions = sessionsResult && sessionsResult.ok ? sessionsResult.data : null;
  const schedule = scheduleResult && scheduleResult.ok ? scheduleResult.data : null;
  const experiment = experimentResult && experimentResult.ok ? experimentResult.data : null;
  const pens = pensResult && pensResult.ok ? pensResult.data : null;
  const feedItems = feedItemsResult && feedItemsResult.ok ? feedItemsResult.data : null;
  const rationGroups = rationGroupsResult && rationGroupsResult.ok ? rationGroupsResult.data : null;
  const shedTags = shedTagsResult && shedTagsResult.ok ? shedTagsResult.data : null;

  // Experiment rows arrive flat (one per shed x feed item) and are grouped by shed for display,
  // because the SHED is the unit of every decision in this section: the arm, the head count and the
  // workflow switch all describe a shed, not a cell. Grouping is a pure regroup of ONE bounded page
  // — no second fetch, no accumulation across pages, and the shed's own rows are the only input.
  const experimentRows = experiment?.items ?? [];
  const experimentSheds = groupExperimentRowsByShed(experimentRows);
  const penItems = (pens?.items ?? []) as FeedConfigPenOption[];
  // PENS with no authored experiment cell — the candidates the enrol control offers.
  //
  // `has_experiment_config` is computed by the backend against the SAME (shed, partition) natural
  // key the experiment table is unique on, so the candidate filter cannot disagree with what a
  // write would land on. It replaces a filter derived from `experimentRows`, i.e. from ONE PAGE of
  // cells, which had two failure modes: a pen whose cells fell on another page looked unconfigured,
  // and a shed with any enrolled pen excluded ALL its pens — so a new pen of that shed could not be
  // added at all. A pen holding only RETIRED cells is correctly not a candidate: it already has
  // authored quantities and is restored through its own row group rather than re-enrolled.
  const candidatePens = penItems
    .filter((pen) => !pen.has_experiment_config)
    .map((pen) => ({
      parkId: pen.park_id,
      shedId: pen.shed_id,
      partitionLabel: pen.partition_label ?? "",
      // Backend-composed. Clients never rejoin a shed name and a partition themselves.
      display: pen.operational_location_display,
    }));
  // ACTIVE items only. A retired item is on no feed sheet, so offering it as something to author a
  // quantity for would invite an author to configure a cell that can never be served — and, on the
  // experiment adder, to fill a pen's last unauthored slot with an item that feeds nothing. The
  // catalog TABLE below still lists retired items, because that is where they are put back.
  const catalogItems = (feedItems?.items ?? [])
    .filter((item) => item.status === "active")
    .map((item) => item.feed_item);

  const gridCols = tableLabels(pageContract, "ration-grid");
  const sessionCols = tableLabels(pageContract, "session-template");
  const scheduleCols = tableLabels(pageContract, "schedule-config");
  const experimentCols = tableLabels(pageContract, "experiment-config");
  const feedItemCols = tableLabels(pageContract, "feed-items");

  const shedNameById = new Map(locations.sheds.map((shed) => [shed.id, shed.name]));
  // The park being read, by name. Live data from the locations master — never composed from a code
  // or an id, and blank only when the master returned no parks at all.
  const parkName = locations.parks.find((park) => park.id === scope.parkId)?.name ?? "";
  // The park the enroller offers in SINGLE-park mode: exactly the one being read, so its select has
  // one option and is preselected. Empty when the locations master returned no match, which leaves
  // the enroller with nothing to enrol into rather than guessing a park.
  // Scoped to the EXPERIMENT section's own park, not the page's, because the enroller lives in that
  // section and the two can now differ. Offering the page's park here would enrol a pen into a park
  // the table on screen is not showing.
  const parkScopedParks = locations.parks
    .filter((park) => park.id === experimentParkId)
    .map((park) => ({ id: park.id, name: park.name }));
  // The experiment section's park name, from its OWN effective scope rather than the page's — the
  // two can differ now, and labelling a CBE table with the page's CPT would be worse than no label.
  const experimentParkName = locations.parks.find((park) => park.id === experimentParkId)?.name ?? "";
  // Sheds offered by the section's Shed filter: the locations master, narrowed to the park the
  // section is actually reading. Keyed on shed_id, NEVER on the name — Castro, Gandhi and Yashoda
  // each exist in BOTH parks, so a name-keyed option would merge two different buildings into one
  // row and narrow to whichever the backend matched first.
  //
  // In all-parks mode the label is park-qualified for the same reason: two options reading "Castro"
  // are indistinguishable to the operator even though their values differ. This is a park + shed
  // pair, not a shed + partition operational location, so it composes here rather than through
  // oploc — that helper owns the shed/partition display and would be the wrong shape for this.
  const parkNameById = new Map(locations.parks.map((park) => [park.id, park.name]));
  // PENS, not physical sheds, because that is what this section's rows are: Castro holds three pens
  // with their own arms, head counts and quantities, so a shed-level option would name one thing and
  // return three.
  //
  // The label is the BACKEND-COMPOSED operational location, rendered verbatim. It must not be
  // rejoined here: the convention renders a pen as "Castro - 1" / "Godel 1 - Part 3", and the naive
  // space-join that produces "Castro 1" is a recorded production defect (OL-3), not a shortcut. The
  // catalog is the pen source rather than the cell rows, so an empty pen is still offered and a pen
  // whose cells fall on another page is not missing from the list.
  const experimentPenOptions = penItems
    .filter((pen) => (experimentParkId ? pen.park_id === experimentParkId : true))
    // ONLY pens that are actually on the experiment. The catalog holds every operational pen in the
    // park — 131 of them tenant-wide — and all but ~35 have no experiment cell at all, so offering
    // the lot made most choices return an empty table for a pen that was never on this workflow.
    // has_experiment_config is computed by the backend against the SAME (shed, partition) natural
    // key the table is keyed on, so the picker cannot disagree with the rows it filters. It counts
    // RETIRED pens too, which is right: a withdrawn pen still has authored quantities and is exactly
    // what someone filters for to restore it.
    .filter((pen) => pen.has_experiment_config)
    .map((pen) => ({
      // Keyed on shed_id + partition, NEVER on a name: Castro, Gandhi and Yashoda each exist in both
      // parks, so a name-keyed value would merge two different buildings.
      value: `${pen.shed_id}|${pen.partition_label ?? ""}`,
      label: experimentParkId
        ? pen.operational_location_display
        : `${parkNameById.get(pen.park_id) ?? ""} · ${pen.operational_location_display}`.replace(/^ · /, ""),
    }))
    .sort((a, b) => a.label.localeCompare(b.label, undefined, { numeric: true }));
  // EVERY control on the bar, so an empty grid says which of the two things happened: nothing is
  // authored, or the filters excluded it. Breed and the grams comparison were missing from this
  // check, which sent a reader who had narrowed by breed alone to the "no rates authored" copy —
  // the more alarming of the two answers, and the wrong one.
  const hasGridFilter = Boolean(
    breedFilter || shedTagFilter || feedItemFilter.length > 0 || gramsOpFilter || gramsValueFilter,
  );
  // The experiment section narrows on its OWN params, so it needs its own answer to "did the filters
  // empty this, or is it genuinely empty?" — the grid's flag would report the wrong section.
  const hasExperimentFilter = Boolean(
    experimentParkFilter ||
      experimentPenFilter ||
      experimentItemFilter.length > 0 ||
      experimentArmFilter ||
      experimentStatusFilter ||
      experimentKgOpFilter ||
      experimentKgValueFilter,
  );

  const gridRows = rates?.items ?? [];

  // Filter options are the tenant's authored catalog — the FULL vocabulary — not the current grid
  // page. Deriving them from gridRows showed only the values on the visible (paginated,
  // already-filtered) rows, so a park's other breeds / shed tags / feed items were unreachable in
  // the filter. Shed tags and feed items carry a backend display_order, which is authoritative and
  // preserved rather than re-sorted alphabetically. If a catalog read failed, fall back to the
  // grid-derived set so the filter never goes empty.
  const uniqueSorted = (values: string[]) => Array.from(new Set(values)).sort();
  const dedupe = (values: string[]) => Array.from(new Set(values));
  const toOptions = (values: string[]) => values.map((value) => ({ value, label: value }));
  const byDisplayOrder = <T extends { display_order: number }>(rows: readonly T[]) =>
    [...rows].sort((a, b) => a.display_order - b.display_order);

  // Real BREEDS, from the same breed -> ration-group map the backend resolves the filter through, so
  // every option is one the query can answer. Deduplicated and sorted by breed rather than by group:
  // Beetal and Sirohi are two options that happen to return the same rows, and collapsing them would
  // put the operator back to picking a group. There is no grid-derived fallback -- the grid carries
  // GROUP labels, and offering "Beetal/Sirohi" or "Kid" as a breed is the mislabelling this filter
  // exists to avoid; if the map cannot be read the control is simply empty.
  const breedOptions = toOptions(uniqueSorted((rationGroups?.items ?? []).map((group) => group.breed)));
  // The arms present on the CURRENT experiment page. See the field definition for why this is
  // page-derived rather than a catalog read.
  const experimentArmOptions = toOptions(
    uniqueSorted(experimentRows.map((row) => row.experiment_category).filter(Boolean)),
  );
  const shedTagOptions = shedTags
    ? toOptions(dedupe(byDisplayOrder(shedTags.items).map((tag) => tag.shed_tag)))
    : toOptions(uniqueSorted(gridRows.map((row) => row.shed_tag)));
  // Also active-only: the grid no longer holds a retired item's rates, so offering one as a FILTER
  // would be an option that always returns nothing — indistinguishable on screen from a combination
  // that genuinely has no rate authored.
  const feedItemOptions = feedItems
    ? toOptions(
        dedupe(
          byDisplayOrder(feedItems.items)
            .filter((item) => item.status === "active")
            .map((item) => item.feed_item),
        ),
      )
    : toOptions(uniqueSorted(gridRows.map((row) => row.feed_item)));

  const compareOptions = optionGroup(pageContract, "feed_grams_compare").map((option) => ({
    value: option.key,
    label: option.label,
  }));

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
    // BREED is the ONLY cohort control here. The options are real breeds from the breed ->
    // ration-group map, so picking Sirohi finds the Beetal/Sirohi rows -- a question a group filter
    // could not express, because no group is named Sirohi. The grid's own column keeps showing the
    // GROUP, which is what the row actually is, so the cohort stays readable without a second
    // dropdown naming it.
    //
    // The note is now RENDERED rather than authored and dropped. It carries the one thing this
    // control cannot show by itself: several breeds share a rate, and picking any breed excludes the
    // breedless `Kid` group entirely. That exclusion is invisible in the grid -- the rows simply are
    // not there -- so a filter that silently removes a fifth of the authored rates has to say so.
    {
      kind: "select",
      param: "fc_breed",
      label: copy(pageContract, "filter.breed_label"),
      value: breedFilter,
      options: breedOptions,
      note: copy(pageContract, "filter.breed_note"),
    },
    {
      kind: "select",
      param: "fc_tag",
      label: copy(pageContract, "filter.shed_tag_label"),
      value: shedTagFilter,
      options: shedTagOptions,
    },
    {
      kind: "multiselect",
      param: "fc_item",
      label: copy(pageContract, "filter.feed_item_label"),
      values: feedItemFilter,
      options: feedItemOptions,
      note: copy(pageContract, "filter.feed_item_note"),
    },
    {
      kind: "compare",
      param: "fc_grams_op",
      valueParam: "fc_grams_value",
      label: copy(pageContract, "filter.grams_label"),
      op: gramsOpFilter,
      value: gramsValueFilter,
      options: compareOptions,
      valueAriaLabel: copy(pageContract, "filter.grams_value_aria"),
      note: copy(pageContract, "filter.grams_note"),
    },
  ];

  // The experiment section's own bar. Same controls, different params and different units: this
  // section's quantity is an ABSOLUTE PEN TOTAL in kg, so its comparison is labelled and named apart
  // from the grid's per-head grams.
  const experimentFilterFields: FeedFilterField[] = [
    {
      kind: "select",
      param: "fc_exp_park",
      label: copy(pageContract, "filter.park_label"),
      value: experimentParkFilter,
      options: locations.parks.map((park) => ({ value: park.id, label: park.name })),
      // A pen belongs to ONE park, so a pen chosen in the other one cannot match anything here.
      // Left in place it emptied the table while both controls still read as a valid pair, which is
      // unexplainable on screen — the reader sees a park that has 60 pens and a table showing none.
      clears: ["fc_exp_shed"],
    },
    {
      kind: "select",
      param: "fc_exp_shed",
      label: copy(pageContract, "filter.shed_label"),
      value: experimentPenFilter,
      options: experimentPenOptions,
    },
    {
      kind: "multiselect",
      param: "fc_exp_item",
      label: copy(pageContract, "filter.feed_item_label"),
      values: experimentItemFilter,
      options: feedItemOptions,
    },
    {
      kind: "select",
      param: "fc_exp_arm",
      label: copy(pageContract, "filter.experiment_arm_label"),
      value: experimentArmFilter,
      // The arms in scope, from the rows themselves: an arm is free prose authored per pen
      // ("Mixed (9 Goat F, 1 Sheep F, 5 Goat M) NEW - warmup 20:80"), not a catalog, so there is no
      // vocabulary endpoint to read. That makes this list page-derived and therefore incomplete when
      // the section is paged -- which is why it sits beside the arm the operator can already see
      // rather than claiming to be every arm in the tenant.
      options: experimentArmOptions,
    },
    {
      kind: "select",
      param: "fc_exp_status",
      label: copy(pageContract, "filter.status_label"),
      value: experimentStatusFilter,
      options: optionGroup(pageContract, "feed_config_status").map((option) => ({
        value: option.key,
        label: option.label,
      })),
    },
    {
      kind: "compare",
      param: "fc_exp_kg_op",
      valueParam: "fc_exp_kg_value",
      label: copy(pageContract, "filter.kg_label"),
      op: experimentKgOpFilter,
      value: experimentKgValueFilter,
      options: compareOptions,
      valueAriaLabel: copy(pageContract, "filter.kg_value_aria"),
      note: copy(pageContract, "filter.experiment_note"),
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
        {/* STAGED, not applied per control. Six filters sit on this bar and an author normally
            narrows by several at once — park, then breed, then item — and every one of those picks
            re-ran the whole page: nine concurrent reads, four tables, and three intermediate result
            sets the reader never asked to see. Edits now collect on the bar and one Apply commits
            them. */}
        <FeedFilters
          basePath={PAGE_PATH}
          pageParam="fc_offset"
          fields={filterFields}
          pageContract={pageContract}
          deferApply
        >
        {/* The grid is passed to the bar so ONE pending state drives both the bar's busy ring and
            these rows being held back. They stay readable while the new page is fetched — the old
            answer is still true until the new one lands — but go inert, so a stale row cannot be
            clicked or mistaken for the result of the filter just applied. */}
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
                  return (
                    <tr key={row.ration_rate_id}>
                      {/* No park cell. Every /feed-config/* read requires park_id and filters on
                          it, and all four sections on this page share ONE Park filter — so a park
                          column would repeat the same value on every row. */}
                      <td>{row.ration_group}</td>
                      <td className="muted">{row.shed_tag}</td>
                      <td>{row.feed_item}</td>
                      {/* A client cell so a just-saved quantity appears at once. Saving writes in
                          ~0.3s but the number only lands when revalidatePath re-renders this whole
                          route, and until then the cell showed the OLD figure beside a form that had
                          closed on success — which reads as "nothing happened" on a screen whose
                          numbers are feeding instructions. The authored-zero rule travels with it. */}
                      <td>
                        <RationRateValue
                          pageContract={pageContract}
                          parkId={row.park_id}
                          rationGroup={row.ration_group}
                          shedTag={row.shed_tag}
                          feedItem={row.feed_item}
                          gramsPerHead={row.grams_per_head}
                        />
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

        {/* Inside the held-back region too: the pager describes the page being replaced, so leaving
            it live would let a reader page a result set that is already on its way out. */}
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
        </FeedFilters>
      </section>


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
                    {/* The status cell carries BOTH the state and the control that changes it. The
                        chip resolves its label through the page contract rather than printing
                        row.status: that column holds storage vocabulary, and the retired value in
                        particular reads as a property of the item rather than as the thing it
                        actually means, which is that the item is on no feed sheet. */}
                    <td>
                      <div style={{ display: "flex", alignItems: "center", gap: 8, whiteSpace: "nowrap" }}>
                        <span
                          className={row.status === "active" ? "tag t-ok" : "tag t-mut"}
                          title={copy(
                            pageContract,
                            row.status === "active" ? "label.feed_item_active_note" : "label.feed_item_retired_note",
                          )}
                        >
                          {copy(
                            pageContract,
                            row.status === "active" ? "label.feed_item_active" : "label.feed_item_retired",
                          )}
                        </span>
                        {/* Offers the OPPOSITE of the current state, so the control always names the
                            change it makes rather than the state it is in — the same rule the
                            experiment pen switch follows. */}
                        <FeedItemStatusSwitch
                          pageContract={pageContract}
                          action={setFeedItemStatus}
                          feedItemId={row.feed_item_id}
                          feedItemLabel={row.feed_item}
                          targetStatus={row.status === "active" ? "retired" : "active"}
                        />
                      </div>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </section>

      {/* ------------------------------------------------------------ experiment sheds (editable) */}
      {/* Its OWN section, deliberately separated from the ration grid above rather than mixed into
          it. The two hold numbers with incompatible units: a grid cell is a per-head RATE that gets
          multiplied by a projected head count, and an experiment cell is an ABSOLUTE shed total that
          must never be multiplied by anything. Putting them in one table would place both under a
          single "quantity" heading on a screen whose output is a feeding instruction.

          Rows are grouped by PEN because the pen is the unit of every decision here — the arm, the
          head count, and the workflow switch all describe one pen rather than a cell, and rather
          than a whole shed: Mandela 1's ten pens each carry their own arm and head count. */}
      <SectionError result={experimentResult} titleKey="state.experiment_unavailable" pageContract={pageContract} />
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.experiment.title")}</h3>
          {/* The scope, on the section itself, and it must state what the table ACTUALLY holds.
              In company-wide mode this read is tenant-wide and the table carries a park column, so
              naming one park here would label a two-park table as one park — the fallback park at
              that, which nobody chose. Single-park mode names the park, because the table then
              really is one park's and carries no park column. The NAME is live data from the
              locations master; both LABELS stay backend-owned. */}
          {experimentAllParks ? (
            <span className="tag t-mut" title={copy(pageContract, "filter.park_label")}>
              {copy(pageContract, "label.all_parks")}
            </span>
          ) : experimentParkName ? (
            <span className="tag t-mut" title={copy(pageContract, "filter.park_label")}>
              {experimentParkName}
            </span>
          ) : null}
          <span className="small muted">{copy(pageContract, "section.experiment.caption")}</span>
          <div className="sp" style={{ flex: 1 }} />
          {/* Enrolment lives in the section header, next to the list it changes. It authors the
              pen's first quantities, which IS what moves it onto the experiment workflow.
              In company-wide mode it offers BOTH parks and makes the reader pick one, rather than
              silently enrolling into the fallback park the table is no longer scoped to. */}
          <ExperimentPenEnroller
            key={experimentEnrollerScopeKey(experimentAllParks ? locations.parks : parkScopedParks)}
            pageContract={pageContract}
            action={enrolExperimentPen}
            parks={experimentAllParks ? locations.parks.map((park) => ({ id: park.id, name: park.name })) : parkScopedParks}
            pens={candidatePens}
            feedItems={catalogItems}
          />
        </div>

        {/* The section's own filter bar, on its own params. It pages independently of the ration
            grid above and holds a different unit, so it narrows independently too. */}
        <FeedFilters
          basePath={PAGE_PATH}
          pageParam="fc_exp_offset"
          fields={experimentFilterFields}
          pageContract={pageContract}
          deferApply
        >
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
                      {/* THREE different states, and telling them apart is the whole point. A failed
                          read is an error. An empty UNFILTERED table is a real, meaningful fact: this
                          park runs no experiments and every shed in it is fed from the ration grid.
                          An empty FILTERED table is neither — the filters simply excluded everything.
                          The filtered copy was authored in the contract and had no code path to the
                          screen, so narrowing to nothing reported "no experiment pens authored for
                          this park", which is alarming and untrue. */}
                      {!experimentResult || experimentResult.ok
                        ? hasExperimentFilter
                          ? copy(pageContract, "empty.experiment_filtered")
                          : copy(pageContract, "empty.experiment")
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
	                  const partition = shed.partitionLabel;
	                  return (
                    // Fragment.key, not a key on the first <tr>: a pen contributes SEVERAL sibling
                    // rows, so the fragment is the list item React reconciles and the key belongs on
                    // it. Keying only the inner rows leaves the fragment itself unkeyed.
                    //
                    // The key carries the PARTITION as well as the shed: a partitioned shed yields
                    // one group per pen, so shed_id alone gave ten siblings the SAME key -- React
                    // then reconciles them onto each other and an edit to one pen can paint another
                    // pen's row.
	                    <Fragment key={shed.shedId + "#partition:" + partition}>
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
                              partitionLabel={shed.partitionLabel}
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
            as "these are all the experiment sheds" when it is not. The backend pages complete pens,
            so rowCount uses the grouped pen count as well. */}
        <FeedPager
          pageContract={pageContract}
          offset={experimentOffset}
          limit={experimentLimit}
          rowCount={experimentSheds.length}
          hasMore={experiment?.has_more ?? false}
          noun={copy(pageContract, "table.experiment.noun")}
          pageSizeOptions={experimentPageSizes}
          hrefForOffset={(next) => feedHref(PAGE_PATH, sp, "fc_exp_offset", String(next))}
          hrefForLimit={(next) => feedHref(PAGE_PATH, sp, "fc_exp_limit", String(next))}
        />
        </FeedFilters>
      </section>

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
    </div>
  );
}
