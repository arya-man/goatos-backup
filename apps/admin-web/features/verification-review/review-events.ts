/**
 * Review events telemetry for the verifier screen.
 *
 * Buffers review events (video play, pause, seek, verdict, etc.) and submits them to
 * POST /verification/review-events for CEO/leadership verification proof. Events are
 * minted with stable client_event_id once and reused on retry so duplicate submissions
 * do not double-count.
 */

import React from "react";

export type ReviewEventType =
  | "queue_opened"
  | "item_opened"
  | "video_play"
  | "video_pause"
  | "video_seek_attempt"
  | "video_ended"
  | "proof_switched"
  | "fullscreen_toggled"
  | "verdict_recorded";

export interface ReviewEventPayload {
  video_position_ms?: number;
  video_duration_ms?: number;
  seek_from_ms?: number;
  seek_to_ms?: number;
  verdict?: "approved" | "rejected";
  category?: string;
  park_id?: string;
  shed_id?: string;
  status?: string;
}

export interface PendingEvent {
  // Null for queue-scoped events: queue_opened fires on landing, before any item is chosen, and the
  // backend enforces item_id IS NULL for it (migration 000118). Never send a placeholder string.
  item_id: string | null;
  proof_id?: string;
  session_id: string;
  event_type: ReviewEventType;
  occurred_at: string;
  client_event_id: string;
  payload: ReviewEventPayload;
}

/**
 * ReviewEventBuffer manages buffering, flushing, and retry logic for review events.
 * - Mints client_event_id once per event and reuses it on retry
 * - Flushes on timer (~10s), modal close, verdict submit, and visibility change
 * - Never exposes bearer token; posts through authenticated Server Action
 *
 * DELIVERY BOUNDARY, stated because a docstring here once claimed a guarantee the code did not
 * implement: there is no navigator.sendBeacon path. Beacon needs a plain URL and this posts through a
 * Server Action, so the last buffered batch can be lost if the window is closed outright rather than
 * hidden. The `visibilitychange` -> hidden flush covers tab switches and most closes, forceFlush()
 * covers modal close and verdict submit, and every event carries a stable client_event_id, so a retry
 * can never double-count. What is at risk is at most the trailing batch of a hard window close.
 */
/** Retry ceiling: ~10 minutes of a busy review at the 10s flush cadence, then oldest-first drop. */
const MAX_BUFFERED_EVENTS = 500;

export class ReviewEventBuffer {
  private sessionId = typeof window !== "undefined" && crypto ? crypto.randomUUID() : "";
  private events: PendingEvent[] = [];
  private flushTimer: NodeJS.Timeout | null = null;
  private flushIntervalMs = 10000; // ~10 seconds
  private readonly postFn: (events: PendingEvent[]) => Promise<void>;
  private lastWatchedMs = 0; // Track last watched position for seek blocking
  // Kept so dispose() can actually detach them. Without this every modal open added another pair of
  // window listeners that lived for the rest of the session, and each dead buffer still woke up on
  // every visibility change to flush a queue nobody would read.
  private unloadHandlers: Array<[string, () => void]> = [];

  constructor(postFn: (events: PendingEvent[]) => Promise<void>) {
    this.postFn = postFn;
    this.startFlushTimer();
    this.setupUnloadHandlers();
  }

  private startFlushTimer(): void {
    if (this.flushTimer) clearInterval(this.flushTimer);
    this.flushTimer = setInterval(() => this.flush(), this.flushIntervalMs);
    // A periodic timer must never be the reason a process or a test run refuses to exit; the browser
    // does not care either way.
    (this.flushTimer as unknown as { unref?: () => void }).unref?.();
  }

  private setupUnloadHandlers(): void {
    if (typeof window === "undefined") return;

    const handleVisibilityChange = () => {
      if (document.visibilityState === "hidden") {
        this.flush();
      }
    };

    const handleBeforeUnload = () => {
      this.flush();
    };

    window.addEventListener("visibilitychange", handleVisibilityChange);
    window.addEventListener("beforeunload", handleBeforeUnload);
    this.unloadHandlers = [
      ["visibilitychange", handleVisibilityChange],
      ["beforeunload", handleBeforeUnload],
    ];
  }

