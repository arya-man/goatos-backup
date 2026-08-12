// A proof is not always a video, and this screen used to assume it was.
//
// Feed distribution submits TWO proofs per shed-session: a distribution VIDEO and a
// water proof that may be a PHOTO OR a video
// (docs/decisions/feed-distribution-verification.md, maintainer decision 2026-07-26).
// The drawer rendered `mime_type.startsWith("video/")` and sent EVERYTHING else to the
// "no media" empty state, so a verifier holding a photo water proof was told her
// evidence was missing while it sat uploaded, linked to the item, and reviewable — and
// the verdict buttons above it stayed enabled, because `hasEvidence` counts media rows
// and correctly saw one. She could only reject work that was actually proved.
//
// These pin the render branch by media KIND. Source-shape tests, like every other test
// in this folder: there is no DOM harness here, and the failure was a missing branch.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const drawerSource = readFileSync(new URL("./verification-review-drawer.tsx", import.meta.url), "utf8");

test("an image proof renders as a picture, not as the missing-media state", () => {
  assert.match(
    drawerSource,
    /activeMedia\?\.mime_type\?\.startsWith\("image\/"\)/,
    "the drawer must branch on an image/* proof before falling back to the empty state",
  );
  assert.match(
    drawerSource,
    /<img[\s\S]{0,240}src=\{activeMedia\.download_url\}/,
    "an image/* proof must render an <img> pointed at the proof's own download URL",
  );
});

test("the image branch is decided before the missing-media fallback", () => {
  const imageBranch = drawerSource.indexOf('activeMedia?.mime_type?.startsWith("image/")');
  const emptyFallback = drawerSource.indexOf('<div className="vr-player-empty">{text("drawer.media.empty")}</div>', imageBranch);
  assert.ok(imageBranch > 0, "image branch must exist");
  assert.ok(
    emptyFallback > imageBranch,
    "the empty state must remain the LAST branch — an image reaching it is the original defect",
  );
});

test("a still is never handed to the video player", () => {
  // The player emits video_play / video_seek_attempt / video_ended and blocks forward
  // seeking on a timeline. A photo has no timeline, so feeding one here would write
  // review events for positions that do not exist.
  const playerAt = drawerSource.indexOf("<ReviewVideoPlayer");
  const videoGuardAt = drawerSource.indexOf('activeMedia?.mime_type?.startsWith("video/")');
  assert.ok(videoGuardAt > 0 && playerAt > videoGuardAt, "ReviewVideoPlayer must stay behind the video/* guard");
});

test("the proof switcher chip names the kind of proof it opens", () => {
  // Two proofs on one feed-distribution item means the strip is always visible there; a
  // play badge on the photo chip promises a clip that does not exist.
  assert.match(
    drawerSource,
    /media\.mime_type\?\.startsWith\("image\/"\)[\s\S]{0,120}<ImageIcon/,
    "an image proof's switcher chip must carry the image icon, not the play icon",
  );
});
