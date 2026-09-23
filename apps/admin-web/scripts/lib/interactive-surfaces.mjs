// Inventory + coverage gate for the admin-web's INTERACTIVE surfaces:
// edit forms, inline editors, row actions, and modals (dialog / drawer / sheet / popover) as a class.
//
// Why this exists (docs/engineering/automation-handover.md §3, §5):
//   * These four classes had ZERO coverage and nobody assigned.
//   * The old ledger called an entry "covered" when the sweep merely VISITED the page, and the
//     assertion engine's four operators (visible / absent / count / url) had no way to state an
//     expected VALUE -- so 178 assertions went green against a blank screen.
//
// So this module does two separate jobs, and neither one trusts the other:
//
//   1. INVENTORY (`scanInteractiveSurfaces`): find every surface in the source tree by marker, and
//      key it stably. Nothing in the tree may be silently absent from the ledger.
//   2. DISCRIMINATION GATE (`gradeAssertion`): an assertion only counts as coverage when it names a
//      concrete expected value AND declares what its subject reads on a blank / degraded screen,
//      and the operator, evaluated for real here, is TRUE for the expected value and FALSE for the
//      blank one. An assertion that cannot fail is not coverage.
//
// Everything here is pure: file text in, findings out. No browser, no network, no DB.

// ---------------------------------------------------------------------------------------------
// 1. Inventory
// ---------------------------------------------------------------------------------------------

/**
 * The marker list IS the contract. Adding a marker widens the inventory; the manifest then has to
 * make a decision about every newly found surface, so widening can never quietly lower coverage.
 * Each marker is anchored on the syntax the app really ships (verified by grep over features/,
 * components/ and app/ on 2026-09-23), never on a guessed convention.
 */
export const SURFACE_MARKERS = [
  { kind: "edit-form", pattern: /<form\b[^>]*>/g, note: "a real <form> element the reader can submit" },
  { kind: "modal", pattern: /role="(?:dialog|alertdialog)"/g, note: "an ARIA dialog: modal, drawer, sheet or lightbox" },
  { kind: "modal", pattern: /<LocalOverlayDrawer\b/g, note: "the shared URL-driven drawer" },
  { kind: "inline-editor", pattern: /contentEditable/g, note: "edit-in-place text" },
  { kind: "inline-editor", pattern: /\binline-cell\b/g, note: "the inline cell editor" },
  { kind: "inline-editor", pattern: /\btagedit\b/g, note: "the tag value editor popover" },
  { kind: "row-action", pattern: /\browacts\b/g, note: "the per-row action cluster" },
  { kind: "row-action", pattern: /data-row-action/g, note: "a control marked as a row action" },
  { kind: "row-action", pattern: /role="menu"/g, note: "a row / kebab menu" },
];

export const SURFACE_KINDS = [...new Set(SURFACE_MARKERS.map((m) => m.kind))].sort();

/** The literal a human would recognise this surface by, taken out of the marker's own tag text. */
function anchorFor(text, index) {
  const tail = text.slice(index, index + 400);
  const className = tail.match(/className=(?:"([^"]{1,60})"|\{"([^"]{1,60})"\})/);
  if (className) return (className[1] ?? className[2]).trim().split(/\s+/).join(" ");
  const aria = tail.match(/aria-label=(?:"([^"]{1,60})"|\{"([^"]{1,60})"\})/);
  if (aria) return (aria[1] ?? aria[2]).trim();
  const action = tail.match(/\baction=\{([A-Za-z0-9_$.]{1,60})\}/);
  if (action) return action[1];
  return "";
}

/**
 * Comments and import lines are not surfaces. `mention-textarea.tsx` documents itself with the
 * words `<form action={serverAction}>` inside a block comment, and `counts-breakdown-table.tsx`
 * imports `./inline-cell-editor` -- both were counted as real controls until this ran. Blanking
 * them (rather than deleting them) keeps every line number honest.
 */
