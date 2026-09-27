"use client";

import { useTransition } from "react";
import { useRouter } from "next/navigation";

import Box from "@mui/material/Box";
import List from "@mui/material/List";
import Typography from "@mui/material/Typography";
import ListItemText from "@mui/material/ListItemText";
import ListItemButton from "@mui/material/ListItemButton";

import Link from "@/components/no-prefetch-link";
import { MinimalDrawer } from "@/components/app/drawer";
import { Iconify } from "@/components/minimal/iconify";

/** One choice in a filter group: a URL the server built, so the choice lives in the address bar. */
export type CalendarFilterOption = { key: string; label: string; href?: string; active: boolean; disabled?: boolean };
export type CalendarFilterGroup = { id: string; label: string; options: CalendarFilterOption[] };

/** Serializable filter model the /calendar server page hands to the client view. */
export type CalendarFiltersModel = {
  title: string;
  closeLabel: string;
  /** Accessible name of the toolbar filter button. */
  openLabel: string;
  groups: CalendarFilterGroup[];
  /** A non-default choice is applied (the reset dot on the button and in the drawer head). */
  canReset: boolean;
  resetHref: string;
};

/**
 * The template calendar filters drawer (sections/calendar/calendar-filters.tsx: 320px right drawer,
 * "Filters" head with the reset dot and close, `subtitle2` section headings over the choices) fed
 * our URL filters: the upcoming / history window, the owner lane and the workstream. Each choice is
 * a soft navigation (replace, scroll kept); the grid swaps to its skeleton through the view's
 * UrlSuspense while the drawer closes.
 */
export function CalendarFilters({ open, onClose, model }: { open: boolean; onClose: () => void; model: CalendarFiltersModel }) {
  const router = useRouter();
  const [, startTransition] = useTransition();
  return (
    <MinimalDrawer
      open={open}
      onClose={onClose}
      title={model.title}
      closeLabel={model.closeLabel}
      width={320}
      onReset={() => {
        onClose();
        startTransition(() => router.replace(model.resetHref, { scroll: false }));
      }}
      canReset={model.canReset}
    >
      {model.groups.map((group) => (
        <Box key={group.id} sx={{ my: 3, display: "flex", flexDirection: "column" }}>
          <Typography variant="subtitle2" sx={{ px: 2.5, mb: 1 }} id={`calendar-filter-${group.id}`}>
            {group.label}
          </Typography>
          <List disablePadding aria-labelledby={`calendar-filter-${group.id}`}>
            {group.options.map((option) => {
              const body = (
                <>
                  <ListItemText primary={option.label} slotProps={{ primary: { sx: { typography: "body2" } } }} />
                  {option.active ? <Iconify icon="eva:checkmark-fill" width={18} sx={{ color: "primary.main" }} /> : null}
                </>
              );
              return option.href && !option.disabled && !option.active ? (
                <ListItemButton
                  key={option.key}
                  component={Link}
                  href={option.href}
                  replace
                  scroll={false}
                  onClick={onClose}
                  sx={{ px: 2.5, minHeight: "var(--tap-min)" }}
                >
                  {body}
                </ListItemButton>
              ) : (
                <ListItemButton
                  key={option.key}
                  selected={option.active}
                  disabled={option.disabled}
                  aria-current={option.active ? "true" : undefined}
                  onClick={option.active ? onClose : undefined}
                  sx={{ px: 2.5, minHeight: "var(--tap-min)" }}
                >
                  {body}
                </ListItemButton>
              );
            })}
          </List>
        </Box>
      ))}
    </MinimalDrawer>
  );
}
