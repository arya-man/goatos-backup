// Drives a page the way a person does, films it, and hands the film to the detector.
//
// Two things here are not negotiable and are the reason this file exists at all:
//
// 1. FRAME RATE. Flicker at 60Hz is invisible to `page.screenshot()`, which takes
//    tens to hundreds of milliseconds per call — by the time the second screenshot
//    lands the flicker has been and gone several times over. CDP's
//    `Page.startScreencast` pushes a frame whenever the compositor produces one,
//    which is the only way to see something that only exists between frames.
//
// 2. EVIDENCE. A still PNG cannot show flicker. Attaching one and calling it proof
//    is worse than attaching nothing, because it looks like evidence and isn't.
//    Everything this module produces for a person to look at is either an animated
//    GIF or a filmstrip of the frames either side of the event.
import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, mkdirSync, readdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { detectFlicker, grayFrameFromRgba } from "./flicker-detector.mjs";
import { assessSubstance, collectSubstance, gateContentCheck } from "./page-substance.mjs";

export const CAPTURE_DEFAULTS = Object.freeze({
  // Long enough to catch a once-a-second recurrence several times over.
  seconds: 8,
  // Twice the phone viewport. This is not a nicety: at 1:1 the screencast downscales
  // the page so hard that the text and buttons bleeding through a panel smear into its
  // background, and a live run of the real bug measured as nothing. At 2x the same run
  // finds it. Detection downsamples again afterwards, so the cost is one decode.
  maxWidth: 780,
  maxHeight: 1688,
  downsampleStep: 8,
});

// The OCI box has no ffmpeg on PATH, and without one a flicker finding degrades to a
// pile of separate PNGs — which is the one thing this lane must not send, because a
// person cannot see flicker in a still. Playwright ships an ffmpeg beside its browsers,
// so look there before giving up.
let ffmpegPath = null;
function ffmpeg() {
  if (ffmpegPath !== null) return ffmpegPath || null;
  const candidates = ["ffmpeg"];
  const root = process.env.PLAYWRIGHT_BROWSERS_PATH;
  if (root) {
    try {
      for (const entry of readdirSync(root)) {
        if (!entry.startsWith("ffmpeg")) continue;
        for (const name of ["ffmpeg-linux", "ffmpeg-mac", "ffmpeg.exe"]) {
          const candidate = join(root, entry, name);
          if (existsSync(candidate)) candidates.push(candidate);
        }
      }
    } catch {
      /* no browsers directory: fall back to PATH */
    }
  }
  for (const candidate of candidates) {
    if (spawnSync(candidate, ["-version"], { stdio: "ignore" }).status === 0) {
      ffmpegPath = candidate;
      return candidate;
    }
  }
  ffmpegPath = "";
  return null;
}
function hasFfmpeg() {
  return ffmpeg() !== null;
}

/**
 * What a person does on a page in the few seconds after it loads. Scrolling is the
 * point: the sticky-plus-blur bug only tears when content moves underneath the
 * pinned element, so a capture that never scrolls cannot see it.
 *
 * Each phase is recorded with its time window so the report can say what the
 * person was doing when the screen misbehaved, in words, not in frame numbers.
 */
export async function driveLikeAPerson(page, mark) {
  await mark("look at the page", async () => {
    await page.waitForTimeout(500);
  });
  await mark("scroll down the page", async () => {
    for (let i = 0; i < 6; i += 1) {
      await page.mouse.wheel(0, 240);
      await page.waitForTimeout(90);
    }
  });
  await mark("scroll back up", async () => {
    for (let i = 0; i < 6; i += 1) {
      await page.mouse.wheel(0, -240);
      await page.waitForTimeout(90);
    }
  });
  // The recording that started this lane has the filter panel open, and the panel
  // is the element carrying the blur. Opening it is part of the journey, not a
  // nicety. Nothing here submits or changes anything: it opens a panel and scrolls.
  await mark("open the filters and scroll", async () => {
    const opener = page
      .locator('button:has-text("Filters"), [aria-label="Filters"], .lt-fmore, .lt-fsheet-open')
      .first();
    if (await opener.count().catch(() => 0)) {
      await opener.click({ timeout: 2_000 }).catch(() => {});
      await page.waitForTimeout(400);
      for (let i = 0; i < 4; i += 1) {
        await page.mouse.wheel(0, 200);
        await page.waitForTimeout(90);
      }
    } else {
      for (let i = 0; i < 4; i += 1) {
        await page.mouse.wheel(0, 200);
        await page.waitForTimeout(90);
      }
    }
  });
}

/**
 * Film the page while `drive` exercises it.
 *
 * @returns {{ frames, phases, result }} frames carry their PNG bytes so evidence
 *          can be written without filming twice.
 */
