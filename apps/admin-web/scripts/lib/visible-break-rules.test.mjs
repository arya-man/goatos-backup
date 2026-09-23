import test from "node:test";
import assert from "node:assert/strict";
import {
  cellOverpaintDetail,
  controlTextIsCutOff,
  scrollOwnerIndex,
  overlapIsVisibleBreak,
  overlayHeaderOutOfView,
} from "./visible-break-rules.mjs";

// Every fixture below is the real measurement from the 2026-09-22 production
// sweep (module-journeys-receipt.json) or from re-measuring the same page on
// dashboard.mesha.sg. Each rule gets both directions: the page that was wrongly
// accused, and the page that must still be caught.

// ---------------------------------------------------------------------------
// Table cell text running past its column
// ---------------------------------------------------------------------------

// People / HRMS -> Notifications. The alert title is 48px wider than its column;
// the tick-box column beside it is empty on that line.
const NOTIFICATIONS_TITLE = {
  rect: { left: 16, right: 330, top: 430, bottom: 590 },
  overflowPx: 48,
  neighbourTexts: [{ rect: { left: 330, right: 396, top: 495, bottom: 515 }, text: "" }],
};

test("a title running into an empty tick-box column is not a break", () => {
  assert.equal(cellOverpaintDetail(NOTIFICATIONS_TITLE), "");
});

test("the same title is still reported when it lands on the next column's words", () => {
  const collides = {
    ...NOTIFICATIONS_TITLE,
    neighbourTexts: [{ rect: { left: 336, right: 420, top: 440, bottom: 460 }, text: "Park Head" }],
  };
  assert.match(cellOverpaintDetail(collides), /painted over "Park Head"/);
});

test("words in the next column on a different line are not painted over", () => {
  const belowIt = {
    ...NOTIFICATIONS_TITLE,
    neighbourTexts: [{ rect: { left: 336, right: 420, top: 700, bottom: 720 }, text: "Park Head" }],
  };
  assert.equal(cellOverpaintDetail(belowIt), "");
});

test("a cell that fits reports nothing whatever is beside it", () => {
  assert.equal(cellOverpaintDetail({ ...NOTIFICATIONS_TITLE, overflowPx: 0 }), "");
});

// ---------------------------------------------------------------------------
// Button / link text cut off
// ---------------------------------------------------------------------------

// Tasks -> List, task #6. Measured live: the link is 320px wide, its words are
// 323px, and every ancestor up to the table is overflow-x: visible, so the last
// 3px paint in the cell's padding. Nothing is hidden.
const TASK_LINK_6 = {
  overflowX: "visible",
  overflowY: "visible",
  textOverflow: "clip",
  lineClamp: "none",
  clientWidth: 320,
  scrollWidth: 323,
  clientHeight: 39,
  scrollHeight: 39,
};

test("a task link whose words paint outside a box that does not clip is not cut off", () => {
  assert.equal(controlTextIsCutOff(TASK_LINK_6), false);
});

test("the same link IS cut off once its box clips", () => {
  assert.equal(controlTextIsCutOff({ ...TASK_LINK_6, overflowX: "hidden" }), true);
});

test("a control that clips but ends in an ellipsis is doing what it was asked to", () => {
  assert.equal(controlTextIsCutOff({ ...TASK_LINK_6, overflowX: "hidden", textOverflow: "ellipsis" }), false);
});

test("a clipped control with a line clamp is not a break either", () => {
  const clamped = { ...TASK_LINK_6, overflowY: "hidden", lineClamp: "2", clientHeight: 39, scrollHeight: 80 };
  assert.equal(controlTextIsCutOff(clamped), false);
});

test("a clipped two-line label with no clamp is still caught", () => {
  const cut = { ...TASK_LINK_6, overflowY: "hidden", lineClamp: "none", clientHeight: 39, scrollHeight: 80 };
  assert.equal(controlTextIsCutOff(cut), true);
});

// Work Board card at phone width: the whole card is a link, 240px wide, 250px of
// content, and the card does not clip.
test("a whole card wrapped in a link is not a cut-off button label", () => {
  const card = { ...TASK_LINK_6, clientWidth: 240, scrollWidth: 250, clientHeight: 186, scrollHeight: 186 };
  assert.equal(controlTextIsCutOff(card), false);
});

// ---------------------------------------------------------------------------
// Wide tables and sideways scroll
// ---------------------------------------------------------------------------

