import { chromium } from "@playwright/test";

const baseUrl = (process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3318").replace(/\/$/, "");

const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage({ viewport: { width: 1512, height: 982 }, deviceScaleFactor: 1 });

try {
  await page.goto(`${baseUrl}/herd-signals?scope_mode=company`, { waitUntil: "domcontentloaded", timeout: 30_000 });
  const rows = page.locator("table.herd-signals-table tbody tr");
  await rows.first().waitFor({ state: "visible", timeout: 15_000 });

  let rowIndex = -1;
  let animal = "";
  const rowCount = await rows.count();
  for (let index = 0; index < rowCount; index += 1) {
    const text = (await rows.nth(index).locator("td[data-l='Animal']").innerText()).trim();
    if (/^\d{12,}/.test(text)) {
      rowIndex = index;
      animal = text;
      break;
    }
  }
  if (rowIndex < 0) throw new Error("No RFID-first live row found");

  const row = rows.nth(rowIndex);
  const smartTag = (await row.locator("td[data-l='Smart tag'] .mono").first().textContent())?.trim() ?? "";

  for (let attempt = 1; attempt <= 2; attempt += 1) {
    await row.click({ position: { x: 18, y: 18 } });
    await page.waitForSelector("aside.drawer.on", { timeout: 5_000 });
    const title = (await page.locator("aside.drawer.on .dh b").first().innerText()).trim();
    if (!title.startsWith(`${animal.split("\n")[0]} · ${smartTag}`)) {
      throw new Error(`Attempt ${attempt} opened wrong drawer title: ${JSON.stringify(title)}`);
    }
    await page.locator("aside.drawer.on button[aria-label='Close tag detail']").click();
    await page.waitForSelector("aside.drawer.on", { state: "detached", timeout: 5_000 }).catch(async () => {
      await page.waitForSelector("aside.drawer.on", { state: "hidden", timeout: 5_000 });
    });
  }

  console.log(
    JSON.stringify(
      {
        animal,
        smartTag,
        rowIndex,
        repeatedClicksOpenedDrawer: true,
        url: page.url(),
      },
      null,
      2,
    ),
  );
} finally {
  await browser.close();
}
