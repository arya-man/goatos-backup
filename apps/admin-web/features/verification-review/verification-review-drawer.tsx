"use client";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { Maximize, Minimize, PlayCircle } from "lucide-react";
import { useCallback, useEffect, useRef, useState, useMemo } from "react";

import { controlEnabled, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { PositionListResponse, VerificationQueueItem } from "@/lib/api/server";
import { fmtDateTime, shortId } from "@/lib/format";
import type { RouteSearchParams } from "@/lib/search-params";
import { reassignVerificationItemAction, recordVerificationVerdictAction, reworkVerificationItemAction } from "./actions";
import { VerificationReviewActionTelemetry } from "./verification-review-telemetry";
import { ReviewVideoPlayer } from "./review-video-player";
import { ReviewEventBuffer } from "./review-events";
import { submitVerificationReviewEvents } from "./review-events-server";

const PATHNAME = "/actions";

function renderLabelOrFallback(label: string | null | undefined): string {
  return label && label.trim() ? label : "—";
}


/**
 * Whether the signed-in principal holds a backend-declared control on this page.
 *
 * /actions serves two personas (verifier vs authority) and the backend contract decides which half
 * each one gets — see compileVerificationReviewControls. A missing control falls back to
 * `fallback` so an older cached contract degrades to the pre-verdict behaviour instead of throwing
 * the whole drawer, which is why this does not use the throwing `control()` helper.
 */
export function VerificationReviewDrawer({
  items,
  initialSelectedId,
  positions,
  searchParams,
  feedback,
  pageContract,
  statusLabels,
}: {
  items: VerificationQueueItem[];
  initialSelectedId?: string;
  positions: PositionListResponse | null;
  searchParams: RouteSearchParams;
  feedback: { status?: string; code?: string };
  pageContract: AdminUiPageContract;
  statusLabels: Record<string, string>;
}) {
  const initialItem = items.find((item) => item.item_id === initialSelectedId);
  const [activeId, setActiveId] = useState(initialItem?.item_id);
  const [displayedId, setDisplayedId] = useState(initialItem?.item_id);
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  const openFrameRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);
  const item = items.find((candidate) => candidate.item_id === displayedId);
  const drawerOpen = Boolean(activeId && item);
  const closeHref = hrefWithout(searchParams, ["vi_row"]);
  const currentIndex = item ? items.findIndex((i) => i.item_id === item.item_id) : -1;
  const canGoBack = currentIndex > 0;
  const canGoForward = currentIndex >= 0 && currentIndex < items.length - 1;

  const syncFromUrl = useCallback((): void => {
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    const id = new URL(window.location.href).searchParams.get("vi_row") ?? undefined;
    const selected = items.find((candidate) => candidate.item_id === id);
    if (selected) {
      triggerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      setActiveId(undefined);
      setDisplayedId(selected.item_id);
      openFrameRef.current = window.requestAnimationFrame(() => {
        setActiveId(selected.item_id);
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

  const stepItem = useCallback((direction: 1 | -1): void => {
    if (!item) return;
    const nextIndex = currentIndex + direction;
    if (nextIndex < 0 || nextIndex >= items.length) return;
    const nextItem = items[nextIndex];
    if (nextItem) {
      replaceLocalOverlayUrl(hrefWithRow(searchParams, nextItem.item_id));
    }
  }, [item, currentIndex, items, searchParams]);

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
  const scopedPositions: PositionListResponse | null = positions
    ? { ...positions, items: positions.items.filter((position) => position.scope_type === "center" && position.scope_id === item.park_id) }
    : null;
  const returnTo = hrefWithRow(searchParams, item.item_id);

  return (
    <>
      <div
        className={`vr-modal-scrim${drawerOpen ? " on" : ""}`}
        aria-label={copy(pageContract, "drawer.close_label")}
        aria-hidden={!drawerOpen}
        onClick={closeDrawer}
        role="presentation"
      />
      <VerificationReviewDrawerPanel
        item={item}
        positions={scopedPositions}
        returnTo={returnTo}
        feedback={feedback}
        open={drawerOpen}
        onClose={closeDrawer}
        closeButtonRef={closeButtonRef}
        pageContract={pageContract}
        statusLabels={statusLabels}
        currentIndex={currentIndex}
        totalItems={items.length}
        canGoBack={canGoBack}
        canGoForward={canGoForward}
        onStepItem={stepItem}
      />
    </>
  );
}

function VerificationReviewDrawerPanel({
  item,
  positions,
  returnTo,
  feedback,
  open,
  onClose,
  closeButtonRef,
  pageContract,
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  statusLabels,
  currentIndex,
  totalItems,
  canGoBack,
  canGoForward,
  onStepItem,
}: {
  item: VerificationQueueItem;
  positions: PositionListResponse | null;
  returnTo: string;
  feedback: { status?: string; code?: string };
  open: boolean;
  onClose: () => void;
  closeButtonRef: React.RefObject<HTMLButtonElement | null>;
  pageContract: AdminUiPageContract;
  statusLabels: Record<string, string>;
  currentIndex: number;
  totalItems: number;
  canGoBack: boolean;
  canGoForward: boolean;
  onStepItem: (direction: 1 | -1) => void;
}) {
  // Keyed by item_id so switching items (Prev/Next or a fresh row click) resets the active proof
  // back to the first one without a setState-in-effect render cascade.
  const [mediaSelection, setMediaSelection] = useState<{ itemId: string; index: number }>({
    itemId: item.item_id,
    index: 0,
  });
  // Two-step reject, mirroring the mock: the footer's Reject reveals the reason field first, and a
  // second press submits — so a rejection can never be recorded without a reason being asked for.
  const [rejecting, setRejecting] = useState(false);
  const mediaIndex = mediaSelection.itemId === item.item_id ? mediaSelection.index : 0;
  const setMediaIndex = (index: number) => setMediaSelection({ itemId: item.item_id, index });
  const [isFullscreen, setIsFullscreen] = useState(false);
  const playerRef = useRef<HTMLDivElement>(null);

  // Initialize the event buffer with the server action for posting events
  const eventBuffer = useMemo(() => {
    return new ReviewEventBuffer((events) => submitVerificationReviewEvents(events));
  }, []);

  // Emit item_opened when the drawer opens
  useEffect(() => {
    if (open) {
      eventBuffer.recordEvent(
        item.item_id,
        "item_opened",
        {
          category: item.category,
          // park_id/shed_id, not the display labels: these are attribution dimensions the CEO
          // aggregate groups by, and a label is not a stable key.
          park_id: item.park_id ?? undefined,
          shed_id: item.shed_id ?? undefined,
          status: item.status,
        },
        item.media[0]?.proof_id,
      );
    }
  }, [open, item.item_id, item.category, item.park_label, item.shed_label, item.status, item.media, eventBuffer]);

  // Emit proof_switched when media changes
  useEffect(() => {
    if (item.media[mediaIndex]) {
      eventBuffer.recordEvent(
        item.item_id,
        "proof_switched",
        {},
        item.media[mediaIndex].proof_id,
      );
    }
  }, [mediaIndex, item.item_id, item.media, eventBuffer]);

  // Flush on the actual open -> false transition, and on unmount.
  //
  // The previous version flushed in the effect cleanup `if (!open)`, which reads backwards: cleanup
  // sees the PREVIOUS render's `open`, so closing an open drawer ran the cleanup captured with
  // open=true and skipped the flush entirely. It only ever fired for a drawer that was already
  // closed — i.e. exactly when there was nothing buffered to send.
  const wasOpenRef = useRef(open);
  useEffect(() => {
    if (wasOpenRef.current && !open) {
      void eventBuffer.forceFlush();
    }
    wasOpenRef.current = open;
  }, [open, eventBuffer]);
  useEffect(() => {
    return () => {
      // dispose(), not forceFlush(): it flushes AND releases the interval plus the window listeners
      // this buffer registered, which otherwise accumulate for every drawer that is ever opened.
      void eventBuffer.dispose();
    };
  }, [eventBuffer]);

  useEffect(() => {
    const onFullscreenChange = () => {
      const newFullscreen = document.fullscreenElement === playerRef.current;
      setIsFullscreen(newFullscreen);
      // Emit fullscreen_toggled event
      const activeMedia = item.media[mediaIndex];
      if (activeMedia) {
        eventBuffer.recordEvent(
          item.item_id,
          "fullscreen_toggled",
          { video_position_ms: 0 },
          activeMedia.proof_id,
        );
      }
    };
    document.addEventListener("fullscreenchange", onFullscreenChange);
    return () => document.removeEventListener("fullscreenchange", onFullscreenChange);
  }, [item.item_id, item.media, mediaIndex, eventBuffer]);

  const toggleFullscreen = useCallback((): void => {
    if (document.fullscreenElement) {
      void document.exitFullscreen();
      return;
    }
    playerRef.current?.requestFullscreen?.().catch(() => {
      /* full screen unsupported/blocked — the player still shows the video inline */
    });
  }, []);

  // Set while the verdict telemetry is being flushed, so the re-submit below is not intercepted again.
  const verdictTelemetrySentRef = useRef(false);

  // Handle verdict form submission to emit verdict_recorded event.
  //
  // The submit is HELD until the telemetry has been posted. Firing it fire-and-forget raced the
  // Server Action: the verdict write navigates/re-renders, which can tear the page down before the
  // flush completes, losing the single most important event in the stream — the decision itself,
  // plus whatever watch events were still buffered behind it. Telemetry failures never block the
  // verdict: forceFlush() does not throw, and a failed batch is re-queued idempotently.
  const handleVerdictSubmit = useCallback((e: React.FormEvent<HTMLFormElement>) => {
    if (verdictTelemetrySentRef.current) {
      verdictTelemetrySentRef.current = false;
      return; // the re-submit below — let it through to the Server Action
    }
    const form = e.currentTarget;
    const submitter = (e.nativeEvent as SubmitEvent).submitter as HTMLButtonElement | null;
    const decision = (submitter?.value ??
      form.querySelector('button[type="submit"]')?.getAttribute("value")) as "approved" | "rejected";
    if (!decision) return;

    e.preventDefault();
    void eventBuffer.recordVerdict(item.item_id, decision).finally(() => {
      verdictTelemetrySentRef.current = true;
      // requestSubmit preserves which button submitted, so the Server Action still receives the
      // approve/reject value; form.submit() would drop it.
      if (submitter) form.requestSubmit(submitter);
      else form.requestSubmit();
    });
  }, [item.item_id, eventBuffer]);

  const activeMedia = item.media[Math.min(mediaIndex, Math.max(item.media.length - 1, 0))];
  const hasTask = Boolean(item.source.task_id);
  const isFlagged = item.status === "rejected";
  const reworkDisabled = !hasTask || !isFlagged;
  const reassignDisabled = !hasTask || !positions || positions.items.length === 0;
  const hasEvidence = item.media.length > 0;
  const text = (key: string) => copy(pageContract, key);

  // Duty split (verifier-app-and-flow.md §Roles): the verifier records the verdict, the authority
  // acts on the source task. A principal sees only the half they hold — showing the other half
  // disabled would advertise an authority they do not have and, for the verifier-only workspace,
  // would put the authority's own actions in front of the person the separation exists to isolate.
  const mayReview = controlEnabled(pageContract, "record_verdict", false);
  const mayAct = controlEnabled(pageContract, "request_rework", true);
  // A verdict is terminal: approved/rejected items stay open for viewing but cannot be re-decided.
  const verdictSettled = item.status !== "pending";

  return (
      <div className={`vr-modal${open ? " on" : ""}`} aria-label={text("drawer.aria")} aria-hidden={!open} inert={!open}>
        <div className="vr-modal-hd">
          <div>
            <h2>{item.category} · {shortId(item.item_id)}</h2>
            <div className="sb">{item.module} · {item.vertical}</div>
          </div>
          <button ref={closeButtonRef} type="button" className="x" aria-label={text("drawer.close_label")} onClick={onClose}>
            &times;
          </button>
        </div>

        <div className="vr-modal-bd">
          <VerificationReviewActionTelemetry status={feedback.status} code={feedback.code} />
          {feedback.status ? (
            <div className={feedback.status === "success" ? "alert ok" : "alert warn"} style={{ marginBottom: 12 }}>
              <b>{feedback.status === "success" ? text("feedback.done") : text("feedback.failed")}</b>&nbsp;
              {feedback.code ?? ""}
            </div>
          ) : null}

          {!hasEvidence && (
            <div className="warnbox" style={{ marginBottom: 14 }}>
              <b>{text("verdict.disabled_no_evidence")}</b>
            </div>
          )}

          {/* Video Player: Large, centered, capped at 44vh */}
          {item.media.length === 0 ? (
            <div className="vr-player" ref={playerRef} style={{ background: "var(--panel-2)", justifyContent: "center" }}>
              <div className="vr-player-empty">{text("drawer.media.empty")}</div>
            </div>
          ) : (
            <div className="vr-player" ref={playerRef}>
              {activeMedia?.mime_type?.startsWith("video/") ? (
                <ReviewVideoPlayer
                  key={activeMedia.proof_id}
                  src={activeMedia.download_url}
                  mimeType={activeMedia.mime_type}
                  proofId={activeMedia.proof_id}
                  itemId={item.item_id}
                  eventBuffer={eventBuffer}
                />
              ) : (
                <div className="vr-player-empty">{text("drawer.media.empty")}</div>
              )}
              <button
                type="button"
                className="vr-fsbtn"
                onClick={toggleFullscreen}
                aria-label={text("drawer.media.fullscreen_label")}
                title={text("drawer.media.fullscreen_label")}
              >
                {isFullscreen ? <Minimize className="ic" /> : <Maximize className="ic" />}
              </button>
            </div>
          )}

          {/* Proof Switcher: Only when 2+ media */}
          {item.media.length > 1 ? (
            <div className="vr-proofstrip">
              {item.media.map((media, index) => (
                <button
                  key={media.proof_id}
                  type="button"
                  className={`vr-pthumb${index === mediaIndex ? " on" : ""}`}
                  onClick={() => setMediaIndex(index)}
                >
                  <PlayCircle className="ic" />
                  {media.label || shortId(media.proof_id)}
                </button>
              ))}
            </div>
          ) : null}

          {/* Facts Grid: Mock anatomy with label/value pairs */}
          <div className="vr-facts">
            <div className="vr-fact">
              <b>{text("drawer.meta.operator")}</b>
              {renderLabelOrFallback(item.operator_name)}
            </div>
            <div className="vr-fact">
              <b>{text("drawer.meta.captured")}</b>
              {fmtDateTime(item.captured_at)}
            </div>
            <div className="vr-fact">
              <b>{text("drawer.meta.shed")}</b>
              {renderLabelOrFallback(item.shed_label)}
            </div>
            <div className="vr-fact">
              <b>{text("drawer.meta.park")}</b>
              {renderLabelOrFallback(item.park_label)}
            </div>
            {item.verified_by_name && (
              <div className="vr-fact">
                <b>{text("drawer.meta.verified_by")}</b>
                {renderLabelOrFallback(item.verified_by_name)}
              </div>
            )}
            {/* Each proof's recorded answer */}
            {item.media.map((media) =>
              media.answer ? (
                <div key={media.proof_id} className="vr-fact">
                  <b>{media.label || text("drawer.media.title")}</b>
                  {media.answer}
                </div>
              ) : null
            )}
            {/* Rejection reason when rejected */}
            {item.status === "rejected" && item.verdict_reason && (
              <div className="vr-fact">
                <b>{text("drawer.meta.reason")}</b>
                {item.verdict_reason}
              </div>
            )}
          </div>

          {mayReview ? (
            <form id="verdict-form" action={recordVerificationVerdictAction} onSubmit={handleVerdictSubmit} style={{ display: "grid", gap: 8 }}>
                  <input type="hidden" name="item_id" value={item.item_id} />
                  {/* Guards THIS item's row: a verdict recorded elsewhere since render makes the
                      submit 409 instead of silently overwriting the other reviewer's decision. */}
                  <input type="hidden" name="row_version" value={item.row_version} />
                  <input type="hidden" name="return_to" value={returnTo} />
                  <label className="fld" style={{ marginBottom: 0, display: rejecting ? "grid" : "none" }}>
                    <span>{text("verdict.reason_label")}</span>
                    {/* Deliberately not `required`: the same field is mandatory for Reject and
                        unused for Approve, so the rule lives in the server action and the backend
                        (422), not in a per-button HTML attribute. */}
                    <textarea
                      name="reason"
                      rows={2}
                      placeholder={text("verdict.reason_placeholder")}
                      disabled={verdictSettled}
                    />
                  </label>
                  {rejecting ? <div className="small muted">{text("verdict.reason_required")}</div> : null}
                  {verdictSettled ? <div className="note">{text("verdict.disabled_not_pending")}</div> : null}
                  {!hasEvidence ? <div className="note">{text("verdict.disabled_no_evidence")}</div> : null}
            </form>
          ) : null}

          {mayAct ? (
          <>
          <section className="card" style={{ marginTop: 14 }}>
            <div className="hd">
              <h3>{text("rework.title")}</h3>
            </div>
            <div className="bd">
              <form action={reworkVerificationItemAction} style={{ display: "grid", gap: 8 }}>
                <input type="hidden" name="task_id" value={item.source.task_id ?? ""} />
                <input type="hidden" name="return_to" value={returnTo} />
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{text("rework.reason_label")}</span>
                  <textarea name="reason" rows={2} placeholder={text("rework.reason_placeholder")} disabled={reworkDisabled} required />
                </label>
                {!isFlagged ? <div className="note">{text("rework.disabled_not_rejected")}</div> : null}
                {!hasTask ? <div className="note">{text("rework.disabled_no_task")}</div> : null}
                <button type="submit" className="btn p" disabled={reworkDisabled} aria-disabled={reworkDisabled} title={reworkDisabled ? (!hasTask ? text("rework.disabled_no_task") : text("rework.disabled_not_rejected")) : undefined}>
                  {text("rework.submit")}
                </button>
              </form>
            </div>
          </section>

          <section className="card" style={{ marginTop: 14 }}>
            <div className="hd">
              <h3>{text("reassign.title")}</h3>
            </div>
            <div className="bd">
              <form action={reassignVerificationItemAction} style={{ display: "grid", gap: 8 }}>
                <input type="hidden" name="task_id" value={item.source.task_id ?? ""} />
                <input type="hidden" name="return_to" value={returnTo} />
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{text("reassign.assignee_label")}</span>
                  <select name="assigned_to" disabled={reassignDisabled} required defaultValue="">
                    <option value="" disabled>
                      {text("reassign.assignee_placeholder")}
                    </option>
                    {(positions?.items ?? []).map((position) => (
                      <option key={position.position_id} value={position.workforce_member_id}>
                        {position.position_code} · {position.position_tier} · {shortId(position.workforce_member_id)}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{text("reassign.reason_label")}</span>
                  <textarea name="reason" rows={2} placeholder={text("reassign.reason_placeholder")} disabled={reassignDisabled} required />
                </label>
                {reassignDisabled ? (
                  <div className="note">{!hasTask ? text("reassign.disabled_no_task") : text("reassign.disabled_no_roster")}</div>
                ) : null}
                <button
                  type="submit"
                  className="btn"
                  disabled={reassignDisabled}
                  aria-disabled={reassignDisabled}
                  title={reassignDisabled ? (!hasTask ? text("reassign.disabled_no_task") : text("reassign.disabled_no_roster")) : undefined}
                >
                  {text("reassign.submit")}
                </button>
              </form>
            </div>
          </section>

          <section className="card" style={{ marginTop: 14 }}>
            <div className="hd">
              <h3>{text("penalty.title")}</h3>
            </div>
            <div className="bd">
              <div style={{ display: "grid", gap: 8 }}>
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{text("penalty.reason_label")}</span>
                  <textarea name="penalty_note" rows={2} placeholder={text("penalty.reason_placeholder")} disabled />
                </label>
                <div className="note">{text("penalty.disabled")}</div>
                <button type="button" className="btn" disabled aria-disabled title={text("penalty.disabled")}>
                  {text("penalty.submit")}
                </button>
              </div>
            </div>
          </section>
          </>
          ) : null}
        </div>

        <div className="vr-modal-ft">
          {/* Position indicator and navigation */}
          <span className="pos">{currentIndex >= 0 ? `${currentIndex + 1} of ${totalItems}` : ""}</span>

          {/* Prev/Next buttons on left */}
          <button type="button" className="btn" onClick={() => onStepItem(-1)} disabled={!canGoBack} title="Previous item">
            ← Prev
          </button>
          <button type="button" className="btn" onClick={() => onStepItem(1)} disabled={!canGoForward} title="Next item">
            Next →
          </button>

          {/* Spacer */}
          <div style={{ marginLeft: "auto" }} />

          {/* Reject and Accept buttons on right (shown only for verifiers) */}
          {mayReview ? (
            <>
              <button
                type={rejecting ? "submit" : "button"}
                onClick={rejecting ? undefined : () => setRejecting(true)}
                form="verdict-form"
                name="decision"
                value="rejected"
                className="btn"
                disabled={verdictSettled}
                title={verdictSettled ? text("verdict.disabled_not_pending") : undefined}
              >
                Reject
              </button>
              <button
                type="submit"
                form="verdict-form"
                name="decision"
                value="approved"
                className="btn p"
                disabled={verdictSettled || !hasEvidence}
                title={!hasEvidence ? text("verdict.disabled_no_evidence") : verdictSettled ? text("verdict.disabled_not_pending") : undefined}
              >
                Accept
              </button>
            </>
          ) : null}
        </div>
      </div>
  );
}

function hrefWithout(params: RouteSearchParams, exclude: string[]): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (exclude.includes(key)) continue;
    if (Array.isArray(value)) {
      for (const item of value) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  const qs = next.toString();
  return qs ? `${PATHNAME}?${qs}` : PATHNAME;
}

function hrefWithRow(params: RouteSearchParams, itemId: string): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (key === "vi_row" || key === "va_status" || key === "va_code") continue;
    if (Array.isArray(value)) {
      for (const item of value) if (item) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  next.set("vi_row", itemId);
  return `${PATHNAME}?${next.toString()}`;
}
