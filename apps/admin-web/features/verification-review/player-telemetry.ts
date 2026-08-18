/**
 * Player watch-telemetry, extracted from the React component so it can be tested without a browser.
 *
 * WHY EXTRACTED: the emit/seek-guard logic used to live inside ReviewVideoPlayer's closure, which
 * meant the only way to check it was to actually decode a video in a real browser. A preview browser
 * that plays video inconsistently could not prove `video_pause`, `video_ended` or
 * `video_seek_attempt` ever fire, so "does the verifier's watch get recorded" stayed unverifiable.
 * Everything here takes a minimal media-element shape, so a fake element in a unit test drives the
 * exact code the browser drives.
 */
export type WatchEventType =
  | "video_play"
  | "video_pause"
  | "video_ended"
  | "video_seek_attempt";

export interface WatchTelemetrySink {
  record(
    eventType: WatchEventType,
    payload: {
      video_position_ms?: number;
      video_duration_ms?: number;
      seek_from_ms?: number;
      seek_to_ms?: number;
    },
  ): void;
}

/** The slice of HTMLMediaElement this logic needs. */
export interface MediaLike {
  currentTime: number;
  duration: number;
  /** The mark only advances while actually playing — see onTimeUpdate. Optional so a paused-state
   * omission is treated as playing rather than silently freezing the mark. */
  paused?: boolean;
  /** Playback speed multiplier (1 = normal, 2 = the verifier's double-speed control). At 2x every
   * real tick covers twice the media time, so the playback tolerance scales with it — otherwise the
   * mark freezes on the first slow tick and the clamp starts dragging the clip backwards. Optional
   * so an omission means normal speed. */
  playbackRate?: number;
}

// There is deliberately NO seek tolerance any more. A 1500ms "jitter" allowance on the seek path
// (SEEK_TOLERANCE_MS, removed 2026-08-18) was a demonstrated ratchet: paced +0.6s hops — each inside
// the allowance, each legalized by the next playback tick advancing the mark — walked a proof from
// 1.96s to 22.75s in 12.6 wall-seconds, with not one video_seek_attempt logged. Measured in-browser
// against this exact component. A user-initiated forward seek past the watched mark is now ALWAYS
// refused and logged, at any offset; jitter tolerance survives only where jitter actually occurs —
// on the playback-tick path (PLAYBACK_TOLERANCE_MS below), which no seek can reach because every
// seek fires `seeking` first and flags itself.
/** A `timeupdate` further ahead than this did not play (at NORMAL speed — the effective tolerance is
 * scaled by [MediaLike.playbackRate], since a 2x tick legitimately covers twice the media time).
 * Real ticks are ~250ms apart; this leaves room for a stalled tab or a slow frame without leaving
 * room for a jump.
 *
 * INVARIANT (the 2026-08-18 pinned-at-0 regression): there must be NO band the overshoot clamp
 * refuses but the mark would still accept. When the cold seek allowance became zero, the clamp —
 * which used to rely on `seekAllowanceMs >= PLAYBACK_TOLERANCE_MS` — started refusing the very first
 * natural playback tick (position ~250ms > mark 0 + allowance 0) and yanked every fresh clip back to
 * the start on every tick, forever: "videos are not playing, pausing, going back". The clamp now
 * grants the playback band explicitly while the element is genuinely playing with no seek in flight
 * (see overshootBeyondWatched), so that invariant no longer depends on the two constants' order. */
export const PLAYBACK_TOLERANCE_MS = 1000;

/** The fastest rate the UI ever offers is the 2x button, so the tolerance never scales past 2.
 * Without this cap, a console-set `video.playbackRate = 16` made every 16x tick look like natural
 * playback — the mark raced to the end and a whole proof was "watched" in a sixteenth of its
 * runtime, measured in-browser 2026-08-18. A rate the product never offers is not playback. */
export const MAX_PLAYBACK_TOLERANCE_SCALE = 2;

/** Effective playback tolerance for this element: 2x playback covers 2x media time per tick, so the
 * scale follows the rate, capped at [MAX_PLAYBACK_TOLERANCE_SCALE]. Never scales DOWN below normal
 * (a 0.5x rate still gets the full tick allowance — a slow rate does not make real ticks arrive
 * closer together in media time than the jitter the tolerance absorbs). */
function playbackToleranceMs(video: MediaLike): number {
  const rate = video.playbackRate;
  const scale =
    typeof rate === "number" && Number.isFinite(rate) && rate > 1
      ? Math.min(rate, MAX_PLAYBACK_TOLERANCE_SCALE)
      : 1;
  return PLAYBACK_TOLERANCE_MS * scale;
}

function ms(seconds: number): number {
  return Number.isFinite(seconds) ? Math.round(seconds * 1000) : 0;
}

/**
 * Tracks how much of a proof video was actually watched and blocks skipping ahead.
 *
 * The watched mark advances ONLY on natural playback progress. An earlier version reverted seeks to
 * a mark that advanced only when a `video_play` event was emitted, so it stayed at 0 through a
 * normal watch and pinned the video at position 0 forever — rewatch that bug in the unit tests.
 */
export class WatchTracker {
  private maxWatchedMs = 0;
  private lastTickMs = 0;
  // Set by EVERY seek, allowed or refused, and cleared by the next progress tick. The mark must never
  // advance on a position the verifier JUMPED to — only on video that actually played through.
  private seekPending = false;
  // Explicit field + assignment rather than a constructor parameter property: the repo runs these
  // tests through Node's strip-only type removal, which rejects parameter properties.
  private readonly sink: WatchTelemetrySink;