export function blankNonMarkup(text) {
  return String(text)
    .replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "))
    .replace(/(^|[^:"'`\\])\/\/[^\n]*/g, (m, lead) => lead + " ".repeat(m.length - lead.length))
    .replace(/^[ \t]*(?:import|export)\s[^\n;]*(?:;|from\s+["'][^"'\n]+["'];?)/gm, (m) => m.replace(/[^\n]/g, " "));
}

/**
 * @param {Array<{path: string, text: string}>} files source files, paths relative to apps/admin-web
 * @returns {Array<{key: string, kind: string, path: string, line: number, anchor: string, marker: string}>}
 */
export function scanInteractiveSurfaces(files) {
  const found = [];
  const usedKeys = new Map();
  for (const file of [...files].sort((a, b) => a.path.localeCompare(b.path))) {
    // A file that was LISTED but whose contents never arrived must not quietly contribute nothing.
    // That is the census defect: a guard reporting "539 files scanned" with the tree deleted,
    // because it counted its candidate list. A count has to be of what was read.
    if (typeof file.text !== "string") {
      throw new Error(`${file.path} was listed but its contents were never read; a count of listed files is not a count of scanned files`);
    }
    if (!/\.(tsx|jsx)$/.test(file.path)) continue;
    if (/\.(test|spec)\.[^/]+$/.test(file.path)) continue;
    const scannable = blankNonMarkup(file.text);
    for (const marker of SURFACE_MARKERS) {
      const re = new RegExp(marker.pattern.source, "g");
      let match;
      while ((match = re.exec(scannable)) !== null) {
        const anchor = anchorFor(file.text, match.index) || "unnamed";
        const base = `${file.path}::${marker.kind}::${anchor}`;
        const seen = (usedKeys.get(base) ?? 0) + 1;
        usedKeys.set(base, seen);
        found.push({
          key: seen === 1 ? base : `${base}#${seen}`,
          kind: marker.kind,
          path: file.path,
          line: scannable.slice(0, match.index).split("\n").length,
          anchor,
          marker: marker.note,
        });
      }
    }
  }
  return found.sort((a, b) => a.key.localeCompare(b.key));
}

// ---------------------------------------------------------------------------------------------
// 1b. Deriving a value from the source -- the ONLY way an expected value may be born
// ---------------------------------------------------------------------------------------------
//
// The gate must be able to RE-DERIVE every expected value from the artefact it names. An author
// who simply asserts "expected: 7" is stating a number nobody measured, which is the coverage
// illusion moved one level up.

const NOISE = /^(?:[-–—|/,.:;]+|\d+|true|false|null|undefined)$/i;

/** Control labels the surface's own source declares, in the window of text the surface owns. */
export function labelsNear(text, index, limit = 4000) {
  // Comments describe a surface; they are not on screen. Harvesting a label out of one would
  // state an expected value the product never renders.
  const window = blankNonMarkup(text).slice(index, index + limit);
  const labels = new Set();
  for (const re of [
    /aria-label=(?:"([^"{}]{2,48})"|\{"([^"{}]{2,48})"\})/g,
    /placeholder=(?:"([^"{}]{2,48})"|\{"([^"{}]{2,48})"\})/g,
    /<label[^>]*>\s*([A-Z][^<>{}\n]{1,46})\s*</g,
    /<(?:button|h1|h2|h3|h4|summary|th|option|legend)[^>]*>\s*([A-Z][^<>{}\n]{1,46})\s*</g,
    /title=(?:"([^"{}]{2,48})"|\{"([^"{}]{2,48})"\})/g,
    // Copy that arrives from the page contract at runtime. The KEY is the offline-derivable
    // reference: the surface must render resolved copy for exactly these keys -- a blank panel
    // renders none of them, and missing copy renders the raw key instead of a sentence.
    // Any copy helper, matched by its ARGUMENT rather than its name: t(), c(), fc(), fdc()...
    // A DOTTED key is a copy key -- a class name or a selector never looks like this -- so this
    // reads every helper the pages use without having to enumerate their names.
    /\b[A-Za-z_$][A-Za-z0-9_$]{0,12}\(\s*"([A-Za-z0-9_-]{2,30}(?:\.[A-Za-z0-9_-]{1,30}){1,5})"/g,
    // copy(pageContract, "key") and copy(pageContract, "key", "fallback").
    /\bcopy\(\s*[A-Za-z0-9_$.]+\s*,\s*"([A-Za-z0-9_.-]{2,60})"/g,
    // Copy handed in as a prop object (features/configuration/row-actions.tsx renders every one of
    // its controls as labels.<slot>). The SLOT is the reference: a blank menu renders none of them.
    /\b(?:labels|copy|strings|text|[A-Za-z][A-Za-z0-9]*Copy|[A-Za-z][A-Za-z0-9]*Labels)\.([A-Za-z][A-Za-z0-9_]{1,29})\b/g,
    // The one-letter copy helper the configuration pages use: c("action.close").
    /\bc\(\s*"([A-Za-z0-9_.-]{2,60})"\s*\)/g,
    /\b(?:ariaLabel|closeLabel|title|label|heading|placeholder)=\{?"([^"{}]{2,48})"\}?/g,
    // What an edit form actually submits. A form that renders with its fields missing -- the exact
    // shape a degraded payload produces -- no longer carries this set.
    /\bname="([A-Za-z][A-Za-z0-9_.-]{1,39})"/g,
    /data-testid="([A-Za-z0-9_-]{2,48})"/g,
  ]) {
    for (const match of window.matchAll(re)) {
      const label = (match[1] ?? match[2] ?? "").replace(/\s+/g, " ").trim();
      if (label && !NOISE.test(label)) labels.add(label);
    }
  }
  return [...labels].sort();
}


/**
 * Labels on the surface's OWN opening tag. Unconditional by construction: if the panel is
 * mounted, its own label is on it -- unlike a child control, which may sit behind a permission
 * or capability gate and legitimately differ per principal (judge B-1).
 *
 * Wire-level names -- `name="row_version"`, `data-testid` -- are deliberately NOT read here.
 * Nobody meets them on a screen, and the contract says a finding names what a person sees.
 */
export function rootLabels(text, index) {
  const window = blankNonMarkup(text).slice(index, index + 500);
  const closing = window.indexOf(">");
  const tag = window.slice(0, closing >= 0 ? closing + 1 : 500);
  const found = new Set();
  for (const re of [
    /aria-label=\{?"([^"{}]{2,60})"\}?/g,
    /title=\{?"([^"{}]{2,60})"\}?/g,
    /\bariaLabel=\{?"([^"{}]{2,60})"\}?/g,
    // A copy KEY on the surface's own label. The key is the offline-derivable reference for a
    // word the page contract supplies at run time; a contract that does not resolve it renders
    // the raw key, which is exactly the failure this catches.
    /\b[A-Za-z_$][A-Za-z0-9_$]{0,12}\(\s*"([A-Za-z0-9_-]{2,30}(?:\.[A-Za-z0-9_-]{1,30}){1,5})"/g,
    /\bcopy\(\s*[A-Za-z0-9_$.]+\s*,\s*"([A-Za-z0-9_.-]{2,60})"/g,
  ]) {
    for (const match of tag.matchAll(re)) {
      const label = (match[1] ?? "").replace(/\s+/g, " ").trim();
      if (label && !/^\d+$/.test(label)) found.add(label);
    }
  }
  return [...found].sort();
}

