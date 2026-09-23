// Read-only overlay journeys for the live visual smoke.
//
// Each step opens ONE drawer / dialog / sheet / popover, proves it is really on screen and usable
// (not parked at translateX(100%), not clipped, not re-anchored by a filtered ancestor, not
// see-through, header not scrolled away, body children not squashed), screenshots it, and closes
// it again. Nothing here ever saves, approves, downloads or submits: every click goes through
// `assertReadOnlyClickTarget`, which refuses write-shaped labels.
//
// Selectors are taken from the component source (paths noted per step), never guessed.
import { join } from "node:path";
import { overlayHeaderOutOfView } from "./visible-break-rules.mjs";

/** Labels that must never be clicked by the smoke. */
export const WRITE_LABEL_PATTERN = /save|approve|reject|delete|retire|submit|upload|download|export|assign|mark|confirm|create|add\b/i;

/**
 * Pure guard: returns a refusal reason, or null when the target is safe to click.
 * `allowDialogTrigger` lets a step open a drawer whose trigger is labelled e.g. "Download" ONLY when
 * the target is a link (<a>) that declares aria-haspopup="dialog" -- it opens a panel, it cannot
 * submit or fetch a file itself.
 */
export function readOnlyClickRefusal({ text = "", ariaLabel = "", tagName = "", ariaHaspopup = "" } = {}, { allowDialogTrigger = false } = {}) {
  const label = `${String(text)} ${String(ariaLabel)}`.replace(/\s+/g, " ").trim();
  if (!WRITE_LABEL_PATTERN.test(label)) return null;
  if (allowDialogTrigger && String(tagName).toLowerCase() === "a" && String(ariaHaspopup).toLowerCase() === "dialog") return null;
  return `refused write-shaped click target "${label.slice(0, 80)}"`;
}

async function describeTarget(locator) {
  return locator.evaluate((el) => ({
    text: (el.innerText || el.textContent || "").trim().slice(0, 200),
    ariaLabel: el.getAttribute("aria-label") || "",
    tagName: el.tagName,
    ariaHaspopup: el.getAttribute("aria-haspopup") || "",
  }));
}

export async function assertReadOnlyClickTarget(locator, options = {}) {
  const refusal = readOnlyClickRefusal(await describeTarget(locator), options);
  if (refusal) throw new Error(refusal);
}

const DRAWER = { overlay: "aside.drawer.on", header: ".dh", body: ".dc" };

/**
 * routeName -> steps. Route names match smoke-visual-live.mjs's route list.
 * step: { id, trigger (css), triggerText? (regex on the trigger's text), overlay, header?, body?,
 *         kind: "drawer"|"dialog"|"sheet"|"popover"|"nav", viewports?, close?: css, allowDialogTrigger?,
 *         minWidthRatio?, source }
 */
