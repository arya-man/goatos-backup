import { chromium } from "@playwright/test";
import { writeFileSync } from "node:fs";

const baseUrl = process.env.ADMIN_WEB_URL || "http://127.0.0.1:3000";
const output = process.env.ADMIN_WEB_SIDEBAR_METRICS || "";
const rounds = Number.parseInt(process.env.ADMIN_WEB_SIDEBAR_ROUNDS || "3", 10);
const timeout = Number.parseInt(process.env.ADMIN_WEB_SIDEBAR_TIMEOUT_MS || "45000", 10);
const settleMs = Number.parseInt(process.env.ADMIN_WEB_SIDEBAR_SETTLE_MS || "0", 10);
const bearerToken = process.env.GOATOS_BEARER_TOKEN || "";
const onlyTabs = process.env.ADMIN_WEB_SIDEBAR_ONLY_TABS === "1";

const routes = [
  "/action-center",
  "/calendar",
  "/protocol-adherence",
  "/workflows",
  "/approvals",
  "/verify",
  "/counts/analytics",
  "/counts/breakdown",
  "/counts/sops",
  "/counts/sops?compose=1",
  "/weighing/analytics",
  "/weighing/sops",
  "/sales",
  "/sales/loads",
  "/sales/config",
  "/feed/config",
  "/feed/analytics",
  "/feed/sops",
  "/vaccination",
  "/vaccination/live-tracker",
  "/vaccination/plan",
  "/procurement/source-entry",
  "/procurement/vendors",
  "/procurement/feed-purchases",
  "/counts/milk-preparation",
  "/milk/sops",
  "/herd-signals",
  "/health/config",
  "/operations/audit",
  "/operations/dlq",
  "/people",
];

const tabClickFlows = [
  {
    route: "/feed/analytics",
    tabs: [
      { label: "Overview", href: "/feed/analytics", ready: /Figures show feed|Directed vs consumed|Feed stock/i },
      { label: "Stock", href: "/feed/analytics?tab=items", ready: /Feed stock|days left|latest load/i, search: { tab: "items" } },
      { label: "Per Animal", href: "/feed/analytics?tab=peranimal", ready: /Per animal|animal/i, search: { tab: "peranimal" } },
      { label: "Experiment", href: "/feed/analytics?tab=experiment", ready: /Experiment feed|wastage|prepared/i, search: { tab: "experiment" } },
      { label: "Execution", href: "/feed/analytics?tab=execution", ready: /Daily execution status|packing variance|distribution/i, search: { tab: "execution" } },
    ],
  },
  {
    route: "/weighing/analytics",
    tabs: [
      { label: "General", ready: /Average|daily gain|Pens|Sheds/i },
      { label: "Breed-wise", ready: /Breed|daily gain|average weight/i, search: { tab: "breed" } },
      { label: "Birth-wise", ready: /Birth|farm born|purchased/i, search: { tab: "birth" } },
      { label: "Pen-wise", ready: /Pen|daily gain|average weight/i, search: { tab: "shed" } },
      { label: "Weight-wise", ready: /Weight|weight band|average/i, search: { tab: "weight" } },
      { label: "Time-wise", ready: /Time|trend|daily gain/i, search: { tab: "time" } },
      { label: "Comparison", ready: /Comparison|load|purchased/i, search: { tab: "load" } },
    ],
  },
];

const routeReady = new Map([
  ["/action-center", /Action Center/i],
  ["/calendar", /Calendar/i],
  ["/protocol-adherence", /Protocol Adherence/i],
  ["/workflows", /Workflows/i],
  ["/approvals", /Approvals/i],
  ["/verify", /Verify/i],
  ["/counts/analytics", /Births and exits|Counts|Analytics/i],
  ["/counts/breakdown", /Breakdown|Counts/i],
  ["/counts/herd", /Herd|Counts/i],
  ["/counts/sops", /SOP/i],
  ["/counts/sops?compose=1", /SOP|Compose|Builder/i],
  ["/weighing/weights", /Weights|Weighing/i],
  ["/weighing/analytics", /Weighing/i],
  ["/weighing/sops", /SOP/i],
  ["/sales", /Sales/i],
  ["/sales/loads", /Sales|Loads/i],
  ["/sales/config", /Sales|Config/i],
  ["/feed/config", /Feed|Config/i],
  ["/feed/analytics", /Feed|Analytics/i],
  ["/feed/sops", /SOP/i],
  ["/feed/direction", /Feed|Direction/i],
  ["/feed/packing", /Feed|Packing/i],
  ["/vaccination", /Vaccination/i],
  ["/vaccination/live-tracker", /Live Tracker|Vaccination/i],
  ["/vaccination/plan", /Vaccination|Plan/i],
  ["/procurement/source-entry", /Procurement|Source/i],
  ["/procurement/vendors", /Procurement|Vendors/i],
  ["/procurement/feed-purchases", /Feed Purchases|Procurement/i],
  ["/counts/milk-preparation", /Milk Preparation/i],
  ["/milk/sops", /SOP/i],
  ["/herd-signals", /Herd Signals/i],
  ["/health/config", /Health|Config/i],
  ["/operations/audit", /Audit/i],
  ["/operations/dlq", /DLQ|Operations/i],
  ["/people", /People|HRMS/i],
]);