/**
 * How to tell this surface is actually ON SCREEN, taken from its own opening tag.
 *
 * Without this, an assertion cannot tell "the panel opened and its heading is missing" (a bug)
 * from "the panel never opened" (not a fact about headings at all), and reports the second as the
 * first. Same shape as the feature-assertion engine's dataProbe, deliberately: a check that
 * cannot tell answers "cannot tell", which is never a pass and never an accusation.
 */
export function presenceProbe(text, index) {
  const window = blankNonMarkup(text).slice(index, index + 500);
  const closing = window.indexOf(">");
  const tag = window.slice(0, closing >= 0 ? closing + 1 : 500);
  const role = tag.match(/role="(dialog|alertdialog|menu)"/);
  if (role) return { css: `[role=${role[1]}]`, min: 1 };
  // The shared URL-driven drawer renders `aside.drawer.on` when it is open
  // (components/local-overlay-drawer.tsx:59). The component NAME is not in the DOM, so without
  // this the probe would look for an element that can never be found and every reading would
  // answer "not checked" -- quiet, but quietly useless.
  if (/^<LocalOverlayDrawer\b/.test(window)) return { css: "aside.drawer.on", min: 1 };
  const className = tag.match(/className=(?:"([^"{}]{1,60})"|\{"([^"{}]{1,60})"\})/);
  const first = (className?.[1] ?? className?.[2] ?? "").trim().split(/\s+/)[0];
  if (first) return { css: `.${first}`, min: 1 };
  // A bare intrinsic element (form, div) only counts when it is really an HTML tag -- a React
  // component name is not a selector, and guessing one would look for an element nothing renders.
  const element = window.match(/^<(form|aside|dialog|section|nav|table)\b/);
  return element ? { css: element[1].toLowerCase(), min: 1 } : null;
}

/** Count the times `pattern` appears in the window a surface owns. */
export function countNear(text, index, pattern, limit = 4000) {
  const window = blankNonMarkup(text).slice(index, index + limit);
  return [...window.matchAll(new RegExp(pattern, "g"))].length;
}

/** The offset of a 1-based line, measured on the same blanked text the scan used. */
export function offsetOfLine(text, line) {
  return blankNonMarkup(text).split("\n").slice(0, Math.max(0, line - 1)).join("\n").length;
}

/**
 * Re-derive an assertion's expected value from the source it names.
 * @returns {{value: unknown} | {error: string}}
 */
