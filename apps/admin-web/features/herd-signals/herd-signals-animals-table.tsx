"use client";

import type { ReactNode } from "react";
import { LocalOverlayLink } from "@/components/local-overlay-link";
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

// The Animals tab is NOT the Live Monitor table with a different heading. The mock
// (mock/herd-signals-mock.html `renderAnimals`) gives it its own NINE-column set, in its own
// order -- Shed comes BEFORE Smart tag here, and Gateway / Motion count / Pattern / Tag temp /
// Status are all absent: this tab answers "how is each animal's tag reading right now", not
// "what is the radio doing". Reusing the fourteen-column live set here was the same defect the
// Tag Mapping tab had, so the column set lives in its own file rather than as a flag on the
// live table's markup.
//
// Per-cell treatment is taken from that same `renderAnimals`, not from the live table:
//   Animal      -- `td.wide`, the display id, linked so the row still opens the tag drawer
//   Shed        -- operational location, park underneath in `.faint.small`
//   Smart tag   -- `.mono` tag id ONLY (the live table's second MAC line is not in this set)
//   15m / 1h    -- `td.num` + `span.delta` with the mock's up/zero/warn tone
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
    <tr>
      <th>Animal</th>
      <th>Shed</th>
      <th>Smart tag</th>
      <th className="num">15m delta</th>
      <th className="num">1h delta</th>
      <th>Activity</th>
      <th>Signal</th>
      <th>Battery</th>
      <th>Last seen</th>
    </tr>
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
    <tr>
      <td data-l="Animal" className="wide">
        <LocalOverlayLink href={href} scroll={false} title="Open tag detail">
          <b>{item.display_id || item.goat_id || "—"}</b>
        </LocalOverlayLink>
      </td>
      <td data-l="Shed">
        {location || "—"}
        {item.park_name ? (
          <>
            <br />
            <span className="faint small">{item.park_name}</span>
          </>
        ) : null}
      </td>
      <td data-l="Smart tag">
        <span className="mono">{item.tag_id}</span>
      </td>
      <td
        data-l="15m delta"
        className="num"
        title={item.gap_delta ? "Accumulated across a reception gap — timing within the gap is unknown, not a normal 15m reading" : undefined}
      >
        <span className={`delta ${delta15.tone}`}>{delta15.text}</span>
        {item.gap_delta ? <sup title="Gap total">*</sup> : null}
      </td>
      <td data-l="1h delta" className="num">
        <span className={`delta ${delta1h.tone}`}>{delta1h.text}</span>
      </td>
      <td data-l="Activity">
        {item.movement_state ? <Tag tone={MOVEMENT_TONE[item.movement_state]}>{MOVEMENT_LABEL[item.movement_state]}</Tag> : "—"}
      </td>
      <td data-l="Signal">{fmtRssi(item.rssi_dbm)}</td>
      <td data-l="Battery">
        {fmtBatteryMv(item.battery_mv)}
        {item.battery_state && item.battery_state !== "healthy" ? (
          <>
            {" "}
            <Tag tone={BATTERY_TONE[item.battery_state]}>{BATTERY_LABEL[item.battery_state]}</Tag>
          </>
        ) : null}
      </td>
      <td data-l="Last seen">{fmtAgo(item.last_seen_at, nowMs)}</td>
    </tr>
  );
}
