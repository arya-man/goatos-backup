"use client";

import { useRouter, useSearchParams } from "next/navigation";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// Server-side filtering for the Counts Breakdown census. Reads the LIVE URL via useSearchParams
// and rewrites one filter param at a time, preserving every other param (top-bar park scope,
// page size). Same idiom as the shed-wise board's filter bar — the filtering itself happens
// server-side on the next render, not as client-side row hiding.
//
// Every option list is passed in from the server component. Farms and sheds come from
// /locations; stage and breed come from the breakdown response's own `facets`, which reports
// the values actually present, so a dropdown can never offer an option matching zero rows.

export type BreakdownFilterOption = {
  value: string;
  label: string;
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
  const routerSearchParams = useSearchParams();
  const current = routerSearchParams?.toString() ?? "";

  const allLabel = copy(pageContract, "filter.all_option");
  const hasAnyFilter = fields.some((field) => field.value !== "");

  function applyFilter(param: string, value: string) {
    const next = new URLSearchParams(current);
    // A filter change must reset paging, or the operator lands on an offset that no longer
    // exists in the newly-filtered result set and sees an empty page.
    next.delete("bd_page");
    if (value) next.set(param, value);
    else next.delete(param);
    const qs = next.toString();
    router.replace(qs ? `/counts/breakdown?${qs}` : "/counts/breakdown", { scroll: false });
  }

  function clearAll() {
    const next = new URLSearchParams(current);
    next.delete("bd_page");
    for (const field of fields) next.delete(field.param);
    const qs = next.toString();
    router.replace(qs ? `/counts/breakdown?${qs}` : "/counts/breakdown", { scroll: false });
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
            value={field.value}
            aria-label={field.label}
            disabled={Boolean(field.disabledReason)}
            title={field.disabledReason}
            style={field.disabledReason ? { opacity: 0.5, cursor: "not-allowed" } : undefined}
            onChange={(event) => applyFilter(field.param, event.target.value)}
          >
            <option value="">{allLabel}</option>
            {field.options.map((option) => (
              <option key={option.value} value={option.value}>
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