export const overlayJourneys = {
  tasks: [
    {
      id: "card-detail-drawer",
      // features/leadership-tasks/task-board-card.tsx: <Link className="ltb-card"> ; drawer: task-detail-drawer.tsx
      trigger: "a.ltb-card",
      overlay: "aside.drawer.on.ltd-drawer",
      header: ".ltd-head",
      kind: "drawer",
      close: "button.ltd-scrim",
      source: "features/leadership-tasks/task-board-card.tsx, task-detail-drawer.tsx, task-detail-panel.tsx",
    },
    {
      id: "filter-sheet",
      // features/leadership-tasks/leadership-tasks-filters.tsx: .btn.lt-fmore toggles .lt-fgroup.open (fixed bottom sheet <=760px)
      trigger: ".lt-fsheet-host button.lt-fmore",
      overlay: ".lt-fsheet-host .lt-fgroup.open",
      header: ".lt-fsheet-hd",
      kind: "sheet",
      viewports: ["mobile"],
      close: ".lt-fgroup.open .lt-fsheet-hd button",
      source: "features/leadership-tasks/leadership-tasks-filters.tsx; app/mesha-theme.css .lt-fgroup.open",
    },
  ],
  "work-board-populated": [
    {
      id: "issue-dialog",
      // features/work-board/work-board-board.tsx: <LocalOverlayLink className="card" data-filter-row>; modal: work-board-modal.tsx
      trigger: "a.card[data-filter-row]",
      overlay: ".wb-modal.on[role=dialog]",
      header: ".mh",
      body: ".mb",
      kind: "dialog",
      close: ".wb-modal.on .mh button.ib",
      source: "features/work-board/work-board-board.tsx, work-board-modal.tsx",
    },
  ],
  people: [
    {
      id: "person-access-modal",
      // features/people/person-access-launcher.tsx: button.btn.sm.ghost aria-label="Access — <name>" ; modal: person-access-modal.tsx
      trigger: 'button.btn.sm.ghost[aria-label^="Access"]',
      overlay: ".vr-modal.on[role=dialog]:has(.vr-modal-hd)",
      header: ".vr-modal-hd",
      body: ".vr-modal-bd",
      kind: "dialog",
      close: ".vr-modal.on .vr-modal-hd button.x",
      source: "features/people/person-access-launcher.tsx, person-access-modal.tsx",
    },
  ],
  "action-center": [
    {
      id: "notification-bell-panel",
      // features/notifications/notification-bell.tsx: button.iconbtn[aria-haspopup=dialog] -> .parkmenu.on[role=dialog] (position:fixed)
      trigger: 'button.iconbtn[aria-haspopup="dialog"][aria-expanded]',
      overlay: ".parkmenu.on[role=dialog]",
      kind: "popover",
      source: "features/notifications/notification-bell.tsx; components/mesha-shell.tsx NotificationBell",
    },
  ],
  "counts-breakdown": [
    {
      id: "tag-editor-popover",
      // features/counts/inline-cell-editor.tsx: button.tagedit-value -> portal .tagedit-pop[role=dialog]. Escape only; never confirm.
      trigger: "button.tagedit-value",
      overlay: ".tagedit-pop[role=dialog]",
      kind: "popover",
      source: "features/counts/inline-cell-editor.tsx",
    },
  ],
  "procurement-vendors": [
    {
      id: "vendor-row-drawer",
      // features/procurement/vendor-board.tsx: <LocalOverlayLink href=?vendor=<id> className="celllink">; drawer: vendor-local-drawer.tsx
      trigger: 'a.celllink[href*="vendor="]:not([href*="vendor=new"])',
      ...DRAWER,
      kind: "drawer",
      source: "features/procurement/vendor-board.tsx, vendor-local-drawer.tsx",
    },
  ],
  vaccination: [
    {
      id: "closed-no-dose-drawer",
      // features/preventive-care-vaccination/command-board-view.tsx: .kpi.kpi-clickable[role=button] (Closed, No Dose tile) -> .dscrim.on > aside.drawer.on
      trigger: '.kpi.kpi-clickable[role="button"]',
      triggerText: /closed/i,
      overlay: ".dscrim.on aside.drawer.on[role=dialog]",
      header: ".dh",
      kind: "drawer",
      source: "features/preventive-care-vaccination/command-board-view.tsx (tile ~L871, drawer ~L1623)",
    },
  ],
  "configuration-items": [
    {
      id: "item-row-drawer",
      // features/configuration/items-page.tsx: <LocalOverlayLink className="cfg-row-link"> ; LocalOverlayDrawer (components/local-overlay-drawer.tsx)
      trigger: "a.cfg-row-link",
      ...DRAWER,
      kind: "drawer",
      source: "features/configuration/items-page.tsx, components/local-overlay-drawer.tsx",
    },
  ],
  routines: [
    {
      id: "routine-drawer",
      // features/pen-routines/routines-page.tsx: routine name cell -> <LocalOverlayLink href=?edit=<id>> (no class); LocalOverlayDrawer
      trigger: 'table.tbl a[href*="edit="]:not(.btn):not([href*="edit=new"])',
      ...DRAWER,
      kind: "drawer",
      source: "features/pen-routines/routines-page.tsx, components/local-overlay-drawer.tsx",
    },
  ],
  "weighing-weights": [
    {
      id: "download-drawer",
      // features/weighing/weights-export.tsx: <LocalOverlayLink className="btn sm" aria-haspopup="dialog">Download ; aside.drawer.on. Open/close only.
      trigger: 'a.btn.sm[aria-haspopup="dialog"][href*="wt_export=1"]',
      ...DRAWER,
      kind: "drawer",
      allowDialogTrigger: true,
      source: "features/weighing/weights-export.tsx, weights.tsx",
    },
  ],
  // The Assumptions drawer moved to /weighing/sops on 2026-09-19 (features/weighing/load-comparison-tab.tsx
  // comment; app/(admin)/weighing/sops/page.tsx). The fcr tab no longer renders it.
  "weighing-sops": [
    {
      id: "assumptions-drawer",
      // features/weighing/weights-assumptions.tsx: <LocalOverlayLink className="btn sm" aria-haspopup="dialog">Assumptions
      trigger: 'a.btn.sm[aria-haspopup="dialog"][href*="wt_assumptions=1"]',
      ...DRAWER,
      kind: "drawer",
      source: "features/weighing/weights-assumptions.tsx, app/(admin)/weighing/sops/page.tsx",
    },
  ],
  "weighing-analytics-weight": [
    {
      id: "weight-band-exits-drawer",
      // features/weighing/feed-weight-band-card.tsx: a.wt-feedband-tile-link href="#fb_exit=all"; feed-weight-band-table.tsx a.wt-feedband-gone
      trigger: 'a.wt-feedband-tile-link[href*="fb_exit="], a.wt-feedband-gone[href*="fb_exit="]',
      overlay: "aside.drawer.wt-feedband-drawer.on",
      header: ".dh",
      body: ".dc",
      kind: "drawer",
      source: "features/weighing/feed-weight-band-card.tsx, feed-weight-band-table.tsx, feed-weight-band-exits-drawer.tsx",
    },
  ],
  "control-tower": [
    {
      id: "mobile-nav",
      // components/mesha-shell.tsx: button.iconbtn.hamb toggles aside.side.open#side (<=860px: fixed, 100vw)
      trigger: "button.iconbtn.hamb",
      overlay: "aside#side.side.open",
      kind: "nav",
      viewports: ["mobile"],
      minWidthRatio: 0.98,
      close: "button.iconbtn.hamb",
      source: "components/mesha-shell.tsx (toggleNav, #side); app/mesha-theme.css .side @max-width:860px",
    },
  ],
};

