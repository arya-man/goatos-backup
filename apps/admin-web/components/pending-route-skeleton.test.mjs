import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

// guard: pending-route-skeleton (REVIEW-37 #1, Ravi: a sidebar click into /configuration/work-instructions
// left the OLD page on screen). Links do not prefetch, so the router cannot show a route's loading.tsx
// until the server answers; the shell paints the TARGET route's skeleton in the click's frame instead
// and hides (keeps mounted) the page being left.
test("the shell paints the target route skeleton on a path-changing click", () => {
  const shell = read("./mesha-shell.tsx");
  assert.match(shell, /setPendingHref\(dest && dest\.pathname !== window\.location\.pathname \?/);
  assert.match(shell, /<PendingRouteSkeleton href=\{pendingHref\} \/>/);
  assert.match(shell, /data-route-skeleton=\{pendingHref \? "" : undefined\}/);
  assert.match(shell, /"& \.msh-wrap\[data-route-skeleton\] > :not\(\[data-route-skeleton-el\]\)/);
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