function scoped(path) {
  const url = new URL(path, baseUrl);
  if (!url.searchParams.has("scope_mode")) url.searchParams.set("scope_mode", "company");
  return `${url.pathname}${url.search}`;
}

function scopedWithScopeFirst(path) {
  const url = new URL(path, baseUrl);
  const next = new URLSearchParams();
  next.set("scope_mode", url.searchParams.get("scope_mode") || "company");
  for (const [key, value] of url.searchParams.entries()) {
    if (key !== "scope_mode") next.append(key, value);
  }
  return `${url.pathname}?${next.toString()}`;
}

function pathnameOf(href) {
  return new URL(href, baseUrl).pathname;
}

function urlOf(href) {
  return new URL(href, baseUrl);
}

function paramsEqual(left, right) {
  return normalizedParams(left) === normalizedParams(right);
}

function normalizedParams(params) {
  return [...params.entries()]
    .sort(([leftKey, leftValue], [rightKey, rightValue]) => leftKey.localeCompare(rightKey) || leftValue.localeCompare(rightValue))
    .map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(value)}`)
    .join("&");
}

async function pageProblem(page) {
  return page.evaluate(() => {
    const body = document.body?.innerText || "";
    const h1 = document.querySelector("h1,h2")?.textContent?.trim() || "";
    const problem = [
      "Admin-web contract unavailable",
      "authorization lookup failed",
      "Sign in with Google",
      "Application error",
      "internal error",
    ].find((needle) => body.includes(needle));
    return { h1, problem: problem || "" };
  });
}

async function waitUsable(page, expectedPath, expectedSearch = null) {
  try {
    await page.waitForFunction(
      ({ expectedPath, expectedSearchText, readyPattern }) => {
        function visible(selector) {
          return [...document.querySelectorAll(selector)].some((element) => {
            const style = window.getComputedStyle(element);
            const rect = element.getBoundingClientRect();
            return style.display !== "none" && style.visibility !== "hidden" && rect.width > 0 && rect.height > 0;
          });
        }
        if (window.location.pathname !== expectedPath) return false;
        if (expectedSearchText !== null) {
          const currentSearchText = [...new URLSearchParams(window.location.search).entries()]
            .sort(([leftKey, leftValue], [rightKey, rightValue]) => leftKey.localeCompare(rightKey) || leftValue.localeCompare(rightValue))
            .map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(value)}`)
            .join("&");
          if (currentSearchText !== expectedSearchText) return false;
        }
        if (document.querySelector(".layout.route-pending")) return false;
        if (visible('[aria-busy="true"], .skel, .wt-tab-skeleton')) return false;
        const bodyText = document.body?.innerText || "";
        if (bodyText.includes("Loading")) return false;
        const pattern = new RegExp(readyPattern, "i");
        const headings = [...document.querySelectorAll("h1,h2,h3")].map((node) => node.textContent?.trim() || "");
        return headings.some((heading) => pattern.test(heading)) || pattern.test(bodyText);
      },
      { expectedPath, expectedSearchText: expectedSearch === null ? null : normalizedParams(expectedSearch), readyPattern: (routeReady.get(expectedPath) ?? /./).source },
      { timeout },
    );
  } catch (error) {
    const state = await page.evaluate(() => {
      const busy = [...document.querySelectorAll('[aria-busy="true"], .skel, .wt-tab-skeleton')]
        .filter((element) => {
          const style = window.getComputedStyle(element);
          const rect = element.getBoundingClientRect();
          return style.display !== "none" && style.visibility !== "hidden" && rect.width > 0 && rect.height > 0;
        })
        .slice(0, 6)
        .map((element) => ({
          tag: element.tagName,
          className: element.getAttribute("class") || "",
          text: (element.textContent || "").trim().slice(0, 80),
        }));
      return {
        path: window.location.pathname,
        headings: [...document.querySelectorAll("h1,h2,h3")].map((node) => node.textContent?.trim() || "").slice(0, 8),
        pending: Boolean(document.querySelector(".layout.route-pending")),
        loadingText: (document.body?.innerText || "").includes("Loading"),
        busy,
      };
    });
    throw new Error(`${expectedPath} did not become usable: ${JSON.stringify(state)}: ${error.message}`);
  }
}

