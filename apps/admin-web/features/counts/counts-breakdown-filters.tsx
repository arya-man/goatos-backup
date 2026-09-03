"use client";

import { useEffect, useMemo, useRef, useState, useTransition } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// Server-side filtering for the Counts Breakdown census, STAGED behind an Apply button
// (maintainer instruction, 2026-09-03): changing a control edits local staged state only, and
// nothing navigates until Apply. Stage, Breed and Shed are MULTI-SELECT (checkbox dropdowns,
// repeated URL params, OR within a dimension); Farm and Gender stay single-select. Apply rewrites
// every filter param at once, preserving every other param (top-bar park scope, page size), and
// the filtering itself still happens server-side on the next render — never client-side row
// hiding.
//
// Every option list is passed in from the server component. Farm, stage, breed AND shed all come
// from the breakdown response's own `facets`, which reports the values actually present, so a
// dropdown can never offer an option matching zero rows. Sheds carry a composite `key`
// (park_id + shed_id) because shed names repeat across parks.

export type BreakdownFilterOption = {
  value: string;
  label: string;
  /**
   * Optional distinct React key. `value` is what the backend receives, but some vocabularies
   * (sheds) share a value shape across scopes and need a composite key (park_id + shed_id) so
   * two same-named entries never collapse. Falls back to `value` when absent.
   */
  key?: string;
  /**
   * Optional group heading. Consecutive options sharing a group render under one heading;
   * options with no group render loose. Used by the Shed vocabulary to file a subdivided shed's
   * pens under the shed itself instead of listing 148 flat rows.
   */
  group?: string;
};

export type BreakdownFilterField = {
  /** URL param name, e.g. "bd_farm". */
  param: string;
  label: string;
  /** The currently APPLIED selection (from the URL). Single-select fields carry 0 or 1 entry. */
  values: string[];
  options: BreakdownFilterOption[];
  /** Multi-select fields render a checkbox dropdown; the rest render a native single select. */
  multi?: boolean;
  /** When set, the control renders disabled and shows this as its reason. */
  disabledReason?: string;
};

/**
 * Split an option list into consecutive same-group runs, preserving order.
 *
 * Deliberately CONSECUTIVE rather than gathered by name: the option order is the vocabulary's own
 * (sheds arrive grouped already), and re-gathering would silently reorder the list and merge two
 * same-named groups from different parks — the exact name-keyed merge the operational-location
 * convention bans. Two runs may therefore share a heading; they stay separate groups.
 */
function groupRuns(options: BreakdownFilterOption[]): { group: string | undefined; options: BreakdownFilterOption[] }[] {
  const runs: { group: string | undefined; options: BreakdownFilterOption[] }[] = [];
  for (const option of options) {
    const last = runs[runs.length - 1];
    if (last && last.group === option.group) last.options.push(option);
    else runs.push({ group: option.group, options: [option] });
  }
  return runs;
}

/**
 * One multi-select filter: a button summarizing the staged selection, opening a checkbox
 * dropdown. Selection edits reach the parent's staged state only — Apply does the navigating.
 */
