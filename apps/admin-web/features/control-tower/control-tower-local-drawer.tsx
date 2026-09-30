"use client";

import { LinkButton } from "@/components/app/link-button";
import { DetailDrawer, DrawerMetaGrid, DrawerMetaItem, DrawerNote } from "@/components/app/detail-drawer";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ControlTowerAlert } from "@/lib/api/server";
import { Iconify } from "@/components/minimal/iconify";
import type { Tone } from "@/features/process-integrity";

const SEVERITY_FILL = {
  broken: { bg: "var(--dangerx)", fg: "var(--palette-error-main)" },
  at_risk: { bg: "var(--warnx)", fg: "var(--palette-warning-main)" },
  watch: { bg: "var(--infox)", fg: "var(--palette-info-main)" },
  ok: { bg: "var(--okx)", fg: "var(--palette-primary-dark)" },
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
  const { displayedItem: displayedRecord, drawerOpen, closeDrawer } = useLocalOverlaySelection({
    items: records,
    itemId: alertId,
    selectionKey: "ct_alert",
    initialSelectedId: initialSelectedAlertId,
    closeHref,
  });

  if (!displayedRecord) return null;
  return <ControlTowerAlertDrawer record={displayedRecord} pageContract={pageContract} open={drawerOpen} closeDrawer={closeDrawer} />;
}

function ControlTowerAlertDrawer({
  record,
  pageContract,
  open,
  closeDrawer,
}: {
  record: ControlTowerDrawerRecord;
  pageContract: AdminUiPageContract;
  open: boolean;
  closeDrawer: () => void;
}) {
  const { alert } = record;
  const fill = SEVERITY_FILL[alert.severity];
  const owner = alert.owner?.operator_name ?? alert.owner?.park_head_name ?? copy(pageContract, "label.owner_unassigned");
  return (
    <DetailDrawer
      open={open}
      onClose={closeDrawer}
      title={alert.title}
      eyebrow={pageContract.title}
      icon={<Iconify icon="solar:danger-triangle-bold" aria-hidden="true" />}
      iconColors={fill}
      ariaLabel={copy(pageContract, "drawer.alert.aria")}
      closeLabel={copy(pageContract, "drawer.alert.close_label")}
      footer={
        <>
          <LinkButton href={record.actionCenterHref} variant="contained">{copy(pageContract, "action.open_action_center")}</LinkButton>
          <LinkButton href={record.workflowHref} variant="outlined" color="inherit">{copy(pageContract, "action.open_workflow")}</LinkButton>
          <LinkButton href={record.adherenceHref} variant="outlined" color="inherit">{copy(pageContract, "action.open_adherence")}</LinkButton>
          <LinkButton href={record.vaccinationHref} variant="outlined" color="inherit">{copy(pageContract, "action.open_vaccination")}</LinkButton>
        </>
      }
    >
      <DrawerMetaGrid>
        <DrawerMetaItem label={copy(pageContract, "label.gap")}><Tag tone={optionTone(pageContract, "work_state_filter_chips", alert.work_state) as Tone}>{optionLabel(pageContract, "work_state_filter_chips", alert.work_state)}</Tag></DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.severity")}><Tag tone={optionTone(pageContract, "severity_chips", alert.severity) as Tone}>{optionLabel(pageContract, "severity_chips", alert.severity)}</Tag></DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.scope")}>{alert.scope_label}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.detail")}>{alert.detail}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.owner")}>{owner}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.next_action")}>{alert.next_action}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.evidence")} span>{evidenceSummary(alert, pageContract)}</DrawerMetaItem>
      </DrawerMetaGrid>
      <DrawerNote>{copy(pageContract, "drawer.alert.guidance")}</DrawerNote>
    </DetailDrawer>
  );
}