async function clickSidebarRoute(page, route) {
  const expectedUrl = urlOf(route);
  const expectedPath = expectedUrl.pathname;
  const groups = page.locator("aside.side [role='button']");
  const count = await groups.count();
  for (let i = 0; i < count && !(await hasVisibleSidebarLink(page, expectedUrl)); i += 1) {
    await groups.nth(i).click();
  }
  if (!(await hasVisibleSidebarLink(page, expectedUrl))) throw new Error(`missing sidebar link for ${route}`);
  const started = performance.now();
  await clickVisibleSidebarLink(page, expectedUrl);
  await page.waitForURL((url) => url.pathname === expectedPath && paramsEqual(url.searchParams, expectedUrl.searchParams), { timeout });
  await waitUsable(page, expectedPath, expectedUrl.searchParams);
  const finished = performance.now();
  const problem = await pageProblem(page);
  if (problem.problem) throw new Error(`${route} rendered ${problem.problem}`);
  return { ms: Math.round(finished - started), h1: problem.h1 };
}

async function hasVisibleSidebarLink(page, expectedUrl) {
  return page.locator("aside.side a").evaluateAll((anchors, expected) => anchors.some((anchor) => {
    const href = anchor.getAttribute("href") || "";
    const url = new URL(href, window.location.origin);
    const expectedSearchText = expected.search;
    const currentSearchText = [...url.searchParams.entries()]
      .sort(([leftKey, leftValue], [rightKey, rightValue]) => leftKey.localeCompare(rightKey) || leftValue.localeCompare(rightValue))
      .map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(value)}`)
      .join("&");
    if (url.pathname !== expected.pathname || currentSearchText !== expectedSearchText) return false;
    const style = window.getComputedStyle(anchor);
    const rect = anchor.getBoundingClientRect();
    return style.display !== "none" && style.visibility !== "hidden" && rect.width > 0 && rect.height > 0;
  }), { pathname: expectedUrl.pathname, search: normalizedParams(expectedUrl.searchParams) });
}

async function clickVisibleSidebarLink(page, expectedUrl) {
  const clicked = await page.locator("aside.side a").evaluateAll((anchors, expected) => {
    const anchor = anchors.find((candidate) => {
      const href = candidate.getAttribute("href") || "";
      const url = new URL(href, window.location.origin);
      const currentSearchText = [...url.searchParams.entries()]
        .sort(([leftKey, leftValue], [rightKey, rightValue]) => leftKey.localeCompare(rightKey) || leftValue.localeCompare(rightValue))
        .map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(value)}`)
        .join("&");
      if (url.pathname !== expected.pathname || currentSearchText !== expected.search) return false;
      const style = window.getComputedStyle(candidate);
      const rect = candidate.getBoundingClientRect();
      return style.display !== "none" && style.visibility !== "hidden" && rect.width > 0 && rect.height > 0;
    });
    if (!anchor) return false;
    anchor.click();
    return true;
  }, { pathname: expectedUrl.pathname, search: normalizedParams(expectedUrl.searchParams) });
  if (!clicked) throw new Error(`missing sidebar link for ${expectedUrl.pathname}${expectedUrl.search}`);
}

