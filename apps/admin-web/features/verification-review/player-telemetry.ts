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
}

/** A forward jump is allowed this far past the watched mark: ordinary playback/buffering jitter. */
export const SEEK_TOLERANCE_MS = 1500;
/** timeupdate ticks further ahead than this are a jump, not natural progress. Kept equal to the seek
 * tolerance so there is no band that the overshoot clamp refuses but the mark would still accept. */
export const PLAYBACK_TOLERANCE_MS = SEEK_TOLERANCE_MS;

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
   * Natural playback progress. Anything beyond the tolerance is a jump, handled by onSeeking.
   *
   * The mark advances only while PLAYING. Otherwise a verifier could creep it forward with repeated
   * paused seeks just inside the tolerance and reach the end without watching anything.
   */
  onTimeUpdate(video: MediaLike): void {
    const currentMs = ms(video.currentTime);
    this.lastTickMs = currentMs;
    if (video.paused === true) return;
    const advanced = currentMs > this.maxWatchedMs;
    const withinPlaybackTolerance = currentMs - this.maxWatchedMs <= PLAYBACK_TOLERANCE_MS;
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
    if (landedMs <= this.maxWatchedMs + SEEK_TOLERANCE_MS) return null;
    return this.maxWatchedMs;
  }

  /**
   * Returns the position the element should be forced back to, or null when the seek is allowed.
   * Rewinding and rewatching are always allowed; only skipping past unwatched video is refused.
   */
  onSeeking(video: MediaLike): number | null {
    const targetMs = ms(video.currentTime);
    if (targetMs <= this.maxWatchedMs + SEEK_TOLERANCE_MS) return null;
    this.sink.record("video_seek_attempt", {
      seek_from_ms: this.lastTickMs,
      seek_to_ms: targetMs,
      video_position_ms: this.maxWatchedMs,
      video_duration_ms: ms(video.duration),
    });
    return this.maxWatchedMs;
  }
}
