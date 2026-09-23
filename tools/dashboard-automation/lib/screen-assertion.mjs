// What a person must see after a write journey, judged the same way wherever it is judged.
//
// Lane 4's screen assertions used to be evaluated inside the browser driver, and only two of
// their five clause kinds were read at all: `absent`, `inColumn` and `doubledWord` were declared
// on four journeys and silently never checked. A declared assertion nobody evaluates is the §3
// defect exactly - it reports green for a page that never changed. This module is the ONE
// evaluator, it reads every clause kind, and it refuses an assertion it does not understand
// rather than passing it.

/** A page as this module judges it: the words on it, and the words under each named column. */
export function pageFrom(text, columns = {}) {
  return { text: String(text ?? ""), columns: { ...columns } };
}

const KNOWN_CLAUSES = new Set(["description", "visible", "notVisible", "absent", "inColumn"]);

export function evaluateScreenAssertion(assertion, page) {
  const missing = [];
  if (!assertion || typeof assertion !== "object") {
    return { ok: false, missing: ["this journey declares nothing that must be on screen"] };
  }
  for (const clause of Object.keys(assertion)) {
    if (!KNOWN_CLAUSES.has(clause)) {
      // Fail closed. A clause nobody reads is worse than no clause: it reads as checked.
      return { ok: false, missing: [`this journey asks for something the screen check cannot judge: ${clause}`] };
    }
  }
  const text = String(page?.text ?? "");
  const columns = page?.columns ?? {};

  for (const want of assertion.visible ?? []) {
    if (!text.includes(String(want.text))) missing.push(`not on screen: ${want.text}`);
  }
  // `notVisible` and `absent` are the same promise written two ways; both are read.
  for (const want of [...(assertion.notVisible ?? []), ...(assertion.absent ?? [])]) {
    if (want?.doubledWord) {
      const doubled = findDoubledWord(text);
      if (doubled) missing.push(`a word is printed twice: ${doubled}`);
      continue;
    }
    if (text.includes(String(want.text))) missing.push(`still on screen: ${want.text}`);
  }
  if (assertion.inColumn) {
    const column = columns[assertion.inColumn];
    if (column == null) {
      missing.push(`the screen has no column called ${assertion.inColumn}`);
    } else {
      for (const want of assertion.visible ?? []) {
        if (!String(column).includes(String(want.text))) {
          missing.push(`not under ${assertion.inColumn}: ${want.text}`);
        }
      }
    }
  }
  return { ok: missing.length === 0, missing };
}

// "Godel 1 1", "Castro Castro" - the crushed/doubled pen label defect.
export function findDoubledWord(text) {
  const match = /\b([\w-]+)\s+\1\b/i.exec(String(text ?? ""));
  return match ? `${match[1]} ${match[1]}` : null;
}

/**
 * Whether an assertion can tell the page after the write from the page before it. An assertion
 * made only of words the screen shows either way passes against a page nothing happened on,
 * which is how 178 assertions once reported green against a blank screen.
 */
export function discriminatingClauses(assertion) {
  const reasons = [];
  for (const want of assertion?.visible ?? []) {
    if (/\{\{\w+\}\}/.test(String(want.text))) reasons.push(`it looks for a value this journey itself created: ${want.text}`);
  }
  for (const want of [...(assertion?.notVisible ?? []), ...(assertion?.absent ?? [])]) {
    if (want?.doubledWord) reasons.push("it refuses a page that prints a word twice");
    else reasons.push(`it requires the state before the write to be gone: ${want.text}`);
  }
  if (assertion?.inColumn) reasons.push(`it requires the row to be under ${assertion.inColumn}`);
  return reasons;
}
