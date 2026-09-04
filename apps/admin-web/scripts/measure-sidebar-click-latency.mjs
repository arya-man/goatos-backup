import { chromium } from "@playwright/test";
import { writeFileSync } from "node:fs";

const baseUrl = process.env.ADMIN_WEB_URL || "http://127.0.0.1:3000";
const output = process.env.ADMIN_WEB_SIDEBAR_METRICS || "";
const rounds = Number.parseInt(process.env.ADMIN_WEB_SIDEBAR_ROUNDS || "3", 10);
const timeout = Number.parseInt(process.env.ADMIN_WEB_SIDEBAR_TIMEOUT_MS || "45000", 10);
const bearerToken = process.env.GOATOS_BEARER_TOKEN || "";

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

const routeReady = new Map([
  ["/action-center", /Action Center/i],
  ["/calendar", /Calendar/i],
  ["/protocol-adherence", /Protocol Adherence/i],
  ["/workflows", /Workflows/i],
  ["/approvals", /Approvals/i],
  ["/verify", /Verify/i],
  ["/counts/analytics", /Births and exits|Counts|Analytics/i],
  ["/counts/breakdown", /Breakdown|Counts/i],
  ["/counts/sops", /SOP/i],
  ["/weighing/analytics", /Weighing/i],
  ["/weighing/sops", /SOP/i],
  ["/sales", /Sales/i],
  ["/sales/loads", /Sales|Loads/i],
  ["/sales/config", /Sales|Config/i],
  ["/feed/config", /Feed|Config/i],
  ["/feed/analytics", /Feed|Analytics/i],
  ["/feed/sops", /SOP/i],
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

function pathnameOf(href) {
  return new URL(href, baseUrl).pathname;
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

async function waitUsable(page, expectedPath) {
  try {
    await page.waitForFunction(
      ({ expectedPath, readyPattern }) => {
        function visible(selector) {
          return [...document.querySelectorAll(selector)].some((element) => {
            const style = window.getComputedStyle(element);
            const rect = element.getBoundingClientRect();
            return style.display !== "none" && style.visibility !== "hidden" && rect.width > 0 && rect.height > 0;
          });
        }
        if (window.location.pathname !== expectedPath) return false;
        if (document.querySelector(".layout.route-pending")) return false;
        if (visible('[aria-busy="true"], .skel, .wt-tab-skeleton')) return false;
        if (document.body.innerText.includes("Loading")) return false;
        const pattern = new RegExp(readyPattern, "i");
        const headings = [...document.querySelectorAll("h1,h2,h3")].map((node) => node.textContent?.trim() || "");
        return headings.some((heading) => pattern.test(heading)) || pattern.test(document.body.innerText);
      },
      { expectedPath, readyPattern: (routeReady.get(expectedPath) ?? /./).source },
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
        loadingText: document.body.innerText.includes("Loading"),
        busy,
      };
    });
    throw new Error(`${expectedPath} did not become usable: ${JSON.stringify(state)}: ${error.message}`);
  }
}

async function clickSidebarRoute(page, route) {
  const expectedPath = pathnameOf(route);
  const link = page.locator(`aside.side a[href^="${expectedPath}"]`).first();
  const groups = page.locator("aside.side [role='button']");
  const count = await groups.count();
  for (let i = 0; i < count && ((await link.count()) === 0 || !(await link.isVisible().catch(() => false))); i += 1) {
    await groups.nth(i).click();
  }
  if ((await link.count()) === 0) throw new Error(`missing sidebar link for ${route}`);
  const started = performance.now();
  await Promise.all([
    page.waitForURL((url) => url.pathname === expectedPath, { timeout }),
    link.click(),
  ]);
  await waitUsable(page, expectedPath);
  const finished = performance.now();
  const problem = await pageProblem(page);
  if (problem.problem) throw new Error(`${route} rendered ${problem.problem}`);
  return { ms: Math.round(finished - started), h1: problem.h1 };
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
  const firstProblem = await pageProblem(page);
  if (firstProblem.problem) throw new Error(`initial page rendered ${firstProblem.problem}`);

  for (let round = 1; round <= rounds; round += 1) {
    for (const route of routes) {
      if (page.url() === `${baseUrl}${scoped(route)}`) {
        await page.goto(`${baseUrl}${scoped("/")}`, { waitUntil: "networkidle", timeout });
      }
      const row = { round, route, ...(await clickSidebarRoute(page, scoped(route))) };
      rows.push(row);
      console.log(JSON.stringify(row));
    }
  }
} finally {
  await browser.close();
}

if (output) {
  writeFileSync(output, `${rows.map((row) => JSON.stringify(row)).join("\n")}\n`);
}
