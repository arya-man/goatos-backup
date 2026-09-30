// The /tasks board drag journey, in a real browser (guard: task-board-touch-dnd).
//
// Ravi 2026-09-30: touch-dragging a card did nothing at 390 (the board used HTML5 drag, which
// touch browsers never fire), and a dragged card had no background (the browser's transparent
// snapshot of the link). The board is dnd-kit now (features/leadership-tasks/task-board-dnd.tsx);
// this journey proves it end to end with the REAL input devices:
//   - mouse (1440): press a card, travel, hover a legal column, release;
//   - touch (390, hasTouch): a CDP touch long-press (> the 200ms activation delay), then drag the
//     finger to the edge so dnd-kit auto-scrolls the track to a legal column, then lift.
// Mid-drag it asserts the DragOverlay is the paper-backed card (opaque background + a shadow) and
// the source slot keeps the template `--dragging` placeholder. After the drop it asserts the card
// MOVED to the target column (the optimistic move), that the drop sent the SAME status write
// (the server action carrying task_id + status), and -- because the audit API is shared and
// read-only -- the write is held and then ABORTED, so the journey also proves the rollback: the
// card returns to its column and the refusal sentence shows. Nothing is ever persisted.
// Touch also proves a TAP still opens the card (the drawer's ?task= in the URL).
//
// Used by scripts/r2-audit-checks/task-board-dnd.mjs (the visual gate, on /tasks) and by
// scripts/task-board-dnd-e2e.mjs (`npm run e2e:task-board-dnd`).

export const CARD = '.ltb-card-root[data-board-draggable="true"]';
export const OVERLAY_CARD = ".ltb-drag-overlay .ltb-card-root.--overlay";

/** Opaque = alpha 1 on a computed `rgb()/rgba()/color()` background. */
export function isOpaqueColor(value) {
  const s = String(value || "").trim();
  if (!s || s === "transparent") return false;
  const rgba = /^rgba?\(([^)]+)\)$/.exec(s);
  if (rgba) {
    const parts = rgba[1].split(/[\s,/]+/).filter(Boolean);
    return parts.length < 4 || Number(parts[3]) >= 0.99;
  }
  const color = /^color\([^)]*?\/\s*([\d.]+)\s*\)$/.exec(s);
  if (color) return Number(color[1]) >= 0.99;
  return /^color\(/.test(s) || /^#[0-9a-f]{6}$/i.test(s);
}

/** True when a multipart / urlencoded server-action body carries `name=value`. */
export function bodyHasField(body, name, value) {
  const text = String(body || "");
  const multipart = new RegExp(`name="(?:_?\\d+_)?${name}"\\r?\\n\\r?\\n${value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\r?\\n`);
  if (multipart.test(text)) return true;
  try {
    return new URLSearchParams(text).get(name) === value;
  } catch {
    return false;
  }
}

const centre = (box) => ({ x: box.x + box.width / 2, y: box.y + Math.min(box.height / 2, 40) });

async function columnOf(page, href) {
  return page.evaluate((h) => {
    const a = [...document.querySelectorAll("a.ltb-card")].find((el) => el.getAttribute("href") === h);
    return a?.closest(".ltb-col")?.getAttribute("data-column") ?? null;
  }, href);
}

async function overlayState(page) {
  return page.evaluate(([overlaySel]) => {
    const card = document.querySelector(overlaySel);
    const source = document.querySelector(".ltb-card-root.--dragging");
    const legal = [...document.querySelectorAll(".ltb-col.ltb-drop-ok")].map((c) => c.getAttribute("data-column"));
    if (!card) return { overlay: false, source: Boolean(source), legal };
    const cs = getComputedStyle(card);
    return {
      overlay: true,
      background: cs.backgroundColor,
      shadow: cs.boxShadow,
      transform: cs.transform,
      text: (card.textContent || "").trim().slice(0, 40),
      source: Boolean(source),
      legal,
    };
  }, [OVERLAY_CARD]);
}

