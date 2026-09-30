#!/usr/bin/env node
// r2-skeleton-iou.mjs — skeleton vs loaded layout check (R2 item 7).
//
// For every route that has an app/(admin)/**/loading.tsx it captures TWO frames of the same hard
// load: the route skeleton (loading.tsx streamed into the real shell) and the settled page. It reads
// the bounding boxes of the page root's top-level blocks (page header, tabs, filter card, KPI row,
// chart cards, table card, card grid …) in both frames, pairs them in order, clips them to the first
// viewport (what the reader sees swap) and fails any pair whose IoU is below the threshold (0.8), and
// any route whose skeleton and page have a different number of visible top-level blocks.
//
// Holding the skeleton frame: pass --delay-control <url> for an API proxy that delays page data
// (GET <url>?ms=N; the shell bootstrap stays fast), or the lane falls back to CDP network throttling
// (--throttle-kbps, default 24), which streams the HTML slowly enough that the loading.tsx frame paints
// before the page content arrives.
//
//   node scripts/r2-skeleton-iou.mjs --base http://127.0.0.1:3498 [--only alerts,counts]
//        [--widths 1440,390] [--themes dark,light] [--out <dir>] [--threshold 0.8]
//        [--delay-control http://127.0.0.1:18298/__delay] [--throttle-kbps 24]
//
// Output: <out>/skeleton-iou.json (per route x width x theme: blocks, IoU, pass) and, per capture,
// <route>__<w>__<theme>__side.png (skeleton | loaded | overlay: skeleton boxes red dashed over the
// loaded frame with the loaded boxes green). Exit code 1 when any capture fails.
//
// Also exported as `auditSkeletonIoU(page, url, opts)` so r2-visual-audit.mjs can run it per route.

