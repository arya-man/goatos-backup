import assert from "node:assert/strict";
import test from "node:test";

import { parseScope, preservedPageFiltersForScopeChange, scopeHref } from "./scope.ts";

test("top bar scope changes preserve valid page date windows and drop half windows", () => {
  const completeWindow = new URLSearchParams("scope_mode=park&park=old&from=2026-09-01&to=2026-09-22&status=open");
  const completeHref = scopeHref(
    "/counts/analytics",
    parseScope(Object.fromEntries(completeWindow.entries())),
    { park: "new", mode: "park" },
    preservedPageFiltersForScopeChange(completeWindow),
  );
  assert.equal(completeHref, "/counts/analytics?scope_mode=park&park=new&from=2026-09-01&to=2026-09-22&status=open");

  const halfWindow = new URLSearchParams("scope_mode=park&park=old&to=2026-09-22&status=open");
  const halfHref = scopeHref(
    "/counts/analytics",
    parseScope(Object.fromEntries(halfWindow.entries())),
    { park: "new", mode: "park" },
    preservedPageFiltersForScopeChange(halfWindow),
  );
  assert.equal(halfHref, "/counts/analytics?scope_mode=park&park=new&status=open");
});

test("top bar scope changes preserve repeated page filters", () => {
  const sp = new URLSearchParams("scope_mode=company&bd_stage=kid&bd_stage=adult&bd_breed=osmanabadi");
  const href = scopeHref(
    "/counts/breakdown",
    parseScope(Object.fromEntries(sp.entries())),
    { park: "new", mode: "park" },
    preservedPageFiltersForScopeChange(sp),
  );
  assert.equal(href, "/counts/breakdown?scope_mode=park&park=new&bd_stage=kid&bd_stage=adult&bd_breed=osmanabadi");
});
