"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import Link from "@/components/no-prefetch-link";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Chip from "@mui/material/Chip";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { DrawerSection, MinimalDrawer } from "@/components/app/drawer";
import { useBackCloses } from "@/components/use-back-closes";
import { parseScope } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";

// Herd Register filters: the template filters drawer (calendar-filters shell via MinimalDrawer, which
// portals, traps focus and restores it; Escape, scrim, X and Back close it). Chips are MUI Chip, the
// search and extra facets are template TextFields.
//
// Backed facets wire to /goats/search (Sex→sex, Breed→breed, Search→q). Park scope is owned by the top
// bar (Scope Chrome Rule), shown read-only here. Additional facets preserve their chosen values in the URL
// and become active automatically when the goat-search contract starts consuming them.

const FORM_ID = "herd-filters-form";

interface HerdFiltersModalProps {
  open: boolean;
  pageContract: AdminUiPageContract;
  searchParams?: RouteSearchParams;
  onClose: () => void;
}

export function HerdFiltersModal({ open, pageContract, searchParams = {}, onClose }: HerdFiltersModalProps) {
  useBackCloses(open, onClose);
  const title = copy(pageContract, "filter.drawer.title");
  return (
    <MinimalDrawer
      open={open}
      onClose={onClose}
      title={title}
      aria-label={title}
      closeLabel={copy(pageContract, "filter.drawer.close_label")}
      footer={
        <>
          <Button component={Link} href={clearAllHref(searchParams)} replace scroll={false} variant="outlined" color="inherit" onClick={onClose}>
            {copy(pageContract, "filter.clear_all")}
          </Button>
          <Button type="submit" form={FORM_ID} variant="contained" color="primary">
            {copy(pageContract, "filter.apply_filters")}
          </Button>
        </>
      }
    >
      {/* Mounted per open, so the chips start from the live URL every time the drawer opens. */}
      {open ? <HerdFiltersForm pageContract={pageContract} searchParams={searchParams} onClose={onClose} /> : null}
    </MinimalDrawer>
  );
}

const PATHNAME = "/counts/herd";

// Preserve the top-bar scope (scope_mode/park/as_of) on every navigation so the bar never disagrees.
function scopeParams(searchParams: RouteSearchParams): URLSearchParams {
  const scope = parseScope(searchParams);
  const params = new URLSearchParams();
  if (scope.mode === "park") {
    params.set("scope_mode", "park");
    if (scope.parkId) params.set("park", scope.parkId);
  } else {
    params.set("scope_mode", "company");
  }
  if (scope.asOf) params.set("as_of", scope.asOf);
  return params;
}

function clearAllHref(searchParams: RouteSearchParams): string {
  const qs = scopeParams(searchParams).toString();
  return qs ? `${PATHNAME}?${qs}` : PATHNAME;
}

function HerdFiltersForm({ pageContract, searchParams, onClose }: { pageContract: AdminUiPageContract; searchParams: RouteSearchParams; onClose: () => void }) {
  const router = useRouter();
  const [, startTransition] = useTransition();
  const breeds = optionGroup(pageContract, "herd_filter_breeds");
  const sexOptions = optionGroup(pageContract, "herd_filter_sexes");
  const extraFacets = optionGroup(pageContract, "herd_filter_extra_facets");
  const [breed, setBreed] = useState(one(searchParams, "breed") ?? "");
  const [sex, setSex] = useState(one(searchParams, "sex") ?? "");

  const handleApplyFilters = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const data = new FormData(e.currentTarget);
    const params = scopeParams(searchParams);

    const searchVal = String(data.get("q") ?? "").trim();
    if (searchVal) params.set("q", searchVal);
    if (breed) params.set("breed", breed);
    if (sex) params.set("sex", sex);
    for (const facet of extraFacets) {
      const value = String(data.get(facet.key) ?? "").trim();
      if (value) params.set(facet.key, value);
    }

    const qs = params.toString();
    startTransition(() => {
      router.replace(qs ? `${PATHNAME}?${qs}` : PATHNAME, { scroll: false });
    });
    onClose();
  };

  return (
    <Box component="form" id={FORM_ID} onSubmit={handleApplyFilters}>
      {/* Park is owned by the top-bar scope (Scope Chrome Rule) — read-only here, not a duplicate filter. */}
      <DrawerSection title={copy(pageContract, "filter.scope_label")}>
        <Typography variant="body2" sx={{ color: "text.secondary" }}>
          {copy(pageContract, "filter.scope_readonly")}
        </Typography>
      </DrawerSection>

      <DrawerSection title={copy(pageContract, "filter.herd_search_label")}>
        <TextField
          id="filter-search"
          name="q"
          fullWidth
          defaultValue={one(searchParams, "q") ?? ""}
          placeholder={copy(pageContract, "filter.herd_search_placeholder")}
          slotProps={{ htmlInput: { "aria-label": copy(pageContract, "filter.herd_search_label") } }}
        />
      </DrawerSection>

      <DrawerSection title={copy(pageContract, "filter.sex_label")}>
        <FacetChips options={sexOptions} facet="sex" value={sex} onChange={setSex} />
      </DrawerSection>

      <DrawerSection title={copy(pageContract, "filter.breed_label")}>
        <FacetChips options={breeds} facet="breed" value={breed} onChange={setBreed} />
      </DrawerSection>

      {extraFacets.map((facet) => (
        <DrawerSection key={facet.key} title={facet.label}>
          <TextField
            name={facet.key}
            fullWidth
            defaultValue={one(searchParams, facet.key) ?? ""}
            placeholder={facet.title}
            slotProps={{ htmlInput: { "aria-label": facet.label } }}
          />
        </DrawerSection>
      ))}
    </Box>
  );
}

/** Single-choice chip row: pressing the selected chip clears it (template product-filters chips). */
function FacetChips({
  options,
  facet,
  value,
  onChange,
}: {
  options: { key: string; label: string }[];
  facet: string;
  value: string;
  onChange: (next: string) => void;
}) {
  return (
    <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
      {options.map((option) => {
        const on = value === option.key;
        return (
          <Chip
            key={option.key}
            clickable
            label={option.label}
            data-facet={facet}
            data-value={option.key}
            data-on={on ? "true" : "false"}
            aria-pressed={on}
            color={on ? "primary" : "default"}
            variant={on ? "filled" : "outlined"}
            onClick={() => onChange(on ? "" : option.key)}
          />
        );
      })}
    </Box>
  );
}
