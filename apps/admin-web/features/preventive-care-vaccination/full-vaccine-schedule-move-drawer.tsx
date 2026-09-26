"use client";

import {
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  currentHistoryEntryIsLocalOverlay,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate, todayIso } from "@/lib/format";
import { useCallback, useEffect, useState, type ComponentProps } from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import { DetailDrawer } from "@/components/app/detail-drawer";
import { ThemedDatePicker } from "@/components/themed-date-picker";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";

export type ScheduleMoveDrawerRow = {
  eventId: string;
  plannedDate: string;
  originalPlannedDate: string;
  operatorName: string;
  parkId: string;
  parkName: string;
  animals: number;
  totalDoses: number;
  vaccineCodes: string[];
  vaccineOriginalDates: Record<string, string>;
  vaccineOriginalDateSets: Record<string, string[]>;
  vaccineNames: string[];
  returnTo: string;
};

type MoveAction = ComponentProps<"form">["action"];

// The submit button sits in the template drawer footer, outside the form; it posts through `form=`.
const MOVE_FORM_ID = "schedule-move-form";

function selectedMoveIdFromUrl(): string | undefined {
  const url = new URL(window.location.href);
  const hash = new URLSearchParams(url.hash.replace(/^#/, ""));
  return hash.get("schedule_move") ?? url.searchParams.get("schedule_move") ?? undefined;
}

export function ScheduleMoveDrawer({
  rows,
  initialSelectedEventId,
  closeHref,
  pageContract,
  action,
}: {
  rows: ScheduleMoveDrawerRow[];
  initialSelectedEventId?: string;
  closeHref: string;
  pageContract: AdminUiPageContract;
  action: MoveAction;
}) {
  const initialRow = rows.find((row) => row.eventId === initialSelectedEventId);
  const [displayedRow, setDisplayedRow] = useState<ScheduleMoveDrawerRow | undefined>(initialRow);
  const [drawerOpen, setDrawerOpen] = useState(Boolean(initialRow));
  const [selectedVaccineCode, setSelectedVaccineCode] = useState(initialRow?.vaccineCodes[0] ?? "");

  const showRow = useCallback((row: ScheduleMoveDrawerRow) => {
    setDisplayedRow(row);
    setSelectedVaccineCode(row.vaccineCodes[0] ?? "");
    window.requestAnimationFrame(() => setDrawerOpen(true));
  }, []);

  const hideRow = useCallback(() => setDrawerOpen(false), []);

  useEffect(() => {
    function syncSelectionFromUrl(): void {
      const row = rows.find((item) => item.eventId === selectedMoveIdFromUrl());
      if (row) showRow(row);
      else hideRow();
    }
    window.addEventListener("popstate", syncSelectionFromUrl);
    window.addEventListener("hashchange", syncSelectionFromUrl);
    window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncSelectionFromUrl);
    const initialFrame = window.requestAnimationFrame(syncSelectionFromUrl);
    return () => {
      window.cancelAnimationFrame(initialFrame);
      window.removeEventListener("popstate", syncSelectionFromUrl);
      window.removeEventListener("hashchange", syncSelectionFromUrl);
      window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncSelectionFromUrl);
    };
  }, [hideRow, rows, showRow]);

  const closeDrawer = useCallback(() => {
    hideRow();
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(closeHref);
  }, [closeHref, hideRow]);

  if (!displayedRow) return null;

  // Template temporary drawer (portal, backdrop, focus trapped and returned). Escape and the backdrop
  // land on closeDrawer, which steps the local-overlay history entry back once.
  return (
    <DetailDrawer
      open={drawerOpen}
      onClose={closeDrawer}
      title={copy(pageContract, "schedule.move.title")}
      eyebrow={`${fmtDate(displayedRow.plannedDate)} · ${displayedRow.operatorName}`}
      subtitle={`${displayedRow.parkName} · ${displayedRow.animals} ${copy(pageContract, "schedule.unit.animals")} · ${displayedRow.totalDoses} ${copy(pageContract, "schedule.unit.doses")}`}
      ariaLabel={copy(pageContract, "schedule.move.title")}
      closeLabel={copy(pageContract, "schedule.move.close")}
      footer={
        <>
          <Button variant="outlined" color="inherit" onClick={closeDrawer}>
            {copy(pageContract, "schedule.move.close")}
          </Button>
          <Button variant="contained" type="submit" form={MOVE_FORM_ID}>
            {copy(pageContract, "schedule.postpone.action")}
          </Button>
        </>
      }
    >
      <Stack component="form" id={MOVE_FORM_ID} action={action} spacing={2.5}>
        <input type="hidden" name="park_id" value={displayedRow.parkId} />
        <input
          type="hidden"
          name="original_drive_date"
          value={displayedRow.vaccineOriginalDates[selectedVaccineCode] || displayedRow.originalPlannedDate || displayedRow.plannedDate}
        />
        <input
          type="hidden"
          name="original_drive_dates"
          value={(displayedRow.vaccineOriginalDateSets[selectedVaccineCode] ?? [
            displayedRow.vaccineOriginalDates[selectedVaccineCode] || displayedRow.originalPlannedDate || displayedRow.plannedDate,
          ]).join(",")}
        />
        <input type="hidden" name="reason" value={copy(pageContract, "schedule.postpone.reason_default")} />
        <input type="hidden" name="return_to" value={displayedRow.returnTo} />
        <input type="hidden" name="vaccine_code" value={selectedVaccineCode} />
        <TextField
          select
          fullWidth
          label={copy(pageContract, "schedule.postpone.vaccine")}
          value={selectedVaccineCode}
          onChange={(event) => setSelectedVaccineCode(event.target.value)}
          slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
        >
          {displayedRow.vaccineCodes.map((code, index) => (
            <MenuItem key={code} value={code}>
              {displayedRow.vaccineNames[index] ?? code}
            </MenuItem>
          ))}
        </TextField>
        <Stack spacing={1}>
          <Typography variant="subtitle2">{copy(pageContract, "schedule.postpone.new_date")}</Typography>
          <ThemedDatePicker
            name="override_date"
            label={copy(pageContract, "schedule.move.date_placeholder")}
            min={todayIso()}
            previousMonthLabel={copy(pageContract, "schedule.move.previous_month")}
            nextMonthLabel={copy(pageContract, "schedule.move.next_month")}
            invalidDateText={copy(pageContract, "schedule.move.invalid_future_date")}
            required
          />
          <Typography variant="caption" sx={{ color: "text.secondary" }}>
            Requested start date. The system may move this to the nearest safe date if vaccine spacing rules require.
          </Typography>
        </Stack>
        <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.75 }}>
          {displayedRow.vaccineNames.map((name) => <Tag key={name} tone="teal">{name}</Tag>)}
        </Box>
      </Stack>
    </DetailDrawer>
  );
}