export async function captureFlicker(page, options = {}) {
  const o = { ...CAPTURE_DEFAULTS, ...options };
  const drive = o.drive ?? driveLikeAPerson;
  const { PNG } = await import("pngjs");

  const cdp = await page.context().newCDPSession(page);
  const frames = [];
  let t0 = null;
  cdp.on("Page.screencastFrame", async (event) => {
    // Acknowledge FIRST and always: an unacknowledged frame stops the screencast
    // dead, and a capture that silently stops half way through looks exactly like
    // a page that does not flicker.
    try {
      await cdp.send("Page.screencastFrameAck", { sessionId: event.sessionId });
    } catch {
      /* the session went away; nothing to do but stop collecting */
    }
    try {
      const png = Buffer.from(event.data, "base64");
      const decoded = PNG.sync.read(png);
      const stamp = Number(event.metadata?.timestamp ?? 0);
      if (t0 === null) t0 = stamp;
      frames.push({
        t: stamp - t0,
        png,
        gray: grayFrameFromRgba(decoded.data, decoded.width, decoded.height, {
          t: stamp - t0,
          step: o.downsampleStep,
        }),
      });
    } catch {
      /* one undecodable frame must not end the capture */
    }
  });

  const phases = [];
  const mark = async (label, fn) => {
    const from = frames.length ? frames[frames.length - 1].t : 0;
    await fn();
    const to = frames.length ? frames[frames.length - 1].t : from;
    phases.push({ label, from, to });
  };

  await cdp.send("Page.startScreencast", {
    format: "png",
    everyNthFrame: 1,
    maxWidth: o.maxWidth,
    maxHeight: o.maxHeight,
  });
  try {
    await drive(page, mark);
    await page.waitForTimeout(600); // let a trailing recurrence land
  } finally {
    await cdp.send("Page.stopScreencast").catch(() => {});
    await cdp.detach().catch(() => {});
  }

  const result = detectFlicker(
    frames.map((f) => f.gray),
    o,
  );
  // Say what the person was doing when it happened, not which frame it was.
  for (const event of result.events) {
    const phase = phases.find((p) => event.startT >= p.from && event.startT <= p.to);
    event.whileDoing = phase?.label ?? "using the page";
  }
  return { frames, phases, result };
}

/** The phase that most of the flicker happened in, for the one-sentence finding. */
export function dominantPhase(result) {
  const counts = new Map();
  for (const event of result.events ?? []) {
    const key = event.whileDoing ?? "using the page";
    counts.set(key, (counts.get(key) ?? 0) + 1);
  }
  let best = null;
  for (const [key, n] of counts) if (!best || n > best[1]) best = [key, n];
  return best ? best[0] : "using the page";
}

/**
 * Write something a person can actually look at: an animated GIF of the offending
 * stretch, plus a filmstrip of the frames either side of it.
 *
 * Returns { gif, filmstrip, frames: [paths], note } — any of the first two may be
 * null if ffmpeg is not on this machine, and `note` says so rather than pretending.
 */
export async function writeFlickerEvidence(frames, result, outDir, name) {
  mkdirSync(outDir, { recursive: true });
  const event = (result.events ?? [])[0];
  if (!event) return { gif: null, filmstrip: null, frames: [], note: "no flicker to show" };

  // A couple of settled frames either side, so the GIF reads as "fine, wrong, fine".
  const first = frames.findIndex((f) => f.gray.t >= event.startT);
  const last = frames.findIndex((f) => f.gray.t >= event.endT);
  const from = Math.max(0, (first < 0 ? 0 : first) - 2);
  const to = Math.min(frames.length - 1, (last < 0 ? frames.length - 1 : last) + 2);

  const seqDir = join(outDir, `${name}-frames`);
  mkdirSync(seqDir, { recursive: true });
  const written = [];
  for (let i = from; i <= to; i += 1) {
    const file = join(seqDir, `f${String(written.length + 1).padStart(3, "0")}.png`);
    writeFileSync(file, frames[i].png);
    written.push(file);
  }
  const filmstrip = written.length >= 2
    ? await composeFilmstrip(frames.slice(from, to + 1).map((f) => f.png), join(outDir, `${name}-flicker-filmstrip.png`))
    : null;
  let gif = null;
  if (hasFfmpeg() && written.length >= 2) {
    const candidate = join(outDir, `${name}-flicker.gif`);
    try {
      // Slow enough that a person can see the wrong frame go past, looping forever.
      execFileSync(
        ffmpeg(),
        ["-v", "error", "-y", "-framerate", "6", "-i", join(seqDir, "f%03d.png"), "-vf", "scale=390:-1:flags=lanczos", "-loop", "0", candidate],
        { stdio: "ignore" },
      );
      if (existsSync(candidate)) gif = candidate;
    } catch {
      /* the filmstrip already carries the evidence */
    }
  }
  return {
    gif,
    filmstrip,
    frames: written,
    note: filmstrip ? "" : "the frames are attached one by one, because a strip of them could not be written here",
  };
}