function MultiSelectFilter({
  field,
  selected,
  allLabel,
  selectedSuffix,
  onToggle,
}: {
  field: BreakdownFilterField;
  selected: string[];
  allLabel: string;
  selectedSuffix: string;
  onToggle: (value: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);

  // Outside click / Escape close, only wired while open.
  useEffect(() => {
    if (!open) return;
    function onPointerDown(event: MouseEvent) {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) setOpen(false);
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

  const selectedSet = new Set(selected);
  // 0 selected reads "All" (the same sentinel the single selects use); 1 selected shows the
  // option's own label (data, not composed copy); more show the count with the backend-owned
  // suffix ("3 selected").
  const summary =
    selected.length === 0
      ? allLabel
      : selected.length === 1
        ? (field.options.find((option) => option.value === selected[0])?.label ?? selected[0])
        : `${selected.length} ${selectedSuffix}`;

  return (
    <div ref={rootRef} style={{ position: "relative", display: "inline-flex" }}>
      <button
        type="button"
        className="tsize"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={field.label}
        disabled={Boolean(field.disabledReason)}
        title={field.disabledReason || undefined}
        style={{
          cursor: field.disabledReason ? "not-allowed" : "pointer",
          opacity: field.disabledReason ? 0.5 : undefined,
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "space-between",
          gap: 6,
          // Match the native selects beside it (Farm/Gender render ~170px): a content-sized
          // button collapses to the width of "All" and reads as a different control family.
          minWidth: 170,
        }}
        onClick={() => setOpen((current) => !current)}
      >
        <span style={{ overflow: "hidden", textOverflow: "ellipsis", textAlign: "left", flex: "1 1 auto" }}>{summary}</span>
        <span aria-hidden="true" style={{ fontSize: 9, color: "var(--muted)" }}>
          ▾
        </span>
      </button>
      {open ? (
        <div
          role="listbox"
          aria-multiselectable="true"
          aria-label={field.label}
          style={{
            position: "absolute",
            top: "calc(100% + 4px)",
            left: 0,
            zIndex: 40,
            minWidth: 220,
            maxWidth: 320,
            maxHeight: 300,
            overflowY: "auto",
            background: "var(--panel)",
            border: "1px solid var(--line)",
            borderRadius: 10,
            boxShadow: "0 12px 28px rgba(0,0,0,.35)",
            padding: 6,
          }}
        >
          {groupRuns(field.options).map((run, runIndex) => (
            <div key={`r:${runIndex}:${run.options[0]?.key ?? run.options[0]?.value}`}>
              {run.group !== undefined ? (
                <div
                  className="muted"
                  style={{ fontSize: 10.5, textTransform: "uppercase", letterSpacing: ".04em", padding: "6px 8px 2px" }}
                >
                  {run.group}
                </div>
              ) : null}
              {run.options.map((option) => {
                const checked = selectedSet.has(option.value);
                return (
                  <label
                    key={option.key ?? option.value}
                    role="option"
                    aria-selected={checked}
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 8,
                      padding: "5px 8px",
                      borderRadius: 8,
                      fontSize: 12.5,
                      cursor: "pointer",
                      whiteSpace: "nowrap",
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                    }}
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() => onToggle(option.value)}
                      style={{ accentColor: "var(--brand)", flex: "0 0 auto" }}
                    />
                    <span style={{ overflow: "hidden", textOverflow: "ellipsis" }}>{option.label}</span>
                  </label>
                );
              })}
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}

export function CountsBreakdownFilters({
  fields,
  pageContract,
}: {
  fields: BreakdownFilterField[];
  pageContract: AdminUiPageContract;
}) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();
  const routerSearchParams = useSearchParams();
  const current = routerSearchParams?.toString() ?? "";
  const serverValues = useMemo(
    () => Object.fromEntries(fields.map((field) => [field.param, field.values])),
    [fields],
  );
  // Staged (not-yet-applied) selections. Keyed to the URL they were staged FROM, so a completed
  // navigation (ours or anything else's) resets the staging to what the server actually applied —
  // the same idiom the previous optimistic single-select bar used.
  const [staged, setStaged] = useState<{ from: string; values: Record<string, string[]> } | null>(null);

  const allLabel = copy(pageContract, "filter.all_option");
  const selectedSuffix = copy(pageContract, "filter.selected_count");
  const stagedValues = staged?.from === current ? staged.values : null;
  const fieldValues = (field: BreakdownFilterField) => stagedValues?.[field.param] ?? field.values;
  const hasAnySelection = fields.some((field) => fieldValues(field).length > 0);

  function stage(param: string, values: string[]) {
    setStaged({ from: current, values: { ...serverValues, ...stagedValues, [param]: values } });
  }

  function navigateWith(values: Record<string, string[]>) {
    const next = new URLSearchParams(current);
    // A filter change must reset paging, or the operator lands on an offset that no longer
    // exists in the newly-filtered result set and sees an empty page.
    next.delete("bd_page");
    for (const field of fields) {
      next.delete(field.param);
      for (const value of values[field.param] ?? []) {
        if (value) next.append(field.param, value);
      }
    }
    const qs = next.toString();
    startTransition(() => {
      router.replace(qs ? `/counts/breakdown?${qs}` : "/counts/breakdown", { scroll: false });
    });
  }

  function applyFilters() {
    const values: Record<string, string[]> = {};
    for (const field of fields) values[field.param] = fieldValues(field);
    navigateWith(values);
  }

  function clearAll() {
    const cleared: Record<string, string[]> = {};
    for (const field of fields) cleared[field.param] = [];
    setStaged({ from: current, values: cleared });
    navigateWith(cleared);
  }

  return (
    <div
      className="tbar"
      style={{ display: "flex", alignItems: "center", gap: 10, padding: "12px 14px", flexWrap: "wrap" }}
      role="group"
      aria-label={copy(pageContract, "filter.bar_aria")}
    >
      {fields.map((field) => (
        <label
          key={field.param}
          style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 12 }}
        >
          <span className="muted">{field.label}</span>
          {field.multi ? (
            <MultiSelectFilter
              field={field}
              selected={fieldValues(field)}
              allLabel={allLabel}
              selectedSuffix={selectedSuffix}
              onToggle={(value) => {
                const currentValues = fieldValues(field);
                stage(
                  field.param,
                  currentValues.includes(value)
                    ? currentValues.filter((entry) => entry !== value)
                    : [...currentValues, value],
                );
              }}
            />
          ) : (
            <select
              className="tsize"
              value={fieldValues(field)[0] ?? ""}
              aria-label={field.label}
              disabled={Boolean(field.disabledReason)}
              title={field.disabledReason || (isPending ? copy(pageContract, "state.loading") : undefined)}
              style={field.disabledReason ? { opacity: 0.5, cursor: "not-allowed" } : undefined}
              onChange={(event) => stage(field.param, event.target.value ? [event.target.value] : [])}
            >
              <option value="">{allLabel}</option>
              {groupRuns(field.options).map((run) =>
                run.group === undefined ? (
                  run.options.map((option) => (
                    <option key={option.key ?? option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))
                ) : (
                  <optgroup key={`g:${run.group}:${run.options[0]?.key ?? run.options[0]?.value}`} label={run.group}>
                    {run.options.map((option) => (
                      <option key={option.key ?? option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </optgroup>
                ),
              )}
            </select>
          )}
        </label>
      ))}
      {/* One right-pinned group, so Clear all sits beside Apply instead of wrapping to a new
          row when the auto margin eats the free space. */}
      <div style={{ marginLeft: "auto", display: "inline-flex", alignItems: "center", gap: 10 }}>
        {hasAnySelection ? (
          <button type="button" className="btn sm" onClick={clearAll} disabled={isPending}>
            {copy(pageContract, "filter.clear_all")}
          </button>
        ) : null}
        <button
          type="button"
          className="btn sm primary"
          onClick={applyFilters}
          disabled={isPending}
          title={isPending ? copy(pageContract, "state.loading") : undefined}
        >
          {copy(pageContract, "filter.apply")}
        </button>
      </div>
    </div>
  );
}
