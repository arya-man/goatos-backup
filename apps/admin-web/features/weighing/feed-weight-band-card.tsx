"use client";

import { useEffect, useMemo, useState } from "react";
import { Search, Wheat } from "lucide-react";

import { LocalOverlayLink } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, table, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedWeightBandResponse } from "@/lib/api/server";
import { fmtDate } from "@/lib/format";
import {
  FeedWeightBandTable,
  FeedWeightBandUnmatchedTable,
  type FeedWeightBandTableRow,
  type FeedWeightBandUnmatchedRow,
} from "./feed-weight-band-table";
import {
  FeedWeightBandExitsDrawer,
  type FeedWeightBandExitItem,
  type FeedWeightBandExitScope,
} from "./feed-weight-band-exits-drawer";

/**
 * FEED BY WEIGHT BAND -- the interactive card (maintainer request 2026-09-18: one read per
 * top-bar filter change, everything under the card is client-side over that payload).
 *
 * The server component fetches ONE payload for the page's Park / Period / Weighing / Sex /
 * Origin and hands it here with the page contract. Every control on the card -- the
 * Matched | Not shown view, On farm | Include exited, the table filters, the search, the
 * pager -- is React state over that payload: no request, no route re-run. The payload carries
 * BOTH head-count variants per band row, so the Animals toggle is a flip, not a refetch. The
 * state is mirrored into the `fb_*` URL params with history.replaceState so a link still opens
 * on the same view, without a navigation.
 */
const PARAMS = {
  view: "fb_view",
  type: "fb_type",
  source: "fb_src",
  band: "fb_band",
  pen: "fb_pen",
  group: "fb_group",
  animals: "fb_animals",
  search: "fb_q",
  limit: "fb_limit",
  offset: "fb_offset",
} as const;

const BAND_KEYS = ["under_15", "15_20", "20_25", "25_30", "30_35", "35_plus"];
const PAGE_SIZES = [10, 25, 50] as const;

export type FeedWeightBandInitialState = {
  view: string;
  type: string;
  source: string;
  band: string;
  pen: string;
  group: string;
  animals: string;
  search: string;
  limit: number;
  offset: number;
  exitScope?: string;
};

type State = Omit<FeedWeightBandInitialState, "exitScope">;

function boundedLimit(raw: number): number {
  return (PAGE_SIZES as readonly number[]).includes(raw) ? raw : 25;
}