/**
 * Film a list of routes at the phone viewport.
 *
 * The browser is launched from here rather than from the runner in
 * tools/dashboard-automation on purpose: Playwright and pngjs are admin-web's
 * dependencies, and they only resolve for a module that lives under admin-web.
 *
 * Read-only throughout. It loads pages, scrolls them and opens panels.
 */
export async function filmRoutes({ baseUrl, bearerToken, outDir, routes, phone }) {
  const { chromium } = await import("@playwright/test");
  const browser = await chromium.launch();
  const runs = [];
  try {
    const context = await browser.newContext({
      viewport: { width: phone.width, height: phone.height },
      isMobile: true,
      hasTouch: true,
      deviceScaleFactor: phone.deviceScaleFactor,
      userAgent:
        "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UQ1A.240205.004; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/128.0.6613.127 Mobile Safari/537.36",
    });
    if (bearerToken) {
      const cookieUrl = new URL(baseUrl);
      await context.addCookies([{
        name: "goatos_firebase_id_token",
        value: bearerToken,
        domain: cookieUrl.hostname,
        path: "/",
        httpOnly: true,
        sameSite: "Lax",
        expires: Math.floor(Date.now() / 1000) + 3600,
      }]);
    }
    for (const route of routes) {
      const page = await context.newPage();
      const url = `${baseUrl}${route.path}`;
      try {
        // Wait for the page to actually have its content. A panel filmed over an empty
        // list has nothing behind it to show through, so an eager load turns a real
        // defect into a clean result.
        await page.goto(url, { waitUntil: "networkidle", timeout: 90_000 }).catch(async () => {
          await page.goto(url, { waitUntil: "domcontentloaded", timeout: 45_000 });
        });
        await page.waitForTimeout(3_500);
        // A page that bounced to a login screen was not the page we meant to film,
        // and a clean result on it would be a false green.
        const landedOn = new URL(page.url()).pathname;
        if (route.path && !route.path.startsWith(landedOn.replace(/\/$/, "")) && /login|signin|sign-in/i.test(landedOn)) {
          // A run that filmed a login screen says nothing about the page we meant to
          // film, and reporting it as "no flicker" would be the purest kind of fake
          // green: a clean result about a page nobody asked about. Park it instead.
          runs.push({
            route: route.name,
            pageName: route.pageName,
            url,
            landedOn,
            parked: "this page needs a signed-in session and the run did not have one, so it was never opened",
          });
          continue;
        }
        // Two journeys, because the two bugs have nothing in common. Scrolling looks
        // for a shimmer over the whole screen; opening a panel and changing a filter
        // looks for a solid overlay that stops painting its own background. The
        // second is the one Ravi filmed, and no amount of scrolling would find it.
        const { frames, phases, result } = await captureFlicker(page);
        const evidence = result.flicker
          ? await writeFlickerEvidence(frames, result, outDir, route.name)
          : { gif: null, filmstrip: null, frames: [], note: "" };

        let overlay = null;
        try {
          const paint = await captureOverlayPaint(page);
          const showedThrough = paint.findings.filter((f) => f.showedThrough);
          overlay = {
            opened: paint.opened,
            interactions: paint.interactions ?? 0,
            capturedFrames: paint.frames.length,
            watched: paint.overlays.map((o) => ({ label: o.label, z: o.z })),
            // An overlay this check cannot judge is said so, never counted as clean.
            unjudged: paint.findings.filter((f) => !f.judged).map((f) => ({ label: f.overlay.label, why: f.reason })),
            findings: await Promise.all(showedThrough.map(async (finding) => ({
              label: finding.overlay.label,
              events: finding.events,
              evidence: await writeOverlayEvidence(
                paint.frames,
                finding.events[0],
                outDir,
                `${route.name}-${(finding.overlay.label || "panel").toLowerCase().replace(/[^a-z0-9]+/g, "-")}`,
              ),
            }))),
            reason: paint.reason,
          };
        } catch (error) {
          overlay = { error: String(error?.message ?? error).split("\n")[0] };
        }

        runs.push({
          route: route.name,
          pageName: route.pageName,
          url,
          landedOn,
          capturedFrames: frames.length,
          framesPerSecond: result.spanSeconds ? Number((frames.length / result.spanSeconds).toFixed(1)) : 0,
          phases: phases.map((p) => p.label),
          whileDoing: result.flicker ? dominantPhase(result) : "",
          result,
          evidence,
          overlay,
        });
      } catch (error) {
        runs.push({ route: route.name, pageName: route.pageName, url, error: String(error?.message ?? error).split("\n")[0] });
      } finally {
        await page.close().catch(() => {});
      }
    }
    await context.close();
  } finally {
    await browser.close();
  }
  return runs;
}

