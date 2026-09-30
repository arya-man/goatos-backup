"use client";

import { Download } from "lucide-react";
import { useCallback, useState, useSyncExternalStore, useTransition } from "react";

import { DateRangePicker, type DateRangePickerLabels } from "@/components/date-range-picker";
import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  LocalOverlayLink,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { weightsWindowSettings } from "./landing-window-constants";
import { weightsSexChoices } from "./sex-filter-contract";
import { exportWeightsCsvAction } from "./weights-export-action";
import Button from "@mui/material/Button";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";
import FormControl from "@mui/material/FormControl";
import FormGroup from "@mui/material/FormGroup";
import FormLabel from "@mui/material/FormLabel";
import Alert from "@mui/material/Alert";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import { MinimalDrawer } from "@/components/app/drawer";

export type WeightsExportPark = { park_id: string; name: string };

export type WeightsExportShed = {
  location_id: string;
  label: string;
  /** The owning park, so the shed list follows the drawer's park select. */
  park_id: string;
};

const EXPORT_PARAM = "wt_export";

/** Reads the export flag from the address bar. "" means the drawer is closed. */
function readExportParam(): string {
  return new URL(window.location.href).searchParams.get(EXPORT_PARAM) ?? "";
}

/**
 * Subscribes to both ways the export param can change: a LocalOverlayLink click (which
 * dispatches the shared event) and browser Back/Forward (popstate).
 */
function subscribeToOverlayUrl(onChange: () => void): () => void {
  window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
  window.addEventListener("popstate", onChange);
  return () => {
    window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, onChange);
    window.removeEventListener("popstate", onChange);
  };
}

/**
 * The Weights page's Download control: a button in the page header that opens a
 * right-side drawer holding the export selection — the period calendar, a park
 * select and a shed list — and hands the finished CSV to the browser.
 *
 * Same overlay mechanics as the sales record drawer: CLIENT state driven by the URL
 * (LocalOverlayLink changes history WITHOUT an RSC request), the template temporary
 * drawer (MinimalDrawer), and SSR always renders it closed. The FILE itself is fetched on click through a server action, because a
 * year of weighings is not something the page should carry on every load.
 */
