"use client";

import Link from "@/components/no-prefetch-link";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { AdherenceRow } from "@/lib/api/server";
import { Syringe, X } from "lucide-react";
import type { Tone } from "./process-integrity";

export type ProtocolAdherenceDrawerRecord = {
  row: AdherenceRow;
  expectedTitle: string;
  expectedDetail: string;
  actual: string;
  gap: string;
  owner: string;
  workflowHref: string;
  actionCenterHref: string;
};

function adherenceRowId(record: ProtocolAdherenceDrawerRecord): string {
  return record.row.row_id;
}

function workStateTone(pageContract: AdminUiPageContract, workState: string): Tone {
  return optionTone(pageContract, "work_state_filter_chips", workState) as Tone;
}

export function ProtocolAdherenceLocalDrawer({
  records,
  ledgerLabels,
  closeHref,
  initialSelectedRowId,
  pageContract,
}: {
  records: ProtocolAdherenceDrawerRecord[];
  ledgerLabels: string[];
  closeHref: string;
  initialSelectedRowId?: string;
  pageContract: AdminUiPageContract;
}) {
  const { displayedItem: displayedRecord, drawerOpen, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
    items: records,
    itemId: adherenceRowId,
    selectionKey: "adh_row",
    initialSelectedId: initialSelectedRowId,
    closeHref,
  });

  return (
    <>
      {displayedRecord ? (
        <button
          type="button"
          className={`scrim${drawerOpen ? " on" : ""}`}
          data-testid="protocol-adherence-drawer-scrim"
          aria-label={copy(pageContract, "drawer.record.close_label")}
          aria-hidden={!drawerOpen}
          tabIndex={drawerOpen ? 0 : -1}
          onClick={closeDrawer}
        />
      ) : null}
      {displayedRecord ? (
        <AdherenceRecordDrawer
          record={displayedRecord}
          ledgerLabels={ledgerLabels}
          pageContract={pageContract}
          open={drawerOpen}
          closeDrawer={closeDrawer}
          closeButtonRef={closeButtonRef}
        />
      ) : null}
    </>
  );
}

// Adherence is a computed read projection. The drawer is local UI; the real actions remain distinct-page links.
function AdherenceRecordDrawer({
  record,
  ledgerLabels,
  pageContract,
  open,
  closeDrawer,
  closeButtonRef,
}: {
  record: ProtocolAdherenceDrawerRecord;
  ledgerLabels: string[];
  pageContract: AdminUiPageContract;
  open: boolean;
  closeDrawer: () => void;
  closeButtonRef: React.RefObject<HTMLButtonElement | null>;
}) {
  const { row } = record;
  const evidence = row.evidence;
  const evidenceView = evidence.latest_rejection_reason ? (
    <Tag tone="dng" title={evidence.latest_rejection_reason}>{copy(pageContract, "label.rejected")}</Tag>
  ) : evidence.evidence_count > 0 ? (
    <Tag tone="ok" title={evidence.audit_ref ?? undefined}>
      {evidence.evidence_count} {copy(pageContract, evidence.evidence_count === 1 ? "label.proof_singular" : "label.proof_plural")}
    </Tag>
  ) : <span className="muted">—</span>;

  return (
    <aside className={`drawer${open ? " on" : ""}`} aria-label={copy(pageContract, "drawer.record.aria")} aria-hidden={!open} inert={!open}>
      <div className="dh">
        <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}><Syringe className="ic" aria-hidden="true" /></span>
        <div>
          <div className="mt">{copy(pageContract, "drawer.record.eyebrow")}</div>
          <h2>{record.expectedTitle}</h2>
          <div className="mt">{record.expectedDetail}</div>
        </div>
        <span className="sp" style={{ flex: 1 }} />
        <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={copy(pageContract, "drawer.record.close_label")} onClick={closeDrawer}><X className="ic" /></button>
      </div>
      <div className="dc">
        <div className="metagrid">
          <div><div className="k">{ledgerLabels[0]}</div><div className="v">{record.expectedTitle}</div><div className="mt">{row.expected}</div></div>
          <div><div className="k">{ledgerLabels[1]}</div><div className="v">{record.actual}</div></div>
          <div><div className="k">{ledgerLabels[2]}</div><div className="v"><Tag tone={workStateTone(pageContract, row.work_state)}>{record.gap}</Tag></div></div>
          <div><div className="k">{ledgerLabels[3]}</div><div className="v"><Tag tone={optionTone(pageContract, "severity_chips", row.severity) as Tone}>{optionLabel(pageContract, "severity_chips", row.severity)}</Tag></div></div>
          <div><div className="k">{ledgerLabels[4]}</div><div className="v">{record.owner}</div></div>
          <div><div className="k">{ledgerLabels[5]}</div><div className="v">{row.next_action}</div></div>
          <div><div className="k">{ledgerLabels[6]}</div><div className="v">{evidenceView}</div></div>
        </div>
        <div className="note" style={{ marginTop: 14 }}>{copy(pageContract, "drawer.record.note")}</div>
      </div>
      <div className="df">
        <Link href={record.workflowHref} className="btn p">{row.next_action}</Link>
        <Link href={record.actionCenterHref} className="btn">{copy(pageContract, "action.open_action_center")}</Link>
        <Link href={record.workflowHref} className="btn">{copy(pageContract, "action.workflow_record")}</Link>
        <button type="button" className="btn" onClick={closeDrawer}>{copy(pageContract, "action.close")}</button>
      </div>
    </aside>
  );
}