export function FeedWeightBandCard({
  pageContract,
  feedBand,
  initial,
  periodLabel,
}: {
  pageContract: AdminUiPageContract;
  feedBand: FeedWeightBandResponse | null;
  initial: FeedWeightBandInitialState;
  periodLabel: string;
}) {
  const [state, setState] = useState<State>({
    view: initial.view === "unmatched" ? "unmatched" : "matched",
    type: initial.type,
    source: initial.source,
    band: initial.band,
    pen: initial.pen,
    group: initial.group,
    animals: initial.animals === "all" ? "all" : "on_farm",
    search: initial.search,
    limit: boundedLimit(initial.limit),
    offset: Math.max(0, initial.offset),
  });
  const [draftSearch, setDraftSearch] = useState(initial.search);
  const includeExited = state.animals === "all";
  const view = state.view === "unmatched" ? "unmatched" : "matched";

  // Mirror the card's state into the URL without a navigation: a copied link opens on the same
  // view, and Back/Forward stay in the page's own history.
  useEffect(() => {
    const url = new URL(window.location.href);
    const set = (key: string, value: string) => {
      if (value) url.searchParams.set(key, value);
      else url.searchParams.delete(key);
    };
    set(PARAMS.view, view === "unmatched" ? "unmatched" : "");
    set(PARAMS.type, state.type);
    set(PARAMS.source, state.source);
    set(PARAMS.band, state.band);
    set(PARAMS.pen, state.pen);
    set(PARAMS.group, state.group);
    set(PARAMS.animals, includeExited ? "all" : "");
    set(PARAMS.search, state.search);
    set(PARAMS.limit, state.limit === 25 ? "" : String(state.limit));
    set(PARAMS.offset, state.offset ? String(state.offset) : "");
    if (url.href !== window.location.href) window.history.replaceState(window.history.state, "", url.href);
  }, [state, view, includeExited]);

  const update = (patch: Partial<State>) => setState((prev) => ({ ...prev, ...patch, offset: patch.offset ?? 0 }));

  const allRows = useMemo(() => feedBand?.rows ?? [], [feedBand]);
  const allUnmatched = useMemo(() => feedBand?.unmatched ?? [], [feedBand]);
  const allExits = useMemo(() => feedBand?.exited ?? [], [feedBand]);
  const recon = feedBand?.reconciliation;

  const sourceLabel = (key: string) =>
    key === "pen_average" ? copy(pageContract, "value.weighing.lump") : copy(pageContract, "value.weighing.individual");
  const typeLabel = (key: string) => copy(pageContract, `value.feed_band.type.${key}`, key);
  const bandLabel = (key: string) => copy(pageContract, `band.weight.${key}`, key);
  const bucketLabel = (key: string) => copy(pageContract, `value.feed_band.${key}`, key);
  // "104 sold · 3 died · 13 other": the three exit buckets, zero buckets left out.
  const exitBreakdown = (sold: number, died: number, other: number) =>
    [[sold, "sold"], [died, "died"], [other, "other"]]
      .filter(([count]) => (count as number) > 0)
      .map(([count, key]) => `${n(count as number)} ${bucketLabel(key as string)}`)
      .join(" · ");
  const n = (value: number) => value.toLocaleString("en-IN");

  // The Animals choice, applied over the ONE payload: under "on farm" a band row with no
  // on-farm animal disappears, and a pen whose rows all disappear files under Not shown; under
  // "include" those rows come back with the all-variant figures.
  const { matchedRows, notShownRows } = useMemo(() => {
    const visibleRows = includeExited ? allRows.filter((row) => row.weight_animals_all > 0) : allRows.filter((row) => row.weight_animals > 0);
    const hiddenPens = new Set<string>();
    const shownPens = new Set(visibleRows.map((row) => `${row.park_id}|${row.pen}|${row.shed_tag}|${row.feed_type}|${row.ration_group}|${row.experiment_arm ?? ""}|${row.breed}`));
    const extraNotShown: FeedWeightBandResponse["unmatched"] = [];
    for (const row of allRows) {
      const key = `${row.park_id}|${row.pen}|${row.shed_tag}|${row.feed_type}|${row.ration_group}|${row.experiment_arm ?? ""}|${row.breed}`;
      if (shownPens.has(key) || hiddenPens.has(key)) continue;
      hiddenPens.add(key);
      extraNotShown.push({
        park_id: row.park_id,
        park_name: row.park_name,
        pen: row.pen,
        shed_tag: row.shed_tag,
        group: row.group,
        ration_group: row.ration_group,
        experiment_arm: row.experiment_arm,
        breed: row.breed,
        feed_type: row.feed_type,
        feed_given: row.feed_given,
        pen_kg_per_day: row.pen_kg_per_day,
      });
    }
    const notShown = [...allUnmatched, ...extraNotShown].sort(
      (a, b) => a.park_name.localeCompare(b.park_name) || a.pen.localeCompare(b.pen, undefined, { numeric: true }) || a.shed_tag.localeCompare(b.shed_tag),
    );
    return { matchedRows: visibleRows, notShownRows: notShown };
  }, [allRows, allUnmatched, includeExited]);

  const search = state.search.trim().toLowerCase();
  const hasSearch = (parts: (string | number | null | undefined)[]) => search === "" || parts.join(" ").toLowerCase().includes(search);
  const matched = matchedRows.filter(
    (row) =>
      (state.type === "" || row.feed_type === state.type) &&
      (state.source === "" || row.weight_source === state.source) &&
      (state.band === "" || row.band === state.band) &&
      (state.pen === "" || row.pen === state.pen) &&
      (state.group === "" || row.group === state.group) &&
      hasSearch([row.pen, row.group, includeExited ? row.gender_all : row.gender, row.breed, row.feed_given, row.shed_tag, row.park_name]),
  );
  const unmatched = notShownRows.filter(
    (row) =>
      (state.type === "" || row.feed_type === state.type) &&
      (state.pen === "" || row.pen === state.pen) &&
      hasSearch([row.pen, row.group, row.shed_tag, row.ration_group, row.experiment_arm, row.breed, row.feed_given, row.park_name]),
  );
  const pageRows = view === "matched" ? matched : unmatched;
  const slice = pageRows.slice(state.offset, state.offset + state.limit);

  const pens = [...new Set((view === "matched" ? matchedRows : notShownRows).map((row) => row.pen))];
  const groups = [...new Set(matchedRows.map((row) => row.group))].sort((a, b) => a.localeCompare(b));
  const parkNames = [...new Set(allRows.map((row) => row.park_name).concat(allUnmatched.map((row) => row.park_name)))].sort();
  const hasTableFilter = Boolean(state.type || state.source || state.band || state.pen || state.group);

  // Stat tiles over the rows of THIS view after the table filters. "Animals weighed" counts each
  // pen × bracket × source once, however many feed rows the pen has.
  const tiles: { label: string; value: string; sub?: string; href?: string }[] = [];
  // A hash-only href keeps the page's path and query (so every filter survives the open) and
  // renders identically on the server, where there is no window.
  const exitHref = (scope: string) => `#fb_exit=${encodeURIComponent(scope)}`;
  if (view === "matched") {
    const perPenBand = new Map<string, { animals: number; exited: number; sold: number; died: number; other: number }>();
    for (const row of matched) {
      perPenBand.set(`${row.park_id}|${row.pen}|${row.band}|${row.weight_source}`, {
        animals: includeExited ? row.weight_animals_all : row.weight_animals,
        exited: row.exited_animals,
        sold: row.exited_sold,
        died: row.exited_died,
        other: row.exited_other,
      });
    }
    let animals = 0;
    let exited = 0;
    let sold = 0;
    let died = 0;
    let other = 0;
    for (const entry of perPenBand.values()) {
      animals += entry.animals;
      exited += entry.exited;
      sold += entry.sold;
      died += entry.died;
      other += entry.other;
    }
    tiles.push({ label: copy(pageContract, "stat.feed_band.rows"), value: n(matched.length) });
    for (const park of parkNames) tiles.push({ label: park, value: n(matched.filter((row) => row.park_name === park).length) });
    tiles.push(
      { label: copy(pageContract, "stat.feed_band.pens"), value: n(new Set(matched.map((row) => `${row.park_id}|${row.pen}`)).size) },
      { label: copy(pageContract, "stat.feed_band.lump"), value: n(matched.filter((row) => row.weight_source === "pen_average").length) },
      { label: copy(pageContract, "stat.feed_band.per_animal"), value: n(matched.filter((row) => row.weight_source === "per_animal").length) },
      { label: copy(pageContract, "stat.feed_band.animals"), value: n(animals) },
      { label: copy(pageContract, "stat.feed_band.exited"), value: n(exited), sub: exited > 0 ? exitBreakdown(sold, died, other) : undefined, href: exited > 0 ? exitHref("all") : undefined },
    );
  } else {
    tiles.push({ label: copy(pageContract, "stat.feed_band.rows"), value: n(unmatched.length) });
    for (const park of parkNames) tiles.push({ label: park, value: n(unmatched.filter((row) => row.park_name === park).length) });
    tiles.push({ label: copy(pageContract, "stat.feed_band.pens"), value: n(new Set(unmatched.map((row) => `${row.park_id}|${row.pen}`)).size) });
  }
  // At most eight tiles (visual judge 2026-09-18): the filtered "showing N of M" reads on the pager.

  // The exited panel's scopes: every exit in the period, and each matched pen × bracket
  // with exits behind it.
  const exitItems: FeedWeightBandExitItem[] = allExits.map((row, index) => ({
    key: `${row.tag}|${row.exited_at}|${index}`,
    park: row.park_name,
    pen: row.pen,
    tag: row.tag,
    gender: row.gender,
    reason: row.reason || row.lifecycle_status,
    bucket: row.bucket,
    bucketLabel: bucketLabel(row.bucket),
    exitedAt: row.exited_at,
    lastWeighedAt: row.last_weighed_at ?? "",
    lastBand: row.last_band ?? "",
    lastBandLabel: row.last_band ? bandLabel(row.last_band) : "",
    lastWeightKg: row.last_weight_kg ?? null,
    feedType: row.feed_type ?? "",
    feedTypeLabel: row.feed_type ? typeLabel(row.feed_type) : "",
    feedGiven: row.feed_given ?? "",
  }));
  const exitsByPen = [...exitItems].sort((a, b) => a.park.localeCompare(b.park) || a.pen.localeCompare(b.pen, undefined, { numeric: true }));
  const exitScopes: FeedWeightBandExitScope[] = [{ id: "all", title: copy(pageContract, "view.feed_band.exited"), items: exitsByPen }];
  const rowScopeId = (row: FeedWeightBandResponse["rows"][number]) => `${row.park_name}|${row.pen}|${row.band}`;
  for (const row of allRows) {
    if (row.exited_animals === 0) continue;
    const id = rowScopeId(row);
    if (exitScopes.some((scope) => scope.id === id)) continue;
    exitScopes.push({
      id,
      title: `${row.pen} · ${bandLabel(row.band)}`,
      items: exitItems.filter((item) => item.park === row.park_name && item.pen === row.pen && item.lastBand === row.band),
    });
  }

  const excludedCount = recon ? (includeExited ? recon.excluded_rollups_all : recon.excluded_rollups) : 0;
  // One park in the payload: the Park column would repeat one word on every row, so the table
  // contract handed down hides it (density, not vocabulary).
  const singlePark = parkNames.length <= 1;
  const withParkColumn = (contract: ReturnType<typeof table>) =>
    singlePark ? { ...contract, columns: contract.columns.map((column) => (column.key === "park" ? { ...column, visible: false } : column)) } : contract;
  const emptyLabel = (all: number, key: string) => (all === 0 ? copy(pageContract, key) : copy(pageContract, "empty.feed_band.filtered"));

  const selectField = (param: keyof State, label: string, value: string, options: { value: string; label: string }[]) => (
    <label key={param} style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12 }}>
      <span className="muted">{label}</span>
      <select className="tsize" value={value} onChange={(event) => update({ [param]: event.target.value } as Partial<State>)}>
        <option value="">{copy(pageContract, "filter.all_option")}</option>
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  );

  return (
    <section className="card wtable wt-feedband-card" aria-label={copy(pageContract, "section.feed_band.aria")}>
      <h2 className="h wt-feedband-head">
        <Wheat className="ic" size={15} aria-hidden /> {copy(pageContract, "section.feed_band.title")}
        <span className="wgl-hint">
          <span className="wgl-i" tabIndex={0} role="note" aria-label={copy(pageContract, "info.feed_band.hint")}>
            i
          </span>
          <span className="wgl-pop">
            <b>{copy(pageContract, "info.feed_band.title")}</b>
            <span className="wgl-pop-list">
              <span>{copy(pageContract, "info.feed_band.wt_n")}</span>
              <span>{copy(pageContract, "info.feed_band.kg_day")}</span>
              <span>{copy(pageContract, "info.feed_band.excluded")}</span>
              <span>{copy(pageContract, "info.feed_band.exited")}</span>
              <span>{copy(pageContract, "info.feed_band.badges")}</span>
            </span>
          </span>
        </span>
      </h2>
      <p className="muted small">{copy(pageContract, "section.feed_band.caption")}</p>
      {recon ? (
        <p className="muted small wt-feedband-recon">
          {recon.feed_day ? (
            <Tag tone="mut">
              {copy(pageContract, "recon.feed_band.sheet")} {fmtDate(recon.feed_day)}
            </Tag>
          ) : (
            copy(pageContract, "recon.feed_band.no_sheet")
          )}
          {recon.feed_day ? (
            <>
              {" "}
              <button type="button" className="wt-feedband-chip" onClick={() => update({ view: "unmatched", source: "", group: "", band: "" })}>
                {n(excludedCount)} {copy(pageContract, "recon.feed_band.excluded")}
              </button>{" "}
              <LocalOverlayLink href={exitHref("all")} className="wt-feedband-chip" scroll={false}>
                {n(recon.exited_animals)} {copy(pageContract, "recon.feed_band.exited")}
                {recon.exited_animals > 0 ? ` · ${exitBreakdown(recon.exited_sold, recon.exited_died, recon.exited_other)}` : ""}
              </LocalOverlayLink>
            </>
          ) : null}
        </p>
      ) : (
        <p className="muted small">{copy(pageContract, "error.load.body")}</p>
      )}
      <div className="wt-feedband-segments">
        <nav className="metricseg" aria-label={copy(pageContract, "view.feed_band.aria")}>
          <button type="button" className={view === "matched" ? "on" : ""} aria-current={view === "matched" ? "true" : undefined} onClick={() => update({ view: "matched" })}>
            {copy(pageContract, "view.feed_band.matched")} · {n(matchedRows.length)}
          </button>
          <button type="button" className={view === "unmatched" ? "on" : ""} aria-current={view === "unmatched" ? "true" : undefined} onClick={() => update({ view: "unmatched", source: "", group: "", band: "" })}>
            {copy(pageContract, "view.feed_band.unmatched")} · {n(notShownRows.length)}
          </button>
        </nav>
        {view === "matched" ? (
          <span className="wt-feedband-animals">
            <span className="muted small">{copy(pageContract, "filter.feed_band.animals")}</span>
            <nav className="metricseg" aria-label={copy(pageContract, "filter.feed_band.animals")}>
              <button type="button" className={includeExited ? "" : "on"} aria-current={includeExited ? undefined : "true"} data-testid="fb-animals-on-farm" onClick={() => update({ animals: "on_farm" })}>
                {copy(pageContract, "value.feed_band.animals.on_farm")}
              </button>
              <button type="button" className={includeExited ? "on" : ""} aria-current={includeExited ? "true" : undefined} data-testid="fb-animals-all" onClick={() => update({ animals: "all" })}>
                {copy(pageContract, "value.feed_band.animals.all")}
              </button>
            </nav>
          </span>
        ) : null}
      </div>
      <div className="tbar" style={{ display: "flex", alignItems: "center", gap: 10, padding: "12px 14px", flexWrap: "wrap" }} role="group" aria-label={copy(pageContract, "filter.feed_band.aria")}>
        {selectField("type", copy(pageContract, "filter.feed_band.feed_type"), state.type, ["normal", "experiment"].map((key) => ({ value: key, label: typeLabel(key) })))}
        {view === "matched"
          ? selectField("source", copy(pageContract, "filter.feed_band.weight_source"), state.source, ["pen_average", "per_animal"].map((key) => ({ value: key, label: sourceLabel(key) })))
          : null}
        {view === "matched" ? selectField("band", copy(pageContract, "filter.feed_band.band"), state.band, BAND_KEYS.map((key) => ({ value: key, label: bandLabel(key) }))) : null}
        {selectField("pen", copy(pageContract, "filter.feed_band.pen"), state.pen, pens.map((pen) => ({ value: pen, label: pen })))}
        {view === "matched" ? selectField("group", copy(pageContract, "filter.feed_band.group"), state.group, groups.map((group) => ({ value: group, label: group }))) : null}
        {hasTableFilter ? (
          <button type="button" className="btn sm" onClick={() => update({ type: "", source: "", band: "", pen: "", group: "" })}>
            {copy(pageContract, "filter.clear_all")}
          </button>
        ) : null}
        <label className="wt-feedband-search" style={{ display: "inline-flex", alignItems: "center", gap: 6, marginLeft: "auto" }}>
          <Search className="ic" style={{ width: 14, color: "var(--brand-d)" }} aria-hidden="true" />
          <input
            type="search"
            className="tsize"
            value={draftSearch}
            placeholder={copy(pageContract, "filter.feed_band.search")}
            aria-label={copy(pageContract, "filter.feed_band.search_aria")}
            onChange={(event) => setDraftSearch(event.target.value)}
            onBlur={() => update({ search: draftSearch.trim() })}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                update({ search: draftSearch.trim() });
              }
            }}
          />
        </label>
      </div>
      {view === "unmatched" ? <p className="muted small wt-feedband-note">{copy(pageContract, "note.feed_band.unmatched")}</p> : null}
      <div className="wt-feedband-stats" role="group" aria-label={copy(pageContract, "section.feed_band.aria")}>
        {tiles.map((tile) =>
          tile.href ? (
            <LocalOverlayLink className="kpi wt-feedband-tile-link" key={tile.label} href={tile.href} scroll={false}>
              <div className="lab">{tile.label}</div>
              <div className="val">
                {tile.value}
                {tile.sub ? <span className="muted small wt-feedband-tile-sub"> {tile.sub}</span> : null}
              </div>
            </LocalOverlayLink>
          ) : (
            <div className="kpi" key={tile.label}>
              <div className="lab">{tile.label}</div>
              <div className="val">
                {tile.value}
                {tile.sub ? <span className="muted small"> {tile.sub}</span> : null}
              </div>
            </div>
          ),
        )}
      </div>
      {view === "matched" ? (
        <FeedWeightBandTable
          contract={withParkColumn(table(pageContract, "feed-weight-band"))}
          rows={(slice as FeedWeightBandResponse["rows"]).map(
            (row, index): FeedWeightBandTableRow => ({
              key: `${row.park_id}|${row.pen}|${row.shed_tag}|${row.feed_type}|${row.ration_group}|${row.experiment_arm ?? ""}|${row.band}|${state.offset + index}`,
              park: row.park_name,
              weightSource: row.weight_source,
              weightSourceLabel: sourceLabel(row.weight_source),
              band: row.band,
              bandLabel: bandLabel(row.band),
              pen: row.pen,
              group: row.group,
              gender: includeExited ? row.gender_all : row.gender,
              breed: row.breed,
              feedType: row.feed_type,
              feedTypeLabel: typeLabel(row.feed_type),
              feedGiven: row.feed_given,
              penKgPerDay: row.pen_kg_per_day,
              weightAnimals: includeExited ? row.weight_animals_all : row.weight_animals,
              averageKg: includeExited ? row.average_weight_kg_all : row.average_weight_kg,
              exitedSold: row.exited_sold,
              exitedDied: row.exited_died,
              exitedOther: row.exited_other,
              includeExited,
              exitHref: row.exited_animals > 0 ? exitHref(rowScopeId(row)) : undefined,
            }),
          )}
          labels={{
            ariaLabel: copy(pageContract, "section.feed_band.aria"),
            noGender: copy(pageContract, "value.feed_band.no_gender"),
            sold: copy(pageContract, "value.feed_band.sold"),
            died: copy(pageContract, "value.feed_band.died"),
            other: copy(pageContract, "value.feed_band.other"),
            incl: copy(pageContract, "value.feed_band.incl"),
            empty: emptyLabel(matchedRows.length, "empty.feed_band.body"),
          }}
        />
      ) : (
        <FeedWeightBandUnmatchedTable
          contract={withParkColumn(table(pageContract, "feed-weight-band-unmatched"))}
          rows={(slice as FeedWeightBandResponse["unmatched"]).map(
            (row, index): FeedWeightBandUnmatchedRow => ({
              key: `${row.park_id}|${row.pen}|${row.shed_tag}|${row.feed_type}|${row.ration_group}|${row.experiment_arm ?? ""}|${state.offset + index}`,
              park: row.park_name,
              pen: row.pen,
              shedTag: row.shed_tag,
              ration: row.ration_group || row.experiment_arm || "",
              breed: row.breed,
              feedType: row.feed_type,
              feedTypeLabel: typeLabel(row.feed_type),
              feedGiven: row.feed_given,
              penKgPerDay: row.pen_kg_per_day,
            }),
          )}
          labels={{
            ariaLabel: copy(pageContract, "view.feed_band.unmatched"),
            empty: emptyLabel(notShownRows.length, "empty.feed_band.unmatched"),
          }}
        />
      )}
      {/* The shared pager shape (.pager2), as buttons: paging is a slice of the payload. */}
      <div className="pager2">
        <span className="small muted" style={{ marginRight: "auto" }}>
          {slice.length === 0
            ? `0 ${copy(pageContract, "pager.feed_band.noun")}s`
            : `${n(state.offset + 1)}-${n(state.offset + slice.length)} ${copy(pageContract, "pager.feed_band.noun")}${slice.length === 1 ? "" : "s"} · ${copy(pageContract, "pager.page")} ${Math.floor(state.offset / state.limit) + 1}`}
        </span>
        <label style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12 }}>
          <span className="muted">{copy(pageContract, "pager.rows")}</span>
          {PAGE_SIZES.map((size) => (
            <button key={size} type="button" className={size === state.limit ? "btn sm p" : "btn sm"} aria-current={size === state.limit ? "true" : undefined} onClick={() => update({ limit: size })}>
              {String(size)}
            </button>
          ))}
        </label>
        <button type="button" className="btn sm" disabled={state.offset === 0} aria-disabled={state.offset === 0} onClick={() => setState((prev) => ({ ...prev, offset: Math.max(0, prev.offset - prev.limit) }))}>
          {copy(pageContract, "action.previous")}
        </button>
        <button type="button" className="btn sm" disabled={state.offset + slice.length >= pageRows.length} aria-disabled={state.offset + slice.length >= pageRows.length} onClick={() => setState((prev) => ({ ...prev, offset: prev.offset + prev.limit }))}>
          {copy(pageContract, "action.next")}
        </button>
      </div>
      <FeedWeightBandExitsDrawer
        scopes={exitScopes}
        initialSelectedId={initial.exitScope}
        closeHref={typeof window === "undefined" ? "" : window.location.pathname + window.location.search}
        contract={table(pageContract, "feed-weight-band-exits")}
        periodLabel={periodLabel}
        labels={{
          aria: copy(pageContract, "drawer.feed_band.aria"),
          eyebrow: copy(pageContract, "drawer.feed_band.eyebrow"),
          close: copy(pageContract, "drawer.feed_band.close"),
          period: copy(pageContract, "drawer.feed_band.period"),
          search: copy(pageContract, "drawer.feed_band.search"),
          searchAria: copy(pageContract, "drawer.feed_band.search_aria"),
          never: copy(pageContract, "value.feed_band.exits.never"),
          noFeed: copy(pageContract, "value.feed_band.no_feed"),
          noGender: copy(pageContract, "value.feed_band.no_gender"),
          empty: copy(pageContract, "empty.feed_band.exits"),
          animals: copy(pageContract, "drawer.feed_band.animals"),
          animal: copy(pageContract, "drawer.feed_band.animal"),
        }}
      />
    </section>
  );
}