export function WeightsExportControl({
  pageContract,
  parks,
  sheds,
  initialParkId,
  initialSex,
  initialFrom,
  initialTo,
  origin,
  weighingCategory,
  today,
  openHref,
  closeHref,
}: {
  pageContract: AdminUiPageContract;
  parks: WeightsExportPark[];
  sheds: WeightsExportShed[];
  /** The page's current park filter, so the drawer opens on the scope the reader is looking at. */
  initialParkId: string;
  /** The page's current Sex filter ("" = every kid), so the drawer opens on what the reader sees. */
  initialSex: string;
  /** The page's selected window, both ends "YYYY-MM-DD" Asia/Kolkata business dates. */
  initialFrom: string;
  initialTo: string;
  /** Optional host-page filters the CSV must match. */
  origin?: string;
  weighingCategory?: string;
  today: string;
  /** Real deep links for new tabs / no-JS; ordinary clicks stay client-local. */
  openHref: string;
  closeHref: string;
}) {
  // The URL is an EXTERNAL store — LocalOverlayLink mutates history outside React — so it is
  // read via useSyncExternalStore rather than mirrored into state. SSR renders the drawer closed.
  const selection = useSyncExternalStore(subscribeToOverlayUrl, readExportParam, () => "");
  const open = selection !== "";

  const [from, setFrom] = useState(initialFrom);
  const [to, setTo] = useState(initialTo);
  const [parkId, setParkId] = useState(initialParkId);
  const [sex, setSex] = useState(initialSex);
  // Empty set = every shed ("All pens"), which is also what the backend receives.
  const [selectedSheds, setSelectedSheds] = useState<ReadonlySet<string>>(new Set());
  const [failed, setFailed] = useState(false);
  const [pending, startTransition] = useTransition();

  // EVERY OPENING starts from the page's period and park as they are now (maintainer request
  // 2026-09-24). useState read them once, at the first render, so after the reader changed the
  // page's period the drawer still offered the old one. Sex follows the page too (maintainer
  // request 2026-09-28, superseding the 2026-09-07 "drawer starts on All"): a page filtered to
  // Male opens a drawer on Male, a page on All opens it on All, and the drawer's own select can
  // still change it before the download.
  // Adjusted while rendering on the closed -> open edge (React's documented pattern for state that
  // follows a prop), so the reader can still change anything inside the drawer before downloading.
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) {
      setFrom(initialFrom);
      setTo(initialTo);
      setParkId(initialParkId);
      setSex(initialSex);
      setSelectedSheds(new Set());
      setFailed(false);
    }
  }

  const close = useCallback(() => {
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(closeHref);
  }, [closeHref]);

  const title = copy(pageContract, "export.title");
  const parkNameById = new Map(parks.map((park) => [park.park_id, park.name]));
  const visibleSheds = parkId === "" ? sheds : sheds.filter((shed) => shed.park_id === parkId);
  const visibleShedIds = new Set(visibleSheds.map((shed) => shed.location_id));
  // Only sheds of the selected park travel: a selection made under "All parks" must not
  // silently pin the export to another park's pens after the park select narrows.
  const effectiveShedIds = [...selectedSheds].filter((id) => visibleShedIds.has(id));
  const allSheds = effectiveShedIds.length === 0;

  function toggleShed(locationId: string): void {
    setSelectedSheds((previous) => {
      const next = new Set(previous);
      if (next.has(locationId)) next.delete(locationId);
      else next.add(locationId);
      return next;
    });
  }

  const rangeLabels: DateRangePickerLabels = {
    field: copy(pageContract, "export.period.label"),
    today: copy(pageContract, "filter.period.today"),
    single: copy(pageContract, "filter.period.single"),
    range: copy(pageContract, "filter.period.range"),
    aria: copy(pageContract, "filter.period.aria"),
    previousMonth: copy(pageContract, "filter.period.previous_month"),
    nextMonth: copy(pageContract, "filter.period.next_month"),
    rangeStartHint: copy(pageContract, "filter.period.range_start_hint"),
    rangeEndHint: copy(pageContract, "filter.period.range_end_hint"),
    rangeSeparator: copy(pageContract, "filter.period.range_separator"),
  };

  function download(): void {
    setFailed(false);
    startTransition(async () => {
      const result = await exportWeightsCsvAction({
        from,
        to,
        parkId: parkId || undefined,
        shedIds: allSheds ? undefined : effectiveShedIds,
        sex: sex || undefined,
        origin: origin || undefined,
        weighingCategory: weighingCategory && weighingCategory !== "all" ? weighingCategory : undefined,
      });
      if (!result.ok) {
        setFailed(true);
        return;
      }
      writeCsvFile(`weights-${from}-${to}.csv`, result.csv);
    });
  }

  return (
    <div className="wt-export-control">
      <Button component={LocalOverlayLink} href={openHref} variant="contained" color="primary" scroll={false} aria-haspopup="dialog" startIcon={<Download size={18} aria-hidden="true" />}>
        {copy(pageContract, "export.button")}
      </Button>

      {/* Template temporary drawer (MinimalDrawer: portal, backdrop, focus trap + return). A short
          options form, so the template settings-drawer width (360). */}
      <MinimalDrawer
        open={open}
        onClose={close}
        title={title}
        closeLabel={title}
        width={360}
        aria-label={title}
        footer={
          <Button
            variant="contained"
            color="primary"
            onClick={download}
            disabled={pending}
            aria-busy={pending}
            loading={pending}
            loadingPosition="start"
            startIcon={<Download size={18} aria-hidden="true" />}
          >
            {pending ? copy(pageContract, "export.preparing") : copy(pageContract, "export.download")}
          </Button>
        }
      >
        <Stack spacing={2.5} sx={{ p: 2.5 }}>
          <Typography variant="overline" sx={{ color: "text.secondary" }}>
            {copy(pageContract, "export.eyebrow")}
          </Typography>
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            {copy(pageContract, "export.hint")}
          </Typography>

          <TextField
            select
            id="wt-export-sex"
            value={sex}
            label={copy(pageContract, "export.sex.label")}
            onChange={(event) => setSex(event.target.value)}
            disabled={pending}
            fullWidth
            slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true } }}
          >
            <MenuItem value="">{copy(pageContract, "export.sex.all")}</MenuItem>
            {weightsSexChoices(pageContract).map((choice) => (
              <MenuItem key={choice.value} value={choice.value}>
                {choice.label}
              </MenuItem>
            ))}
          </TextField>

          <Stack spacing={1}>
            <Typography variant="subtitle2">{copy(pageContract, "export.period.label")}</Typography>
            <DateRangePicker
              labels={rangeLabels}
              from={from}
              to={to}
              today={today}
              minDate={weightsWindowSettings(pageContract.copy, today).earliestDate}
              busy={pending}
              onChange={(nextFrom, nextTo) => {
                setFrom(nextFrom);
                setTo(nextTo);
              }}
            />
          </Stack>

          <TextField
            select
            label={copy(pageContract, "export.park.label")}
            value={parkId}
            disabled={pending}
            onChange={(event) => setParkId(event.target.value)}
            fullWidth
            slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
          >
            <MenuItem value="">{copy(pageContract, "export.park.all")}</MenuItem>
            {parks.map((park) => (
              <MenuItem key={park.park_id} value={park.park_id}>
                {park.name}
              </MenuItem>
            ))}
          </TextField>

          <FormControl component="fieldset" disabled={pending}>
            <FormLabel component="legend" sx={{ typography: "subtitle2", color: "text.primary", mb: 0.5 }}>
              {copy(pageContract, "export.sheds.label")}
            </FormLabel>
            <FormGroup>
              <FormControlLabel
                control={<Checkbox checked={allSheds} onChange={() => setSelectedSheds(new Set())} sx={{ p: { xs: 1.5, sm: 1 } }} />}
                label={copy(pageContract, "export.sheds.all")}
              />
              {visibleSheds.map((shed) => (
                <FormControlLabel
                  key={shed.location_id}
                  control={<Checkbox checked={selectedSheds.has(shed.location_id)} onChange={() => toggleShed(shed.location_id)} sx={{ p: { xs: 1.5, sm: 1 } }} />}
                  label={
                    // With no park selected the park travels on the row: 39 shed names exist in
                    // BOTH parks, so a bare shed name would appear twice, indistinguishably.
                    parkId === "" ? `${parkNameById.get(shed.park_id) ?? ""} · ${shed.label}` : shed.label
                  }
                />
              ))}
            </FormGroup>
          </FormControl>

          {failed ? <Alert severity="warning">{copy(pageContract, "export.error")}</Alert> : null}
        </Stack>
      </MinimalDrawer>
    </div>
  );
}

/** Hands the CSV text to the browser as a saved file. */
function writeCsvFile(filename: string, csv: string): void {
  // The BOM makes Excel read this as UTF-8. Without it a shed name with an accent, or any
  // non-ASCII operator name, opens as mojibake.
  const blob = new Blob(["﻿" + csv], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  // Revoking on the next tick rather than immediately: Safari has not always started the
  // download by the time click() returns, and revoking too early cancels it.
  setTimeout(() => URL.revokeObjectURL(url), 0);
}
