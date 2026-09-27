"use client";

import { useId, useRef, useState } from "react";
import { InfoHint } from "@/components/app/info-hint";
import Link from "@/components/no-prefetch-link";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Chip from "@mui/material/Chip";
import InputAdornment from "@mui/material/InputAdornment";
import TextField from "@mui/material/TextField";
import { DrawerSection, MinimalDrawer } from "@/components/app/drawer";
import { Iconify } from "@/components/minimal/iconify";
import { copy, optionGroup, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";

export interface VaccinationFilterModalProps {
  pageContract: AdminUiPageContract;
  title: string;
  searchReason: string;
  filterReason: string;
  rowsLabel: string;
  actionHref?: string;
  actionLabel?: string;
  quickTerms?: AdminUiOption[];
  facets: string[];
}

function filterRoot(node: HTMLElement | null): ParentNode {
  return node?.closest("[data-filter-scope]") ?? node?.closest(".card") ?? document;
}

function filterableRows(root: ParentNode): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>("[data-filter-row], tbody tr, .pexr, .wfrow, .task"));
}

export function VisibleTableSearch({
  pageContract,
  label,
  placeholder,
}: {
  pageContract: AdminUiPageContract;
  label: string;
  placeholder?: string;
}) {
  const inputRef = useRef<HTMLInputElement>(null);

  function apply(query: string) {
    const rows = filterableRows(filterRoot(inputRef.current));
    const q = query.trim().toLowerCase();
    for (const row of rows) {
      const haystack = (row.textContent ?? "").toLowerCase();
      row.style.display = !q || haystack.includes(q) ? "" : "none";
    }
  }

  // Template toolbar search (UserTableToolbar TextField + magnifier) that filters the rows on screen.
  return (
    <TextField
      inputRef={inputRef}
      title={copy(pageContract, "filter.search_visible_rows")}
      placeholder={placeholder ?? copy(pageContract, "filter.search_placeholder")}
      onChange={(event) => apply(event.target.value)}
      sx={{ flex: "1 1 220px", minWidth: { xs: 1, sm: 200 } }}
      slotProps={{
        htmlInput: { "aria-label": label },
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
}

export function VaccinationFilterButton({
  pageContract,
  title,
  searchReason,
  filterReason,
  rowsLabel,
  actionHref,
  actionLabel,
  quickTerms,
  facets,
}: VaccinationFilterModalProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [activeFacet, setActiveFacet] = useState("all");
  const [filteredCount, setFilteredCount] = useState<number | null>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);
  const searchId = useId();
  const facetTerms = quickTerms ?? optionGroup(pageContract, "filter_quick_terms");


  function applyVisibleTableFilter(nextQuery = query, nextFacet = activeFacet) {
    const rows = filterableRows(filterRoot(buttonRef.current));
    const q = nextQuery.trim().toLowerCase();
    const facet = nextFacet === "all" ? "" : nextFacet;
    let shown = 0;
    for (const row of rows) {
      const haystack = (row.textContent ?? "").toLowerCase();
      const match = (!q || haystack.includes(q)) && (!facet || haystack.includes(facet));
      row.style.display = match ? "" : "none";
      if (match) shown += 1;
    }
    setFilteredCount(shown);
  }

  function clearVisibleTableFilter() {
    for (const row of filterableRows(filterRoot(buttonRef.current))) {
      row.style.display = "";
    }
    setQuery("");
    setActiveFacet("all");
    setFilteredCount(null);
  }

  return (
    <>
      <Button
        ref={buttonRef}
        variant="outlined"
        color="inherit"
        onClick={() => setOpen(true)}
        title={filterReason}
        aria-haspopup="dialog"
        startIcon={<Iconify icon="ic:round-filter-list" />}
      >
        {copy(pageContract, "action.filters")}
      </Button>
      {/* Template filters drawer (calendar filters shell): sections, then the action footer. */}
      <MinimalDrawer
        open={open}
        onClose={() => setOpen(false)}
        title={title}
        aria-label={title}
        onReset={clearVisibleTableFilter}
        canReset={filteredCount !== null}
        footer={
          <>
            <Button variant="outlined" color="inherit" onClick={clearVisibleTableFilter}>
              {copy(pageContract, "action.clear")}
            </Button>
            <Button
              variant="contained" color="primary"
              title={filterReason}
              onClick={() => {
                applyVisibleTableFilter();
                setOpen(false);
              }}
            >
              {copy(pageContract, "filter.apply_filters")}
            </Button>
            {actionHref && actionLabel ? (
              <Button component={Link} href={actionHref} variant="contained" color="primary" onClick={() => setOpen(false)}>
                {actionLabel}
              </Button>
            ) : null}
          </>
        }
      >
        <DrawerSection title={copy(pageContract, "filter.search_rows_label")}>
          <TextField
            id={searchId}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={searchReason || copy(pageContract, "filter.search_visible_rows")}
            slotProps={{ htmlInput: { "aria-label": copy(pageContract, "filter.search_rows_label") } }}
          />
          <Box sx={{ typography: "body2", color: "text.secondary", display: "flex", alignItems: "center", gap: 0.75 }}>
            {filteredCount === null ? rowsLabel : `${filteredCount} ${copy(pageContract, "pager.matching_rows")}`}
            <InfoHint text={copy(pageContract, "filter.apply_immediately")} />
          </Box>
        </DrawerSection>
        <DrawerSection title={copy(pageContract, "filter.facets_label")}>
          <Box role="group" aria-label={`${title} ${copy(pageContract, "filter.quick_filters_aria")}`} sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
            {facetTerms.map((facet) => (
              <Chip
                key={facet.key}
                clickable
                label={facet.label}
                color={activeFacet === facet.key ? "primary" : "default"}
                variant={activeFacet === facet.key ? "filled" : "outlined"}
                aria-pressed={activeFacet === facet.key}
                onClick={() => {
                  setActiveFacet(facet.key);
                  applyVisibleTableFilter(query, facet.key);
                }}
              />
            ))}
          </Box>
          <Box sx={{ typography: "caption", color: "text.secondary", display: "flex", alignItems: "center", gap: 0.75 }}>
            <InfoHint text={`${copy(pageContract, "filter.available_columns")}: ${facets.join(copy(pageContract, "filter.column_separator"))}`} />
          </Box>
        </DrawerSection>
      </MinimalDrawer>
    </>
  );
}
