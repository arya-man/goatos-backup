#!/usr/bin/env node
// Guard: a page-level entry animation must not replay while a client-local overlay is open.
//
// WHY THIS EXISTS (regression, 2026-08-06, verifier /actions):
// The shell's page container carries an entry animation (`.screen { animation: fade .25s ease }`).
// A client-local overlay — the verifier's video-review modal — encodes its open state in a query
// param (`vi_row`). Every change to that param is an RSC re-render, which REPLAYS the page entry
// animation underneath the open modal. Opening the modal, and every Prev/Next step inside it, made
// the whole page flash. The maintainer's words: "when the model comes and goes feels like stuck not
// smooth". Nothing failed, no test broke, and no other guard could see it, because the bug is the
// INTERACTION between an entry animation and an overlay that lives in the URL.
//
// THE RULE: if a selector applies a non-`none` `animation` to a page-level container, the same
// stylesheet must also suppress that animation while an overlay scrim is open, e.g.
//   .screen:has(.vr-modal-scrim.on) { animation: none; }
// Suppression may be written with :has(), a body/html state class, or a data attribute — this guard
// only requires that SOME rule turns the animation off for the same container while an overlay is
// open.
//
// Usage:
//   node tools/agent-hooks/check-overlay-motion.mjs              real check
//   node tools/agent-hooks/check-overlay-motion.mjs --self-test  adversarial fixtures
import { readFileSync, existsSync } from "node:fs";
import { resolve, dirname } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

// Page-level containers: the elements an RSC re-render re-mounts under an overlay.
const PAGE_CONTAINERS = ["screen", "main", "layout", "wrap"];

function animationRules(css) {
  // Matches `.selector { ... animation: <value> ... }` blocks, capturing selector + declarations.
  const out = [];
  const re = /([^{}]+)\{([^{}]*)\}/g;
  let m;
  while ((m = re.exec(css)) !== null) {
    const selector = m[1].trim();
    const body = m[2];
    const anim = /(?:^|[;\s])animation(?:-name)?\s*:\s*([^;}]+)/i.exec(body);
    if (!anim) continue;
    out.push({ selector, value: anim[1].trim() });
  }
  return out;
}

function isPageContainer(selector) {
  return PAGE_CONTAINERS.some((name) => new RegExp(`\\.${name}\\b`).test(selector));
}

function suppressesUnderOverlay(selector) {
  // A suppression rule names the container AND an open-overlay condition.
  const overlayish = /(:has\([^)]*(scrim|modal|overlay|drawer)[^)]*\)|\[data-overlay|\.overlay-open|\.modal-open)/i;
  return overlayish.test(selector);
}

export function findingsForCss(css, rel) {
  const rules = animationRules(css);
  const animated = rules.filter(
    (rule) => isPageContainer(rule.selector) && !/^none\b/i.test(rule.value) && !suppressesUnderOverlay(rule.selector),
  );
  if (animated.length === 0) return [];

  const suppressed = rules.some(
    (rule) => isPageContainer(rule.selector) && suppressesUnderOverlay(rule.selector) && /^none\b/i.test(rule.value),
  );
  if (suppressed) return [];

  return animated.map(
    (rule) =>
      `${rel}: page container \`${rule.selector}\` animates (\`animation: ${rule.value}\`) with no rule disabling it while a local overlay is open — an overlay whose state lives in the URL replays this on every step and reads as jank. Add e.g. \`${rule.selector}:has(.<overlay>-scrim.on) { animation: none; }\``,
  );
}

const FIXTURES = [
  {
    name: "animated page container with no overlay suppression (the 2026-08-06 regression)",
    css: ".screen{display:none;animation:fade .25s ease}\n.screen.on{display:block}",
    expectFinding: true,
  },
  {
    name: "animated page container WITH overlay suppression",
    css: ".screen{animation:fade .25s ease}\n.screen:has(.vr-modal-scrim.on){animation:none}",
    expectFinding: false,
  },
  {
    name: "suppression written with a body state class",
    css: ".layout{animation:slide .2s}\n.layout.modal-open{animation:none}",
    expectFinding: false,
  },
  {
    name: "non-page element animating freely is none of this guard's business",
    css: ".spinner{animation:spin 1s linear infinite}",
    expectFinding: false,
  },
  {
    name: "page container with animation:none only — nothing to suppress",
    css: ".screen{animation:none}",
    expectFinding: false,
  },
];

function selfTest() {
  let failed = 0;
  for (const fixture of FIXTURES) {
    const got = findingsForCss(fixture.css, "fixture.css").length > 0;
    if (got !== fixture.expectFinding) {
      console.error(`  ✗ ${fixture.name}: expected finding=${fixture.expectFinding}, got ${got}`);
      failed += 1;
    }
  }
  if (failed > 0) {
    console.error(`overlay-motion guard self-test FAILED (${failed}/${FIXTURES.length})`);
    process.exit(1);
  }
  console.log(`overlay-motion guard: self-test passed (${FIXTURES.length} adversarial cases verified)`);
}

function realCheck() {
  const sheets = ["apps/admin-web/app/mesha-theme.css"];
  const findings = [];
  let scanned = 0;
  for (const rel of sheets) {
    const abs = resolve(repo, rel);
    if (!existsSync(abs)) continue;
    scanned += 1;
    findings.push(...findingsForCss(readFileSync(abs, "utf8"), rel));
  }
  if (findings.length > 0) {
    console.error("overlay-motion guard failed:");
    for (const finding of findings) console.error(`- ${finding}`);
    console.error(
      "\nAn overlay that stores its open state in the URL re-renders the page on every step. Suppress the page entry animation while it is open.",
    );
    process.exit(1);
  }
  console.log(`overlay-motion guard: ok (${scanned} stylesheet(s) scanned)`);
}

if (process.argv.includes("--self-test")) selfTest();
else realCheck();
