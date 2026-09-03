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
  await page.waitForFunction(
    ({ expectedPath }) =>
      window.location.pathname === expectedPath &&
      !document.querySelector(".layout.route-pending") &&
      !document.body.innerText.includes("Loading"),
    { expectedPath },
    { timeout },
  );
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
