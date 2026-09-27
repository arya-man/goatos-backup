"use client";

import { useState } from "react";
import Box from "@mui/material/Box";
import Avatar from "@mui/material/Avatar";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { Label } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { LinkButton } from "@/components/app/link-button";
import { BlockLabel, KanbanDetails } from "@/components/app/kanban/kanban-details";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { control, controlEnabled, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { WorkBoardRow } from "@/lib/api/work-board-server";
import { flagParkHeadAction } from "./actions";
import { WorkBoardSubtasks } from "./work-board-subtasks";
import { ClockLabel, WorkProgress } from "./work-board-parts";
import { dayLabel, findOption, initials, lanes, moduleOptions, needsAttention, ownerStack, PARAM_ROW, parkLabel, parkOptions, pendingSplit, stateOptions } from "./work-board-model";

// The card's detail in the template kanban details shape (sections/kanban/details): a right drawer
// with the status in the toolbar, Overview / Subtasks tabs, and BlockLabel rows. Open/close is
// local (URL mirrored, no document request); the Flag form is the one write and it is gated on
// the page contract's control.
function FlagForm({ pageContract, row, returnTo, hot }: { pageContract: AdminUiPageContract; row: WorkBoardRow; returnTo: string; hot: boolean }) {
  const enabled = controlEnabled(pageContract, "flag_park_head", false);
  const ctl = control(pageContract, "flag_park_head");
  return (
    <Box component="form" action={flagParkHeadAction} sx={{ display: "flex", flexDirection: "column", gap: 1.5 }}>
      <input type="hidden" name="row_key" value={row.row_key} />
      <input type="hidden" name="park_id" value={row.park_id} />
      <input type="hidden" name="business_date" value={row.business_date} />
      <input type="hidden" name="return_to" value={returnTo} />
      <TextField
        fullWidth
        multiline
        minRows={3}
        size="small"
        id={`flag-note-${row.row_key}`}
        name="note"
        label={copy(pageContract, "flag.note")}
        placeholder={copy(pageContract, "flag.note.placeholder")}
        disabled={!enabled}
        slotProps={{ inputLabel: { shrink: true }, htmlInput: { maxLength: 1000 } }}
      />
      <Box sx={{ display: "flex", gap: 1, alignItems: "center", flexWrap: "wrap" }}>
        <Button type="submit" variant={hot ? "contained" : "outlined"} color={hot ? "primary" : "inherit"} disabled={!enabled} title={enabled ? copy(pageContract, "flag.hint") : ctl.disabled_reason}>
          {ctl.label}
        </Button>
        {enabled ? null : (
          <Typography variant="caption" sx={{ color: "text.secondary" }}>
            {ctl.disabled_reason}
          </Typography>
        )}
      </Box>
    </Box>
  );
}

const LANE_COLOR = { todo: "default", in_progress: "warning", in_review: "info", done: "success" } as const;

export function WorkBoardModal({ pageContract, rows, initialSelectedRowKey, closeHref, returnToByRow, selectedOwner }: { pageContract: AdminUiPageContract; rows: WorkBoardRow[]; initialSelectedRowKey?: string; closeHref: string; returnToByRow: Record<string, string>; selectedOwner?: string }) {
  const { displayedItem: row, drawerOpen: open, closeDrawer: close } = useLocalOverlaySelection({
    items: rows,
    itemId: (r) => r.row_key,
    selectionKey: PARAM_ROW,
    initialSelectedId: initialSelectedRowKey,
    closeHref,
  });
  const [tab, setTab] = useState("overview");
  if (!row) return null;
  const moduleOpt = findOption(moduleOptions(pageContract), row.module);
  const stateOpt = findOption(stateOptions(pageContract), row.work_state);
  const laneOpt = lanes(pageContract).find((lane) => lane.key === row.lane);
  const total = row.counts.done + row.counts.pending;
  const split = pendingSplit(row);
  const hot = needsAttention(row);
  const stack = ownerStack(row);
  const ownerLabel = stack.names[0] || (row.owner_state === "pool" ? copy(pageContract, "owner.pool") : copy(pageContract, "owner.missing"));
  const laneColor = LANE_COLOR[row.lane as keyof typeof LANE_COLOR] ?? "default";
  const info = (label: string, value: React.ReactNode) => (
    <Box sx={{ display: "flex", alignItems: "center", minWidth: 0 }}>
      <BlockLabel>{label}</BlockLabel>
      <Box sx={{ minWidth: 0, typography: "body2", display: "flex", alignItems: "center", flexWrap: "wrap", gap: 1 }}>{value}</Box>
    </Box>
  );
  const splitLine = split
    ? [
        split.inReview > 0 ? `${split.inReview} ${copy(pageContract, "card.in_review")}` : "",
        split.started > 0 ? `${split.started} ${copy(pageContract, "card.started")}` : "",
        split.notStarted > 0 ? `${split.notStarted} ${copy(pageContract, "card.not_started")}` : "",
      ]
        .filter(Boolean)
        .join(" · ")
    : "";
  return (
    // Template kanban details drawer (portal, focus trap and return). URL/Back/Escape stay with
    // useLocalOverlaySelection through `close`.
    <KanbanDetails
      open={open}
      onClose={close}
      ariaLabel={row.title}
      closeLabel={copy(pageContract, "action.close")}
      status={
        <Label variant="soft" color={laneColor} title={copy(pageContract, "drawer.status_auto.title")}>
          {laneOpt?.label ?? row.lane} · {copy(pageContract, "drawer.status_auto")}
        </Label>
      }
      actions={
        row.href ? (
          <LinkButton href={row.href} size="small" variant="soft" color="inherit" endIcon={<Iconify icon="eva:arrow-forward-fill" width={16} />} sx={{ mr: 1 }}>
            {copy(pageContract, "drawer.open_module")}
          </LinkButton>
        ) : null
      }
      tabs={[
        { value: "overview", label: copy(pageContract, "drawer.details") },
        { value: "subtasks", label: copy(pageContract, "drawer.subtasks") },
      ]}
      tab={tab}
      onChangeTab={setTab}
    >
      {tab === "overview" ? (
        <Box sx={{ gap: 3, display: "flex", flexDirection: "column" }}>
          <div>
            <Typography variant="h6">{row.title}</Typography>
            {row.subtitle ? (
              <Typography variant="body2" sx={{ mt: 0.5, color: "text.secondary" }}>
                {row.subtitle}
              </Typography>
            ) : null}
          </div>
          {info(copy(pageContract, "detail.status"), <><Label variant="soft" color={laneColor}>{stateOpt?.label ?? row.work_state}</Label><Label variant="outlined">{copy(pageContract, "drawer.status_auto")}</Label></>)}
          {info(copy(pageContract, "detail.module"), <Label variant="soft" color="primary">{moduleOpt?.label ?? row.module}</Label>)}
          {info(copy(pageContract, "detail.park"), <span title={row.park_name || undefined}>{parkLabel(parkOptions(pageContract), row)}</span>)}
          {info(copy(pageContract, "detail.pen"), row.pen.operational_location_display || "—")}
          {info(
            copy(pageContract, "detail.owner"),
            <>
              <Avatar sx={{ width: "var(--sp-4)", height: "var(--sp-4)", typography: "caption", ...(stack.names.length ? {} : row.owner_state === "missing" ? { bgcolor: "error.main", color: "error.contrastText" } : {}) }}>
                {stack.names.length ? initials(stack.names[0]) : row.owner_state === "pool" ? "–" : "!"}
              </Avatar>
              <Box component="span" sx={{ color: stack.names.length ? "text.primary" : "text.secondary" }}>
                {ownerLabel}
                {stack.extra > 0 ? ` +${stack.extra}` : ""}
              </Box>
            </>,
          )}
          {info(copy(pageContract, "detail.clock"), row.clock_label ? <ClockLabel row={row} /> : "—")}
          {info(copy(pageContract, "detail.business_date"), dayLabel(row.business_date))}
          {info(
            copy(pageContract, "tile.done"),
            <>
              <b>{row.counts.done}</b>
              {total > 0 ? <Box component="span" sx={{ color: "text.secondary" }}>/ {total}</Box> : null}
            </>,
          )}
          {info(
            copy(pageContract, "tile.pending"),
            <>
              <b>{row.counts.pending}</b>
              {total > 0 ? <Box component="span" sx={{ color: "text.secondary" }}>/ {total}</Box> : null}
              {/* Where the pending work is, when the source said (the feed cards): the same split
                  the card shows, so the drawer never reads differently. */}
              {splitLine ? <Box component="span" sx={{ color: "text.secondary", typography: "caption" }}>{splitLine}</Box> : null}
            </>,
          )}
          {info(copy(pageContract, "tile.attention"), <Box component="b" sx={{ color: row.counts.needs_attention > 0 ? "warning.main" : "text.primary" }}>{row.counts.needs_attention}</Box>)}
          {total > 0 ? (
            <div>
              <Typography variant="body2" sx={{ mb: 1 }}>
                {row.counts.done} {copy(pageContract, "drawer.subtasks.of")} {total}
              </Typography>
              <WorkProgress row={row} size="lg" />
            </div>
          ) : null}
          <FlagForm pageContract={pageContract} row={row} returnTo={returnToByRow[row.row_key] ?? closeHref} hot={hot} />
        </Box>
      ) : open ? (
        <WorkBoardSubtasks key={`${row.row_key}:${selectedOwner ?? ""}`} pageContract={pageContract} row={row} selectedOwner={selectedOwner} />
      ) : null}
    </KanbanDetails>
  );
}
