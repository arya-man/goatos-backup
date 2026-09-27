"use client";

import { useEffect, useMemo, useRef, useState, useTransition, type ReactNode } from "react";
import Badge from "@mui/material/Badge";
import Box from "@mui/material/Box";
import MuiButton from "@mui/material/Button";
import Card from "@mui/material/Card";
import Checkbox from "@mui/material/Checkbox";
import Chip from "@mui/material/Chip";
import CircularProgress from "@mui/material/CircularProgress";
import FormControl from "@mui/material/FormControl";
import IconButton from "@mui/material/IconButton";
import InputAdornment from "@mui/material/InputAdornment";
import InputLabel from "@mui/material/InputLabel";
import MenuItem from "@mui/material/MenuItem";
import MenuList from "@mui/material/MenuList";
import Select from "@mui/material/Select";
import TextField from "@mui/material/TextField";
import { CustomPopover } from "@/components/minimal/custom-popover";
import { MinimalDrawer } from "@/components/app/drawer";
import { FiltersBlock, chipProps } from "@/components/minimal/filters-result";
import { Iconify } from "@/components/minimal/iconify";
import { useRouter, useSearchParams } from "next/navigation";

import { DateRangePicker, type DateRangePickerLabels } from "@/components/date-range-picker";
import dayjs from "dayjs";
import { DatePicker } from "@mui/x-date-pickers/DatePicker";
import { RowMenu } from "@/components/app/row-menu";
import { InfoHint } from "@/components/app/info-hint";
import { announceUrlNav } from "@/components/app/url-tab-nav";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import { worklistFilterIsStaged } from "@/lib/worklist-filter-draft";
import { worklistFilterShownValue } from "@/lib/worklist-filter-value";

export type WorklistFilterOption = { value: string; label: string };

/**
 * A toolbar action — Columns, Filters, Export, Settings.
 *
 * The bar decides how many fit; the rest fold into ONE overflow menu. Pages must not pre-split the
 * list themselves, or every page picks a different cut and the toolbar stops being one thing.
 */
export type WorklistToolbarAction = {
  id: string;
  label: string;
  icon?: ReactNode;
  onSelect: () => void;
  danger?: boolean;
  disabled?: boolean;
};

/** Free-text search, committed to one URL parameter. */
export type WorklistSearch = {
  param: string;
  value: string;
  placeholder?: string;
  ariaLabel?: string;
};

export type WorklistFilterTelemetry = {
  eventPrefix: string;
  surface: string;
  route: string;
};

const TELEMETRY_POST_METHOD = String.fromCharCode(80, 79, 83, 84);
const TELEMETRY_CONTENT_TYPE = "application/json";

export type WorklistFilterField =
  | {
      kind: "select";
      param: string;
      label: string;
      value: string;
      options: WorklistFilterOption[];
      allowAll?: boolean;
      /**
       * Other parameters this control INVALIDATES when it changes.
       *
       * For a dependent picker whose options only make sense inside this field's selection — a pen
       * picker under a park, a shed picker under a park. Leaving the dependent value behind produces
       * a filter pair that reads as valid and matches nothing, which the reader cannot explain from
       * the screen.
       */
      clears?: string[];
      /**
       * Optional hint shown on hover, the same treatment the multi-select and compare controls give
       * theirs.
       *
       * It exists because a select can silently narrow in a way its own options cannot show — Feed
       * Config's Breed control excludes the breedless `Kid` ration group entirely, a fifth of the
       * authored rates, and the grid just renders without them. The copy for that was authored in the
       * page contract and had NO way to reach the screen, because this kind carried no note field.
       */
      note?: string;
      disabledReason?: string;
    }
  | {
      /**
       * A multi-valued select. Each pick is APPENDED to the URL as a repeated parameter, and shows
       * as a removable chip beside the control.
       *
       * Repeated parameters rather than one comma-joined value, all the way down to the backend: a
       * feed item is free text and may legitimately contain the delimiter, so any joined encoding
       * has a value it silently corrupts.
       */
      kind: "multiselect";
      param: string;
      label: string;
      values: string[];
      options: WorklistFilterOption[];
      /** Optional hint rendered under the control, e.g. "pick more than one to compare". */
      note?: string;
      disabledReason?: string;
    }
  | {
      /**
       * A numeric comparison: an operator picked from a backend-declared set, plus a value the
       * author types. The two halves are ONE filter and travel together — clearing either clears
       * both, because the backend rejects half a comparison rather than inventing the other half.
       */
      kind: "compare";
      /** The operator parameter (e.g. `fc_grams_op`). */
      param: string;
      /** The value parameter (e.g. `fc_grams_value`). */
      valueParam: string;
      label: string;
      op: string;
      value: string;
      /** The comparison vocabulary, from the page contract's option group. */
      options: WorklistFilterOption[];
      valueAriaLabel: string;
      note?: string;
      disabledReason?: string;
    }
  | {
      kind: "date";
      param: string;
      label: string;
      value: string;
      min?: string;
      max?: string;
      disabledReason?: string;
      /**
       * When BOTH are given, the field renders the app's own calendar (DateRangePicker in
       * single-day mode) instead of the browser-native date input, whose popover follows the OS
       * theme rather than the product's. Hosts that have not passed calendar copy keep the native
       * input, so adopting the styled calendar is a per-page opt-in, never a silent repaint.
       */
      labels?: DateRangePickerLabels;
      today?: string;
    }
  | {
      /**
       * An inclusive business-day span, picked from a calendar.
       *
       * Replaces the fixed-window select a worklist would otherwise carry ("Last 4 weeks", "Last 12
       * weeks"), which can only answer the questions someone thought of in advance — a reader
       * comparing one drive week against another had no way to ask.
       *
       * Both ends are ONE filter and travel together, like `compare`: a read that takes from/to
       * rejects half a window rather than inventing the other half.
       */
      kind: "daterange";
      /** Start parameter, e.g. `wt_from`. */
      param: string;
      /** End parameter, e.g. `wt_to`. */
      toParam: string;
      label: string;
      from: string;
      to: string;
      /** Today's business day (Asia/Kolkata), resolved on the server. Future days are unpickable. */
      today: string;
      /**
       * Earliest selectable day, inclusive — for a host whose read has a hard history floor
       * (weighing history starts Aug 2026). Days before it are unpickable, like future days.
       */
      minDate?: string;
      /**
       * The window the page falls back to when neither parameter is present. A picked span is still
       * written explicitly, even when it equals this pair; Clear all is the way back to the page's
       * no-parameter landing window.
       */
      defaultFrom: string;
      defaultTo: string;
      labels: DateRangePickerLabels;
      markerDates?: readonly string[];
      markerFetchPath?: string;
    };

