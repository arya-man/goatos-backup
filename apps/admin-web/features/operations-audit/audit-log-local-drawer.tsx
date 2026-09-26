"use client";

import Link from "@/components/no-prefetch-link";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import type { Tone } from "@/components/ui-primitives";
import { copy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { dash } from "@/lib/format";
import { ClipboardList, Database, Syringe, Truck, Zap } from "lucide-react";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";
import { LinkButton } from "@/components/minimal/link-button";
import { DetailDrawer, DrawerMetaGrid, DrawerMetaItem, DrawerNote } from "@/components/app/detail-drawer";

export type AuditDrawerRecord = {
  id: string;
  anomaly: boolean;
  actionLabel: string;
  recordedAt: string;
  result: string;
  proof?: string;
  operation: { key: string; label: string; detail?: string };
  operator: { primary: string; secondary: string };
  target: { label: string; href?: string };
};

function auditId(record: AuditDrawerRecord): string {
  return record.id;
}

const OPERATION_ICONS = {
  vaccination: Syringe,
  procurement: Truck,
  counts: ClipboardList,
  admin: Database,
} as const;

function toneForResult(result: string): Tone {
  const normalized = result.toLowerCase();
  if (["failed", "rejected", "rollback", "deleted", "skipped", "mismatch", "flagged"].includes(normalized)) return "dng";
  if (["queued", "pending", "awaiting", "awaiting_verification", "verification_pending", "rework"].includes(normalized)) return "warn";
  if (["accepted", "success", "succeeded", "completed", "recorded"].includes(normalized)) return "ok";
  return "info";
}

export function AuditLogLocalDrawer({
  records,
  initialSelectedAuditId,
  closeHref,
  pageContract,
}: {
  records: AuditDrawerRecord[];
  initialSelectedAuditId?: string;
  closeHref: string;
  pageContract: AdminUiPageContract;
}) {
  const { displayedItem: displayedRecord, drawerOpen, closeDrawer } = useLocalOverlaySelection({
    items: records,
    itemId: auditId,
    selectionKey: "audit_id",
    initialSelectedId: initialSelectedAuditId,
    closeHref,
  });

  if (!displayedRecord) return null;
  return <AuditDetailDrawer record={displayedRecord} pageContract={pageContract} open={drawerOpen} closeDrawer={closeDrawer} />;
}

function AuditDetailDrawer({
  record,
  pageContract,
  open,
  closeDrawer,
}: {
  record: AuditDrawerRecord;
  pageContract: AdminUiPageContract;
  open: boolean;
  closeDrawer: () => void;
}) {
  const OperationIcon = OPERATION_ICONS[record.operation.key as keyof typeof OPERATION_ICONS] ?? Zap;
  const cols = tableLabels(pageContract, "activity-trail");
  return (
    <DetailDrawer
      open={open}
      onClose={closeDrawer}
      title={record.actionLabel}
      eyebrow={copy(pageContract, "drawer.record.eyebrow")}
      icon={<OperationIcon aria-hidden="true" />}
      ariaLabel={copy(pageContract, "drawer.record.aria")}
      closeLabel={copy(pageContract, "drawer.record.close_label")}
      footer={
        <>
          {record.target.href ? (
            <LinkButton href={record.target.href} variant="contained">{copy(pageContract, "drawer.open_target")}</LinkButton>
          ) : (
            <Button variant="contained" color="primary" disabled>{copy(pageContract, "drawer.open_target")}</Button>
          )}
          <Button variant="outlined" color="inherit" onClick={closeDrawer}>{copy(pageContract, "action.close")}</Button>
        </>
      }
    >
      <DrawerMetaGrid>
        <DrawerMetaItem label={cols[0]}>{record.recordedAt}</DrawerMetaItem>
        <DrawerMetaItem label={cols[1]}>
          {record.operation.label}
          {record.operation.detail ? <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{record.operation.detail}</Typography> : null}
        </DrawerMetaItem>
        <DrawerMetaItem label={cols[2]}>
          {record.operator.primary}
          <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{record.operator.secondary}</Typography>
        </DrawerMetaItem>
        <DrawerMetaItem label={cols[4]}>{record.target.href ? <Link href={record.target.href} className="gid">{record.target.label}</Link> : record.target.label}</DrawerMetaItem>
        <DrawerMetaItem label={cols[5]}><Tag tone={record.anomaly ? "dng" : toneForResult(record.result)}>{record.result}</Tag></DrawerMetaItem>
        <DrawerMetaItem label={cols[6]}>{dash(record.proof)}</DrawerMetaItem>
      </DrawerMetaGrid>
      <DrawerNote>{copy(pageContract, "drawer.record.note")}</DrawerNote>
    </DetailDrawer>
  );
}
