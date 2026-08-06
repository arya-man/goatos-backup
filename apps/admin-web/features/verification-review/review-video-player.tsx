"use client";

import React, { useEffect, useMemo, useState } from "react";
import type { ReviewEventBuffer } from "./review-events";
import { WatchTracker } from "./player-telemetry";

interface ReviewVideoPlayerProps {
  src: string;
  mimeType: string;
  proofId?: string;
  itemId: string;
  eventBuffer: ReviewEventBuffer;
}

/**
 * Custom video player that emits review events and blocks forward seeking.
 *
 * - Emits video_play, video_pause, video_seek_attempt, video_ended on relevant events
 * - Blocks forward seeking (jumps past unwatched video)
 * - Tracks last watched position and reverts forward seeks
 * - Reports seek_attempt events for all attempts
 */
export const ReviewVideoPlayer = React.forwardRef<
  HTMLDivElement,
  ReviewVideoPlayerProps & React.HTMLAttributes<HTMLDivElement>
>(
  (
    { src, mimeType, proofId, itemId, eventBuffer, ...divProps },
    ref,
  ) => {
    // The element is held in STATE, not a ref, so the listener effect re-runs whenever React mounts a
    // different <video> instance (switching proofs changes `src` and can replace the element). With a
    // ref + an effect keyed only on the tracker, the listeners bound to the discarded element and the
    // new one recorded nothing and could not block a skip — proven in-browser: a forward jump past
    // unwatched video stood, and no video_seek_attempt row was written.
    const [videoEl, setVideoEl] = useState<HTMLVideoElement | null>(null);

    // All watch/seek logic lives in WatchTracker (player-telemetry.ts) so it is unit-testable
    // without a browser; this component only wires DOM events to it and applies the revert it asks
    // for. Do not reintroduce the emit/tolerance arithmetic here — the tests would stop guarding it.
    const tracker = useMemo(
      () =>
        new WatchTracker({
          record: (eventType, payload) => eventBuffer.recordEvent(itemId, eventType, payload, proofId),
        }),
      [itemId, proofId, eventBuffer],
    );

    useEffect(() => {
      const video = videoEl;
      if (!video) return;

      const onPlay = () => tracker.onPlay(video);
      const onPause = () => tracker.onPause(video);
      const onEnded = () => tracker.onEnded(video);
      // Runs on BOTH `timeupdate` and `seeked`. A skip is stopped here rather than in the `seeking`
      // handler, because the assignment made mid-seek is overridden once the in-flight seek
      // completes. Checking on `seeked` alone worked only intermittently, so this is level-triggered:
      // whenever the position is observed past what was watched, it is rolled back.
      const onProgress = () => {
        const clampToMs = tracker.overshootBeyondWatched(video);
        if (clampToMs === null) {
          tracker.onTimeUpdate(video);
          return;
        }
        video.currentTime = clampToMs / 1000;
      };
      const onSeeking = () => {
        const revertToMs = tracker.onSeeking(video);
        if (revertToMs === null) return;
        const revertToSeconds = revertToMs / 1000;
        // Assign immediately AND again on a macrotask. Immediate alone does not reliably stop the
        // jump (the browser is mid-seek). A requestAnimationFrame retry was the first attempt and
        // FAILED an in-browser proof: rAF is suspended while the tab is not visible, so the skip
        // stood. setTimeout still fires in a hidden tab, so the block cannot be defeated by
        // backgrounding the tab.
        video.currentTime = revertToSeconds;
        setTimeout(() => {
          if (video.currentTime > revertToSeconds) video.currentTime = revertToSeconds;
        }, 0);
      };

      // Media events alone were NOT enough, proven in-browser three runs in a row: a skip while PAUSED
      // emits `seeking`/`seeked` (the attempt was logged every time) but the browser kept the new
      // position, and `timeupdate` does not tick while paused, so nothing rolled it back. This poll
      // makes the block independent of which events fire at all — an overshoot cannot survive a tick.
      const clampTimer = setInterval(() => {
        const clampToMs = tracker.overshootBeyondWatched(video);
        if (clampToMs !== null) video.currentTime = clampToMs / 1000;
      }, 250);

      video.addEventListener("play", onPlay);
      video.addEventListener("pause", onPause);
      video.addEventListener("seeking", onSeeking);
      video.addEventListener("seeked", onProgress);
      video.addEventListener("timeupdate", onProgress);
      video.addEventListener("ended", onEnded);

      return () => {
        clearInterval(clampTimer);
        video.removeEventListener("play", onPlay);
        video.removeEventListener("pause", onPause);
        video.removeEventListener("seeking", onSeeking);
        video.removeEventListener("seeked", onProgress);
        video.removeEventListener("timeupdate", onProgress);
        video.removeEventListener("ended", onEnded);
      };
    }, [tracker, videoEl]);

    return (
      <div ref={ref} {...divProps}>
        <video ref={setVideoEl} controls preload="metadata">
          <source src={src} type={mimeType} />
        </video>
      </div>
    );
  },
);

ReviewVideoPlayer.displayName = "ReviewVideoPlayer";