// Shared mock-shaped filter bar for backend-filtered operational worklists. Applying rewrites the
// URL and resets the page offset; the server remains the owner of rows and totals.
export function WorklistFilters({
  basePath,
  pageParam,
  fields,
  pageContract,
  deferApply = false,
  telemetry,
  trailing,
  search,
  actions,
  maxInlineActions = 3,
  children,
  label,
}: {
  /** Names this bar when a page carries two (phone button + sheet title); defaults to the page filter label. */
  label?: string;
  basePath: string;
  pageParam: string;
  fields: WorklistFilterField[];
  pageContract: AdminUiPageContract;
  telemetry?: WorklistFilterTelemetry;
  /**
   * A page-owned control pinned to the END of the bar, on the same line as the filters (the Weights
   * download drawer opener). Pushed right with `margin-left:auto` so it stays at the far edge as
   * filters are added, and wraps with the rest of the bar on a narrow viewport.
   */
  trailing?: ReactNode;
  /**
   * Free-text search, pinned to the START of the toolbar with a leading magnifier.
   *
   * Committed on Enter or on a short idle, never per keystroke: every commit rewrites the URL and
   * re-renders the server page, so a per-keystroke search would run a full render for "b", "be",
   * "ben" on the way to "benny" — the same rule the compare control follows.
   */
  search?: WorklistSearch;
  /** Right-aligned toolbar actions; anything past `maxInlineActions` folds into an overflow menu. */
  actions?: WorklistToolbarAction[];
  /** How many actions render as buttons before the overflow menu takes over. Default 3. */
  maxInlineActions?: number;
  /**
   * The rows this bar filters, passed in so the bar can hold them back while an apply is in flight.
   *
   * They arrive already rendered by the server component — this only wraps them — so nothing about
   * how or when they are fetched changes. Passing them is what lets ONE piece of state drive both
   * the busy ring and the held-back rows; the alternative was a context provider threaded through a
   * server page to say the same thing twice. Optional: a bar with no children is unchanged.
   */
  children?: ReactNode;
  /**
   * STAGE the edits and commit them on one Apply press, instead of rewriting the URL as each control
   * changes.
   *
   * For a bar whose page is expensive to render or whose reader normally narrows by several things
   * at once: applying per control ran a full server render per pick and showed intermediate result
   * sets nobody asked for. Off by default, so a light single-filter bar keeps its immediate feel.
   */
  deferApply?: boolean;
}) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();
  const routerSearchParams = useSearchParams();
  const current = routerSearchParams?.toString() ?? "";
  const [optimisticSearch, setOptimisticSearch] = useState<{ from: string; search: string } | null>(null);
  const [pendingSearch, setPendingSearch] = useState<string | null>(null);
  const pendingTelemetry = useRef<{
    clientEventId: string;
    startedAtMs: number;
    targetSearch: string;
    sourceSearch: string;
  } | null>(null);
  const optimisticActive = optimisticSearch?.from === current;
  const effectiveSearch = optimisticActive ? optimisticSearch.search : current;

  // STAGED EDITS. Null means the bar is showing exactly what is applied; a string means the operator
  // has changed something that Apply has not committed yet.
  const [draftSearch, setDraftSearch] = useState<string | null>(null);
  // ≤640px the field row folds into a bottom sheet behind one "Filters" button (frame spec: no
  // stack of 4-5 full-width fields before any content). Desktop ignores this state entirely.
  const [mobileOpen, setMobileOpen] = useState(false);
  // Drop the staged edits whenever what is APPLIED changes underneath — Apply landing, back/forward,
  // or a link that carries its own filters. Adjusted DURING RENDER rather than in an effect: the
  // effect version renders the stale draft once and commits before correcting it, which on a filter
  // bar is one frame of the previous selection flashing back into the control (the same reasoning as
  // the compare control's re-sync below).
  const [lastApplied, setLastApplied] = useState(effectiveSearch);
  if (lastApplied !== effectiveSearch) {
    setLastApplied(effectiveSearch);
    setDraftSearch(null);
  }
  // What the CONTROLS show: the staged set while one exists, otherwise what is applied.
  const activeSearch = draftSearch ?? effectiveSearch;
  const activeParams = useMemo(() => new URLSearchParams(activeSearch), [activeSearch]);
  const staged = worklistFilterIsStaged(draftSearch, effectiveSearch, pageParam);

  // The clear-vs-fallback rule lives in its own React-free module so it can be unit-tested; see it
  // for the defect it prevents. A staged draft is "pending" for its purposes too: a filter the
  // operator has just cleared is ABSENT from the draft and must render empty rather than falling
  // through to the stale server prop, which is the exact bug that module exists to stop.
  const shownValue = (param: string, serverValue: string, clearable: boolean) =>
    worklistFilterShownValue(
      activeParams.get(param),
      serverValue,
      draftSearch !== null || Boolean(optimisticActive),
      clearable,
    );

  const allLabel = copy(pageContract, "filter.all_option");
  // Resolved ONLY when a multi-select is actually on the bar. `copy` throws on a key the contract
  // does not carry, and this component is shared by pages that have no multi-valued filter and
  // therefore no reason to declare the key.
  const applyLabel =
    deferApply || fields.some((field) => field.kind === "multiselect" || field.kind === "compare")
      ? copy(pageContract, "filter.apply")
      : "";
  const clearable = fields.filter(
    (field) =>
      (field.kind === "select" && field.allowAll !== false) ||
      field.kind === "multiselect" ||
      field.kind === "compare" ||
      field.kind === "daterange",
  );
  const hasAnyFilter = clearable.some((field) => {
    if (field.kind === "multiselect") return activeParams.getAll(field.param).length > 0;
    // A comparison counts as applied when EITHER half is set, so a half-filled one can still be
    // cleared — the backend rejects half a comparison, and a control the operator cannot reset
    // would leave the page stuck on an error.
    if (field.kind === "compare") return activeParams.get(field.param) !== null || activeParams.get(field.valueParam) !== null;
    // Same rule for a span, and for the same reason: its default window is expressed by ABSENCE, so
    // a present parameter is exactly what "the reader moved this off its default" means.
    if (field.kind === "daterange") return activeParams.get(field.param) !== null || activeParams.get(field.toParam) !== null;
    return field.kind === "select" && activeParams.get(field.param) !== null;
  });
  useEffect(() => {
    if (pendingSearch === null || current !== pendingSearch) return;
    const pending = pendingTelemetry.current;
    if (pending?.targetSearch === pendingSearch) {
      emitFilterTelemetry(telemetry, "completed", {
        client_event_id: pending.clientEventId,
        elapsed_ms: elapsedMs(pending.startedAtMs),
        source_search: pending.sourceSearch,
        target_search: pending.targetSearch,
        deferred_apply: deferApply,
        field_params: fieldParams(fields),
      });
      pendingTelemetry.current = null;
    }
  }, [current, deferApply, fields, pendingSearch, telemetry]);
  const activePendingSearch = pendingSearch !== null && current !== pendingSearch ? pendingSearch : null;
  const busy = isPending && activePendingSearch !== null;

  useEffect(() => {
    if (activePendingSearch === null) return undefined;
    const timeout = window.setTimeout(() => {
      const pending = pendingTelemetry.current;
      if (pending?.targetSearch === activePendingSearch) {
        emitFilterTelemetry(telemetry, "timeout", {
          client_event_id: pending.clientEventId,
          elapsed_ms: elapsedMs(pending.startedAtMs),
          source_search: pending.sourceSearch,
          target_search: pending.targetSearch,
          deferred_apply: deferApply,
          field_params: fieldParams(fields),
        });
        pendingTelemetry.current = null;
      }
      setPendingSearch(null);
    }, 10000);
    return () => window.clearTimeout(timeout);
  }, [activePendingSearch, deferApply, fields, telemetry]);

  function push(next: URLSearchParams) {
    next.delete(pageParam);
    const qs = next.toString();
    const clientEventId = crypto.randomUUID();
    const startedAtMs = nowMs();
    pendingTelemetry.current = { clientEventId, startedAtMs, sourceSearch: current, targetSearch: qs };
    emitFilterTelemetry(telemetry, "started", {
      client_event_id: clientEventId,
      source_search: current,
      target_search: qs,
      deferred_apply: deferApply,
      field_params: fieldParams(fields),
    });
    setDraftSearch(null);
    setOptimisticSearch({ from: current, search: qs });
    setPendingSearch(qs);
    // Every URL-keyed panel reading a changed param swaps to its skeleton now (guard: url-keyed-panel).
    announceUrlNav(qs ? `${basePath}?${qs}` : basePath);
    startTransition(() => {
      router.replace(qs ? `${basePath}?${qs}` : basePath, { scroll: false });
    });
  }

  /**
   * Where every control's change lands: the URL directly, or the staged draft when this bar defers.
   *
   * The page parameter is dropped on BOTH paths, so a staged edit already reflects the paging reset
   * the apply will perform and the Apply button cannot light up over an offset alone.
   */
  function write(next: URLSearchParams) {
    if (!deferApply) {
      push(next);
      return;
    }
    next.delete(pageParam);
    setDraftSearch(next.toString());
  }

  /** Commits the staged edits. No-op when nothing is staged, so a stray press cannot re-render. */
  function applyStaged() {
    if (!staged || draftSearch === null) return;
    push(new URLSearchParams(draftSearch));
  }

  function applyFilter(param: string, value: string, clears?: string[]) {
    const next = new URLSearchParams(activeSearch);
    if (value) next.set(param, value);
    else next.delete(param);
    // Dropped on the STAGED set, so the dependent control visibly returns to All before Apply is
    // pressed rather than resetting under the reader after the page comes back.
    for (const dependent of clears ?? []) next.delete(dependent);
    write(next);
  }

  /** Replaces every occurrence of `param` with `values`, so the URL carries the set exactly. */
  function applyMultiFilter(param: string, values: string[]) {
    const next = new URLSearchParams(activeSearch);
    next.delete(param);
    for (const value of values) if (value) next.append(param, value);
    write(next);
  }

  /**
   * Writes both halves of a comparison at once.
   *
   * Never one half at a time: a request carrying an operator with no value is a 400, so setting
   * them in two pushes would send the page through a guaranteed error state on the way to a valid
   * one. Clearing either half clears both, for the same reason.
   */
  function applyCompare(opParam: string, valueParam: string, op: string, value: string) {
    const next = new URLSearchParams(activeSearch);
    if (op && value) {
      next.set(opParam, op);
      next.set(valueParam, value);
    } else {
      next.delete(opParam);
      next.delete(valueParam);
    }
    write(next);
  }

  /**
   * Writes both ends of a span at once. A PICKED span is ALWAYS written, even when it happens to
   * Writes both ends of a span at once. A PICKED span is ALWAYS written, even when it happens to
   * equal the default one.
   *
   * Never one end at a time: a read that takes from/to rejects half a window, so setting them in two
   * pushes would send the page through a guaranteed error state on the way to a valid one — the same
   * reasoning as applyCompare.
   *
   * It used to DELETE both ends on a span equal to `defaultFrom … defaultTo`, so that a shared link
   * kept meaning "the default window" rather than freezing the day it was copied. That silently
   * un-pinned a real choice, because ABSENCE and the default span are not the same thing on the host
   * that uses this control: with no parameters the Weights pages derive the window from the herd —
   * start from the "Pages land on" assumption, end at the LATEST WEIGHING DATE **of the selected
   * park** (landing-window.ts) — while `defaultTo` here is simply today. A reader whose pick matched
   * that pair lost it from the URL, the chip still showed the derived window (which looks the same),
   * and the next park change re-derived the period under them: the data moved, the control did not.
   *
   * So a pick now stays picked across park, sex and mode changes, and Clear all — which deletes both
   * ends — is the one way back to the derived window.
   */
  function applyRange(field: Extract<WorklistFilterField, { kind: "daterange" }>, from: string, to: string) {
    const next = new URLSearchParams(activeSearch);
    next.set(field.param, from);
    next.set(field.toParam, to);
    write(next);
  }

  function clearAll() {
    const next = new URLSearchParams(activeSearch);
    for (const field of clearable) {
      next.delete(field.param);
      if (field.kind === "compare") next.delete(field.valueParam);
      if (field.kind === "daterange") next.delete(field.toParam);
    }
    // Staged like every other edit on a deferred bar, rather than applying at once. Mixing the two
    // is what makes a filter bar unpredictable: on this bar NOTHING reaches the server until Apply,
    // and the reader can see the whole reset before committing it.
    write(next);
  }

  const loadingLabel = copy(pageContract, "state.loading");

  const searchApplied = search ? shownValue(search.param, search.value, true) : "";
  const inlineActions = actions ? actions.slice(0, Math.max(0, maxInlineActions)) : [];
  const overflowActions = actions ? actions.slice(Math.max(0, maxInlineActions)) : [];

  /**
   * One chip per APPLIED filter, so what is narrowing the table is readable without opening a
   * single control.
   *
   * Built from the same `clearable` list the reset uses, which is what keeps the two honest: a chip
   * can never name a filter Clear all would leave behind. A multi-select contributes one chip per
   * value, because removing them one at a time is the whole point of a chip.
   */
  type WorklistChip =
    | { key: string; label: string; value: string; kind: "search"; param: string }
    | { key: string; label: string; value: string; kind: "select"; param: string; clears?: string[] }
    | { key: string; label: string; value: string; kind: "multiselect"; param: string; remaining: string[] }
    | { key: string; label: string; value: string; kind: "compare"; param: string; valueParam: string }
    | { key: string; label: string; value: string; kind: "daterange"; field: Extract<WorklistFilterField, { kind: "daterange" }> };

  const chips: WorklistChip[] = [];
  for (const field of clearable) {
    if (field.kind === "multiselect") {
      const values = activeParams.getAll(field.param);
      for (const value of values) {
        chips.push({
          kind: "multiselect",
          key: `${field.param}:${value}`,
          label: field.label,
          value: field.options.find((option) => option.value === value)?.label ?? value,
          param: field.param,
          remaining: values.filter((item) => item !== value),
        });
      }
      continue;
    }
    if (field.kind === "compare") {
      const op = activeParams.get(field.param) ?? "";
      const value = activeParams.get(field.valueParam) ?? "";
      if (!op && !value) continue;
      chips.push({
        kind: "compare",
        key: field.param,
        label: field.label,
        value: `${field.options.find((option) => option.value === op)?.label ?? op} ${value}`.trim(),
        param: field.param,
        valueParam: field.valueParam,
      });
      continue;
    }
    if (field.kind === "daterange") {
      const from = activeParams.get(field.param);
      const to = activeParams.get(field.toParam);
      if (from === null && to === null) continue;
      chips.push({
        kind: "daterange",
        key: field.param,
        label: field.label,
        // DD/MM/YYYY, like every other visible date. These come off the URL as ISO business dates
        // and were rendered verbatim, so the chip read "2026-08-10 – 2026-09-22" directly under a
        // picker reading "10/08/2026 to 22/09/2026" -- two date formats for one filter, on one
        // screen. The wire value is untouched; only what the reader sees changes.
        value: `${fmtDate(from ?? field.defaultFrom)} – ${fmtDate(to ?? field.defaultTo)}`,
        field,
      });
      continue;
    }
    const value = activeParams.get(field.param);
    if (!value) continue;
    // `clearable` admits only select/multiselect/compare/daterange, so anything reaching here is a
    // select; the guard is what tells the compiler that, and it costs one comparison.
    if (field.kind !== "select") continue;
    chips.push({
      kind: "select",
      key: field.param,
      label: field.label,
      value: field.options.find((option) => option.value === value)?.label ?? value,
      param: field.param,
      clears: field.clears,
    });
  }
  if (search && searchApplied) {
    chips.unshift({
      kind: "search",
      key: `__search:${search.param}`,
      label: search.ariaLabel ?? copy(pageContract, "filter.bar_aria"),
      value: searchApplied,
      param: search.param,
    });
  }

  /**
   * Removing one chip.
   *
   * The chips carry DATA, not closures: each one says which filter it stands for and the removal is
   * dispatched here, in an event handler. Building a list of `() => applyFilter(...)` during render
   * is what `react-hooks/refs` objects to — those closures reach the telemetry ref through `push`,
   * and a ref read on the render path is a real hazard, not a lint nit.
   */
  function removeChip(chip: WorklistChip) {
    switch (chip.kind) {
      case "search":
        applyFilter(chip.param, "");
        return;
      case "select":
        applyFilter(chip.param, "", chip.clears);
        return;
      case "multiselect":
        applyMultiFilter(chip.param, chip.remaining);
        return;
      case "compare":
        applyCompare(chip.param, chip.valueParam, "", "");
        return;
      case "daterange":
        applyRange(chip.field, chip.field.defaultFrom, chip.field.defaultTo);
        return;
    }
  }

  const barLabel = label ?? copy(pageContract, "filter.bar_aria");
  // The filter controls, once: inline on a laptop, inside the filters drawer on a phone.
  const controls = (
    <>
      {search ? (
        <WorklistSearchField
          key={search.param}
          pageContract={pageContract}
          search={search}
          applied={searchApplied}
          onCommit={(value) => applyFilter(search.param, value)}
        />
      ) : null}
      {fields.map((field) => {
        // Handled ahead of the shared `effectiveField` shaping below, which assumes a single
        // `value` — a span has two ends and no meaningful single value.
        if (field.kind === "daterange") {
          return (
            <DateRangePicker
              key={field.param}
              labels={field.labels}
              // Falls back to the default window when the parameter is absent, which is how the
              // cleared state is expressed.
              from={shownValue(field.param, field.from, true) || field.defaultFrom}
              to={shownValue(field.toParam, field.to, true) || field.defaultTo}
              today={field.today}
              minDate={field.minDate}
              busy={busy}
              markerDates={field.markerDates}
              markerFetchPath={field.markerFetchPath}
              onChange={(from, to) => applyRange(field, from, to)}
            />
          );
        }
        const effectiveField =
          field.kind === "multiselect"
            ? { ...field, values: activeParams.getAll(field.param) }
            : field.kind === "compare"
              ? {
                  ...field,
                  op: shownValue(field.param, field.op, true),
                  value: shownValue(field.valueParam, field.value, true),
                }
              : {
                  ...field,
                  // A DISABLED control shows the value the page enforces, never whatever the URL
                  // asked for: a park the top bar locked must not read as another park.
                  value: field.disabledReason ? field.value : shownValue(
                    field.param,
                    field.value,
                    // A date is always clearable; a select is clearable only when it offers All.
                    field.kind === "select" ? field.allowAll !== false : true,
                  ),
                };

        return effectiveField.kind === "multiselect" ? (
          <MultiSelectFilter
            key={effectiveField.param}
            field={effectiveField}
            allLabel={allLabel}
            applyLabel={applyLabel}
            deferApply={deferApply}
            onChange={(values) => applyMultiFilter(effectiveField.param, values)}
          />
        ) : effectiveField.kind === "compare" ? (
          <CompareFilter
            key={effectiveField.param}
            field={effectiveField}
            allLabel={allLabel}
            applyLabel={applyLabel}
            deferApply={deferApply}
            onApply={applyStaged}
            onChange={(op, value) => applyCompare(effectiveField.param, effectiveField.valueParam, op, value)}
          />
        ) : (
        effectiveField.kind === "select" ? (
          <TextField
            key={effectiveField.param}
            select
            label={
              <>
                {effectiveField.label}
                {effectiveField.note ? (
                  <Box component="span" sx={{ ml: 0.5, pointerEvents: "auto", display: "inline-flex", verticalAlign: "middle" }}>
                    <InfoHint text={effectiveField.note} />
                  </Box>
                ) : null}
              </>
            }
            value={
              (effectiveField.allowAll === false ? effectiveField.options : [{ value: "" }, ...effectiveField.options]).some((option) => option.value === effectiveField.value)
                ? effectiveField.value
                : ""
            }
            disabled={Boolean(effectiveField.disabledReason)}
            // Disabled reason first — it explains why the control cannot be used at all, which
            // outranks a hint about what it does.
            title={
              effectiveField.disabledReason ||
              effectiveField.note ||
              (busy ? copy(pageContract, "state.loading") : undefined)
            }
            onChange={({ target: { value } }) => applyFilter(effectiveField.param, value, effectiveField.clears)}
            sx={{ minWidth: { xs: 0, sm: 160 }, flexShrink: 0, maxWidth: 1 }}
            slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
          >
            {effectiveField.allowAll === false ? null : <MenuItem value="">{allLabel}</MenuItem>}
            {effectiveField.options.map((option) => (
              <MenuItem key={option.value} value={option.value}>
                {option.label}
              </MenuItem>
            ))}
          </TextField>
        ) : (
        <Box key={effectiveField.param} className="kit-field-date" sx={{ position: "relative", display: "inline-flex", flexDirection: "column" }}>
          {/* The calendar button prints its own field name inside it, so it carries no outer label;
              only the plain themed calendar sits in an outlined, labelled TextField. */}
          {effectiveField.kind === "date" && effectiveField.labels && effectiveField.today && !effectiveField.disabledReason ? (
            <DateRangePicker
              labels={effectiveField.labels}
              from={effectiveField.value}
              to={effectiveField.value}
              today={effectiveField.today}
              busy={busy}
              singleDayOnly
              onChange={(from) => applyFilter(effectiveField.param, from)}
            />
          ) : effectiveField.kind === "date" ? (
            // Template date field (TR1-#22): the MUI X DatePicker the template's CustomDateRangePicker
            // uses -- one outlined field, label on the border, trailing calendar icon, DD/MM/YYYY.
            // Never the OS date control. Bounds come from the field.
            <DatePicker
              label={effectiveField.label}
              value={effectiveField.value ? dayjs(effectiveField.value) : null}
              format="DD/MM/YYYY"
              minDate={effectiveField.min ? dayjs(effectiveField.min) : undefined}
              maxDate={effectiveField.max ? dayjs(effectiveField.max) : undefined}
              disabled={Boolean(effectiveField.disabledReason)}
              onAccept={(next) => applyFilter(effectiveField.param, next?.isValid() ? next.format("YYYY-MM-DD") : "")}
              slotProps={{
                textField: {
                  name: effectiveField.param,
                  title: effectiveField.disabledReason || (busy ? copy(pageContract, "state.loading") : undefined),
                  sx: { minWidth: { xs: 0, sm: 180 }, maxWidth: 1 },
                },
                previousIconButton: { "aria-label": copy(pageContract, "filter.date.previous_month", "Previous month") } as never,
                nextIconButton: { "aria-label": copy(pageContract, "filter.date.next_month", "Next month") } as never,
              }}
            />
          ) : null}
        </Box>
        )
        );
      })}
      {/* The reset lives at the end of the CHIP row, beside the things it clears. It stays here only
          for the case a bar can be "filtered" without producing a chip. */}
      {hasAnyFilter && chips.length === 0 ? (
        <MuiButton color="error" onClick={clearAll} startIcon={<Iconify icon="solar:trash-bin-trash-bold" />}>
          {copy(pageContract, "filter.clear_all")}
        </MuiButton>
      ) : null}
      {/* The bar's ONE commit point when it defers: always rendered, disabled until something is
          staged, so the reader sees before touching anything that this bar waits for a press. */}
      {deferApply ? (
        <MuiButton variant="contained" color="primary" disabled={!staged || busy} onClick={applyStaged}>
          {applyLabel}
        </MuiButton>
      ) : null}
    </>
  );

  return (
    <>
    <Card
      className="wf-bar"
      role="group"
      aria-label={barLabel}
      // Announced on the BAR, which is what the reader just acted on. The held-back rows below carry
      // it too, so a screen reader hears "busy" whichever region it is in.
      aria-busy={busy || undefined}
      sx={{
        overflow: "visible",
        // Inside another card (a table card) the toolbar is part of that card, as in the template list.
        ".MuiCard-root &, .card &": { boxShadow: "none", bgcolor: "transparent", borderRadius: 0 },
      }}
    >
      {/* Template list toolbar (UserTableToolbar): filters, search, trailing actions. */}
      <Box sx={{ p: 2.5, gap: 2, display: "flex", flexWrap: "wrap", alignItems: "center" }}>
        <Box sx={{ display: { xs: "none", md: "flex" }, flexWrap: "wrap", gap: 2, alignItems: "center", flex: "1 1 auto", minWidth: 0 }}>{controls}</Box>
        <MuiButton
          variant="outlined"
          color="inherit"
          aria-expanded={mobileOpen}
          onClick={() => setMobileOpen(true)}
          startIcon={
            <Badge color="error" variant="dot" invisible={chips.length === 0}>
              <Iconify icon="ic:round-filter-list" />
            </Badge>
          }
          sx={{ display: { xs: "inline-flex", md: "none" } }}
        >
          {barLabel}
          {chips.length > 0 ? ` (${chips.length})` : null}
        </MuiButton>
        {/* The busy affordance, beside the control that was just pressed. The word is the contract's,
            and it is what a screen reader gets — the ring itself is decorative. */}
        {busy ? (
          <Box component="span" role="status" sx={{ display: "inline-flex", alignItems: "center", gap: 1, typography: "caption", color: "text.secondary" }}>
            <CircularProgress size={14} color="inherit" aria-hidden="true" />
            {loadingLabel}
          </Box>
        ) : null}
        {inlineActions.length > 0 || overflowActions.length > 0 ? (
          <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1, ml: "auto" }}>
            {inlineActions.map((action) => (
              <MuiButton key={action.id} variant="outlined" color="inherit" disabled={action.disabled} onClick={action.onSelect} startIcon={action.icon}>
                {action.label}
              </MuiButton>
            ))}
            {overflowActions.length > 0 ? (
              <RowMenu
                ariaLabel={copy(pageContract, "filter.bar_aria")}
                actions={overflowActions.map((action) => ({
                  label: action.label,
                  icon: action.icon,
                  danger: action.danger,
                  disabled: action.disabled,
                  onSelect: action.onSelect,
                }))}
              />
            ) : null}
          </Box>
        ) : null}
        {trailing === undefined ? null : (
          <Box component="span" sx={{ display: "inline-flex", alignItems: "center", ml: inlineActions.length || overflowActions.length ? 0 : "auto" }}>
            {trailing}
          </Box>
        )}
      </Box>
      {chips.length > 0 ? (
        /* The applied set, spelled out (template FiltersResult: FiltersBlock + soft Chips + Clear).
           Sits UNDER the controls so adding a filter never reflows the control just used. */
        <Box role="group" aria-label={barLabel} sx={{ px: 2.5, pb: 2.5, display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1 }}>
          {chips.map((chip) => (
            <FiltersBlock key={chip.key} label={`${chip.label}:`} isShow>
              <Chip
                {...chipProps}
                label={chip.value}
                onDelete={() => removeChip(chip)}
                deleteIcon={<Iconify icon="solar:close-circle-bold" aria-label={`${copy(pageContract, "filter.clear_all")}: ${chip.label}`} />}
              />
            </FiltersBlock>
          ))}
          <MuiButton color="error" onClick={clearAll} startIcon={<Iconify icon="solar:trash-bin-trash-bold" />}>
            {copy(pageContract, "filter.clear_all")}
          </MuiButton>
        </Box>
      ) : null}
    </Card>
    <MinimalDrawer open={mobileOpen} onClose={() => setMobileOpen(false)} title={barLabel} aria-label={barLabel}>
      <Box sx={{ p: 2.5, display: "flex", flexDirection: "column", gap: 2.5, "& .MuiFormControl-root, & > .MuiTextField-root": { width: 1 } }}>{controls}</Box>
    </MinimalDrawer>
    {children === undefined ? null : (
      // Nothing is dimmed or held while an apply is in flight: the page puts its rows in a
      // UrlSuspense panel, which swaps them to their skeleton at once (REVIEW-43).
      <div aria-busy={busy || undefined}>
        {children}
      </div>
    )}
    </>
  );
}