// ---------------------------------------------------------------------------
// The overlay-paint journey: open a panel, change something, watch the panel
// ---------------------------------------------------------------------------
// This is the half that finds the bug Ravi actually filmed. It is a different shape
// from the scroll journey above on purpose: the defect is triggered by CHANGING A
// FILTER while an opaque panel is open, and no amount of scrolling will ever produce
// it. Read-only throughout — it opens a panel and ticks a filter box. Nothing is
// submitted, nothing is saved.

/** The rows a person ticks to filter a list. */
const OPTION_ROWS =
  '.lt-people-row, .lt-fgroup.open input[type="checkbox"], .lt-fsheet input[type="checkbox"], .lt-fgroup.open [role="option"]';

/**
 * Open the filter panel AND get its options on screen.
 *
 * Both halves matter. On /tasks the panel opens showing "Assignee / Raised by / Sort /
 * Dates" and the list of people is one more tap away, so a driver that stops at the
 * panel has nothing to tick and the check reports "could not judge it" — which is what
 * the first production run did. The options are revealed here, before the overlay is
 * measured, so the panel's box and colour are read once it has finished growing.
 */
export async function openFilterPanel(page) {
  // By the button's accessible name, not by a list of class guesses. The guessy
  // version matched some other button in the filter bar first and opened nothing,
  // which reads downstream as "this page has no panel" — a clean result about a
  // panel that was never opened.
  let opener = page.getByRole("button", { name: /^Filters$/i }).first();
  if (!(await opener.count().catch(() => 0))) {
    opener = page.locator('[aria-label="Filters"], .lt-fmore, .lt-fsheet-open').first();
  }
  if (!(await opener.count().catch(() => 0))) return false;
  await opener.click({ timeout: 5_000 }).catch(() => {});
  await page.waitForTimeout(1_500);

  if (!(await page.locator(OPTION_ROWS).count().catch(() => 0))) {
    const discloser = page.locator("text=/^Assignee/").first();
    if (await discloser.count().catch(() => 0)) {
      await discloser.click({ timeout: 5_000 }).catch(() => {});
      await page.waitForTimeout(1_500);
    }
  }
  return (await page.locator(OPTION_ROWS).count().catch(() => 0)) > 0;
}

/**
 * Tick filter options one at a time, pausing between, so each change is a separate
 * moment in the film. Only rows inside the open panel are touched, and only ones that
 * read as a filter option — never a button that could save, apply, clear or submit.
 */
export async function changeFilterOptions(page, mark, { times = 5 } = {}) {
  const options = page.locator(OPTION_ROWS);
  const available = await options.count().catch(() => 0);
  if (!available) return 0;
  let changed = 0;
  for (let i = 0; i < Math.min(times, available - 1); i += 1) {
    // Skip the first row: on this page it is "All", which clears rather than filters.
    const option = options.nth(i + 1);
    await mark("change the filters", async () => {
      await option.click({ timeout: 4_000 }).catch(() => {});
      // Long enough for the list behind to re-render, which is what triggers it.
      await page.waitForTimeout(1_300);
    });
    changed += 1;
  }
  return changed;
}

/**
 * Film an opaque overlay while the page is driven, and answer one question: did the
 * overlay ever stop painting its own background?
 *
 * The overlays are probed from the live page AFTER the panel has opened and settled,
 * so their colours and boxes are what the browser actually computed rather than what
 * a stylesheet suggests.
 */
export async function captureOverlayPaint(page, options = {}) {
  const o = { ...CAPTURE_DEFAULTS, ...options };
  const { PNG } = await import("pngjs");
  const { collectOpaqueOverlays, detectShowThrough, interiorFrame } = await import("./overlay-paint-checks.mjs");

  const opened = await (o.open ?? openFilterPanel)(page);
  const overlays = await page.evaluate(collectOpaqueOverlays, {});
  const viewport = page.viewportSize() ?? { width: o.maxWidth, height: o.maxHeight };
  if (!overlays.length) {
    return { opened, overlays: [], frames: [], phases: [], findings: [], reason: "this page has no solid panel open to watch" };
  }

  const cdp = await page.context().newCDPSession(page);
  const frames = [];
  let t0 = null;
  cdp.on("Page.screencastFrame", async (event) => {
    try {
      await cdp.send("Page.screencastFrameAck", { sessionId: event.sessionId });
    } catch {
      /* the session went away */
    }
    try {
      const png = Buffer.from(event.data, "base64");
      const stamp = Number(event.metadata?.timestamp ?? 0);
      if (t0 === null) t0 = stamp;
      frames.push({ t: stamp - t0, png });
    } catch {
      /* one undecodable frame must not end the capture */
    }
  });

  const phases = [];
  const mark = async (label, fn) => {
    const from = frames.length ? frames[frames.length - 1].t : 0;
    await fn();
    const to = frames.length ? frames[frames.length - 1].t : from;
    phases.push({ label, from, to });
  };

  await cdp.send("Page.startScreencast", { format: "png", everyNthFrame: 1, maxWidth: o.maxWidth, maxHeight: o.maxHeight });
  let interactions = 0;
  try {
    interactions = await (o.interact ?? changeFilterOptions)(page, mark);
    await page.waitForTimeout(700);
  } finally {
    await cdp.send("Page.stopScreencast").catch(() => {});
    await cdp.detach().catch(() => {});
  }

  // Decode once per frame and measure every overlay from it, so a long capture never
  // holds more than one decoded frame in memory.
  const series = overlays.map(() => []);
  for (const frame of frames) {
    let decoded;
    try {
      decoded = PNG.sync.read(frame.png);
    } catch {
      continue;
    }
    const scale = decoded.width / viewport.width;
    overlays.forEach((overlay, index) => {
      series[index].push(
        interiorFrame(decoded.data, decoded.width, decoded.height, overlay.rect, { ...o, scale, t: frame.t }),
      );
    });
  }

  const findings = overlays.map((overlay, index) => ({
    overlay,
    ...detectShowThrough(series[index], o),
    samples: series[index].length,
  }));
  return { opened, overlays, frames, phases, findings, interactions, reason: "" };
}

