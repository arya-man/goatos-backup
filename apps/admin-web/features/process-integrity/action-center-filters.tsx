"use client";

import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import DialogActions from "@mui/material/DialogActions";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import IconButton from "@mui/material/IconButton";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import Link from "@/components/no-prefetch-link";
import { FilterChip } from "@/components/app/list/filter-chip";
import { Iconify } from "@/components/minimal/iconify";
import { useState } from "react";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { ACTION_CENTER_CARD_SELECTOR } from "./action-center-board-parts";

type FilterLink = {
  label: string;
  href: string;
  active?: boolean;
  count?: number;
};

export function ActionCenterFiltersButton({
  pageContract,
  label,
  mode = "filters",
  rowsLabel,
  clearHref,
  stateLinks,
  severityLinks,
}: {
  pageContract: AdminUiPageContract;
  label: string;
  mode?: "filters" | "my";
  rowsLabel: string;
  clearHref: string;
  stateLinks: FilterLink[];
  severityLinks: FilterLink[];
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [owner, setOwner] = useState("");
  const icon = mode === "my" ? "solar:users-group-rounded-bold" : "eva:search-fill";
  const title = mode === "my" ? copy(pageContract, "filter.my_tasks.title") : copy(pageContract, "filter.drawer.title");

  function applyLocalFilters(nextQuery = query, nextOwner = owner) {
    const q = nextQuery.trim().toLowerCase();
    const o = nextOwner.trim().toLowerCase();
    for (const card of Array.from(document.querySelectorAll<HTMLElement>(ACTION_CENTER_CARD_SELECTOR))) {
      const text = (card.textContent ?? "").toLowerCase();
      const match = (!q || text.includes(q)) && (!o || text.includes(o));
      card.style.display = match ? "" : "none";
    }
  }

  function clearLocalFilters() {
    setQuery("");
    setOwner("");
    for (const card of Array.from(document.querySelectorAll<HTMLElement>(ACTION_CENTER_CARD_SELECTOR))) {
      card.style.display = "";
    }
  }

  // TR1-#7: the template Dialog (portal, visible backdrop, focus trap, Escape / backdrop close), not a
  // hand-made `div.modal.on.card` with no backdrop layer. guard: action-center-filters-dialog
  return (
    <>
      <Button variant="outlined" color="inherit" size="small" startIcon={<Iconify icon={icon} />} onClick={() => setOpen(true)} aria-haspopup="dialog">
        {label}
      </Button>
      <Dialog fullWidth maxWidth="xs" open={open} onClose={() => setOpen(false)} aria-label={title}>
        <DialogTitle sx={{ display: "flex", alignItems: "center", gap: 1 }}>
          <Box component="span" sx={{ flexGrow: 1 }}>{title}</Box>
          <IconButton onClick={() => setOpen(false)} aria-label={copy(pageContract, "filter.close_button_label")}>
            <Iconify icon="mingcute:close-line" />
          </IconButton>
        </DialogTitle>
        <DialogContent sx={{ display: "flex", flexDirection: "column", gap: 2.5, pt: 1 }}>
          <Typography variant="body2" sx={{ color: "text.secondary" }}>{rowsLabel}</Typography>
          <TextField
            fullWidth
            slotProps={{ inputLabel: { shrink: true } }}
            label={copy(pageContract, "filter.search_label")}
            value={query}
            placeholder={copy(pageContract, "filter.search_placeholder")}
            onChange={(event) => {
              setQuery(event.target.value);
              applyLocalFilters(event.target.value, owner);
            }}
          />
          <Facet title={copy(pageContract, "filter.work_state.title")} links={stateLinks} onPick={() => setOpen(false)} />
          <Facet title={copy(pageContract, "filter.severity.title")} links={severityLinks} onPick={() => setOpen(false)} />
          <TextField
            fullWidth
            slotProps={{ inputLabel: { shrink: true } }}
            label={copy(pageContract, "filter.owner_label")}
            value={owner}
            placeholder={copy(pageContract, "filter.owner_placeholder")}
            onChange={(event) => {
              setOwner(event.target.value);
              applyLocalFilters(query, event.target.value);
            }}
          />
        </DialogContent>
        <DialogActions sx={{ gap: 1.5, "& > :not(style) ~ :not(style)": { ml: 0 } }}>
          {/* ONE Clear all (URL facets + the local search / owner text) and Done, no lone "i" and no
              Clear all / Clear local outlined pair (TR2-P2-7; guard: action-center-dialog-actions). */}
          <Button
            component={Link}
            href={clearHref}
            replace
            scroll={false}
            variant="outlined"
            color="inherit"
            onClick={() => {
              clearLocalFilters();
              setOpen(false);
            }}
          >
            {copy(pageContract, "filter.clear_all")}
          </Button>
          <Box sx={{ flexGrow: 1 }} />
          <Button
            variant="contained"
            color="primary"
            onClick={() => {
              applyLocalFilters();
              setOpen(false);
            }}
          >
            {copy(pageContract, "filter.done")}
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}

function Facet({ title, links, onPick }: { title: string; links: FilterLink[]; onPick: () => void }) {
  const shown = links
    // Hide empty buckets — a chip that reads "0" is dead microcopy. Keep the active one so the
    // current selection never vanishes, and keep count-less facets (e.g. severity) untouched.
    .filter((link) => link.active || link.count !== 0);
  return (
    <div>
      <Typography variant="subtitle2" sx={{ mb: 1 }}>
        {title}
      </Typography>
      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }} onClick={onPick}>
        {shown.map((link) => (
          <FilterChip
            key={link.label}
            href={link.href}
            replace
            on={link.active}
            label={typeof link.count === "number" ? `${link.label} ${link.count}` : link.label}
          />
        ))}
      </Box>
    </div>
  );
}
