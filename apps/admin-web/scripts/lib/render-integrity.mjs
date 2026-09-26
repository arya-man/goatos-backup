// render-integrity.mjs — shared "did it actually render properly" probe for the visual lanes.
//
// The design contract (docs/design/README.md, .agents/skills/design-system) says web and
// mobile must render properly: no breaking graphs, tags, numbers. A pixel diff only proves
// "same as last time"; this probe proves the page is not broken in the first place, so a
// baseline can never be an approved picture of a broken screen. Used by
// smoke-stories-visual.mjs (every story) and smoke-routes-visual.mjs (every route).
//
// Runs INSIDE the page (page.evaluate(RENDER_INTEGRITY_PROBE, options)) and returns findings:
//   { check, target, detail }
// Checks:
//   page-horizontal-overflow     documentElement.scrollWidth > viewport width
//   element-overflows-viewport   a visible element escapes the viewport horizontally with no
//                                horizontally scrollable ancestor to pan it
//   sibling-overlap              two in-flow siblings of a row/grid container whose boxes intersect by
//                                >4px on both axes (a field drawn under its neighbour); absolutely/fixed
//                                positioned decorations (badges, watermarks, popovers) are excluded
//   clipped-text                 text inside an overflow:hidden|clip box whose scrollWidth exceeds
//                                its clientWidth (ellipsis with title/aria-label is the only escape)
//   chart-svg-empty              an <svg> of chart size (>= 48x32) with zero rendered size or no
//                                path/rect/circle/line/polyline/polygon inside it
//   bad-text                     "NaN", "undefined", "null", "[object", "Infinity" rendered as text
//   f2-literal                   the legacy "F2" lifecycle code rendered as text
//   raw-float                    a table cell / KPI value with > 2 decimals (formatter bypassed)
//   blank-card                   a card/section with a visible title and nothing rendered under it
//   font-not-loaded              Public Sans or Barlow not resolvable via document.fonts.check
//   console-error                (collected by the caller, see collectConsoleErrors)
import { createHash } from "node:crypto";

export const RENDER_INTEGRITY_CHECKS = [
  "page-horizontal-overflow",
  "element-overflows-viewport",
  "clipped-text",
  "chart-svg-empty",
  "bad-text",
  "f2-literal",
  "raw-float",
  "blank-card",
  "font-not-loaded",
  "console-error",
  "double-skeleton",
  "blank-frame",
  "sibling-overlap",
];

