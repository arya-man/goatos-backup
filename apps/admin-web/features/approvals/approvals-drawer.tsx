"use client";

import Link from "@/components/no-prefetch-link";
import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Gavel, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";

import { Tag, type Tone } from "@/components/ui-primitives";
import type { AdminWebApprovalItem } from "@/lib/api/server";
import { fmtDateTime } from "@/lib/format";
import type { RouteSearchParams } from "@/lib/search-params";
import { APPROVALS_COPY as COPY } from "./copy";
import { approveApprovalAction, rejectApprovalAction, resolveApprovalCaptureMediaUrl } from "./actions";
import { ApprovalsActionTelemetry } from "./approvals-telemetry";
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

function statusTone(status: AdminWebApprovalItem["status"]): Tone {
  if (status === "rejected") return "dng";
  if (status === "approved") return "ok";
  if (status === "cancelled") return "info";
  return "warn";
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
  const closeButtonRef = useRef<HTMLButtonElement>(null);
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
      setDisplayedId(selected.approval_request_id);
      openFrameRef.current = window.requestAnimationFrame(() => {
        setActiveId(selected.approval_request_id);
        openFrameRef.current = null;
      });
      return;
    }
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

  useEffect(() => {
    if (!drawerOpen) return;
    const frame = window.requestAnimationFrame(() => closeButtonRef.current?.focus());
    return () => window.cancelAnimationFrame(frame);
  }, [drawerOpen]);

  const closeDrawer = useCallback((): void => {
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    setActiveId(undefined);
    if (currentHistoryEntryIsLocalOverlay()) {
      window.history.back();
      return;
    }
    replaceLocalOverlayUrl(closeHref);
  }, [closeHref]);

  useEffect(() => {
    if (!drawerOpen) return;
    const onKeyDown = (event: KeyboardEvent): void => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      closeDrawer();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [closeDrawer, drawerOpen]);

  if (!item) return null;
  const returnTo = hrefWithRow(searchParams, item.approval_request_id);

  return (
    <>
      <button
        type="button"
        className={`scrim${drawerOpen ? " on" : ""}`}
        aria-label={COPY.drawer.closeLabel}
        aria-hidden={!drawerOpen}
        tabIndex={drawerOpen ? 0 : -1}
        onClick={closeDrawer}
      />
      <ApprovalsDrawerPanel
        item={item}
        returnTo={returnTo}
        feedback={feedback}
        open={drawerOpen}
        onClose={closeDrawer}
        closeButtonRef={closeButtonRef}
        locationNames={locationNames}
      />
    </>
  );
}

