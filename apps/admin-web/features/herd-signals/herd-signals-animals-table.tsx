"use client";

import type { ReactNode } from "react";
import Box from "@mui/material/Box";
import MuiLink from "@mui/material/Link";
import TableCell from "@mui/material/TableCell";
import TableRow from "@mui/material/TableRow";
import { LocalOverlayLink, pushLocalOverlayUrl } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { operationalLocationLabel } from "@/lib/operational-location";
import type { HerdSignalItem } from "@/lib/api/herd-signals";
import {
  BATTERY_LABEL,
  BATTERY_TONE,
  MOVEMENT_LABEL,
  MOVEMENT_TONE,
  fmtAgo,
  fmtBatteryMv,
  fmtDelta1h,
  fmtRssi,
  fmtSignedDelta,
} from "./format";
import { HS_MONO, HS_SUBLINE, deltaSx, selectableRowSx } from "./herd-signals-sx";

function isInteractiveTarget(target: EventTarget | null): boolean {
  return target instanceof Element && Boolean(target.closest("a,button,input,select,textarea,[role='button']"));
}

function animalPrimaryLabel(item: HerdSignalItem): string {
  return item.animal_identifier_1 || item.animal_identifier_2 || item.display_id || item.goat_id || "—";
}

// The Animals tab is NOT the Live Monitor table with a different heading. The mock
// (mock/herd-signals-mock.html `renderAnimals`) gives it its own NINE-column set, in its own
// order -- Shed comes BEFORE Smart tag here, and Gateway / Motion count / Pattern / Tag temp /
// Status are all absent: this tab answers "how is each animal's tag reading right now", not
// "what is the radio doing". Reusing the fourteen-column live set here was the same defect the
// Tag Mapping tab had, so the column set lives in its own file rather than as a flag on the
// live table's markup.
//
// Per-cell treatment is taken from that same `renderAnimals`, not from the live table:
//   Animal      -- full-width on a phone (data-wide), the display id, linked so the row still opens the tag drawer
//   Shed        -- operational location, park underneath as a caption line
//   Smart tag   -- monospace tag id ONLY (the live table's second MAC line is not in this set)
//   15m / 1h    -- right-aligned (data-num) delta with the mock's up/zero/warn tone (deltaSx)
//   Activity    -- movement tone chip
//   Signal      -- PLAIN "-62 dBm" text, not the live table's chip (mock renders it bare here)
//   Battery     -- volts, plus the battery-state chip when it is not healthy
//   Last seen   -- relative age
//
// The mock's Battery cell carries a second "est. <n> left" line. This product removed the
// life estimate (features/herd-signals/format.ts): the tag does not report remaining life and
// the discharge curve was never vendor-confirmed, so that line is deliberately not ported --
// the battery STATE chip is the honest replacement, and a missing value renders "—".

export function HerdSignalsAnimalsHead() {
  return (
    <TableRow>
      <TableCell component="th">Animal</TableCell>
      <TableCell component="th">Pen</TableCell>
      <TableCell component="th">Smart tag</TableCell>
      <TableCell component="th" data-num>15m delta</TableCell>
      <TableCell component="th" data-num>1h delta</TableCell>
      <TableCell component="th">Activity</TableCell>
      <TableCell component="th">Signal</TableCell>
      <TableCell component="th">Battery</TableCell>
      <TableCell component="th">Last seen</TableCell>
    </TableRow>
  );
}

export function HerdSignalsAnimalsRow({
  item,
  nowMs,
  href,
}: {
  item: HerdSignalItem;
  nowMs: number;
  href: string;
}): ReactNode {
  const location =
    item.operational_location_display ||
    operationalLocationLabel({ shedName: item.shed_name, partitionLabel: item.partition_label });
  const delta15 = fmtSignedDelta(item.motion_delta);
  const delta1hText = fmtDelta1h(item.motion_delta_1h, item.motion_delta);
  const delta1h = delta1hText === "—" ? { text: "—", tone: "zero" as const } : fmtSignedDelta(item.motion_delta_1h);
  return (
    <TableRow
      sx={selectableRowSx()}
      onClick={(event) => {
        if (isInteractiveTarget(event.target)) return;
        pushLocalOverlayUrl(href);
      }}
    >
      <TableCell data-l="Animal" data-wide>
        <MuiLink
          component={LocalOverlayLink}
          href={href}
          scroll={false}
          title="Open tag detail"
          color="inherit"
          underline="hover"
          sx={{ typography: "subtitle2", display: "inline-flex", alignItems: "center", minHeight: { xs: "var(--tap-min)", md: "auto" } }}
        >
          {animalPrimaryLabel(item)}
        </MuiLink>
        {item.display_id && (item.animal_identifier_1 || item.animal_identifier_2) ? (
          <Box component="span" sx={HS_SUBLINE}>{item.display_id}</Box>
        ) : null}
      </TableCell>
      <TableCell data-l="Pen">
        {location || "—"}
        {item.park_name ? <Box component="span" sx={HS_SUBLINE}>{item.park_name}</Box> : null}
      </TableCell>
      <TableCell data-l="Smart tag">
        <Box component="span" sx={HS_MONO}>{item.tag_id}</Box>
      </TableCell>
      <TableCell
        data-l="15m delta"
        data-num
        title={item.gap_delta ? "Accumulated across a reception gap — timing within the gap is unknown, not a normal 15m reading" : undefined}
      >
        <Box component="span" sx={deltaSx(delta15.tone)}>{delta15.text}</Box>
        {item.gap_delta ? <sup title="Gap total">*</sup> : null}
      </TableCell>
      <TableCell data-l="1h delta" data-num>
        <Box component="span" sx={deltaSx(delta1h.tone)}>{delta1h.text}</Box>
      </TableCell>
      <TableCell data-l="Activity">
        {item.movement_state ? <Tag tone={MOVEMENT_TONE[item.movement_state]}>{MOVEMENT_LABEL[item.movement_state]}</Tag> : "—"}
      </TableCell>
      <TableCell data-l="Signal">{fmtRssi(item.rssi_dbm)}</TableCell>
      <TableCell data-l="Battery">
        {fmtBatteryMv(item.battery_mv)}
        {item.battery_state && item.battery_state !== "healthy" ? (
          <>
            {" "}
            <Tag tone={BATTERY_TONE[item.battery_state]}>{BATTERY_LABEL[item.battery_state]}</Tag>
          </>
        ) : null}
      </TableCell>
      <TableCell data-l="Last seen">{fmtAgo(item.last_seen_at, nowMs)}</TableCell>
    </TableRow>
  );
}