async function columnUnder(page, point) {
  return page.evaluate(({ x, y }) => {
    for (const el of document.elementsFromPoint(x, y)) {
      const col = el.closest(".ltb-col");
      if (col) return { key: col.getAttribute("data-column"), legal: col.classList.contains("ltb-drop-ok") };
    }
    return null;
  }, point);
}

/**
 * Block every server-action POST for the whole journey, so no drop can ever reach the API.
 * A POST is HELD until `release()` and then ABORTED (never continued); after `release()` a new
 * one aborts at once. The block is installed before the first pointer-down and removed only by
 * `dispose()` at the very end. (2026-09-30: an earlier version released the hold with
 * `page.unroute`, which let the held writes through to the shared API.)
 */
export async function blockServerActions(page) {
  const bodies = [];
  let open;
  const gate = new Promise((resolve) => { open = resolve; });
  const handler = async (route) => {
    const req = route.request();
    if (req.method() !== "POST" || !req.headers()["next-action"]) return route.fallback();
    bodies.push(req.postData() || "");
    await gate;
    await route.abort("failed").catch(() => {});
  };
  await page.route("**/*", handler);
  return {
    bodies,
    release: () => open(),
    async dispose() {
      open();
      await page.unroute("**/*", handler, { behavior: "wait" }).catch(() => {});
    },
  };
}

/**
 * Run the journey on a /tasks page that is already loaded. `mode` is "mouse" or "touch".
 * Returns `{ findings: [{ label, pattern, detail }], moved, note }`; `shot(name)` (optional) is
 * called mid-drag so a caller can screenshot the lifted card.
 */
export async function runBoardDragJourney(page, { mode, shot } = {}) {
  const findings = [];
  const fail = (pattern, label, detail = "") => findings.push({ pattern, label, detail });
  await page.waitForSelector(CARD, { timeout: 20000 }).catch(() => {});
  const cards = page.locator(CARD);
  if ((await cards.count()) === 0) {
    fail("no-draggable-card", "No draggable card on the /tasks board", "no card carries data-board-draggable=true");
    return { findings, moved: false };
  }
  const native = await page.locator('.ltb-cols [draggable="true"]').count();
  if (native) fail("native-drag", "A board card still uses native HTML5 drag", `${native} [draggable=true] in the board`);

  const block = await blockServerActions(page);
  try {
    return await journey(page, { mode, shot, fail, findings, cards, block });
  } finally {
    await block.dispose();
  }
}

