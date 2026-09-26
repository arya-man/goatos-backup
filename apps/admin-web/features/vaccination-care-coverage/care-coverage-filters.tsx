"use client";

import { useMemo, useState, useTransition } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { ChevronDown, Layers } from "lucide-react";
import { usePopover } from "minimal-shared/hooks";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Checkbox from "@mui/material/Checkbox";
import MenuItem from "@mui/material/MenuItem";
import MenuList from "@mui/material/MenuList";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { CustomPopover } from "@/components/minimal/custom-popover";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { PENS_PARAM, encodePens } from "./pen-param";

export type CareCoverageParkChoice = { value: string; label: string; href: string };
export type CareCoveragePenChoice = { value: string; label: string };


// Care Coverage's filter bar, in the Live Drive Tracker's .lt-fbar look. Park is the template MUI
// select whose destinations the SERVER computed (it writes the shared top-bar `park` key). Pen is
// a checkbox multi-select on the template popover: ticks are STAGED in the open dropdown and
// nothing navigates until Apply, so ticking five pens is one page load, not five. Filtering itself
// happens server-side on the next render — never client-side row hiding.
export function CareCoverageFilters({
  parkChoices,
  parkSelected,
  parkClearHref,
  penChoices,
  penSelected,
  clearAllHref,
  pageContract,
}: {
  parkChoices: CareCoverageParkChoice[];
  parkSelected: string;
  parkClearHref: string;
  penChoices: CareCoveragePenChoice[];
  penSelected: string[];
  clearAllHref: string | null;
  pageContract: AdminUiPageContract;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const [isPending, startTransition] = useTransition();
  const [optimisticParkSelected, setOptimisticParkSelected] = useState<string | null>(null);
  const selectedParkValue = optimisticParkSelected ?? parkSelected;

  function go(href: string, nextParkSelected?: string) {
    if (nextParkSelected !== undefined) setOptimisticParkSelected(nextParkSelected);
    startTransition(() => router.push(href, { scroll: false }));
  }

  // Rewrites ONLY the pen key (and the page cursor, which belongs to the old result), keeping the
  // park scope, page size and every other parameter as they are.
  function hrefForPens(pens: string[]): string {
    const next = new URLSearchParams(searchParams?.toString() ?? "");
    next.delete(PENS_PARAM);
    next.delete("cc_cursor");
    next.delete("cc_from");
    if (pens.length) next.set(PENS_PARAM, encodePens(pens));
    const qs = next.toString();
    return qs ? `${pathname}?${qs}` : pathname;
  }

  const parkLabel = parkChoices.find((choice) => choice.value === parkSelected)?.label;
  const penLabels = new Map(penChoices.map((choice) => [choice.value, choice.label]));
  if (optimisticParkSelected !== null && optimisticParkSelected === parkSelected) setOptimisticParkSelected(null);

  return (
    <div className={`lt-fbar cc-fbar${isPending ? " wfbusy" : ""}`} aria-busy={isPending}>
      <span className="lt-fsel">
        <Layers className="ic" size={13} aria-hidden="true" />
        <TextField
          select
          label={copy(pageContract, "filter.park")}
          value={parkChoices.some((choice) => choice.value === selectedParkValue) ? selectedParkValue : ""}
          onChange={({ target: { value } }) => {
            const next = parkChoices.find((choice) => choice.value === value);
            go(next ? next.href : parkClearHref, next?.value ?? "");
          }}
          sx={{ minWidth: { xs: 0, sm: 180 }, flexShrink: 0, maxWidth: 1 }}
          slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
        >
          <MenuItem value="">{copy(pageContract, "filter.all_parks")}</MenuItem>
          {parkChoices.map((choice) => (
            <MenuItem key={choice.value} value={choice.value}>
              {choice.label}
            </MenuItem>
          ))}
        </TextField>
      </span>

      <PenMultiSelect
        choices={penChoices}
        selected={penSelected}
        pageContract={pageContract}
        onApply={(pens) => go(hrefForPens(pens))}
      />

      <span className="lt-chips">
        {parkSelected ? (
          <span className="achip">
            {parkLabel ?? copy(pageContract, "filter.unlisted_selection")}
            <b
              role="button"
              tabIndex={0}
              aria-label={`${copy(pageContract, "filter.remove_one")} — ${copy(pageContract, "filter.park")}`}
              onClick={() => go(parkClearHref)}
              onKeyDown={(event) => {
                if (event.key === "Enter" || event.key === " ") go(parkClearHref);
              }}
            >
              ×
            </b>
          </span>
        ) : null}
        {penSelected.map((pen) => {
          const without = hrefForPens(penSelected.filter((candidate) => candidate !== pen));
          const label = penLabels.get(pen) ?? copy(pageContract, "filter.unlisted_selection");
          return (
            <span className="achip" key={pen}>
              {label}
              <b
                role="button"
                tabIndex={0}
                aria-label={`${copy(pageContract, "filter.remove_one")} — ${label}`}
                onClick={() => go(without)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") go(without);
                }}
              >
                ×
              </b>
            </span>
          );
        })}
        {clearAllHref ? (
          <Button size="small" color="error" onClick={() => go(clearAllHref)}>
            {copy(pageContract, "filter.clear_all")}
          </Button>
        ) : null}
      </span>
      <span className="lt-fnote">{copy(pageContract, "filter.apply_note")}</span>
    </div>
  );
}