export function deriveExpected(provenance, readFile) {
  if (!provenance || typeof provenance !== "object") return { error: "carries no provenance for its expected value" };
  const { kind, path, line, extractor, pattern } = provenance;
  if (kind !== "source") return { error: `provenance kind ${JSON.stringify(kind ?? null)} cannot be re-derived here` };
  const text = readFile(path);
  if (typeof text !== "string") return { error: `names ${JSON.stringify(path)}, which is not a file in the app` };
  const offset = offsetOfLine(text, Number(line));
  if (extractor === "root-label") return { value: rootLabels(text, offset) };
  if (extractor === "labels-near") return { value: labelsNear(text, offset) };
  if (extractor === "count-matches") {
    if (!pattern) return { error: "uses count-matches without saying what to count" };
    return { value: countNear(text, offset, pattern) };
  }
  return { error: `extractor ${JSON.stringify(extractor ?? null)} is not one this gate can run` };
}

/**
 * What the subject reads when nothing painted. DERIVED from the operator, never taken from the
 * author -- otherwise an author could declare a blank reading that conveniently differs from
 * their expected value and the "can it fail?" question answers itself.
 */
export function blankValueFor(operator) {
  switch (operator) {
    case "field-set-equals":
    case "field-set-contains-all":
      return [];
    case "count-equals":
    case "number-equals":
      return 0;
    case "enabled-equals":
      return false;
    default:
      return "";
  }
}

// ---------------------------------------------------------------------------------------------
// 2. The discrimination gate
// ---------------------------------------------------------------------------------------------

/**
 * Operators that can state an expected VALUE. `visible` / `absent` / bare `count` / `url` are
 * deliberately NOT here: those are the four that let 178 assertions pass against a blank screen.
 */
export const VALUE_OPERATORS = {
  "text-equals": (actual, expected) => String(actual).trim() === String(expected).trim(),
  "text-matches": (actual, expected) => new RegExp(expected).test(String(actual)),
  "value-equals": (actual, expected) => String(actual) === String(expected),
  "number-equals": (actual, expected) => Number(actual) === Number(expected),
  "count-equals": (actual, expected) => Number(actual) === Number(expected),
  "attribute-equals": (actual, expected) => String(actual) === String(expected),
  "enabled-equals": (actual, expected) => Boolean(actual) === Boolean(expected),
  // Superset, not equality (judge B-1). Admin-web pages are role-agnostic: the controls a person
  // meets are compiled from a backend contract against THEIR permissions, so an EXACT set pinned
  // from component source lists every control the component COULD render and fails on a correct
  // page for a narrower principal. What a surface owes every principal who can open it is that
  // its own labels are there.
  "field-set-contains-all": (actual, expected) => {
    const have = new Set((Array.isArray(actual) ? actual : [actual]).map(String));
    const want = Array.isArray(expected) ? expected : [expected];
    return want.length > 0 && want.every((item) => have.has(String(item)));
  },
  "field-set-equals": (actual, expected) => {
    const norm = (v) => (Array.isArray(v) ? [...v].map(String).sort() : [String(v)]);
    const a = norm(actual);
    const b = norm(expected);
    return a.length === b.length && a.every((item, i) => item === b[i]);
  },
};

export const REQUIRED_VIEWPORTS = ["1440", "390"];

/**
 * Grade ONE assertion. Returns { ok, reasons[] }.
 *
 * An assertion counts only when all of this holds:
 *   - its operator can state a value;
 *   - it names a concrete `expected`;
 *   - it declares `blankScreenValue`: what its subject reads when the screen renders blank or the
 *     payload is empty -- the exact state the old engine reported green against;
 *   - evaluating the operator here gives TRUE for `expected` and FALSE for `blankScreenValue`.
 *
 * The last line is the whole point: a check that cannot go red is not coverage.
 */
