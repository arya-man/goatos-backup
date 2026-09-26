"use client";

import { Tag } from "@/components/ui-primitives";

import { Settings, X } from "lucide-react";
import { LinkButton } from "@/components/minimal/link-button";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import IconButton from "@mui/material/IconButton";
import useMediaQuery from "@mui/material/useMediaQuery";
import type { Theme } from "@mui/material/styles";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { control, controlEnabled, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { WorkBoardRow } from "@/lib/api/work-board-server";
import { flagParkHeadAction } from "./actions";
import { WorkBoardSubtasks } from "./work-board-subtasks";
import { ClockLabel, WorkProgress } from "./work-board-parts";
import { dayLabel, findOption, lanes, moduleClass, moduleOptions, needsAttention, ownerStack, PARAM_ROW, parkLabel, parkOptions, pendingSplit, stateOptions } from "./work-board-model";
import TextField from "@mui/material/TextField";
import { AvatarGroup } from "@/components/app/avatar";

// The card's detail in the mock's Jira issue-view shape: a centred dialog over a scrim, the
// module and key as breadcrumb, the title, a description with the progress bar and the three
// count tiles, the subtasks section, and a right rail with the auto status, the actions and the
// Details table. Open/close is local (URL mirrored, no document request); the Flag form is the
// one write and it is gated on the page contract's control.
function FlagForm({ pageContract, row, returnTo, hot }: { pageContract: AdminUiPageContract; row: WorkBoardRow; returnTo: string; hot: boolean }) {
  const enabled = controlEnabled(pageContract, "flag_park_head", false);
  const ctl = control(pageContract, "flag_park_head");
  return (
    <form action={flagParkHeadAction} className="flag">
      <input type="hidden" name="row_key" value={row.row_key} />
      <input type="hidden" name="park_id" value={row.park_id} />
      <input type="hidden" name="business_date" value={row.business_date} />
      <input type="hidden" name="return_to" value={returnTo} />
      <TextField
        fullWidth
        id={`flag-note-${row.row_key}`}
        name="note"
        label={copy(pageContract, "flag.note")}
        placeholder={copy(pageContract, "flag.note.placeholder")}
        disabled={!enabled}
        sx={{ mb: 1.5 }}
        slotProps={{ inputLabel: { shrink: true }, htmlInput: { maxLength: 1000 } }}
      />
      <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
        <Button type="submit" size="small" variant={hot ? "contained" : "outlined"} color={hot ? "primary" : "inherit"} disabled={!enabled} title={enabled ? copy(pageContract, "flag.hint") : ctl.disabled_reason}>
          {ctl.label}
        </Button>
        {enabled ? null : <span className="muted small">{ctl.disabled_reason}</span>}
      </div>
    </form>
  );
}

export function WorkBoardModal({ pageContract, rows, initialSelectedRowKey, closeHref, returnToByRow, selectedOwner }: { pageContract: AdminUiPageContract; rows: WorkBoardRow[]; initialSelectedRowKey?: string; closeHref: string; returnToByRow: Record<string, string>; selectedOwner?: string }) {
  const { displayedItem: row, drawerOpen: open, closeDrawer: close } = useLocalOverlaySelection({
    items: rows,
    itemId: (r) => r.row_key,
    selectionKey: PARAM_ROW,
    initialSelectedId: initialSelectedRowKey,
    closeHref,
  });
  const fullScreen = useMediaQuery((theme: Theme) => theme.breakpoints.down("sm"));
  if (!row) return null;
  const moduleOpt = findOption(moduleOptions(pageContract), row.module);
  const stateOpt = findOption(stateOptions(pageContract), row.work_state);
  const laneOpt = lanes(pageContract).find((lane) => lane.key === row.lane);
  const total = row.counts.done + row.counts.pending;
  const split = pendingSplit(row);
  const hot = needsAttention(row);
  const stack = ownerStack(row);
  const ownerLabel = stack.names[0] || (row.owner_state === "pool" ? copy(pageContract, "owner.pool") : copy(pageContract, "owner.missing"));
  const closeLabel = copy(pageContract, "action.close");
  const kv = (k: string, v: React.ReactNode) => (
    <div className="kv">
      <span className="k">{k}</span>
      <span className="v">{v}</span>
    </div>
  );
  return (
    // Template MUI Dialog (portal, theme backdrop, focus trap and return; full screen below sm).
    // URL/Back/Escape stay with useLocalOverlaySelection through `close`.
    <Dialog
      open={open}
      onClose={close}
      fullWidth
      maxWidth="lg"
      fullScreen={fullScreen}
      scroll="paper"
      slotProps={{ paper: { className: "wb wb-dialog", "aria-label": row.title } as object }}
    >
        <DialogTitle component="div" className="mh" sx={{ display: "flex", alignItems: "center", gap: 1.25 }}>
          <div className="bc">
            <span className={moduleClass(row.module)}>{moduleOpt?.label ?? row.module}</span>
            <span>/</span>
            <span className={`ti${row.module === "counts" ? " p" : ""}`} aria-hidden="true">▣</span>
            <b>{row.pen.operational_location_display || parkLabel(parkOptions(pageContract), row)}</b>
          </div>
          <Box sx={{ flex: 1 }} />
          <IconButton aria-label={closeLabel} onClick={close}>
            <X size={20} aria-hidden="true" />
          </IconButton>
        </DialogTitle>
        <DialogContent dividers className="mb" sx={{ display: "grid", gridTemplateColumns: { xs: "minmax(0,1fr)", md: "minmax(0,1fr) 380px" }, gap: 3 }}>
          <div>
            <h2>{row.title}</h2>
            <div className="sec" style={{ marginTop: 6 }}>
              <h4>{copy(pageContract, "drawer.description")}</h4>
              <div className="desc">
                {row.subtitle ? <div>{row.subtitle}</div> : null}
                <div className="bigprog">
                  {total > 0 ? <WorkProgress row={row} size="lg" /> : null}
                  <div className="cnts">
                    <div className="ct ok">
                      <div className="l">{copy(pageContract, "tile.done")}</div>
                      <div className="v">
                        {row.counts.done}
                        {total > 0 ? <small>/ {total}</small> : null}
                      </div>
                    </div>
                    <div className="ct pend">
                      <div className="l">{copy(pageContract, "tile.pending")}</div>
                      <div className="v">
                        {row.counts.pending}
                        {total > 0 ? <small>/ {total}</small> : null}
                      </div>
                      {split ? (
                        // Where the pending work is, when the source said (the feed cards): the
                        // same split the card shows, so the drawer never reads differently.
                        <div className="muted small" style={{ marginTop: "var(--sp-half)" }}>
                          {[
                            split.inReview > 0 ? `${split.inReview} ${copy(pageContract, "card.in_review")}` : "",
                            split.started > 0 ? `${split.started} ${copy(pageContract, "card.started")}` : "",
                            split.notStarted > 0 ? `${split.notStarted} ${copy(pageContract, "card.not_started")}` : "",
                          ]
                            .filter(Boolean)
                            .join(" · ")}
                        </div>
                      ) : null}
                    </div>
                    <div className={`ct${row.counts.needs_attention > 0 ? " hot" : ""}`}>
                      <div className="l">{copy(pageContract, "tile.attention")}</div>
                      <div className="v">{row.counts.needs_attention}</div>
                    </div>
                  </div>
                </div>
              </div>
            </div>
            <div className="sec">
              <h4>{copy(pageContract, "drawer.subtasks")}</h4>
              {open ? <WorkBoardSubtasks key={`${row.row_key}:${selectedOwner ?? ""}`} pageContract={pageContract} row={row} selectedOwner={selectedOwner} /> : null}
            </div>
          </div>
          <div className="rail">
            <div className="actions">
              <span className={`status ${row.lane}`} title={copy(pageContract, "drawer.status_auto.title")}>
                {laneOpt?.label ?? row.lane} · {copy(pageContract, "drawer.status_auto")}
              </span>
              {row.href ? (
                <LinkButton href={row.href} size="small" variant="outlined" color="inherit">
                  {copy(pageContract, "drawer.open_module")} →
                </LinkButton>
              ) : null}
            </div>
            <div className="dets">
              <div className="dh">
                {copy(pageContract, "drawer.details")}
                <span className="sp" />
                <Settings className="ic" aria-hidden="true" />
              </div>
              {kv(copy(pageContract, "detail.status"), <><span className={`status sm ${row.lane}`}>{stateOpt?.label ?? row.work_state}</span><Tag tone="mut">{copy(pageContract, "drawer.status_auto")}</Tag></>)}
              {kv(copy(pageContract, "detail.module"), <span className={moduleClass(row.module)}>{moduleOpt?.label ?? row.module}</span>)}
              {kv(copy(pageContract, "detail.park"), <span title={row.park_name || undefined}>{parkLabel(parkOptions(pageContract), row)}</span>)}
              {kv(copy(pageContract, "detail.pen"), row.pen.operational_location_display || "—")}
              {kv(copy(pageContract, "detail.owner"), <><AvatarGroup className="stack" names={stack.names} extra={stack.extra} size={24} /><span className={stack.names.length ? "" : "muted"}>{ownerLabel}</span></>)}
              {kv(copy(pageContract, "detail.clock"), row.clock_label ? <ClockLabel row={row} /> : "—")}
              {kv(copy(pageContract, "detail.business_date"), dayLabel(row.business_date))}
            </div>
            <FlagForm pageContract={pageContract} row={row} returnTo={returnToByRow[row.row_key] ?? closeHref} hot={hot} />
          </div>
        </DialogContent>
    </Dialog>
  );
}