function PenMultiSelect({
  choices,
  selected,
  pageContract,
  onApply,
}: {
  choices: CareCoveragePenChoice[];
  selected: string[];
  pageContract: AdminUiPageContract;
  onApply: (pens: string[]) => void;
}) {
  // Template popover: outside click and Escape close it without applying.
  const popover = usePopover();
  const [staged, setStaged] = useState<string[]>(selected);
  const [query, setQuery] = useState("");

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return needle ? choices.filter((choice) => choice.label.toLowerCase().includes(needle)) : choices;
  }, [choices, query]);
  const stagedSet = new Set(staged);

  const summary =
    selected.length === 0
      ? copy(pageContract, "filter.all_pens")
      : selected.length === 1
        ? (choices.find((choice) => choice.value === selected[0])?.label ?? copy(pageContract, "filter.unlisted_selection"))
        : `${selected.length} ${copy(pageContract, "label.pens")}`;

  function toggle(value: string) {
    setStaged((current) => (current.includes(value) ? current.filter((candidate) => candidate !== value) : [...current, value]));
  }

  function apply() {
    // Keep the choice list's own order, so the URL (and the chips) read in pen order.
    const order = new Map(choices.map((choice, index) => [choice.value, index]));
    const next = [...staged].sort((a, b) => (order.get(a) ?? 1e9) - (order.get(b) ?? 1e9));
    popover.onClose();
    setQuery("");
    onApply(next);
  }

  return (
    <span className="lt-fsel cc-pensel">
      <Button
        color="inherit"
        aria-haspopup="listbox"
        aria-expanded={popover.open}
        aria-label={copy(pageContract, "filter.pen")}
        endIcon={<ChevronDown className="ic" size={14} aria-hidden="true" />}
        onClick={(event) => {
          // Opening seeds the staged ticks from the APPLIED selection, so a chip removed or a
          // park switched since the last Apply is reflected in the boxes.
          setStaged(selected);
          popover.onOpen(event);
        }}
        sx={{ justifyContent: "space-between", minWidth: 150, maxWidth: { xs: 1, sm: 220 }, fontWeight: "fontWeightSemiBold", minHeight: { xs: 44, sm: 36 } }}
      >
        <Box component="span" sx={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", textAlign: "left", flex: "1 1 auto" }}>
          {summary}
        </Box>
      </Button>
      <CustomPopover
        open={popover.open}
        anchorEl={popover.anchorEl}
        onClose={popover.onClose}
        slotProps={{ arrow: { placement: "top-left" }, paper: { sx: { width: 300, maxWidth: "calc(100vw - var(--sp-4))" } } }}
      >
        <Box sx={{ p: 1, display: "flex", flexDirection: "column", gap: 1 }}>
          <TextField
            size="small"
            type="search"
            fullWidth
            placeholder={copy(pageContract, "filter.search_pens")}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            slotProps={{ htmlInput: { "aria-label": copy(pageContract, "filter.search_pens") } }}
          />
          <MenuList
            role="listbox"
            aria-multiselectable="true"
            aria-label={copy(pageContract, "filter.pen")}
            sx={{ maxHeight: { xs: "min(50vh, 320px)", sm: 280 }, overflowY: "auto" }}
          >
            {visible.length === 0 ? (
              <Typography component="li" variant="body2" sx={{ color: "text.secondary", p: 1 }}>
                {copy(pageContract, "filter.no_pen_match")}
              </Typography>
            ) : null}
            {visible.map((choice) => {
              const checked = stagedSet.has(choice.value);
              return (
                <MenuItem key={choice.value} role="option" aria-selected={checked} onClick={() => toggle(choice.value)} sx={{ gap: 1, minHeight: { xs: 44, sm: 36 } }}>
                  <Checkbox size="small" checked={checked} tabIndex={-1} disableRipple sx={{ p: 0 }} slotProps={{ input: { "aria-label": choice.label } }} />
                  <Box component="span" sx={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    {choice.label}
                  </Box>
                </MenuItem>
              );
            })}
          </MenuList>
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, borderTop: 1, borderColor: "divider", pt: 1 }}>
            <Typography variant="caption" sx={{ color: "text.secondary" }}>
              {staged.length} {copy(pageContract, "filter.selected")}
            </Typography>
            <Box sx={{ flex: 1 }} />
            <Button size="small" color="inherit" onClick={() => setStaged([])} disabled={staged.length === 0}>
              {copy(pageContract, "filter.clear")}
            </Button>
            <Button size="small" variant="contained" onClick={apply}>
              {copy(pageContract, "filter.apply")}
            </Button>
          </Box>
        </Box>
      </CustomPopover>
    </span>
  );
}