export function gradeAssertion(assertion, readFile = null, context = {}) {
  const reasons = [];
  const { operator, expected, blankScreenValue, subject } = assertion ?? {};
  if (!subject || !String(subject).trim()) reasons.push("names no subject a person could read");
  const evaluate = VALUE_OPERATORS[operator];
  if (!evaluate) {
    reasons.push(
      `operator ${JSON.stringify(operator ?? null)} cannot state an expected value` +
        ` (use one of: ${Object.keys(VALUE_OPERATORS).join(", ")})`,
    );
    return { ok: false, reasons };
  }
  if (expected === undefined || expected === null || expected === "") {
    reasons.push("states no expected value");
    return { ok: false, reasons };
  }
  // The blank reading is DERIVED from the operator, never accepted from the author (judge B1):
  // an author who may choose both sides of the comparison can always make it look discriminating.
  const derivedBlank = blankValueFor(operator);
  if (blankScreenValue !== undefined && JSON.stringify(blankScreenValue) !== JSON.stringify(derivedBlank)) {
    reasons.push(
      `declares a blank-screen reading of ${JSON.stringify(blankScreenValue)}, but a ${operator} subject ` +
        `reads ${JSON.stringify(derivedBlank)} when nothing painted; the author does not get to pick this side`,
    );
    return { ok: false, reasons };
  }

  // The expected value must be RE-DERIVABLE from the artefact it names. Without this the gate
  // grades an author's declaration rather than a measurement -- the coverage illusion one level up.
  if (readFile) {
    const derived =
      assertion.provenance?.kind === "measured"
        ? deriveMeasured(assertion.provenance, context.readReceipt, context.principal)
        : deriveExpected(assertion.provenance, readFile);
    if (derived.error) {
      reasons.push(derived.error);
      return { ok: false, reasons };
    }
    if (JSON.stringify(derived.value) !== JSON.stringify(expected)) {
      reasons.push(
        `states an expected value the source does not produce: its own provenance re-derives ` +
          `${JSON.stringify(derived.value)}, not ${JSON.stringify(expected)}`,
      );
      return { ok: false, reasons };
    }
  }
  let passesOnGood = false;
  let passesOnBlank = true;
  try {
    passesOnGood = evaluate(expected, expected);
    passesOnBlank = evaluate(derivedBlank, expected);
  } catch (error) {
    reasons.push(`operator threw on its own values: ${error.message}`);
    return { ok: false, reasons };
  }
  if (!passesOnGood) reasons.push("does not hold for the value it says it expects");
  if (passesOnBlank) {
    reasons.push(
      `goes green on a blank screen: ${JSON.stringify(derivedBlank)} still satisfies ${operator} ${JSON.stringify(expected)}`,
    );
  }
  return { ok: reasons.length === 0, reasons };
}

/**
 * The verdict for ONE reading at run time. The only place that decides it, so the executing side
 * cannot answer this question differently.
 *
 * @param {{operator:string, expected:unknown}} assertion
 * @param {unknown} reading what was read, or null when nothing was read
 * @param {boolean|null} present did the presence probe find the surface? null = probe not run
 */
export function gradeReading(assertion, reading, present) {
  if (present !== true) {
    return {
      verdict: "not-checked",
      why:
        present === null
          ? "nothing looked for this surface on the screen, so there is no way to tell whether it was there"
          : "this surface was not on the screen, so there was nothing to judge — that is not the same as its heading being wrong",
    };
  }
  if (reading === null || reading === undefined) {
    return { verdict: "not-checked", why: "the surface was on the screen but nothing could be read from it" };
  }
  const evaluate = VALUE_OPERATORS[assertion.operator];
  if (!evaluate) return { verdict: "not-checked", why: `no operator named ${JSON.stringify(assertion.operator)}` };
  return evaluate(reading, assertion.expected)
    ? { verdict: "pass", why: "" }
    : { verdict: "fail", why: `${JSON.stringify(reading)} does not satisfy ${assertion.operator} ${JSON.stringify(assertion.expected)}` };
}

// ---------------------------------------------------------------------------------------------
// 3. Ledger validation + coverage
// ---------------------------------------------------------------------------------------------

/**
 * @param {ReturnType<typeof scanInteractiveSurfaces>} surfaces
 * @param {{entries: Array<object>}} ledger
 */
