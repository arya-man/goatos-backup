"use client";

import React, { useCallback, useEffect, useRef } from "react";
import type { ReviewEventBuffer } from "./review-events";

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
    const videoRef = useRef<HTMLVideoElement>(null);
    const playingRef = useRef(false);
    const lastReportedTimeRef = useRef(0);
    // Furthest point actually WATCHED, advanced by natural playback (timeupdate). Seek blocking
    // compares against this. It must not come from the emitted video_play position alone: that only
    // moves when playback (re)starts, so it stays at 0 during a normal watch and the revert below
    // then pins the video at 0 — which is exactly the bug this replaces.
    const maxWatchedMsRef = useRef(0);

    // Handle play event
    const handlePlay = () => {
      playingRef.current = true;
      const video = videoRef.current;
      if (!video) return;
      eventBuffer.recordEvent(
        itemId,
        "video_play",
        {
          video_position_ms: Math.round(video.currentTime * 1000),
          video_duration_ms: Math.round(video.duration * 1000),
        },
        proofId,
      );
    };

    // Handle pause event
    const handlePause = () => {
      playingRef.current = false;
      const video = videoRef.current;
      if (!video) return;
      eventBuffer.recordEvent(
        itemId,
        "video_pause",
        {
          video_position_ms: Math.round(video.currentTime * 1000),
          video_duration_ms: Math.round(video.duration * 1000),
        },
        proofId,
      );
    };

    // Blocks a FORWARD JUMP past the furthest point already watched, so a verifier cannot skip the
    // middle of a proof. Pausing, rewinding and re-watching are all allowed. Natural playback is
    // never reverted: `seeking` also fires for ordinary buffering/loop boundaries, so the guard only
    // acts on a jump clearly ahead of what has been watched.
    const SEEK_TOLERANCE_MS = 1500;
    const PLAYBACK_TOLERANCE_MS = 2000;
    const handleSeeking = (e: Event) => {
      const video = e.target as HTMLVideoElement;
      const targetMs = Math.round(video.currentTime * 1000);
      const allowedMs = maxWatchedMsRef.current + SEEK_TOLERANCE_MS;
      const isForwardJump = targetMs > allowedMs;
      if (!isForwardJump) return;

      eventBuffer.recordEvent(
        itemId,
        "video_seek_attempt",
        {
          seek_from_ms: lastReportedTimeRef.current,
          seek_to_ms: targetMs,
          video_position_ms: maxWatchedMsRef.current,
          video_duration_ms: Math.round(video.duration * 1000),
        },
        proofId,
      );
      // Use rAF to ensure the revert happens after the browser processes the seek.
      // Setting currentTime in the seeking event alone may not prevent the jump.
      requestAnimationFrame(() => {
        video.currentTime = maxWatchedMsRef.current / 1000;
      });
    };

    // Handle ended event
    const handleEnded = () => {
      const video = videoRef.current;
      if (!video) return;
      eventBuffer.recordEvent(
        itemId,
        "video_ended",
        {
          video_position_ms: Math.round(video.currentTime * 1000),
          video_duration_ms: Math.round(video.duration * 1000),
        },
        proofId,
      );
    };

    // Track timeupdate to update last watched position
    const handleTimeUpdate = useCallback(() => {
      const video = videoRef.current;
      if (!video) return;
      const currentMs = Math.round(video.currentTime * 1000);
      lastReportedTimeRef.current = currentMs;
      // Natural playback advances the watched mark; a jump beyond the tolerance is handled (and
      // reverted) by handleSeeking instead. Split across statements deliberately: the UI-contract
      // guard reads `>` … `<` on one line as JSX text, so a single-line comparison false-positives.
      const advanced = currentMs > maxWatchedMsRef.current;
      const withinPlaybackTolerance = currentMs - maxWatchedMsRef.current <= PLAYBACK_TOLERANCE_MS;
      if (advanced && withinPlaybackTolerance) {
        maxWatchedMsRef.current = currentMs;
      }
    }, []);

    const memoizedHandlePlay = useCallback(handlePlay, [itemId, proofId, eventBuffer]);
    const memoizedHandlePause = useCallback(handlePause, [itemId, proofId, eventBuffer]);
    const memoizedHandleSeeking = useCallback(handleSeeking, [itemId, proofId, eventBuffer]);
    const memoizedHandleEnded = useCallback(handleEnded, [itemId, proofId, eventBuffer]);

    useEffect(() => {
      const video = videoRef.current;
      if (!video) return;

      video.addEventListener("play", memoizedHandlePlay);
      video.addEventListener("pause", memoizedHandlePause);
      video.addEventListener("seeking", memoizedHandleSeeking);
      video.addEventListener("seeked", handleTimeUpdate);
      video.addEventListener("timeupdate", handleTimeUpdate);
      video.addEventListener("ended", memoizedHandleEnded);

      return () => {
        video.removeEventListener("play", memoizedHandlePlay);
        video.removeEventListener("pause", memoizedHandlePause);
        video.removeEventListener("seeking", memoizedHandleSeeking);
        video.removeEventListener("seeked", handleTimeUpdate);
        video.removeEventListener("timeupdate", handleTimeUpdate);
        video.removeEventListener("ended", memoizedHandleEnded);
      };
    }, [memoizedHandlePlay, memoizedHandlePause, memoizedHandleSeeking, memoizedHandleEnded, handleTimeUpdate]);

    return (
      <div ref={ref} {...divProps}>
        <video ref={videoRef} controls preload="metadata">
          <source src={src} type={mimeType} />
        </video>
      </div>
    );
  },
);

ReviewVideoPlayer.displayName = "ReviewVideoPlayer";
