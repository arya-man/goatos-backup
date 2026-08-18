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

/**
 * A forward jump is allowed this far past the watched mark: ordinary playback/buffering jitter.
 *
 * It applies ONLY once something has actually been watched — see seekAllowanceMs. The allowance is
 * absolute, so on a short clip it is proportionally enormous: 1500ms is 0.8% of a three-minute proof
 * but 12.7% of an 11.8s one. Measured in WebKit, a cold scrubber click at 85% of an 11.8s clip
 * landed at 1.49s — a verifier who had watched nothing was 1.5 seconds in, which reads (correctly)
 * as "I can skip ahead". Chromium happened to clamp the same click to 0; the rule must not depend on
 * which engine she opens.
 */
export const SEEK_TOLERANCE_MS = 1500;
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

/** Effective playback tolerance for this element: 2x playback covers 2x media time per tick. Never
 * scales DOWN below normal (a 0.5x rate still gets the full tick allowance — a slow rate does not
 * make real ticks arrive closer together in media time than the jitter the tolerance absorbs). */
function playbackToleranceMs(video: MediaLike): number {
  const rate = video.playbackRate;
  const scale = typeof rate === "number" && Number.isFinite(rate) && rate > 1 ? rate : 1;
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

  /**
   * How far past the watched mark a forward seek may land.
   *
   * ZERO until something has actually been watched. The tolerance is there to absorb playback and
   * buffering jitter, and there is no jitter to absorb before a single frame has played — so a cold
   * jump gets no allowance at all and is rolled back to the start. Once real progress exists the
   * full tolerance applies, exactly as before, so a mid-watch nudge is not fought.
   *
   * maxWatchedMs is the right signal rather than a separate "has played" flag: it advances only on
   * natural playback progress (onTimeUpdate refuses to advance it on a seek), so it is already the
   * authoritative answer to "has any of this clip actually been watched".
   */
  private seekAllowanceMs(): number {
    return this.maxWatchedMs > 0 ? SEEK_TOLERANCE_MS : 0;
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
    // While the element is genuinely PLAYING with no seek in flight, the position legitimately runs
    // up to one tick's worth of media ahead of the mark (the mark only catches up when the next
    // `timeupdate` lands). The clamp must never refuse that band, or it fights natural playback —
    // the 2026-08-18 regression: with the cold seek allowance at zero, the very first playback tick
    // of every fresh clip (position ~250ms > mark 0 + allowance 0) was yanked back to 0, on every
    // tick and every 250ms poll, so no proof video could play at all.
    //
    // This band is NOT a skip loophole: any seek — scrubber, keyboard, or programmatic
    // currentTime assignment — fires `seeking` first, which sets seekPending, and the strict
    // allowance below applies until a natural tick clears that flag. A paused element gets the
    // strict allowance too, so the WebKit cold-click rollback (2026-08-17) is unchanged.
    const allowanceMs =
      video.paused !== true && !this.seekPending
        ? Math.max(this.seekAllowanceMs(), playbackToleranceMs(video))
        : this.seekAllowanceMs();
    if (landedMs <= this.maxWatchedMs + allowanceMs) return null;
    return this.maxWatchedMs;
  }

  /**
   * Returns the position the element should be forced back to, or null when the seek is allowed.
   * Rewinding and rewatching are always allowed; only skipping past unwatched video is refused.
   */
  onSeeking(video: MediaLike): number | null {
    const targetMs = ms(video.currentTime);
    // Flagged for EVERY seek, including one small enough to allow: the following tick must not be
    // mistaken for playback progress.
    this.seekPending = true;
    if (targetMs <= this.maxWatchedMs + this.seekAllowanceMs()) return null;
    this.sink.record("video_seek_attempt", {
      seek_from_ms: this.lastTickMs,
      seek_to_ms: targetMs,
      video_position_ms: this.maxWatchedMs,
      video_duration_ms: ms(video.duration),
    });
    return this.maxWatchedMs;
  }
}