export const RENDER_INTEGRITY_PROBE = ({ viewportWidth, fonts = ["Public Sans", "Barlow"], requireFonts = true, textRoot = "body" } = {}) => {
  const findings = [];
  const seen = new Set();
  // One finding per (check, target) per capture; the target is selector-shaped (no text) so
  // data-driven pages produce stable waiver keys instead of one key per row of data.
  const push = (check, target, detail) => {
    const key = `${check}|${target}`;
    if (seen.has(key)) return;
    seen.add(key);
    findings.push({ check, target: String(target).slice(0, 160), detail: String(detail ?? "").slice(0, 200) });
  };
  const describe = (el) => {
    const id = el.id ? `#${el.id}` : "";
    const cls = typeof el.className === "string" && el.className ? `.${el.className.trim().split(/\s+/).slice(0, 3).join(".")}` : "";
    const parent = el.parentElement && el.parentElement !== document.body ? `${el.parentElement.tagName.toLowerCase()}${typeof el.parentElement.className === "string" && el.parentElement.className ? `.${el.parentElement.className.trim().split(/\s+/)[0]}` : ""} > ` : "";
    return `${parent}${el.tagName.toLowerCase()}${id}${cls}`;
  };
  const sample = (el) => (el.textContent ?? "").trim().replace(/\s+/g, " ").slice(0, 40);
  const visible = (el) => {
    if (typeof el.checkVisibility === "function" && !el.checkVisibility()) return false;
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.visibility === "hidden" || Number(cs.opacity) === 0) return false;
    const r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0;
  };
  const width = viewportWidth ?? window.innerWidth;
  const doc = document.documentElement;

  // 1. page-level sideways scroll
  if (doc.scrollWidth > width + 1) push("page-horizontal-overflow", "documentElement", `scrollWidth ${doc.scrollWidth} > viewport ${width}`);

  const hasScrollableAncestor = (el) => {
    for (let n = el.parentElement; n && n !== document.body; n = n.parentElement) {
      const ox = getComputedStyle(n).overflowX;
      if ((ox === "auto" || ox === "scroll") && n.scrollWidth > n.clientWidth) return true;
    }
    return false;
  };

  const all = Array.from(document.querySelectorAll(`${textRoot} *`));
  let overflowReports = 0;
  let clipReports = 0;
  for (const el of all) {
    // MUI's TouchRipple is an overflow:hidden decoration layer whose pulsate child is drawn larger
    // than its button on purpose; it holds no text and is not layout.
    if (el.closest("[data-render-integrity-ignore], .sb-errordisplay, #storybook-docs, .MuiTouchRipple-root")) continue;
    const tag = el.tagName.toLowerCase();
    if (tag === "script" || tag === "style" || tag === "svg" || el.namespaceURI === "http://www.w3.org/2000/svg") continue;
    if (!visible(el)) continue;
    const r = el.getBoundingClientRect();
    const cs = getComputedStyle(el);
    // 2. element escapes the viewport (fixed/absolute overlays that are intentionally off-canvas are skipped)
    if (overflowReports < 12 && r.right > width + 1 && r.left < width && cs.position !== "fixed" && !hasScrollableAncestor(el)) {
      const isOffCanvasDrawer = cs.position === "absolute" && (cs.transform !== "none" || Number(cs.opacity) < 1);
      if (!isOffCanvasDrawer) {
        overflowReports += 1;
        push("element-overflows-viewport", describe(el), `right ${Math.round(r.right)} > ${width} "${sample(el)}"`);
      }
    }
    // 3. clipped text
    if (clipReports < 12 && (cs.overflowX === "hidden" || cs.overflowX === "clip" || cs.overflow === "hidden")) {
      const ownText = Array.from(el.childNodes).some((n) => n.nodeType === 3 && n.textContent.trim().length > 0);
      const inlineOnly = ownText || (el.children.length > 0 && Array.from(el.children).every((c) => /^(span|b|strong|em|i|small|abbr|time|code)$/i.test(c.tagName)));
      const lineClamp = cs.webkitLineClamp && cs.webkitLineClamp !== "none";
      const visuallyHidden = el.matches(".sr-only, .visually-hidden") || (el.clientWidth <= 1 && el.clientHeight <= 1) || cs.clip !== "auto" || cs.clipPath !== "none";
      if (inlineOnly && !lineClamp && !visuallyHidden && el.scrollWidth > el.clientWidth + 2) {
        const ellipsisWithFullText = cs.textOverflow === "ellipsis" && (el.getAttribute("title") || el.getAttribute("aria-label"));
        if (!ellipsisWithFullText) {
          clipReports += 1;
          push("clipped-text", describe(el), `scrollWidth ${el.scrollWidth} > clientWidth ${el.clientWidth} "${sample(el)}"`);
        }
      }
    }
  }

  // 4. charts: every chart-sized svg has size and marks
  for (const svg of Array.from(document.querySelectorAll("svg"))) {
    if (svg.closest("[data-render-integrity-ignore], .sb-errordisplay")) continue;
    const r = svg.getBoundingClientRect();
    const cs = getComputedStyle(svg);
    if (cs.display === "none" || cs.visibility === "hidden") continue;
    // A hidden ancestor (display:none at phone width) is a layout choice, not a broken chart.
    if (typeof svg.checkVisibility === "function" && !svg.checkVisibility()) continue;
    const attrW = Number(svg.getAttribute("width")) || 0;
    const attrH = Number(svg.getAttribute("height")) || 0;
    const chartSized = (r.width >= 48 && r.height >= 32) || (attrW >= 48 && attrH >= 32) || svg.closest(".minimal__chart__root, [data-chart], .chart, .kit-chart");
    if (!chartSized) continue;
    if (r.width === 0 || r.height === 0) {
      push("chart-svg-empty", describe(svg.parentElement ?? svg), `svg rendered ${Math.round(r.width)}x${Math.round(r.height)}`);
      continue;
    }
    const marks = svg.querySelectorAll("path, rect, circle, line, polyline, polygon, ellipse, use, image, text");
    if (marks.length === 0) push("chart-svg-empty", describe(svg.parentElement ?? svg), "svg has no path/rect/circle/line marks");
  }

  // 5-7. text content checks
  const root = document.querySelector(textRoot) ?? document.body;
  const text = (root.innerText ?? root.textContent ?? "");
  const bad = text.match(/\bNaN\b|\bundefined\b|\[object |\bInfinity\b|(?:^|\s)null(?=\s|$)/);
  if (bad) push("bad-text", textRoot, `rendered "${bad[0].trim()}" near: ${text.slice(Math.max(0, bad.index - 40), bad.index + 40).replace(/\s+/g, " ")}`);
  const f2 = text.match(/(?:^|[\s(/•·,-])F2(?=[\s)/•·,.-]|$)/);
  if (f2) push("f2-literal", textRoot, `rendered legacy "F2" near: ${text.slice(Math.max(0, f2.index - 40), f2.index + 40).replace(/\s+/g, " ")}`);
  let rawFloats = 0;
  // Chart text too: Apex prints its own default (`val + "%"`) when a formatter is bypassed.
  for (const cell of Array.from(root.querySelectorAll("td, th, .kit-kpi-value, .kpi-value, [data-kpi-value], .num, .val, .apexcharts-datalabel-value, .apexcharts-datalabel, .apexcharts-data-labels text, .apexcharts-xaxis-label, .apexcharts-yaxis-label"))) {
    if (rawFloats >= 8) break;
    const t = (cell.innerText ?? cell.textContent ?? "").trim();
    if (/(?:^|[^\d.])\d+\.\d{3,}(?![\d.])/.test(t) && !/[a-z]{3,}/i.test(t.replace(/[a-z]+\/[a-z]+/gi, ""))) {
      rawFloats += 1;
      push("raw-float", `${describe(cell)} "${t.slice(0, 24)}"`, `has > 2 decimals (use lib/format)`);
    }
  }

  // A gauge's centre must print what its aria-label says (68.7887…% once reached the screen).
  for (const gauge of Array.from(root.querySelectorAll(".kit-radial[aria-label]"))) {
    const centre = gauge.querySelector(".apexcharts-datalabel-value");
    const said = (centre?.textContent ?? "").trim();
    if (centre && said !== gauge.getAttribute("aria-label")) push("bad-text", describe(gauge), `gauge centre "${said.slice(0, 24)}" differs from its label "${gauge.getAttribute("aria-label")}"`);
  }

  // 7b. blank cards: a titled card with nothing under the title (owner blind spot).
  let blankCards = 0;
  for (const card of Array.from(root.querySelectorAll(".MuiCard-root, .card, section[aria-label]"))) {
    if (blankCards >= 6 || !visible(card)) continue;
    if (card.closest("[data-render-integrity-ignore], .sb-errordisplay")) continue;
    // A closed <details> card (source-entry "New load", audit "Advanced filters") hides its body by design.
    if (card.matches("details:not([open])") || (card.querySelector(":scope > details:not([open])") && card.children.length <= 2)) continue;
    const header = card.querySelector(".MuiCardHeader-root, .card-head, header, h1, h2, h3, h4, summary");
    const title = header ? (header.textContent ?? "").trim().replace(/\s+/g, " ") : (card.getAttribute("aria-label") ?? "");
    if (!title || (header && !visible(header))) continue;
    const headerBottom = header ? header.getBoundingClientRect().bottom : card.getBoundingClientRect().top;
    let hasContent = false;
    for (const el of Array.from(card.querySelectorAll("*"))) {
      if (header && (el === header || header.contains(el))) continue;
      if (!visible(el)) continue;
      // Controls and empty/chart containers count wherever they sit (a form card's inputs may share the header row).
      if (el.matches(".kit-empty, .kit-emptystate, [class*=\"empty\"], .MuiSkeleton-root, [data-skel-root], .skeleton, [data-empty], .cx-fig, .kit-chart, [data-chart], .minimal__chart__root, input, select, textarea, button, [role=\"switch\"], [role=\"tablist\"]")) { hasContent = true; break; }
      if (el.getBoundingClientRect().top < headerBottom - 1) continue;
      if (el.matches("tr, li, img, canvas, video")) { hasContent = true; break; }
      if (el.tagName.toLowerCase() === "svg" && el.querySelector("path, rect, circle, line, polyline, polygon")) { hasContent = true; break; }
      if (Array.from(el.childNodes).some((n) => n.nodeType === 3 && n.textContent.trim().length >= 1)) { hasContent = true; break; }
    }
    if (!hasContent) {
      blankCards += 1;
      push("blank-card", `${describe(card)} "${title.slice(0, 40)}"`, "card has a title but no visible content under it");
    }
  }

  // 8. fonts — a family must be LOADED when anything visible resolves to it (a story that never
  // uses the display face is not a font failure; a body that resolved to system-ui is).
  if (requireFonts && document.fonts && typeof document.fonts.check === "function") {
    const used = new Set();
    for (const el of [root, ...Array.from(root.querySelectorAll("h1,h2,h3,p,span,td,th,button,a,label,dd,dt,li,div")).slice(0, 400)]) {
      if (!visible(el)) continue;
      const first = (getComputedStyle(el).fontFamily.split(",")[0] ?? "").replace(/["']/g, "").trim();
      if (first) used.add(first);
    }
    for (const family of fonts) {
      if (!used.has(family)) continue;
      let ok = false;
      try {
        // Not fonts.check(): it is true when NO face matches, and false for an unmatched weight.
        ok = Array.from(document.fonts).some((f) => f.family.replace(/["']/g, "") === family && f.status === "loaded");
      } catch {
        ok = false;
      }
      if (!ok) push("font-not-loaded", family, "document.fonts has no loaded face for this family");
    }
  }
  // ---- sibling-overlap: in-flow children of a flex/grid/inline-flex row that paint over each other.
  {
    const isFlowChild = (el) => {
      const cs = getComputedStyle(el);
      return cs.position !== "absolute" && cs.position !== "fixed" && cs.position !== "sticky" && cs.display !== "contents";
    };
    for (const box of document.querySelectorAll(`${textRoot} *`)) {
      const cs = getComputedStyle(box);
      if (!/flex|grid/.test(cs.display)) continue;
      // Shapes inside an inline-flex <svg> icon overlap by design, and an MUI AvatarGroup stacks its
      // avatars with a negative margin on purpose; neither is a field drawn under its neighbour.
      if (box.namespaceURI === "http://www.w3.org/2000/svg" || box.matches(".MuiAvatarGroup-root")) continue;
      if (!visible(box)) continue;
      const kids = [...box.children].filter((k) => k.nodeType === 1 && visible(k) && isFlowChild(k) && !k.matches("svg, canvas, i, hr"));
      if (kids.length < 2) continue;
      const rects = kids.map((k) => ({ k, r: k.getBoundingClientRect() }));
      for (let a = 0; a < rects.length; a += 1) {
        for (let b = a + 1; b < rects.length; b += 1) {
          const A = rects[a].r, B = rects[b].r;
          const ox = Math.min(A.right, B.right) - Math.max(A.left, B.left);
          const oy = Math.min(A.bottom, B.bottom) - Math.max(A.top, B.top);
          if (ox > 4 && oy > 4) {
            push("sibling-overlap", describe(rects[a].k), `overlaps ${describe(rects[b].k)} by ${Math.round(ox)}x${Math.round(oy)}px "${sample(rects[a].k)}" / "${sample(rects[b].k)}"`);
          }
        }
      }
    }
  }
  return findings;
};

/**
 * Wait until every required family has at least one LOADED face (document.fonts.ready resolves
 * before a lazily-triggered swap font has even started downloading, so a font check right after
 * it races). Returns the families still missing after the timeout.
 */
export async function waitForFonts(page, families = ["Public Sans", "Barlow"], timeoutMs = 4000) {
  // State-based: kick the loads (check() alone does not start a swap font download; display
  // faces may only ship bold weights, so both a regular and a bold load), then await the
  // FontFace promises themselves — no polling, no timer.
  return page
    .evaluate(
      async ({ wanted, timeoutMs }) => {
        if (!document.fonts) return [];
        const loads = wanted.flatMap((family) => [document.fonts.load(`16px "${family}"`), document.fonts.load(`700 16px "${family}"`)]);
        // Upper bound only (the font loads are the awaited state); never a sleep.
        let giveUp;
        const budget = new Promise((resolve) => {
          giveUp = resolve;
        });
        setTimeout(giveUp, timeoutMs);
        await Promise.race([Promise.allSettled([...loads, document.fonts.ready]), budget]);
        const loaded = new Set(Array.from(document.fonts).filter((f) => f.status === "loaded").map((f) => f.family.replace(/["']/g, "")));
        return wanted.filter((family) => !loaded.has(family));
      },
      { wanted: families, timeoutMs },
    )
    .catch(() => families);
}

/**
 * Load-phase sampler for hard loads (route lane). Installed with addInitScript so it starts at
 * document start; samples on requestAnimationFrame at >= 100ms spacing for `windowMs`, recording
 * how much of <main> is skeleton vs content and whether the shell is present. Read back with
 * LOAD_PHASES_READ and judged by judgeLoadPhases().
 */
export const LOAD_PHASES_INSTALL = `(() => {
  // Frames persist in sessionStorage so the chain / -> /weighing/analytics -> ?wt_from... (server
  // 307s and client replaces) is ONE timeline; every document appends its own frames tagged with
  // location.href and an absolute timestamp. The lane clears the key before each route's goto.
  const KEY = "__GOATOS_LOAD_FRAMES__";
  const read = () => { try { return JSON.parse(sessionStorage.getItem(KEY) || "[]"); } catch { return []; } };
  const write = (frames) => { try { sessionStorage.setItem(KEY, JSON.stringify(frames)); } catch {} };
  const docStart = performance.timeOrigin + performance.now();
  const mine = [];
  let last = -1000;
  const isSkeleton = (el) => el.matches(".MuiSkeleton-root, [data-skel-root], .skeleton, [data-skeleton], [aria-busy='true']");
  const tick = () => {
    const now = performance.now();
    if (now - last >= 100) {
      last = now;
      const main = document.querySelector("main") || document.body;
      let skeleton = 0, content = 0;
      if (main) {
        for (const el of main.querySelectorAll("*")) {
          if (isSkeleton(el)) { skeleton += 1; continue; }
          if (el.closest(".MuiSkeleton-root, [data-skel-root], .skeleton, [data-skeleton]")) continue;
          for (const n of el.childNodes) { if (n.nodeType === 3 && n.textContent.trim().length > 0) { content += 1; break; } }
          if (el.tagName === "svg" && el.querySelector("path, rect, circle")) content += 1;
        }
      }
      const shell = Boolean(document.querySelector("nav, header, aside, .sidebar, .kit-sidebar, .topbar"));
      mine.push({ at: Math.round(performance.timeOrigin + now), docStart: Math.round(docStart), href: location.href, skeleton, content, shell });
      write(read().filter((f) => f.docStart !== Math.round(docStart)).concat(mine));
    }
    if (now < 3000) requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
})();`;
export const LOAD_PHASES_CLEAR = () => { try { sessionStorage.removeItem("__GOATOS_LOAD_FRAMES__"); } catch {} };
export const LOAD_PHASES_READ = () => { try { return JSON.parse(sessionStorage.getItem("__GOATOS_LOAD_FRAMES__") || "[]"); } catch { return []; } };

/** double-skeleton / blank-frame judgement over sampled load frames. */
export function judgeLoadPhases(input, { ignorePaths = [] } = {}) {
  const findings = [];
  if (!input || input.length < 3) return findings;
  // Normalise: frames from the cross-document sampler carry absolute `at`; test fixtures carry `t`.
  // Frames from the runner's warm-up document (`ignorePaths`, e.g. /robots.txt — which the app
  // renders as its own 404 page, skeleton and all) are not part of the route's load: keeping them
  // made every hard load read as "skeleton → content → skeleton".
  const pathOf = (href) => (href ? String(href).replace(/^https?:\/\/[^/]+/, "").replace(/[?#].*$/, "") : null);
  const frames = [...input]
    .filter((f) => !ignorePaths.includes(pathOf(f.href)))
    .map((f) => ({ ...f, t: f.t ?? f.at }))
    .sort((a, b) => a.t - b.t);
  if (frames.length < 3) return findings;
  const t0 = frames[0].t;
  const lastCommit = Math.max(...frames.map((f) => f.docStart ?? t0));
  const window = frames.filter((f) => f.t <= lastCommit + 3000);
  const kind = (f) => {
    const total = f.skeleton + f.content;
    if (total === 0) return "blank";
    return f.skeleton / total >= 0.4 ? "skeleton" : "content";
  };
  let phases = 0;
  let prev = null;
  let prevDoc = null;
  const blanks = [];
  const docs = new Set();
  for (const f of window) {
    const k = kind(f);
    // A new document (redirect / replace) remounts its own skeleton: that is a second flash the
    // person sees even when no content frame separates the two.
    const doc = f.docStart ?? 0;
    if (f.href) docs.add(f.href.replace(/^https?:\/\/[^/]+/, "").replace(/\?.*$/, ""));
    if (k === "skeleton" && (prev !== "skeleton" || doc !== prevDoc)) phases += 1;
    prevDoc = doc;
    if (k === "blank" && f.t - t0 > 150 && !f.shell) blanks.push(f.t - t0);
    prev = k;
  }
  const timeline = window.map((f) => `${f.t - t0}:${kind(f)[0]}`).join(" ");
  const chain = docs.size ? ` chain=${[...docs].join(" -> ")}` : "";
  if (phases > 1) findings.push({ check: "double-skeleton", target: "main", detail: `${phases} skeleton phases across the navigation chain${chain}: ${timeline}`.slice(0, 200) });
  if (blanks.length) findings.push({ check: "blank-frame", target: "document", detail: `blank frame(s) with no shell/skeleton/content at ${blanks.slice(0, 5).join(",")}ms${chain}` });
  return findings;
}

/** Attach console/page error collection to a Playwright page; returns a drain() function. */
export function collectConsoleErrors(page, { ignore = [] } = {}) {
  const errors = [];
  const noise = [
    /AbortError: The user aborted a request/,
    /net::ERR_(NETWORK_CHANGED|ABORTED|CONNECTION_RESET|NAME_NOT_RESOLVED|INTERNET_DISCONNECTED)/,
    /Failed to load resource: the server responded/,
    /Download the React DevTools/,
    /ResizeObserver loop/,
    ...ignore,
  ];
  const keep = (text) => !noise.some((pattern) => pattern.test(text));
  page.on("pageerror", (error) => {
    const text = `pageerror: ${String(error)}`;
    if (keep(text)) errors.push(text);
  });
  page.on("console", (message) => {
    if (message.type() !== "error") return;
    const text = `console.error: ${message.text()}`;
    if (keep(text)) errors.push(text);
  });
  return () => errors.splice(0, errors.length).map((detail) => ({ check: "console-error", target: "console", detail: detail.slice(0, 200) }));
}

/** Stable waiver key for a finding in a given capture context. */
export function integrityKey(context, finding) {
  return `${finding.check}|${context}|${finding.target}`;
}

/**
 * Perceptual fingerprint for a PNG (pngjs object): 16x16 luminance mean-hash (256 bits as hex)
 * plus a sha256 of the raw pixels. Committed to a manifest instead of the PNG itself, so the
 * baseline lives in git (small, diffable, per-capture) while the pixels stay on disk.
 */
export function fingerprintPng(png) {
  const { width, height, data } = png;
  const grid = 16;
  const lum = new Float64Array(grid * grid);
  const counts = new Uint32Array(grid * grid);
  for (let y = 0; y < height; y += 1) {
    const gy = Math.min(grid - 1, Math.floor((y * grid) / height));
    for (let x = 0; x < width; x += 1) {
      const gx = Math.min(grid - 1, Math.floor((x * grid) / width));
      const i = (y * width + x) * 4;
      const l = 0.299 * data[i] + 0.587 * data[i + 1] + 0.114 * data[i + 2];
      lum[gy * grid + gx] += l;
      counts[gy * grid + gx] += 1;
    }
  }
  let mean = 0;
  for (let k = 0; k < lum.length; k += 1) {
    lum[k] = counts[k] ? lum[k] / counts[k] : 0;
    mean += lum[k];
  }
  mean /= lum.length;
  let bits = "";
  for (let k = 0; k < lum.length; k += 1) bits += lum[k] > mean ? "1" : "0";
  const hash = BigInt(`0b${bits}`).toString(16).padStart(64, "0");
  const sha256 = createHash("sha256").update(data).digest("hex");
  return { width, height, hash, sha256 };
}

/** Fraction of differing bits between two 256-bit hex hashes (0 = identical, 1 = inverted). */
export function hashDistance(a, b) {
  const x = BigInt(`0x${a}`) ^ BigInt(`0x${b}`);
  let n = 0;
  for (let v = x; v > 0n; v >>= 1n) if (v & 1n) n += 1;
  return n / 256;
}