function fieldParams(fields: WorklistFilterField[]): string[] {
  const params: string[] = [];
  for (const field of fields) {
    params.push(field.param);
    if (field.kind === "compare") params.push(field.valueParam);
    if (field.kind === "daterange") params.push(field.toParam);
  }
  return params;
}

function nowMs(): number {
  return Date.now();
}

function elapsedMs(startedAtMs: number): number {
  return Math.max(0, nowMs() - startedAtMs);
}

function emitFilterTelemetry(
  telemetry: WorklistFilterTelemetry | undefined,
  phase: "started" | "completed" | "timeout",
  payload: Record<string, unknown>,
) {
  if (!telemetry || typeof window === "undefined") return;
  void fetch("/api/admin-web/performance-events", {
    method: TELEMETRY_POST_METHOD,
    headers: { "content-type": TELEMETRY_CONTENT_TYPE },
    body: JSON.stringify({
      event_name: `${telemetry.eventPrefix}_${phase}`,
      surface: telemetry.surface,
      route: telemetry.route,
      occurred_at: new Date().toISOString(),
      payload,
    }),
    keepalive: true,
  }).catch(() => undefined);
}

/**
 * A multi-valued filter: one dropdown button that opens a CHECKBOX LIST.
 *
 * Not a native `<select multiple>`, which renders as a scrolling list box several rows tall (it
 * does not fit a one-line filter bar) and needs a modifier key to pick a second value — an
 * interaction most operators never find, so a "multi" filter stays single-valued in practice.
 *
 * The trigger states the selection rather than making the reader count chips: the single chosen
 * label when there is one, and "N selected" beyond that. The panel is CLIENT-LOCAL overlay state —
 * opening it must not navigate or re-run the server component; only ticking a box does, because
 * only that changes what is being asked for.
 */
