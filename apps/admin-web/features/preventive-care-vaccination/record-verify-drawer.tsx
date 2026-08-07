"use client";

import Link from "@/components/no-prefetch-link";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import type { RefObject } from "react";
import { Syringe, X } from "lucide-react";
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
      <div className="note" style={{ margin: "14px 0 12px" }}>
        {copy(pageContract, "drawer.record_verify.record_reason")}
      </div>
      <div className="metagrid">
        <div>
          <div className="k">{copy(pageContract, "drawer.record_verify.form.shed_name")}</div>
          <div className="v">{cohortShed}</div>
        </div>
        <div>
          <div className="k">{copy(pageContract, "drawer.record_verify.form.vaccine")}</div>
          <div className="v">{vaccineName}</div>
        </div>
      </div>
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
  const { displayedItem, drawerOpen, closeDrawer, closeButtonRef } = useLocalOverlaySelection({
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
      closeButtonRef={closeButtonRef}
    />
  );
}

function VaccinationRecordVerifyDrawer({
  context,
  scope,
  pageContract,
  drawerOpen,
  closeDrawer,
  closeButtonRef,
}: {
  context: VaccinationRecordContext;
  scope: Scope;
  pageContract: AdminUiPageContract;
  drawerOpen: boolean;
  closeDrawer: () => void;
  closeButtonRef: RefObject<HTMLButtonElement | null>;
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

  return (
    <>
      <button
        type="button"
        className={`scrim${drawerOpen ? " on" : ""}`}
        aria-label={copy(pageContract, "drawer.record_verify.close_label")}
        aria-hidden={!drawerOpen}
        tabIndex={drawerOpen ? 0 : -1}
        onClick={closeDrawer}
      />
      <aside className={`drawer${drawerOpen ? " on" : ""}`} aria-label={copy(pageContract, "drawer.record_verify.aria")} aria-hidden={!drawerOpen} inert={!drawerOpen}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <Syringe className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">{copy(pageContract, "drawer.record_verify.eyebrow")}</div>
            <h2>{copy(pageContract, "drawer.record_verify.title")}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={copy(pageContract, "drawer.record_verify.close_label")} onClick={closeDrawer}>
            <X className="ic" />
          </button>
        </div>

        <div className="dc">
          <div className="note" style={{ marginBottom: 12 }}>
            {copy(pageContract, "drawer.record_verify.note")}
          </div>

          {/* Real cohort × protocol context (RECORD anatomy = .metagrid, never a flat stack). */}
          <div className="metagrid">
            <div>
              <div className="k">{copy(pageContract, "drawer.record_verify.form.shed_name")}</div>
              <div className="v">
                {/* Render the backend-composed operational location: "Godel 1 - Part 3", not bare "Godel 1" when partitioned */}
                {cohort.operationalLocationDisplay || operationalLocationLabel({ shedName: cohort.shedName, partitionLabel: cohort.partitionLabel })}
              </div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.record_verify.form.vaccine")}</div>
              <div className="v">{vaccineName}</div>
            </div>
            <div>
              <div className="k">{cohortLabels[1]}</div>
              <div className="v">{cohort.animals || copy(pageContract, "label.placeholder")}</div>
            </div>
            <div>
              <div className="k">{cohortLabels[2]}</div>
              <div className="v">{cohort.ageBand ?? cohort.stage}</div>
            </div>
            <div>
              <div className="k">{cohortLabels[3]}</div>
              <div className="v">{lastDose ? fmtDate(lastDose) : copy(pageContract, "label.placeholder")}</div>
            </div>
            <div>
              <div className="k">{cohortLabels[4]}</div>
              <div className="v">{nextDue ? fmtDate(nextDue) : copy(pageContract, "label.placeholder")}</div>
            </div>
            <div>
              <div className="k">{cohortLabels[5]}</div>
              <div className="v">
                <Tag tone={meta.tone}>{meta.label}</Tag>
              </div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "label.park")}</div>
              <div className="v">{cohort.parkName}</div>
            </div>
          </div>

          {/* Live obligation/completion tallies for this cell (or cohort rollup). */}
          <div className="chipset" style={{ margin: "14px 0 4px" }}>
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
              <span className="muted small">{copy(pageContract, "drawer.record_verify.no_obligations")}</span>
            ) : null}
          </div>

          <VaccinationRecordFormFields cohortShed={cohort.operationalLocationDisplay || operationalLocationLabel({ shedName: cohort.shedName, partitionLabel: cohort.partitionLabel })} vaccineName={vaccineName} pageContract={pageContract} />
        </div>

        <div className="df">
          <Link href={actionCenterHref} className="btn" scroll={false}>
            {copy(pageContract, "action.open_action_center")}
          </Link>
          <Link href={adherenceHref} className="btn" scroll={false}>
            {copy(pageContract, "action.open_protocol_adherence")}
          </Link>
          <button type="button" className="btn" onClick={closeDrawer}>
            {copy(pageContract, "action.cancel")}
          </button>
        </div>
      </aside>
    </>
  );
}