function ApprovalsDrawerPanel({
  item,
  returnTo,
  feedback,
  open,
  onClose,
  closeButtonRef,
  locationNames,
}: {
  item: AdminWebApprovalItem;
  returnTo: string;
  feedback: { status?: string; code?: string };
  open: boolean;
  onClose: () => void;
  closeButtonRef: React.RefObject<HTMLButtonElement | null>;
  locationNames: Record<string, string>;
}) {
  const decided = item.status !== "pending";
  const detail = approvalDetailRows(item.summary, locationNames, item.subject_animal_location);

  return (
    <aside className={`drawer approvals-drawer${open ? " on" : ""}`} aria-label={COPY.drawer.aria} aria-hidden={!open} inert={!open}>
      <div className="dh">
        <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
          <Gavel className="ic" aria-hidden="true" />
        </span>
        <div>
          <div className="mt">{COPY.drawer.eyebrow}</div>
          <h2>
            {titleCase(item.request_type)} request
          </h2>
        </div>
        <span className="sp" style={{ flex: 1 }} />
        <button ref={closeButtonRef} type="button" className="iconbtn" aria-label={COPY.drawer.closeLabel} onClick={onClose}>
          <X className="ic" />
        </button>
      </div>

      <div className="dc">
        <ApprovalsActionTelemetry status={feedback.status} code={feedback.code} />
        {/* Only a REFUSED decision is shown here: the row is still pending, so it is still in the
            list. A successful decision moves the row off this list and its confirmation is rendered
            at page level. The error code is a machine key and is mapped to a sentence, never shown. */}
        {feedback.status === "error" ? (
          <div className="alert warn" role="alert" style={{ marginBottom: 12 }}>
            <b>{COPY.feedback.failed}</b>
            <div className="small" style={{ marginTop: 4 }}>
              {approvalErrorSentence(feedback.code)}
            </div>
          </div>
        ) : null}

        <div className="note" style={{ marginBottom: 12 }}>
          {COPY.drawer.note}
        </div>

        {/* Readable facts only — request type, status, when it was raised/decided. Internal UUIDs
            (raiser/decider/goat/event ids) are intentionally not shown; an operator reads names and
            dates, not ids. */}
        <div className="metagrid">
          <Meta label={COPY.drawer.metaType}>{titleCase(item.request_type)}</Meta>
          <Meta label={COPY.drawer.metaStatus}>
            <Tag tone={statusTone(item.status)}>{approvalStatusLabel(item.status)}</Tag>
          </Meta>
          <Meta label={COPY.drawer.metaRaisedAt}>{fmtDateTime(item.raised_at)}</Meta>
          {item.raised_by_name ? <Meta label={COPY.drawer.metaRaisedBy}>{item.raised_by_name}</Meta> : null}
          {/* Backend-composed line: head count, farm and pens for a move; the animal's tag for a
              death. Absent when nothing resolved, never an id. */}
          {item.summary_line ? <Meta label={COPY.drawer.metaSummary}>{item.summary_line}</Meta> : null}
          {item.decided_at ? <Meta label={COPY.drawer.metaDecidedAt}>{fmtDateTime(item.decided_at)}</Meta> : null}
          {item.decision_reason ? <Meta label={COPY.drawer.metaDecisionReason}>{item.decision_reason}</Meta> : null}
        </div>

        <section className="card" style={{ marginTop: 14 }}>
          <div className="hd">
            <h3>{COPY.drawer.summaryTitle}</h3>
          </div>
          <div className="bd">
            {detail.length === 0 ? (
              <div className="muted small">{COPY.drawer.summaryEmpty}</div>
            ) : (
              <div className="metagrid">
                {detail.map((entry) => (
                  <Meta key={entry.label} label={entry.label}>
                    {entry.value}
                  </Meta>
                ))}
              </div>
            )}
          </div>
        </section>

        <CaptureSection item={item} />

        {/* One decision block: the prominent green Approve action on top, then the reject reason and
            a red (destructive) Reject action, separated by an "or" rule. Both are full-width so the
            two choices read as equal-weight, mutually-exclusive decisions rather than two stray
            buttons. Kept as two separate <form>s because approve and reject post to different server
            actions and only reject carries a reason. */}
        <section className="card" style={{ marginTop: 14 }}>
          <div className="hd">
            <h3>{COPY.decision.title}</h3>
          </div>
          <div className="bd" style={{ display: "grid", gap: 12 }}>
            {decided ? <div className="note">{COPY.approve.disabledDecided}</div> : null}

            <form action={approveApprovalAction}>
              <input type="hidden" name="request_id" value={item.approval_request_id} />
              <input type="hidden" name="return_to" value={returnTo} />
              <button
                type="submit"
                className="btn p"
                style={{ width: "100%", justifyContent: "center" }}
                disabled={decided}
                aria-disabled={decided}
                title={decided ? COPY.approve.disabledDecided : undefined}
              >
                {COPY.approve.submit}
              </button>
            </form>

            <div className="decision-or">{COPY.decision.or}</div>

            <form action={rejectApprovalAction} style={{ display: "grid", gap: 8 }}>
              <input type="hidden" name="request_id" value={item.approval_request_id} />
              <input type="hidden" name="return_to" value={returnTo} />
              <label className="fld" style={{ marginBottom: 0 }}>
                <span>{COPY.reject.reasonLabel}</span>
                <textarea
                  name="reason"
                  rows={2}
                  placeholder={COPY.reject.reasonPlaceholder}
                  disabled={decided}
                  required
                  maxLength={APPROVAL_REASON_MAX_BYTES}
                />
              </label>
              <button
                type="submit"
                className="btn dng"
                style={{ width: "100%", justifyContent: "center" }}
                disabled={decided}
                aria-disabled={decided}
                title={decided ? COPY.reject.disabledDecided : undefined}
              >
                {COPY.reject.submit}
              </button>
            </form>
          </div>
        </section>
      </div>

      <div className="df">
        <Link href="/counts/herd" className="btn" scroll={false}>
          {COPY.action.openAuditLog}
        </Link>
        <button type="button" className="btn" onClick={onClose}>
          {COPY.action.close}
        </button>
      </div>
    </aside>
  );
}

function Meta({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <span className="muted small">{label}</span>
      <b style={{ display: "block", marginTop: 3, overflowWrap: "anywhere" }}>{children}</b>
    </div>
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
    <section className="card" style={{ marginTop: 14 }}>
      <div className="hd">
        <h3>{COPY.capture.title}</h3>
        {capture.version_label ? <span className="muted small">{COPY.capture.version}: {capture.version_label}</span> : null}
      </div>
      <div className="bd">
        {item.capture_review_status ? (
          <div className="note" role="status">
            {COPY.capture.review[item.capture_review_status] ?? item.capture_review_status}
            {item.capture_review_reason ? ` — ${item.capture_review_reason}` : ""}
          </div>
        ) : null}
        {[...groups.entries()].map(([group, rows]) => (
          <div key={group || "rows"} style={{ marginTop: 8 }}>
            {group ? <div className="muted small b700">{group}</div> : null}
            <div className="metagrid">
              {rows.map((row, i) => (
                <Meta key={`${row.label}-${i}`} label={row.label}>
                  {row.value}
                </Meta>
              ))}
            </div>
          </div>
        ))}
        {capture.missing_note ? (
          <div className="metagrid" style={{ marginTop: 8 }}>
            <Meta label={COPY.capture.missing}>{capture.missing_note}</Meta>
          </div>
        ) : null}
        {capture.media?.length ? (
          <div style={{ marginTop: 10 }}>
            <div className="muted small b700">{COPY.capture.media}</div>
            <div className="htl">
              {capture.media.map((m) => (
                <CaptureMediaRow key={m.proof_id} proofId={m.proof_id} label={m.label} kind={m.kind} />
              ))}
            </div>
          </div>
        ) : null}
      </div>
    </section>
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
    <div className="hrow">
      <div className="htx">
        <b>{label}</b>
        <div className="hmeta muted small">
          {COPY.capture.kind[kind] ?? ""}
          {state === "failed" ? ` · ${COPY.capture.unavailable}` : ""}
        </div>
      </div>
      <button type="button" className="btn sm" onClick={open} disabled={state === "opening"}>
        {state === "opening" ? COPY.capture.opening : COPY.capture.open}
      </button>
    </div>
  );
}
