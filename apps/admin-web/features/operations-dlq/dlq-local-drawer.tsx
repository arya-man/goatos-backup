"use client";

import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag, type Tone } from "@/components/ui-primitives";
import { copy, optionGroup, optionLabel, optionTone, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { OutboxDLQMessage } from "@/lib/api/server";
import { dash, fmtDateTime, shortId } from "@/lib/format";
import { Iconify } from "@/components/minimal/iconify";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import { DetailDrawer, DrawerBlock, DrawerMetaGrid, DrawerMetaItem, DrawerNote } from "@/components/app/detail-drawer";
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
  const { displayedItem: displayedRow, drawerOpen, closeDrawer } = useLocalOverlaySelection({
    items: rows,
    itemId: outboxId,
    selectionKey: "dlq_id",
    initialSelectedId: initialSelectedOutboxId,
    closeHref,
  });

  if (!displayedRow) return null;
  return (
    <DLQDrawer row={displayedRow} pageContract={pageContract} returnTo={closeHref} open={drawerOpen} closeDrawer={closeDrawer} />
  );
}

function DLQDrawer({
  row,
  pageContract,
  returnTo,
  open,
  closeDrawer,
}: {
  row: DLQDrawerRecord;
  pageContract: AdminUiPageContract;
  returnTo: string;
  open: boolean;
  closeDrawer: () => void;
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
    <DetailDrawer
      open={open}
      onClose={closeDrawer}
      title={row.event_type}
      eyebrow={copy(pageContract, "drawer.record.eyebrow")}
      icon={<Iconify icon="solar:ssd-round-bold" width={24} aria-hidden="true" />}
      ariaLabel={copy(pageContract, "drawer.record.aria")}
      closeLabel={copy(pageContract, "drawer.record.close_label")}
      footer={
        <Button variant="outlined" color="inherit" onClick={closeDrawer}>
          {copy(pageContract, "action.close")}
        </Button>
      }
    >
      <DrawerMetaGrid>
        <DrawerMetaItem label={copy(pageContract, "label.status")}><Tag tone={statusTone}>{optionLabel(pageContract, "dlq_status_tabs", row.status)}</Tag></DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.event_id")}>{row.event_id}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.aggregate")}>{row.aggregate_type} · {shortId(row.aggregate_id)}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.idempotency_key")}>{row.idempotency_key}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.trace")}>{dash(row.trace_id)}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.created")}>{fmtDateTime(row.created_at)}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.updated")}>{fmtDateTime(row.updated_at)}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.attempts")}>{row.attempt_count}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.replays")}>{row.replay_count}</DrawerMetaItem>
      </DrawerMetaGrid>
      <DrawerNote>{copy(pageContract, "drawer.record.note")}</DrawerNote>
      <DrawerMetaGrid columns={1}>
        <DrawerMetaItem label={copy(pageContract, "label.last_error")}>{dash(row.last_error)}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.headers")}><Box component="pre" sx={preSx}>{pretty(row.headers)}</Box></DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.payload")}><Box component="pre" sx={preSx}>{pretty(row.payload)}</Box></DrawerMetaItem>
      </DrawerMetaGrid>
      <DrawerBlock title={copy(pageContract, "section.repair.title")}>
        <Box component="form" action={replayDLQAction} sx={{ display: "grid", gap: 1.5 }}>
          <Box component="input" type="hidden" name="outbox_id" value={row.outbox_id} />
          <Box component="input" type="hidden" name="return_to" value={returnTo} />
          <TextField name="reason" label={copy(pageContract, "form.reason_label")} placeholder={copy(pageContract, "form.reason_placeholder")} multiline rows={2} fullWidth disabled={replayDisabled} slotProps={{ inputLabel: { shrink: true } }} />
          <DrawerNote>{replayDisabledReason || copy(pageContract, "reason.replay")}</DrawerNote>
          <Button type="submit" variant="contained" color="primary" disabled={replayDisabled} title={replayDisabledReason} startIcon={<Iconify icon="solar:check-circle-bold" width={16} aria-hidden="true" />}>{replayAction.label}</Button>
        </Box>
        <Box component="form" action={discardDLQAction} sx={{ display: "grid", gap: 1.5 }}>
          <Box component="input" type="hidden" name="outbox_id" value={row.outbox_id} />
          <Box component="input" type="hidden" name="return_to" value={returnTo} />
          <TextField name="reason" label={copy(pageContract, "form.reason_label")} placeholder={copy(pageContract, "form.reason_placeholder")} multiline rows={2} fullWidth disabled={discardDisabled} slotProps={{ inputLabel: { shrink: true } }} />
          <DrawerNote>{discardDisabledReason || copy(pageContract, "reason.discard")}</DrawerNote>
          <Button type="submit" variant="outlined" color="inherit" disabled={discardDisabled} title={discardDisabledReason} startIcon={<Iconify icon="solar:trash-bin-trash-bold" width={16} aria-hidden="true" />}>{discardAction.label}</Button>
        </Box>
        {repairDisabledReason ? <DrawerNote>{repairDisabledReason}</DrawerNote> : null}
      </DrawerBlock>
    </DetailDrawer>
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

function pretty(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2);
}

const preSx = {
  m: 0,
  whiteSpace: "pre-wrap",
  overflowWrap: "anywhere",
  typography: "caption",
  fontFamily: "monospace",
  bgcolor: "background.neutral",
  borderRadius: "var(--r-md)",
  p: 1.5,
} as const;