  /**
   * Record a review event. The client_event_id is minted once and stable.
   */
  recordEvent(
    itemId: string | null,
    eventType: ReviewEventType,
    payload: ReviewEventPayload = {},
    proofId?: string,
  ): string {
    const clientEventId = crypto.randomUUID();
    const event: PendingEvent = {
      item_id: itemId,
      proof_id: proofId,
      session_id: this.sessionId,
      event_type: eventType,
      occurred_at: new Date().toISOString(),
      client_event_id: clientEventId,
      payload,
    };

    // Track last watched position for seek blocking
    if (eventType === "video_play" && payload.video_position_ms !== undefined) {
      this.lastWatchedMs = Math.max(this.lastWatchedMs, payload.video_position_ms);
    }

    this.events.push(event);

    // Auto-flush if we hit the backend's max (200 per batch)
    if (this.events.length >= 200) {
      this.flush();
    }

    return clientEventId;
  }

  /**
   * Get the last watched position (for seek validation).
   */
  getLastWatchedMs(): number {
    return this.lastWatchedMs;
  }

  /**
   * Reset last watched position (e.g., on item switch).
   */
  resetLastWatched(): void {
    this.lastWatchedMs = 0;
  }

  /**
   * Flush pending events to the server. Never throws; failures are silent and are retried with the
   * same client_event_id on the next flush, which the server treats as an idempotent replay. See the
   * class docstring for the hard-window-close delivery boundary.
   */
  private async flush(): Promise<void> {
    if (this.events.length === 0) return;

    const eventsToSend = [...this.events];
    this.events = [];

    try {
      await this.postFn(eventsToSend);
    } catch (error: unknown) {
      // A rejection the server will never accept (forbidden/unauthorized/validation) must be DROPPED.
      // Re-queueing it grew the buffer forever behind an unsatisfiable retry — which is exactly what a
      // leadership principal browsing the queue produces now that ingest is verifier-only authority.
      if (error instanceof Error && error.message.startsWith("permanent:")) return;
      // Transient failure (network, restart): re-queue and retry with the same client_event_id, which
      // the server dedupes. Bounded so a long outage cannot grow the tab's memory without limit; the
      // OLDEST events are dropped because the newest carry the current review.
      this.events = eventsToSend.concat(this.events);
      if (this.events.length > MAX_BUFFERED_EVENTS) {
        this.events = this.events.slice(this.events.length - MAX_BUFFERED_EVENTS);
      }
    }
  }

  /**
   * Flush now (modal close, verdict submit, tab hidden). Does NOT stop the periodic timer: it used
   * to, which meant the first verdict permanently disabled auto-flush for the rest of that buffer's
   * life, so a verifier who reviewed a second item on the same buffer only ever delivered on an
   * explicit flush. Use dispose() to release resources.
   */
  async forceFlush(): Promise<void> {
    await this.flush();
  }

  /**
   * Release the timer and window listeners, flushing whatever is left. Call from the owning
   * component's effect cleanup.
   */
  async dispose(): Promise<void> {
    if (this.flushTimer) {
      clearInterval(this.flushTimer);
      this.flushTimer = null;
    }
    if (typeof window !== "undefined") {
      for (const [type, handler] of this.unloadHandlers) window.removeEventListener(type, handler);
    }
    this.unloadHandlers = [];
    await this.flush();
  }

  /**
   * Emit a verdict event and flush immediately.
   */
  async recordVerdict(itemId: string, decision: "approved" | "rejected"): Promise<void> {
    this.recordEvent(itemId, "verdict_recorded", { verdict: decision });
    await this.forceFlush();
  }
}

/**
 * Hook to create and manage a ReviewEventBuffer for a component.
 * Handles server-side posting through the provided Server Action.
 */
export function useReviewEventBuffer(
  postFn: (events: PendingEvent[]) => Promise<void>,
): ReviewEventBuffer {
  // useMemo creates once and keeps the same reference across renders
  const buffer = React.useMemo(() => new ReviewEventBuffer(postFn), [postFn]);

  // Cleanup on unmount
  React.useEffect(() => {
    return () => {
      buffer.forceFlush();
    };
  }, [buffer]);

  return buffer;
}
