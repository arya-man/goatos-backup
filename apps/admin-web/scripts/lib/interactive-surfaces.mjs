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
export function gradeAssertion(assertion) {
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
  if (blankScreenValue === undefined) {
    reasons.push("does not declare what it reads on a blank screen, so nobody can tell whether it can fail");
    return { ok: false, reasons };
  }
  let passesOnGood = false;
  let passesOnBlank = true;
  try {
    passesOnGood = evaluate(expected, expected);
    passesOnBlank = evaluate(blankScreenValue, expected);
  } catch (error) {
    reasons.push(`operator threw on its own values: ${error.message}`);
    return { ok: false, reasons };
  }
  if (!passesOnGood) reasons.push("does not hold for the value it says it expects");
  if (passesOnBlank) {
    reasons.push(
      `goes green on a blank screen: ${JSON.stringify(blankScreenValue)} still satisfies ${operator} ${JSON.stringify(expected)}`,
    );
  }
  return { ok: reasons.length === 0, reasons };
}

// ---------------------------------------------------------------------------------------------
// 3. Ledger validation + coverage
// ---------------------------------------------------------------------------------------------

/**
 * @param {ReturnType<typeof scanInteractiveSurfaces>} surfaces
 * @param {{entries: Array<object>}} ledger
 */
export function validateLedger(surfaces, ledger) {
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
        const graded = gradeAssertion(assertion);
        if (!graded.ok) {
          good = false;
          for (const reason of graded.reasons) problems.push(`${entry.key}: assertion ${reason}`);
        }
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
