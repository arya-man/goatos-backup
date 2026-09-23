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
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { detectFlicker, grayFrameFromRgba } from "./flicker-detector.mjs";

export const CAPTURE_DEFAULTS = Object.freeze({
  // Long enough to catch a once-a-second recurrence several times over.
  seconds: 8,
  // The frames are downsampled for detection anyway; this keeps the screencast
  // cheap and keeps the compositor doing roughly what it does for a real phone.
  maxWidth: 390,
  maxHeight: 844,
  downsampleStep: 8,
});

function hasFfmpeg() {
  return spawnSync("ffmpeg", ["-version"], { stdio: "ignore" }).status === 0;
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
export function writeFlickerEvidence(frames, result, outDir, name) {
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
  if (!hasFfmpeg()) {
    return {
      gif: null,
      filmstrip: null,
      frames: written,
      note: "ffmpeg is not installed here, so the frames are attached one by one instead of as a GIF",
    };
  }

  const gif = join(outDir, `${name}-flicker.gif`);
  const filmstrip = join(outDir, `${name}-flicker-filmstrip.png`);
  const pattern = join(seqDir, "f%03d.png");
  try {
    // Slow enough that a person can see the wrong frame go past, looping forever.
    execFileSync(
      "ffmpeg",
      ["-v", "error", "-y", "-framerate", "6", "-i", pattern, "-vf", "scale=390:-1:flags=lanczos", "-loop", "0", gif],
      { stdio: "ignore" },
    );
  } catch {
    /* fall through: the frames are still on disk */
  }
  try {
    execFileSync(
      "ffmpeg",
      [
        "-v", "error", "-y", "-i", pattern,
        "-vf", `scale=260:-1,tile=${Math.min(written.length, 8)}x1:margin=6:padding=4:color=0x1b1f1d`,
        "-frames:v", "1", filmstrip,
      ],
      { stdio: "ignore" },
    );
  } catch {
    /* same */
  }
  return {
    gif: existsSync(gif) ? gif : null,
    filmstrip: existsSync(filmstrip) ? filmstrip : null,
    frames: written,
    note: "",
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
        await page.goto(url, { waitUntil: "domcontentloaded", timeout: 45_000 });
        await page.waitForTimeout(1_500);
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
          ? writeFlickerEvidence(frames, result, outDir, route.name)
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
            findings: showedThrough.map((finding) => ({
              label: finding.overlay.label,
              events: finding.events,
              settledShare: finding.settledShare,
              evidence: writeOverlayEvidence(paint.frames, finding.events[0], outDir, `${route.name}-${(finding.overlay.label || "panel").toLowerCase().replace(/[^a-z0-9]+/g, "-")}`),
            })),
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

/** Open the filter panel on a list page, if the page has one. */
export async function openFilterPanel(page) {
  const opener = page
    .locator('button:has-text("Filters"), [aria-label="Filters"], .lt-fmore, .lt-fsheet-open, .lt-fbar button')
    .first();
  if (!(await opener.count().catch(() => 0))) return false;
  await opener.click({ timeout: 3_000 }).catch(() => {});
  await page.waitForTimeout(700);
  return true;
}

/**
 * Tick filter options one at a time, pausing between, so each change is a separate
 * moment in the film. Only rows inside the open panel are touched, and only ones that
 * read as a filter option — never a button that could save, apply, clear or submit.
 */
export async function changeFilterOptions(page, mark, { times = 4 } = {}) {
  const options = page.locator(
    '.lt-fgroup.open input[type="checkbox"], .lt-fgroup.open [role="checkbox"], .lt-fsheet input[type="checkbox"], [class*="fgroup"].open label',
  );
  const available = await options.count().catch(() => 0);
  if (!available) return 0;
  let changed = 0;
  for (let i = 0; i < Math.min(times, available); i += 1) {
    // Skip the first row: on this page it is "All", which clears rather than filters.
    const option = options.nth(Math.min(i + 1, available - 1));
    const done = await mark(`change the filters`, async () => {
      await option.click({ timeout: 3_000 }).catch(() => {});
      await page.waitForTimeout(900);
    });
    void done;
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
  const { collectOpaqueOverlays, detectShowThrough, opaqueShare } = await import("./overlay-paint-checks.mjs");

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
      series[index].push({
        t: frame.t,
        share: opaqueShare(decoded.data, decoded.width, decoded.height, overlay.rect, overlay.rgb, { ...o, scale }),
      });
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
 * Before / during / after, from the capture, for an overlay that showed through.
 * The middle frame is the defect; the two either side are what it should look like.
 * A single still is never enough here — the whole point is that it came back.
 */
export function writeOverlayEvidence(frames, event, outDir, name) {
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
  if (!hasFfmpeg() || written.length < 2) {
    return { gif: null, filmstrip: null, frames: written, note: hasFfmpeg() ? "" : "ffmpeg is not installed here, so the frames are attached one by one instead of as a GIF" };
  }
  const gif = join(outDir, `${name}-overlay.gif`);
  const filmstrip = join(outDir, `${name}-overlay-filmstrip.png`);
  const pattern = join(seqDir, "f%03d.png");
  try {
    execFileSync("ffmpeg", ["-v", "error", "-y", "-framerate", "5", "-i", pattern, "-vf", "scale=390:-1:flags=lanczos", "-loop", "0", gif], { stdio: "ignore" });
  } catch { /* the frames are still on disk */ }
  try {
    execFileSync("ffmpeg", [
      "-v", "error", "-y", "-i", join(seqDir, "f%03d.png"),
      "-vf", `scale=260:-1,tile=${Math.min(written.length, 5)}x1:margin=6:padding=4:color=0x1b1f1d`,
      "-frames:v", "1", filmstrip,
    ], { stdio: "ignore" });
  } catch { /* same */ }
  return { gif: existsSync(gif) ? gif : null, filmstrip: existsSync(filmstrip) ? filmstrip : null, frames: written, note: "" };
}
