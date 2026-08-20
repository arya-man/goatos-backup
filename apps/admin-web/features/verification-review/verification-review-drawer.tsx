"use client";

import {
  currentHistoryEntryIsLocalOverlay,
  LOCAL_OVERLAY_URL_CHANGE_EVENT,
  replaceLocalOverlayUrl,
} from "@/components/local-overlay-link";
import { ImageIcon, Maximize, Minimize, PlayCircle } from "lucide-react";
import { useCallback, useEffect, useRef, useState, useMemo } from "react";

import { controlEnabled, copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { VerificationQueueItem } from "@/lib/api/server";
import { fmtDateTime, shortId } from "@/lib/format";
import type { RouteSearchParams } from "@/lib/search-params";
import { recordVerificationVerdictAction } from "./actions";
import { VerificationReviewActionTelemetry } from "./verification-review-telemetry";
import { ReviewVideoPlayer } from "./review-video-player";
import { ReviewEventBuffer } from "./review-events";
import { submitVerificationReviewEvents } from "./review-events-server";

/* The re-assign roster machinery that used to live here is GONE with the authority panels it fed.
   It was originally an unconditional SSR fetch of 500 staff positions on every Actions page load,
   then a lazy per-park load; now that the picker is not on this screen, the verifier's queue does
   not read the roster at all. One less read on a screen with a sub-500ms budget. */

// The open class must never depend on ONE animation frame landing. A busy or backgrounded tab can
// skip it, and this component's own effect cleanup cancels it whenever syncFromUrl changes identity
// -- leaving a mounted drawer parked off-screen behind its scrim: shade visible, no panel. A timer
// races the frame, and whichever lands first wins. Same remedy as b76a7c1dd applied to the shared
// hook and the Action Center drawer; THIS drawer keeps a private copy of the logic and was missed.
const OPEN_FALLBACK_MS = 50;

const PATHNAME = "/verify";

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
  nextCursor,
  nextTrail,
  actionTypeLabels,
  searchParams,
  feedback,
  pageContract,
  statusLabels,
}: {
  items: VerificationQueueItem[];
  initialSelectedId?: string;
  nextCursor?: string;
  nextTrail?: string;
  // Backend-owned module/page labels, keyed by category, composed once by the page so the drawer
  // subtitle and the queue's ACTION TYPE column can never disagree.
  actionTypeLabels: Record<string, string>;
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
  const openFallbackRef = useRef<number | null>(null);
  const closeTimerRef = useRef<number | null>(null);
  const item = items.find((candidate) => candidate.item_id === displayedId);
  const drawerOpen = Boolean(activeId && item);
  const closeHref = hrefWithout(searchParams, ["vi_row", "vi_open_first"]);
  const currentIndex = item ? items.findIndex((i) => i.item_id === item.item_id) : -1;
  const canGoBack = currentIndex > 0;
  const canGoForward = currentIndex >= 0 && currentIndex < items.length - 1;

  const syncFromUrl = useCallback((): void => {
    if (closeTimerRef.current !== null) window.clearTimeout(closeTimerRef.current);
    if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
    const id = new URL(window.location.href).searchParams.get("vi_row") ?? undefined;
    const selected = items.find((candidate) => candidate.item_id === id);
    if (openFallbackRef.current !== null) window.clearTimeout(openFallbackRef.current);
    if (selected) {
      triggerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      setActiveId(undefined);
      setDisplayedId(selected.item_id);
      const open = (): void => {
        if (openFrameRef.current !== null) window.cancelAnimationFrame(openFrameRef.current);
        if (openFallbackRef.current !== null) window.clearTimeout(openFallbackRef.current);
        openFrameRef.current = null;
        openFallbackRef.current = null;
        setActiveId(selected.item_id);
      };
      openFrameRef.current = window.requestAnimationFrame(open);
      openFallbackRef.current = window.setTimeout(open, OPEN_FALLBACK_MS);
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
      if (openFallbackRef.current !== null) window.clearTimeout(openFallbackRef.current);
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

  const returnTo = hrefWithRow(searchParams, item.item_id);
  // The item AFTER this one in the currently rendered queue order. Carried through the verdict
  // form so an APPROVAL advances the drawer to the next video instead of dropping the verifier
  // back to the list (on the pending tab the approved item leaves the filtered list, so the
  // redirect's vi_row no longer resolves and the drawer closed). If the current page is exhausted
  // but the keyset queue has another page, the action follows nextCursor instead of declaring the
  // queue done.
  const nextRowId = currentIndex >= 0 ? (items[currentIndex + 1]?.item_id ?? "") : "";

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
        actionTypeLabel={actionTypeLabels[item.category] ?? item.category}
        returnTo={returnTo}
        nextRowId={nextRowId}
        nextCursor={nextCursor ?? ""}
        nextTrail={nextTrail ?? ""}
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
  actionTypeLabel,
  returnTo,
  nextRowId,
  nextCursor,
  nextTrail,
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
  actionTypeLabel: string;
  returnTo: string;
  /** The next queue row's item_id, "" when this page is exhausted. See nextRowId at the call site. */
  nextRowId: string;
  /** Keyset cursor/trail for the next page, used only when nextRowId is empty. */
  nextCursor: string;
  nextTrail: string;
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
  //
  // Seeded from the feedback code, which is what makes the failure RECOVERABLE. Submitting with an
  // empty reason redirects back with va_code=missing_reason, and the drawer re-mounts -- which
  // reset this to false and HID the reason field again. The verifier was told the reason was
  // missing while the only box she could type it into had just disappeared, and pressing Reject
  // again simply repeated the error. It was an inescapable loop.
  const [rejecting, setRejecting] = useState(feedback.code === "missing_reason");
  // The reason TEXT is state, not just DOM, because the Reject button's type depends on it: an
  // empty reason must never be submittable. Seeding `rejecting` open (above) was not enough on its
  // own -- it also flipped Reject straight to type=submit, so the next click re-posted an empty
  // reason and bounced with the same error. Correct-by-construction beats a better error message.
  const [reason, setReason] = useState("");
  // The measurement TEXT is state, not just DOM, for the same reason `reason` is: the Accept
  // button's enabled-ness depends on it where the category requires a number, and a value read off
  // an uncontrolled input cannot drive that. Kept as a STRING so blank ("she typed nothing") stays
  // distinct from "0" (an empty trough, a real wastage reading).
  //
  // KEYED BY item_id, like mediaSelection above, because this panel is not remounted when the
  // verifier steps to the next video. Plain state would carry one animal's weight onto the next
  // animal's Accept -- a wrong number written to a record she never typed it for. Reading it back
  // through the key resets it on every item switch without a setState-in-effect cascade.
  const [measurement, setMeasurement] = useState<{ itemId: string; value: string }>({
    itemId: item.item_id,
    value: "",
  });
  const measurementValue = measurement.itemId === item.item_id ? measurement.value : "";
  const setMeasurementValue = useCallback(
    (value: string) => setMeasurement({ itemId: item.item_id, value }),
    [item.item_id],
  );
  const reasonRef = useRef<HTMLTextAreaElement>(null);
  const reasonReady = reason.trim().length > 0;

  // When the reason field opens -- by pressing Reject, or seeded by the server's missing_reason --
  // put the caret in it AND scroll it into view. The modal body is a scrolling column with the
  // verdict footer pinned below it, so on a short viewport the field opens BELOW the fold: the
  // verifier presses Reject, the screen looks unchanged, and the box she is being asked to fill is
  // off-screen under the footer. Focus alone is not enough -- it does not reliably scroll inside an
  // overflow container -- so the scroll is explicit.
  useEffect(() => {
    if (!rejecting) return;
    const field = reasonRef.current;
    if (!field) return;
    field.focus();
    field.scrollIntoView({ block: "nearest" });
  }, [rejecting]);

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
  const text = (key: string) => copy(pageContract, key);
  const hasEvidence = item.media.length > 0;
  // feedback.<code> is the backend-owned human sentence for a server error code; an unmapped code
  // renders nothing extra rather than printing the token itself.
  const feedbackMessage = (code: string) => copy(pageContract, `feedback.${code}`, "");

  // The heading is the same sentence the verifier clicked in the queue -- shed, animal/tag,
  // vaccine, weight -- falling back to operator/shed only when the backend sent no subject. It is
  // never the item id: an id tells her nothing about the video she is about to judge.
  const subjectHeading = item.subject_label?.trim()
    ? item.subject_label
    : [item.operator_name, item.shed_label].filter(Boolean).join(" · ") || text("drawer.eyebrow");

  // Duty split (verifier-app-and-flow.md §Roles): the verifier records the verdict, the authority
  // acts on the source task. This screen now carries ONLY the verifier's half -- the authority
  // panels were removed on 2026-08-07 because they are not hers (SPEC section 1/3). mayAct is gone
  // with them; keeping a permission flag for controls that no longer exist is how a screen quietly
  // regrows them.
  const mayReview = controlEnabled(pageContract, "record_verdict", false);
  // The backend attaches this only to items carrying a number the verifier may correct, and owns
  // every word of the control. Absent -- every category but weighing today -- means no control.
  const correction = item.measurement_correction;
  const measurementReasonSupported = correction?.ref_type !== "feed_wastage_completion";
  // A verdict is terminal: approved/rejected items stay open for viewing but cannot be re-decided.
  const verdictSettled = item.status !== "pending";
  // Blank means she has typed nothing. It is NOT a zero: for wastage an empty trough is a real
  // reading, so the two must stay distinguishable all the way to the server action.
  const measurementEntered = measurementValue.trim() !== "";
  // Feed wastage cannot be approved without a number -- the operator submits only a video, so the
  // reading is born on this screen. The backend refuses it too (422 measurement_required); doing it
  // here as well means she is told before she loses a round-trip. Weighing's flag is false, so a
  // verifier who agrees with the operator's weight still approves in one press.
  const measurementMissing = Boolean(correction?.required_for_approve) && !measurementEntered;

  return (
      <div className={`vr-modal${open ? " on" : ""}`} aria-label={text("drawer.aria")} aria-hidden={!open} inert={!open}>
        <div className="vr-modal-hd">
          <div>
            {/* This heading used to be the raw category token joined to a shortened item id, over
                the raw module/vertical pair -- a config token plus a UUID as the headline of the
                review surface. The locked spec bans rendering an id as a label, and the raw
                vertical/module pair is the same leak just removed from the queue table. The
                verifier needs the sentence she clicked: shed, animal/tag, vaccine, weight. */}
            <h2>{subjectHeading}</h2>
            <div className="sb">{actionTypeLabel}</div>
          </div>
          <button ref={closeButtonRef} type="button" className="x" aria-label={text("drawer.close_label")} onClick={onClose}>
            &times;
          </button>
        </div>

        <div className="vr-modal-bd">
          <VerificationReviewActionTelemetry status={feedback.status} code={feedback.code} />
          {feedback.status ? (
            <div className={feedback.status === "success" ? "alert ok" : "alert warn"} style={{ marginBottom: 12 }}>
              {/* The raw server code (missing_reason, permission_denied, ...) is an internal token
                  and must not be the sentence a verifier reads. Resolve it to backend-owned copy,
                  falling back to the generic failure line rather than leaking the token. */}
              <b>{feedback.status === "success" ? text("feedback.done") : text("feedback.failed")}</b>
              {feedback.code ? <>&nbsp;{feedbackMessage(feedback.code)}</> : null}
            </div>
          ) : null}

          {!hasEvidence && (
            <div className="warnbox" style={{ marginBottom: 14 }}>
              <b>{text("verdict.disabled_no_evidence")}</b>
            </div>
          )}

          {/* Media Player: Large, centered, capped at 44vh */}
          {item.media.length === 0 ? (
            <div className="vr-player" ref={playerRef} style={{ background: "var(--panel-2)", justifyContent: "center" }}>
              <div className="vr-player-empty">{text("drawer.media.empty")}</div>
            </div>
          ) : (
            <div className="vr-player" ref={playerRef}>
              {/* A proof is not always a video. Feed distribution's WATER proof is
                  explicitly photo-or-video (docs/decisions/feed-distribution-verification.md),
                  so an image/* proof is a valid capture and must render as a picture.
                  It previously fell through to the "no media" empty state, which told the
                  verifier her evidence was missing while it sat uploaded and reviewable.
                  There is no seek/watch telemetry for a still: the review events this
                  player emits are all video positions, and a photo has none. */}
              {activeMedia?.mime_type?.startsWith("video/") ? (
                <ReviewVideoPlayer
                  key={activeMedia.proof_id}
                  src={activeMedia.download_url}
                  mimeType={activeMedia.mime_type}
                  proofId={activeMedia.proof_id}
                  itemId={item.item_id}
                  eventBuffer={eventBuffer}
                  // Backend-owned copy for the double-speed control; the player renders it and
                  // composes none of it. It only appears on clips longer than 20 seconds.
                  speedLabels={{
                    normal: text("player.speed_normal"),
                    fast: text("player.speed_fast"),
                    hint: text("player.speed_hint"),
                  }}
                />
              ) : activeMedia?.mime_type?.startsWith("image/") ? (
                // A signed, short-lived proof URL on an external media host: next/image would
                // proxy and cache evidence, so this stays a plain <img>.
                // eslint-disable-next-line @next/next/no-img-element
                <a key={activeMedia.proof_id} href={activeMedia.download_url} target="_blank" rel="noreferrer" className="vr-image-link">
                  <img className="vr-image-proof" src={activeMedia.download_url} alt={activeMedia.label || subjectHeading || text("drawer.media.title")} />
                </a>
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
                  {/* The icon states what the chip switches TO. A play badge on a photo
                      proof promises a clip that does not exist. */}
                  {media.mime_type?.startsWith("image/") ? (
                    <ImageIcon className="ic" />
                  ) : (
                    <PlayCircle className="ic" />
                  )}
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
              {renderLabelOrFallback(item.operational_location_display || item.shed_label)}
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

          {/* THE APPROVE CARRIES THE NUMBER (maintainer decision 2026-08-20, replacing the
              separate save forms of the 2026-08-17 weighing and 2026-08-18 wastage decisions).

              There used to be two sibling forms here, each with its own save button posting to its
              producing module's route. Saving relabelled the item, which bumps row_version, so the
              Accept she pressed next carried the version this page rendered with and the
              version-fenced verdict matched nothing -- she pressed Accept and nothing happened.

              So the field now sits INSIDE the verdict form: she types the number and presses
              Accept once, and the backend applies the value and the verdict together, resolving
              the producer route from the item's own source.

              Shown only when the backend attaches measurement_correction. Every visible word comes
              from that block; this component composes none of it, and the head-count field appears
              only when the backend sent count_label -- a lump-sum shed proof has one, a single
              animal's proof does not. Gated on the same record_verdict control as the verdict:
              correcting the number the evidence shows belongs to the person judging the evidence. */}
          {mayReview && correction ? (
            <div
              style={{ display: "grid", gap: 8, marginBottom: 14, paddingBottom: 14, borderBottom: "1px solid var(--line)" }}
            >
              <div>
                <b>{correction.title}</b>
                <div className="small muted">{correction.help}</div>
              </div>
              <label className="fld" style={{ marginBottom: 0 }}>
                <span>{correction.value_label}</span>
                {/* min is 0, never 0.001: for wastage an empty trough is a real, good measurement
                    and zero must stay enterable. Deliberately NOT `required` -- the field is
                    optional for weighing, and where it IS required the Accept button below carries
                    the rule, so she is never blocked by a browser message on a form she also uses
                    to Reject. */}
                <input
                  form="verdict-form"
                  type="number"
                  name="measurement_value"
                  step="0.001"
                  min="0"
                  max="100000"
                  inputMode="decimal"
                  value={measurementValue}
                  onChange={(e) => setMeasurementValue(e.target.value)}
                  disabled={verdictSettled}
                />
              </label>
              {correction.count_label ? (
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{correction.count_label}</span>
                  {/* Blank means "leave the recorded count alone", which is the normal case -- she
                      is usually fixing a mistyped total, not a miscount. */}
                  <input
                    /* Uncontrolled, so it needs a key to be REMOUNTED on an item switch -- see
                       measurement above: a head count left in the DOM would be sent with the next
                       animal's Accept. */
                    key={item.item_id}
                    form="verdict-form"
                    type="number"
                    name="measurement_count"
                    step="1"
                    min="1"
                    max="100000"
                    inputMode="numeric"
                    disabled={verdictSettled}
                  />
                </label>
              ) : null}
              {measurementReasonSupported ? (
                <label className="fld" style={{ marginBottom: 0 }}>
                  <span>{text("verdict.reason_label")}</span>
                  <textarea key={item.item_id} form="verdict-form" name="measurement_reason" rows={2} disabled={verdictSettled} />
                </label>
              ) : null}
              {/* Named the same way the button below is: an Accept she cannot press needs to say
                  why, or it reads as a broken screen. */}
              {measurementMissing ? <div className="note">{text("verdict.disabled_measurement_required")}</div> : null}
            </div>
          ) : null}

          {mayReview ? (
            <form id="verdict-form" action={recordVerificationVerdictAction} onSubmit={handleVerdictSubmit} style={{ display: "grid", gap: 8 }}>
                  <input type="hidden" name="item_id" value={item.item_id} />
                  {/* Guards THIS item's row: a verdict recorded elsewhere since render makes the
                      submit 409 instead of silently overwriting the other reviewer's decision. */}
                  <input type="hidden" name="row_version" value={item.row_version} />
                  <input type="hidden" name="return_to" value={returnTo} />
                  {/* Lets an APPROVAL advance straight to the next video (see the action). Sent for
                      both decisions but read only on approve — a rejection keeps its current
                      return, because the verifier may still be mid-thought on the reason. */}
                  <input type="hidden" name="next_row" value={nextRowId} />
                  <input type="hidden" name="next_cursor" value={nextCursor} />
                  <input type="hidden" name="next_trail" value={nextTrail} />
                  <label className="fld" style={{ marginBottom: 0, display: rejecting ? "grid" : "none" }}>
                    <span>{text("verdict.reason_label")}</span>
                    {/* Deliberately not `required`: the same field is mandatory for Reject and
                        unused for Approve, so the rule lives in the server action and the backend
                        (422), not in a per-button HTML attribute. */}
                    <textarea
                      ref={reasonRef}
                      name="reason"
                      rows={2}
                      placeholder={text("verdict.reason_placeholder")}
                      disabled={verdictSettled}
                      value={reason}
                      onChange={(e) => setReason(e.target.value)}
                    />
                  </label>
                  {rejecting ? <div className="small muted">{text("verdict.reason_required")}</div> : null}
                  {verdictSettled ? <div className="note">{text("verdict.disabled_not_pending")}</div> : null}
                  {!hasEvidence ? <div className="note">{text("verdict.disabled_no_evidence")}</div> : null}
                  {/* Reject stays available: she can always send an unreadable clip back, and it is
                      the ONLY correct move when the number cannot be read at all. Only Accept is
                      held. */}
            </form>
          ) : null}

          {/* The authority half -- Rework, Re-assign, Penalty note -- was REMOVED from this screen
              (maintainer decision 2026-08-07). mock/verifier-web-mock.SPEC.md section 1 is explicit
              about what this surface is: "She watches a proof video and accepts it, or rejects it
              with a reason. That is all. Nothing else belongs on this screen." Section 3 bans
              source-task action here by name, because it is verification.act and not the verifier's.

              These panels were gated on request_rework, which a CEO holds and a verifier does not,
              so the two roles saw disjoint screens: leadership got three task-management forms and
              never the verdict controls, the verifier got the verdict and never these. That is not
              a permission subtlety a reviewer can see -- it reads as "the accept button is missing".

              Penalty note was additionally DEAD: no server action, no route, and its own visible
              label admitted it was unbacked while leaking an internal word into user-facing copy.

              Rework and Re-assign remain real and wired (requestSopTaskRework / assignSopTask in
              ./actions.ts, kept for the authority surface that owns them). Only their placement on
              the verifier's review screen is removed. */}
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
              {/* type=submit ONLY once a reason exists. While the box is empty this stays a plain
                  button that opens/refocuses the field, so an empty rejection cannot be POSTed at
                  all -- the server's 422 becomes unreachable from the UI instead of something the
                  verifier has to read and recover from. */}
              <button
                type={rejecting && reasonReady ? "submit" : "button"}
                onClick={
                  rejecting && reasonReady
                    ? undefined
                    : () => {
                        setRejecting(true);
                        reasonRef.current?.focus();
                      }
                }
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
                disabled={verdictSettled || !hasEvidence || measurementMissing}
                title={
                  !hasEvidence
                    ? text("verdict.disabled_no_evidence")
                    : verdictSettled
                      ? text("verdict.disabled_not_pending")
                      : measurementMissing
                        ? text("verdict.disabled_measurement_required")
                        : undefined
                }
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
