#!/usr/bin/env node
// r2-drawer-audit.mjs — runtime guard for Ravi R2-4: every right drawer is the template temporary
// Drawer and nothing inside it is squeezed or clipped.
//
//   node scripts/r2-drawer-audit.mjs --base http://127.0.0.1:3499 [--only id,id] [--out dir]
//        [--viewports 1440,390] [--themes dark,light] [--template https://minimals.cc]
//
// For each drawer in DRAWERS (route + how to open it) at each viewport x theme it asserts:
//   drawer-missing      no visible right-anchored MUI Drawer paper after the open step
//   drawer-width        from 600px up the paper is a template width (320/360/420/480); below it is
//                       the full viewport width (template `{ xs: 1, sm: W }`)
//   drawer-backdrop     the modal has a visible backdrop (not MuiBackdrop-invisible, alpha > 0)
//   drawer-body-hscroll the drawer BODY scrolls sideways (wide content must scroll in its own
//                       Scrollbar, not drag the whole drawer)
//   drawer-overflow     an element pokes out of the paper, or overflows (scrollWidth > clientWidth)
//                       while clipping instead of scrolling, without its own scroll container
// and writes <out>/report.json plus one PNG per drawer/viewport/theme. With --template it also
// captures the template kanban details drawer at the same viewport/theme and writes
// <out>/side/<id>__<vp>__<theme>.png (ours | template).
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright";
import sharp from "sharp";

/** ours | template, top-aligned, on one canvas. */
export async function sideBySide(left, right, out) {
  const [a, b] = await Promise.all([sharp(left).metadata(), sharp(right).metadata()]);
  const gap = 16;
  await sharp({ create: { width: a.width + gap + b.width, height: Math.max(a.height, b.height), channels: 3, background: { r: 128, g: 128, b: 128 } } })
    .composite([{ input: left, left: 0, top: 0 }, { input: right, left: a.width + gap, top: 0 }])
    .png()
    .toFile(out);
}

export const TEMPLATE_DRAWER_WIDTHS = [320, 360, 420, 480];
const THEME_STORAGE_KEY = "mesha.shell.theme";

// How to reach each right drawer. `open` is either absent (the URL opens it), a CSS selector, or
// { role, name } for getByRole. `wait` is an optional selector that must exist before opening.
export const DRAWERS = [
  { id: "sales-config-tag-sale", path: "/sales/config", open: { css: "a[href*='tag_sale=']" } },
  { id: "sales-config-record-sale", path: "/sales/config?deal_id=new" },
  { id: "sales-sold-deal", path: "/sales/sold", open: { css: "a[href*='deal_id=']:not([href*='deal_id=new'])" } },
  { id: "sales-loads-load-cost", path: "/sales/loads", open: { css: "a[href*='cost_load=']" } },
  { id: "sales-loads-new-load", path: "/sales/loads", open: { role: "button", name: /new load/i } },
  { id: "vendors-vendor", path: "/procurement/vendors", open: { css: "a[href*='vendor=']" } },
  { id: "feed-purchase", path: "/procurement/feed-purchases", open: { css: "a[href*='purchase_id=']" } },
  { id: "source-entry-load", path: "/procurement/source-entry", open: { css: "a[href*='source_load=']" } },
  { id: "approvals-row", path: "/approvals", open: { css: "a[href*='ap_row=']" } },
  { id: "action-center-row", path: "/action-center", open: { css: "a[href*='ac_row=']" } },
  { id: "control-tower-alert", path: "/?lens=control-tower", open: { css: "a[href*='ct_alert=']" } },
  { id: "protocol-adherence-row", path: "/protocol-adherence", open: { css: "a[href*='adh_row=']" } },
  { id: "audit-row", path: "/operations/audit", open: { css: "a[href*='audit_id=']" } },
  { id: "dlq-row", path: "/operations/dlq", open: { css: "a[href*='dlq_id=']" } },
  { id: "herd-passport", path: "/counts/herd", open: { css: "a[href*='goat_passport=']" } },
  { id: "herd-filters", path: "/counts/herd", open: { role: "button", name: /^filters/i } },
  { id: "herd-signals-tag", path: "/herd-signals", open: { css: "a[href*='hs_tag=']" } },
  { id: "vaccination-record", path: "/vaccination", open: { css: "a[href*='vacc_record=']" } },
  { id: "vaccination-cohort", path: "/vaccination", open: { css: "a[href*='cohort_record=']" } },
  { id: "vaccination-warmup", path: "/vaccination", open: { css: "a[href*='warmup_load=']" } },
  { id: "vaccination-shed-event", path: "/vaccination", open: { css: "a[href*='shed_event=']" } },
  { id: "vaccination-schedule-move", path: "/vaccination", open: { css: "a[href*='schedule_move=']" } },
  { id: "live-tracker-passport", path: "/vaccination/live-tracker", open: { css: "a[href*='goat_passport=']" } },
  { id: "calendar-event", path: "/calendar", open: { css: "a[href*='event=']" } },
  { id: "tasks-detail", path: "/tasks", open: { css: "a[href*='task=']" } },
  { id: "people-add", path: "/people?person=new" },
  { id: "people-filters", path: "/people", open: { role: "button", name: /^filters/i } },
  { id: "weights-export", path: "/weighing/weights?wt_export=1" },
  { id: "weights-assumptions", path: "/weighing/weights?wt_assumptions=1" },
  { id: "weights-band-exits", path: "/weighing/analytics", open: { css: "a[href*='fb_exit=']" } },
  { id: "feed-completion-row", path: "/feed/analytics", open: { css: "a[href*='fdc_row=']" } },
  { id: "verify-analytics", path: "/verify", open: { css: "a[href*='vi_analytics=']" } },
  { id: "verify-video-log", path: "/verify", open: { css: "a[href*='vi_video_log=']" } },
  { id: "verify-randomization", path: "/verify", open: { css: "a[href*='vi_randomization=']" } },
  { id: "notifications", path: "/action-center", open: { css: "button[aria-label*='otification' i]" } },
  { id: "configuration-row", path: "/configuration/items", open: { css: "a[href*='edit=']" } },
  { id: "routines-row", path: "/routines", open: { css: "a[href*='edit=']" } },
  { id: "alerts-configure", path: "/alerts?configure=1" },
];

