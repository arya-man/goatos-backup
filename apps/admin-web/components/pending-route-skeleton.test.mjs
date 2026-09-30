import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { legacyCss } from "../scripts/lib/legacy-css.mjs";

const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

// guard: pending-route-skeleton (REVIEW-37 #1, Ravi: a sidebar click into /configuration/work-instructions
// left the OLD page on screen). Links do not prefetch, so the router cannot show a route's loading.tsx
// until the server answers; the shell paints the TARGET route's skeleton in the click's frame instead
// and hides (keeps mounted) the page being left.
test("the shell paints the target route skeleton on a path-changing click", () => {
  const shell = read("./mesha-shell.tsx");
  assert.match(shell, /const pathChange = !!dest && dest\.pathname !== window\.location\.pathname;\s*setPendingHref\(pathChange \?/);
  assert.match(shell, /<PendingRouteSkeleton href=\{pendingHref\} \/>/);
  assert.match(shell, /data-route-skeleton=\{pendingHref \? "" : undefined\}/);
  // FIXJ6: the page column is the sx Box `[data-page-column]` (no `.wrap.msh-wrap`); it hides the page
  // being left while the target's skeleton shows.
  assert.match(shell, /"&\[data-route-skeleton\] > :not\(\[data-route-skeleton-el\]\):not\(\[data-shell-alert\]\)": \{ display: "none !important" \}/);
  assert.match(shell, /<Box data-page-column="" data-route-skeleton=\{pendingHref \? "" : undefined\} sx=\{PAGE_COLUMN_SX\}>/);
  // cleared with the route-busy state (commit, timeout, supersede)
  assert.match(shell, /setRoutePending\(false\);\s*setPendingHref\(null\);/);
});

// REVIEW-40 O59: programmatic navigation (router.push from a calendar drive click, a row menu, an
// editor) and the header back arrow paint the target's skeleton too: every startRoutePending call
// passes the target href, and the shell turns UrlNavRouter's announced path changes into one.
test("programmatic navigations and the back arrow pass a target href", () => {
  const shell = read("./mesha-shell.tsx");
  assert.doesNotMatch(shell, /startRoutePending\(\s*\)/, "a bare startRoutePending() leaves the old page up");
  assert.match(shell, /startRoutePending\(null, last\?\.href, "back"\)/);
  assert.match(shell, /window\.addEventListener\(URL_NAV_EVENT, onNav\)/);
  assert.match(shell, /startRoutePending\(null, to, "programmatic"\)/);
  // every router.push / replace below the shell is announced (so the shell sees it)
  const router = read("./app/url-nav-router.tsx");
  assert.match(router, /push: \(href: string[^)]*\) => \{\s*announceUrlNav\(href\);/);
  assert.match(router, /replace: \(href: string[^)]*\) => \{\s*announceUrlNav\(href\);/);
});

test("every SOP library route (work instructions included) has its skeleton in the registry", () => {
  const reg = read("./route-skeleton.tsx");
  for (const route of ["configuration/work-instructions", "pc-care/sops", "procurement/sops", "milk/sops", "counts/sops", "feed/sops", "weighing/sops", "sales/sops"]) {
    const entry = `[/^\\/${route.replace("/", "\\/")}(?:\\/|$)/, L`;
    assert.ok(reg.includes(entry), `${route} missing from ROUTE_SKELETONS`);
  }
  assert.match(reg, /export function PendingRouteSkeleton/);
});

// guard: pending-skeleton-root-gap (SK1 -> FIXJ6). The pending skeleton sits in a `display: contents`
// wrapper. SK1 restated frame.css's `.wrap > .screen` page grid on it; since FIXJ6 every page root and
// every skeleton twin is PageRoot / `PageSkeleton root="page-root"`, which carries the 24px grid in its
// own sx, so the wrapper stays layout-transparent and adds nothing (frame.css is deleted).
test("the pending skeleton wrapper is layout-transparent over the PageRoot twin", () => {
  const shell = read("./mesha-shell.tsx");
  assert.match(shell, /<Box data-route-skeleton-el="" sx=\{PENDING_ROOT_SX\}>/);
  assert.match(shell, /const PENDING_ROOT_SX = \{ display: "contents" \} as const;/);
  // one mechanism: PageSkeleton does not add a second frame gap of its own (REVIEW-45 O74)
  assert.doesNotMatch(read("./app/skeletons/blocks.tsx"), /FRAME_GAP_ROOTS/);
  assert.match(read("./app/skeletons/blocks.tsx"), /root = "page-root"/);
  assert.equal(legacyCss("frame"), "", "frame.css is deleted");
});

// guard: skeleton-no-forced-wrap (SK1). frame.css forced `flex-wrap: wrap` on every div of a busy
// `.screen` skeleton, so rows the loaded page scrolls (the /verify status strip, kanban lanes, tab
// strips) wrapped into extra rows (strip 256px vs 108px at 390) and every block fought it with `&&&`.
// The rule is deleted; skeleton rows wrap exactly where the page's rows wrap.
test("no stylesheet forces flex-wrap on busy skeleton divs", () => {
  const frame = legacyCss("frame");
  assert.doesNotMatch(frame, /\.screen\[aria-busy="true"\]\s+div\s*\{[^}]*flex-wrap/);
  assert.doesNotMatch(read("./app/skeletons/blocks.tsx"), /"&&&": \{ flexWrap/);
});

// guard: pending-route-scroll-top (TR-2 P1-11): a path-changing click lands at the top of the new page.
test("a path-changing navigation (not history back) scrolls to the top with the target skeleton", () => {
  const shell = read("./mesha-shell.tsx");
  assert.match(shell, /const pathChange = !!dest && dest\.pathname !== window\.location\.pathname;/);
  assert.match(shell, /if \(pathChange && source !== "back"\) window\.scrollTo\(0, 0\);/);
});