async function journey(page, { mode, shot, fail, findings, cards, block }) {
  const cdp = mode === "touch" ? await page.context().newCDPSession(page) : null;
  const touch = (type, points) => cdp.send("Input.dispatchTouchEvent", { type, touchPoints: points.map((p) => ({ x: Math.round(p.x), y: Math.round(p.y) })) });
  const wait = (ms) => page.waitForTimeout(ms);

  // Try the cards in order until one offers a legal column (status_options is per actor).
  const count = Math.min(await cards.count(), 6);
  for (let i = 0; i < count; i += 1) {
    const root = cards.nth(i);
    await root.scrollIntoViewIfNeeded().catch(() => {});
    const href = await root.locator("a.ltb-card").getAttribute("href");
    const from = await columnOf(page, href);
    const box = await root.boundingBox();
    if (!box || !href) continue;
    const start = centre(box);

    // ---- pick up
    if (cdp) {
      await touch("touchStart", [start]);
      await wait(350);
      await touch("touchMove", [{ x: start.x + 2, y: start.y + 2 }]);
    } else {
      await page.mouse.move(start.x, start.y);
      await page.mouse.down();
      await page.mouse.move(start.x + 12, start.y + 4, { steps: 4 });
    }
    await wait(150);
    let state = await overlayState(page);
    const legal = state.legal.filter((key) => key !== from);
    if (!state.overlay || !legal.length) {
      if (cdp) await touch("touchEnd", []);
      else await page.mouse.up();
      await wait(250);
      if (!state.overlay) {
        fail(`${mode}-no-overlay`, `A ${mode} drag did not lift the card (no DragOverlay)`, `card ${href}`);
        return { findings, moved: false };
      }
      continue; // this task has no legal move for this actor; try the next card
    }

    // ---- carry it to a legal column (touch: to the edge, so the track auto-scrolls)
    const target = legal[0];
    let point = null;
    const columnBox = async () => page.locator(`.ltb-col[data-column="${target}"]`).boundingBox();
    const viewport = page.viewportSize();
    for (let step = 0; step < 40 && !point; step += 1) {
      const cb = await columnBox();
      const inView = cb && cb.x + 40 < viewport.width && cb.x + cb.width > 40;
      const aim = inView
        ? { x: Math.min(Math.max(cb.x + cb.width / 2, 30), viewport.width - 30), y: Math.min(cb.y + 60, viewport.height - 40) }
        : { x: cb && cb.x > viewport.width / 2 ? viewport.width - 8 : 8, y: start.y };
      if (cdp) await touch("touchMove", [aim]);
      else await page.mouse.move(aim.x, aim.y, { steps: 6 });
      await wait(inView ? 120 : 200);
      const under = await columnUnder(page, aim);
      if (inView && under?.key === target) point = aim;
    }
    state = await overlayState(page);
    if (shot) await shot(`${mode}-mid-drag`);
    if (!isOpaqueColor(state.background)) fail(`${mode}-overlay-transparent`, `The dragged card has no card background (${mode})`, `overlay background ${state.background}`);
    if (!state.shadow || state.shadow === "none") fail(`${mode}-overlay-no-shadow`, `The dragged card has no lift shadow (${mode})`, "box-shadow none");
    if (!state.text) fail(`${mode}-overlay-empty`, `The drag overlay is not the card (${mode})`, "overlay has no text");
    if (!state.source) fail(`${mode}-no-placeholder`, `The source slot shows no placeholder while dragging (${mode})`, "no .ltb-card-root.--dragging");
    if (!point) {
      if (cdp) await touch("touchEnd", []);
      else await page.mouse.up();
      fail(`${mode}-no-target`, `A ${mode} drag could not reach the legal column ${target}`, `from ${from}`);
      return { findings, moved: false };
    }

    // ---- drop, with the write held (the block has been in place since the start)
    const before = block.bodies.length;
    if (cdp) await touch("touchEnd", []);
    else await page.mouse.up();
    await wait(400);
    const after = await columnOf(page, href);
    const moved = after === target;
    if (!moved) fail(`${mode}-not-moved`, `A ${mode} drop did not move the card`, `expected ${target}, card is in ${after} (from ${from})`);
    const sent = block.bodies.slice(before).some((b) => bodyHasField(b, "status", target) && bodyHasField(b, "from_status", from));
    if (!sent) fail(`${mode}-no-write`, `A ${mode} drop did not send the status change`, `${block.bodies.length - before} server-action posts held`);
    // ---- rollback on error (the write is aborted: the shared API is read-only)
    block.release();
    await page.waitForSelector('[data-testid="ltb-refusal"]', { timeout: 10000 }).catch(() => {});
    await wait(300);
    const back = await columnOf(page, href);
    if (back !== from) fail(`${mode}-no-rollback`, `A refused ${mode} drop did not put the card back`, `card in ${back}, expected ${from}`);
    if ((await page.locator('[data-testid="ltb-refusal"]').count()) === 0) fail(`${mode}-no-refusal`, `A refused ${mode} drop showed no sentence`, "");

    // ---- touch: a TAP still opens the card
    if (cdp) {
      await root.scrollIntoViewIfNeeded().catch(() => {});
      const tb = await root.boundingBox();
      if (tb) {
        await page.touchscreen.tap(tb.x + tb.width / 2, tb.y + Math.min(tb.height / 2, 30));
        await page.waitForURL(/[?&]task=/, { timeout: 10000 }).catch(() => {});
        if (!/[?&]task=/.test(page.url())) fail("touch-tap-no-open", "A tap on a card no longer opens it", page.url());
      }
    }
    return { findings, moved, note: `${mode}: ${href} ${from} -> ${target}${moved ? " moved" : ""}, write held + aborted, rolled back to ${back}` };
  }
  fail("no-legal-move", "No card on the board offered a legal column to drag to", `${count} cards tried`);
  return { findings, moved: false };
}
