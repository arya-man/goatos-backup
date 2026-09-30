// The notification centre is a full-height RIGHT DRAWER (the template MinimalDrawer) at every width,
// portaled to <body> by MUI. The measured-popover placement it replaced (and its `notification-placement` arithmetic)
// is gone: nothing is measured, so nothing can be measured wrong on a wrapped phone top bar.
//
// These are SOURCE assertions on the bell, because the invariants they pin are structural:
//   * the drawer is the MUI Drawer and is portaled (`.top`'s backdrop-filter would otherwise make
//     the bar the containing block of a `position:fixed` sheet, pinning it inside the top bar);
//   * `setOpen` is only ever called with a boolean literal (a state updater must be pure -- React
//     runs it twice in StrictMode, and a side effect in there updated the Router mid-render);
//   * the first feed read is deferred off the hydration commit (the App Router's action queue is
//     not initialised yet on a fresh load), and it is a GET, never a Server Action (an action POST
//     re-renders the page server-side and crashed the Router on pages that `redirect()`).
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const read = (path) => readFileSync(new URL(path, import.meta.url), "utf8");
const bell = read("./notification-bell.tsx");
const panel = read("./notification-panel.tsx");

test("the bell renders the centre as the portaled template temporary drawer at every width", () => {
  assert.match(bell, /import \{ MinimalDrawer \} from "@\/components\/app\/drawer"/, "the drawer must be the template MinimalDrawer (right, backdrop)");
  assert.doesNotMatch(bell, /disablePortal/, "the drawer must stay portaled out of `.top`");
  assert.doesNotMatch(bell, /invisible: true/, "the backdrop dims the page (Ravi R2-4)");
  assert.match(bell, /<MinimalDrawer\s+open=\{open\}\s+onClose=\{closePanel\}/, "opened and closed by the bell");
  assert.match(bell, /width=\{420\}/, "the template notifications-drawer width (420), full width on a phone");
  assert.doesNotMatch(bell, /notification-placement|placePanel|panelBox|PopSurface|matchMedia/, "no measured popover, no breakpoint switch");
});

test("the bell toggles `open` with boolean literals only", () => {
  const setOpenArgs = [...bell.matchAll(/setOpen\(([^;]*?)\);/g)].map((match) => match[1].trim());
  assert.ok(setOpenArgs.length > 0, "the bell must still toggle `open`");
  for (const arg of setOpenArgs) {
    assert.match(arg, /^(true|false)$/, `setOpen must be called with a boolean literal: setOpen(${arg})`);
  }
});

test("the bell's reads are deferred GETs, never Server Actions", () => {
  assert.match(
    bell,
    /setTimeout\(\(\) => \{[\s\S]{0,400}?fetchNotificationFeed\(\)/,
    "the mount feed read must be deferred off the hydration commit",
  );
  assert.doesNotMatch(bell, /loadNotificationFeedAction|loadNotificationBadgeAction/, "reads go through the GET route handler");
  assert.match(bell, /markNotificationsReadAction/, "mark-read stays a Server Action (a real mutation)");
});

test("the panel is template notifications-drawer anatomy in theme sx and keeps the row contract", () => {
  // FIXJ3: notification-panel.css is gone; the rows are the template NotificationItem in sx.
  assert.doesNotMatch(panel, /\.css["']/, "no feature stylesheet");
  assert.doesNotMatch(panel, /className=/, "no legacy classes: sx + data-* hooks only");
  assert.doesNotMatch(panel, /#[0-9a-fA-F]{3,8}\b|rgba?\(\s*\d/, "no colour literals; theme palette only");
  assert.match(panel, /minHeight: "var\(--table-row-h\)",/, "rows keep the 72px floor (--table-row-h)");
  assert.match(panel, /p: 2\.5,/, "rows keep the template 20px padding");
  assert.match(panel, /width: "calc\(5 \* var\(--spacing\)\)",\s*height: "calc\(5 \* var\(--spacing\)\)",/, "40px actor avatar / icon circle (theme.spacing(5))");
  assert.match(panel, /borderBottom: 1,\s*borderBottomStyle: "dashed",\s*borderColor: "divider",/, "template dashed row divider");
  assert.match(panel, /<Tabs\s+variant="fullWidth"[\s\S]*?indicatorColor="custom"/, "template full-width tabs");
});
