// r2-visual-audit plugin (guard: task-board-touch-dnd): on /tasks, drag a board card between
// columns for real -- the mouse at 1440, touch (CDP long-press, the 390 profile has hasTouch) at
// 390 -- and fail when the card does not lift onto a paper-backed DragOverlay, does not move, does
// not send the status write, or does not roll back when that write fails. The write is always
// held and aborted (scripts/lib/task-board-dnd-journey.mjs), so the shared API is never written.
//
// Runs in its own tab of the audit's context, so the scan page is left exactly as it was.
import { runBoardDragJourney } from "../lib/task-board-dnd-journey.mjs";

/** Findings that depend on the data (no card this actor may move) are reported, never P0. */
const DATA_PATTERNS = new Set(["no-draggable-card", "no-legal-move"]);

export default {
  name: "task-board-dnd",
  p0: true,
  profiles: ["1440-dark", "390-dark"],
  async run(page, ctx) {
    if (ctx.route !== "/tasks") return [];
    const tab = await page.context().newPage();
    try {
      await tab.goto(new URL("/tasks", page.url()).href, { waitUntil: "networkidle", timeout: 45000 });
      const { findings } = await runBoardDragJourney(tab, { mode: ctx.profile.mobile ? "touch" : "mouse" });
      return findings.map((f) => ({ ...f, p0: !DATA_PATTERNS.has(f.pattern) }));
    } finally {
      await tab.close();
    }
  },
};