async function clickTab(page, flowRoute, tab) {
  const scopedHref = tab.href ? scoped(tab.href) : "";
  const started = performance.now();
  if (scopedHref) {
    const hrefs = [...new Set([scopedHref, scopedWithScopeFirst(tab.href)])];
    const selector = hrefs.map((href) => `a[href="${href}"]`).join(", ");
    let target = page.locator(selector).filter({ visible: true }).first();
    if ((await target.count()) === 0) {
      await page.goto(`${baseUrl}${flowRoute}`, { waitUntil: "networkidle", timeout });
      await waitUsable(page, pathnameOf(flowRoute));
      target = page.locator(selector).filter({ visible: true }).first();
    }
    if ((await target.count()) === 0) throw new Error(`missing visible tab link ${tab.label} (${hrefs.join(" or ")}) on ${flowRoute}`);
    await target.hover().catch(() => {});
    await target.click();
  } else {
    const control = page.getByRole("link", { name: new RegExp(`^${escapeRegExp(tab.label)}$`, "i") }).first();
    const button = page.getByRole("button", { name: new RegExp(`^${escapeRegExp(tab.label)}$`, "i") }).first();
    let target = (await control.count()) > 0 ? control : button;
    if ((await target.count()) === 0) {
      await page.goto(`${baseUrl}${flowRoute}`, { waitUntil: "networkidle", timeout });
      await waitUsable(page, pathnameOf(flowRoute));
      const retryControl = page.getByRole("link", { name: new RegExp(`^${escapeRegExp(tab.label)}$`, "i") }).first();
      const retryButton = page.getByRole("button", { name: new RegExp(`^${escapeRegExp(tab.label)}$`, "i") }).first();
      target = (await retryControl.count()) > 0 ? retryControl : retryButton;
    }
    if ((await target.count()) === 0) throw new Error(`missing tab control ${tab.label} on ${flowRoute}`);
    await target.click();
  }
  await page.waitForFunction(
    ({ expectedPath, expectedSearch, readyPattern }) => {
      if (window.location.pathname !== expectedPath) return false;
      const expectedEntries = Object.entries(expectedSearch || {});
      for (const [key, value] of expectedEntries) {
        if (new URLSearchParams(window.location.search).get(key) !== value) return false;
      }
      if (document.querySelector(".layout.route-pending")) return false;
      if ([...document.querySelectorAll('[aria-busy="true"], .skel, .wt-tab-skeleton')].some((element) => {
        const style = window.getComputedStyle(element);
        const rect = element.getBoundingClientRect();
        return style.display !== "none" && style.visibility !== "hidden" && rect.width > 0 && rect.height > 0;
      })) return false;
      const tabBars = [...document.querySelectorAll(".feed-tabbar, .wt-tabbar, .metricseg")];
      const bodyClone = document.body.cloneNode(true);
      for (const bar of tabBars) {
        const match = [...bodyClone.querySelectorAll(".feed-tabbar, .wt-tabbar, .metricseg")]
          .find((candidate) => candidate.textContent === bar.textContent);
        match?.remove();
      }
      return new RegExp(readyPattern, "i").test(bodyClone.textContent || "");
    },
    { expectedPath: pathnameOf(flowRoute), expectedSearch: tab.search || {}, readyPattern: tab.ready.source },
    { timeout },
  );
  const problem = await pageProblem(page);
  if (problem.problem) throw new Error(`${flowRoute} tab ${tab.label} rendered ${problem.problem}`);
  return { route: flowRoute, tab: tab.label, ms: Math.round(performance.now() - started), h1: problem.h1 };
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

const browser = await chromium.launch({ channel: "chrome", headless: true });
const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
if (bearerToken) {
  const url = new URL(baseUrl);
  await context.addCookies([
    {
      name: "goatos_firebase_id_token",
      value: bearerToken,
      domain: url.hostname,
      path: "/",
      httpOnly: true,
      sameSite: "Lax",
      expires: Math.floor(Date.now() / 1000) + 3600,
    },
  ]);
}
const page = await context.newPage();
const rows = [];

try {
  await page.goto(`${baseUrl}${scoped("/")}`, { waitUntil: "networkidle", timeout });
  await waitUsable(page, pathnameOf("/"));
  const firstProblem = await pageProblem(page);
  if (firstProblem.problem) throw new Error(`initial page rendered ${firstProblem.problem}`);

  for (let round = 1; round <= rounds; round += 1) {
    if (!onlyTabs) {
      for (const route of routes) {
        if (page.url() === `${baseUrl}${scoped(route)}`) {
          await page.goto(`${baseUrl}${scoped("/")}`, { waitUntil: "networkidle", timeout });
        }
        const started = performance.now();
        let row;
        try {
          row = { round, route, ok: true, ...(await clickSidebarRoute(page, scoped(route))) };
        } catch (error) {
          row = { round, route, ok: false, ms: Math.round(performance.now() - started), error: error.message };
        }
        rows.push(row);
        console.log(JSON.stringify(row));
      }
    }
    for (const flow of tabClickFlows) {
      await clickSidebarRoute(page, scoped(flow.route));
      if (settleMs > 0) await page.waitForTimeout(settleMs);
      for (const tab of flow.tabs) {
        const started = performance.now();
        let row;
        try {
          row = { round, type: "tab-click", ok: true, ...(await clickTab(page, scoped(flow.route), tab)) };
        } catch (error) {
          row = { round, type: "tab-click", ok: false, route: flow.route, tab: tab.label, ms: Math.round(performance.now() - started), error: error.message };
        }
        rows.push(row);
        console.log(JSON.stringify(row));
      }
    }
  }
} finally {
  await browser.close();
}

if (output) {
  writeFileSync(output, `${rows.map((row) => JSON.stringify(row)).join("\n")}\n`);
}
