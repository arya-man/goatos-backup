"use client";

import { X } from "lucide-react";
import Link from "@/components/no-prefetch-link";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { control, controlEnabled, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { WorkBoardRow } from "@/lib/api/work-board-server";
import { fmtDate } from "@/lib/format";
import { flagParkHeadAction } from "./actions";
import { barSegments, clockClass, findOption, initials, lanes, moduleClass, moduleOptions, needsAttention, ownerStack, PARAM_ROW, stateOptions } from "./work-board-model";

// The card's detail in the mock's Jira issue-view shape: a centred dialog over a scrim, the
// module and key as breadcrumb, the title, a description with the progress bar and the three
// count tiles, the subtasks section, and a right rail with the auto status, the actions and the
// Details table. Open/close is local (URL mirrored, no document request); the Flag form is the
// one write and it is gated on the page contract's control.
function Bar({ row }: { row: WorkBoardRow }) {
  const seg = barSegments(row);
  return (
    <div className="prog" aria-hidden="true">
      <i className="ok" style={{ width: `${seg.ok}%` }} />
      <i className="rev" style={{ width: `${seg.rev}%` }} />
      <i className="run" style={{ width: `${seg.run}%` }} />
      <i className="brk" style={{ width: `${seg.brk}%` }} />
    </div>
  );
}

function FlagForm({ pageContract, row, returnTo, hot }: { pageContract: AdminUiPageContract; row: WorkBoardRow; returnTo: string; hot: boolean }) {
  const enabled = controlEnabled(pageContract, "flag_park_head", false);
  const ctl = control(pageContract, "flag_park_head");
  return (
    <form action={flagParkHeadAction} className="flag">
      <input type="hidden" name="row_key" value={row.row_key} />
      <input type="hidden" name="park_id" value={row.park_id} />
      <input type="hidden" name="business_date" value={row.business_date} />
      <input type="hidden" name="return_to" value={returnTo} />
      <label className="muted small" htmlFor={`flag-note-${row.row_key}`}>{copy(pageContract, "flag.note")}</label>
      <input id={`flag-note-${row.row_key}`} name="note" maxLength={1000} placeholder={copy(pageContract, "flag.note.placeholder")} disabled={!enabled} />
      <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
        <button type="submit" className={`btn sm${hot ? " b" : ""}`} disabled={!enabled} aria-disabled={!enabled || undefined} title={enabled ? copy(pageContract, "flag.hint") : ctl.disabled_reason}>
          {ctl.label}
        </button>
        <span className="muted small">{enabled ? copy(pageContract, "flag.hint") : ctl.disabled_reason}</span>
      </div>
    </form>
  );
}

export function WorkBoardModal({ pageContract, rows, initialSelectedRowKey, closeHref, returnToByRow }: { pageContract: AdminUiPageContract; rows: WorkBoardRow[]; initialSelectedRowKey?: string; closeHref: string; returnToByRow: Record<string, string> }) {
  const { displayedItem: row, drawerOpen: open, closeDrawer: close, closeButtonRef } = useLocalOverlaySelection({
    items: rows,
    itemId: (r) => r.row_key,
    selectionKey: PARAM_ROW,
    initialSelectedId: initialSelectedRowKey,
    closeHref,
  });
  if (!row) return null;
  const moduleOpt = findOption(moduleOptions(pageContract), row.module);
  const stateOpt = findOption(stateOptions(pageContract), row.work_state);
  const laneOpt = lanes(pageContract).find((lane) => lane.key === row.lane);
  const total = row.counts.done + row.counts.pending;
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
    <>
      <button type="button" className={`wb-scrim${open ? " on" : ""}`} aria-label={closeLabel} aria-hidden={!open} tabIndex={open ? 0 : -1} onClick={close} />
      <div className={`wb wb-modal${open ? " on" : ""}`} role="dialog" aria-modal="true" aria-label={row.title} aria-hidden={!open} inert={!open}>
        <div className="mh">
          <div className="bc">
            <span className={moduleClass(row.module)}>{moduleOpt?.label ?? row.module}</span>
            <span>/</span>
            <span className={`ti${row.module === "counts" ? " p" : ""}`} aria-hidden="true">▣</span>
            <b>{row.pen.operational_location_display || row.park_name || row.park_id}</b>
          </div>
          <span className="sp" />
          <button ref={closeButtonRef} type="button" className="ib" aria-label={closeLabel} onClick={close}>
            <X className="ic" />
          </button>
        </div>
        <div className="mb">
          <div>
            <h2>{row.title}</h2>
            <div className="sec" style={{ marginTop: 6 }}>
              <h4>{copy(pageContract, "drawer.description")}</h4>
              <div className="desc">
                {row.subtitle ? <div>{row.subtitle}</div> : null}
                <div className="bigprog">
                  {total > 0 ? <Bar row={row} /> : null}
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
              <div className="note">
                {copy(pageContract, "drawer.subtasks.in_module")}
                {row.href ? (
                  <>
                    {" "}
                    <Link href={row.href} style={{ color: "var(--brand)", fontWeight: 650 }}>
                      {copy(pageContract, "drawer.open_module")} →
                    </Link>
                  </>
                ) : null}
              </div>
            </div>
          </div>
          <div className="rail">
            <div className="actions">
              <span className={`status ${row.lane}`} title={copy(pageContract, "drawer.status_auto.title")}>
                {laneOpt?.label ?? row.lane} · {copy(pageContract, "drawer.status_auto")}
              </span>
              {row.href ? (
                <Link href={row.href} className="btn sm ghost">
                  {copy(pageContract, "drawer.open_module")} →
                </Link>
              ) : null}
            </div>
            <div className="dets">
              <div className="dh">{copy(pageContract, "drawer.details")}</div>
              {kv(copy(pageContract, "detail.status"), <><span className={`status sm ${row.lane}`}>{stateOpt?.label ?? row.work_state}</span><span className="muted small">{copy(pageContract, "drawer.status_auto")}</span></>)}
              {kv(copy(pageContract, "detail.module"), <span className={moduleClass(row.module)}>{moduleOpt?.label ?? row.module}</span>)}
              {kv(copy(pageContract, "detail.park"), row.park_name || row.park_id)}
              {kv(copy(pageContract, "detail.pen"), row.pen.operational_location_display || "—")}
              {kv(copy(pageContract, "detail.owner"), <><span className="stack">{stack.names.map((name) => <span key={name} className="av" title={name}>{initials(name)}</span>)}{stack.extra > 0 ? <span className="av more">+{stack.extra}</span> : null}</span><span className={stack.names.length ? "" : "muted"}>{ownerLabel}</span></>)}
              {kv(copy(pageContract, "detail.clock"), row.clock_label ? <span className={`clk ${clockClass(row)}`.trim()}>{row.clock_label}</span> : "—")}
              {kv(copy(pageContract, "detail.business_date"), fmtDate(row.business_date))}
            </div>
            <FlagForm pageContract={pageContract} row={row} returnTo={returnToByRow[row.row_key] ?? closeHref} hot={hot} />
          </div>
        </div>
      </div>
    </>
  );
}
