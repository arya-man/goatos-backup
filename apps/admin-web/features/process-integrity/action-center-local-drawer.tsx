"use client";

import Link from "@/components/no-prefetch-link";
import {
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  currentHistoryEntryIsLocalOverlay,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import { VACCINATION_DRIVE_SOP_STEPS } from "@/lib/vaccination-sop-steps";
import { copy, optionGroup, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ActionCenterObligation, ProcessIntegritySeverity } from "@/lib/api/server";
import { fmtDate } from "@/lib/format";
import { Ban, GitBranch, ShieldCheck, Syringe, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { rejectCompletionAction, verifyCompletionAction } from "./actions";
import { type Tone } from "./process-integrity";
import { SopChecklist } from "./sop-checklist";
import { actionWorkTitle } from "./action-center-presenters";
import { EvidenceMedia } from "./evidence-media";

const OPEN_FALLBACK_MS = 50;

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
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const openFrameRef = useRef<number | null>(null);
  const openFallbackRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);

  // The open class must never depend on one animation frame landing: a busy or
  // backgrounded tab can skip it, leaving a mounted drawer parked off-screen
  // behind its scrim (shade visible, no panel). A timer races the frame.
  const cancelPendingOpen = useCallback((): void => {
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    if (openFallbackRef.current !== null) window.clearTimeout(openFallbackRef.current);
    openFrameRef.current = null;
    openFallbackRef.current = null;
  }, []);

  const showDrawer = useCallback(
    (row: ActionCenterObligation): void => {
      if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
      previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      setDisplayedRow(row);
      cancelPendingOpen();
      const open = (): void => {
        cancelPendingOpen();
        setDrawerOpen(true);
      };
      openFrameRef.current = window.requestAnimationFrame(open);
      openFallbackRef.current = window.setTimeout(open, OPEN_FALLBACK_MS);
    },
    [cancelPendingOpen],
  );

  const hideDrawer = useCallback((): void => {
    cancelPendingOpen();
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    setDrawerOpen(false);
    closeTimerRef.current = window.setTimeout(() => {
      setDisplayedRow(undefined);
      closeTimerRef.current = null;
    }, 280);
  }, [cancelPendingOpen]);

  useEffect(
    () => () => {
      cancelPendingOpen();
      if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    },
    [cancelPendingOpen],
  );

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
    // Sync immediately; a deep-linked drawer must not wait for a frame a hidden
    // or busy tab may never deliver.
    syncSelectionFromUrl();
    return () => {
      window.removeEventListener("popstate", syncSelectionFromUrl);
      window.removeEventListener("hashchange", syncSelectionFromUrl);
      window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncSelectionFromUrl);
    };
  }, [hideDrawer, rows, showDrawer]);

  useEffect(() => {
    if (drawerOpen) {
      const frame = window.requestAnimationFrame(() => closeButtonRef.current?.focus());
      return () => window.cancelAnimationFrame(frame);
    }
    previousFocusRef.current?.focus();
  }, [drawerOpen]);

  const closeDrawer = useCallback((): void => {
    hideDrawer();
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(closeHref);
  }, [closeHref, hideDrawer]);

  useEffect(() => {
    if (!drawerOpen) return undefined;
    function onKeyDown(event: KeyboardEvent): void {
      if (event.key !== "Escape") return;
      event.preventDefault();
      closeDrawer();
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [closeDrawer, drawerOpen]);

  return (
    <>
      {displayedRow ? (
        <button
          type="button"
          className={`scrim${drawerOpen ? " on" : ""}`}
          data-testid="action-center-drawer-scrim"
          aria-label={copy(pageContract, "drawer.work_item.close_label")}
          aria-hidden={!drawerOpen}
          tabIndex={drawerOpen ? 0 : -1}
          onClick={closeDrawer}
        />
      ) : null}
      {displayedRow ? (
        <ActionCenterRowDrawer
          row={displayedRow}
          open={drawerOpen}
          closeDrawer={closeDrawer}
          closeButtonRef={closeButtonRef}
          returnTo={drawerHrefs[displayedRow.row_id]}
          workflowHref={workflowHrefs[displayedRow.row_id]}
          passportHref={passportHrefs[displayedRow.row_id]}
          pageContract={pageContract}
        />
      ) : null}
    </>
  );
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
    <form action={action} style={{ display: "inline" }}>
      <input type="hidden" name="completion_id" value={completionId} />
      {taskId ? <input type="hidden" name="task_id" value={taskId} /> : null}
      {rowVersion ? <input type="hidden" name="row_version" value={rowVersion} /> : null}
      <input type="hidden" name="return_to" value={returnTo} />
      {reason ? <input type="hidden" name="reason" value={reason} /> : null}
      <button type="submit" className="btn sm" disabled={!canReview} aria-disabled={!canReview || undefined}>
        {children}
      </button>
    </form>
  );
}

