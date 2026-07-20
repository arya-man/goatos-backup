"use client";

import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, optionGroup, optionLabel, optionTone, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { OutboxDLQMessage } from "@/lib/api/server";
import { dash, fmtDateTime, shortId } from "@/lib/format";
import { CheckCircle2, DatabaseZap, RotateCcw, Trash2, X } from "lucide-react";
import type { ReactNode } from "react";
import { discardDLQAction, replayDLQAction } from "./actions";

export type DLQDrawerRecord = Pick<
  OutboxDLQMessage,
  | "outbox_id"
  | "event_type"
  | "status"
  | "event_id"
  | "aggregate_type"
  | "aggregate_id"
  | "idempotency_key"
  | "trace_id"
  | "created_at"
  | "updated_at"
  | "attempt_count"
  | "replay_count"
  | "last_error"
  | "headers"
  | "payload"
>;

function outboxId(row: DLQDrawerRecord): string {
  return row.outbox_id;
}

export function DLQLocalDrawer({
  rows,
  pageContract,
  initialSelectedOutboxId,
  closeHref,
}: {
  rows: DLQDrawerRecord[];
  pageContract: AdminUiPageContract;
  initialSelectedOutboxId?: string;
  closeHref: string;
}) {
  const { displayedItem: displayedRow, drawerOpen, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
    items: rows,
    itemId: outboxId,
    selectionKey: "dlq_id",
    initialSelectedId: initialSelectedOutboxId,
    closeHref,
  });

  return (
    <>
      {displayedRow ? (
        <button
          type="button"
          className={`scrim${drawerOpen ? " on" : ""}`}
          data-testid="dlq-drawer-scrim"
          aria-label={copy(pageContract, "drawer.record.close_label")}
          aria-hidden={!drawerOpen}
          tabIndex={drawerOpen ? 0 : -1}
          onClick={closeDrawer}
        />
      ) : null}
      {displayedRow ? (
        <DLQDrawer
          row={displayedRow}
          pageContract={pageContract}
          returnTo={closeHref}
          open={drawerOpen}
          closeDrawer={closeDrawer}
          closeButtonRef={closeButtonRef}
        />
      ) : null}
    </>
  );
}

