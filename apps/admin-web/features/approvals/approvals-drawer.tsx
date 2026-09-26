"use client";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Gavel } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";

import type { AdminWebApprovalItem } from "@/lib/api/server";
import { fmtDateTime } from "@/lib/format";
import type { RouteSearchParams } from "@/lib/search-params";
import { APPROVALS_COPY as COPY } from "./copy";
import { StatusChip } from "@/components/review-queue/review-queue-ui";
import { approveApprovalAction, rejectApprovalAction, resolveApprovalCaptureMediaUrl } from "./actions";
import { ApprovalsActionTelemetry } from "./approvals-telemetry";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import Divider from "@mui/material/Divider";
import Stack from "@mui/material/Stack";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import { LinkButton } from "@/components/minimal/link-button";
import { DetailDrawer, DrawerMetaGrid, DrawerMetaItem, DrawerNote } from "@/components/app/detail-drawer";
import {
  APPROVAL_REASON_MAX_BYTES,
  approvalDetailRows,
  approvalErrorSentence,
  approvalStatusLabel,
} from "./approval-display";

const PATHNAME = "/approvals";

function titleCase(v: string): string {
  return v ? v.charAt(0).toUpperCase() + v.slice(1).replace(/_/g, " ") : v;
}

export function ApprovalsDrawer({
  items,
  initialSelectedId,
  searchParams,
  feedback,
  locationNames,
}: {
  items: AdminWebApprovalItem[];
  initialSelectedId?: string;
  searchParams: RouteSearchParams;
  feedback: { status?: string; code?: string };
  locationNames: Record<string, string>;
}) {
  const initialItem = items.find((item) => item.approval_request_id === initialSelectedId);
  const [activeId, setActiveId] = useState(initialItem?.approval_request_id);
  const [displayedId, setDisplayedId] = useState(initialItem?.approval_request_id);
  const activeRef = useRef(Boolean(initialItem));
  const triggerRef = useRef<HTMLElement | null>(null);
  const openFrameRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);
  const item = items.find((candidate) => candidate.approval_request_id === displayedId);
  const drawerOpen = Boolean(activeId && item);
  const closeHref = hrefWithout(searchParams, ["ap_row"]);

  const syncFromUrl = useCallback((): void => {
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    const id = new URL(window.location.href).searchParams.get("ap_row") ?? undefined;
    const selected = items.find((candidate) => candidate.approval_request_id === id);
    if (selected) {
      triggerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      setActiveId(undefined);
      activeRef.current = true;
      setDisplayedId(selected.approval_request_id);
      openFrameRef.current = window.requestAnimationFrame(() => {
        setActiveId(selected.approval_request_id);
        openFrameRef.current = null;
      });
      return;
    }
    activeRef.current = false;
    setActiveId(undefined);
    closeTimerRef.current = window.setTimeout(() => {
      setDisplayedId(undefined);
      closeTimerRef.current = null;
      triggerRef.current?.focus();
    }, 280);
  }, [items]);

  useEffect(() => {
    window.addEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncFromUrl);
    window.addEventListener("popstate", syncFromUrl);
    return () => {
      window.removeEventListener(LOCAL_OVERLAY_URL_CHANGE_EVENT, syncFromUrl);
      window.removeEventListener("popstate", syncFromUrl);
      if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
      if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    };
  }, [syncFromUrl]);

  const closeDrawer = useCallback((): void => {
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    // Idempotent: MUI reports Escape/backdrop through onClose, and a second close in the same tick
    // must not step history back twice.
    if (!activeRef.current) return;
    activeRef.current = false;
    setActiveId(undefined);
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(closeHref);
  }, [closeHref]);

  if (!item) return null;
  const returnTo = hrefWithRow(searchParams, item.approval_request_id);

  // Template temporary drawer (DetailDrawer on MinimalDrawer: portal, backdrop, focus trapped and
  // returned, 480 paper). Escape and the backdrop land on the same idempotent closeDrawer.
  return (
    <ApprovalsDrawerPanel
      item={item}
      returnTo={returnTo}
      feedback={feedback}
      open={drawerOpen}
      onClose={closeDrawer}
      locationNames={locationNames}
    />
  );
}

