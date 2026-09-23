// Static compositing check: the cause half of the mobile-webview flicker lane.
//
// One CSS combination has cost this team a filmed, reproduced-on-a-real-phone bug:
//
//     position: sticky  +  backdrop-filter / filter
//
// A sticky element that blurs what is behind it forces the compositor to
// re-rasterise its blurred backdrop every time content scrolls underneath it.
// Desktop GPUs composite that once and it is never seen. Mobile GPUs tear on it,
// which is exactly why the Tasks page flickers on a phone and looks perfect on a
// laptop. `position: fixed` carries the same combination but is much less likely
// to be seen, because a fixed overlay normally sits over content that is not
// moving; it is reported separately rather than lumped in.
//
// This check reads the stylesheet. No browser, no network, no timing, fully
// deterministic — so it runs on every sweep and would have caught the bug on the
// day the CSS landed rather than the day someone filmed it.
//
// Deliberate limits, so nobody mistakes this for more than it is:
//   • it matches a declaration block, so a sticky PARENT with a blurred CHILD
//     (two different selectors) is not found. `.vplan .actionbar` +
//     `.vplan .ab-in` on main is exactly that shape and is NOT reported here.
//   • it does not evaluate the cascade across differing selectors. A rule that is
//     neutralised by a more specific selector elsewhere is still reported, because
//     the combination is still in the stylesheet waiting for the next element that
//     matches it on its own.
//   • it DOES follow the cascade for the same selector at phone width, because that
//     turned out to matter: on this checkout `.lt-page .lt-fbar` is declared
//     sticky-and-blurred and then made `position:static` below 640px. The
//     combination is real on a laptop and absent on a phone, and only `onPhone`
//     may be spoken about as something a phone will show.
import { readFileSync } from "node:fs";

/** The stylesheets lane 1's pages actually load. */
export const ADMIN_WEB_STYLESHEETS = Object.freeze([
  "apps/admin-web/app/mesha-theme.css",
  "apps/admin-web/app/globals.css",
]);

const POSITION = /(?:^|;)\s*position\s*:\s*([a-z-]+)/gi;
const FILTER = /(?:^|;)\s*(-webkit-)?(backdrop-filter|filter)\s*:\s*([^;}]+)/gi;

/** `none` and an empty value are not a filter; a var() we cannot resolve is treated as one. */
function isRealFilter(value) {
  const v = String(value).trim().toLowerCase();
  return v !== "" && v !== "none" && v !== "initial" && v !== "unset";
}

