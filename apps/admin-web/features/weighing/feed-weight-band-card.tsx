"use client";

import { SearchTextField } from "@/components/minimal/list/search-text-field";
import { useEffect, useMemo, useState } from "react";

import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import IconButton from "@mui/material/IconButton";
import Link from "@mui/material/Link";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { TableFooter } from "@/components/app/table-footer";
import { KpiGrid } from "@/components/app/kpi-grid";
import { KpiWidget } from "@/components/app/kpi-widget";
import { AnimatedTabs } from "@/components/minimal/list/animated-tabs";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { copy, table, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { FeedWeightBandResponse } from "@/lib/api/server";
import { fmtDate } from "@/lib/format";
import Box from "@mui/material/Box";
import Tooltip from "@mui/material/Tooltip";
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
  // The bands are the tenant's weight_band_edges_kg assumption (maintainer decision 2026-09-19):
  // the response carries the vocabulary and the farm label for every bracket, so the filter and
  // the labels never name a bracket the rows were not banded into. The page copy is only the
  // fallback for a row served without a label.
  const bandOptions = useMemo(() => feedBand?.bands ?? [], [feedBand]);
  const bandLabel = (key: string) => bandOptions.find((band) => band.key === key)?.label ?? copy(pageContract, `band.weight.${key}`, key);
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
  const tiles: { label: string; value: number; sub?: string; href?: string }[] = [];
  // A hash-only href keeps the page's path and query (so every filter survives the open) and
  // renders identically on the server, where there is no window.
  const exitHref = (scope: string) => `#fb_exit=${encodeURIComponent(scope)}`;
  if (view === "matched") {
    const perPenBand = new Map<string, number>();
    for (const row of matched) {
      perPenBand.set(`${row.park_id}|${row.pen}|${row.band}|${row.weight_source}`, includeExited ? row.weight_animals_all : row.weight_animals);
    }
    let animals = 0;
    for (const entry of perPenBand.values()) animals += entry;
    const totalWeighed = recon ? recon.individual_animals_weighed + recon.lump_sum_animals_weighed : 0;
    const animalsSub = totalWeighed > 0 ? `${n(totalWeighed)} ${copy(pageContract, "stat.feed_band.total_weighed")}` : undefined;
    tiles.push({ label: copy(pageContract, "stat.feed_band.rows"), value: (matched.length) });
    for (const park of parkNames) tiles.push({ label: park, value: (matched.filter((row) => row.park_name === park).length) });
    tiles.push(
      { label: copy(pageContract, "stat.feed_band.pens"), value: (new Set(matched.map((row) => `${row.park_id}|${row.pen}`)).size) },
      { label: copy(pageContract, "stat.feed_band.lump"), value: (matched.filter((row) => row.weight_source === "pen_average").length) },
      { label: copy(pageContract, "stat.feed_band.per_animal"), value: (matched.filter((row) => row.weight_source === "per_animal").length) },
      { label: copy(pageContract, "stat.feed_band.animals"), value: (animals), sub: animalsSub },
      // The period's exits, the Herd Analytics figure (recon), with the weighed / not-weighed
      // split; the band rows' own exit notes are the weighed subset of this.
      {
        label: copy(pageContract, "stat.feed_band.exited"),
        value: (recon?.exited_animals ?? 0),
        sub: recon && recon.exited_animals > 0 ? `${n(recon.exited_weighed)} ${copy(pageContract, "stat.feed_band.weighed")} · ${n(recon.exited_not_weighed)} ${copy(pageContract, "stat.feed_band.not_weighed")}` : undefined,
        href: recon && recon.exited_animals > 0 ? exitHref("all") : undefined,
      },
    );
  } else {
    tiles.push({ label: copy(pageContract, "stat.feed_band.rows"), value: (unmatched.length) });
    for (const park of parkNames) tiles.push({ label: park, value: (unmatched.filter((row) => row.park_name === park).length) });
    tiles.push({ label: copy(pageContract, "stat.feed_band.pens"), value: (new Set(unmatched.map((row) => `${row.park_id}|${row.pen}`)).size) });
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
    weighed: row.weighed_in_period,
    exitedAt: row.exited_at,
    lastWeighedAt: row.last_weighed_at ?? "",
    lastBand: row.last_band ?? "",
    lastBandLabel: row.last_band ? (row.last_band_label || bandLabel(row.last_band)) : "",
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
      title: `${row.pen} · ${row.band_label || bandLabel(row.band)}`,
      items: exitItems.filter((item) => item.park === row.park_name && item.pen === row.pen && item.lastBand === row.band),
    });
  }

  // "· plan 19/09/2026" beside the title: one date when every park's plan is the same day,
  // otherwise each park's own day (each park reads its own latest plan), with the workflow
  // named when a park's normal and experiment plans differ. Every plan is on the tooltip.
  const planLine = (() => {
    if (!recon?.feed_day) return copy(pageContract, "recon.feed_band.no_sheet");
    const sheets = recon.feed_sheets;
    const days = new Set(sheets.map((sheet) => sheet.feed_day));
    if (days.size <= 1) return `${copy(pageContract, "recon.feed_band.sheet")} ${fmtDate(recon.feed_day)}`;
    const perPark = new Map<string, Set<string>>();
    for (const sheet of sheets) {
      if (!perPark.has(sheet.park_name)) perPark.set(sheet.park_name, new Set());
      perPark.get(sheet.park_name)!.add(sheet.feed_day);
    }
    const parts = sheets
      .filter((sheet, index) => perPark.get(sheet.park_name)!.size > 1 || sheets.findIndex((other) => other.park_name === sheet.park_name) === index)
      .map((sheet) => (perPark.get(sheet.park_name)!.size > 1 ? `${sheet.park_name} ${typeLabel(sheet.workflow)} ${fmtDate(sheet.feed_day)}` : `${sheet.park_name} ${fmtDate(sheet.feed_day)}`));
    return `${copy(pageContract, "recon.feed_band.sheets")} · ${parts.join(" · ")}`;
  })();
  const planTitle = recon ? recon.feed_sheets.map((sheet) => `${sheet.park_name} · ${typeLabel(sheet.workflow)} · ${fmtDate(sheet.feed_day)}`).join(" · ") : "";
  // "56 sold · 3 died · 6 other — 10 no weighing in period": the exit chip's tooltip and the
  // panel's header line.
  const exitDetail =
    recon && recon.exited_animals > 0
      ? `${exitBreakdown(recon.exited_sold, recon.exited_died, recon.exited_other)} — ${n(recon.exited_not_weighed)} ${copy(pageContract, "stat.feed_band.not_weighed")}`
      : "";
  // One park in the payload: the Park column would repeat one word on every row, so the table
  // contract handed down hides it (density, not vocabulary).
  const singlePark = parkNames.length <= 1;
  const withParkColumn = (contract: ReturnType<typeof table>) =>
    singlePark ? { ...contract, columns: contract.columns.map((column) => (column.key === "park" ? { ...column, visible: false } : column)) } : contract;
  const emptyLabel = (all: number, key: string) => (all === 0 ? copy(pageContract, key) : copy(pageContract, "empty.feed_band.filtered"));

  // Kit listbox, not a native <select>: the "" option is the contract's All, same apply-on-change.
  const selectField = (param: keyof State, label: string, value: string, options: { value: string; label: string }[]) => (
    <TextField
      key={param}
      select
      label={label}
      value={options.some((option) => option.value === value) ? value : ""}
      onChange={({ target: { value: next } }) => update({ [param]: next } as Partial<State>)}
      sx={{ minWidth: { xs: 0, sm: 140 }, flexShrink: 0, maxWidth: 1 }}
      slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
    >
      <MenuItem value="">{copy(pageContract, "filter.all_option")}</MenuItem>
      {options.map((option) => (
        <MenuItem key={option.value} value={option.value}>
          {option.label}
        </MenuItem>
      ))}
    </TextField>
  );
  const pageNumber = Math.floor(state.offset / state.limit) + 1;

  return (
    <Card aria-label={copy(pageContract, "section.feed_band.aria")}>
      <CardHeader
        title={
          <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.5 }}>
            {copy(pageContract, "section.feed_band.title")}
            <Box component="span">
              {/* The template Tooltip (hover, focus, tap), portalled so the card cannot clip it. */}
              <Tooltip
                enterTouchDelay={0}
                leaveTouchDelay={8000}
                slotProps={{ tooltip: { sx: { maxWidth: 360 } } }}
                title={
              <Box component="span" sx={{ display: "flex", flexDirection: "column", gap: 0.75, maxHeight: 320, overflowY: "auto", textAlign: "left" }}>
                <Box component="b" sx={{ typography: "subtitle2" }}>{copy(pageContract, "info.feed_band.title")}</Box>
                <Box component="span" sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
                  {recon ? (
                    <span title={planTitle}>
                      <b>{planLine}</b>
                      {recon.exited_animals > 0 ? ` · ${n(recon.exited_animals)} ${copy(pageContract, "stat.feed_band.exited").toLowerCase()}: ${exitDetail}` : ""}
                    </span>
                  ) : null}
                  <span>{copy(pageContract, "info.feed_band.caption")}</span>
                  <span>{copy(pageContract, "info.feed_band.plan")}</span>
                  <span>{copy(pageContract, "info.feed_band.rules")}</span>
                  <span>{copy(pageContract, "info.feed_band.wt_n")}</span>
                  <span>{copy(pageContract, "info.feed_band.kg_day")}</span>
                  <span>{copy(pageContract, "info.feed_band.excluded")}</span>
                  <span>{copy(pageContract, "info.feed_band.exited")}</span>
                  <span>{copy(pageContract, "info.feed_band.badges")}</span>
                </Box>
              </Box>
                }
              >
                <IconButton size="small" role="note" aria-label={copy(pageContract, "info.feed_band.hint")}>
                  <Iconify icon="eva:info-outline" width={20} />
                </IconButton>
              </Tooltip>
            </Box>
          </Box>
        }
        action={
          recon ? null : (
            <Typography variant="body2" sx={{ color: "text.secondary" }}>
              {copy(pageContract, "error.load.body")}
            </Typography>
          )
        }
        sx={{ mb: 2 }}
      />
      <Box sx={{ px: 2.5, pb: 1, display: "flex", flexWrap: "wrap", alignItems: "center", gap: 2 }}>
        <AnimatedTabs
          variant="pill"
          ariaLabel={copy(pageContract, "view.feed_band.aria")}
          value={view}
          onChange={(next) => (next === "unmatched" ? update({ view: "unmatched", source: "", group: "", band: "" }) : update({ view: "matched" }))}
          items={[
            { value: "matched", label: copy(pageContract, "view.feed_band.matched"), count: n(matchedRows.length) },
            { value: "unmatched", label: copy(pageContract, "view.feed_band.unmatched"), count: n(notShownRows.length) },
          ]}
        />
        {view === "matched" ? (
          <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1 }}>
            <Typography component="span" variant="body2" sx={{ color: "text.secondary" }}>
              {copy(pageContract, "filter.feed_band.animals")}
            </Typography>
            <AnimatedTabs
              variant="pill"
              ariaLabel={copy(pageContract, "filter.feed_band.animals")}
              value={includeExited ? "all" : "on_farm"}
              onChange={(next) => update({ animals: next === "all" ? "all" : "on_farm" })}
              items={[
                { value: "on_farm", label: copy(pageContract, "value.feed_band.animals.on_farm") },
                { value: "all", label: copy(pageContract, "value.feed_band.animals.all") },
              ]}
            />
          </Box>
        ) : null}
      </Box>
      <Box
        role="group"
        aria-label={copy(pageContract, "filter.feed_band.aria")}
        sx={{ p: 2.5, display: "flex", flexWrap: "wrap", alignItems: "center", gap: 2 }}
      >
        {selectField("type", copy(pageContract, "filter.feed_band.feed_type"), state.type, ["normal", "experiment"].map((key) => ({ value: key, label: typeLabel(key) })))}
        {view === "matched"
          ? selectField("source", copy(pageContract, "filter.feed_band.weight_source"), state.source, ["pen_average", "per_animal"].map((key) => ({ value: key, label: sourceLabel(key) })))
          : null}
        {view === "matched" ? selectField("band", copy(pageContract, "filter.feed_band.band"), state.band, bandOptions.map((band) => ({ value: band.key, label: band.label }))) : null}
        {selectField("pen", copy(pageContract, "filter.feed_band.pen"), state.pen, pens.map((pen) => ({ value: pen, label: pen })))}
        {view === "matched" ? selectField("group", copy(pageContract, "filter.feed_band.group"), state.group, groups.map((group) => ({ value: group, label: group }))) : null}
        {hasTableFilter ? (
          <Button variant="soft" color="inherit" size="small" onClick={() => update({ type: "", source: "", band: "", pen: "", group: "" })}>
            {copy(pageContract, "filter.clear_all")}
          </Button>
        ) : null}
        <SearchTextField
          value={draftSearch}
          placeholder={copy(pageContract, "filter.feed_band.search")}
          ariaLabel={copy(pageContract, "filter.feed_band.search_aria")}
          onChange={setDraftSearch}
          onBlur={() => update({ search: draftSearch.trim() })}
          onEnter={() => update({ search: draftSearch.trim() })}
          sx={{ flex: "1 1 220px", width: "auto", minWidth: 0 }}
        />
      </Box>
      {view === "unmatched" ? (
        <Typography variant="body2" sx={{ px: 2.5, pb: 2, color: "text.secondary" }}>
          {copy(pageContract, "note.feed_band.unmatched")}
        </Typography>
      ) : null}
      {/* Template CourseWidgetSummary tiles (KpiWidget), flat inside this card. The exits figure
          opens the exited-animals drawer in place (hash link via LocalOverlayLink, no navigation). */}
      <Box component="section" aria-label={copy(pageContract, "section.feed_band.aria")} sx={{ px: 2.5, pb: 2.5 }}>
        <KpiGrid>
          {tiles.map((tile) => (
            <KpiWidget
              key={tile.label}
              title={tile.label}
              total={tile.value}
              caption={tile.sub}
              color={tile.href ? "warning" : "primary"}
              href={tile.href}
              linkComponent={LocalOverlayLink}
              sx={{ height: 1, boxShadow: "none", border: 1, borderColor: "divider" }}
            />
          ))}
        </KpiGrid>
      </Box>
      {view === "matched" ? (
        <FeedWeightBandTable
          contract={withParkColumn(table(pageContract, "feed-weight-band"))}
          rows={(slice as FeedWeightBandResponse["rows"]).map(
            (row, index): FeedWeightBandTableRow => ({
              key: `${row.park_id}|${row.pen}|${row.shed_tag}|${row.feed_type}|${row.ration_group}|${row.experiment_arm ?? ""}|${row.band}|${state.offset + index}`,
              park: row.park_name,
              weightSource: row.weight_source,
              weightSourceLabel: sourceLabel(row.weight_source),
              weightSourceShort: copy(pageContract, `value.feed_band.source.short.${row.weight_source}`, sourceLabel(row.weight_source)),
              band: row.band,
              bandLabel: row.band_label || bandLabel(row.band),
              pen: row.pen,
              group: row.group,
              gender: includeExited ? row.gender_all : row.gender,
              breed: row.breed,
              feedType: row.feed_type,
              feedTypeLabel: typeLabel(row.feed_type),
              feedTypeShort: copy(pageContract, `value.feed_band.type.short.${row.feed_type}`, typeLabel(row.feed_type)),
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
      {/* Kit footer: rows-per-page, the visible range, prev/next. Paging is a slice of the payload. */}
      <TableFooter
        page={pageNumber}
        rowsPerPage={state.limit}
        total={pageRows.length}
        rowsPerPageOptions={[...PAGE_SIZES]}
        onPageChange={(next) => setState((prev) => ({ ...prev, offset: (next - 1) * prev.limit }))}
        onRowsPerPageChange={(size) => update({ limit: size })}
        left={
          <Typography component="span" variant="body2" sx={{ color: "text.secondary" }}>
            {`${n(pageRows.length)} ${copy(pageContract, "pager.feed_band.noun")}${pageRows.length === 1 ? "" : "s"}`}
          </Typography>
        }
      />
      <FeedWeightBandExitsDrawer
        scopes={exitScopes}
        initialSelectedId={initial.exitScope}
        closeHref={typeof window === "undefined" ? "" : window.location.pathname + window.location.search}
        contract={table(pageContract, "feed-weight-band-exits")}
        periodLabel={periodLabel}
        labels={{
          aria: copy(pageContract, "drawer.feed_band.aria"),
          eyebrow: copy(pageContract, "drawer.feed_band.eyebrow"),
          detail: exitDetail,
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
          notWeighed: copy(pageContract, "drawer.feed_band.not_weighed"),
        }}
      />
    </Card>
  );
}
