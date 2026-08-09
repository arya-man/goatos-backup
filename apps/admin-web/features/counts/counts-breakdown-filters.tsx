"use client";

import { useMemo, useState, useTransition } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// Server-side filtering for the Counts Breakdown census. Reads the LIVE URL via useSearchParams
// and rewrites one filter param at a time, preserving every other param (top-bar park scope,
// page size). Same idiom as the shed-wise board's filter bar — the filtering itself happens
// server-side on the next render, not as client-side row hiding.
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
};

export type BreakdownFilterField = {
  /** URL param name, e.g. "bd_farm". */
  param: string;
  label: string;
  value: string;
  options: BreakdownFilterOption[];
  /** When set, the control renders disabled and shows this as its reason. */
  disabledReason?: string;
};

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
    () => Object.fromEntries(fields.map((field) => [field.param, field.value])),
    [fields],
  );
  const [optimistic, setOptimistic] = useState<{ from: string; values: Record<string, string> } | null>(null);

  const allLabel = copy(pageContract, "filter.all_option");
  const optimisticValues = optimistic?.from === current ? optimistic.values : null;
  const fieldValue = (field: BreakdownFilterField) => optimisticValues?.[field.param] ?? field.value;
  const hasAnyFilter = fields.some((field) => fieldValue(field) !== "");

  function effectiveParams() {
    const next = new URLSearchParams(current);
    if (!optimisticValues) return next;
    for (const field of fields) {
      const value = optimisticValues[field.param] ?? "";
      if (value) next.set(field.param, value);
      else next.delete(field.param);
    }
    return next;
  }

  function applyFilter(param: string, value: string) {
    setOptimistic({ from: current, values: { ...serverValues, ...optimisticValues, [param]: value } });
    const next = effectiveParams();
    // A filter change must reset paging, or the operator lands on an offset that no longer
    // exists in the newly-filtered result set and sees an empty page.
    next.delete("bd_page");
    if (value) next.set(param, value);
    else next.delete(param);
    const qs = next.toString();
    startTransition(() => {
      router.replace(qs ? `/counts/breakdown?${qs}` : "/counts/breakdown", { scroll: false });
    });
  }

  function clearAll() {
    setOptimistic(() => {
      const nextValues = { ...serverValues, ...optimisticValues };
      for (const field of fields) nextValues[field.param] = "";
      return { from: current, values: nextValues };
    });
    const next = effectiveParams();
    next.delete("bd_page");
    for (const field of fields) next.delete(field.param);
    const qs = next.toString();
    startTransition(() => {
      router.replace(qs ? `/counts/breakdown?${qs}` : "/counts/breakdown", { scroll: false });
    });
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
          <select
            className="tsize"
            value={fieldValue(field)}
            aria-label={field.label}
            disabled={Boolean(field.disabledReason)}
            title={field.disabledReason || (isPending ? copy(pageContract, "state.loading") : undefined)}
            style={field.disabledReason ? { opacity: 0.5, cursor: "not-allowed" } : undefined}
            onChange={(event) => applyFilter(field.param, event.target.value)}
          >
            <option value="">{allLabel}</option>
            {field.options.map((option) => (
              <option key={option.key ?? option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
      ))}
      {hasAnyFilter ? (
        <button type="button" className="btn sm" onClick={clearAll}>
          {copy(pageContract, "filter.clear_all")}
        </button>
      ) : null}
    </div>
  );
}
