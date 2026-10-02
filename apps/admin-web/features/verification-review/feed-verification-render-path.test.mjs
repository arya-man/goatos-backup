import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// GOS-461-1 (review of PR #461): FeedVerification was server-rendered as the panel's children on
// EVERY /verify render the capability allowed, and it awaited GET /feed-analytics/packing-verification
// whether the drawer was open or not -- so every Accept redirect to the next video paid for a feed
// read nobody was looking at. A closed panel must make no feed read at all.

const read = (name) => readFileSync(new URL(`./${name}`, import.meta.url), "utf8");
const page = read("verification-review-page.tsx");
const panel = read("feed-verification-panel.tsx");
const view = read("feed-verification-view.tsx");
const server = read("feed-verification.tsx");
const action = read("feed-verification-actions.ts");

test("the page reads the feed day only when the URL already asks for the panel", () => {
  // The server-rendered day is gated on the panel key being in the QUERY ...
  assert.match(
    page,
    /\{feedVerificationOpenInQuery \? \(\s*<FeedVerification \{\.\.\.feedVerificationView\}/,
    "FeedVerification must render only when the query says the panel is open",
  );
  assert.match(
    page,
    /const feedVerificationOpenInQuery = one\(sp, FEED_VERIFICATION_PANEL_SELECTION_KEY\) === FEED_VERIFICATION_PANEL_ID;/,
  );
  // ... and there is exactly ONE <FeedVerification> on the page, the gated one.
  assert.equal(page.match(/<FeedVerification\b/g)?.length, 1, "no second, ungated FeedVerification render");
});

test("a closed panel's page render has no path to the feed read", () => {
  // The only server-side caller is the wrapper the page gates above, plus the drawer's own action.
  assert.match(server, /await getFeedPackingVerificationLog\(/);
  assert.match(action, /^"use server";/);
  assert.match(action, /await getFeedPackingVerificationLog\(/);
  // The view is a pure renderer of a day it is handed: a client module that never fetches.
  assert.match(view, /^"use client";/);
  assert.doesNotMatch(view, /getFeedPackingVerificationLog\(|loadFeedVerificationLogAction\(/);
  assert.doesNotMatch(page, /getFeedPackingVerificationLog\(/, "the page itself must not read the log");
});

test("a panel opened by its button loads the day itself, once, and only when it has no server render", () => {
  assert.match(panel, /loadFeedVerificationLogAction\(feedDay, parkId\)/);
  assert.match(panel, /serverRendered \? \(\s*children\s*\) : drawerOpen \? \(\s*<FeedVerificationClientBody\b/);
  assert.match(panel, /function FeedVerificationClientBody\(/);
  // The effect must not depend on its own loading state: re-running on "loading" cancels the
  // request it just made, and the drawer sat on its skeleton forever (E2E 2026-10-01).
  assert.match(panel, /\}, \[feedDay, parkId\]\);/);
  assert.doesNotMatch(panel, /\[[^\]]*\bloaded\b[^\]]*\]\);/, "no effect may list `loaded` as a dependency");
  // Opening stays client-local: the button is a hash LocalOverlayLink, never a route navigation.
  assert.match(panel, /<LocalOverlayLink href=\{`#\$\{selectionKey\}=\$\{panelId\}`\}/);
});
