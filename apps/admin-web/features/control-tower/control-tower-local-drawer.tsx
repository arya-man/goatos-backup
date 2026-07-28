"use client";

import Link from "@/components/no-prefetch-link";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ControlTowerAlert } from "@/lib/api/server";
import { AlertTriangle, X } from "lucide-react";
import type { Tone } from "@/features/process-integrity";

const SEVERITY_FILL = {
  broken: { bg: "var(--dangerx)", fg: "var(--danger)" },
  at_risk: { bg: "var(--warnx)", fg: "var(--warn)" },
  watch: { bg: "var(--infox)", fg: "var(--info)" },
  ok: { bg: "var(--okx)", fg: "var(--brand-d)" },
} as const;

export type ControlTowerDrawerRecord = {
  alert: ControlTowerAlert;
  actionCenterHref: string;
  workflowHref: string;
  adherenceHref: string;
  vaccinationHref: string;
};

function alertId(record: ControlTowerDrawerRecord): string {
  return record.alert.row_id;
}

function evidenceSummary(alert: ControlTowerAlert, pageContract: AdminUiPageContract): string {
  const evidence = alert.evidence_summary?.trim();
  const proof = alert.proof_summary?.trim();
  if (evidence && proof) return `${evidence}. ${proof}`;
  if (evidence) return evidence;
  if (proof) return proof;
  return optionLabel(pageContract, "work_state_filter_chips", alert.work_state) || copy(pageContract, "label.not_ready");
}

export function ControlTowerLocalDrawer({
  records,
  pageContract,
  initialSelectedAlertId,
  closeHref,
}: {
  records: ControlTowerDrawerRecord[];
  pageContract: AdminUiPageContract;
  initialSelectedAlertId?: string;
  closeHref: string;
}) {
  const { displayedItem: displayedRecord, drawerOpen, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
    items: records,
    itemId: alertId,
    selectionKey: "ct_alert",
    initialSelectedId: initialSelectedAlertId,
    closeHref,
  });

  return (
    <>
      {displayedRecord ? (
        <button
          type="button"
          className={`scrim${drawerOpen ? " on" : ""}`}
          data-testid="control-tower-drawer-scrim"
          aria-label={copy(pageContract, "drawer.alert.close_label")}
          aria-hidden={!drawerOpen}
          tabIndex={drawerOpen ? 0 : -1}
          onClick={closeDrawer}
        />
      ) : null}
      {displayedRecord ? (
        <ControlTowerAlertDrawer
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

function ControlTowerAlertDrawer({
  record,
  pageContract,
  open,
  closeDrawer,
  closeButtonRef,
}: {
  record: ControlTowerDrawerRecord;
  pageContract: AdminUiPageContract;
  open: boolean;
  closeDrawer: () => void;
  closeButtonRef: React.RefObject<HTMLButtonElement | null>;
}) {
  const { alert } = record;
  const fill = SEVERITY_FILL[alert.severity];
  const owner = alert.owner?.operator_name ?? alert.owner?.park_head_name ?? copy(pageContract, "label.owner_unassigned");
  return (
    <aside className={`drawer${open ? " on" : ""}`} aria-label={copy(pageContract, "drawer.alert.aria")} aria-hidden={!open} inert={!open}>
      <div className="dh">
        <span className="fic" style={{ background: fill.bg, color: fill.fg }}>
          <AlertTriangle className="ic" aria-hidden="true" />
        </span>
        <div>
          <div className="mt">{pageContract.title}</div>
          <h2>{alert.title}</h2>
        </div>
        <span className="sp" style={{ flex: 1 }} />
        <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={copy(pageContract, "drawer.alert.close_label")} onClick={closeDrawer}>
          <X className="ic" />
        </button>
      </div>
      <div className="dc">
        <div className="metagrid">
          <div><div className="k">{copy(pageContract, "label.gap")}</div><div className="v"><Tag tone={optionTone(pageContract, "work_state_filter_chips", alert.work_state) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", alert.work_state)}</Tag></div></div>
          <div><div className="k">{copy(pageContract, "label.severity")}</div><div className="v"><Tag tone={optionTone(pageContract, "severity_chips", alert.severity) as Tone}>{optionLabel(pageContract, "severity_chips", alert.severity)}</Tag></div></div>
          <div><div className="k">{copy(pageContract, "label.scope")}</div><div className="v">{alert.scope_label}</div></div>
          <div><div className="k">{copy(pageContract, "label.detail")}</div><div className="v">{alert.detail}</div></div>
          <div><div className="k">{copy(pageContract, "label.owner")}</div><div className="v">{owner}</div></div>
          <div><div className="k">{copy(pageContract, "label.next_action")}</div><div className="v">{alert.next_action}</div></div>
          <div><div className="k">{copy(pageContract, "label.evidence")}</div><div className="v">{evidenceSummary(alert, pageContract)}</div></div>
        </div>
        <div className="note" style={{ marginTop: 14 }}>{copy(pageContract, "drawer.alert.guidance")}</div>
      </div>
      <div className="df">
        <Link href={record.actionCenterHref} className="btn p">{copy(pageContract, "action.open_action_center")}</Link>
        <Link href={record.workflowHref} className="btn">{copy(pageContract, "action.open_workflow")}</Link>
        <Link href={record.adherenceHref} className="btn">{copy(pageContract, "action.open_adherence")}</Link>
        <Link href={record.vaccinationHref} className="btn">{copy(pageContract, "action.open_vaccination")}</Link>
      </div>
    </aside>
  );
}
