import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const drawerSource = readFileSync(new URL("./verification-review-drawer.tsx", import.meta.url), "utf8");
const cssSource = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");

test("unresolved proof media renders as a player tile, not a form button", () => {
  assert.doesNotMatch(
    drawerSource,
    /className="btn vr-media-open"/,
    "the unresolved media affordance must not inherit generic button styling",
  );
  assert.match(
    cssSource,
    /\.vr-media-open\{[^}]*position:absolute;inset:0/,
    "the unresolved media affordance should fill the 16:9 player stage",
  );
  assert.match(
    cssSource,
    /\.vr-media-open-mark/,
    "the player tile should carry a centered play/image affordance",
  );
});

test("clicking the queue thumbnail opens the drawer with a play intent", () => {
  assert.match(
    drawerSource,
    /const \[playIntent, setPlayIntent\] = useState\(one\(searchParams, "vi_play"\) === "1"\)/,
    "the drawer must seed the explicit thumbnail play intent from the URL",
  );
  assert.match(
    drawerSource,
    /setPlayIntent\(currentParams\.get\("vi_play"\) === "1"\)/,
    "local overlay row clicks must update play intent without waiting for a server rerender",
  );
  assert.match(
    drawerSource,
    /if \(!open \|\| !playIntent \|\| !activeProofId \|\| resolvedMediaUrls\[activeProofId\]\) return;\s+const timeout = window\.setTimeout\(resolveActiveMedia, 0\);\s+return \(\) => window\.clearTimeout\(timeout\);/,
    "opening from the queue thumbnail should resolve the first proof without a second click",
  );
  assert.match(
    drawerSource,
    /autoPlay=\{playIntent\}/,
    "the resolved player should receive the queue thumbnail's play intent",
  );
  assert.match(
    drawerSource,
    /text\("drawer\.media\.play_video"\)/,
    "video proofs should not use the generic media-open label",
  );
  assert.match(
    drawerSource,
    /text\("drawer\.media\.open_photo"\)/,
    "photo proofs should have their own open-photo label",
  );
  assert.match(
    drawerSource,
    /key === "vi_row" \|\| key === "vi_play"/,
    "normal drawer navigation must clear the play intent so it does not leak to next items",
  );
});
