"use client";

import { Search } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { useState, useTransition } from "react";

import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import InputAdornment from "@mui/material/InputAdornment";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
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
  pathname,
  catalog,
  search,
  filters,
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
  const effectiveSelection = draft;

  function apply(next: Record<string, string>): void {
    const qs = draftSearchString(next);
    startTransition(() => {
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
    });
  }

  const hasAnyApplied = Boolean(search) || FILTERS.some((f) => filters[f.key]);

  return (
    // Template table toolbar (ecommerce ProductTableToolbar / invoice InvoiceTableToolbar): search
    // TextField with a start adornment, the facet selects, then the apply / clear actions, on the
    // Card surface. On a phone the search takes its own row and the facets pair up two per row.
    <Card
      sx={(theme) => ({
        display: "flex",
        flexWrap: "wrap",
        alignItems: "center",
        gap: 1.25,
        p: 1.5,
        mb: 1.5,
        width: "100%",
        "& > .vendor-filter-select": { flex: "0 1 10rem", minWidth: "8.75rem" },
        [theme.breakpoints.down("sm")]: {
          p: 1.25,
          "& > .vendor-filter-select, & > .MuiButton-root": { flex: "1 1 calc(50% - 5px)", minWidth: 0 },
        },
      })}
    >
      <TextField
        type="search"
        size="small"
        value={effectiveSelection.search ?? ""}
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
        slotProps={{
          htmlInput: { "aria-label": copy(pageContract, "filter.search_label") },
          input: {
            startAdornment: (
              <InputAdornment position="start">
                <Search className="ic" aria-hidden="true" />
              </InputAdornment>
            ),
          },
        }}
        sx={{ flex: { xs: "1 1 100%", sm: "1 1 16rem" }, minWidth: { xs: 0, sm: "12rem" }, maxWidth: { xs: "none", sm: "20rem" } }}
      />

      {/* MUI TextField select, not a native <select>: five OS dropdowns sat in this bar with their own
          height, font and chevron, so the register's filter row read as a different design system
          from the page it filtered. The select reports the chosen value as
          `event.target.value`, so the staged-draft logic below is unchanged. */}
      {FILTERS.map((filter) => (
        <TextField
          key={filter.key}
          select
          className="vendor-filter-select"
          label={copy(pageContract, filter.copyKey)}
          value={(catalog?.[filter.catalogKey] ?? []).some((entry) => entry.value === effectiveSelection[filter.key]) ? effectiveSelection[filter.key] : ""}
          disabled={!catalog}
          onChange={({ target: { value } }) => setDraft({ ...draft, [filter.key]: value })}
          sx={{ minWidth: { xs: 0, sm: 160 }, flexShrink: 0, maxWidth: 1 }}
          slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
        >
          <MenuItem value="">{copy(pageContract, "filter.all")}</MenuItem>
          {(catalog?.[filter.catalogKey] ?? []).map((entry) => (
            <MenuItem key={entry.value} value={entry.value}>
              {entry.label}
            </MenuItem>
          ))}
        </TextField>
      ))}

      {/* Disabled with nothing staged, so the control tells the truth about whether pressing it
          would change anything. */}
      <Button
        variant="contained"
        onClick={() => apply(draft)}
        disabled={pending || !staged}
        aria-disabled={pending || !staged}
        title={staged ? undefined : copy(pageContract, "filter.apply.nothing_staged")}
      >
        {pending ? copy(pageContract, "filter.applying") : copy(pageContract, "filter.apply")}
      </Button>

      {hasAnyApplied || staged ? (
        <Button variant="outlined" color="inherit" onClick={() => apply({})} disabled={pending}>
          {copy(pageContract, "filter.clear")}
        </Button>
      ) : null}
    </Card>
  );
}

/** The applied server state as a plain draft record, with absent values as "". */
function toDraft(applied: Record<string, string | undefined>): Record<string, string> {
  const draft: Record<string, string> = { search: applied.search ?? "" };
  for (const filter of FILTERS) draft[filter.key] = applied[filter.key] ?? "";
  return draft;
}