async function describeTriggerName(target, step) {
  const described = await describeTarget(target).catch(() => null);
  const name = String(described?.ariaLabel || described?.text || "").trim();
  return name || step.id.replace(/-/g, " ");
}

/** Whatever the page put on screen instead of the panel: an error, a toast, a banner. */
async function messageShownInstead(page) {
  return page
    .evaluate(() => {
      const seen = [];
      for (const el of document.querySelectorAll('[role=alert], [role=status], .err, .error, .toast, .banner-error, .vr-err')) {
        if (el.getClientRects().length === 0) continue;
        const text = (el.textContent ?? "").trim().replace(/\s+/g, " ");
        if (text) seen.push(text.slice(0, 120));
      }
      return seen[0] ?? "";
    })
    .catch(() => "");
}

/** What is painted on top of a control that refused a press — in words, not selectors. */
async function coveringElementName(page, target) {
  const box = await target.boundingBox().catch(() => null);
  if (!box) return "";
  return page
    .evaluate(
      ({ x, y }) => {
        const hit = document.elementFromPoint(x, y);
        if (!hit) return "";
        const named = hit.closest("[aria-label], [role=dialog], [role=alertdialog]") ?? hit;
        const label = named.getAttribute?.("aria-label") || (named.textContent ?? "").trim().slice(0, 40);
        return label ? `"${label}"` : "";
      },
      { x: Math.round(box.x + box.width / 2), y: Math.round(box.y + box.height / 2) },
    )
    .catch(() => "");
}

