import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const rootPageSource = readFileSync(
  new URL("../../app/(admin)/page.tsx", import.meta.url),
  "utf8",
);
const landingHrefSource = readFileSync(
  new URL("../../app/(admin)/landing-href.mjs", import.meta.url),
  "utf8",
);

const { hrefWithWindow } = await import("../../app/(admin)/landing-href.mjs");

// ADG Analytics is the landing page (maintainer request 2026-09-09): "/" redirects straight to
// the dated weighing-analytics window whenever the principal holds it, so launch is one page load
// instead of root -> analytics -> canonical-window.
test("admin root route lands on ADG Analytics when its page contract is present", () => {
  assert.match(rootPageSource, /const LANDING_ROUTE_ID = "weighing-analytics"/);
  assert.match(rootPageSource, /const CONTROL_TOWER_LENS = "control-tower"/);
  assert.match(rootPageSource, /one\(sp, "lens"\) === CONTROL_TOWER_LENS/);
  assert.match(rootPageSource, /item\.route_id === LANDING_ROUTE_ID/);
  assert.match(rootPageSource, /import \{ landingWindow, weightsWindowSettings, WINDOW_FROM_PARAM, WINDOW_TO_PARAM \} from "@\/features\/weighing";/);
  assert.match(rootPageSource, /import \{ hrefWithWindow \} from "\.\/landing-href\.mjs";/);
  assert.match(rootPageSource, /import \{ parseScope, scopeHref \} from "@\/lib\/scope";/);
  assert.match(rootPageSource, /async function landingHref\(landing: \{ href: string; copy\?: Record<string, string> \}, params: RouteSearchParams\): Promise<string>/);
  assert.match(rootPageSource, /if \(landing\.href !== "\/weighing\/analytics"\) return scopeHref\(landing\.href, parseScope\(params\)\);/);
  assert.match(rootPageSource, /if \(selectedFrom && selectedTo\) return hrefWithWindow\(landing\.href, params, selectedFrom, selectedTo\);/);
  assert.match(rootPageSource, /const windowSettings = weightsWindowSettings\(landing\.copy, today\);/);
  assert.match(rootPageSource, /const window = await landingWindow\(/);
  assert.match(landingHrefSource, /next\.set\(WINDOW_FROM_PARAM, from\);/);
  assert.match(landingHrefSource, /next\.set\(WINDOW_TO_PARAM, to\);/);
  assert.match(rootPageSource, /if \(!requestedControlTower && landing\?\.href\) \{\s*redirect\(await landingHref\(landing, sp\)\);/);
  assert.doesNotMatch(rootPageSource, /redirect\(scopeHref\(landing\.href, parseScope\(sp\)\)\);/);
});

test("admin root ADG redirect preserves analytics filters while adding the canonical window", () => {
  const href = hrefWithWindow(
    "/weighing/analytics",
    {
      scope_mode: "company",
      sex: "all",
      tab: "time",
      weighing: "individual_animal",
      origin: "purchased",
      limit: "50",
      offset: "25",
      w_op: "gte",
      w_kg: "30",
    },
    "2026-08-03",
    "2026-09-15",
  );
  const url = new URL(href, "https://dashboard.mesha.sg");
  assert.equal(url.pathname, "/weighing/analytics");
  assert.equal(url.searchParams.get("scope_mode"), "company");
  assert.equal(url.searchParams.get("sex"), "all");
  assert.equal(url.searchParams.get("tab"), "time");
  assert.equal(url.searchParams.get("weighing"), "individual_animal");
  assert.equal(url.searchParams.get("origin"), "purchased");
  assert.equal(url.searchParams.get("limit"), "50");
  assert.equal(url.searchParams.get("offset"), "25");
  assert.equal(url.searchParams.get("w_op"), "gte");
  assert.equal(url.searchParams.get("w_kg"), "30");
  assert.equal(url.searchParams.get("wt_from"), "2026-08-03");
  assert.equal(url.searchParams.get("wt_to"), "2026-09-15");
});

test("admin root ADG redirect preserves analytics filters when the window is already selected", () => {
  const href = hrefWithWindow(
    "/weighing/analytics",
    {
      scope_mode: "company",
      sex: "all",
      tab: "time",
      limit: "50",
      wt_from: "2026-08-03",
      wt_to: "2026-09-15",
    },
    "2026-08-03",
    "2026-09-15",
  );
  const url = new URL(href, "https://dashboard.mesha.sg");
  assert.equal(url.searchParams.get("sex"), "all");
  assert.equal(url.searchParams.get("tab"), "time");
  assert.equal(url.searchParams.get("limit"), "50");
  assert.equal(url.searchParams.get("wt_from"), "2026-08-03");
  assert.equal(url.searchParams.get("wt_to"), "2026-09-15");
});

test("admin root route keeps explicit Control Tower deep links reachable", () => {
  assert.match(rootPageSource, /getAdminWebBootstrap/);
  assert.doesNotMatch(rootPageSource, /requireAdminWebPageContract\("control-tower"\)/);
  assert.match(rootPageSource, /route_id === "control-tower"/);
  assert.match(rootPageSource, /if \(requestedControlTower && controlTower\) \{\s*return <ControlTowerPage searchParams=\{sp\} pageContract=\{controlTower\} \/>;/);
  assert.match(rootPageSource, /if \(controlTower\?\.href && !landing\?\.href\) \{\s*redirect\(scopeHref\("\/", parseScope\(sp\), \{\}, \{ lens: CONTROL_TOWER_LENS \}\)\);/);
  assert.match(rootPageSource, /enabledPublished\.find\(\(item\) => item\.href === "\/verify"\)/);
  assert.match(rootPageSource, /redirect\(firstEnabledPublishedHref\(contract\) \?\? "\/vaccination"\)/);
});
