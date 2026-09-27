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

test("every SOP library route (work instructions included) has its skeleton in the registry", () => {
  const reg = read("./route-skeleton.tsx");
  for (const route of ["configuration/work-instructions", "pc-care/sops", "procurement/sops", "milk/sops", "counts/sops", "feed/sops", "weighing/sops", "sales/sops"]) {
    const entry = `[/^\\/${route.replace("/", "\\/")}(?:\\/|$)/, L`;
    assert.ok(reg.includes(entry), `${route} missing from ROUTE_SKELETONS`);
  }
  assert.match(reg, /export function PendingRouteSkeleton/);
});
