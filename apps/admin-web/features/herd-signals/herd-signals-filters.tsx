"use client";

import { useRef, useState } from "react";
import Badge from "@mui/material/Badge";
import Box from "@mui/material/Box";
import MuiButton from "@mui/material/Button";
import Card from "@mui/material/Card";
import CircularProgress from "@mui/material/CircularProgress";
import IconButton from "@mui/material/IconButton";
import InputAdornment from "@mui/material/InputAdornment";
import TextField from "@mui/material/TextField";
import { MinimalDrawer } from "@/components/app/drawer";
import { Iconify } from "@/components/minimal/iconify";

import MenuItem from "@mui/material/MenuItem";
import { MOVEMENT_LABEL, MAPPING_LABEL, PATTERN_LABEL, RISK_LABEL } from "./format";
import { useHerdSignalsNav } from "./herd-signals-nav-context";
import { HerdSignalsKpiChip } from "./herd-signals-kpis";
import { herdSignalsHref, type HerdSignalsParams } from "./params";
import { HERD_SIGNALS_SEARCH_SX, HERD_SIGNALS_SELECT_MIN } from "./herd-signals-layout";
import hs from "./herd-signals-filters.module.css";
import { Label } from "@/components/minimal/label";

export type ShedOption = { id: string; label: string };

const MOVEMENT_OPTIONS = Object.entries(MOVEMENT_LABEL) as [keyof typeof MOVEMENT_LABEL, string][];
// Filter-dropdown wording is "<state> only" (mock/herd-signals-mock.html #fMap), distinct from the
// bare MAPPING_LABEL used elsewhere (table Status column, mapping-table quick-filter buttons).
const MAPPING_FILTER_LABEL: Record<keyof typeof MAPPING_LABEL, string> = {
  mapped: "Mapped only",
  unmapped: "Unmapped only",
  conflict: "Conflicts only",
};
const MAPPING_OPTIONS = Object.entries(MAPPING_FILTER_LABEL) as [keyof typeof MAPPING_LABEL, string][];
// Only the alert-shaped pattern states are offered here (Section 8) — "normal" is not a useful
// filter choice since it is the majority of every fleet.
const PATTERN_OPTIONS: [string, string][] = (["inactive", "quiet_watch", "spike", "recovered", "missing"] as const).map(
  (key) => [key, PATTERN_LABEL[key]],
);
const RISK_OPTIONS: [string, string][] = (["high", "watch", "low"] as const).map((key) => [key, RISK_LABEL[key]]);
const LIVE_WINDOW_OPTIONS = [
  ["30s", "30 sec"],
  ["1m", "1 min"],
  ["5m", "5 min"],
  ["15m", "15 min"],
] as const;
const OWN_BASELINE_OPTIONS = [
  ["off", "Off"],
  ["24h", "Last 24h"],
] as const;
const SHED_BASELINE_OPTIONS = [
  ["now", "Now"],
] as const;