function ApprovalsDrawerPanel({
  item,
  returnTo,
  feedback,
  open,
  onClose,
  locationNames,
}: {
  item: AdminWebApprovalItem;
  returnTo: string;
  feedback: { status?: string; code?: string };
  open: boolean;
  onClose: () => void;
  locationNames: Record<string, string>;
}) {
  const decided = item.status !== "pending";
  const detail = approvalDetailRows(item.summary, locationNames, item.subject_animal_location);

  return (
    <DetailDrawer
      open={open}
      onClose={onClose}
      title={`${titleCase(item.request_type)} request`}
      eyebrow={COPY.drawer.eyebrow}
      icon={<Gavel aria-hidden="true" />}
      ariaLabel={COPY.drawer.aria}
      closeLabel={COPY.drawer.closeLabel}
      footer={
        <>
          <LinkButton href="/counts/herd" scroll={false} variant="outlined" color="inherit">
            {COPY.action.openAuditLog}
          </LinkButton>
          <Button variant="outlined" color="inherit" onClick={onClose}>
            {COPY.action.close}
          </Button>
        </>
      }
    >
      <ApprovalsActionTelemetry status={feedback.status} code={feedback.code} />
      {/* Only a REFUSED decision is shown here: the row is still pending, so it is still in the
          list. A successful decision moves the row off this list and its confirmation is rendered
          at page level. The error code is a machine key and is mapped to a sentence, never shown. */}
      {feedback.status === "error" ? (
        <Alert severity="warning" role="alert">
          <b>{COPY.feedback.failed}</b>
          <div>{approvalErrorSentence(feedback.code)}</div>
        </Alert>
      ) : null}

      <DrawerNote>{COPY.drawer.note}</DrawerNote>

      {/* Readable facts only — request type, status, when it was raised/decided. Internal UUIDs
          (raiser/decider/goat/event ids) are intentionally not shown; an operator reads names and
          dates, not ids. */}
      <DrawerMetaGrid>
        <DrawerMetaItem label={COPY.drawer.metaType}>{titleCase(item.request_type)}</DrawerMetaItem>
        <DrawerMetaItem label={COPY.drawer.metaStatus}>
          <StatusChip status={item.status}>{approvalStatusLabel(item.status)}</StatusChip>
        </DrawerMetaItem>
        <DrawerMetaItem label={COPY.drawer.metaRaisedAt}>{fmtDateTime(item.raised_at)}</DrawerMetaItem>
        {item.raised_by_name ? <DrawerMetaItem label={COPY.drawer.metaRaisedBy}>{item.raised_by_name}</DrawerMetaItem> : null}
        {/* Backend-composed line: head count, farm and pens for a move; the animal's tag for a
            death. Absent when nothing resolved, never an id. */}
        {item.summary_line ? <DrawerMetaItem label={COPY.drawer.metaSummary}>{item.summary_line}</DrawerMetaItem> : null}
        {item.decided_at ? <DrawerMetaItem label={COPY.drawer.metaDecidedAt}>{fmtDateTime(item.decided_at)}</DrawerMetaItem> : null}
        {item.decision_reason ? <DrawerMetaItem label={COPY.drawer.metaDecisionReason}>{item.decision_reason}</DrawerMetaItem> : null}
      </DrawerMetaGrid>

      <DrawerSectionCard title={COPY.drawer.summaryTitle}>
        {detail.length === 0 ? (
          <Typography variant="body2" sx={{ color: "text.secondary" }}>{COPY.drawer.summaryEmpty}</Typography>
        ) : (
          <DrawerMetaGrid>
            {detail.map((entry) => (
              <DrawerMetaItem key={entry.label} label={entry.label}>
                {entry.value}
              </DrawerMetaItem>
            ))}
          </DrawerMetaGrid>
        )}
      </DrawerSectionCard>

      <CaptureSection item={item} />

      {/* One decision block: the prominent Approve action on top, then the reject reason and a red
          (destructive) Reject action, separated by an "or" rule. Both are full-width so the two
          choices read as equal-weight, mutually-exclusive decisions rather than two stray buttons.
          Kept as two separate <form>s because approve and reject post to different server actions
          and only reject carries a reason. */}
      <DrawerSectionCard title={COPY.decision.title}>
        <Stack spacing={1.5}>
          {decided ? <DrawerNote>{COPY.approve.disabledDecided}</DrawerNote> : null}

          <form action={approveApprovalAction}>
            <input type="hidden" name="request_id" value={item.approval_request_id} />
            <input type="hidden" name="return_to" value={returnTo} />
            <Button
              type="submit"
              variant="contained"
              color="primary"
              fullWidth
              disabled={decided}
              aria-disabled={decided}
              title={decided ? COPY.approve.disabledDecided : undefined}
            >
              {COPY.approve.submit}
            </Button>
          </form>

          <Divider sx={{ typography: "overline", color: "text.disabled" }}>{COPY.decision.or}</Divider>

          <Stack component="form" action={rejectApprovalAction} spacing={1.5}>
            <input type="hidden" name="request_id" value={item.approval_request_id} />
            <input type="hidden" name="return_to" value={returnTo} />
            <TextField
              name="reason"
              label={COPY.reject.reasonLabel}
              placeholder={COPY.reject.reasonPlaceholder}
              multiline
              minRows={2}
              fullWidth
              disabled={decided}
              required
              slotProps={{ htmlInput: { maxLength: APPROVAL_REASON_MAX_BYTES } }}
            />
            <Button
              type="submit"
              variant="contained"
              color="error"
              fullWidth
              disabled={decided}
              aria-disabled={decided}
              title={decided ? COPY.reject.disabledDecided : undefined}
            >
              {COPY.reject.submit}
            </Button>
          </Stack>
        </Stack>
      </DrawerSectionCard>
    </DetailDrawer>
  );
}