  constructor(sink: WatchTelemetrySink) {
    this.sink = sink;
  }

  get watchedMs(): number {
    return this.maxWatchedMs;
  }

  onPlay(video: MediaLike): void {
    this.sink.record("video_play", {
      video_position_ms: ms(video.currentTime),
      video_duration_ms: ms(video.duration),
    });
  }

  onPause(video: MediaLike): void {
    this.sink.record("video_pause", {
      video_position_ms: ms(video.currentTime),
      video_duration_ms: ms(video.duration),
    });
  }

  onEnded(video: MediaLike): void {
    // Reaching the end means the whole clip was watched, whatever the tick granularity was.
    this.maxWatchedMs = Math.max(this.maxWatchedMs, ms(video.duration));
    this.sink.record("video_ended", {
      video_position_ms: ms(video.currentTime),
      video_duration_ms: ms(video.duration),
    });
  }

  /**
   * Natural playback progress. A jump is never progress.
   *
   * Three conditions must all hold before the mark advances, and each one closes a real bypass:
   *  - the element is PLAYING — otherwise repeated paused seeks inside the tolerance creep the mark
   *    to the end of a long proof having watched a second of it;
   *  - no seek is pending — `seeked` fires this same handler, so without this a loop of
   *    `currentTime += 1.4s` (each hop small enough that onSeeking allows it and records nothing)
   *    walked the mark through a 10-minute video in well under a second, with no frame decoded and no
   *    `video_seek_attempt` ever logged. That was a WORKING bypass of the whole feature;
   *  - the step is one tick's worth of video — a real `timeupdate` fires every ~250ms, so anything
   *    beyond PLAYBACK_TOLERANCE_MS did not play, it skipped.
   */
  onTimeUpdate(video: MediaLike): void {
    const currentMs = ms(video.currentTime);
    const hadSeekPending = this.seekPending;
    this.seekPending = false;
    this.lastTickMs = currentMs;
    if (video.paused === true) return;
    if (hadSeekPending) return;
    const advanced = currentMs > this.maxWatchedMs;
    const withinPlaybackTolerance = currentMs - this.maxWatchedMs <= playbackToleranceMs(video);
    if (advanced && withinPlaybackTolerance) this.maxWatchedMs = currentMs;
  }

  /**
   * Position the element must be clamped back to when it is sitting past what has been watched, or
   * null when the position is fine. Records nothing — `onSeeking` already logged the attempt.
   *
   * Why this exists, and why callers must poll it: assigning `currentTime` inside the `seeking`
   * handler is overridden when the in-flight seek completes, so a skip STOOD in a real browser even
   * though the attempt was logged. Checking only on `seeked` fixed it intermittently — that is one
   * event that may be missed or reordered. So the guard is LEVEL-triggered: the caller asks on every
   * progress tick as well, and any overshoot is rolled back within a tick regardless of which media
   * event surfaced it.
   */
  overshootBeyondWatched(video: MediaLike): number | null {
    const landedMs = ms(video.currentTime);
    // Three bands, strictest condition first:
    //
    //  - A SEEK IN FLIGHT gets no allowance at all: any position past the mark while seekPending is
    //    an in-flight jump the revert has not caught yet, and letting it stand until the next tick
    //    was half of the paced-hop ratchet. Clamp it to the mark immediately.
    //  - Genuinely PLAYING with no seek in flight gets the playback-tick band: the position
    //    legitimately runs up to one tick's worth of media ahead of the mark (the mark only catches
    //    up when the next `timeupdate` lands). The clamp must never refuse that band, or it fights
    //    natural playback — the 2026-08-18 pinned-at-0 regression, where every fresh clip's first
    //    tick (position ~250ms > mark 0 + allowance 0) was yanked back to 0 forever. No seek can
    //    hide in this band, because every seek fires `seeking` first and flags itself.
    //  - PAUSED with no seek in flight can trail honest playback by at most a tick (pausing lands
    //    within a tick of the last advance), so it gets one NORMAL tick of allowance once something
    //    has been watched — and ZERO before then, preserving the WebKit cold-click rollback
    //    (2026-08-17): a cold position past 0 that somehow arrived without a seeking event is still
    //    an overshoot.
    const allowanceMs = this.seekPending
      ? 0
      : video.paused !== true
        ? playbackToleranceMs(video)
        : this.maxWatchedMs > 0
          ? PLAYBACK_TOLERANCE_MS
          : 0;
    if (landedMs <= this.maxWatchedMs + allowanceMs) return null;
    return this.maxWatchedMs;
  }

  /**
   * Returns the position the element should be forced back to, or null when the seek is allowed.
   * Rewinding and rewatching are always allowed; ANY seek past the watched mark is refused and
   * logged, at any offset.
   *
   * There is deliberately no forward allowance here (the 1500ms one was removed 2026-08-18): paced
   * sub-tolerance hops ratcheted through a whole proof — each hop individually allowed, each
   * legalized by the next playback tick — gaining ~65% over honest watching, with nothing logged.
   * Jitter tolerance lives only on the playback-tick path, which a seek can never reach.
   */
  onSeeking(video: MediaLike): number | null {
    const targetMs = ms(video.currentTime);
    // Flagged for EVERY seek, including an allowed rewind: the following tick must not be mistaken
    // for playback progress.
    this.seekPending = true;
    if (targetMs <= this.maxWatchedMs) return null;
    this.sink.record("video_seek_attempt", {
      seek_from_ms: this.lastTickMs,
      seek_to_ms: targetMs,
      video_position_ms: this.maxWatchedMs,
      video_duration_ms: ms(video.duration),
    });
    return this.maxWatchedMs;
  }
}
