"use client";

import { useRouter } from "next/navigation";
import { LT_BLOCK_MB, LT_FILTER_MIN } from "./live-tracker-layout";
import { useTransition, type ReactNode } from "react";
import { X } from "lucide-react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import Chip from "@mui/material/Chip";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import Typography from "@mui/material/Typography";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

export type LiveFilterChoice = { value: string; label: string; href: string };

export type LiveFilterSpec = {
  id: string;
  label: string;
  allLabel: string;
  icon?: "layers" | "syringe" | "user";
  selected: string;
  choices: LiveFilterChoice[];
  clearHref: string;
};

// The persistent filter bar. Every option's destination href is computed on the SERVER through
// scopeHref, so this component never assembles a URL and never reads a scope key — it only navigates
// to an href it was handed. That is what keeps the top-bar park/date scope intact across a filter
// change instead of being silently dropped.
export function LiveTrackerFilters({
  filters,
  clearAllHref,
  optionsTruncated,
  pageContract,
  extra,
}: {
  /** A trailing field on the same grid (the live refresh interval on a live day). */
  extra?: ReactNode;
  filters: LiveFilterSpec[];
  clearAllHref: string | null;
  optionsTruncated: boolean;
  pageContract: AdminUiPageContract;
}) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();

  function go(href: string) {
    startTransition(() => {
      router.push(href, { scroll: false });
    });
  }

  const active = filters.filter((filter) => filter.selected !== "");

  // Template list toolbar card (UserTableToolbar + UserTableFiltersResult): outlined TextField
  // selects that stack full-width on a phone, applied filters as MUI Chips with a Clear action.
  return (
    <Card aria-busy={isPending} sx={{ mb: LT_BLOCK_MB }}>
      <Box
        sx={{
          p: 2.5,
          gap: 2,
          display: "grid",
          gridTemplateColumns: { xs: "1fr", sm: `repeat(auto-fill, minmax(${LT_FILTER_MIN}px, 1fr))` },
          alignItems: "center",
        }}
      >
        {filters.map((filter) => (
          <TextField
            key={filter.id}
            select
            fullWidth
            label={filter.label}
            value={filter.choices.some((choice) => choice.value === filter.selected) ? filter.selected : ""}
            onChange={({ target: { value } }) => {
              const next = filter.choices.find((choice) => choice.value === value);
              go(next ? next.href : filter.clearHref);
            }}
            slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
          >
            <MenuItem value="">{filter.allLabel}</MenuItem>
            {filter.choices.map((c) => (
              <MenuItem key={c.value} value={c.value}>
                {c.label}
              </MenuItem>
            ))}
          </TextField>
        ))}
        {extra}
      </Box>

      {active.length > 0 || clearAllHref || optionsTruncated ? (
        <Box sx={{ px: 2.5, pb: 2.5, display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1 }}>
          {active.map((filter) => {
            const choice = filter.choices.find((candidate) => candidate.value === filter.selected);
            // NEVER fall through to the raw value. filter.selected is an internal identifier (a park or
            // shed uuid, a vaccine family token), and the vocabulary is compiled from the day's OWN
            // rows — so a selection that has no work on this drive day is simply absent from choices.
            // The old fallback then printed the uuid in the chip while the select beside it, having
            // no matching option, rendered "All parks": two controls contradicting each other while
            // the data really was narrowed.
            return (
              <Chip
                key={filter.id}
                size="small"
                label={choice?.label ?? copy(pageContract, "filter.unlisted_selection")}
                onDelete={() => go(filter.clearHref)}
                deleteIcon={<X aria-label={`${copy(pageContract, "filter.remove_one")} — ${filter.label}`} role="button" />}
              />
            );
          })}
          {clearAllHref ? (
            <Button color="error" onClick={() => go(clearAllHref)} sx={{ minHeight: { xs: 44, sm: 36 } }}>
              {copy(pageContract, "filter.clear_all")}
            </Button>
          ) : null}
          {optionsTruncated ? (
            <Typography variant="caption" sx={{ color: "text.disabled", ml: { sm: "auto" } }}>
              {copy(pageContract, "filter.truncated_note")}
            </Typography>
          ) : null}
        </Box>
      ) : null}
    </Card>
  );
}
