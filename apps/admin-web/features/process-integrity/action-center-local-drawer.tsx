"use client";

import Link from "@/components/no-prefetch-link";
import {
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  currentHistoryEntryIsLocalOverlay,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { InfoHint } from "@/components/app/info-hint";
import { VACCINATION_DRIVE_SOP_STEPS } from "@/lib/vaccination-sop-steps";
import { copy, optionGroup, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ActionCenterObligation, ProcessIntegritySeverity } from "@/lib/api/server";
import { fmtDate } from "@/lib/format";
import { GitBranch, ShieldCheck, Syringe } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { rejectCompletionAction, verifyCompletionAction } from "./actions";
import { type Tone } from "./process-integrity";
import { SopChecklist } from "./sop-checklist";
import { actionWorkTitle } from "./action-center-presenters";
import { EvidenceMedia } from "./evidence-media";
import { operationalLocationLabel } from "@/lib/operational-location";
import { stageLabel } from "@/lib/stage-labels";
import Alert from "@mui/material/Alert";
import Button from "@mui/material/Button";
import Chip from "@mui/material/Chip";
import Divider from "@mui/material/Divider";
import Stack from "@mui/material/Stack";
import { LinkButton } from "@/components/minimal/link-button";
import { phoneTapSx } from "@/components/minimal/_shared/tap";
import { DetailDrawer, DrawerBlock, DrawerMetaGrid, DrawerMetaItem } from "@/components/app/detail-drawer";

const PRIORITY_BY_SEVERITY: Record<ProcessIntegritySeverity, "high" | "med" | "low"> = {
  broken: "high",
  at_risk: "med",
  watch: "low",
  ok: "low",
};

function optionLabel(options: AdminUiOption[], key: string): string {
  const option = options.find((item) => item.key === key);
  if (!option) throw new Error(`Admin-web contract missing option ${key}`);
  return option.label;
}

function optionTone(options: AdminUiOption[], key: string): Tone {
  const option = options.find((item) => item.key === key);
  if (!option) throw new Error(`Admin-web contract missing option ${key}`);
  return (option.tone || "mut") as Tone;
}

function sopDoneThrough(row: ActionCenterObligation): number {
  let n = 1;
  if (
    row.sop_task_state === "submitted" ||
    row.sop_task_state === "accepted" ||
    row.proof_state === "uploaded" ||
    row.proof_state === "accepted"
  ) {
    n = 2;
  }
  if (row.proof_state === "accepted" && row.verification_state === "accepted") n = 3;
  if (row.completion_state === "completed") n = 4;
  return n;
}

function hasReviewHandle(taskId?: string, rowVersion?: number): boolean {
  return Boolean(taskId) && Number(rowVersion ?? 0) > 0;
}

export function ActionCenterLocalDrawer({
  rows,
  pageContract,
  initialSelectedRowId,
  drawerHrefs,
  closeHref,
  workflowHrefs,
  passportHrefs,
}: {
  rows: ActionCenterObligation[];
  pageContract: AdminUiPageContract;
  initialSelectedRowId?: string;
  drawerHrefs: Record<string, string>;
  closeHref: string;
  workflowHrefs: Record<string, string>;
  passportHrefs: Record<string, string>;
}) {
  const initialRow = rows.find((row) => row.row_id === initialSelectedRowId);
  const [displayedRow, setDisplayedRow] = useState<ActionCenterObligation | undefined>(initialRow);
  const [drawerOpen, setDrawerOpen] = useState(Boolean(initialRow));
  const openRef = useRef(Boolean(initialRow));
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const openFrameRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);

  const showDrawer = useCallback((row: ActionCenterObligation): void => {
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    openRef.current = true;
    setDisplayedRow(row);
    openFrameRef.current = window.requestAnimationFrame(() => {
      setDrawerOpen(true);
      openFrameRef.current = null;
    });
  }, []);

  const hideDrawer = useCallback((): void => {
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    openRef.current = false;
    setDrawerOpen(false);
    closeTimerRef.current = window.setTimeout(() => {
      setDisplayedRow(undefined);
      closeTimerRef.current = null;
    }, 280);
  }, []);

  useEffect(() => () => {
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
  }, []);

  useEffect(() => {
    function syncSelectionFromUrl(): void {
      const url = new URL(window.location.href);
      const hashParams = new URLSearchParams(url.hash.replace(/^#/, ""));
      const rowId = hashParams.get("ac_row") ?? url.searchParams.get("ac_row") ?? undefined;
      const row = rows.find((item) => item.row_id === rowId);
      if (row) showDrawer(row);
      else hideDrawer();
    }
    window.addEventListener("popstate", syncSelectionFromUrl);
    window.addEventListener("hashchange", syncSelectionFromUrl);
    window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncSelectionFromUrl);
    const initialFrame = window.requestAnimationFrame(syncSelectionFromUrl);
    return () => {
      window.cancelAnimationFrame(initialFrame);
      window.removeEventListener("popstate", syncSelectionFromUrl);
      window.removeEventListener("hashchange", syncSelectionFromUrl);
      window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncSelectionFromUrl);
    };
  }, [hideDrawer, rows, showDrawer]);

  useEffect(() => {
    if (!drawerOpen) previousFocusRef.current?.focus();
  }, [drawerOpen]);

  const closeDrawer = useCallback((): void => {
    // Idempotent: MUI reports Escape and the backdrop through onClose; history steps back once.
    if (!openRef.current) return;
    hideDrawer();
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(closeHref);
  }, [closeHref, hideDrawer]);

  // Template temporary drawer (DetailDrawer: portal, backdrop, focus trap + return, 480 paper).
  return displayedRow ? (
    <ActionCenterRowDrawer
      row={displayedRow}
      open={drawerOpen}
      closeDrawer={closeDrawer}
      returnTo={drawerHrefs[displayedRow.row_id]}
      workflowHref={workflowHrefs[displayedRow.row_id]}
      passportHref={passportHrefs[displayedRow.row_id]}
      pageContract={pageContract}
    />
  ) : null;
}

