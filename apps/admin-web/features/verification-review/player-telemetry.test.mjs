import assert from "node:assert/strict";
import test from "node:test";

const { WatchTracker, SEEK_TOLERANCE_MS } = await import("./player-telemetry.ts");

function sink() {
  const events = [];
  return { events, record: (eventType, payload) => events.push({ eventType, payload }) };
}
const media = (currentTime, duration = 3) => ({ currentTime, duration });

test("play, pause and ended each emit their event with position and duration", () => {
  const s = sink();
  const t = new WatchTracker(s);
  t.onPlay(media(0));
  t.onTimeUpdate(media(1.2));
  t.onPause(media(1.2));
  t.onTimeUpdate(media(3));
  t.onEnded(media(3));
  assert.deepEqual(
    s.events.map((e) => e.eventType),
    ["video_play", "video_pause", "video_ended"],
  );
  assert.equal(s.events[1].payload.video_position_ms, 1200);
  assert.equal(s.events[1].payload.video_duration_ms, 3000);
});

test("natural playback advances the watched mark — the regression that pinned playback at 0", () => {
  const s = sink();
  const t = new WatchTracker(s);
  // The old implementation only advanced its mark when a video_play event fired, so the mark stayed
  // at 0 during a normal watch and every progression got reverted to 0.
  t.onPlay(media(0));
  for (const seconds of [0.25, 0.5, 0.75, 1.0]) t.onTimeUpdate(media(seconds));
  assert.equal(t.watchedMs, 1000);
  assert.equal(t.onSeeking(media(1.0)), null, "playing on past the watched mark must not be reverted");
});

test("a forward jump past unwatched video is refused and logged", () => {
  const s = sink();
  const t = new WatchTracker(s);
  t.onTimeUpdate(media(0.9));
  const revertTo = t.onSeeking(media(2.9));
  assert.equal(revertTo, 900, "must be forced back to the furthest point actually watched");
  const attempt = s.events.at(-1);
  assert.equal(attempt.eventType, "video_seek_attempt");
  assert.equal(attempt.payload.seek_to_ms, 2900, "records what she TRIED to skip to");
  assert.equal(attempt.payload.video_position_ms, 900, "and where she was put back");
});

test("rewinding and rewatching are always allowed", () => {
  const s = sink();
  const t = new WatchTracker(s);
  for (const seconds of [0.5, 1.0, 1.5, 2.0]) t.onTimeUpdate(media(seconds));
  assert.equal(t.onSeeking(media(0.2)), null);
  assert.equal(t.onSeeking(media(1.0)), null);
  assert.equal(
    s.events.filter((e) => e.eventType === "video_seek_attempt").length,
    0,
    "a rewind is not a skip and must not be logged as one",
  );
});

test("a jump inside the tolerance is treated as playback jitter, not a skip", () => {
  const s = sink();
  const t = new WatchTracker(s);
  t.onTimeUpdate(media(1.0));
  assert.equal(t.onSeeking(media(1.0 + (SEEK_TOLERANCE_MS - 100) / 1000)), null);
  assert.equal(s.events.length, 0);
});

test("reaching the end counts the whole clip as watched", () => {
  const s = sink();
  const t = new WatchTracker(s);
  t.onTimeUpdate(media(2.4));
  t.onEnded(media(3));
  assert.equal(t.watchedMs, 3000, "so a completed watch is never scored short by tick granularity");
});

test("a NaN duration (metadata not loaded) never produces NaN telemetry", () => {
  const s = sink();
  const t = new WatchTracker(s);
  t.onPlay({ currentTime: 0, duration: Number.NaN });
  assert.equal(s.events[0].payload.video_duration_ms, 0);
  assert.ok(!Number.isNaN(s.events[0].payload.video_duration_ms));
});

test("a skip that LANDS past unwatched video is clamped back after the seek completes", () => {
  const s = sink();
  const t = new WatchTracker(s);
  t.onTimeUpdate(media(0.8));
  t.onSeeking(media(2.95)); // logged the attempt
  // The browser then completes the seek, overriding whatever was assigned mid-`seeking`. This is the
  // authoritative check, and it is what made the block real in a browser rather than only logged.
  assert.equal(t.overshootBeyondWatched(media(2.95)), 800);
  // Nothing extra is logged by the clamp itself.
  assert.equal(s.events.filter((e) => e.eventType === "video_seek_attempt").length, 1);
});

test("a landed rewind, and natural playback, are never clamped", () => {
  const t = new WatchTracker(sink());
  for (const seconds of [0.5, 1.0, 1.5]) t.onTimeUpdate(media(seconds));
  assert.equal(t.overshootBeyondWatched(media(0.4)), null, "rewatching is allowed");
  assert.equal(t.overshootBeyondWatched(media(1.6)), null, "ordinary playback progress is not a skip");
});

test("repeated paused seeks just inside the tolerance cannot creep the watched mark forward", () => {
  const t = new WatchTracker(sink());
  const paused = (seconds) => ({ currentTime: seconds, duration: 30, paused: true });
  t.onTimeUpdate({ currentTime: 1.0, duration: 30, paused: false }); // watched 1s for real
  // Without the paused check each of these advanced the mark by ~1.4s, so a verifier reached the end
  // of a long proof in a handful of clicks having watched one second of it.
  for (const seconds of [2.4, 3.8, 5.2, 6.6]) t.onTimeUpdate(paused(seconds));
  assert.equal(t.watchedMs, 1000, "the mark must still reflect only what actually played");
  assert.equal(t.overshootBeyondWatched(paused(6.6)), 1000, "and the position is rolled back");
});
