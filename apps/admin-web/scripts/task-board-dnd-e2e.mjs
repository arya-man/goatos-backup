// E2E for the /tasks board drag (guard: task-board-touch-dnd): drags a card between columns at
// 1440 with the mouse and at 390 with touch emulation (hasTouch, CDP touch long-press), asserting
// the paper-backed DragOverlay, the move, the status write and its rollback. Read-only: the write
// is held and aborted (scripts/lib/task-board-dnd-journey.mjs).
//
// Run: TASKS_DND_BASE=http://127.0.0.1:3597 npm run e2e:task-board-dnd [-- --shots <dir>]
// (the visual gate runs the same journey on /tasks through scripts/r2-audit-checks/task-board-dnd.mjs).
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "@playwright/test";

import { runBoardDragJourney } from "./lib/task-board-dnd-journey.mjs";

const base = process.env.TASKS_DND_BASE || "http://127.0.0.1:3300";
const shotsAt = process.argv.indexOf("--shots");
const shots = shotsAt > 0 ? process.argv[shotsAt + 1] : null;
if (shots) mkdirSync(shots, { recursive: true });

const RUNS = process.env.TASKS_DND_DEBUG ? [{ mode: "mouse", width: 1440, height: 900, mobile: false }] : [
  { mode: "mouse", width: 1440, height: 900, mobile: false },
  { mode: "touch", width: 390, height: 844, mobile: true },
];

const browser = await chromium.launch();
let failed = 0;
try {
  for (const run of RUNS) {
    for (const theme of shots ? ["dark", "light"] : ["dark"]) {
      const ctx = await browser.newContext({
        viewport: { width: run.width, height: run.height },
        isMobile: run.mobile,
        hasTouch: run.mobile,
        colorScheme: theme,
      });
      await ctx.addInitScript((t) => { try { for (const k of ["mesha.shell.theme", "goatos-theme"]) localStorage.setItem(k, t); } catch {} }, theme);
      const page = await ctx.newPage();
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      await page.goto(`${base}/tasks`, { waitUntil: "networkidle", timeout: 60000 });
      const { findings, note } = await runBoardDragJourney(page, {
        mode: run.mode,
        shot: shots ? (name) => page.screenshot({ path: join(shots, `${name}-${run.width}-${theme}.png`) }) : undefined,
      });
      for (const e of errors) findings.push({ pattern: "pageerror", label: "Page runtime error", detail: e.slice(0, 160) });
      const tag = `${run.mode} ${run.width} ${theme}`;
      if (findings.length) {
        failed += findings.length;
        for (const f of findings) console.log(`FAIL ${tag}: ${f.label} -- ${f.detail}`);
      } else {
        console.log(`PASS ${tag}: ${note}`);
      }
      await ctx.close();
    }
  }
} finally {
  await browser.close();
}
process.exit(failed ? 1 : 0);