// People / HRMS -> Notifications at phone width, measured live: the table is
// 961px inside a 362px wrapper that scrolls.
const NOTIFICATION_MATRIX_CHAIN = [
  { cls: "tblwrap", overflowX: "auto", clientWidth: 362, scrollWidth: 961 },
  { cls: "notification-matrix", overflowX: "visible", clientWidth: 362, scrollWidth: 362 },
  { cls: "wrap", overflowX: "hidden", clientWidth: 390, scrollWidth: 390 },
];

test("a wide table whose wrapper scrolls has a scroll owner, whatever the wrapper is called", () => {
  assert.equal(scrollOwnerIndex(NOTIFICATION_MATRIX_CHAIN), 0);
});

test("a wide table with nothing scrollable above it still has no scroll owner", () => {
  const stuck = NOTIFICATION_MATRIX_CHAIN.map((node) => ({ ...node, overflowX: "hidden" }));
  assert.equal(scrollOwnerIndex(stuck), -1);
});

test("a wrapper set to auto that has no room to scroll is not the scroll owner", () => {
  const noRoom = [{ cls: "tblwrap", overflowX: "auto", clientWidth: 961, scrollWidth: 961 }];
  assert.equal(scrollOwnerIndex(noRoom), -1);
});

test("an outer scroller further up the chain still counts", () => {
  const outer = [
    { cls: "inner", overflowX: "visible", clientWidth: 362, scrollWidth: 362 },
    { cls: "shell", overflowX: "scroll", clientWidth: 362, scrollWidth: 961 },
  ];
  assert.equal(scrollOwnerIndex(outer), 1);
});

// ---------------------------------------------------------------------------
// Controls on top of each other
// ---------------------------------------------------------------------------

// Verify with the video log open: the Analytics link is behind the drawer and
// its backdrop, which is what an open drawer looks like.
test("a backdrop covering the page it dimmed is not two buttons overlapping", () => {
  const pair = {
    first: { coveredByOverlay: true, isOverlayChrome: false },
    second: { coveredByOverlay: false, isOverlayChrome: true },
  };
  assert.equal(overlapIsVisibleBreak(pair), false);
});

test("a control behind an open drawer and one inside it are not on the same layer", () => {
  const pair = {
    first: { coveredByOverlay: true, isOverlayChrome: false },
    second: { coveredByOverlay: false, isOverlayChrome: false },
  };
  assert.equal(overlapIsVisibleBreak(pair), false);
});

test("two controls colliding on the page itself are still a break", () => {
  const pair = {
    first: { coveredByOverlay: false, isOverlayChrome: false },
    second: { coveredByOverlay: false, isOverlayChrome: false },
  };
  assert.equal(overlapIsVisibleBreak(pair), true);
});

test("two controls colliding inside the same open drawer are still a break", () => {
  const pair = {
    first: { coveredByOverlay: true, isOverlayChrome: false },
    second: { coveredByOverlay: true, isOverlayChrome: false },
  };
  assert.equal(overlapIsVisibleBreak(pair), true);
});

// ---------------------------------------------------------------------------
// Drawer opening with its title out of view
// ---------------------------------------------------------------------------

// Tasks card drawer, measured live: panel top 0, title block top 63px, 110px
// tall, nothing scrolled — the breadcrumb and Edit/Close bar sit above it.
const TASKS_DRAWER = {
  overlayTop: 0,
  headerTop: 63,
  headerBottom: 173,
  headerHeight: 110,
  viewportHeight: 1000,
  scrollerScrollTop: 0,
};

test("a drawer with a toolbar above its title is not opening scrolled away", () => {
  assert.equal(overlayHeaderOutOfView(TASKS_DRAWER), "");
});

test("the phone layout, where the toolbar is taller, is fine too", () => {
  assert.equal(overlayHeaderOutOfView({ ...TASKS_DRAWER, headerTop: 90, headerBottom: 200, viewportHeight: 900 }), "");
});

test("a drawer that opens already scrolled past its title is still caught", () => {
  assert.match(overlayHeaderOutOfView({ ...TASKS_DRAWER, scrollerScrollTop: 220 }), /already scrolled/);
});

test("a title pushed above the panel's own top edge is still caught", () => {
  assert.match(overlayHeaderOutOfView({ ...TASKS_DRAWER, headerTop: -40, headerBottom: 70 }), /above the panel/);
});

test("a title pushed off the bottom of the screen is still caught", () => {
  assert.match(
    overlayHeaderOutOfView({ ...TASKS_DRAWER, headerTop: 1200, headerBottom: 1310 }),
    /below the bottom of the screen/,
  );
});

test("a title collapsed to nothing is still caught", () => {
  assert.match(overlayHeaderOutOfView({ ...TASKS_DRAWER, headerHeight: 0 }), /title bar is 0px tall/);
});
