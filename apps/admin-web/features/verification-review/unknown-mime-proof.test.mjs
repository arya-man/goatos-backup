// A proof whose kind nobody could tell (an `either` SOP slot the proof register could not type, an
// attachment) arrives with a blank or non-media mime_type. The backend deliberately leaves it
// unknown rather than guessing a player (verifier-app-and-flow.md, media kind order). The drawer
// used to send it to the "no proof media is attached" empty state -- telling the verifier the
// evidence was missing while it sat uploaded and openable.
//
// Source-shape tests, like every other test in this folder: there is no DOM harness here.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const drawerSource = readFileSync(new URL("./verification-review-drawer.tsx", import.meta.url), "utf8");

test("an unknown-kind proof renders an Open proof tile, not the missing-media state", () => {
  const imageBranch = drawerSource.indexOf(') : activeMedia?.mime_type?.startsWith("image/") ? (');
  assert.ok(imageBranch > 0, "the image branch must exist");
  const unknownBranch = drawerSource.indexOf('text("drawer.media.open")', imageBranch);
  const emptyFallback = drawerSource.indexOf('<Box className="vr-player-empty" sx={PLAYER_EMPTY_SX}>{text("drawer.media.empty")}</Box>', imageBranch);
  assert.ok(unknownBranch > imageBranch, "after video and image, a present proof of unknown kind must offer the backend's Open proof copy");
  assert.ok(emptyFallback > unknownBranch, "the empty state stays the LAST branch, reached only when there is no active proof at all");
  assert.match(
    drawerSource,
    /className="vr-media-open" sx=\{MEDIA_OPEN_SX\} onClick=\{resolveActiveMedia\}[\s\S]{0,400}text\("drawer\.media\.open"\)/,
    "the tile resolves the proof link on click, exactly like the video tile",
  );
  assert.match(
    drawerSource,
    /href=\{resolvedMediaUrls\[activeMedia\.proof_id\]\}[\s\S]{0,300}text\("drawer\.media\.open"\)/,
    "once resolved, the tile opens the proof itself",
  );
});

test("the proof switcher never promises a clip for a proof of unknown kind", () => {
  assert.match(
    drawerSource,
    /media\.mime_type\?\.startsWith\("image\/"\)\s*\?\s*\(\s*<Iconify icon="solar:gallery-wide-bold"[\s\S]{0,120}\)\s*:\s*media\.mime_type\?\.startsWith\("video\/"\)\s*\?\s*\(\s*<Iconify icon="solar:play-circle-bold"[\s\S]{0,120}\)\s*:\s*\(\s*<Iconify icon="solar:file-text-bold"/,
    "an unknown-kind proof chip shows a neutral file icon, not a play badge",
  );
});
