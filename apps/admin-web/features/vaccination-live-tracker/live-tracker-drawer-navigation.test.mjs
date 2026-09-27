import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// Every admin-web table that lists individual goats opens the shared Goat Passport as a LOCAL
// overlay. A route navigation would tear down the live board — losing the poll state, the filter
// state and the scroll position — every time someone checked one animal.
const combo = readFileSync(new URL("./live-tracker-combo.tsx", import.meta.url), "utf8");
const drawer = readFileSync(new URL("./live-tracker-passport-drawer.tsx", import.meta.url), "utf8");
const board = readFileSync(new URL("./live-tracker-board.tsx", import.meta.url), "utf8");

test("combo animal rows open the passport through LocalOverlayLink", () => {
  assert.match(combo, /import \{ LocalOverlayLink \} from "@\/components\/local-overlay-link"/);
  assert.match(combo, /component=\{LocalOverlayLink\}[\s\S]{0,400}passportHref\(row\.goat_id\)/);
});

test("the drawer is mounted by the board and closes back to the same board state", () => {
  assert.match(board, /<LiveTrackerPassportDrawer/);
  assert.match(board, /closeHref=\{closePassportHref\}/);
  assert.match(board, /const closePassportHref = /);
});

test("the drawer reads the vaccination passport route, not a bespoke endpoint", () => {
  assert.match(drawer, /\/api\/goats\/\$\{encodeURIComponent\(goatId\)\}\/vaccination-passport/);
  assert.match(drawer, /useLocalOverlaySelection/);
  assert.match(drawer, /selectionKey: "goat_passport"/);
});

test("the drawer is the template temporary drawer with the record meta grid, never helpgrid", () => {
  assert.match(drawer, /<DetailDrawer\b/);
  assert.match(drawer, /onClose=\{closeDrawer\}/);
  assert.match(drawer, /<DrawerMetaGrid>/);
  assert.match(drawer, /<DrawerTableScroll>/, "the obligations table scrolls inside its own Scrollbar, never clipped");
  assert.ok(!/helpgrid|className=\{?`?"?drawer\b|className="scrim/.test(drawer), "no legacy .drawer/.scrim/.helpgrid shell");
});

test("the drawer surfaces both tags, so a dual-tagged animal is fully identified", () => {
  assert.match(drawer, /drawer\.passport\.tag_1/);
  assert.match(drawer, /drawer\.passport\.tag_2/);
  assert.match(drawer, /displayedItem\.secondary_tag/);
});