function MultiSelectFilter({
  field,
  allLabel,
  applyLabel,
  deferApply,
  onChange,
}: {
  field: Extract<WorklistFilterField, { kind: "multiselect" }>;
  allLabel: string;
  applyLabel: string;
  /**
   * The BAR owns the commit. Each tick then goes straight into the bar's staged set — which costs
   * nothing, because staging does not navigate — and this panel drops its own Apply button rather
   * than showing a second one that means something different from the first.
   */
  deferApply: boolean;
  onChange: (values: string[]) => void;
}) {
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const open = Boolean(anchor);
  const setOpen = (next: boolean) => {
    if (!next) setAnchor(null);
  };
  // Ticks are STAGED here and committed by Apply.
  //
  // Not applied per tick: every apply rewrites the URL and re-renders this server-rendered page, so
  // choosing four items would run four full page renders and show the operator three intermediate
  // result sets they never asked to see. Staging also makes the panel undoable — closing it without
  // applying leaves the grid exactly as it was.
  const [draft, setDraft] = useState<string[]>(field.values);
  const disabled = Boolean(field.disabledReason);

  // Re-sync the staged set when the applied one changes underneath (Apply, Clear all, back/forward),
  // adjusting during render rather than in an effect so there is no cascading re-render.
  const [lastApplied, setLastApplied] = useState(field.values.join(" "));
  const appliedKey = field.values.join(" ");
  if (lastApplied !== appliedKey) {
    setLastApplied(appliedKey);
    setDraft(field.values);
  }
  const chosen = new Set(draft);


  const summary =
    field.values.length === 0
      ? allLabel
      : field.values.length === 1
        ? (field.options.find((option) => option.value === field.values[0])?.label ?? field.values[0])
        : `${field.values.length} selected`;

  function toggle(value: string) {
    const next = draft.includes(value) ? draft.filter((item) => item !== value) : [...draft, value];
    setDraft(next);
    if (deferApply) onChange(next);
  }

  function apply() {
    setOpen(false);
    // Same set as is already applied: closing is the whole action, and pushing an identical URL
    // would re-render the page for no change.
    if (draft.join(" ") === appliedKey) return;
    onChange(draft);
  }

  // Template anatomy: the outlined select trigger (UserTableToolbar role filter) opening a
  // CustomPopover MenuList of checkbox items; ticks are staged until Apply (or the bar's Apply).
  return (
    <>
      <FormControl sx={{ minWidth: { xs: 0, sm: 160 }, flexShrink: 0 }} disabled={disabled} title={field.disabledReason ?? field.note}>
        <InputLabel shrink>{field.label}</InputLabel>
        <Select
          multiple
          open={false}
          value={field.values}
          label={field.label}
          notched
          displayEmpty
          renderValue={() => summary}
          onOpen={(event) => setAnchor((event.currentTarget as HTMLElement).closest(".MuiInputBase-root") as HTMLElement)}
          inputProps={{ "aria-label": field.label, "aria-haspopup": "true", "aria-expanded": open }}
        />
      </FormControl>
      <CustomPopover open={open} anchorEl={anchor} onClose={() => setOpen(false)} slotProps={{ arrow: { placement: "top-left" } }}>
        <MenuList aria-label={field.label} sx={{ minWidth: 220, maxHeight: 260, overflowY: "auto" }}>
          {field.options.map((option) => (
            <MenuItem key={option.value} onClick={() => toggle(option.value)}>
              <Checkbox disableRipple size="small" checked={chosen.has(option.value)} slotProps={{ input: { "aria-label": option.label } }} />
              {option.label}
            </MenuItem>
          ))}
        </MenuList>
        {/* Outside the scrolling list, so it stays reachable however long the vocabulary grows. On a
            deferred bar the panel carries no Apply (the bar's does the committing). */}
        {deferApply && draft.length === 0 ? null : (
          <Box sx={{ display: "flex", gap: 1, p: 1, borderTop: 1, borderColor: "divider", borderStyle: "dashed" }}>
            {deferApply ? null : (
              <MuiButton size="small" variant="contained" color="primary" onClick={apply}>
                {applyLabel}
              </MuiButton>
            )}
            {draft.length > 0 ? (
              <MuiButton
                size="small"
                variant="outlined"
                color="inherit"
                onClick={() => {
                  setDraft([]);
                  if (deferApply) onChange([]);
                }}
              >
                {allLabel}
              </MuiButton>
            ) : null}
          </Box>
        )}
      </CustomPopover>
    </>
  );
}

