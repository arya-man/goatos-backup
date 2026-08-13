"use client";

import { Search } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { useState, useTransition } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { worklistFilterIsStaged } from "@/lib/worklist-filter-draft";
import type { ProcurementVendorCatalog } from "@/lib/api/server";

export type VendorFilterKey = "record_type" | "status" | "state" | "city" | "breed";

const FILTERS: { key: VendorFilterKey; catalogKey: keyof ProcurementVendorCatalog; copyKey: string }[] = [
  { key: "record_type", catalogKey: "record_types", copyKey: "filter.record_type" },
  { key: "status", catalogKey: "statuses", copyKey: "filter.status" },
  { key: "state", catalogKey: "states", copyKey: "filter.state" },
  { key: "city", catalogKey: "cities", copyKey: "filter.city" },
  { key: "breed", catalogKey: "breeds", copyKey: "filter.breed" },
];

const PAGE_PARAM = "offset";
const FILTER_PARAMS = ["search", ...FILTERS.map((f) => f.key)];

/**
 * The register's search + facet bar. STAGED: edits collect locally and commit on Apply.
 *
 * Why staged rather than apply-on-change, which is what this bar did first: every control wrote the
 * URL the moment it changed, so narrowing by record type AND state AND city ran three full server
 * renders and showed two intermediate result sets nobody asked for. That is the same defect the
 * maintainer reported on Feed Config (see lib/worklist-filter-draft.ts), and the same fix.
 *
 * It is a CLIENT component doing router.replace inside useTransition, never a native
 * `<form method="GET">`. A native GET submit is a full DOCUMENT navigation: the browser tears the
 * page down and rebuilds the whole shell, which reads as "the entire site is loading" on every
 * search. router.replace re-renders only this route's server tree, so shell, sidebar and scroll
 * position survive.
 *
 * Applying always RESETS to page 1. A filter applied while on page 5 would otherwise keep
 * offset=100 and land past the end of a now-shorter result -- an empty table under a non-zero count.
 */
export function VendorFilterBar({
  pageContract,
  catalog,
  search,
  filters,
}: {
  pageContract: AdminUiPageContract;
  catalog: ProcurementVendorCatalog | null;
  /** What the server last rendered -- the APPLIED state the draft is compared against. */
  search: string;
  filters: Partial<Record<VendorFilterKey, string>>;
}) {
  const router = useRouter();
  const params = useSearchParams();
  const [pending, startTransition] = useTransition();

  const applied: Record<string, string | undefined> = { search, ...filters };
  const [draft, setDraft] = useState<Record<string, string>>(() => toDraft(applied));

  // Re-sync the whole draft when the SERVER answers with different applied values (Back/Forward, or
  // Clear all). Adjusted during render rather than in an effect: React re-runs this component with
  // the new state before painting, so the controls never flash the stale selection -- and it avoids
  // the cascading render that setState-in-an-effect causes.
  const appliedKey = JSON.stringify(toDraft(applied));
  const [syncedKey, setSyncedKey] = useState(appliedKey);
  if (syncedKey !== appliedKey) {
    setSyncedKey(appliedKey);
    setDraft(toDraft(applied));
  }

  function draftSearchString(next: Record<string, string>): string {
    const qs = new URLSearchParams(params.toString());
    for (const key of FILTER_PARAMS) {
      const value = (next[key] ?? "").trim();
      if (value) qs.set(key, value);
      else qs.delete(key);
    }
    qs.delete(PAGE_PARAM);
    return qs.toString();
  }

  // Is there anything to apply? Compared with the shared helper so "the same question asked in a
  // different order" is not mistaken for a change, and so being on page 3 never counts as one.
  const staged = worklistFilterIsStaged(`?${draftSearchString(draft)}`, `?${params.toString()}`, PAGE_PARAM);

  function apply(next: Record<string, string>): void {
    const qs = draftSearchString(next);
    startTransition(() => {
      router.replace(qs ? `/procurement/vendors?${qs}` : "/procurement/vendors", { scroll: false });
    });
  }

  const hasAnyApplied = Boolean(search) || FILTERS.some((f) => filters[f.key]);

  return (
    <div className="fchipsbar" style={{ marginBottom: 14, gap: 8, flexWrap: "wrap", alignItems: "center" }}>
      <Search className="ic" style={{ width: 14, color: "var(--brand-d)" }} aria-hidden="true" />
      <input
        type="search"
        value={draft.search ?? ""}
        onChange={(event) => setDraft({ ...draft, search: event.target.value })}
        onKeyDown={(event) => {
          // Enter applies the WHOLE staged bar, not just the search term -- otherwise typing a term
          // and pressing Enter would silently discard a facet the operator had just picked.
          if (event.key === "Enter") {
            event.preventDefault();
            apply(draft);
          }
        }}
        placeholder={copy(pageContract, "filter.search_placeholder")}
        aria-label={copy(pageContract, "filter.search_label")}
        className="input"
        style={{ minWidth: 240 }}
      />

      {FILTERS.map((filter) => (
        <select
          key={filter.key}
          value={draft[filter.key] ?? ""}
          onChange={(event) => setDraft({ ...draft, [filter.key]: event.target.value })}
          aria-label={copy(pageContract, filter.copyKey)}
          className="input"
          disabled={!catalog}
        >
          <option value="">
            {copy(pageContract, filter.copyKey)}: {copy(pageContract, "filter.all")}
          </option>
          {(catalog?.[filter.catalogKey] ?? []).map((entry) => (
            <option key={entry.value} value={entry.value}>
              {entry.label}
            </option>
          ))}
        </select>
      ))}

      {/* Disabled with nothing staged, so the control tells the truth about whether pressing it
          would change anything. */}
      <button
        type="button"
        className="btn p"
        onClick={() => apply(draft)}
        disabled={pending || !staged}
        aria-disabled={pending || !staged}
        title={staged ? undefined : copy(pageContract, "filter.apply.nothing_staged")}
      >
        {pending ? copy(pageContract, "filter.applying") : copy(pageContract, "filter.apply")}
      </button>

      {hasAnyApplied || staged ? (
        <button type="button" className="chip" onClick={() => apply({})} disabled={pending}>
          {copy(pageContract, "filter.clear")}
        </button>
      ) : null}
    </div>
  );
}

/** The applied server state as a plain draft record, with absent values as "". */
function toDraft(applied: Record<string, string | undefined>): Record<string, string> {
  const draft: Record<string, string> = { search: applied.search ?? "" };
  for (const filter of FILTERS) draft[filter.key] = applied[filter.key] ?? "";
  return draft;
}