/**
 * Stitch consecutive frames side by side into one PNG: fine, wrong, fine.
 *
 * This is written by hand rather than shelled out to ffmpeg because the evidence for
 * this lane cannot be optional. The OCI box has no ffmpeg on PATH, and the one
 * Playwright ships beside its browsers is a stripped build with no image-sequence
 * demuxer — it reports "No such file or directory" for a perfectly good run of PNGs.
 * Both were found the hard way, on a real production finding that came back with no
 * picture attached. pngjs is already a dependency because the frames arrive as PNG, so
 * a filmstrip always works, everywhere, and a flicker finding is never reduced to a
 * single still that cannot show flicker.
 */
export async function composeFilmstrip(pngBuffers, outFile, { targetWidth = 260, gap = 8 } = {}) {
  if (pngBuffers.length < 2) return null;
  const { PNG } = await import("pngjs");
  let decoded;
  try {
    decoded = pngBuffers.map((buffer) => PNG.sync.read(buffer));
  } catch {
    return null;
  }
  const source = decoded[0];
  const scale = Math.max(1, Math.round(source.width / targetWidth));
  const w = Math.floor(source.width / scale);
  const h = Math.floor(source.height / scale);
  const out = new PNG({ width: w * decoded.length + gap * (decoded.length - 1), height: h });
  out.data.fill(0);
  for (let i = 3; i < out.data.length; i += 4) out.data[i] = 255;
  decoded.forEach((frame, index) => {
    if (frame.width !== source.width || frame.height !== source.height) return;
    const offsetX = index * (w + gap);
    for (let y = 0; y < h; y += 1) {
      for (let x = 0; x < w; x += 1) {
        const from = (y * scale * frame.width + x * scale) * 4;
        const to = (y * out.width + offsetX + x) * 4;
        out.data[to] = frame.data[from];
        out.data[to + 1] = frame.data[from + 1];
        out.data[to + 2] = frame.data[from + 2];
        out.data[to + 3] = 255;
      }
    }
  });
  writeFileSync(outFile, PNG.sync.write(out));
  return outFile;
}

/**
 * Before / during / after, from the capture, for an overlay that showed through.
 * The middle frame is the defect; the two either side are what it should look like.
 * A single still is never enough here — the whole point is that it came back.
 */
export async function writeOverlayEvidence(frames, event, outDir, name) {
  mkdirSync(outDir, { recursive: true });
  const at = (t) => {
    let best = 0;
    for (let i = 0; i < frames.length; i += 1) if (Math.abs(frames[i].t - t) < Math.abs(frames[best].t - t)) best = i;
    return best;
  };
  const during = at((event.startT + event.endT) / 2);
  const before = Math.max(0, at(event.startT) - 1);
  const after = Math.min(frames.length - 1, at(event.endT) + 1);
  const seqDir = join(outDir, `${name}-frames`);
  mkdirSync(seqDir, { recursive: true });
  const written = [];
  // Every frame across the flash, so the GIF actually moves rather than cutting.
  for (let i = before; i <= after; i += 1) {
    const file = join(seqDir, `f${String(written.length + 1).padStart(3, "0")}.png`);
    writeFileSync(file, frames[i].png);
    written.push(file);
  }
  // The filmstrip is built in-process and always works. The GIF is a bonus when a
  // real ffmpeg happens to be installed.
  const filmstrip = written.length >= 2
    ? await composeFilmstrip(frames.slice(before, after + 1).map((f) => f.png), join(outDir, `${name}-overlay-filmstrip.png`))
    : null;
  let gif = null;
  if (hasFfmpeg() && written.length >= 2) {
    const candidate = join(outDir, `${name}-overlay.gif`);
    try {
      execFileSync(ffmpeg(), ["-v", "error", "-y", "-framerate", "5", "-i", join(seqDir, "f%03d.png"), "-vf", "scale=390:-1:flags=lanczos", "-loop", "0", candidate], { stdio: "ignore" });
      if (existsSync(candidate)) gif = candidate;
    } catch { /* the filmstrip already carries the evidence */ }
  }
  return {
    gif,
    filmstrip,
    frames: written,
    note: filmstrip ? "" : "the frames are attached one by one, because a strip of them could not be written here",
  };
}

