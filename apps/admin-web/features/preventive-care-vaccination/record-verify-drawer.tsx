"use client";

import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Typography from "@mui/material/Typography";
import { LinkButton } from "@/components/minimal/link-button";
import { DetailDrawer, DrawerMetaGrid, DrawerMetaItem, DrawerNote } from "@/components/app/detail-drawer";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Syringe } from "lucide-react";
import type {
  VaccinationOperationsCell,
  VaccinationOperationsCohort,
  VaccinationOperationsCounts,
  VaccinationOperationsProtocol,
} from "@/lib/api/server";
import { Tag, type Tone } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
import { copy, optionGroup, optionLabel, optionTone, tableLabels, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { scopeHref, type Scope } from "@/lib/scope";
import { operationalLocationLabel } from "@/lib/operational-location.ts";
import { vaccinationProtocolDisplayName } from "./vaccine-display";
import { stageLabel } from "@/lib/stage-labels";

// Shared vaccination work-context drawer. Cohort/protocol rows are rollups:
// they carry counts and links, not a single executable SOP task id.

type CountKey = keyof VaccinationOperationsCounts;

function statusMeta(pageContract: AdminUiPageContract, workState: string): { label: string; tone: Tone } {
  return {
    label: optionLabel(pageContract, "work_state_filter_chips", workState),
    tone: optionTone(pageContract, "work_state_filter_chips", workState) as Tone,
  };
}

// Read-only vaccination shed completion summary. Displays scan progress, proof readiness,
// vaccine breakdown, and submission status without fillable manual form fields.
export function VaccinationRecordFormFields({ cohortShed, vaccineName, pageContract }: { cohortShed: string; vaccineName: string; pageContract: AdminUiPageContract }) {
  return (
    <>
      <DrawerNote>{copy(pageContract, "drawer.record_verify.record_reason")}</DrawerNote>
      <DrawerMetaGrid>
        <DrawerMetaItem label={copy(pageContract, "drawer.record_verify.form.shed_name")}>{cohortShed}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "drawer.record_verify.form.vaccine")}>{vaccineName}</DrawerMetaItem>
      </DrawerMetaGrid>
    </>
  );
}

export type VaccinationRecordContext = {
  cohort: VaccinationOperationsCohort;
  protocol?: VaccinationOperationsProtocol | null;
  cell?: VaccinationOperationsCell | null;
};

export type VaccinationRecordSelection = {
  id: string;
  context: VaccinationRecordContext;
};

function selectionId(selection: VaccinationRecordSelection): string {
  return selection.id;
}

export function VaccinationRecordVerifyLocalDrawer({
  records,
  selectionKey,
  initialSelectedId,
  scope,
  closePath = "/vaccination",
  pageContract,
}: {
  records: VaccinationRecordSelection[];
  selectionKey: string;
  initialSelectedId?: string;
  scope: Scope;
  closePath?: string;
  pageContract: AdminUiPageContract;
}) {
  const closeHref = scopeHref(closePath, scope);
  const { displayedItem, drawerOpen, closeDrawer } = useLocalOverlaySelection({
    items: records,
    itemId: selectionId,
    selectionKey,
    initialSelectedId,
    closeHref,
  });
  if (!displayedItem) return null;

  return (
    <VaccinationRecordVerifyDrawer
      context={displayedItem.context}
      scope={scope}
      pageContract={pageContract}
      drawerOpen={drawerOpen}
      closeDrawer={closeDrawer}
    />
  );
}