function stripComments(css) {
  // Replace comment bodies with spaces so every byte offset — and therefore every
  // reported line number — still points at the real line in the real file.
  return css.replace(/\/\*[\s\S]*?\*\//g, (match) => match.replace(/[^\n]/g, " "));
}

function lineOf(text, index) {
  let line = 1;
  for (let i = 0; i < index && i < text.length; i += 1) if (text[i] === "\n") line += 1;
  return line;
}

/**
 * Split a stylesheet into leaf rules: { selector, body, index }. At-rule preludes
 * (@media, @supports, @layer) are carried along as context rather than parsed, so
 * a rule inside a media query keeps its own identity.
 */
export function leafRules(css) {
  const text = stripComments(css);
  const rules = [];
  const stack = [];
  let start = 0;
  for (let i = 0; i < text.length; i += 1) {
    const ch = text[i];
    if (ch === "{") {
      stack.push({ prelude: text.slice(start, i).trim(), at: i });
      start = i + 1;
    } else if (ch === "}") {
      const open = stack.pop();
      if (!open) continue;
      const body = text.slice(start, i);
      // A leaf rule is one whose body contains no nested block.
      if (open.prelude && !open.prelude.startsWith("@") && !body.includes("{")) {
        rules.push({
          selector: open.prelude.replace(/\s+/g, " "),
          body,
          index: open.at,
          context: stack.map((s) => s.prelude.replace(/\s+/g, " ")).filter((s) => s.startsWith("@")),
        });
      }
      start = i + 1;
    }
  }
  return rules.map((rule) => ({ ...rule, line: lineOf(text, rule.index) }));
}

/** The phone viewport lane 1 sweeps at. A media query that matches here is live on a phone. */
export const PHONE_WIDTH = 390;

/** `@media(max-width:640px)` matches at 390; `@media(min-width:900px)` does not. */
function matchesPhone(context) {
  for (const at of context) {
    if (!at.startsWith("@media")) continue;
    for (const m of at.matchAll(/min-width\s*:\s*(\d+)px/g)) if (Number(m[1]) > PHONE_WIDTH) return false;
    for (const m of at.matchAll(/max-width\s*:\s*(\d+)px/g)) if (Number(m[1]) < PHONE_WIDTH) return false;
  }
  return true;
}

/**
 * Find every rule that pins an element and blurs behind it in the same declaration
 * block.
 *
 * @returns Array<{ file, line, selector, position, filterProperty, filterValue, context,
 *                  appliesOnPhone, neutralisedAtLine, risk: "sticky"|"fixed", what }>
 */
export function findCompositedPinnedElements(css, file = "") {
  const rules = leafRules(css);
  const found = [];
  for (const rule of rules) {
    POSITION.lastIndex = 0;
    let position = null;
    for (let m = POSITION.exec(rule.body); m; m = POSITION.exec(rule.body)) position = m[1].toLowerCase();
    if (position !== "sticky" && position !== "fixed") continue;

    FILTER.lastIndex = 0;
    let filterProperty = null;
    let filterValue = null;
    for (let m = FILTER.exec(rule.body); m; m = FILTER.exec(rule.body)) {
      // Last one wins, exactly as the cascade inside one block does, so a block
      // that declares a blur and then `backdrop-filter:none` is correctly clean.
      filterProperty = `${m[1] ?? ""}${m[2]}`.toLowerCase();
      filterValue = m[3].trim();
    }
    if (!filterProperty || !isRealFilter(filterValue)) continue;

    // Does a later rule for the SAME selector switch this off at phone width? This is
    // not pedantry: on this checkout the task filter bar is declared sticky-and-blurred
    // in the base rule and then made `position:static` below 640px, so the combination
    // is real on a laptop and gone on a phone. Saying "this flickers on a phone" about
    // a rule that does not apply on a phone would be a confident wrong answer, and the
    // people reading Slack cannot check it.
    const neutralised = rules.find((other) =>
      other.index > rule.index &&
      other.selector === rule.selector &&
      matchesPhone(other.context) &&
      (/(?:^|;)\s*position\s*:\s*(?!sticky|fixed)[a-z-]+/i.test(other.body) ||
        /(?:^|;)\s*(?:-webkit-)?(?:backdrop-)?filter\s*:\s*none/i.test(other.body)));

    found.push({
      file,
      line: rule.line,
      selector: rule.selector,
      position,
      filterProperty,
      filterValue,
      context: rule.context,
      appliesOnPhone: matchesPhone(rule.context) && !neutralised,
      neutralisedAtLine: neutralised ? neutralised.line : null,
      risk: position === "sticky" ? "sticky" : "fixed",
      what:
        position === "sticky"
          ? "stays pinned while the page scrolls under it and blurs what is behind it, so the phone's GPU has to redraw the blur on every frame"
          : "sits over the page and blurs what is behind it; this only tears if what is underneath keeps moving",
    });
  }
  return found;
}

/**
 * Run the check over a repo checkout.
 *
 * @param repoRoot absolute path to the checkout
 * @param files    stylesheet paths relative to the checkout
 */
export function checkCompositingHazards(repoRoot, files = ADMIN_WEB_STYLESHEETS) {
  const findings = [];
  const scanned = [];
  for (const relative of files) {
    let css;
    try {
      css = readFileSync(`${repoRoot}/${relative}`, "utf8");
    } catch {
      continue; // A stylesheet that moved is not this check's business to fail on.
    }
    scanned.push(relative);
    findings.push(...findCompositedPinnedElements(css, relative));
  }
  const scrolling = findings.filter((f) => f.risk === "sticky");
  return {
    scanned,
    findings,
    scrolling,
    // The subset that is still pinned-and-blurred at the width lane 1 sweeps phones
    // at. This is the list that may be spoken about as a phone problem.
    onPhone: scrolling.filter((f) => f.appliesOnPhone),
    overlays: findings.filter((f) => f.risk === "fixed"),
    // Only the sticky ones are a failure. The fixed overlays are reported so the
    // next person can see them, not so the sweep goes red over a dimmed backdrop.
    ok: scrolling.length === 0,
  };
}

/** The one-line summary the smoke run prints and the receipt carries. */
export function compositingSummary(result) {
  if (result.ok) return `no pinned-and-blurred elements in ${result.scanned.length} stylesheet(s)`;
  const names = result.scrolling.map((f) => f.selector).join(", ");
  const laptopOnly = result.scrolling.length - result.onPhone.length;
  const aside = laptopOnly
    ? `; ${laptopOnly} of them is switched off at phone width and only applies on a laptop`
    : "";
  return `${result.scrolling.length} element(s) stay pinned while the page scrolls under them and blur what is behind: ${names}${aside}`;
}
