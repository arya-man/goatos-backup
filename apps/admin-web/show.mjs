import { chromium } from "playwright";
const BASE="http://127.0.0.1:3414", SD=process.env.SD;
const pass=[], fail=[];
const check=(n,ok,d="")=> (ok?pass:fail).push(`${n}${d?" — "+d:""}`);
const b = await chromium.launch({ channel: "chrome" });
const p = await b.newPage({ viewport: { width: 1500, height: 1000 } });
const errs=[]; p.on("pageerror", e=>errs.push(e.message.slice(0,140)));

await p.goto(`${BASE}/health/config`, { waitUntil: "networkidle" });
await p.locator("tr", { hasText: "Anemia" }).first().getByRole("button", { name: /^Edit$/ }).click();
await p.waitForURL(/hc_version=/, { timeout: 45000 });
await p.waitForSelector("text=Add step", { timeout: 45000 });
await p.waitForTimeout(1200);

const med = p.locator('input[role="combobox"]').first();
await med.scrollIntoViewIfNeeded();

// HALF-TYPED: the list appears below the field.
await med.click();
await med.fill("");
await med.type("chl", { delay: 130 });
await p.waitForTimeout(700);
const list = p.locator('ul[role="listbox"]').first();
check("suggestions appear below after half typing", await list.isVisible(), (await med.getAttribute("aria-expanded")) ?? "");
const items = await list.locator('li[role="option"]').allInnerTexts();
check("it shows the matching medicine", items.some(t=>/Chlorpheniramine/.test(t)), items.map(t=>t.split("\n")[0]).join(" | "));
await p.screenshot({ path: `${SD}/type-1-suggest.png` });

// Keyboard: arrow down then Enter picks it.
await med.press("ArrowDown");
await med.press("Enter");
await p.waitForTimeout(600);
check("Enter picks the highlighted one", (await med.inputValue()).includes("heniramine"), await med.inputValue());
check("the list closes once chosen", !(await list.isVisible().catch(()=>false)));
check("the chosen medicine is accepted", (await med.getAttribute("aria-invalid")) !== "true");
await p.screenshot({ path: `${SD}/type-2-picked.png` });

// A partial that matches several.
await med.fill("bel");
await p.waitForTimeout(700);
const items2 = await p.locator('ul[role="listbox"] li[role="option"]').allInnerTexts();
check("a shorter prefix offers several", items2.length >= 2, items2.map(t=>t.split("\n")[0]).join(" | "));
check("no warning while still choosing", !(await p.locator("text=Not in this farm's medicine list").isVisible().catch(()=>false)));
await p.screenshot({ path: `${SD}/type-3-multi.png` });

// But something that matches nothing IS flagged.
await med.fill("Paracetamol syrup");
await p.waitForTimeout(900);
check("a medicine the farm does not stock is flagged", await p.locator("text=Not in this farm's medicine list").isVisible());
await p.screenshot({ path: `${SD}/type-4-unknown.png` });

console.log("\nPASS"); for (const t of pass) console.log("  ok   " + t);
if (fail.length) { console.log("\nFAIL"); for (const t of fail) console.log("  FAIL " + t); }
console.log("page errors:", errs.length?errs:"none");
await b.close(); process.exit(fail.length?1:0);