function VaccinationRecordVerifyDrawer({
  context,
  scope,
  pageContract,
  drawerOpen,
  closeDrawer,
}: {
  context: VaccinationRecordContext;
  scope: Scope;
  pageContract: AdminUiPageContract;
  drawerOpen: boolean;
  closeDrawer: () => void;
}) {
  const { cohort, protocol, cell } = context;
  const counts = (cell?.counts ?? cohort.counts ?? {}) as Partial<VaccinationOperationsCounts>;
  const workState = cell?.workState ?? cohort.workState;
  const meta = statusMeta(pageContract, workState);
  const lastDose = cell?.lastDose ?? cohort.lastDose;
  const nextDue = cell?.nextDue ?? cohort.nextDue;
  const vaccineName = protocol ? vaccinationProtocolDisplayName(protocol) : copy(pageContract, "drawer.record_verify.all_protocols");
  const cohortLabels = tableLabels(pageContract, "cohort-detail");
  const countChips = optionGroup(pageContract, "obligation_count_chips") as Array<AdminUiOption & { key: CountKey }>;

  const actionCenterHref = scopeHref("/action-center", scope, {}, { state: workState });
  const adherenceHref = scopeHref("/protocol-adherence", scope);
  const countFor = (key: CountKey): number => Number(counts[key] ?? 0);

  const shedLabel = cohort.operationalLocationDisplay || operationalLocationLabel({ shedName: cohort.shedName, partitionLabel: cohort.partitionLabel });
  const placeholder = copy(pageContract, "label.placeholder");

  return (
    <DetailDrawer
      open={drawerOpen}
      onClose={closeDrawer}
      title={copy(pageContract, "drawer.record_verify.title")}
      eyebrow={copy(pageContract, "drawer.record_verify.eyebrow")}
      icon={<Syringe aria-hidden="true" />}
      ariaLabel={copy(pageContract, "drawer.record_verify.aria")}
      closeLabel={copy(pageContract, "drawer.record_verify.close_label")}
      footer={
        <>
          <LinkButton href={actionCenterHref} scroll={false} variant="contained">
            {copy(pageContract, "action.open_action_center")}
          </LinkButton>
          <LinkButton href={adherenceHref} scroll={false} variant="outlined" color="inherit">
            {copy(pageContract, "action.open_protocol_adherence")}
          </LinkButton>
          <Button variant="outlined" color="inherit" onClick={closeDrawer}>
            {copy(pageContract, "action.cancel")}
          </Button>
        </>
      }
    >
      <DrawerNote>{copy(pageContract, "drawer.record_verify.note")}</DrawerNote>

      {/* Real cohort × protocol context (RECORD anatomy = two-column meta grid, never a flat stack). */}
      <DrawerMetaGrid>
        {/* Render the backend-composed operational location: "Godel 1 - Part 3", not bare "Godel 1" when partitioned */}
        <DrawerMetaItem label={copy(pageContract, "drawer.record_verify.form.shed_name")}>{shedLabel}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "drawer.record_verify.form.vaccine")}>{vaccineName}</DrawerMetaItem>
        <DrawerMetaItem label={cohortLabels[1]}>{cohort.animals || placeholder}</DrawerMetaItem>
        <DrawerMetaItem label={cohortLabels[2]}>{cohort.ageBand ?? stageLabel(cohort.stage)}</DrawerMetaItem>
        <DrawerMetaItem label={cohortLabels[3]}>{lastDose ? fmtDate(lastDose) : placeholder}</DrawerMetaItem>
        <DrawerMetaItem label={cohortLabels[4]}>{nextDue ? fmtDate(nextDue) : placeholder}</DrawerMetaItem>
        <DrawerMetaItem label={cohortLabels[5]}>
          <Tag tone={meta.tone}>{meta.label}</Tag>
        </DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.park")}>{cohort.parkName}</DrawerMetaItem>
      </DrawerMetaGrid>

      {/* Live obligation/completion tallies for this cell (or cohort rollup). */}
      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
        {countChips.map(({ key, label, tone }) => {
          const n = countFor(key);
          if (!n) return null;
          return (
            <Tag key={key} tone={(tone || "mut") as Tone}>
              {label} · {n}
            </Tag>
          );
        })}
        {countChips.every(({ key }) => countFor(key) === 0) ? (
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            {copy(pageContract, "drawer.record_verify.no_obligations")}
          </Typography>
        ) : null}
      </Box>

      <VaccinationRecordFormFields cohortShed={shedLabel} vaccineName={vaccineName} pageContract={pageContract} />
    </DetailDrawer>
  );
}
