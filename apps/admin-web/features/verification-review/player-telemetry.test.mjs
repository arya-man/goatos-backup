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

test("the sub-tolerance seek loop bypass: many small forward jumps cannot walk the mark forward", () => {
  const s = sink();
  const t = new WatchTracker(s);
  const playing = (seconds) => ({ currentTime: seconds, duration: 600, paused: false });
  // One second genuinely played.
  t.onTimeUpdate(playing(0.5));
  t.onTimeUpdate(playing(1.0));
  assert.equal(t.watchedMs, 1000);
  // Now the attack: jump repeatedly by just under the seek tolerance. Each hop is individually
  // allowed and records nothing, and `seeked` runs the same progress handler — which used to accept
  // the landing position as playback and march the mark through the whole video.
  let at = 1.0;
  for (let i = 0; i < 200; i++) {
    at += 1.4;
    t.onSeeking(playing(at));    // allowed, not logged
    t.onTimeUpdate(playing(at)); // `seeked` -> progress handler
  }
  assert.equal(t.watchedMs, 1000, "a 10-minute proof must not be 'watched' by seeking through it");
  assert.equal(t.overshootBeyondWatched(playing(at)), 1000, "and the position is pulled back");
});

test("a genuine playback tick right after an allowed rewind still advances once playing resumes", () => {
  const t = new WatchTracker(sink());
  const playing = (seconds) => ({ currentTime: seconds, duration: 60, paused: false });
  for (const seconds of [0.5, 1.0, 1.5, 2.0]) t.onTimeUpdate(playing(seconds)); // watched 2s for real
  assert.equal(t.watchedMs, 2000);
  t.onSeeking(playing(0.5));      // rewind — allowed
  t.onTimeUpdate(playing(0.5));   // the seeked tick: consumed, not progress
  t.onTimeUpdate(playing(0.75));  // real playback resumes
  assert.equal(t.watchedMs, 2000, "rewatching does not lower the mark");
  t.onTimeUpdate(playing(2.25));  // one tick's worth past the old mark
  assert.equal(t.watchedMs, 2250, "so ordinary playback still advances it after a rewind");
  t.onTimeUpdate(playing(9.0));   // a >1s leap is not a tick
  assert.equal(t.watchedMs, 2250, "and a leap dressed as a tick is still refused");
});

// A COLD forward seek gets NO tolerance at all.
//
// Regression, measured in WebKit 2026-08-17: a scrubber click at 85% of an 11.8s proof landed at
// 1.49s -- inside the 1500ms tolerance, so nothing rolled it back, and a verifier who had watched
// nothing was a second and a half in. Chromium clamped the identical click to 0, so the rule
// silently depended on the engine she opened. The tolerance absorbs playback/buffering jitter, and
// there is no jitter before a frame has played.
//
// It matters most on exactly the clips this product records: 1500ms is 0.8% of a three-minute proof
// but 12.7% of an 11.8s one.
test("a forward seek before anything is watched gets no tolerance and is refused", () => {
  const events = [];
  const tracker = new WatchTracker({ record: (type, payload) => events.push({ type, payload }) });
  const video = { currentTime: 1.4, duration: 11.79, paused: true };

  // 1.4s is INSIDE the absolute tolerance and used to be allowed.
  assert.equal(tracker.onSeeking(video), 0, "a cold forward seek must be reverted to the start");
  assert.equal(
    events.filter((e) => e.type === "video_seek_attempt").length,
    1,
    "and it must be recorded as a seek attempt, not silently permitted",
  );
  assert.equal(
    tracker.overshootBeyondWatched(video),
    0,
    "the level-triggered clamp must also treat a cold forward position as an overshoot",
  );
});

// THE 2026-08-18 REGRESSION: no proof video could play at all on admin-web.
//
// When the cold seek allowance became zero (2ec889ce2), the level-triggered clamp — which runs on
// every `timeupdate` AND on a 250ms poll — started refusing the very first natural playback tick of
// every fresh clip: position ~250ms > mark 0 + allowance 0, so the element was yanked back to 0, the
// mark never advanced, and the clip looped "play a fraction of a second, snap back to the start"
// forever. The verifier reported it as "videos are not playing — pausing, going back".
test("cold natural playback is NEVER clamped — the regression that pinned every clip at 0", () => {
  const t = new WatchTracker(sink());
  const playing = (seconds) => ({ currentTime: seconds, duration: 30, paused: false });
  // The 250ms poll can fire BEFORE the first timeupdate has advanced the mark.
  assert.equal(t.overshootBeyondWatched(playing(0.25)), null, "the poll must not fight cold playback");
  for (const seconds of [0.25, 0.5, 0.75, 1.0]) {
    assert.equal(
      t.overshootBeyondWatched(playing(seconds)),
      null,
      `natural playback at ${seconds}s must not be clamped`,
    );
    t.onTimeUpdate(playing(seconds));
  }
  assert.equal(t.watchedMs, 1000, "and the mark advances normally through the watch");
});

test("a cold forward SEEK is still refused even while playing", () => {
  const t = new WatchTracker(sink());
  const playing = (seconds) => ({ currentTime: seconds, duration: 30, paused: false });
  // Any seek fires `seeking` first, so seekPending guards the clamp until a natural tick lands.
  assert.equal(t.onSeeking(playing(9)), 0, "a cold skip is rolled back to the start");
  assert.equal(
    t.overshootBeyondWatched(playing(9)),
    0,
    "and the clamp stays strict while the seek is in flight — playing is no loophole",
  );
});

test("2x playback ticks advance the mark and are never clamped", () => {
  const t = new WatchTracker(sink());
  const at2x = (seconds) => ({ currentTime: seconds, duration: 60, paused: false, playbackRate: 2 });
  // At 2x a slightly slow tick covers >1s of media — over the NORMAL tolerance, within the scaled one.
  for (const seconds of [0.9, 2.4, 4.2]) {
    assert.equal(t.overshootBeyondWatched(at2x(seconds)), null, "2x playback must not be dragged back");
    t.onTimeUpdate(at2x(seconds));
  }
  assert.equal(t.watchedMs, 4200, "the mark keeps up with double-speed playback");
  // A genuine leap is still refused even at 2x: 2000ms of tolerance, not unlimited.
  t.onTimeUpdate(at2x(9.9));
  assert.equal(t.watchedMs, 4200, "a leap past the scaled tolerance is still not watching");
});

test("the tolerance returns once real playback progress exists", () => {
  const tracker = new WatchTracker({ record: () => {} });
  const video = { currentTime: 0.5, duration: 11.79, paused: false };
  tracker.onTimeUpdate(video); // natural progress: the mark advances to 500ms
  assert.equal(tracker.watchedMs, 500);

  // 500 + 1500 = 2000ms is now allowed again, so ordinary jitter is not fought.
  video.currentTime = 1.9;
  assert.equal(tracker.onSeeking(video), null, "a nudge within tolerance must stay allowed once watching");
  video.currentTime = 9;
  assert.equal(tracker.onSeeking(video), 500, "a real skip past the watched mark must still be refused");
});