function ActionCenterRowDrawer({
  row,
  open,
  closeDrawer,
  closeButtonRef,
  returnTo,
  workflowHref,
  passportHref,
  pageContract,
}: {
  row: ActionCenterObligation;
  open: boolean;
  closeDrawer: () => void;
  closeButtonRef: React.RefObject<HTMLButtonElement | null>;
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

  return (
    <aside className={`drawer${open ? " on" : ""}`} aria-label={copy(pageContract, "drawer.work_item.aria")} aria-hidden={!open} inert={!open}>
      <div className="dh">
        <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
          <Syringe className="ic" aria-hidden="true" />
        </span>
        <div>
          <div className="mt">{copy(pageContract, "drawer.work_item.eyebrow")}</div>
          <h2>{title}</h2>
        </div>
        <span className="sp" style={{ flex: 1 }} />
        <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={copy(pageContract, "drawer.work_item.close_label")} onClick={closeDrawer}>
          <X className="ic" />
        </button>
      </div>
      <div className="dc">
        <div className="fld">
          <label>{copy(pageContract, "drawer.adherence_status_label")}</label>
          <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
            <Tag tone={optionTone(workStateOptions, row.work_state)}>{optionLabel(workStateOptions, row.work_state)}</Tag>
            <Tag tone={optionTone(severityOptions, row.severity)}>{optionLabel(severityOptions, row.severity)}</Tag>
            <span className="muted small">{copy(pageContract, "drawer.adherence_status_help")}</span>
          </div>
        </div>

        <div className="metagrid" style={{ marginBottom: 14 }}>
          <div><div className="k">{copy(pageContract, "drawer.owner_chain_label")}</div><div className="v">{operatorMissing ? <Tag tone="dng">{copy(pageContract, "label.owner_chain_assign")}</Tag> : ownerName}</div></div>
          <div><div className="k">{copy(pageContract, "drawer.due_label")}</div><div className="v">{fmtDate(row.due_at)}</div></div>
          <div><div className="k">{copy(pageContract, "drawer.priority_label")}</div><div className="v"><Tag tone={optionTone(priorityOptions, priority)}>{optionLabel(priorityOptions, priority)}</Tag></div></div>
          <div><div className="k">{copy(pageContract, "label.next_action")}</div><div className="v">{row.next_action}</div></div>
        </div>

        <div className="metagrid">
          <div><div className="k">{copy(pageContract, "drawer.protocol_label")}</div><div className="v">{row.protocol_name}</div></div>
          <div><div className="k">{copy(pageContract, "drawer.dose_label")}</div><div className="v">{row.dose_code}</div></div>
          <div><div className="k">{copy(pageContract, "drawer.park_shed_label")}</div><div className="v">{row.park_name} · {row.shed_name}</div></div>
          <div><div className="k">{copy(pageContract, "drawer.cohort_progress_label")}</div><div className="v">{row.animal_stage} · {row.completed_count}/{row.expected_count} {copy(pageContract, "label.done_suffix")}</div></div>
          <div><div className="k">{copy(pageContract, "label.evidence")}</div><div className="v"><EvidenceMedia evidence={row.evidence} pageContract={pageContract} /></div></div>
        </div>

        {blocker ? <div className="alert" style={{ marginTop: 14, marginBottom: 0 }}><Ban className="ic" aria-hidden="true" /><span>{blocker}</span></div> : null}

        <div style={{ marginTop: 16 }}>
          <div className="b700" style={{ margin: "4px 0 10px" }}>{copy(pageContract, "drawer.sop_checklist.title")}</div>
          <SopChecklist steps={VACCINATION_DRIVE_SOP_STEPS} doneThrough={sopProgress} />
        </div>

        <div className="chipset" style={{ marginTop: 14 }}>
          <Tag tone={optionTone(sopStateOptions, row.sop_task_state)}>{optionLabel(sopStateOptions, row.sop_task_state)}</Tag>
          <Tag tone={optionTone(proofStateOptions, row.proof_state)}>{optionLabel(proofStateOptions, row.proof_state)}</Tag>
          <Tag tone={optionTone(verificationStateOptions, row.verification_state)}>{optionLabel(verificationStateOptions, row.verification_state)}</Tag>
        </div>

        <div style={{ marginTop: 16 }}>
          <div className="b700" style={{ margin: "2px 0 8px" }}>{copy(pageContract, "drawer.linked_title")}</div>
          <div style={{ display: "flex", gap: 7, flexWrap: "wrap" }}>
            <Link href={workflowHref} className="tag t-info" style={{ display: "inline-flex", alignItems: "center", gap: 5 }}><GitBranch className="ic" style={{ width: 12 }} aria-hidden="true" />{copy(pageContract, "drawer.link.workflow_record")}</Link>
            <Link href="/protocol-adherence" className="tag t-warn" style={{ display: "inline-flex", alignItems: "center", gap: 5 }}><ShieldCheck className="ic" style={{ width: 12 }} aria-hidden="true" />{copy(pageContract, "drawer.link.adherence")}</Link>
            <Link href="/vaccination" className="tag t-teal" style={{ display: "inline-flex", alignItems: "center", gap: 5 }}><Syringe className="ic" style={{ width: 12 }} aria-hidden="true" />{copy(pageContract, "drawer.link.vaccination")}</Link>
          </div>
        </div>
      </div>

      <div className="df">
        {hasCompletion && hasReviewHandle(taskId, taskRowVersion) ? <ActionForm action={verifyCompletionAction} completionId={completionId} taskId={taskId} rowVersion={taskRowVersion} returnTo={returnTo}>{copy(pageContract, "action.verify")}</ActionForm> : null}
        {hasCompletion && hasReviewHandle(taskId, taskRowVersion) ? <ActionForm action={rejectCompletionAction} completionId={completionId} taskId={taskId} rowVersion={taskRowVersion} returnTo={returnTo} reason="rework_requested">{copy(pageContract, "action.request_rework")}</ActionForm> : null}
        <Link href={workflowHref} className="btn">{copy(pageContract, "action.workflow_record")}</Link>
        {passportHref ? <Link href={passportHref} className="btn">{copy(pageContract, "action.goat_passport")}</Link> : null}
        <button type="button" className="btn" onClick={closeDrawer}>{copy(pageContract, "action.close")}</button>
      </div>
    </aside>
  );
}