import { mkdirSync, readdirSync, statSync, writeFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const appDir = resolve(scriptDir, "..");
const THEME_STORAGE_KEYS = ["mesha.shell.theme", "goatos-theme"];

/** Dynamic segments resolve to the first matching link on a parent list page. */
const DYNAMIC_PARENTS = {
  "/goats/[goat_id]": ["/counts/herd", /^\/goats\/[^/?#]+$/],
  "/calendar/drive/[eventId]": ["/calendar", /^\/calendar\/drive\/[^/?#]+$/],
  "/workflows/[row_id]": ["/workflows", /^\/workflows\/[^/?#]+$/],
  "/procurement/source-entry/loads/[load_id]": ["/procurement/source-entry", /^\/procurement\/source-entry\/loads\/[^/?#]+$/],
  "/vaccination/execution/sheds/[shedId]": ["/vaccination", /^\/vaccination\/execution\/sheds\/[^/?#]+$/],
};

export function loadingRoutes() {
  const base = join(appDir, "app", "(admin)");
  const out = [];
  const walk = (dir) => {
    for (const name of readdirSync(dir)) {
      const p = join(dir, name);
      if (statSync(p).isDirectory()) walk(p);
      else if (name === "loading.tsx") out.push("/" + relative(base, dir).split("/").filter((s) => !/^\(.*\)$/.test(s)).join("/"));
    }
  };
  walk(base);
  return out.sort();
}

/** In-page: the page root's visible top-level blocks, viewport coordinates. */
function readBlocks() {
  const wrap = document.querySelector(".msh-wrap") || document.querySelector(".wrap");
  if (!wrap) return { busy: false, blocks: [] };
  const visible = (el) => {
    const r = el.getBoundingClientRect();
    const cs = getComputedStyle(el);
    return r.width > 1 && r.height > 1 && cs.display !== "none" && cs.visibility !== "hidden";
  };
  let root = [...wrap.children].find((el) => visible(el)) || null;
  while (root && [...root.children].filter(visible).length === 1 && !root.matches("[data-skel-root], [data-page-root], .screen, .kit-page, .pagegrid")) {
    root = [...root.children].find(visible);
  }
  if (!root) return { busy: false, blocks: [] };
  const busy = Boolean(root.closest('[aria-busy="true"]') || root.querySelector(':scope[aria-busy="true"]')) && Boolean(root.querySelector(".MuiSkeleton-root"));
  // `display: contents` wrappers are layout-transparent: their children are the blocks. Closed
  // off-canvas drawers (fixed, parked past the right edge) are not page blocks.
  // A skeleton block wrapped in OptionalSkeleton (display: contents, data-skel-optional) stands for a
  // block the page renders only when its data exists; the comparison may drop it.
  const flat = (el, optional = false) =>
    getComputedStyle(el).display === "contents"
      ? [...el.children].flatMap((child) => flat(child, optional || el.hasAttribute("data-skel-optional")))
      : [Object.assign(el, { __skelOptional: optional })];
  const onCanvas = (el) => {
    const r = el.getBoundingClientRect();
    return getComputedStyle(el).position !== "fixed" && r.x < innerWidth && r.x + r.width > 0;
  };
  const blocks = [...root.children].flatMap(flat).filter((el) => visible(el) && onCanvas(el)).map((el) => {
    const r = el.getBoundingClientRect();
    const kind =
      el.getAttribute("data-skel") ||
      (el.matches("header,[data-page-header]") ? "header" : el.querySelector(":scope > .MuiTabs-root, :scope.MuiTabs-root") ? "tabs" : el.matches(".MuiGrid-container") ? "grid" : el.matches(".MuiCard-root,.MuiPaper-root") ? "card" : el.tagName.toLowerCase());
    return { kind, x: r.x, y: r.y, w: r.width, h: r.height, optional: Boolean(el.__skelOptional) };
  });
  return { busy, blocks, vw: innerWidth, vh: innerHeight };
}

function clip(b, vw, vh) {
  const x0 = Math.max(0, b.x), y0 = Math.max(0, b.y);
  const x1 = Math.min(vw, b.x + b.w), y1 = Math.min(vh, b.y + b.h);
  return x1 > x0 && y1 > y0 ? { x: x0, y: y0, w: x1 - x0, h: y1 - y0 } : null;
}

export function iou(a, b) {
  if (!a && !b) return null;
  if (!a || !b) return 0;
  const ix = Math.max(0, Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x));
  const iy = Math.max(0, Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y));
  const inter = ix * iy;
  return inter / (a.w * a.h + b.w * b.h - inter);
}

/**
 * Pair skeleton and loaded blocks in order. When the page skipped blocks the skeleton marks optional
 * (rendered only with data), try every way of dropping that many optional skeleton blocks and keep
 * the pairing with the best worst-block IoU; the dropped ones are reported, not failed.
 */
export function compareBlocks(skeleton, loaded, opts) {
  const optionalIdx = skeleton.map((b, i) => (b.optional ? i : -1)).filter((i) => i >= 0);
  const extra = skeleton.length - loaded.length;
  if (extra <= 0 || optionalIdx.length < extra || optionalIdx.length > 10) return comparePairs(skeleton, loaded, opts, []);
  let best = null;
  const choose = (start, picked) => {
    if (picked.length === extra) {
      const drop = new Set(picked);
      const res = comparePairs(skeleton.filter((_, i) => !drop.has(i)), loaded, opts, picked);
      const worst = Math.min(1, ...res.pairs.map((p) => p.iou));
      if (!best || worst > best.worst) best = { worst, res };
      return;
    }
    for (let k = start; k < optionalIdx.length; k++) choose(k + 1, [...picked, optionalIdx[k]]);
  };
  choose(0, []);
  return best.res;
}

function comparePairs(skeleton, loaded, { vw, vh, threshold = 0.8 }, dropped) {
  const n = Math.max(skeleton.length, loaded.length);
  const pairs = [];
  for (let i = 0; i < n; i++) {
    const s = skeleton[i], l = loaded[i];
    const cs = s ? clip(s, vw, vh) : null, cl = l ? clip(l, vw, vh) : null;
    const score = iou(cs, cl);
    if (score === null) continue; // both below the fold
    pairs.push({ index: i, skeleton: s?.kind ?? null, loaded: l?.kind ?? null, iou: Math.round(score * 1000) / 1000, pass: score >= threshold, s: cs, l: cl });
  }
  const countMatch = skeleton.length === loaded.length;
  return { countMatch, pairs, droppedOptional: dropped, pass: countMatch && pairs.every((p) => p.pass) };
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

/**
 * One hard load of `url`: skeleton frame + loaded frame, block IoU. `hold` starts the slow path
 * before navigating and returns a function that ends it.
 */
export async function auditSkeletonIoU(page, url, { threshold = 0.8, hold, timeoutMs = 90_000 } = {}) {
  const release = hold ? await hold(page) : null;
  let skeleton = null, skeletonShot = null;
  const started = Date.now();
  await page.goto(url, { waitUntil: "commit", timeout: timeoutMs });
  while (Date.now() - started < timeoutMs) {
    const read = await page.evaluate(readBlocks).catch(() => null);
    if (read?.busy && read.blocks.length) {
      await sleep(150); // let the streamed shell and fonts settle one beat
      skeleton = await page.evaluate(readBlocks);
      if (skeleton.busy) {
        skeletonShot = await page.screenshot();
        break;
      }
    }
    await sleep(40);
  }
  if (release) await release();
  await page.waitForFunction(() => !document.querySelector('.msh-wrap [aria-busy="true"] .MuiSkeleton-root, .wrap [aria-busy="true"] .MuiSkeleton-root'), null, { timeout: timeoutMs }).catch(() => {});
  await page.waitForLoadState("networkidle", { timeout: timeoutMs }).catch(() => {});
  await sleep(1200);
  const loaded = await page.evaluate(readBlocks);
  const loadedShot = await page.screenshot();
  if (!skeleton) return { url, error: "skeleton frame never painted", loaded: loaded.blocks, loadedShot, pass: false };
  const cmp = compareBlocks(skeleton.blocks, loaded.blocks, { vw: loaded.vw, vh: loaded.vh, threshold });
  return { url, skeleton: skeleton.blocks, loaded: loaded.blocks, ...cmp, skeletonShot, loadedShot, vw: loaded.vw, vh: loaded.vh };
}

/** skeleton | loaded | overlay, composed in a blank page (no image deps). */
export async function sideBySide(browser, result, file) {
  if (!result.skeletonShot) return;
  const ctx = await browser.newContext({ viewport: { width: result.vw * 3 + 32, height: result.vh + 40 } });
  const p = await ctx.newPage();
  const img = (buf) => `data:image/png;base64,${buf.toString("base64")}`;
  const boxes = (list, color, dash) =>
    list.filter(Boolean).map((b) => `<div style="position:absolute;left:${b.x}px;top:${b.y}px;width:${b.w}px;height:${b.h}px;outline:2px ${dash} ${color};outline-offset:-2px"></div>`).join("");
  const labels = result.pairs.map((pr) => (pr.l ? `<div style="position:absolute;left:${pr.l.x + 4}px;top:${pr.l.y + 4}px;font:600 12px monospace;background:${pr.pass ? "#118d57" : "#b71d18"};color:#fff;padding:1px 4px">${pr.index}:${pr.iou}</div>` : "")).join("");
  await p.setContent(`<body style="margin:0;background:#888;display:flex;gap:16px;font:13px sans-serif">
    <div><div>skeleton</div><div style="position:relative"><img src="${img(result.skeletonShot)}" style="display:block;width:${result.vw}px"></div></div>
    <div><div>loaded</div><div style="position:relative"><img src="${img(result.loadedShot)}" style="display:block;width:${result.vw}px"></div></div>
    <div><div>overlay (red dashed = skeleton, green = loaded)</div><div style="position:relative"><img src="${img(result.loadedShot)}" style="display:block;width:${result.vw}px;opacity:.55">${boxes(result.pairs.map((x) => x.l), "#22c55e", "solid")}${boxes(result.pairs.map((x) => x.s), "#ef4444", "dashed")}${labels}</div></div>
  </body>`);
  await p.waitForTimeout(100);
  await p.screenshot({ path: file, fullPage: true });
  await ctx.close();
}

async function resolveDynamic(browser, base, route) {
  const parent = DYNAMIC_PARENTS[route];
  if (!parent) return null;
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const p = await ctx.newPage();
  await p.goto(base + parent[0], { waitUntil: "networkidle", timeout: 90_000 }).catch(() => {});
  const hrefs = await p.$$eval("a[href]", (as) => as.map((a) => a.getAttribute("href")));
  await ctx.close();
  return hrefs.map((h) => (h || "").split(/[?#]/)[0]).find((h) => parent[1].test(h)) ?? null;
}

function parseArgs(argv) {
  const a = {};
  for (let i = 0; i < argv.length; i++) {
    const k = argv[i];
    if (k.startsWith("--")) a[k.slice(2)] = argv[i + 1] && !argv[i + 1].startsWith("--") ? argv[++i] : true;
  }
  return a;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const base = String(args.base ?? process.env.GOATOS_ADMIN_WEB_BASE_URL ?? "http://127.0.0.1:3300").replace(/\/$/, "");
  const widths = String(args.widths ?? "1440,390").split(",").map(Number);
  const themes = String(args.themes ?? "dark,light").split(",");
  const threshold = Number(args.threshold ?? 0.8);
  const out = resolve(String(args.out ?? join(appDir, ".r2-skeleton-iou")));
  const only = args.only ? String(args.only).split(",") : null;
  const kbps = Number(args["throttle-kbps"] ?? 24);
  const delayControl = args["delay-control"] ? String(args["delay-control"]) : null;
  const delayMs = Number(args["delay-ms"] ?? 5000);
  mkdirSync(out, { recursive: true });

  const { chromium } = await import("@playwright/test");
  const browser = await chromium.launch();
  let routes = loadingRoutes().filter((r) => !only || only.some((o) => r.includes(o)));
  const resolved = [];
  for (const r of routes) {
    if (!r.includes("[")) resolved.push([r, r]);
    else {
      const path = await resolveDynamic(browser, base, r);
      resolved.push([r, path]);
    }
  }

  const hold = delayControl
    ? async () => {
        await fetch(`${delayControl}?ms=${delayMs}`);
        return async () => {
          await fetch(`${delayControl}?ms=0`);
        };
      }
    : async (page) => {
        const cdp = await page.context().newCDPSession(page);
        await cdp.send("Network.enable");
        await cdp.send("Network.emulateNetworkConditions", { offline: false, latency: 40, downloadThroughput: (kbps * 1024) / 8, uploadThroughput: 1_000_000 });
        return async () => {
          await cdp.send("Network.emulateNetworkConditions", { offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1 });
        };
      };

  const report = [];
  for (const [route, path] of resolved) {
    for (const w of widths) {
      for (const theme of themes) {
        const phone = w < 800;
        const name = `${route.replace(/^\//, "").replace(/[/[\]]+/g, "_") || "root"}__${w}__${theme}`;
        if (!path) {
          report.push({ route, width: w, theme, pass: false, error: "no concrete path for dynamic route" });
          continue;
        }
        const ctx = await browser.newContext({ viewport: { width: w, height: phone ? 844 : 900 }, isMobile: phone, hasTouch: phone, colorScheme: theme });
        await ctx.addInitScript(([keys, value]) => {
          try {
            for (const k of keys) localStorage.setItem(k, value);
          } catch {}
        }, [THEME_STORAGE_KEYS, theme]);
        const page = await ctx.newPage();
        let res;
        try {
          res = await auditSkeletonIoU(page, base + path, { threshold, hold });
        } catch (e) {
          res = { url: base + path, pass: false, error: String(e?.message ?? e) };
        }
        const file = join(out, `${name}__side.png`);
        await sideBySide(browser, res, file).catch(() => {});
        const { skeletonShot, loadedShot, ...rest } = res;
        report.push({ route, path, width: w, theme, ...rest, side: skeletonShot ? file : null });
        const worst = (rest.pairs ?? []).filter((p) => !p.pass).map((p) => `${p.index}:${p.skeleton}/${p.loaded}=${p.iou}`).join(" ");
        console.log(`${rest.pass ? "PASS" : "FAIL"} ${route} ${w} ${theme}${rest.error ? " " + rest.error : ""}${rest.countMatch === false ? ` blocks ${rest.skeleton?.length}/${rest.loaded?.length}` : ""}${worst ? " " + worst : ""}`);
        await ctx.close();
      }
    }
  }
  await browser.close();
  writeFileSync(join(out, "skeleton-iou.json"), JSON.stringify(report, null, 2));
  const failed = report.filter((r) => !r.pass);
  console.log(`\n${report.length - failed.length}/${report.length} captures pass (IoU >= ${threshold} per top-level block) -> ${join(out, "skeleton-iou.json")}`);
  process.exitCode = failed.length ? 1 : 0;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) await main();