/** Runs inside the page. Returns { issues: string[], offenders: number, headerFacts } and outlines offenders when `mark`. */
function inspectOverlayInPage({ overlay, header, body, kind, minWidthRatio, mark, headerVerdict }) {
  const issues = [];
  const offenders = [];
  let headerFacts = null;
  let headerNode = null;
  const el = [...document.querySelectorAll(overlay)].find((node) => node.getClientRects().length > 0) || document.querySelector(overlay);
  if (!el) return { issues: [`overlay ${overlay} not mounted`], offenders: 0, headerFacts: null };
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  const r = el.getBoundingClientRect();
  const cs = getComputedStyle(el);
  const flag = (node, msg) => {
    issues.push(msg);
    if (node) offenders.push(node);
  };
  if (r.width < 2 || r.height < 2 || cs.visibility === "hidden" || cs.display === "none" || Number(cs.opacity) < 0.5) {
    flag(el, `not visible (size ${Math.round(r.width)}x${Math.round(r.height)}, visibility ${cs.visibility}, opacity ${cs.opacity})`);
  } else {
    const ix = Math.max(0, Math.min(r.right, vw) - Math.max(r.left, 0));
    const iy = Math.max(0, Math.min(r.bottom, vh) - Math.max(r.top, 0));
    const ratio = (ix * iy) / (r.width * r.height);
    if (ratio < 0.9) flag(el, `only ${Math.round(ratio * 100)}% inside the viewport (rect ${Math.round(r.left)},${Math.round(r.top)} ${Math.round(r.width)}x${Math.round(r.height)})`);
    if ((kind === "sheet" || kind === "nav") && (r.top < -1 || r.bottom > vh + 1)) flag(el, `sheet off-screen: top ${Math.round(r.top)}px bottom ${Math.round(r.bottom)}px (viewport ${vh})`);
    if (minWidthRatio && r.width < vw * minWidthRatio) flag(el, `width ${Math.round(r.width)}px is not full width (${vw}px)`);
  }
  if (cs.transform && cs.transform !== "none") {
    const m = cs.transform.match(/matrix\(([^)]+)\)/);
    if (m) {
      const [, , , , tx, ty] = m[1].split(",").map(Number);
      if (Math.abs(tx) >= r.width * 0.5 || Math.abs(ty) >= r.height * 0.5) flag(el, `off-screen transform ${cs.transform}`);
    }
  }
  if (r.width > 2 && r.height > 2) {
    const cx = Math.min(vw - 1, Math.max(0, r.left + r.width / 2));
    const cy = Math.min(vh - 1, Math.max(0, r.top + r.height / 2));
    const hit = document.elementFromPoint(cx, cy);
    if (!hit || !el.contains(hit)) flag(el, `centre is covered by ${hit ? `${hit.tagName.toLowerCase()}.${String(hit.className).split(" ").slice(0, 2).join(".")}` : "nothing"}`);
  }
  // A fixed overlay under a filtered ancestor only matters if it actually lands off-screen.
  const selfBox = el.getBoundingClientRect();
  const fullyOnScreen = selfBox.top >= -1 && selfBox.left >= -1 && selfBox.right <= vw + 1 && selfBox.bottom <= vh + 1;
  if (cs.position === "fixed" && !fullyOnScreen) {
    for (let p = el.parentElement; p && p !== document.documentElement; p = p.parentElement) {
      const ps = getComputedStyle(p);
      const culprit =
        (ps.backdropFilter && ps.backdropFilter !== "none" && `backdrop-filter:${ps.backdropFilter}`) ||
        (ps.webkitBackdropFilter && ps.webkitBackdropFilter !== "none" && `backdrop-filter:${ps.webkitBackdropFilter}`) ||
        (ps.filter && ps.filter !== "none" && `filter:${ps.filter}`) ||
        (ps.transform && ps.transform !== "none" && `transform:${ps.transform}`);
      if (!culprit) continue;
      // Only a real bug when that ancestor does not itself cover the viewport (a full-screen fixed scrim is harmless).
      const pr = p.getBoundingClientRect();
      const coversViewport = pr.left <= 1 && pr.top <= 1 && pr.right >= vw - 1 && pr.bottom >= vh - 1;
      if (!coversViewport) flag(p, `position:fixed overlay is anchored to ancestor ${p.tagName.toLowerCase()}.${String(p.className).split(" ")[0]} (${culprit})`);
    }
  }
  const bg = cs.backgroundColor || "";
  const alpha = /rgba\([^)]*,\s*0\)$/.test(bg) || bg === "transparent";
  if (alpha && (!cs.backgroundImage || cs.backgroundImage === "none")) flag(el, `background is transparent (${bg})`);
  if (header) {
    const h = el.querySelector(header);
    if (!h) flag(el, `header ${header} missing`);
    else {
      const hr = h.getBoundingClientRect();
      // Facts only. overlayHeaderOutOfView decides in Node whether a reader can see the
      // title: the old rule demanded the title start within 48px of the panel's top edge,
      // which failed every drawer that puts a breadcrumb or an Edit/Close bar above it.
      let scrollerScrollTop = 0;
      for (let q = h; q && q !== el.parentElement; q = q.parentElement) {
        if (q.scrollHeight > q.clientHeight + 1 && q.scrollTop > scrollerScrollTop) scrollerScrollTop = q.scrollTop;
      }
      headerFacts = {
        overlayTop: r.top,
        headerTop: hr.top,
        headerBottom: hr.bottom,
        headerHeight: hr.height,
        viewportHeight: vh,
        scrollerScrollTop,
      };
      headerNode = h;
    }
  }
  const bodyEl = body ? el.querySelector(body) : el;
  if (body && !bodyEl) flag(el, `body ${body} missing`);
  if (bodyEl) {
    const kids = [...bodyEl.children].flatMap((c) => [c, ...c.children]);
    for (const kid of kids) {
      const ks = getComputedStyle(kid);
      if (!/hidden|clip/.test(ks.overflowY)) continue;
      if (kid.clientHeight === 0 && kid.scrollHeight === 0) continue;
      if (kid.scrollHeight > kid.clientHeight + 4 && /-webkit-box/.test(ks.display) === false) {
        flag(kid, `body child ${kid.tagName.toLowerCase()}.${String(kid.className).split(" ")[0]} collapsed to ${kid.clientHeight}px (content ${kid.scrollHeight}px)`);
        break;
      }
    }
  }
  // On the marking pass Node tells us what it decided about the title, so the red
  // outline lands on the same element the message talks about.
  if (headerVerdict && headerNode) {
    issues.push(headerVerdict);
    offenders.push(headerNode);
  }
  if (mark) for (const node of offenders) node.style.outline = "3px solid red";
  return { issues, offenders: offenders.length, headerFacts };
}