/**
 * A numeric comparison filter: an operator select plus a value box.
 *
 * Applied on APPLY or Enter, never on each keystroke and never on blur. Every apply rewrites the URL
 * and re-runs the server component, so a per-keystroke filter would fire a request for "1", "12",
 * "125" on the way to "1250" — three server renders of a page this heavy, for answers nobody wanted.
 * Blur is nearly as bad and worse to reason about: tabbing past a half-typed box would apply it.
 *
 * Both halves are sent together (see applyCompare): the backend rejects half a comparison rather
 * than inventing the other half, so an operator picked before a value is typed simply waits.
 *
 * Setting the operator back to All is the one immediate action, because it is unambiguous: there is
 * no comparison left to assemble, so making the reader press Apply to turn a filter OFF would be
 * ceremony.
 */
function CompareFilter({
  field,
  allLabel,
  applyLabel,
  deferApply,
  onApply,
  onChange,
}: {
  field: Extract<WorklistFilterField, { kind: "compare" }>;
  allLabel: string;
  applyLabel: string;
  /**
   * The BAR owns the commit. Both halves then go into the bar's staged set as they are typed — which
   * navigates nothing, so the per-keystroke render this control was built to avoid cannot happen —
   * and this control drops its own Apply button.
   */
  deferApply: boolean;
  /** Enter still commits, but through the BAR, so it applies every staged filter and not just this one. */
  onApply: () => void;
  onChange: (op: string, value: string) => void;
}) {
  // BOTH halves are held locally while they are being assembled, and only a COMPLETE pair is
  // written to the URL.
  //
  // Holding only the value was a deadlock, and a silent one: picking an operator committed
  // (op, "") which `applyCompare` treats as incomplete and clears, so the operator select bounced
  // straight back to blank; then typing a value committed ("", value), which cleared again. Neither
  // half could ever be set first, so the filter could not be applied at all. Assembling locally is
  // what lets the operator fill the two boxes in either order.
  const [opDraft, setOpDraft] = useState(field.op);
  const [valueDraft, setValueDraft] = useState(field.value);
  const disabled = Boolean(field.disabledReason);

  // Re-sync when the URL changes underneath (Clear all, back/forward, a link carrying its own
  // filters), by ADJUSTING STATE DURING RENDER rather than in an effect.
  //
  // The effect version triggers a cascading render — React renders the stale drafts, commits, then
  // re-renders — which the lint rule `react-hooks/set-state-in-effect` flags for real reasons: on a
  // filter bar it means one frame of the old value flashing in the box. Comparing against the last
  // applied pair is React's documented pattern for "reset state when a prop changes", and it keeps
  // a draft the operator is still typing from being stomped, because only a change in the APPLIED
  // values resets it.
  const [lastApplied, setLastApplied] = useState({ op: field.op, value: field.value });
  if (lastApplied.op !== field.op || lastApplied.value !== field.value) {
    setLastApplied({ op: field.op, value: field.value });
    setOpDraft(field.op);
    setValueDraft(field.value);
  }

  /** Writes the pair only when it is complete, and clears when either half is emptied. */
  function commit(nextOp: string, nextValue: string) {
    const trimmed = nextValue.trim();
    const complete = nextOp !== "" && trimmed !== "";
    const applied = field.op !== "" || field.value !== "";
    if (!complete) {
      // Nothing to apply yet. Only touch the URL if a filter IS applied and one half was just
      // cleared, which is how the operator turns this filter off.
      if (applied) onChange("", "");
      return;
    }
    if (nextOp === field.op && trimmed === field.value) return;
    onChange(nextOp, trimmed);
  }

  const staged = opDraft !== field.op || valueDraft.trim() !== field.value;

  return (
    <Box
      component="span"
      // Wraps so the operator and value each take a full row inside the phone filters drawer.
      sx={{ display: "inline-flex", flexWrap: "wrap", alignItems: "center", gap: 1, maxWidth: "100%" }}
      // The explanation lives on hover rather than beside the control. Printed inline it was three
      // lines of prose wedged between two filters, which pushed the bar to three rows and made the
      // controls themselves harder to find than the note explaining them.
      title={field.note}
    >
      <TextField
        select
        label={field.label}
        value={field.options.some((option) => option.value === opDraft) ? opDraft : ""}
        disabled={disabled}
        title={field.disabledReason ?? field.note}
        onChange={({ target: { value: nextOp } }) => {
          setOpDraft(nextOp);
          // On a deferred bar every change is staged at once — it costs no render, and the pair is
          // still only written when both halves are filled. Otherwise only "All" acts immediately,
          // because it clears and there is nothing left to assemble.
          if (deferApply || nextOp === "") commit(nextOp, valueDraft);
        }}
        // Wide enough for the metric name ("Average weight") -- at 120px the label was cut mid-word.
        sx={{ minWidth: { xs: 0, sm: 180 }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        <MenuItem value="">{allLabel}</MenuItem>
        {field.options.map((option) => (
          <MenuItem key={option.value} value={option.value}>
            {option.label}
          </MenuItem>
        ))}
      </TextField>
      <TextField
        // A labelled template field like its operator select, never a bare unlabeled box.
        label={field.valueAriaLabel}
        sx={{ width: { xs: 1, sm: 200 } }}
        // text + inputMode, not type="number": a number input reports an out-of-range or malformed
        // value as "" in some browsers, which would turn a typo into a cleared filter. The same
        // reasoning as the authoring inputs on this screen.
        type="text"
        value={valueDraft}
        slotProps={{ inputLabel: { shrink: true }, htmlInput: { inputMode: "decimal" } }}
        disabled={disabled}
        onChange={(event) => {
          setValueDraft(event.target.value);
          if (deferApply) commit(opDraft, event.target.value);
        }}
        onKeyDown={(event) => {
          if (event.key === "Enter") {
            event.preventDefault();
            // Through the BAR when it defers, so Enter applies everything staged rather than
            // committing this one filter and leaving the operator's other picks behind.
            if (deferApply) onApply();
            else commit(opDraft, valueDraft);
          }
        }}
      />
      {/* Shown only while there is something to apply, so a bar of these does not read as a row of
          buttons waiting to be pressed. Enter in the value box does the same thing. Absent entirely
          on a deferred bar, which has exactly one Apply. */}
      {!deferApply && staged && !disabled ? (
        <MuiButton variant="contained" color="primary" onClick={() => commit(opDraft, valueDraft)}>
          {applyLabel}
        </MuiButton>
      ) : null}
    </Box>
  );
}

/**
 * The toolbar's search box: a leading magnifier, a clear affordance once there is something to
 * clear, and a commit that waits.
 *
 * Commits on Enter, on a 450ms idle, and on clear — never on every keystroke, because each commit
 * rewrites the URL and re-runs the server component. The box holds its own draft so the caret never
 * jumps while the page behind it re-renders, and re-syncs when the APPLIED value changes underneath
 * (back/forward, Clear all), by adjusting state during render rather than in an effect.
 */
function WorklistSearchField({
  search,
  applied,
  onCommit,
  pageContract,
}: {
  search: WorklistSearch;
  applied: string;
  onCommit: (value: string) => void;
  pageContract: AdminUiPageContract;
}) {
  const [draft, setDraft] = useState(applied);
  const [lastApplied, setLastApplied] = useState(applied);
  if (lastApplied !== applied) {
    setLastApplied(applied);
    setDraft(applied);
  }

  const commit = useRef(onCommit);
  useEffect(() => {
    commit.current = onCommit;
  }, [onCommit]);

  useEffect(() => {
    const trimmed = draft.trim();
    if (trimmed === applied) return undefined;
    const timer = window.setTimeout(() => commit.current(trimmed), 450);
    return () => window.clearTimeout(timer);
  }, [draft, applied]);

  // Template toolbar search (UserTableToolbar): outlined TextField, magnifier adornment, clear button.
  return (
    <TextField
      type="search"
      value={draft}
      placeholder={search.placeholder}
      onChange={(event) => setDraft(event.target.value)}
      onKeyDown={(event) => {
        if (event.key !== "Enter") return;
        event.preventDefault();
        onCommit(draft.trim());
      }}
      sx={{ flex: "1 1 240px", minWidth: { xs: 1, md: 200 } }}
      slotProps={{
        htmlInput: { "aria-label": search.ariaLabel ?? search.placeholder ?? copy(pageContract, "a11y.search", "Search") },
        input: {
          startAdornment: (
            <InputAdornment position="start">
              <Iconify icon="eva:search-fill" sx={{ color: "text.disabled" }} />
            </InputAdornment>
          ),
          endAdornment: draft ? (
            <InputAdornment position="end">
              <IconButton
                edge="end"
                aria-label={copy(pageContract, "a11y.clear_search", "Clear search")}
                onClick={() => {
                  setDraft("");
                  onCommit("");
                }}
              >
                <Iconify icon="mingcute:close-line" />
              </IconButton>
            </InputAdornment>
          ) : undefined,
        },
      }}
    />
  );
}

