"use client";

import Box from "@mui/material/Box";

import { useRouter, useSearchParams } from "next/navigation";
import { useState, useTransition } from "react";

import Button from "@mui/material/Button";
import InputAdornment from "@mui/material/InputAdornment";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { Iconify } from "@/components/minimal/iconify";
import { worklistFilterIsStaged } from "@/lib/worklist-filter-draft";
import type { ProcurementVendorCatalog } from "@/lib/api/server";
import { OrderTableToolbar } from "@/components/app/sections/order/order-table-toolbar";
import { orderToolbarFilterSx, orderToolbarSearchSx } from "@/components/app/order-toolbar-filter";
import { useTableColumnsMenu } from "./table-toolbar";

export type VendorFilterKey = "record_type" | "status" | "state" | "city" | "breed";

// Status is not a select here: it is the template list's Tabs row above this toolbar
// (vendor-board.tsx), which writes the same `?status=` param.
const FILTERS: { key: Exclude<VendorFilterKey, "status">; catalogKey: keyof ProcurementVendorCatalog; copyKey: string }[] = [
  { key: "record_type", catalogKey: "record_types", copyKey: "filter.record_type" },
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
  pathname,
  catalog,
  search,
  filters,
  tableId,
  exportName,
}: {
  pageContract: AdminUiPageContract;
  /**
   * The route this bar applies its filters on. Passed in rather than hardcoded because the same bar
   * serves both halves of the register -- Procurement > Vendors and Sales > Vendors -- and applying
   * a filter must keep the person on the page they are looking at.
   */
  pathname: string;
  catalog: ProcurementVendorCatalog | null;
  /** What the server last rendered -- the APPLIED state the draft is compared against. */
  search: string;
  filters: Partial<Record<VendorFilterKey, string>>;
  /** Id of the table's scroll region, for the ⋮ Columns / Export actions. */
  tableId: string;
  exportName: string;
}) {
  const router = useRouter();
  const params = useSearchParams();
  const [pending, startTransition] = useTransition();
  const columns = useTableColumnsMenu({
    tableId,
    exportName,
    columnsLabel: copy(pageContract, "action.columns", "Columns"),
    exportLabel: copy(pageContract, "action.export", "Export"),
  });

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
  const effectiveSelection = draft;

  function apply(next: Record<string, string>): void {
    const qs = draftSearchString(next);
    startTransition(() => {
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
    });
  }

  const hasAnyApplied = Boolean(search) || FILTERS.some((f) => filters[f.key]);

  const searchField = (
    <TextField
      type="search"
      fullWidth
      value={effectiveSelection.search ?? ""}
      onChange={(event) => setDraft({ ...draft, search: event.target.value })}
      onKeyDown={(event) => {
        if (event.key === "Enter") {
          event.preventDefault();
          apply(draft);
        }
      }}
      placeholder={copy(pageContract, "filter.search_placeholder")}
      slotProps={{
        htmlInput: { "aria-label": copy(pageContract, "filter.search_label") },
        input: {
          startAdornment: (
            <InputAdornment position="start">
              <Iconify icon="eva:search-fill" sx={{ color: "text.disabled" }} />
            </InputAdornment>
          ),
        },
      }}
    />
  );

  // MUI TextField selects (never native <select>), one per facet, in the template toolbar's
  // fixed-width leading slot. The select reports the chosen value as `event.target.value`, so the
  // staged-draft logic above is unchanged.
  const selects = FILTERS.map((filter) => (
    <TextField
      key={filter.key}
      select
      sx={orderToolbarFilterSx}
      label={copy(pageContract, filter.copyKey)}
      value={(catalog?.[filter.catalogKey] ?? []).some((entry) => entry.value === effectiveSelection[filter.key]) ? effectiveSelection[filter.key] : ""}
      disabled={!catalog}
      onChange={({ target: { value } }) => setDraft({ ...draft, [filter.key]: value })}
      slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
    >
      <MenuItem value="">{copy(pageContract, "filter.all")}</MenuItem>
      {(catalog?.[filter.catalogKey] ?? []).map((entry) => (
        <MenuItem key={entry.value} value={entry.value}>
          {entry.label}
        </MenuItem>
      ))}
    </TextField>
  ));

  return (
    <>
      <OrderTableToolbar
        filters={selects}
        search={<Box sx={orderToolbarSearchSx}>{searchField}</Box>}
        menuActions={columns.menuActions}
        menuLabel={copy(pageContract, "action.more", "More")}
        trailing={
          <>
            {/* DECIDED "no dead controls" (J2B P2-8): Apply renders only once something is staged and
                shows its loading state while the filters land; the template toolbar has no idle,
                dimmed Apply. Medium size, like the template toolbar buttons (no 56px sizeLarge). */}
            {staged || pending ? (
              <Button
                variant="contained"
                color="primary"
                onClick={() => apply(draft)}
                loading={pending}
                sx={{ flexShrink: 0, alignSelf: "center" }}
              >
                {copy(pageContract, "filter.apply")}
              </Button>
            ) : null}
            {hasAnyApplied || staged ? (
              <Button variant="outlined" color="inherit" onClick={() => apply({})} disabled={pending} sx={{ flexShrink: 0, alignSelf: "center" }}>
                {copy(pageContract, "filter.clear")}
              </Button>
            ) : null}
          </>
        }
      />
      {columns.popover}
    </>
  );
}

/** The applied server state as a plain draft record, with absent values as "". */
function toDraft(applied: Record<string, string | undefined>): Record<string, string> {
  const draft: Record<string, string> = { search: applied.search ?? "" };
  for (const filter of FILTERS) draft[filter.key] = applied[filter.key] ?? "";
  return draft;
}