const VIEWPORTS = { 1440: { width: 1440, height: 900 }, 390: { width: 390, height: 844, isMobile: true, hasTouch: true } };

function parseArgs(argv) {
  const out = { base: process.env.GOATOS_ADMIN_WEB_BASE_URL || "http://127.0.0.1:3499", only: [], out: "", viewports: ["1440", "390"], themes: ["dark", "light"], template: "" };
  for (let i = 0; i < argv.length; i += 1) {
    const a = argv[i];
    const next = () => argv[++i];
    if (a === "--base") out.base = next();
    else if (a === "--only") out.only = next().split(",").filter(Boolean);
    else if (a === "--out") out.out = next();
    else if (a === "--viewports") out.viewports = next().split(",");
    else if (a === "--themes") out.themes = next().split(",");
    else if (a === "--template") out.template = next();
  }
  out.out ||= join(process.cwd(), ".codex-goatos-render", "r2-drawer-audit", new Date().toISOString().replace(/[:.]/g, "-"));
  return out;
}

/** In-page probe: returns findings for the open right drawer. Exported for reuse by other audits. */
export async function probeOpenDrawer(page, viewportWidth) {
  return page.evaluate(({ widths, vw }) => {
    const findings = [];
    // Right drawer paper: an MUI Drawer paper flush with the viewport's right edge (MUI v7 no longer
    // guarantees the paperAnchorRight class).
    const papers = [...document.querySelectorAll(".MuiDrawer-paper")].filter((p) => {
      const r = p.getBoundingClientRect();
      return r.width > 0 && Math.abs(r.right - document.documentElement.clientWidth) <= 2 && r.left > -1;
    });
    const paper = papers.at(-1);
    if (!paper) return { findings: [{ rule: "drawer-missing", detail: "no visible right-anchored MUI Drawer paper" }] };
    const rect = paper.getBoundingClientRect();
    const width = Math.round(rect.width);
    if (vw >= 600) {
      if (!widths.includes(width)) findings.push({ rule: "drawer-width", detail: `paper ${width}px, template widths ${widths.join("/")}` });
    } else if (Math.abs(width - vw) > 1) findings.push({ rule: "drawer-width", detail: `paper ${width}px on a ${vw}px phone (template: full width)` });
    const modal = paper.closest(".MuiModal-root, .MuiDrawer-root");
    const backdrop = modal?.querySelector(":scope > .MuiBackdrop-root");
    if (!backdrop) findings.push({ rule: "drawer-backdrop", detail: "no MuiBackdrop in the drawer modal" });
    else {
      const cs = getComputedStyle(backdrop);
      const alpha = (() => {
        const m = /rgba?\(([^)]+)\)/.exec(cs.backgroundColor);
        if (!m) return 0;
        const parts = m[1].split(/[ ,/]+/).filter(Boolean);
        return parts.length >= 4 ? Number(parts[3]) : 1;
      })();
      if (backdrop.classList.contains("MuiBackdrop-invisible") || alpha === 0 || Number(cs.opacity) === 0) findings.push({ rule: "drawer-backdrop", detail: `backdrop invisible (bg ${cs.backgroundColor}, opacity ${cs.opacity})` });
    }
    const describe = (el) => {
      const cls = typeof el.className === "string" ? el.className.split(/\s+/).filter((c) => c && !/^css-/.test(c)).slice(0, 3).join(".") : "";
      const txt = (el.textContent || "").trim().replace(/\s+/g, " ").slice(0, 40);
      return `${el.tagName.toLowerCase()}${cls ? "." + cls : ""}${txt ? ` "${txt}"` : ""}`;
    };
    const scrollsX = (el) => {
      const ox = getComputedStyle(el).overflowX;
      return ox === "auto" || ox === "scroll";
    };
    // The drawer body: the first SimpleBar content wrapper (template Scrollbar) in the paper.
    const body = paper.querySelector(".simplebar-content-wrapper");
    if (body && body.scrollWidth > body.clientWidth + 1) {
      // Allowed only when the overflow comes from a nested own scroll container (then the body itself would not overflow).
      findings.push({ rule: "drawer-body-hscroll", detail: `drawer body scrolls sideways (${body.scrollWidth} > ${body.clientWidth})` });
    }
    const seen = new Set();
    for (const el of paper.querySelectorAll("*")) {
      if (el.closest("[aria-hidden='true']") && el.closest("[aria-hidden='true']") !== paper) continue;
      const cls = typeof el.className === "string" ? el.className : "";
      if (/simplebar-/.test(cls) || el.tagName === "svg" || el.closest("svg")) continue;
      const cs = getComputedStyle(el);
      if (cs.display === "none" || cs.visibility === "hidden" || cs.position === "fixed") continue;
      // Inside its own horizontal scroll container (not the drawer body): contained, fine.
      let contained = false;
      for (let a = el.parentElement; a && a !== paper; a = a.parentElement) {
        if (a === body) break;
        const acls = typeof a.className === "string" ? a.className : "";
        if (scrollsX(a) || /simplebar-content-wrapper/.test(acls)) {
          contained = true;
          break;
        }
      }
      if (contained) continue;
      const r = el.getBoundingClientRect();
      if (r.width === 0 || r.height === 0) continue;
      if (r.right > rect.right + 1 || r.left < rect.left - 1) {
        const key = describe(el);
        if (!seen.has(key)) findings.push({ rule: "drawer-overflow", detail: `${key} pokes out of the drawer (${Math.round(r.left)}..${Math.round(r.right)} vs ${Math.round(rect.left)}..${Math.round(rect.right)})` });
        seen.add(key);
        continue;
      }
      const clipsX = cs.overflowX === "hidden" || cs.overflowX === "clip";
      if (clipsX && el.scrollWidth > el.clientWidth + 1 && cs.textOverflow !== "ellipsis" && !scrollsX(el)) {
        const key = describe(el);
        if (!seen.has(key)) findings.push({ rule: "drawer-overflow", detail: `${key} clips its content (${el.scrollWidth} > ${el.clientWidth}) without its own scroll container` });
        seen.add(key);
      }
    }
    return { width, findings };
  }, { widths: [320, 360, 420, 480], vw: viewportWidth });
}