function ActionForm({
  action,
  completionId,
  taskId,
  rowVersion,
  returnTo,
  reason,
  children,
}: {
  action: (formData: FormData) => void | Promise<void>;
  completionId: string;
  taskId?: string;
  rowVersion?: number;
  returnTo: string;
  reason?: string;
  children: React.ReactNode;
}) {
  const canReview = hasReviewHandle(taskId, rowVersion);
  return (
    <form action={action}>
      <input type="hidden" name="completion_id" value={completionId} />
      {taskId ? <input type="hidden" name="task_id" value={taskId} /> : null}
      {rowVersion ? <input type="hidden" name="row_version" value={rowVersion} /> : null}
      <input type="hidden" name="return_to" value={returnTo} />
      {reason ? <input type="hidden" name="reason" value={reason} /> : null}
      <Button type="submit" variant="outlined" color="inherit" disabled={!canReview} aria-disabled={!canReview || undefined}>
        {children}
      </Button>
    </form>
  );
}

function ActionCenterRowDrawer({
  row,
  open,
  closeDrawer,
  returnTo,
  workflowHref,
  passportHref,
  pageContract,
}: {
  row: ActionCenterObligation;
  open: boolean;
  closeDrawer: () => void;
  returnTo: string;
  workflowHref: string;
  passportHref?: string;
  pageContract: AdminUiPageContract;
}) {
  const blocker = row.blocker_reason;
  const operatorMissing = row.owner_state === "missing" || !row.owner?.operator_name;
  const ownerName = row.owner?.operator_name ?? row.owner?.park_head_name ?? copy(pageContract, "label.unassigned");
  const title = actionWorkTitle(pageContract, row);
  const priority = PRIORITY_BY_SEVERITY[row.severity];
  const priorityOptions = optionGroup(pageContract, "priority_chips");
  const workStateOptions = optionGroup(pageContract, "work_state_filter_chips");
  const severityOptions = optionGroup(pageContract, "severity_chips");
  const sopStateOptions = optionGroup(pageContract, "sop_state_chips");
  const proofStateOptions = optionGroup(pageContract, "proof_state_chips");
  const verificationStateOptions = optionGroup(pageContract, "verification_state_chips");
  const sopProgress = sopDoneThrough(row);
  const hasCompletion = Boolean(row.completion_id);
  const completionId = row.completion_id ?? "";
  const taskId = row.sop_task_id;
  const taskRowVersion = row.sop_task_row_version;

  const linkChip = (href: string, icon: React.ReactNode, label: string, color: "info" | "warning" | "secondary") => (
    <Chip component={Link} href={href} clickable variant="soft" color={color} icon={<>{icon}</>} label={label} sx={phoneTapSx} />
  );

  return (
    <DetailDrawer
      open={open}
      onClose={closeDrawer}
      title={title}
      eyebrow={copy(pageContract, "drawer.work_item.eyebrow")}
      icon={<Syringe aria-hidden="true" />}
      ariaLabel={copy(pageContract, "drawer.work_item.aria")}
      closeLabel={copy(pageContract, "drawer.work_item.close_label")}
      paperTestId="action-center-drawer"
      footer={
        <>
          {hasCompletion && hasReviewHandle(taskId, taskRowVersion) ? <ActionForm action={verifyCompletionAction} completionId={completionId} taskId={taskId} rowVersion={taskRowVersion} returnTo={returnTo}>{copy(pageContract, "action.verify")}</ActionForm> : null}
          {hasCompletion && hasReviewHandle(taskId, taskRowVersion) ? <ActionForm action={rejectCompletionAction} completionId={completionId} taskId={taskId} rowVersion={taskRowVersion} returnTo={returnTo} reason="rework_requested">{copy(pageContract, "action.request_rework")}</ActionForm> : null}
          <LinkButton href={workflowHref} variant="outlined" color="inherit">{copy(pageContract, "action.workflow_record")}</LinkButton>
          {passportHref ? <LinkButton href={passportHref} variant="outlined" color="inherit">{copy(pageContract, "action.goat_passport")}</LinkButton> : null}
          <Button variant="outlined" color="inherit" onClick={closeDrawer}>{copy(pageContract, "action.close")}</Button>
        </>
      }
    >
      <DrawerBlock title={copy(pageContract, "drawer.adherence_status_label")}>
        <Stack direction="row" spacing={1} useFlexGap sx={{ alignItems: "center", flexWrap: "wrap" }}>
          <Tag tone={optionTone(workStateOptions, row.work_state)}>{optionLabel(workStateOptions, row.work_state)}</Tag>
          <Tag tone={optionTone(severityOptions, row.severity)}>{optionLabel(severityOptions, row.severity)}</Tag>
          <InfoHint text={copy(pageContract, "drawer.adherence_status_help")} />
        </Stack>
      </DrawerBlock>

      <DrawerMetaGrid>
        <DrawerMetaItem label={copy(pageContract, "drawer.owner_chain_label")}>{operatorMissing ? <Tag tone="dng">{copy(pageContract, "label.owner_chain_assign")}</Tag> : ownerName}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "drawer.due_label")}>{fmtDate(row.due_at)}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "drawer.priority_label")}><Tag tone={optionTone(priorityOptions, priority)}>{optionLabel(priorityOptions, priority)}</Tag></DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.next_action")}>{row.next_action}</DrawerMetaItem>
      </DrawerMetaGrid>

      <Divider sx={{ borderStyle: "dashed" }} />

      <DrawerMetaGrid>
        <DrawerMetaItem label={copy(pageContract, "drawer.protocol_label")}>{row.protocol_name}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "drawer.dose_label")}>{row.dose_code}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "drawer.park_shed_label")}>{row.park_name} · {row.operational_location_display || operationalLocationLabel({ shedName: row.shed_name, partitionLabel: row.partition_label })}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "drawer.cohort_progress_label")}>{stageLabel(row.animal_stage)} · {row.completed_count}/{row.expected_count} {copy(pageContract, "label.done_suffix")}</DrawerMetaItem>
        <DrawerMetaItem label={copy(pageContract, "label.evidence")} span><EvidenceMedia evidence={row.evidence} pageContract={pageContract} /></DrawerMetaItem>
      </DrawerMetaGrid>

      {blocker ? <Alert severity="error"><span>{blocker}</span></Alert> : null}

      <DrawerBlock title={copy(pageContract, "drawer.sop_checklist.title")}>
        <SopChecklist steps={VACCINATION_DRIVE_SOP_STEPS} doneThrough={sopProgress} />
      </DrawerBlock>

      <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap" }}>
        <Tag tone={optionTone(sopStateOptions, row.sop_task_state)}>{optionLabel(sopStateOptions, row.sop_task_state)}</Tag>
        <Tag tone={optionTone(proofStateOptions, row.proof_state)}>{optionLabel(proofStateOptions, row.proof_state)}</Tag>
        <Tag tone={optionTone(verificationStateOptions, row.verification_state)}>{optionLabel(verificationStateOptions, row.verification_state)}</Tag>
      </Stack>

      <DrawerBlock title={copy(pageContract, "drawer.linked_title")}>
        <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: "wrap" }}>
          {linkChip(workflowHref, <GitBranch size={16} aria-hidden="true" />, copy(pageContract, "drawer.link.workflow_record"), "info")}
          {linkChip("/protocol-adherence", <ShieldCheck size={16} aria-hidden="true" />, copy(pageContract, "drawer.link.adherence"), "warning")}
          {linkChip("/vaccination", <Syringe size={16} aria-hidden="true" />, copy(pageContract, "drawer.link.vaccination"), "secondary")}
        </Stack>
      </DrawerBlock>
    </DetailDrawer>
  );
}
