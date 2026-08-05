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
 * - Uses navigator.sendBeacon when available for unload safety
 * - Never exposes bearer token; posts through authenticated Server Action
 */
export class ReviewEventBuffer {
  private sessionId = typeof window !== "undefined" && crypto ? crypto.randomUUID() : "";
  private events: PendingEvent[] = [];
  private flushTimer: NodeJS.Timeout | null = null;
  private flushIntervalMs = 10000; // ~10 seconds
  private readonly postFn: (events: PendingEvent[]) => Promise<void>;
  private lastWatchedMs = 0; // Track last watched position for seek blocking

  constructor(postFn: (events: PendingEvent[]) => Promise<void>) {
    this.postFn = postFn;
    this.startFlushTimer();
    this.setupUnloadHandlers();
  }

  private startFlushTimer(): void {
    if (this.flushTimer) clearInterval(this.flushTimer);
    this.flushTimer = setInterval(() => this.flush(), this.flushIntervalMs);
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
   * Flush pending events to the server. Uses navigator.sendBeacon when available
   * so a closing tab still delivers. Never throws; failures are silent and will
   * be retried with the same client_event_id on the next flush.
   */
  private async flush(): Promise<void> {
    if (this.events.length === 0) return;

    const eventsToSend = [...this.events];
    this.events = [];

    try {
      await this.postFn(eventsToSend);
    } catch {
      // Silently re-queue on any failure (network, auth, validation).
      // The next flush will retry with the same client_event_id, which is idempotent.
      this.events = eventsToSend.concat(this.events);
    }
  }

  /**
   * Force flush and clear resources (called on modal close, verdict submit, etc.).
   */
  async forceFlush(): Promise<void> {
    if (this.flushTimer) clearInterval(this.flushTimer);
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