async function openDrawer(page, spec) {
  if (!spec.open) return true;
  const { css, role, name } = spec.open;
  // Server pages stream; wait for the first VISIBLE opener (tables render phone/laptop twins).
  const target = css ? page.locator(`${css} >> visible=true`).first() : page.getByRole(role, { name }).first();
  await target.waitFor({ state: "visible", timeout: 15_000 }).catch(() => {});
  if (!(await target.count())) return false;
  await target.scrollIntoViewIfNeeded().catch(() => {});
  await target.click({ timeout: 5_000 });
  return true;
}

async function captureTemplate(browser, templateBase, vp, theme, file) {
  const context = await browser.newContext({ viewport: VIEWPORTS[vp], isMobile: VIEWPORTS[vp].isMobile, hasTouch: VIEWPORTS[vp].hasTouch });
  await context.addInitScript(([mode]) => {
    try {
      window.localStorage.setItem("theme-mode", mode);
    } catch {
      /* ignore */
    }
  }, [theme]);
  const page = await context.newPage();
  try {
    await page.goto(`${templateBase}/dashboard/kanban`, { waitUntil: "networkidle", timeout: 45_000 });
    // The hosted demo (minimals.cc) signs in with its published demo account first.
    const signIn = page.getByRole("button", { name: /^sign in$/i });
    if (await signIn.count()) {
      await signIn.click();
      await page.waitForURL(/dashboard/, { timeout: 30_000 }).catch(() => {});
      await page.goto(`${templateBase}/dashboard/kanban`, { waitUntil: "networkidle", timeout: 45_000 });
    }
    await page.locator("[data-rfd-draggable-id], .kanban-task, [class*='task'] ").first().click({ timeout: 10_000 }).catch(() => {});
    await page.getByText(/./).first().waitFor({ timeout: 2_000 }).catch(() => {});
    await page.waitForTimeout(800);
    await page.screenshot({ path: file });
    return true;
  } catch {
    return false;
  } finally {
    await context.close();
  }
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  mkdirSync(join(args.out, "side"), { recursive: true });
  const drawers = DRAWERS.filter((d) => !args.only.length || args.only.includes(d.id));
  const browser = await chromium.launch();
  const results = [];
  try {
    for (const vp of args.viewports) {
      for (const theme of args.themes) {
        const context = await browser.newContext({ viewport: VIEWPORTS[vp], isMobile: VIEWPORTS[vp].isMobile, hasTouch: VIEWPORTS[vp].hasTouch });
        await context.addInitScript(([key, value]) => {
          try {
            window.localStorage.setItem(key, value);
          } catch {
            /* ignore */
          }
        }, [THEME_STORAGE_KEY, theme]);
        let templateShot = "";
        if (args.template) {
          templateShot = join(args.out, `template-kanban-details__${vp}__${theme}.png`);
          if (!(await captureTemplate(browser, args.template, vp, theme, templateShot))) templateShot = "";
        }
        for (const spec of drawers) {
          const page = await context.newPage();
          const entry = { id: spec.id, path: spec.path, viewport: vp, theme, findings: [] };
          try {
            await page.goto(`${args.base}${spec.path}`, { waitUntil: "domcontentloaded", timeout: 60_000 });
            await page.waitForLoadState("networkidle", { timeout: 10_000 }).catch(() => {});
            const opened = await openDrawer(page, spec);
            if (!opened) {
              entry.skipped = "no opener on the page (no data?)";
            } else {
              await page.waitForSelector(".MuiDrawer-paper", { timeout: 8_000 }).catch(() => {});
              await page.waitForTimeout(700);
              const probe = await probeOpenDrawer(page, VIEWPORTS[vp].width);
              entry.width = probe.width;
              entry.findings = probe.findings;
              const shot = join(args.out, `${spec.id}__${vp}__${theme}.png`);
              await page.screenshot({ path: shot });
              entry.screenshot = shot;
              if (templateShot) {
                const side = join(args.out, "side", `${spec.id}__${vp}__${theme}.png`);
                await sideBySide(shot, templateShot, side);
                entry.side = side;
              }
            }
          } catch (error) {
            entry.error = String(error?.message ?? error).slice(0, 300);
          } finally {
            await page.close();
          }
          results.push(entry);
          const status = entry.skipped ? `SKIP (${entry.skipped})` : entry.error ? `ERROR ${entry.error}` : entry.findings.length ? `FAIL ${entry.findings.map((f) => `${f.rule}: ${f.detail}`).join(" | ")}` : `OK width=${entry.width}`;
          console.log(`${spec.id} ${vp} ${theme}: ${status}`);
        }
        await context.close();
      }
    }
  } finally {
    await browser.close();
  }
  const failures = results.filter((r) => r.findings.length || r.error);
  writeFileSync(join(args.out, "report.json"), JSON.stringify({ base: args.base, generatedAt: new Date().toISOString(), results }, null, 2));
  console.log(`r2_drawer_audit=${failures.length ? "FAIL" : "OK"} checked=${results.filter((r) => !r.skipped).length} skipped=${results.filter((r) => r.skipped).length} failures=${failures.length} out=${args.out}`);
  process.exit(failures.length ? 1 : 0);
}

if (import.meta.url === `file://${process.argv[1]}`) await main();