function applies(step, viewportLabel) {
  return !step.viewports || step.viewports.includes(viewportLabel);
}

async function pickTrigger(page, step) {
  let locator = page.locator(step.trigger);
  if (step.triggerText) locator = locator.filter({ hasText: step.triggerText });
  const count = Math.min(await locator.count(), 12);
  let refused = "";
  for (let i = 0; i < count; i += 1) {
    const candidate = locator.nth(i);
    if (!(await candidate.isVisible().catch(() => false))) continue;
    const refusal = readOnlyClickRefusal(await describeTarget(candidate), { allowDialogTrigger: step.allowDialogTrigger });
    if (refusal) {
      refused = refusal;
      continue;
    }
    return { target: candidate };
  }
  return { reason: count === 0 ? "trigger-absent" : refused ? "only-write-shaped-triggers" : "trigger-hidden" };
}

// Web-first: the page decides when the overlay is gone. waitForFunction re-evaluates on the
// browser's own animation frames, so a slow close waits exactly as long as it needs to and a fast
// one returns immediately -- neither is rounded up to a poll interval on our clock.
async function waitClosed(page, overlay, ms) {
  return page
    .waitForFunction(
      (sel) => ![...document.querySelectorAll(sel)].some((n) => n.getClientRects().length > 0 && getComputedStyle(n).visibility !== "hidden"),
      overlay,
      { timeout: ms },
    )
    .then(() => true, () => false);
}