// ---------------------------------------------------------------------------
// The full surface: every route, every dialog, both viewports
// ---------------------------------------------------------------------------
// The filmed bug was one filter panel, but the defect class is not: any overlay that
// is opaque by design can briefly show what is behind it. So the sweep drives the
// overlays lane 1 already enumerates in `overlay-journeys.mjs` — the same catalogue,
// the same selectors, the same read-only gate — and then looks for any OTHER opaque
// overlay a page will open when a safe control is pressed.
//
// What it watches and what it deliberately does not: an overlay is only judged while
// it is SETTLED OPEN. Opening and closing legitimately show the page behind, because
// that is what an entrance animation is; reporting those would be reporting the design.
// The transitions judged are changing a control inside the overlay and scrolling
// behind it, which is where the real defect lives.

/** Controls inside an overlay that only change what is displayed. */
const SAFE_INSIDE = [
  'input[type="checkbox"]',
  'input[type="radio"]',
  ".lt-people-row",
  '[role="option"]',
  '[role="tab"]',
  "select",
].join(", ");

/**
 * Press something inside the overlay that only changes what is shown, then scroll the
 * page behind it. Returns how many changes were made.
 */
async function nudgeOverlay(page, overlaySelector, mark) {
  let changes = 0;
  const inside = page.locator(`${overlaySelector} ${SAFE_INSIDE}`);
  const available = await inside.count().catch(() => 0);
  for (let i = 0; i < Math.min(3, available); i += 1) {
    await mark("change something in the panel", async () => {
      await inside.nth(i).click({ timeout: 3_000 }).catch(() => {});
      await page.waitForTimeout(900);
    });
    changes += 1;
  }
  await mark("scroll the page behind it", async () => {
    for (let i = 0; i < 4; i += 1) {
      await page.mouse.wheel(0, 220);
      await page.waitForTimeout(110);
    }
  });
  return changes;
}

/**
 * Film one overlay while it is open, and say whether it ever showed the page behind it.
 *
 * `open` opens the overlay and returns true; `overlaySelector` names it so the controls
 * pressed are the ones inside it.
 */
export async function filmOverlay(page, { open, overlaySelector, outDir, name, options = {} }) {
  const o = { ...CAPTURE_DEFAULTS, ...options };
  const { PNG } = await import("pngjs");
  const { collectOpaqueOverlays, detectShowThrough, interiorFrame } = await import("./overlay-paint-checks.mjs");

  const opened = await open();
  if (!opened) return { opened: false, reason: "the overlay did not open" };
  await page.waitForTimeout(700);

  // Probe once the overlay has settled, so its box and colour are what it really is.
  const overlays = (await page.evaluate(collectOpaqueOverlays, {}).catch(() => [])) ?? [];
  const viewport = page.viewportSize() ?? { width: o.maxWidth / 2, height: o.maxHeight / 2 };
  if (!overlays.length) {
    return { opened: true, overlays: [], findings: [], reason: "nothing opened that is solid enough to judge" };
  }

  const cdp = await page.context().newCDPSession(page);
  const frames = [];
  let t0 = null;
  cdp.on("Page.screencastFrame", async (event) => {
    try {
      await cdp.send("Page.screencastFrameAck", { sessionId: event.sessionId });
    } catch { /* the session went away */ }
    try {
      const stamp = Number(event.metadata?.timestamp ?? 0);
      if (t0 === null) t0 = stamp;
      frames.push({ t: stamp - t0, png: Buffer.from(event.data, "base64") });
    } catch { /* one bad frame must not end the capture */ }
  });
  const phases = [];
  const mark = async (label, fn) => {
    const from = frames.length ? frames[frames.length - 1].t : 0;
    await fn();
    phases.push({ label, from, to: frames.length ? frames[frames.length - 1].t : from });
  };

  await cdp.send("Page.startScreencast", { format: "png", everyNthFrame: 1, maxWidth: o.maxWidth, maxHeight: o.maxHeight });
  let changes = 0;
  try {
    changes = await nudgeOverlay(page, overlaySelector ?? "body", mark);
    await page.waitForTimeout(500);
  } finally {
    await cdp.send("Page.stopScreencast").catch(() => {});
    await cdp.detach().catch(() => {});
  }

  const series = overlays.map(() => []);
  for (const frame of frames) {
    let decoded;
    try {
      decoded = PNG.sync.read(frame.png);
    } catch {
      continue;
    }
    const scale = decoded.width / viewport.width;
    overlays.forEach((overlay, index) => {
      series[index].push(interiorFrame(decoded.data, decoded.width, decoded.height, overlay.rect, { ...o, scale, t: frame.t }));
    });
  }

  const findings = [];
  const unjudged = [];
  for (const [index, overlay] of overlays.entries()) {
    const verdict = detectShowThrough(series[index], o);
    if (!verdict.judged) {
      unjudged.push({ label: overlay.label, why: verdict.reason });
      continue;
    }
    if (!verdict.showedThrough) continue;
    findings.push({
      label: overlay.label,
      events: verdict.events,
      evidence: await writeOverlayEvidence(frames, verdict.events[0], outDir, name),
    });
  }
  return { opened: true, overlays: overlays.map((v) => ({ label: v.label, z: v.z })), findings, unjudged, changes, capturedFrames: frames.length, phases: phases.map((p) => p.label), reason: "" };
}

