"use client";

import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";
import { LinkButton } from "@/components/minimal/link-button";
import { DetailDrawer, DrawerMetaGrid, DrawerMetaItem, DrawerNote } from "@/components/app/detail-drawer";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { copy, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { AdherenceRow } from "@/lib/api/server";
import { Syringe } from "lucide-react";
import type { Tone } from "./process-integrity";
import { EvidenceMedia } from "./evidence-media";

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
  const { displayedItem: displayedRecord, drawerOpen, closeDrawer } = useLocalOverlaySelection({
    items: records,
    itemId: adherenceRowId,
    selectionKey: "adh_row",
    initialSelectedId: initialSelectedRowId,
    closeHref,
  });

  if (!displayedRecord) return null;
  return (
    <AdherenceRecordDrawer record={displayedRecord} ledgerLabels={ledgerLabels} pageContract={pageContract} open={drawerOpen} closeDrawer={closeDrawer} />
  );
}

// Adherence is a computed read projection. The drawer is local UI; the real actions remain distinct-page links.
function AdherenceRecordDrawer({
  record,
  ledgerLabels,
  pageContract,
  open,
  closeDrawer,
}: {
  record: ProtocolAdherenceDrawerRecord;
  ledgerLabels: string[];
  pageContract: AdminUiPageContract;
  open: boolean;
  closeDrawer: () => void;
}) {
  const { row } = record;

  return (
    <DetailDrawer
      open={open}
      onClose={closeDrawer}
      title={record.expectedTitle}
      eyebrow={copy(pageContract, "drawer.record.eyebrow")}
      icon={<Syringe aria-hidden="true" />}
      ariaLabel={copy(pageContract, "drawer.record.aria")}
      closeLabel={copy(pageContract, "drawer.record.close_label")}
      footer={
        <>
          <LinkButton href={record.workflowHref} variant="contained">{row.next_action}</LinkButton>
          <LinkButton href={record.actionCenterHref} variant="outlined" color="inherit">{copy(pageContract, "action.open_action_center")}</LinkButton>
          <LinkButton href={record.workflowHref} variant="outlined" color="inherit">{copy(pageContract, "action.workflow_record")}</LinkButton>
          <Button variant="outlined" color="inherit" onClick={closeDrawer}>{copy(pageContract, "action.close")}</Button>
        </>
      }
    >
      <DrawerMetaGrid>
        <DrawerMetaItem label={ledgerLabels[0]}>
          {record.expectedTitle}
          <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{record.expectedDetail}</Typography>
        </DrawerMetaItem>
        <DrawerMetaItem label={ledgerLabels[1]}>{record.actual}</DrawerMetaItem>
        <DrawerMetaItem label={ledgerLabels[2]}><Tag tone={workStateTone(pageContract, row.work_state)}>{record.gap}</Tag></DrawerMetaItem>
        <DrawerMetaItem label={ledgerLabels[3]}><Tag tone={optionTone(pageContract, "severity_chips", row.severity) as Tone}>{optionLabel(pageContract, "severity_chips", row.severity)}</Tag></DrawerMetaItem>
        <DrawerMetaItem label={ledgerLabels[4]}>{record.owner}</DrawerMetaItem>
        <DrawerMetaItem label={ledgerLabels[5]}>{row.next_action}</DrawerMetaItem>
        <DrawerMetaItem label={ledgerLabels[6]} span><EvidenceMedia evidence={row.evidence} pageContract={pageContract} /></DrawerMetaItem>
      </DrawerMetaGrid>
      <DrawerNote>{copy(pageContract, "drawer.record.note")}</DrawerNote>
    </DetailDrawer>
  );
}