function DLQDrawer({
  row,
  pageContract,
  returnTo,
  open,
  closeDrawer,
  closeButtonRef,
}: {
  row: DLQDrawerRecord;
  pageContract: AdminUiPageContract;
  returnTo: string;
  open: boolean;
  closeDrawer: () => void;
  closeButtonRef: React.RefObject<HTMLButtonElement | null>;
}) {
  const statusTone = toneForStatus(row.status, pageContract);
  const replayAction = repairAction(pageContract, "replay");
  const discardAction = repairAction(pageContract, "discard");
  const replayDisabledReason = row.status === "discarded" ? copy(pageContract, "reason.repair_discarded") : replayAction.disabled_reason;
  const discardDisabledReason = row.status === "discarded" ? copy(pageContract, "reason.repair_discarded") : discardAction.disabled_reason;
  const replayDisabled = row.status === "discarded" || !replayAction.enabled;
  const discardDisabled = row.status === "discarded" || !discardAction.enabled;
  const repairDisabledReason = replayDisabledReason || discardDisabledReason;
  return (
    <aside className={`drawer${open ? " on" : ""}`} aria-label={copy(pageContract, "drawer.record.aria")} aria-hidden={!open} inert={!open}>
      <div className="dh">
        <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}><DatabaseZap className="ic" aria-hidden="true" /></span>
        <div><div className="mt">{copy(pageContract, "drawer.record.eyebrow")}</div><h2>{row.event_type}</h2></div>
        <span className="sp" style={{ flex: 1 }} />
        <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={copy(pageContract, "drawer.record.close_label")} onClick={closeDrawer}><X className="ic" /></button>
      </div>
      <div className="dc">
        <div className="metagrid">
          <Meta label={copy(pageContract, "label.status")}><Tag tone={statusTone}>{optionLabel(pageContract, "dlq_status_tabs", row.status)}</Tag></Meta>
          <Meta label={copy(pageContract, "label.event_id")}>{row.event_id}</Meta>
          <Meta label={copy(pageContract, "label.aggregate")}>{row.aggregate_type} · {shortId(row.aggregate_id)}</Meta>
          <Meta label={copy(pageContract, "label.idempotency_key")}>{row.idempotency_key}</Meta>
          <Meta label={copy(pageContract, "label.trace")}>{dash(row.trace_id)}</Meta>
          <Meta label={copy(pageContract, "label.created")}>{fmtDateTime(row.created_at)}</Meta>
          <Meta label={copy(pageContract, "label.updated")}>{fmtDateTime(row.updated_at)}</Meta>
          <Meta label={copy(pageContract, "label.attempts")}>{row.attempt_count}</Meta>
          <Meta label={copy(pageContract, "label.replays")}>{row.replay_count}</Meta>
        </div>
        <div className="note" style={{ marginTop: 14 }}>{copy(pageContract, "drawer.record.note")}</div>
        <div className="helpgrid" style={{ marginTop: 14 }}>
          <div className="hk">{copy(pageContract, "label.last_error")}</div><div>{dash(row.last_error)}</div>
          <div className="hk">{copy(pageContract, "label.headers")}</div><pre style={preStyle}>{pretty(row.headers)}</pre>
          <div className="hk">{copy(pageContract, "label.payload")}</div><pre style={preStyle}>{pretty(row.payload)}</pre>
        </div>
        <section className="card" style={{ marginTop: 14 }}>
          <div className="hd"><RotateCcw className="ic" style={{ color: "var(--brand)" }} aria-hidden="true" /><h3>{copy(pageContract, "section.repair.title")}</h3></div>
          <div className="bd">
            <form action={replayDLQAction} style={{ display: "grid", gap: 8, marginBottom: 12 }}>
              <input type="hidden" name="outbox_id" value={row.outbox_id} />
              <input type="hidden" name="return_to" value={returnTo} />
              <label className="fld" style={{ marginBottom: 0 }}><span>{copy(pageContract, "form.reason_label")}</span><textarea name="reason" rows={2} placeholder={copy(pageContract, "form.reason_placeholder")} disabled={replayDisabled} /></label>
              <div className="note">{replayDisabledReason || copy(pageContract, "reason.replay")}</div>
              <button type="submit" className="btn p" disabled={replayDisabled} aria-disabled={replayDisabled} title={replayDisabledReason}><CheckCircle2 className="ic" aria-hidden="true" />{replayAction.label}</button>
            </form>
            <form action={discardDLQAction} style={{ display: "grid", gap: 8 }}>
              <input type="hidden" name="outbox_id" value={row.outbox_id} />
              <input type="hidden" name="return_to" value={returnTo} />
              <label className="fld" style={{ marginBottom: 0 }}><span>{copy(pageContract, "form.reason_label")}</span><textarea name="reason" rows={2} placeholder={copy(pageContract, "form.reason_placeholder")} disabled={discardDisabled} /></label>
              <div className="note">{discardDisabledReason || copy(pageContract, "reason.discard")}</div>
              <button type="submit" className="btn" disabled={discardDisabled} aria-disabled={discardDisabled} title={discardDisabledReason}><Trash2 className="ic" aria-hidden="true" />{discardAction.label}</button>
            </form>
            {repairDisabledReason ? <div className="note" style={{ marginTop: 10 }}>{repairDisabledReason}</div> : null}
          </div>
        </section>
      </div>
      <div className="df"><button type="button" className="btn" onClick={closeDrawer}>{copy(pageContract, "action.close")}</button></div>
    </aside>
  );
}

function repairAction(pageContract: AdminUiPageContract, key: string): AdminUiOption {
  const action = optionGroup(pageContract, "dlq_repair_actions").find((item) => item.key === key);
  if (!action) throw new Error(`Admin-web page contract ${pageContract.route_id} missing option dlq_repair_actions.${key}`);
  return action;
}

function toneForStatus(status: string, pageContract: AdminUiPageContract): Tone {
  const value = optionTone(pageContract, "dlq_status_tabs", status);
  return ["ok", "warn", "dng", "info", "mut", "pur", "teal"].includes(value) ? (value as Tone) : "mut";
}

function Meta({ label, children }: { label: string; children: ReactNode }) {
  return <div><span className="muted small">{label}</span><b style={{ display: "block", marginTop: 3, overflowWrap: "anywhere" }}>{children}</b></div>;
}

function pretty(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2);
}

const preStyle = {
  margin: 0,
  whiteSpace: "pre-wrap",
  overflowWrap: "anywhere",
  fontSize: 11,
  lineHeight: 1.5,
} as const;
