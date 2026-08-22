import { chromium } from "@playwright/test";

const baseUrl = (process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3318").replace(/\/$/, "");

const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage({ viewport: { width: 1512, height: 982 }, deviceScaleFactor: 1 });

try {
  await page.goto(`${baseUrl}/herd-signals?scope_mode=company`, { waitUntil: "domcontentloaded", timeout: 30_000 });
  const firstRow = page.locator("table.herd-signals-table tbody tr").first();
  await firstRow.waitFor({ state: "visible", timeout: 15_000 });

  const animal = (await firstRow.locator("td[data-l='Animal']").innerText()).trim();
  const smartTag = (await firstRow.locator("td[data-l='Smart tag'] .mono").first().textContent())?.trim() ?? "";
  if (!/^\d{12,}/.test(animal)) {
    throw new Error(`Animal column is not RFID-first: ${JSON.stringify(animal)}`);
  }

  await firstRow.click({ position: { x: 18, y: 18 } });
  await page.waitForSelector("aside.drawer.on", { timeout: 5_000 });

  const url = new URL(page.url());
  const selected = new URLSearchParams(url.hash.replace(/^#/, "")).get("hs_tag") ?? url.searchParams.get("hs_tag");
  if (smartTag && selected !== smartTag) {
    throw new Error(`Row click opened ${selected || "nothing"}, expected ${smartTag}`);
  }

  const title = (await page.locator("aside.drawer.on .dh b").first().innerText()).trim();
  if (!/^\d{12,}\s*·/.test(title)) {
    throw new Error(`Drawer title is not RFID-first: ${JSON.stringify(title)}`);
  }

  console.log(
    JSON.stringify(
      {
        animal,
        smartTag,
        selected,
        title,
        url: page.url(),
      },
      null,
      2,
    ),
  );
} finally {
  await browser.close();
}
