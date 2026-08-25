"use client";

import { useEffect, useMemo, useRef, useState, useTransition, type ReactNode } from "react";
import { ChevronDown } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";

import { DateRangePicker, type DateRangePickerLabels } from "@/components/date-range-picker";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { worklistFilterIsStaged } from "@/lib/worklist-filter-draft";
import { worklistFilterShownValue } from "@/lib/worklist-filter-value";

export type WorklistFilterOption = { value: string; label: string };

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
       * The window the page falls back to when neither parameter is present. Selecting exactly this
       * span CLEARS both parameters, so a bookmark keeps meaning "the last 30 days" rather than
       * freezing on the span it was taken in.
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
  inlineTrailing,
  trailing,
  children,
}: {
  basePath: string;
  pageParam: string;
  fields: WorklistFilterField[];
  pageContract: AdminUiPageContract;
  telemetry?: WorklistFilterTelemetry;
  /**
   * A page-owned control that sits IN LINE with the filters, immediately after the last one, as
   * though it were another field.
   *
   * For a control that belongs beside the filters but is not one of them, because it changes
   * nothing the server has to fetch — the Weights Sex control switches between grains that all
   * arrived in one response, so routing it through the URL would cost a full page render to show
   * rows the reader already has. It is rendered raw and owns its own label and state; this bar
   * neither reads nor writes it.
   */
  inlineTrailing?: ReactNode;
  /**
   * A page-owned control pinned to the END of the bar, on the same line as the filters (the Weights
   * download drawer opener). Pushed right with `margin-left:auto` so it stays at the far edge as
   * filters are added, and wraps with the rest of the bar on a narrow viewport.
   */
  trailing?: ReactNode;
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
   * Writes both ends of a span at once, or clears both when the reader lands back on the default.
   *
   * Never one end at a time: a read that takes from/to rejects half a window, so setting them in two
   * pushes would send the page through a guaranteed error state on the way to a valid one — the same
   * reasoning as applyCompare.
   */
  function applyRange(field: Extract<WorklistFilterField, { kind: "daterange" }>, from: string, to: string) {
    const next = new URLSearchParams(activeSearch);
    if (from === field.defaultFrom && to === field.defaultTo) {
      next.delete(field.param);
      next.delete(field.toParam);
    } else {
      next.set(field.param, from);
      next.set(field.toParam, to);
    }
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

  return (
    <>
    <div
      className="tbar"
      style={{ display: "flex", alignItems: "center", gap: 10, padding: "12px 14px", flexWrap: "wrap" }}
      role="group"
      aria-label={copy(pageContract, "filter.bar_aria")}
      // Announced on the BAR, which is what the reader just acted on. The held-back rows below carry
      // it too, so a screen reader hears "busy" whichever region it is in.
      aria-busy={busy || undefined}
    >
      {fields.map((field) => {
        // Handled ahead of the shared `effectiveField` shaping below, which assumes a single
        // `value` — a span has two ends and no meaningful single value.
        if (field.kind === "daterange") {
          return (
            <label
              key={field.param}
              style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12 }}
            >
              <span className="muted">{field.label}</span>
              <DateRangePicker
                labels={field.labels}
                // Falls back to the default window when the parameter is absent, which is how the
                // cleared state is expressed.
                from={shownValue(field.param, field.from, true) || field.defaultFrom}
                to={shownValue(field.toParam, field.to, true) || field.defaultTo}
                today={field.today}
                busy={busy}
                markerDates={field.markerDates}
                markerFetchPath={field.markerFetchPath}
                onChange={(from, to) => applyRange(field, from, to)}
              />
            </label>
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
                  value: shownValue(
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
        <label
          key={effectiveField.param}
          style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12 }}
          // On the whole control, label included, so the hint is reachable from the word the reader
          // is already looking at rather than only from the box itself.
          title={effectiveField.kind === "select" ? effectiveField.note : undefined}
        >
          <span className="muted">{effectiveField.label}</span>
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
            <input
              className="tsize"
              type="date"
              value={effectiveField.value}
              min={effectiveField.min}
              max={effectiveField.max}
              aria-label={effectiveField.label}
              disabled={Boolean(effectiveField.disabledReason)}
              title={effectiveField.disabledReason || (busy ? copy(pageContract, "state.loading") : undefined)}
              style={effectiveField.disabledReason ? { opacity: 0.5, cursor: "not-allowed" } : undefined}
              onChange={(event) => applyFilter(effectiveField.param, event.target.value)}
            />
          ) : (
            <select
              className="tsize"
              value={effectiveField.value}
              aria-label={effectiveField.label}
              disabled={Boolean(effectiveField.disabledReason)}
              // Disabled reason first — it explains why the control cannot be used at all, which
              // outranks a hint about what it does.
              title={
                effectiveField.disabledReason ||
                effectiveField.note ||
                (busy ? copy(pageContract, "state.loading") : undefined)
              }
              style={effectiveField.disabledReason ? { opacity: 0.5, cursor: "not-allowed" } : undefined}
              onChange={(event) =>
                applyFilter(effectiveField.param, event.target.value, effectiveField.clears)
              }
            >
              {effectiveField.allowAll === false ? null : <option value="">{allLabel}</option>}
              {effectiveField.options.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
          )}
        </label>
        );
      })}
      {inlineTrailing}
      {hasAnyFilter ? (
        <button type="button" className="btn sm" onClick={clearAll}>
          {copy(pageContract, "filter.clear_all")}
        </button>
      ) : null}
      {/* The bar's ONE commit point when it defers. Always rendered rather than appearing with the
          first edit, so the reader can see before touching anything that this bar waits for a press —
          a button that materialises after the fact would leave the first pick looking like it did
          nothing. Disabled until something is actually staged, which is also what stops a press from
          re-running the page for an unchanged set. */}
      {deferApply ? (
        <button
          type="button"
          className="btn sm p"
          disabled={!staged || busy}
          aria-disabled={!staged || busy}
          style={staged && !busy ? undefined : { opacity: 0.5, cursor: "not-allowed" }}
          onClick={applyStaged}
        >
          {applyLabel}
        </button>
      ) : null}
      {/* The busy affordance, at the end of the bar so it appears beside the control that was just
          pressed rather than somewhere the reader has to go looking. The word is the contract's, and
          it is what a screen reader gets — the ring itself is decorative. */}
      {busy ? (
        <span
          style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12 }}
          className="muted"
          role="status"
        >
          <span className="wfspin" aria-hidden="true" />
          {loadingLabel}
        </span>
      ) : null}
      {trailing === undefined ? null : (
        <span style={{ marginLeft: "auto", display: "inline-flex", alignItems: "center" }}>
          {trailing}
        </span>
      )}
    </div>
    {children === undefined ? null : (
      <div className={busy ? "wfbusy" : undefined} aria-busy={busy || undefined}>
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
  const [open, setOpen] = useState(false);
  const wrapper = useRef<HTMLSpanElement | null>(null);
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

  // Close on an outside click or Escape — the two ways every popover on this screen closes. The
  // listeners are attached only while the panel is open, so a page full of these costs nothing.
  useEffect(() => {
    if (!open) return undefined;
    function onPointerDown(event: MouseEvent) {
      if (wrapper.current && !wrapper.current.contains(event.target as Node)) setOpen(false);
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

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

  return (
    <span
      ref={wrapper}
      style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12, position: "relative" }}
    >
      <span className="muted">{field.label}</span>
      <button
        type="button"
        className="tsize"
        aria-haspopup="true"
        aria-expanded={open}
        aria-label={field.label}
        disabled={disabled}
        // Hover, not inline: see CompareFilter for why these notes are no longer printed on the bar.
        title={field.disabledReason ?? field.note}
        onClick={() => setOpen((value) => !value)}
        style={{
          display: "inline-flex",
          alignItems: "center",
          gap: 6,
          cursor: disabled ? "not-allowed" : "pointer",
          opacity: disabled ? 0.5 : 1,
          minWidth: 120,
          justifyContent: "space-between",
        }}
      >
        <span>{summary}</span>
        <ChevronDown className="ic" aria-hidden="true" style={{ width: 14, height: 14 }} />
      </button>
      {open ? (
        <div
          className="card"
          role="group"
          aria-label={field.label}
          style={{
            position: "absolute",
            top: "calc(100% + 6px)",
            left: 0,
            zIndex: 40,
            minWidth: 220,
            // Capped and scrollable: the feed vocabulary is a dozen items today and grows with the
            // workbook, and a panel that grows without limit would run off the bottom of the screen.
            maxHeight: 260,
            overflowY: "auto",
            padding: "8px 4px",
          }}
        >
          <div style={{ maxHeight: 200, overflowY: "auto" }}>
            {field.options.map((option) => (
              <label
                key={option.value}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                  padding: "5px 10px",
                  cursor: "pointer",
                  whiteSpace: "nowrap",
                }}
              >
                <input
                  type="checkbox"
                  checked={chosen.has(option.value)}
                  onChange={() => toggle(option.value)}
                />
                <span>{option.label}</span>
              </label>
            ))}
          </div>
          {/* Sits OUTSIDE the scrolling list, so it stays reachable however long the vocabulary
              grows. On a deferred bar the panel carries no Apply — the bar's does the committing —
              and this row is only the reset, shown when there is something to reset. */}
          {deferApply && draft.length === 0 ? null : (
            <div
              style={{
                display: "flex",
                gap: 6,
                padding: "8px 10px 2px",
                borderTop: "1px solid var(--line)",
                marginTop: 6,
              }}
            >
              {deferApply ? null : (
                <button type="button" className="btn sm p" onClick={apply}>
                  {applyLabel}
                </button>
              )}
              {draft.length > 0 ? (
                <button
                  type="button"
                  className="btn sm"
                  onClick={() => {
                    setDraft([]);
                    if (deferApply) onChange([]);
                  }}
                >
                  {allLabel}
                </button>
              ) : null}
            </div>
          )}
        </div>
      ) : null}
    </span>
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
    <span
      style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12 }}
      // The explanation lives on hover rather than beside the control. Printed inline it was three
      // lines of prose wedged between two filters, which pushed the bar to three rows and made the
      // controls themselves harder to find than the note explaining them.
      title={field.note}
    >
      <label style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
        <span className="muted">{field.label}</span>
        <select
          className="tsize"
          value={opDraft}
          aria-label={field.label}
          disabled={disabled}
          title={field.disabledReason ?? field.note}
          style={disabled ? { opacity: 0.5, cursor: "not-allowed" } : undefined}
          onChange={(event) => {
            setOpDraft(event.target.value);
            // On a deferred bar every change is staged at once — it costs no render, and the pair is
            // still only written when both halves are filled. Otherwise only "All" acts immediately,
            // because it clears and there is nothing left to assemble; any real operator waits for
            // Apply, since the value half is not filled in yet.
            if (deferApply || event.target.value === "") commit(event.target.value, valueDraft);
          }}
        >
          <option value="">{allLabel}</option>
          {field.options.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      </label>
      <input
        className="tsize"
        // text + inputMode, not type="number": a number input reports an out-of-range or malformed
        // value as "" in some browsers, which would turn a typo into a cleared filter. The same
        // reasoning as the authoring inputs on this screen.
        type="text"
        inputMode="decimal"
        value={valueDraft}
        aria-label={field.valueAriaLabel}
        disabled={disabled}
        style={{ width: 72, ...(disabled ? { opacity: 0.5, cursor: "not-allowed" } : {}) }}
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
        <button type="button" className="btn sm p" onClick={() => commit(opDraft, valueDraft)}>
          {applyLabel}
        </button>
      ) : null}
    </span>
  );
}