export function HerdSignalsFilters({
  params,
  sheds,
}: {
  params: HerdSignalsParams;
  sheds: ShedOption[];
}) {
  const { isPending, navigate } = useHerdSignalsNav();
  // Synced from the URL's own hs_q on every navigation, using React's "adjust state during render"
  // pattern (react.dev/learn/you-might-not-need-an-effect) rather than an Effect that calls
  // setState synchronously — the debounced local edits below still take priority between renders.
  const [syncedFromProp, setSyncedFromProp] = useState(params.q);
  const [q, setQ] = useState(params.q ?? "");
  if (params.q !== syncedFromProp) {
    setSyncedFromProp(params.q);
    setQ(params.q ?? "");
  }
  const debounceRef = useRef<number | null>(null);

  function go(href: string) {
    navigate(href);
  }

  function onSearchChange(value: string) {
    setQ(value);
    if (debounceRef.current !== null) window.clearTimeout(debounceRef.current);
    debounceRef.current = window.setTimeout(() => {
      go(herdSignalsHref(params, { hs_q: value.trim() || undefined }));
    }, 300);
  }

  // ≤640: the six controls fold behind ONE "Filters" button into the same bottom sheet the
  // WorklistFilters bar uses (frame.css `.wf-mobile-*`), instead of a five-row stack.
  const [mobileOpen, setMobileOpen] = useState(false);
  // Desktop: search, Pen and Movement stay on the bar; the rarely used six sit behind "More filters"
  // so the card is one row. The phone sheet always shows everything.
  const moreActive = [params.mappingState, params.pattern, params.risk].filter(Boolean).length;
  const [moreOpen, setMoreOpen] = useState(moreActive > 0);
  const showMore = moreOpen || mobileOpen;
  const activeCount = [params.q, params.shedId, params.movementState, params.mappingState, params.pattern, params.risk].filter(Boolean).length;

  // Template list toolbar on a Card; on a phone the controls open in the template filters drawer.
  const controls = (
    <>
      {isPending ? <CircularProgress size={16} color="inherit" aria-label="Applying filter" /> : null}
      <TextField
        type="search"
        placeholder="Search animal, RFID, smart tag, BLE MAC, pen, breed or gateway"
        value={q}
        onChange={(event) => onSearchChange(event.target.value)}
        autoComplete="off"
        sx={HERD_SIGNALS_SEARCH_SX}
        slotProps={{
          htmlInput: { "aria-label": "Search animal, RFID, smart tag, BLE MAC, pen, breed or gateway" },
          input: {
            startAdornment: (
              <InputAdornment position="start">
                <Iconify icon="eva:search-fill" sx={{ color: "text.disabled" }} />
              </InputAdornment>
            ),
            endAdornment: q ? (
              <InputAdornment position="end">
                <IconButton
                  edge="end"
                  title="Clear search"
                  aria-label="Clear search"
                  onClick={() => {
                    setQ("");
                    go(herdSignalsHref(params, { hs_q: undefined }));
                  }}
                >
                  <Iconify icon="mingcute:close-line" />
                </IconButton>
              </InputAdornment>
            ) : undefined,
          },
        }}
      />

      {/* Park scope is owned by the shell top bar (lib/scope.ts), not this page's own filter bar —
          see check-ia-guard.mjs: command/authority screens must not repeat park scope inline. The
          top bar writes the same `park` query param herdSignalsHref/parseHerdSignalsParams already
          read, so scoping by park still filters this page's table and KPI aggregates exactly as
          before; only the duplicate in-page control is gone. */}
      <TextField
        select
        label="Pen"
        value={sheds.some((shed) => shed.id === params.shedId) ? params.shedId : ""}
        onChange={({ target: { value } }) => go(herdSignalsHref(params, { hs_shed: value || undefined }))}
        sx={{ minWidth: { xs: 0, sm: HERD_SIGNALS_SELECT_MIN }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        <MenuItem value="">All pens</MenuItem>
        {sheds.map((shed) => (
          <MenuItem key={shed.id} value={shed.id}>
            {shed.label}
          </MenuItem>
        ))}
      </TextField>

      <TextField
        select
        label="Movement"
        value={params.movementState ?? ""}
        onChange={({ target: { value } }) => go(herdSignalsHref(params, { hs_move: value || undefined, hs_kpi: undefined }))}
        sx={{ minWidth: { xs: 0, sm: HERD_SIGNALS_SELECT_MIN }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        <MenuItem value="">Any movement state</MenuItem>
        {MOVEMENT_OPTIONS.map(([key, label]) => (
          <MenuItem key={key} value={key}>
            {label}
          </MenuItem>
        ))}
      </TextField>

      {/* mock/herd-signals-mock.html `#kpiChip` — the ONLY chip the fbar carries is the active KPI
          filter, one per active KPI, independently removable via its own ✕. The mock has no
          generic "Clear filters" control in the filter bar (`clearFilters()` is only wired to the
          empty-state action button) — do not invent one here. */}
      <HerdSignalsKpiChip params={params} />

      <MuiButton
        variant="outlined"
        color="inherit"
        aria-expanded={showMore}
        aria-controls="herd-signals-more-filters"
        onClick={() => setMoreOpen((open) => !open)}
        startIcon={<Iconify icon="ic:round-filter-list" />}
        sx={{ display: { xs: "none", md: "inline-flex" } }}
      >
        {moreOpen ? "Fewer filters" : "More filters"}
        {moreActive > 0 ? <Label variant="soft">{moreActive}</Label> : null}
      </MuiButton>

      <div id="herd-signals-more-filters" className={hs.more} hidden={!showMore}>
      <TextField
        select
        label="Mapping"
        value={params.mappingState ?? ""}
        onChange={({ target: { value } }) => go(herdSignalsHref(params, { hs_map: value || undefined }))}
        sx={{ minWidth: { xs: 0, sm: HERD_SIGNALS_SELECT_MIN }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        <MenuItem value="">Mapped + unmapped</MenuItem>
        {MAPPING_OPTIONS.map(([key, label]) => (
          <MenuItem key={key} value={key}>
            {label}
          </MenuItem>
        ))}
      </TextField>

      <TextField
        select
        label="Pattern"
        value={params.pattern ?? ""}
        onChange={({ target: { value } }) => go(herdSignalsHref(params, { hs_pattern: value || undefined }))}
        sx={{ minWidth: { xs: 0, sm: HERD_SIGNALS_SELECT_MIN }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        <MenuItem value="">Any pattern</MenuItem>
        {PATTERN_OPTIONS.map(([key, label]) => (
          <MenuItem key={key} value={key}>
            {label}
          </MenuItem>
        ))}
      </TextField>

      <TextField
        select
        label="Watchlist"
        value={params.risk ?? ""}
        onChange={({ target: { value } }) => go(herdSignalsHref(params, { hs_risk: value || undefined }))}
        sx={{ minWidth: { xs: 0, sm: HERD_SIGNALS_SELECT_MIN }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        <MenuItem value="">Any watchlist</MenuItem>
        {RISK_OPTIONS.map(([key, label]) => (
          <MenuItem key={key} value={key}>
            {label}
          </MenuItem>
        ))}
      </TextField>


      <TextField
        select
        label="Movement window"
        value={params.liveWindow}
        onChange={({ target: { value } }) => go(herdSignalsHref(params, { hs_live_window: value }))}
        sx={{ minWidth: { xs: 0, sm: HERD_SIGNALS_SELECT_MIN }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        {LIVE_WINDOW_OPTIONS.map(([key, label]) => (
          <MenuItem key={key} value={key}>
            {label}
          </MenuItem>
        ))}
      </TextField>

      <TextField
        select
        label="Compare to self"
        value={params.ownBaseline}
        onChange={({ target: { value } }) => go(herdSignalsHref(params, { hs_own_base: value }))}
        sx={{ minWidth: { xs: 0, sm: HERD_SIGNALS_SELECT_MIN }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        {OWN_BASELINE_OPTIONS.map(([key, label]) => (
          <MenuItem key={key} value={key}>
            {label}
          </MenuItem>
        ))}
      </TextField>

      <TextField
        select
        label="Compare to pen"
        value={params.shedBaseline}
        onChange={({ target: { value } }) => go(herdSignalsHref(params, { hs_shed_base: value }))}
        sx={{ minWidth: { xs: 0, sm: HERD_SIGNALS_SELECT_MIN }, flexShrink: 0, maxWidth: 1 }}
        slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
      >
        {SHED_BASELINE_OPTIONS.map(([key, label]) => (
          <MenuItem key={key} value={key}>
            {label}
          </MenuItem>
        ))}
      </TextField>

      <span className="fnote">15m activity is sustained movement &middot; thresholds provisional</span>
      </div>
    </>
  );

  return (
    <>
    <Card
      className="herd-signals-fbar"
      aria-busy={isPending}
      sx={{
        overflow: "visible",
        // Inside another card (a table card) the toolbar is part of that card, as in the template list.
        ".MuiCard-root &, .card &": { boxShadow: "none", bgcolor: "transparent", borderRadius: 0 },
      }}
    >
      <Box sx={{ p: 2.5, gap: 2, display: "flex", flexWrap: "wrap", alignItems: "center" }}>
        <Box sx={{ display: { xs: "none", md: "flex" }, flexWrap: "wrap", gap: 2, alignItems: "center", flex: "1 1 auto", minWidth: 0 }}>{controls}</Box>
        <MuiButton
          variant="outlined"
          color="inherit"
          aria-expanded={mobileOpen}
          onClick={() => setMobileOpen(true)}
          startIcon={
            <Badge color="error" variant="dot" invisible={activeCount === 0}>
              <Iconify icon="ic:round-filter-list" />
            </Badge>
          }
          sx={{ display: { xs: "inline-flex", md: "none" } }}
        >
          Filters
          {activeCount > 0 ? ` (${activeCount})` : null}
        </MuiButton>
      </Box>
    </Card>
    <MinimalDrawer open={mobileOpen} onClose={() => setMobileOpen(false)} title="Filters" aria-label="Filters">
      <Box sx={{ p: 2.5, display: "flex", flexDirection: "column", gap: 2.5, "& .MuiFormControl-root, & > .MuiTextField-root": { width: 1 } }}>{controls}</Box>
    </MinimalDrawer>
    </>
  );
}