export function validateLedger(surfaces, ledger, readFile = null, readReceipt = null) {
  const problems = [];
  const entries = Array.isArray(ledger?.entries) ? ledger.entries : null;
  if (!entries) return { problems: ["the interactive-surface ledger has no entries array"], coverage: null };

  const byKey = new Map();
  for (const entry of entries) {
    if (!entry?.key) {
      problems.push("a ledger entry has no key");
      continue;
    }
    if (byKey.has(entry.key)) problems.push(`the ledger lists ${entry.key} twice`);
    byKey.set(entry.key, entry);
  }

  const scanned = new Map(surfaces.map((s) => [s.key, s]));
  for (const surface of surfaces) {
    if (!byKey.has(surface.key)) {
      problems.push(
        `${surface.kind} at ${surface.path}:${surface.line} ("${surface.anchor}") is on screen in the app ` +
          `but the ledger makes no decision about it`,
      );
    }
  }
  for (const entry of byKey.values()) {
    if (!scanned.has(entry.key)) {
      problems.push(`the ledger still claims ${entry.key}, which no longer exists in the source`);
    }
  }

  const covered = [];
  const stated = [];
  for (const entry of byKey.values()) {
    if (!scanned.has(entry.key)) continue;

    // An example is never the scope: a modal defined once is a modal on every route that renders
    // it, so the ledger names those routes. Zero routes with no reason means either the import
    // graph lost the surface or nothing can reach it -- both are findings, not silence.
    const routes = Array.isArray(entry.routes) ? entry.routes : [];
    if (routes.length === 0 && !String(entry.routeGapReason ?? "").trim()) {
      problems.push(`${entry.key} names no route that renders it, and no reason why not`);
    }

    // "covered" and "stated-not-executed" share the SAME assertion bar. The only difference is
    // whether the assertion has ever been run against the product. Handover §2: a check that did
    // not run must not render a verdict -- so a stated assertion counts ZERO toward coverage until
    // a run receipt names it. That is the whole reason the old ledger read 87.2%.
    if (entry.status === "covered" || entry.status === "stated-not-executed") {
      const assertions = Array.isArray(entry.assertions) ? entry.assertions : [];
      if (assertions.length === 0) {
        problems.push(`${entry.key} is claimed ${entry.status} with no assertion at all`);
        continue;
      }
      let good = true;
      for (const assertion of assertions) {
        const graded = gradeAssertion(assertion, readFile, { readReceipt, principal: entry.principal });
        if (!graded.ok) {
          good = false;
          for (const reason of graded.reasons) problems.push(`${entry.key}: assertion ${reason}`);
        }
      }
      // An assertion with no way to tell whether the surface was on screen reports "the heading
      // is missing" for a panel that simply never opened -- a correct page, accused. A check that
      // fires on a correct page is worse than no check.
      for (const assertion of assertions) {
        if (!assertion.presence?.css) {
          good = false;
          problems.push(
            `${entry.key}: assertion gives no way to tell whether this surface was on the screen, so a ` +
              `panel that never opened would be reported as a panel whose heading is wrong`,
          );
          break;
        }
      }
      // Judge B-1: an expectation with no principal is an expectation about nobody. Admin-web
      // compiles a different contract per permission set, so a set comparison run for the wrong
      // principal fails in BOTH directions on a correct page.
      if (!String(entry.principal ?? "").trim()) {
        good = false;
        problems.push(
          `${entry.key} states an expectation without saying WHOSE screen it describes; this product ` +
            `compiles a different set of controls per principal, so an expectation with no principal ` +
            `cannot be right or wrong`,
        );
      }
      const viewports = (entry.viewports ?? []).map(String);
      const missing = REQUIRED_VIEWPORTS.filter((v) => !viewports.includes(v));
      if (missing.length && !entry.viewportGapReason) {
        good = false;
        problems.push(`${entry.key} claims ${entry.status} but never runs at ${missing.join(" and ")}px, with no reason given`);
      }
      if (entry.status === "covered") {
        const receipt = String(entry.receipt?.path ?? "").trim();
        if (!receipt || !String(entry.receipt?.runId ?? "").trim()) {
          good = false;
          problems.push(
            `${entry.key} is claimed covered but names no run receipt; without one it is "stated-not-executed", not covered`,
          );
        }
      } else if (!String(entry.notExecutedReason ?? "").trim()) {
        problems.push(`${entry.key} has a stated assertion that has never run and does not say why`);
      }
      if (good) (entry.status === "covered" ? covered : stated).push(entry.key);
      continue;
    }

    if (entry.status === "not-checked") {
      if (!String(entry.notCheckedReason ?? "").trim()) {
        problems.push(`${entry.key} is not checked and gives no reason -- silence is not a verdict`);
      }
      continue;
    }
    problems.push(`${entry.key} has status ${JSON.stringify(entry.status ?? null)}; use "covered", "stated-not-executed" or "not-checked"`);
  }

  return { problems, coverage: coverageOf(surfaces, covered, stated) };
}

/** Coverage as a fraction, whole tree and per kind. Denominator is always the SCAN, never the ledger. */
export function routePairs(surfaces, routesByPath) {
  let pairs = 0;
  for (const surface of surfaces) pairs += (routesByPath.get(surface.path) ?? []).length;
  return pairs;
}

export function coverageOf(surfaces, coveredKeys, statedKeys = []) {
  const coveredSet = new Set(coveredKeys);
  const statedSet = new Set(statedKeys);
  const perKind = {};
  for (const kind of SURFACE_KINDS) perKind[kind] = { total: 0, covered: 0, stated: 0 };
  for (const surface of surfaces) {
    const bucket = (perKind[surface.kind] ??= { total: 0, covered: 0, stated: 0 });
    bucket.total += 1;
    if (coveredSet.has(surface.key)) bucket.covered += 1;
    else if (statedSet.has(surface.key)) bucket.stated += 1;
  }
  const total = surfaces.length;
  const covered = surfaces.filter((s) => coveredSet.has(s.key)).length;
  const stated = surfaces.filter((s) => !coveredSet.has(s.key) && statedSet.has(s.key)).length;
  return { total, covered, stated, perKind };
}