/** Outlined template card with a subtitle header, used for each drawer section. */
function DrawerSectionCard({ title, action, children }: { title: ReactNode; action?: ReactNode; children: ReactNode }) {
  return (
    <Card variant="outlined" sx={{ p: 2.5, display: "flex", flexDirection: "column", gap: 1.5, minWidth: 0 }}>
      <Stack direction="row" spacing={1} sx={{ alignItems: "baseline", flexWrap: "wrap" }}>
        <Typography variant="subtitle2" sx={{ flex: 1, minWidth: 0 }}>{title}</Typography>
        {action}
      </Stack>
      {children}
    </Card>
  );
}

function hrefWithout(params: RouteSearchParams, exclude: string[]): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (exclude.includes(key)) continue;
    if (Array.isArray(value)) {
      for (const entry of value) next.append(key, entry);
    } else if (value) {
      next.set(key, value);
    }
  }
  const qs = next.toString();
  return qs ? `${PATHNAME}?${qs}` : PATHNAME;
}

function hrefWithRow(params: RouteSearchParams, requestId: string): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (key === "ap_row" || key === "ap_status" || key === "ap_code") continue;
    if (Array.isArray(value)) {
      for (const entry of value) if (entry) next.append(key, entry);
    } else if (value) {
      next.set(key, value);
    }
  }
  next.set("ap_row", requestId);
  return `${PATHNAME}?${next.toString()}`;
}

// CaptureSection renders the request's SOP capture snapshot verbatim (ONE renderer for the birth /
// death capture card and the shifting raise card, shared CountsApprovalCapture): answers grouped by their
// section, every proof under its authored title with click-to-open, the older-app note and the
// verifier's verdict on the report proof. Nothing is composed here.
function CaptureSection({ item }: { item: AdminWebApprovalItem }) {
  const capture = item.capture;
  if (!capture) return null;
  const groups = new Map<string, { label: string; value: string }[]>();
  for (const row of capture.rows ?? []) {
    const key = row.group ?? "";
    groups.set(key, [...(groups.get(key) ?? []), { label: row.label, value: row.value }]);
  }
  return (
    <DrawerSectionCard
      title={COPY.capture.title}
      action={
        capture.version_label ? (
          <Typography variant="caption" sx={{ color: "text.secondary" }}>{COPY.capture.version}: {capture.version_label}</Typography>
        ) : undefined
      }
    >
      {item.capture_review_status ? (
        <DrawerNote>
          <span role="status">
            {COPY.capture.review[item.capture_review_status] ?? item.capture_review_status}
            {item.capture_review_reason ? ` — ${item.capture_review_reason}` : ""}
          </span>
        </DrawerNote>
      ) : null}
      {[...groups.entries()].map(([group, rows]) => (
        <Stack key={group || "rows"} spacing={1}>
          {group ? <Typography variant="caption" sx={{ color: "text.secondary", fontWeight: "fontWeightBold" }}>{group}</Typography> : null}
          <DrawerMetaGrid>
            {rows.map((row, i) => (
              <DrawerMetaItem key={`${row.label}-${i}`} label={row.label}>
                {row.value}
              </DrawerMetaItem>
            ))}
          </DrawerMetaGrid>
        </Stack>
      ))}
      {capture.missing_note ? (
        <DrawerMetaGrid>
          <DrawerMetaItem label={COPY.capture.missing}>{capture.missing_note}</DrawerMetaItem>
        </DrawerMetaGrid>
      ) : null}
      {capture.media?.length ? (
        <Stack spacing={1}>
          <Typography variant="caption" sx={{ color: "text.secondary", fontWeight: "fontWeightBold" }}>{COPY.capture.media}</Typography>
          <Stack divider={<Divider flexItem sx={{ borderStyle: "dashed" }} />} spacing={1}>
            {capture.media.map((m) => (
              <CaptureMediaRow key={m.proof_id} proofId={m.proof_id} label={m.label} kind={m.kind} />
            ))}
          </Stack>
        </Stack>
      ) : null}
    </DrawerSectionCard>
  );
}

function CaptureMediaRow({ proofId, label, kind }: { proofId: string; label: string; kind: string }) {
  const [state, setState] = useState<"idle" | "opening" | "failed">("idle");
  const open = async () => {
    setState("opening");
    // A tab is opened synchronously in the click so the browser does not treat it as a popup.
    const tab = window.open("about:blank", "_blank");
    const url = await resolveApprovalCaptureMediaUrl(proofId).catch(() => null);
    if (!url) {
      tab?.close();
      setState("failed");
      return;
    }
    if (tab) tab.location.href = url;
    else window.location.assign(url);
    setState("idle");
  };
  return (
    <Stack direction="row" spacing={1.5} sx={{ alignItems: "center" }}>
      <Box sx={{ flex: 1, minWidth: 0 }}>
        <Typography variant="subtitle2" sx={{ overflowWrap: "anywhere" }}>{label}</Typography>
        <Typography variant="caption" sx={{ color: "text.secondary" }}>
          {COPY.capture.kind[kind] ?? ""}
          {state === "failed" ? ` · ${COPY.capture.unavailable}` : ""}
        </Typography>
      </Box>
      <Button size="small" variant="outlined" color="inherit" onClick={open} disabled={state === "opening"}>
        {state === "opening" ? COPY.capture.opening : COPY.capture.open}
      </Button>
    </Stack>
  );
}