export async function exerciseOverlays(page, { routeName, viewportLabel, screenshotDir, relativeToRepo = (p) => p }) {
  const steps = (overlayJourneys[routeName] ?? []).filter((step) => applies(step, viewportLabel));
  for (const step of steps) {
    const tag = `${routeName}:${step.id}`;
    const picked = await pickTrigger(page, step);
    if (!picked.target) {
      console.log(`overlay_skip=${tag}:${picked.reason}`);
      continue;
    }
    await picked.target.scrollIntoViewIfNeeded().catch(() => {});
    await assertReadOnlyClickTarget(picked.target, { allowDialogTrigger: step.allowDialogTrigger });
    const fail = async (what) => {
      await page.evaluate(inspectOverlayInPage, { ...step, mark: true, headerVerdict: headerVerdict || undefined }).catch(() => {});
      const issuesPath = join(screenshotDir, `${viewportLabel}-${routeName}-${step.id}-issues.png`);
      await page.screenshot({ path: issuesPath }).catch(() => {});
      console.log(`screenshot_path=${relativeToRepo(issuesPath)}`);
      throw new Error(`${routeName} ${viewportLabel} overlay ${step.id}: ${what}`);
    };
    // A raw Playwright timeout reads "locator.click: Timeout 5000ms exceeded." and names
    // nothing — not the page, not the control, not the reason. Say which control on which
    // page, and try once more after the page has settled so a board that was still
    // re-rendering is not reported as a control nobody can press.
    const triggerName = await describeTriggerName(picked.target, step);
    let clicked = await picked.target.click({ timeout: 5_000 }).then(() => true, () => false);
    if (!clicked) {
      await page.waitForLoadState("networkidle", { timeout: 5_000 }).catch(() => {});
      clicked = await picked.target.click({ timeout: 5_000 }).then(() => true, () => false);
    }
    let headerVerdict = "";
    if (!clicked) {
      const blocker = await coveringElementName(page, picked.target);
      await fail(blocker ? `"${triggerName}" cannot be pressed: ${blocker} is on top of it` : `"${triggerName}" did not respond to a press`);
    }
    const opened = await page.locator(step.overlay).first().waitFor({ state: "attached", timeout: 10_000 }).then(() => true, () => false);
    if (!opened) {
      // Say what the reader is left looking at. "never mounted after clicking
      // button.btn.sm.ghost[aria-label^=Access]" is a selector dump; "showed 'That person
      // is no longer on the roster.'" is the thing on the screen.
      const shown = await messageShownInstead(page);
      await fail(
        shown
          ? `did not open — pressing "${triggerName}" showed "${shown}" instead`
          : `did not open — pressing "${triggerName}" did nothing`,
      );
    }
    // Poll so open transitions settle; report the last inspection if it never becomes healthy.
    let result = { issues: ["not inspected"] };
    const deadline = Date.now() + 3_000;
    while (Date.now() < deadline) {
      result = await page.evaluate(inspectOverlayInPage, { ...step, mark: false });
      headerVerdict = overlayHeaderOutOfView(result.headerFacts);
      if (headerVerdict) result = { ...result, issues: [...result.issues, headerVerdict] };
      if (result.issues.length === 0) break;
      // Re-inspect once the page has actually painted again. Two animation frames is the browser's
      // own signal that the open transition advanced; a fixed sleep is a guess at how long it takes.
      await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))));
    }
    if (result.issues.length > 0) await fail(result.issues.slice(0, 3).join("; "));
    const shotPath = join(screenshotDir, `${viewportLabel}-${routeName}-${step.id}.png`);
    await page.screenshot({ path: shotPath });
    console.log(`screenshot_path=${relativeToRepo(shotPath)}`);
    console.log(`overlay_ok=${tag}`);

    await page.keyboard.press("Escape").catch(() => {});
    let closed = await waitClosed(page, step.overlay, 1_500);
    if (!closed && step.close) {
      const closer = page.locator(step.close).first();
      if (await closer.isVisible().catch(() => false)) {
        await assertReadOnlyClickTarget(closer);
        await closer.click({ timeout: 5_000 }).catch(() => {});
        closed = await waitClosed(page, step.overlay, 2_000);
      }
    }
    if (!closed) await fail("did not close on Escape or its close button");
  }
}
