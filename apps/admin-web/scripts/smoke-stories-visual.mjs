#!/usr/bin/env node
// smoke-stories-visual.mjs — COMPONENT-level visual regression lane for admin-web.
//
// Route-level coverage lives in scripts/smoke-visual-live.mjs (needs a live app +
// API). This lane needs neither: it builds the Storybook static bundle, serves it
// from a local http server, and drives Playwright over EVERY story at BOTH the
// desktop (1440x900) and mobile (390x844) viewports in BOTH themes, then diffs
// each capture against a committed baseline with pixelmatch.
//
// It deliberately mirrors smoke-visual-live.mjs's baseline contract so the two
// lanes behave identically for a developer:
//   --baseline-dir <dir>      compare against (or write) baselines in <dir>
//   --update-baseline         rewrite baselines instead of comparing (intentional)
//   --require-baseline        a missing baseline is a FAILURE, not a skip
//   --max-diff-ratio <0..1>   per-capture tolerance (default 0.01)
// plus lane-specific:
//   --only <substr>           restrict to story ids containing <substr>
//   --viewports desktop,mobile / --themes dark,light
//   --no-build                reuse an existing storybook-static
//   --port <n>                static server port (default 6017)
//   --storybook-url <url>     drive an already-running Storybook (e.g. http://127.0.0.1:6007)
//                             instead of building; the story list comes from <url>/index.json
//   --no-integrity            skip the render-integrity probe (pixel diff only)
//   --no-frames               skip interaction frames (parameters.motionFrames stories)
//
// Baselines (Paparazzi-style, see scripts/lib/visual-baseline.mjs): the COMMITTED manifest
// apps/admin-web/visual-baselines/stories/manifest.json fingerprints every capture; PNGs for
// pixel diffs live in --baseline-dir (default .codex-goatos-render/admin-web-story-baselines,
// gitignored). Render-integrity findings (overflow, clipped text, empty chart svg, NaN/F2 text,
// raw floats, fonts, console errors) fail the story unless keyed in visual-baselines/stories/
// waivers.json. `--update-baseline` rewrites manifest + PNGs + waivers — deliberately, never to
// land a change.
//
// Interaction (play) functions are part of the lane: every story is driven to its
// terminal render phase and a story whose play function threw FAILS the run, so
// the same command covers both "does it look right" and "does it still work".
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { createReadStream } from "node:fs";
import { createServer } from "node:http";
import { dirname, extname, isAbsolute, join, normalize, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import { chromium } from "@playwright/test";
import { RENDER_INTEGRITY_PROBE, waitForFonts } from "./lib/render-integrity.mjs";
import { VisualBaseline } from "./lib/visual-baseline.mjs";

const appDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = resolve(appDir, "../..");
const args = parseArgs(process.argv.slice(2));

const baselineDir = normalizeRepoPath(args.baselineDir ?? process.env.GOATOS_STORY_VISUAL_BASELINE_DIR ?? ".codex-goatos-render/admin-web-story-baselines");
const manifestDir = join(appDir, "visual-baselines", "stories");
const updateBaseline = args.updateBaseline || process.env.GOATOS_VISUAL_UPDATE_BASELINE === "1";
const requireBaseline = args.requireBaseline || process.env.GOATOS_VISUAL_REQUIRE_BASELINE === "1";
const maxDiffRatio = args.maxDiffRatio ?? Number(process.env.GOATOS_VISUAL_MAX_DIFF_RATIO ?? "0.01");
const port = args.port ?? Number(process.env.GOATOS_STORY_VISUAL_PORT ?? "6017");
const staticDir = join(appDir, "storybook-static");
// Loaded by .storybook/preview-head.html and wired to --font-sans/--font-display in preview.css.
const STORY_FONTS = ["Public Sans", "Barlow"];

// Kept in step with VIEWPORTS in .storybook/preview.tsx. The two that gate CI are
// `desktop` and `mobile`: the mobile lane is not optional, it is the half that the
// route sweep's 390px lane mirrors.
const ALL_VIEWPORTS = {
  desktop: { width: 1440, height: 900 },
  laptop: { width: 1280, height: 800 },
  tablet: { width: 768, height: 1024 },
  mobile: { width: 390, height: 844 },
};
const viewports = (args.viewports ?? ["desktop", "mobile"]).map((name) => {
  const size = ALL_VIEWPORTS[name];
  if (!size) throw new Error(`Unknown viewport "${name}". Known: ${Object.keys(ALL_VIEWPORTS).join(", ")}`);
  return { name, ...size };
});
const themes = args.themes ?? ["dark", "light"];
for (const theme of themes) {
  if (theme !== "dark" && theme !== "light") throw new Error(`Unknown theme "${theme}" (dark|light)`);
}

const outDir = join(repoRoot, ".codex-goatos-render", "admin-web-story-screenshots", new Date().toISOString().replaceAll(/[:.]/g, "-"));
const diffDir = join(outDir, "diffs");
mkdirSync(outDir, { recursive: true });

const baseline = new VisualBaseline({
  lane: "admin-web-stories-visual",
  manifestDir,
  pngDir: baselineDir,
  diffDir,
  updateBaseline,
  requireBaseline,
  waive: args.waive ? { reason: args.waive } : null,
  maxDiffRatio,
  relativeToRepo,
});
const failures = [];
const integrityFindings = [];
const storyResults = [];
const realtimeFrames = [];

// A GOATOS_STORYBOOK_URL that does not answer index.json (stale gate server, wrong port) is not an
// error to throw on: fall back to building + serving storybook-static ourselves, as CI expects.
if (args.storybookUrl) {
  const probe = await fetch(`${args.storybookUrl.replace(/\/$/, "")}/index.json`).then((r) => r.ok).catch(() => false);
  if (!probe) {
    console.log(`-- ${args.storybookUrl}/index.json is not answering 200; building storybook-static and serving it locally instead`);
    args.storybookUrl = null;
    args.build = true;
  }
}
if (args.storybookUrl) {
  console.log(`-- driving live Storybook at ${args.storybookUrl} (no build)`);
} else if (args.build) {
  console.log("-- building storybook-static (npm run build-storybook)");
  const built = spawnSync("npm", ["run", "build-storybook"], { cwd: appDir, stdio: "inherit", env: process.env });
  if (built.status !== 0) {
    console.error("build-storybook failed");
    process.exit(1);
  }
} else if (!existsSync(join(staticDir, "index.json"))) {
  throw new Error(`--no-build was given but ${relativeToRepo(join(staticDir, "index.json"))} does not exist. Run without --no-build.`);
}

const index = args.storybookUrl
  ? await fetch(`${args.storybookUrl.replace(/\/$/, "")}/index.json`).then((response) => {
      if (!response.ok) throw new Error(`${args.storybookUrl}/index.json -> HTTP ${response.status}`);
      return response.json();
    })
  : JSON.parse(readFileSync(join(staticDir, "index.json"), "utf8"));
const allStoryCount = Object.values(index.entries).filter((entry) => entry.type === "story").length;
const stories = Object.values(index.entries)
  .filter((entry) => entry.type === "story")
  .filter((entry) => !args.only || entry.id.includes(args.only) || entry.title.toLowerCase().includes(args.only.toLowerCase()))
  .sort((a, b) => a.id.localeCompare(b.id));

if (stories.length === 0) throw new Error(`No stories matched${args.only ? ` --only ${args.only}` : ""}.`);

const server = args.storybookUrl ? null : await startStaticServer(staticDir, port);
const baseUrl = args.storybookUrl ? args.storybookUrl.replace(/\/$/, "") : `http://127.0.0.1:${port}`;
let browser = await chromium.launch();
let captured = 0;
let transportNoise = 0;

try {
  for (const viewport of viewports) {
    for (const theme of themes) {
      // A long sweep occasionally loses the browser process (memory pressure on a
      // laptop that is also running the dev server and other agents). Relaunch
      // rather than crashing the whole lane: a gate that dies at story 900 of 952
      // teaches people to skip it.
      if (!browser.isConnected()) browser = await chromium.launch();
      const contextOptions = {
        viewport: { width: viewport.width, height: viewport.height },
        deviceScaleFactor: 1,
        colorScheme: theme,
        // NOTE: deliberately NOT reducedMotion:"reduce" — the kit's Sheet/Dialog/
        // tooltip surfaces gate their enter state on a motion transition, and
        // forcing reduce left them mounted-but-invisible so their play functions
        // failed. Animations are frozen with CSS after the render settles instead.
        //
        // Frozen locale/timezone: a story that formats a date must not diff because
        // the machine running the lane sits in another zone.
        locale: "en-SG",
        timezoneId: "Asia/Singapore",
      };
      // Storybook reports a thrown play function on its own channel, not as a page
      // error: without this subscription a broken interaction test would be
      // screenshotted mid-failure and pass the lane.
      const installStoryErrorProbe = (target) =>
        target.addInitScript(() => {
          window.__GOATOS_STORY_ERRORS__ = [];
          const timer = setInterval(() => {
            const preview = window.__STORYBOOK_PREVIEW__;
            if (!preview?.channel) return;
            clearInterval(timer);
            for (const event of ["playFunctionThrewException", "storyThrewException", "storyErrored", "storyMissing", "unhandledErrors"]) {
              preview.channel.on(event, (payload) => {
                const detail = payload?.message ?? payload?.title ?? (Array.isArray(payload) ? payload.map(String).join("; ") : String(payload ?? ""));
                window.__GOATOS_STORY_ERRORS__.push(`${event}: ${String(detail).slice(0, 300)}`);
              });
            }
          }, 10);
        });
      let context = await browser.newContext(contextOptions);
      await installStoryErrorProbe(context);
      let page = await context.newPage();
      const pageErrors = [];
      // Transport noise is not a component defect. The preview pulls the Public Sans
      // webfont from Google Fonts, and a navigation that supersedes an in-flight
      // request (or a laptop switching networks) surfaces as AbortError /
      // net::ERR_NETWORK_CHANGED. Failing the lane on those makes the gate lie about
      // which stories are broken, so they are logged and skipped, not failed.
      const isTransportNoise = (text) =>
        /AbortError: The user aborted a request|net::ERR_(NETWORK_CHANGED|ABORTED|CONNECTION_RESET|NAME_NOT_RESOLVED|INTERNET_DISCONNECTED)|Failed to load resource: the server responded/.test(text);
      const pushError = (text) => {
        if (isTransportNoise(text)) {
          transportNoise += 1;
          return;
        }
        pageErrors.push(text);
      };
      page.on("pageerror", (error) => pushError(String(error)));
      page.on("console", (message) => {
        if (message.type() === "error") pushError(`console.error: ${message.text()}`);
      });

      const storyQueue = [...stories];
      const retried = new Set();
      while (storyQueue.length > 0) {
        const story = storyQueue.shift();
        const name = `${story.id}__${viewport.name}__${theme}.png`;
        pageErrors.length = 0;
        if (process.env.GOATOS_STORY_VISUAL_VERBOSE === "1") console.log(`   ${name}`);
        try {
          await page.goto(
            `${baseUrl}/iframe.html?viewMode=story&id=${encodeURIComponent(story.id)}&globals=theme:${theme}`,
            { waitUntil: "domcontentloaded", timeout: 60_000 },
          );
          let phase;
          try {
            phase = await waitForStoryPhase(page, story.id);
          } catch {
            // A live Storybook (--storybook-url) reloads the iframe on HMR when another editor
            // saves a file; one navigation retry keeps that from reading as a broken story.
            // One reload before calling it a failure: a story that never reaches a
            // terminal phase is usually a superseded font/asset request, and a flaky
            // gate is a gate people learn to ignore.
            await page.reload({ waitUntil: "domcontentloaded", timeout: 60_000 });
            phase = await waitForStoryPhase(page, story.id);
          }
          if (phase === "errored" || phase === "aborted") {
            throw new Error(`story render phase "${phase}" (play function or render threw)`);
          }
          // Storybook can report a terminal phase while the canvas still shows its "preparing
          // story" loader (an HMR re-render landing at that instant). Wait for the loader to
          // clear and the root to hold content; otherwise the capture is a spinner on a blank
          // page and diffs at ~100%.
          const shown = await page
            .waitForFunction(() => {
              const body = document.body;
              const preparing = body.classList.contains("sb-show-preparing-story") || body.classList.contains("sb-show-preparing-docs");
              const loader = document.querySelector(".sb-loader");
              const loaderVisible = loader ? getComputedStyle(loader).display !== "none" && loader.getClientRects().length > 0 : false;
              const root = document.querySelector("#storybook-root");
              return body.classList.contains("sb-show-main") && !preparing && !loaderVisible && Boolean(root && root.childElementCount > 0);
            }, { timeout: 20_000 })
            .then(() => true)
            .catch(() => false);
          if (!shown) throw new Error("story never left the preparing/loader state (sb-show-main + #storybook-root content)");
          const rendered = await page.evaluate(() => {
            // iframe.html always CONTAINS the error template; it is only shown when
            // the body carries sb-show-errordisplay. Reading the node unconditionally
            // made every story look broken.
            const shown = document.body.classList.contains("sb-show-errordisplay");
            const error = shown ? document.querySelector("#error-message, .sb-errordisplay") : null;
            return {
              error: error ? error.textContent?.slice(0, 400) ?? "unknown" : null,
              storyErrors: window.__GOATOS_STORY_ERRORS__ ?? [],
            };
          });
          if (rendered.error) throw new Error(`storybook error display: ${rendered.error}`);
          if (rendered.storyErrors.length > 0) throw new Error(`interaction/render failure: ${rendered.storyErrors.join(" | ")}`);
          // Settle: kill transitions/animations and give rAF-driven widgets
          // (CountUp, chart mount tweens, PageEnter) one frame budget to land.
          await page.addStyleTag({
            content: `*,*::before,*::after{animation-duration:0s!important;animation-delay:0s!important;transition-duration:0s!important;transition-delay:0s!important;caret-color:transparent!important}`,
          });
          await waitForFonts(page, STORY_FONTS);
          // The template Chart loads react-apexcharts lazily and shows its loading skeleton until
          // then: a capture taken first recorded a grey box as the chart's baseline. Wait until
          // every chart root has drawn its Apex canvas (an empty-state chart has no chart root).
          await page
            .waitForFunction(
              () =>
                Array.from(document.querySelectorAll("#storybook-root .minimal__chart__root")).every(
                  (el) => el.querySelector(".apexcharts-canvas") && !el.querySelector(".minimal__chart__loading"),
                ),
              { timeout: 10_000 },
            )
            .catch(() => {});
          await waitForSettledDom(page, "#storybook-root");
          const screenshotPath = join(outDir, name);
          await page.screenshot({ path: screenshotPath, fullPage: true, animations: "disabled" });
          captured += 1;
          // Render integrity: the picture must not be a picture of a broken component.
          if (args.integrity) {
            const probed = await page.evaluate(RENDER_INTEGRITY_PROBE, {
              viewportWidth: viewport.width,
              textRoot: "#storybook-root",
              fonts: STORY_FONTS,
            });
            const found = [...probed, ...pageErrors.map((detail) => ({ check: "console-error", target: "console", detail: detail.slice(0, 200) }))];
            const live = baseline.unwaived(`${story.id}|${viewport.name}|${theme}`, found);
            for (const finding of live) integrityFindings.push({ capture: name, ...finding });
            if (live.length > 0 && !args.waive) {
              throw new Error(`render integrity: ${live.map((f) => `${f.check} ${f.target} (${f.detail})`).slice(0, 4).join(" | ")}`);
            }
          } else if (pageErrors.length > 0) throw new Error(`page errors: ${pageErrors.slice(0, 3).join(" | ")}`);
          baseline.compare(name, screenshotPath);
          storyResults.push({ id: story.id, viewport: viewport.name, theme, status: "ok" });

          // Interaction frames (parameters.motionFrames): re-open the story with motion LIVE,
          // pause the page clock, fire the trigger, then advance virtual time to each frame and
          // capture. Deterministic: rAF/timers/CSS run on the virtual clock, so a 60ms frame is
          // the same 60ms frame on every machine. Each frame diffs against its own baseline.
          const motion = await readMotionFrames(page, story.id);
          if (motion && args.frames) {
            // A dedicated page: virtual time stays paused on it, so it is closed afterwards and
            // the main page keeps a real clock for the next story.
            const storyUrl = `${baseUrl}/iframe.html?viewMode=story&id=${encodeURIComponent(story.id)}&globals=theme:${theme}`;
            const pathFor = (at) => join(outDir, `${story.id}__${viewport.name}__${theme}__f${at}.png`);
            const openFramePage = async () => {
              const framePage = await context.newPage();
              await framePage.goto(storyUrl, { waitUntil: "domcontentloaded", timeout: 60_000 });
              await waitForStoryPhase(framePage, story.id);
              await waitForFonts(framePage, STORY_FONTS);
              await waitForSettledDom(framePage, "#storybook-root");
              return framePage;
            };
            let frames;
            let framePage = await openFramePage();
            try {
              frames = await captureMotionFrames(framePage, motion, pathFor);
            } catch (error) {
              // Virtual time stalled (renderer never reports the budget spent — happens under
              // load, and with overlay stories more than others). Re-open the story and capture
              // the frames on the real clock instead: approximate (±16ms) but never a hang, and
              // the summary lists which captures were taken this way.
              if (!/motion frame budget|timed out/.test(String(error))) throw error;
              await framePage.close().catch(() => {});
              framePage = await openFramePage();
              frames = await captureMotionFramesRealtime(framePage, motion, pathFor);
              realtimeFrames.push(`${story.id}__${viewport.name}__${theme}`);
            } finally {
              await framePage.close().catch(() => {});
            }
            captured += frames.length;
            const frameFailures = [];
            for (const frame of frames) {
              try {
                baseline.compare(frame.name, frame.path);
              } catch (error) {
                frameFailures.push(`f${frame.at}: ${error instanceof Error ? error.message : String(error)}`);
              }
            }
            if (frameFailures.length) throw new Error(`motion frames: ${frameFailures.join(" | ")}`);
          }
        } catch (error) {
          const message = error instanceof Error ? error.message : String(error);
          if (/interrupted by another navigation|Execution context was destroyed|frame was detached/.test(message) && !retried.has(name)) {
            retried.add(name);
            storyQueue.unshift(story); // HMR reload from a concurrent editor: try this story once more
            continue;
          }
          failures.push(`${name}: ${message}`);
          storyResults.push({ id: story.id, viewport: viewport.name, theme, status: "fail", error: message.split("\n")[0].slice(0, 200) });
          if (/Target page, context or browser has been closed|browser has been closed|Target crashed|Page crashed/.test(message)) {
            // The browser died under us. Rebuild the lane in place so the remaining
            // stories still get captured instead of cascading into hundreds of
            // identical "browser closed" failures.
            if (!browser.isConnected()) browser = await chromium.launch();
            try { await context.close(); } catch {}
            context = await browser.newContext(contextOptions);
            await installStoryErrorProbe(context);
            page = await context.newPage();
            page.on("pageerror", (error) => pushError(String(error)));
            page.on("console", (consoleMessage) => {
              if (consoleMessage.type() === "error") pushError(`console.error: ${consoleMessage.text()}`);
            });
          }
        }
      }
      await context.close();
    }
  }
} finally {
  await browser.close();
  if (server) await new Promise((done) => server.close(done));
}

baseline.writeUpdated({
  waiverNote: "Accepted render-integrity debt for Storybook stories (key = check|storyId|viewport|theme|target). Every entry is a KNOWN broken render; shrink it, never grow it to land a change.",
});

// Every story in the index must have produced a settled capture per viewport x theme. A story
// that was never reached (browser died, filter bug) is a failure, not a silent skip.
const expectedSettled = stories.length * viewports.length * themes.length;
const settledOk = storyResults.filter((r) => r.status === "ok").length;
const reached = new Set(storyResults.map((r) => `${r.id}|${r.viewport}|${r.theme}`));
const unreached = [];
for (const story of stories) for (const viewport of viewports) for (const theme of themes) {
  if (!reached.has(`${story.id}|${viewport.name}|${theme}`)) unreached.push(`${story.id}__${viewport.name}__${theme}`);
}
for (const miss of unreached) failures.push(`${miss}: story never reached (no capture attempted)`);

const summary = {
  lane: "admin-web-stories-visual",
  stories_in_index: allStoryCount,
  stories_selected: stories.length,
  stories_passed: settledOk,
  stories_failed: storyResults.filter((r) => r.status === "fail").length,
  stories_unreached: unreached.length,
  expected_settled_captures: expectedSettled,
  stories: stories.length,
  viewports: viewports.map((viewport) => `${viewport.name}:${viewport.width}x${viewport.height}`),
  themes,
  captures: captured,
  expected_captures: stories.length * viewports.length * themes.length,
  baseline: baseline.summary(),
  ignored_transport_errors: transportNoise,
  motion_frames_captured_on_real_clock: realtimeFrames,
  integrity_findings: integrityFindings.length,
  integrity_by_check: integrityFindings.reduce((acc, f) => ({ ...acc, [f.check]: (acc[f.check] ?? 0) + 1 }), {}),
  screenshot_dir: relativeToRepo(outDir),
  failures,
};
writeFileSync(join(outDir, "integrity.json"), `${JSON.stringify(integrityFindings, null, 2)}\n`);
writeFileSync(join(outDir, "stories.json"), `${JSON.stringify(storyResults, null, 2)}\n`);
writeFileSync(join(outDir, "summary.json"), `${JSON.stringify(summary, null, 2)}\n`);
console.log(JSON.stringify(summary, null, 2));

if (failures.length > 0) {
  console.error(`\nFAIL: ${failures.length} story capture(s) failed:`);
  for (const failure of failures.slice(0, 40)) console.error(`  - ${failure}`);
  process.exit(1);
}
console.log(`\nOK: ${captured} story captures across ${viewports.length} viewport(s) x ${themes.length} theme(s).`);

/**
 * Wait for the DOM under `selector` to stop mutating (MutationObserver + rAF text sample) —
 * the state-based replacement for "sleep 400ms then screenshot". Resolves when two animation
 * frames pass with no mutation and unchanged innerText, or at the budget.
 */
async function waitForSettledDom(page, selector, budgetMs = 2_000) {
  await page
    .evaluate(
      ({ selector, budgetMs }) =>
        new Promise((resolve) => {
          const root = document.querySelector(selector) ?? document.body;
          let quietFrames = 0;
          let lastText = root.innerText;
          let dirty = false;
          const observer = new MutationObserver(() => { dirty = true; });
          observer.observe(root, { subtree: true, childList: true, characterData: true, attributes: true });
          const deadline = performance.now() + budgetMs;
          const tick = () => {
            const text = root.innerText;
            if (!dirty && text === lastText) quietFrames += 1;
            else quietFrames = 0;
            dirty = false;
            lastText = text;
            if (quietFrames >= 2 || performance.now() > deadline) {
              observer.disconnect();
              resolve();
              return;
            }
            requestAnimationFrame(tick);
          };
          requestAnimationFrame(tick);
        }),
      { selector, budgetMs },
    )
    .catch(() => {});
}

async function readMotionFrames(page, storyId) {
  return page
    .evaluate((id) => {
      const preview = window.__STORYBOOK_PREVIEW__;
      const render = (preview?.storyRenders ?? []).find((candidate) => candidate.id === id);
      const params = render?.story?.parameters?.motionFrames;
      return params ? JSON.parse(JSON.stringify(params)) : null;
    }, storyId)
    .catch(() => null);
}

function resolveTrigger(page, trigger) {
  const match = /^role=([a-z]+)(?:\[name=(.+)\])?$/i.exec(trigger.selector ?? "");
  if (match) return page.getByRole(match[1], match[2] ? { name: match[2] } : undefined).first();
  return page.locator(trigger.selector).first();
}

/** Resolve the trigger point and fire it from inside the page (no input-event ack needed). */
async function resolveTriggerPoint(page, motion) {
  // Layout queries need a live clock: always called BEFORE virtual time is paused.
  const target = resolveTrigger(page, motion.trigger ?? {});
  await target.waitFor({ state: "visible", timeout: 10_000 });
  await target.scrollIntoViewIfNeeded().catch(() => {});
  const box = await target.boundingBox();
  if (!box) throw new Error(`motion trigger has no box: ${motion.trigger?.selector}`);
  const pos = motion.trigger?.position ?? { x: 0.5, y: 0.5 };
  return { x: box.x + box.width * pos.x, y: box.y + box.height * pos.y };
}

async function fireTrigger(page, motion, point) {
  // Dispatch from inside the page: real input events wait for a rendering frame that a paused
  // clock never produces, and React listens to the synthetic events all the same.
  await page.evaluate(
    ({ x, y, type }) => {
      const el = document.elementFromPoint(x, y);
      if (!el) throw new Error(`nothing at ${x},${y}`);
      const init = { bubbles: true, cancelable: true, clientX: x, clientY: y, view: window };
      if (type === "hover") {
        for (const name of ["pointerover", "pointerenter", "mouseover", "mouseenter", "pointermove", "mousemove"]) el.dispatchEvent(name.startsWith("pointer") ? new PointerEvent(name, { ...init, pointerType: "mouse" }) : new MouseEvent(name, init));
      } else {
        for (const name of ["pointerdown", "mousedown", "pointerup", "mouseup", "click"]) el.dispatchEvent(name.startsWith("pointer") ? new PointerEvent(name, { ...init, pointerType: "mouse", button: 0 }) : new MouseEvent(name, { ...init, button: 0 }));
      }
    },
    { x: point.x, y: point.y, type: motion.trigger?.type === "hover" ? "hover" : "click" },
  );
}

function frameTimes(motion) {
  return [...new Set((motion.at ?? [0, 60, 180, 300]).map((t) => Math.max(0, Math.round(t))))].sort((a, b) => a - b);
}

/** Deterministic frames: pause the page clock (CDP virtual time), trigger, advance to each offset. */
async function captureMotionFrames(page, motion, pathFor) {
  const times = frameTimes(motion);
  const cdp = await page.context().newCDPSession(page);
  // Upper bound on waiting for the CDP budget-expired event (an event wait, not a sleep).
  const timeoutIn = (ms) => {
    let fail;
    const bound = new Promise((_, reject) => {
      fail = reject;
    });
    setTimeout(() => fail(new Error(`motion frame budget did not expire within ${ms}ms`)), ms);
    return bound;
  };
  const advance = (budget) =>
    new Promise((done) => {
      if (budget <= 0) return done();
      cdp.once("Emulation.virtualTimeBudgetExpired", () => done());
      cdp.send("Emulation.setVirtualTimePolicy", { policy: "advance", budget });
    });
  const frames = [];
  const point = await resolveTriggerPoint(page, motion);
  try {
    await cdp.send("Emulation.setVirtualTimePolicy", { policy: "pause" });
    await fireTrigger(page, motion, point);
    let clock = 0;
    for (const at of times) {
      await Promise.race([advance(at - clock), timeoutIn(8_000)]);
      clock = at;
      const path = pathFor(at);
      await Promise.race([page.screenshot({ path, fullPage: false, animations: "allow", caret: "hide" }), timeoutIn(15_000)]);
      frames.push({ at, path, name: path.slice(path.lastIndexOf("/") + 1), realtime: false });
    }
  } finally {
    await cdp.detach().catch(() => {});
  }
  return frames;
}

/** Fallback frames on the real clock (used only when virtual time stalls). */
async function captureMotionFramesRealtime(page, motion, pathFor) {
  const times = frameTimes(motion);
  const frames = [];
  const point = await resolveTriggerPoint(page, motion);
  const startedAt = Date.now();
  await fireTrigger(page, motion, point);
  for (const at of times) {
    const wait = at - (Date.now() - startedAt);
    // Real-clock fallback only: this offset IS the thing being measured, so it is a
    // frame-aligned wait inside the page, not a harness sleep.
    if (wait > 0) await page.evaluate((ms) => new Promise((resolve) => { const t0 = performance.now(); const tick = () => (performance.now() - t0 >= ms ? resolve() : requestAnimationFrame(tick)); tick(); }), wait);
    const path = pathFor(at);
    await page.screenshot({ path, fullPage: false, animations: "allow", caret: "hide", timeout: 15_000 });
    frames.push({ at, path, name: path.slice(path.lastIndexOf("/") + 1), realtime: true });
  }
  return frames;
}

async function waitForStoryPhase(page, storyId) {
  return page.waitForFunction(
    (id) => {
      const preview = window.__STORYBOOK_PREVIEW__;
      if (!preview) return false;
      const renders = preview.storyRenders ?? [];
      const render = renders.find((candidate) => candidate.id === id) ?? renders[renders.length - 1];
      if (!render) return false;
      const phase = render.phase;
      return ["finished", "completed", "played", "errored", "aborted"].includes(phase) ? phase : false;
    },
    storyId,
    { timeout: 60_000 },
  ).then((handle) => handle.jsonValue());
}

function startStaticServer(root, listenPort) {
  const types = {
    ".html": "text/html; charset=utf-8",
    ".js": "text/javascript; charset=utf-8",
    ".mjs": "text/javascript; charset=utf-8",
    ".css": "text/css; charset=utf-8",
    ".json": "application/json; charset=utf-8",
    ".png": "image/png",
    ".jpg": "image/jpeg",
    ".svg": "image/svg+xml",
    ".woff": "font/woff",
    ".woff2": "font/woff2",
    ".map": "application/json; charset=utf-8",
  };
  const httpServer = createServer((request, response) => {
    const url = new URL(request.url ?? "/", "http://127.0.0.1");
    let pathname = decodeURIComponent(url.pathname);
    if (pathname.endsWith("/")) pathname += "index.html";
    // Path traversal guard: serve only from inside the static build.
    const filePath = normalize(join(root, pathname));
    if (!filePath.startsWith(root) || !existsSync(filePath)) {
      response.writeHead(404).end("not found");
      return;
    }
    response.writeHead(200, { "content-type": types[extname(filePath)] ?? "application/octet-stream" });
    createReadStream(filePath).pipe(response);
  });
  return new Promise((ready, reject) => {
    httpServer.once("error", reject);
    httpServer.listen(listenPort, "127.0.0.1", () => ready(httpServer));
  });
}

function relativeToRepo(path) {
  return path.startsWith(repoRoot) ? path.slice(repoRoot.length + 1) : path;
}

function normalizeRepoPath(path) {
  if (!path) return undefined;
  return isAbsolute(path) ? path : join(repoRoot, path);
}

function parseArgs(argv) {
  const parsed = {
    baselineDir: undefined,
    updateBaseline: false,
    waive: null,
    requireBaseline: false,
    maxDiffRatio: undefined,
    only: undefined,
    viewports: undefined,
    themes: undefined,
    build: true,
    port: undefined,
    storybookUrl: process.env.GOATOS_STORYBOOK_URL,
    integrity: true,
    frames: true,
  };
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === "--baseline-dir") parsed.baselineDir = argv[++index];
    else if (arg === "--update-baseline") parsed.updateBaseline = true;
    else if (arg === "--waive") { parsed.waive = String(argv[++index] ?? "").trim(); if (!parsed.waive) throw new Error("--waive needs a reason"); }
    else if (arg === "--require-baseline") parsed.requireBaseline = true;
    else if (arg === "--no-build") parsed.build = false;
    else if (arg === "--storybook-url") parsed.storybookUrl = argv[++index];
    else if (arg === "--no-integrity") parsed.integrity = false;
    else if (arg === "--no-frames") parsed.frames = false;
    else if (arg === "--only") parsed.only = argv[++index];
    else if (arg === "--port") parsed.port = Number(argv[++index]);
    else if (arg === "--viewports") parsed.viewports = String(argv[++index]).split(",").map((s) => s.trim()).filter(Boolean);
    else if (arg === "--themes") parsed.themes = String(argv[++index]).split(",").map((s) => s.trim()).filter(Boolean);
    else if (arg === "--max-diff-ratio") {
      parsed.maxDiffRatio = Number(argv[++index]);
      if (!Number.isFinite(parsed.maxDiffRatio) || parsed.maxDiffRatio < 0 || parsed.maxDiffRatio > 1) {
        throw new Error("--max-diff-ratio must be a number between 0 and 1");
      }
    } else throw new Error(`Unknown argument: ${arg}`);
  }
  return parsed;
}