export function coverageSentence(coverage) {
  const pct = coverage.total === 0 ? "0.0" : ((coverage.covered / coverage.total) * 100).toFixed(1);
  const parts = Object.entries(coverage.perKind)
    .map(([kind, c]) => `${kind} ${c.covered}/${c.total} proven, ${c.stated} stated`)
    .join("; ");
  return (
    `interactive surfaces PROVEN (assertion stated AND run against the product): ` +
    `${coverage.covered}/${coverage.total} (${pct}%). ` +
    `Discriminating assertion stated but never executed: ${coverage.stated}/${coverage.total}. ` +
    `By kind -- ${parts}`
  );
}

// ---------------------------------------------------------------------------------------------
// 4. Which ROUTES own a surface
// ---------------------------------------------------------------------------------------------
//
// "An example is never the scope" -- a modal defined once is a modal on every route that renders
// it, so the ledger has to say which routes those are. Resolved from the import graph rather than
// guessed: a page.tsx reaches a component, so that page owns every surface in it.

const EXTENSIONS = ["", ".tsx", ".ts", "/index.tsx", "/index.ts", ".jsx", ".js"];

/** `@/features/x` and `./x` -> a path in the file map, or "" when it leaves the app (node_modules). */
export function resolveImport(specifier, fromPath, byPath) {
  let base;
  if (specifier.startsWith("@/")) base = specifier.slice(2);
  else if (specifier.startsWith(".")) {
    const dir = fromPath.split("/").slice(0, -1);
    const parts = specifier.split("/");
    for (const part of parts) {
      if (part === ".") continue;
      else if (part === "..") dir.pop();
      else dir.push(part);
    }
    base = dir.join("/");
  } else return "";
  for (const ext of EXTENSIONS) if (byPath.has(base + ext)) return base + ext;
  return "";
}

/**
 * Next.js reaches these by CONVENTION, with no import anywhere: the router renders loading.tsx
 * while a page streams, error.tsx when it throws, not-found.tsx when it 404s, and layout.tsx
 * around everything beneath it. Leaving them out meant a real screen -- the empty and error
 * states this work is supposed to cover -- carried "no page imports this file", which reads like
 * dead code and is not.
 */
export const ROUTER_CONVENTION_FILES = ["loading", "error", "not-found", "template", "default", "global-error"];

/** The admin route a `app/(admin)/.../page.tsx` serves, in the shape the sweep's route list uses. */
export function routeOfPageFile(path) {
  const rel = path
    .replace(/^app\//, "")
    .replace(/\/(?:page|layout|loading|error|not-found|template|default|global-error)\.(t|j)sx?$/, "");
  const route = `/${rel}`
    .replaceAll(/\([^)]*\)\//g, "")
    // A route GROUP at the very end has no trailing slash to match on -- app/(admin)/layout.tsx is
    // the layout of "/", not a route called "/(admin)".
    .replace(/\/\([^)]*\)$/, "/")
    .replaceAll(/\[[^/]+\]/g, "placeholder")
    .replace(/(.)\/$/, "$1");
  return route === "" ? "/" : route;
}

/**
 * @returns Map<filePath, string[] routes> -- every route whose page transitively imports that file.
 */
