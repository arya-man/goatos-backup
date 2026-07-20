"use client";

import Link from "@/components/no-prefetch-link";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import type { Tone } from "@/components/ui-primitives";
import { copy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { dash } from "@/lib/format";
import { ClipboardList, Database, Syringe, Truck, X, Zap } from "lucide-react";

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
  const { displayedItem: displayedRecord, drawerOpen, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
    items: records,
    itemId: auditId,
    selectionKey: "audit_id",
    initialSelectedId: initialSelectedAuditId,
    closeHref,
  });

  return (
    <>
      {displayedRecord ? (
        <button
          type="button"
          className={`scrim${drawerOpen ? " on" : ""}`}
          data-testid="audit-log-drawer-scrim"
          aria-label={copy(pageContract, "drawer.record.close_label")}
          aria-hidden={!drawerOpen}
          tabIndex={drawerOpen ? 0 : -1}
          onClick={closeDrawer}
        />
      ) : null}
      {displayedRecord ? (
        <AuditDetailDrawer
          record={displayedRecord}
          pageContract={pageContract}
          open={drawerOpen}
          closeDrawer={closeDrawer}
          closeButtonRef={closeButtonRef}
        />
      ) : null}
    </>
  );
}

function AuditDetailDrawer({
  record,
  pageContract,
  open,
  closeDrawer,
  closeButtonRef,
}: {
  record: AuditDrawerRecord;
  pageContract: AdminUiPageContract;
  open: boolean;
  closeDrawer: () => void;
  closeButtonRef: React.RefObject<HTMLButtonElement | null>;
}) {
  const OperationIcon = OPERATION_ICONS[record.operation.key as keyof typeof OPERATION_ICONS] ?? Zap;
  const cols = tableLabels(pageContract, "activity-trail");
  return (
    <aside className={`drawer${open ? " on" : ""}`} aria-label={copy(pageContract, "drawer.record.aria")} aria-hidden={!open} inert={!open}>
      <div className="dh">
        <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}><OperationIcon className="ic" aria-hidden="true" /></span>
        <div><div className="mt">{copy(pageContract, "drawer.record.eyebrow")}</div><h2>{record.actionLabel}</h2></div>
        <span className="sp" style={{ flex: 1 }} />
        <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={copy(pageContract, "drawer.record.close_label")} onClick={closeDrawer}><X className="ic" /></button>
      </div>
      <div className="dc">
        <div className="helpgrid">
          <div className="hk">{cols[0]}</div><div>{record.recordedAt}</div>
          <div className="hk">{cols[1]}</div><div><b>{record.operation.label}</b>{record.operation.detail ? <div className="mt">{record.operation.detail}</div> : null}</div>
          <div className="hk">{cols[2]}</div><div><b>{record.operator.primary}</b><div className="mt">{record.operator.secondary}</div></div>
          <div className="hk">{cols[4]}</div><div>{record.target.href ? <Link href={record.target.href} className="gid">{record.target.label}</Link> : record.target.label}</div>
          <div className="hk">{cols[5]}</div><div><Tag tone={record.anomaly ? "dng" : toneForResult(record.result)}>{record.result}</Tag></div>
          <div className="hk">{cols[6]}</div><div>{dash(record.proof)}</div>
        </div>
        <div className="note" style={{ marginTop: 14 }}>{copy(pageContract, "drawer.record.note")}</div>
      </div>
      <div className="df">
        {record.target.href ? (
          <Link href={record.target.href} className="btn p">{copy(pageContract, "drawer.open_target")}</Link>
        ) : (
          <button type="button" className="btn p disabled" aria-disabled="true" disabled>{copy(pageContract, "drawer.open_target")}</button>
        )}
        <button type="button" className="btn" onClick={closeDrawer}>{copy(pageContract, "action.close")}</button>
      </div>
    </aside>
  );
}
