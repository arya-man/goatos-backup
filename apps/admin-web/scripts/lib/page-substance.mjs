// Did this page actually draw anything, and may a check render a verdict on it?
//
// THE DEFECT THIS EXISTS FOR. Every surviving `covered` entry passed against a
// blank page. Not because any one detector is broken — because they are defect
// FINDERS. Colliding chart labels, overpainting cells, leaked raw text: a page
// with nothing on it has no defects to find, so every finder is silent and the
// sweep reads that silence as "clean". 178 of 276 assertions went green against
// a screen with nothing drawn on it, and a plan editor reported 17 of 17.
//
// Nothing in the sweep compensated: smoke-route-identity only checks the URL
// did not redirect. A page can return 200, render its shell, and be judged
// perfect.
//
// So this is the gate. It answers one question — did the page draw its content,
// or only its chrome — and a check that names page content may not render a
// verdict until the answer is yes.
//
// WHAT IT CANNOT DO, said here rather than discovered later: emptiness is not
// correctness. A page that draws every figure WRONG is substantial, and this
// gate passes it. Catching that needs an expected value, which is the
// `equals`/`compare` half of the engine, not this half. Of the seven blank-page
// scenarios this was built against, this gate catches five; the two that are
// "the numbers are wrong" are out of its reach BY CONSTRUCTION.
//
// Pure. It takes a snapshot and returns a verdict, so every case below is a
// test with no browser in it.

/**
 * Runs in the page. Kept separate from the judging so the judging can be tested
 * without one.
 *
 * Serialisable, self-contained (page.evaluate cannot see this module's scope).
 */
export function collectSubstance() {
  const main = document.querySelector("main") ?? document.body;
  const vis = (el) => {
    const s = getComputedStyle(el);
    if (s.display === "none" || s.visibility === "hidden" || Number(s.opacity) === 0) return false;
    const r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0;
  };
  const count = (sel) => [...main.querySelectorAll(sel)].filter(vis).length;
  const text = (main.innerText ?? "").trim();
  // An explicit empty state is the page SAYING it has nothing, which is a
  // different fact from the page drawing nothing at all.
  const emptyState = [...main.querySelectorAll('[data-empty], .empty, .empty-state, [role="status"]')]
    .filter(vis).map((el) => (el.innerText ?? "").trim()).filter(Boolean);
  return {
    rows: count("tbody tr"),
    cards: count("[data-card], .card, article"),
    cells: count("tbody td"),
    chartMarks: count("svg rect, svg path[d], svg circle, canvas"),
    controls: count("button, a[href], input, select"),
    headings: count("h1, h2, h3"),
    // Digits are what a farm screen is mostly made of; a screen with no figure
    // on it is a strong signal, but never the only one consulted.
    figures: (text.match(/\d/g) ?? []).length,
    textLength: text.length,
    emptyState,
  };
}

/** Content, as opposed to chrome. A heading and a nav prove nothing was drawn. */
export function contentUnits(s) {
  return (s.rows ?? 0) + (s.cards ?? 0) + (s.cells ?? 0) + (s.chartMarks ?? 0);
}

export const VERDICTS = Object.freeze({
  SUBSTANTIAL: "substantial",
  EMPTY_STATE: "empty-state",
  BLANK: "blank",
});

/**
 * @param {object} snapshot from collectSubstance()
 * @param {{minContentUnits?: number}} [options]
 * @returns {{verdict, why, units}}
 */
export function assessSubstance(snapshot, { minContentUnits = 3 } = {}) {
  const units = contentUnits(snapshot);
  if (units >= minContentUnits) {
    return { verdict: VERDICTS.SUBSTANTIAL, units, why: "" };
  }
  if ((snapshot.emptyState ?? []).length) {
    return {
      verdict: VERDICTS.EMPTY_STATE,
      units,
      why: `the page says it has nothing to show ("${snapshot.emptyState[0].slice(0, 60)}"), so a check about what it draws was never put to the test`,
    };
  }
  return {
    verdict: VERDICTS.BLANK,
    units,
    why: `the page loaded and drew ${units} piece(s) of content — no rows, no cards, no chart marks and no empty state — so nothing on it could be judged, and a check that found no fault here found nothing at all`,
  };
}

/**
 * May a check that names page content render a verdict here?
 *
 * Only on a substantial page. On an empty-state page the answer is
 * NOT-ATTEMPTED, never a pass. On a blank page it is a FINDING about the page
 * itself, because a screen that loaded and drew nothing is broken whatever the
 * detectors say.
 */
export function gateContentCheck(assessment) {
  if (assessment.verdict === VERDICTS.SUBSTANTIAL) return { judge: true };
  if (assessment.verdict === VERDICTS.EMPTY_STATE) return { judge: false, notAttempted: assessment.why };
  return { judge: false, finding: assessment.why };
}