export function routesOwningFiles(files) {
  const byPath = new Map(files.map((f) => [f.path, f.text]));
  const importsOf = new Map();
  for (const file of files) {
    const text = blankNonMarkup(file.text);
    const raw = file.text;
    const specs = new Set();
    // Imports were blanked for the surface scan; read them off the RAW text here, which is the
    // one place they are the subject rather than noise.
    for (const m of raw.matchAll(/(?:^|\n)\s*(?:import|export)[^\n;]*?from\s+["']([^"']+)["']/g)) specs.add(m[1]);
    for (const m of raw.matchAll(/\bimport\(\s*["']([^"']+)["']\s*\)/g)) specs.add(m[1]);
    void text;
    importsOf.set(
      file.path,
      [...specs].map((s) => resolveImport(s, file.path, byPath)).filter(Boolean),
    );
  }
  // A dynamic import built from a variable is an edge this graph cannot follow. There are none in
  // the app today, and if one appears it must show up as UNRESOLVED rather than quietly making a
  // reachable file look like dead code.
  const unresolvable = new Set();
  for (const file of files) {
    if (/\bimport\(\s*[^"'`)\s]/.test(file.text)) unresolvable.add(file.path);
  }

  const owners = new Map();
  const conventions = ROUTER_CONVENTION_FILES.join("|");
  const entryPoint = new RegExp(`^app/(?:.*/)?(?:page|layout|${conventions})\\.(t|j)sx?$`);
  for (const file of files) {
    if (!entryPoint.test(file.path)) continue;
    const route = routeOfPageFile(file.path);
    const seen = new Set();
    const stack = [file.path];
    while (stack.length) {
      const current = stack.pop();
      if (seen.has(current)) continue;
      seen.add(current);
      for (const next of importsOf.get(current) ?? []) stack.push(next);
    }
    for (const reached of seen) {
      if (!owners.has(reached)) owners.set(reached, new Set());
      owners.get(reached).add(route);
    }
  }
  const resolved = new Map([...owners].map(([path, routes]) => [path, [...routes].sort()]));
  resolved.unresolvableImportSites = [...unresolvable].sort();
  return resolved;
}

// ---------------------------------------------------------------------------------------------
// 5. Measured values — the seam with the feature-assertion engine
// ---------------------------------------------------------------------------------------------
//
// ONE notion of "a value that was actually measured", so neither side ships a second:
//
//   A measured value is one a RUN RECORDED while it had the page open. It is copied out of a run
//   receipt by the grader; it is never typed by an author. It carries the principal it was read
//   under and the page-contract revision that compiled for them, because on a role-agnostic page
//   the same surface legitimately reads differently for two people. And it is only promotable to
//   an expectation when at least two independent readings AGREE -- one reading cannot tell a
//   stable label from a timestamp, a row id, or whatever the page happened to render once.
//
// `kind: "source"` (above) stays what it is: a value re-derived from committed source, true of
// every principal. `kind: "measured"` is for everything source cannot answer -- which today is
// 124 of 148 surfaces, because they carry no label of their own.

import { compareReadings, contractRevisionRefusal } from "./reading-comparison.mjs";

export const RECEIPT_VERSION = 1;

/**
 * Readings that disagree cannot become an expectation; readings that agree can.
 *
 * The comparison itself is the SHARED primitive (lib/reading-comparison.mjs), agreed with the
 * feature-assertion engine: one comparison, two verdict vocabularies. This side passes
 * conditions: "same" -- nothing was done between the readings -- so a disagreement means the page
 * varies here rather than that something moved when it should not.
 */
export function stableReading(readings, { label = "this reading", all } = {}) {
  if (!Array.isArray(readings) || readings.length < 2) {
    return { stable: false, reason: `needs at least two independent readings, got ${readings?.length ?? 0}` };
  }
  // The absence refusal used to live here AND in A's caller, by two different private
  // conventions, while the primitive underneath still agreed that nothing equals nothing. It now
  // lives in compareReadings alone: a caller that has to remember is a caller that will forget.
  for (let i = 1; i < readings.length; i += 1) {
    const compared = compareReadings(readings[0], readings[i], { conditions: "same", label, all });
    if (!compared.agreed) return { stable: false, reason: compared.verdict };
  }
  return { stable: true, value: readings[0] };
}

/**
 * Look a measured value up in a receipt. Same contract as deriveExpected: the grader reads the
 * artefact, the author does not get to state the value.
 * @param {{kind:string, receipt:string, runId:string, observation:string}} provenance
 * @param {(path:string)=>unknown} readReceipt returns the parsed receipt for a path
 */
export function deriveMeasured(provenance, readReceipt, principal) {
  const { receipt: path, runId, observation } = provenance ?? {};
  const receipt = readReceipt?.(path);
  if (!receipt || typeof receipt !== "object") return { error: `names receipt ${JSON.stringify(path)}, which is not a receipt this gate can read` };
  if (receipt.version !== RECEIPT_VERSION) return { error: `receipt ${path} is version ${receipt.version}, not ${RECEIPT_VERSION}` };
  if (receipt.runId !== runId) return { error: `receipt ${path} holds run ${receipt.runId}, not ${runId}` };
  if (String(receipt.principal ?? "") !== String(principal ?? "")) {
    return {
      error:
        `was read as ${JSON.stringify(receipt.principal ?? null)} but the entry describes ` +
        `${JSON.stringify(principal ?? null)}; on a role-agnostic page those are different screens`,
    };
  }
  const found = (receipt.observations ?? []).find((o) => o.id === observation);
  if (!found) return { error: `receipt ${path} has no observation ${JSON.stringify(observation)}` };
  const revisionRefusal = contractRevisionRefusal(receipt.contractRevision ?? receipt.apiBuildSha);
  if (revisionRefusal) return { error: `receipt ${path} ${revisionRefusal}` };
  // A route that could not be reached is carried on the observation, so it reads as "not
  // checked" rather than as an observation nobody bothered to take.
  if (found.notReached) return { error: `observation ${observation} was not taken: ${found.notReached}` };
  const stable = stableReading(found.readings, { label: found.subject ?? "this reading" });
  if (!stable.stable) return { error: `observation ${observation} ${stable.reason}` };
  return { value: stable.value };
}
