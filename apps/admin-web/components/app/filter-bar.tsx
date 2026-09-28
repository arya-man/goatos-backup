"use client";

import { FILTER_SEARCH_BASIS, FILTER_SEARCH_FOLD_BASIS } from "@/components/app/filter-field-widths";
import { useState, type ReactNode } from "react";
import Badge from "@mui/material/Badge";
import Box from "@mui/material/Box";
import MuiButton from "@mui/material/Button";
import Card from "@mui/material/Card";
import InputAdornment from "@mui/material/InputAdornment";
import TextField from "@mui/material/TextField";
import { MinimalDrawer } from "@/components/app/drawer";
import { Iconify } from "@/components/minimal/iconify";
import { cx } from "@/lib/tone";

export type FilterBarProps = {
  /** Filter controls (selects, date pickers, chips). Wrap on small screens. */
  children?: ReactNode;
  /** Right-aligned actions (export, add). */
  actions?: ReactNode;
  /** Optional search field; pass the input's props. */
  search?: { value?: string; defaultValue?: string; placeholder?: string; name?: string; onChange?: (v: string) => void; ariaLabel?: string };
  /** Row under the bar, e.g. "12 results" + active filter chips. */
  summary?: ReactNode;
  /**
   * Phone fold: at <=640px the controls leave the bar and open in a Filters sheet behind this
   * button. `label` is the page's own copy ("Filters"); `count` badges the button with the number
   * of filters in effect so a closed sheet still says something is on.
   */
  fold?: { label: string; closeLabel?: string; count?: number };
  className?: string;
  /**
   * Page-level toolbar with no Card (template job/tour list: search + filter controls sit on the
   * page, above the card grid). TR1-#33: the SOP library's filter card.
   */
  bare?: boolean;
};

/**
 * The list filter toolbar: the template UserTableToolbar anatomy (filter controls, a keyword search
 * that takes the remaining width, trailing actions) on a Card, with the active-filter summary
 * (FiltersResult chips / counts) under it. Below `md` a foldable bar keeps search + actions and moves
 * the controls into the template's filters drawer (MinimalDrawer), badge = filters in effect.
 */
export function FilterBar({ children, actions, search, summary, fold, className, bare }: FilterBarProps) {
  const [open, setOpen] = useState(false);
  const foldable = Boolean(fold && children);
  return (
    <Card
      className={cx("kit-filterbar", className)}
      sx={{
        overflow: "visible",
        ...(bare ? { boxShadow: "none", bgcolor: "transparent", borderRadius: 0, "& > .kit-filterbar-row": { p: 0 }, "& > .kit-filterbar-summary": { px: 0, pt: 2, pb: 0 } } : null),
        // Inside another card (a table card) the toolbar is part of that card, as in the template list.
        ".MuiCard-root &, .card &": { boxShadow: "none", bgcolor: "transparent", borderRadius: 0 },
      }}
    >
      <Box
        className="kit-filterbar-row"
        sx={{
          p: 2.5,
          gap: 2,
          display: "flex",
          flexWrap: "wrap",
          alignItems: "center",
        }}
      >
        {children ? (
          <Box sx={{ display: foldable ? { xs: "none", md: "flex" } : "flex", flexWrap: "wrap", gap: 2, alignItems: "center", minWidth: 0 }}>{children}</Box>
        ) : null}
        {search ? (
          <TextField
            name={search.name}
            value={search.value}
            defaultValue={search.defaultValue}
            placeholder={search.placeholder}
            onChange={search.onChange ? (event) => search.onChange?.(event.target.value) : undefined}
            sx={{ flexGrow: 1, flexShrink: 1, flexBasis: foldable ? FILTER_SEARCH_FOLD_BASIS : FILTER_SEARCH_BASIS, minWidth: 0 }}
            slotProps={{
              htmlInput: { "aria-label": search.ariaLabel ?? search.placeholder },
              input: {
                startAdornment: (
                  <InputAdornment position="start">
                    <Iconify icon="eva:search-fill" sx={{ color: "text.disabled" }} />
                  </InputAdornment>
                ),
              },
            }}
          />
        ) : null}
        {foldable ? (
          <MuiButton
            variant="outlined"
            color="inherit"
            aria-expanded={open}
            onClick={() => setOpen(true)}
            startIcon={
              <Badge color="error" variant="dot" invisible={!fold?.count}>
                <Iconify icon="ic:round-filter-list" />
              </Badge>
            }
            sx={{ display: { xs: "inline-flex", md: "none" } }}
          >
            {fold?.label}
            {fold?.count ? ` (${fold.count})` : null}
          </MuiButton>
        ) : null}
        {actions ? <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1, alignItems: "center", ml: "auto" }}>{actions}</Box> : null}
      </Box>
      {summary ? <Box className="kit-filterbar-summary" sx={{ px: 2.5, pb: 2.5 }}>{summary}</Box> : null}
      {foldable ? (
        <MinimalDrawer open={open} onClose={() => setOpen(false)} title={fold?.label} aria-label={fold?.label}>
          <Box sx={{ p: 2.5, display: "flex", flexDirection: "column", gap: 2.5, "& .MuiFormControl-root": { width: 1 } }}>{children}</Box>
        </MinimalDrawer>
      ) : null}
    </Card>
  );
}