/**
 * Every overlay this route can open, at this viewport.
 *
 * The catalogued ones come from lane 1's own overlay journeys so the two lanes cannot
 * drift apart. The discovered ones are anything else on the page that opens something
 * solid — and every one of them goes through `assertReadOnlyClickTarget` first, which
 * is the same guard lane 1 uses to make sure a sweep never presses Save, Approve or
 * Delete on production.
 */
export async function overlayCandidates(page, { routeName, viewportLabel, maxDiscovered = 3 }) {
  const { overlayJourneys, assertReadOnlyClickTarget } = await import("./overlay-journeys.mjs");
  const catalogued = (overlayJourneys[routeName] ?? [])
    .filter((step) => !step.viewports || step.viewports.includes(viewportLabel))
    .map((step) => ({ id: step.id, kind: step.kind, trigger: step.trigger, overlay: step.overlay, close: step.close, catalogued: true }));

  const discovered = [];
  const triggers = page.locator('[aria-haspopup="dialog"], [aria-haspopup="menu"], [data-overlay-trigger], button[aria-expanded="false"]');
  const count = await triggers.count().catch(() => 0);
  for (let i = 0; i < Math.min(count, maxDiscovered * 3) && discovered.length < maxDiscovered; i += 1) {
    const target = triggers.nth(i);
    try {
      await assertReadOnlyClickTarget(target, { allowDialogTrigger: true });
    } catch {
      continue; // it could write something: never pressed on production
    }
    discovered.push({ id: `opened-${discovered.length + 1}`, kind: "discovered", locator: target, overlay: null, catalogued: false });
  }
  return [...catalogued, ...discovered];
}

/**
 * The whole surface: every route given, every overlay it opens, at one viewport.
 *
 * Coverage is counted as it goes and every gap carries its reason, because the only
 * number worth reporting here is a fraction. "No flicker found" over a sweep that
 * silently skipped half the routes is the kind of green this automation exists to
 * stop producing.
 */
/**
 * Is this landed page worth filming, and if not, why not?
 *
 * A function rather than a branch inside the sweep loop, for §8's reason: a
 * check that can only be reached by opening a browser against a real site is a
 * check nothing can hold to account. sweepViewport calls THIS.
 *
 * @param {{landedOn: string, snapshot: object}} args
 * @returns {{film: boolean, parked?: string, blankPage?: boolean}}
 */
export function judgeLandedPage({ landedOn = "", snapshot = {} } = {}) {
  if (/login|signin|sign-in/i.test(landedOn)) {
    // A run that filmed a login screen says nothing about the page it aimed at.
    return { film: false, parked: "this page needs a signed-in session and the run did not have one" };
  }
  // A page that STAYED PUT and rendered nothing — an error state, a "something
  // went wrong" fallback, an empty shell — used to be filmed, produce identical
  // frames, and be reported clean: twelve identical blank frames return
  // `flicker: false` with the same verdict a correct page gets. That is §3's
  // failure reproduced inside the new check. The redirect test above covers an
  // expired token; this covers everything else.
  const substance = assessSubstance(snapshot);
  const gate = gateContentCheck(substance);
  if (gate.judge) return { film: true, substance: substance.verdict };
  return {
    film: false,
    substance: substance.verdict,
    parked: gate.finding ?? gate.notAttempted,
    blankPage: Boolean(gate.finding),
  };
}

