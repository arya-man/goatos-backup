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
  const pageSource = readFileSync(new URL("./verification-review-page.tsx", import.meta.url), "utf8");
  const apiSource = readFileSync(new URL("../../lib/api/server.ts", import.meta.url), "utf8");
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
    /if \(!open \|\| !activeProofId \|\| resolvedMediaUrls\[activeProofId\]\) return;\s+if \(!playIntent && !activeIsPhoto\) return;[\s\S]*?const timeout = window\.setTimeout\(resolveActiveMedia, 0\);\s+return \(\) => window\.clearTimeout\(timeout\);/,
    "opening from the queue thumbnail resolves the first proof without a second click, and a photo proof resolves on open",
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
    /text\("drawer\.media\.loading_photo"\)/,
    "photo proofs should show a passive loading state while automatic resolution finishes",
  );
  assert.doesNotMatch(
    drawerSource,
    /text\("drawer\.media\.open_photo"\)/,
    "photo proofs must not render the retired open-photo tap-to-reveal label",
  );
  assert.match(
    drawerSource,
    /key === "vi_row" \|\| key === "vi_play"/,
    "normal drawer navigation must clear the play intent so it does not leak to next items",
  );
  assert.match(
    apiSource,
    /thumbnail_url: media\.thumbnail_url \? absolutizeBackendURL\(media\.thumbnail_url, baseUrl\) : media\.thumbnail_url/,
    "queue media should carry backend-provided thumbnail URLs through the admin-web API client",
  );
  assert.match(
    pageSource,
    /const leadMedia = item\.media\.find\(\(media\) => media\.thumbnail_url\) \?\? item\.media\[0\]/,
    "the list tile should prefer media that has backend-provided thumbnail metadata",
  );
  assert.match(
    pageSource,
    /<Avatar variant="rounded" src=\{leadMedia\?\.thumbnail_url \|\| undefined\} alt="" slotProps=\{\{ img: \{ loading: "lazy", decoding: "async" \} \}\}/,
    "the list tile should render a lightweight thumbnail when the backend provides one",
  );
  assert.doesNotMatch(
    pageSource,
    /<img src=\{leadMedia\?\.download_url\}|<img src=\{item\.media\[0\]\?\.download_url\}/,
    "the list tile must not use the full proof download URL as a thumbnail",
  );
});
