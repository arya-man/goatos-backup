import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const drawerSource = readFileSync(new URL("./verification-review-drawer.tsx", import.meta.url), "utf8");
const pageSource = readFileSync(new URL("./verification-review-page.tsx", import.meta.url), "utf8");

test("pc care inventory verification items use the generic media drawer path", () => {
  assert.match(
    drawerSource,
    /const hasEvidence = item\.media\.length > 0/,
    "the drawer must decide evidence presence from VerificationQueueItem.media, not from category-specific animal rows",
  );
  assert.match(
    drawerSource,
    /activeMedia\?\.mime_type\?\.startsWith\("video\/"\)[\s\S]{0,240}<ReviewVideoPlayer/,
    "a fridge stock video must render through the generic video proof player",
  );
  assert.match(
    drawerSource,
    /activeMedia\?\.mime_type\?\.startsWith\("image\/"\)/,
    "a fridge stock photo must enter the generic image proof branch",
  );
  assert.match(
    drawerSource,
    /<img[\s\S]{0,260}src=\{activeMedia\.download_url\}/,
    "the generic image proof branch must point at the proof's own download URL",
  );
});

test("verification action labels stay backend-owned for inventory_vaccine", () => {
  assert.match(
    pageSource,
    /actionTypeLabels=\{Object\.fromEntries\(typeLabels\)\}/,
    "the drawer must receive backend-composed action type labels keyed by category",
  );
  assert.match(
    drawerSource,
    /actionTypeLabels\[item\.category\] \?\? item\.category/,
    "inventory_vaccine should use its backend category label when present and only fall back to the token",
  );
  assert.doesNotMatch(
    drawerSource + pageSource,
    /inventory_vaccine[\s\S]{0,120}(?:return null|display:\s*["']none|media:\s*\[\])/,
    "admin-web must not special-case inventory_vaccine by hiding the row or stripping media",
  );
});