export async function sweepViewport({ baseUrl, bearerToken, outDir, routes, viewport, onRoute }) {
  const { chromium } = await import("@playwright/test");
  let browser = null;
  let context = null;
  // A sweep of this many pages will lose a browser somewhere: a page crashes, a tab runs out
  // of memory, a renderer dies. Losing the browser must cost ONE route, not every
  // route after it — the first run of this sweep died on page four and reported
  // nothing at all. (It said "118 pages" here; the count has moved since and a
  // number baked into a comment only goes stale, so it no longer names one.)
  const freshContext = async () => {
    await context?.close().catch(() => {});
    await browser?.close().catch(() => {});
    browser = await chromium.launch();
    context = await browser.newContext({
      viewport: { width: viewport.width, height: viewport.height },
      isMobile: Boolean(viewport.isMobile),
      hasTouch: Boolean(viewport.isMobile),
      deviceScaleFactor: viewport.deviceScaleFactor ?? 1,
      userAgent: viewport.userAgent,
    });
    if (bearerToken) {
      const cookieUrl = new URL(baseUrl);
      await context.addCookies([{
        name: "goatos_firebase_id_token",
        value: bearerToken,
        domain: cookieUrl.hostname,
        path: "/",
        httpOnly: true,
        sameSite: "Lax",
        expires: Math.floor(Date.now() / 1000) + 3600,
      }]);
    }
  };
  await freshContext();
  const results = [];
  try {
    for (const route of routes) {
      const url = `${baseUrl}${route.path}`;
      const row = { route: route.name, viewport: viewport.label, url, overlays: 0, judged: 0, findings: [], skipped: [] };
      let page = null;
      try {
        page = await context.newPage().catch(async (error) => {
          if (!/closed/i.test(String(error?.message ?? error))) throw error;
          row.recovered = "the browser had to be restarted before this page";
          await freshContext();
          return context.newPage();
        });
        await page.goto(url, { waitUntil: "networkidle", timeout: 60_000 })
          .catch(() => page.goto(url, { waitUntil: "domcontentloaded", timeout: 30_000 }));
        await page.waitForTimeout(2_000);
        const landedOn = new URL(page.url()).pathname;
        row.landedOn = landedOn;
        const snapshot = await page.evaluate(collectSubstance).catch(() => null);
        const verdict = judgeLandedPage({ landedOn, snapshot });
        row.substance = verdict.substance;
        if (!verdict.film) {
          row.parked = verdict.parked;
          if (verdict.blankPage) row.blankPage = true;
          results.push(row);
          continue;
        }
        const candidates = await overlayCandidates(page, { routeName: route.name, viewportLabel: viewport.label });
        row.candidates = candidates.length;
        for (const candidate of candidates) {
          const name = `${viewport.label}-${route.name}-${candidate.id}`;
          const open = async () => {
            const target = candidate.locator ?? page.locator(candidate.trigger).first();
            if (!(await target.count().catch(() => 0))) return false;
            await target.scrollIntoViewIfNeeded().catch(() => {});
            await target.click({ timeout: 4_000 }).catch(() => {});
            await page.waitForTimeout(800);
            if (candidate.overlay) {
              return page.locator(candidate.overlay).first().isVisible({ timeout: 3_000 }).catch(() => false);
            }
            return true;
          };
          let filmed;
          try {
            filmed = await filmOverlay(page, { open, overlaySelector: candidate.overlay, outDir, name });
          } catch (error) {
            row.skipped.push({ id: candidate.id, why: String(error?.message ?? error).split("\n")[0].slice(0, 160) });
            continue;
          }
          if (!filmed.opened || !(filmed.overlays ?? []).length) {
            row.skipped.push({ id: candidate.id, why: filmed.reason || "nothing solid opened" });
            continue;
          }
          row.overlays += filmed.overlays.length;
          row.judged += filmed.overlays.length - (filmed.unjudged?.length ?? 0);
          for (const skipped of filmed.unjudged ?? []) row.skipped.push({ id: candidate.id, why: skipped.why });
          for (const finding of filmed.findings) row.findings.push({ ...finding, overlayId: candidate.id });
          // Put the page back before the next candidate.
          if (candidate.close) await page.locator(candidate.close).first().click({ timeout: 2_000 }).catch(() => {});
          else await page.keyboard.press("Escape").catch(() => {});
          await page.waitForTimeout(400);
        }
      } catch (error) {
        row.error = String(error?.message ?? error).split("\n")[0].slice(0, 200);
        // A dead browser is not a verdict about this page, and it must not end the run.
        if (/closed|crash|Target/i.test(row.error)) {
          row.recovered = "the browser was lost on this page and restarted for the next one";
          await freshContext().catch(() => {});
        }
      } finally {
        await page?.close().catch(() => {});
      }
      results.push(row);
      if (onRoute) onRoute(row);
    }
    await context.close();
  } finally {
    await browser.close();
  }
  return results;
}

/** The two viewports lane 1 sweeps. Desktop is not optional: Ravi named both. */
export const SWEEP_VIEWPORTS = Object.freeze([
  { label: "laptop", width: 1440, height: 1000, deviceScaleFactor: 1 },
  {
    label: "mobile", width: 390, height: 844, isMobile: true, deviceScaleFactor: 2,
    userAgent: "Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UQ1A.240205.004; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/128.0.6613.127 Mobile Safari/537.36",
  },
]);
