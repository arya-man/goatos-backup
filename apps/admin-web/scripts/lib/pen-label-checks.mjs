// Pen / partition label checks for the live browser smoke (lane 1).
//
// WHY THIS EXISTS
// The farm reads its pens off the screen. Since Aug 2026 the same defect class has
// shipped again and again, always as a WRONG STRING on a real page, and always caught
// by a human rather than by the automation:
//
//   475312f5d fix: prevent doubled worded partition labels
//             (replaced ad-hoc `operational_location_display || shed_label` rendering
//              with the shared helper across 8 admin-web files, plus Android and Go)
//   e2a70f3db fix(weighing): stop rendering the pen twice on the schedule
//             ("Mandela 1 - Part 1 - Part 1" — the composer was fed an already-composed name)
//   1a256a225 test(weighing): follow one pen's label from the bucket row to the verifier
//             (string assertions at every hop, because field-presence tests all passed
//              while the real output was wrong)
//   601f6b72b fix(migrations): make the merged pen-label repair actually runnable
//   3d4dfc8d7 docs(agents): harden operational-location review guard
//
// Every one of those was found by reading a screen. Until now NOTHING in the automation
// looked at a rendered label, so lane 1 could sweep a page showing "Castro 1 1" and
// report it green. This file is that missing eye.
//
// THE FOUR REPORTED SHAPES (maintainer's own words, 2026-09-23):
//   "we have double partitions coming up like godel1 - part 1 - part 1 or just godel1
//    without partition or castro part 1 or instead of castro 1 or like castro 1 1"
//
//   doubled worded partition   "Godel 1 - Part 1 - Part 1"   -> P-pen-part-doubled
//   missing partition          "Godel 1" with no part        -> P-pen-partition-missing
//   wrong separator            "Godel 1 Part 3" / "Castro - 2" -> P-pen-separator-wrong
//   doubled numeral            "Castro 1 1"                  -> P-pen-number-doubled
//   plus the sentinel leak     "Yashoda whole"               -> P-pen-whole-leaked
//
// HOW A LABEL IS JUDGED — from the farm's data, not from a regex someone invented.
// pen-label-vocabulary.json carries the real (shed, partition_label) pairs. The valid
// rendered set is COMPOSED from them with the documented separator rule, and a string on
// screen is judged only when it begins with a real shed name. Anything that does not start
// with a real shed name is never judged at all, which is what keeps this quiet.
//
// Sheds are matched LONGEST NAME FIRST. The farm has both "Yashoda" and "Old Yashoda";
// prefix matching in the other order reports every "Old Yashoda 3" as a broken "Yashoda".
//
// SEPARATOR RULE (see apps/admin-web/lib/operational-location.ts):
//   bare numeral  ('2')      -> "<shed> <label>"      "Castro 2"          SPACE
//   worded label  ('Part 3') -> "<shed> - <label>"    "Godel 1 - Part 3"  DASH
// The dash exists because six live shed names END IN A DIGIT (Godel 1, Godel 2,
// Mandela 1, Mandela 2, Sumathi 1, Sumathi 2), so "Godel 1 1" would be ambiguous.
// That same fact is why "Castro 1" is CORRECT and "Castro 1 1" is not, and why this
// checker must never treat "a name ending in a digit" as suspicious on its own.
//
// DELIBERATE SILENCE — a check that fires on a correct label is worse than no check:
//   - A well-formed label whose partition is simply not in the vocabulary yet
//     ("Yashoda 11" after a new pen is added) is NOT flagged. Only the five known
//     malformations are. The vocabulary going stale must never invent a bug.
//   - Prose that merely starts with a shed name ("Castro 1 has 42 animals") is not a
//     label and is not judged.
//   - A bare shed name is flagged only where a SIBLING in the same table column / list
//     shows partitions for that same shed. Shed-grain aggregation is a real feature
//     (shed-wise charts, shed filters), and those render every entry bare; flagging
//     them would bury the real defect in noise.

/** Composes the one correct rendering of a (shed, partition) pair. Mirrors
 *  operationalLocationLabel() in apps/admin-web/lib/operational-location.ts,
 *  oploc.Display() in Go and PartitionLabel.kt on Android. */
export function composePenLabel(shedName, partitionLabel) {
  const shed = String(shedName ?? "").trim();
  const label = String(partitionLabel ?? "").trim();
  if (!label || label.toLowerCase() === "whole") return shed;
  if (!shed) return label;
  return /^\d+$/.test(label) ? `${shed} ${label}` : `${shed} - ${label}`;
}

const norm = (s) => String(s ?? "").replace(/\s+/g, " ").trim();
const fold = (s) => norm(s).toLowerCase();

/** Turns the vocabulary manifest into the lookup this checker runs against. */
export function buildPenVocabulary(manifest) {
  const sheds = (manifest?.sheds ?? [])
    .map((shed) => {
      const name = norm(shed.name);
      const labels = (shed.labels ?? []).map(norm).filter(Boolean);
      return {
        name,
        folded: fold(name),
        labels,
        // A shed's convention follows its own stored labels, never a guess from the shed name.
        worded: labels.some((l) => !/^\d+$/.test(l)),
        partitioned: labels.length > 0,
        valid: new Set(labels.map((l) => fold(composePenLabel(name, l)))),
      };
    })
    .filter((shed) => shed.name);
  // Longest first: "Old Yashoda" must win over "Yashoda".
  sheds.sort((a, b) => b.name.length - a.name.length || a.name.localeCompare(b.name));
  return { sheds };
}

/** The shed a rendered string belongs to, plus the separator and remainder after it.
 *  Returns null when the string does not begin with a real shed name. */
function matchShed(text, vocab) {
  const t = norm(text);
  const folded = fold(t);
  for (const shed of vocab.sheds) {
    if (!folded.startsWith(shed.folded)) continue;
    const after = t.slice(shed.name.length);
    if (after === "") return { shed, sep: "", rest: "" };
    // The shed name must end on a word boundary, or "Castro" matches "Castrol".
    const sep = after.match(/^(\s*-\s*|\s+)/);
    if (!sep) continue;
    return { shed, sep: sep[0], rest: t.slice(shed.name.length + sep[0].length) };
  }
  return null;
}

const PART_RE = /\bpart\s+\d{1,3}\b/gi;

/**
 * Judges ONE rendered string. Returns null when the string is not a pen label, or is a
 * correct one, or is well-formed but simply unknown to the vocabulary.
 *
 * The bare-shed case returns pending:true rather than a finding: on its own it is not
 * evidence, and penLabelFindings() promotes it only when a sibling proves the column
 * renders partitions for that shed.
 */
export function classifyPenLabel(text, vocab) {
  const t = norm(text);
  // A pen label is a short leaf string. Anything longer is prose that happens to name a pen.
  if (!t || t.length > 64) return null;
  const hit = matchShed(t, vocab);
  if (!hit) return null;
  const { shed, sep, rest } = hit;

  if (rest === "") {
    if (!shed.partitioned) return null; // a genuinely unpartitioned shed renders bare, correctly
    return { pending: true, shed: shed.name, text: t };
  }

  // Correct, per the farm's own data. Nothing to say.
  if (shed.valid.has(fold(t))) return null;

  const restFolded = fold(rest);

  // "Godel 1 - Part 1 - Part 1" — the composer ran twice over the same name.
  const parts = restFolded.match(PART_RE) ?? [];
  if (parts.length >= 2) {
    return { pattern: "P-pen-part-doubled", shed: shed.name, text: t, detail: `pen "${t}" repeats its part number` };
  }

  // "Yashoda whole" — the non-partition sentinel is a comparison key, never a label.
  if (/(^|[\s-])whole$/.test(restFolded)) {
    return { pattern: "P-pen-whole-leaked", shed: shed.name, text: t, detail: `pen "${t}" renders the sentinel "whole" instead of a bare shed name` };
  }

  // "Castro 1 1" — the same numeral twice after the shed name.
  const twice = restFolded.match(/^(\d{1,3})\s+\1$/);
  if (twice) {
    return { pattern: "P-pen-number-doubled", shed: shed.name, text: t, detail: `pen "${t}" repeats the partition number "${twice[1]}"` };
  }
  // "Godel 1 1" — the shed name already ends in the numeral that was appended again.
  // This is the exact case alreadyEndsWithPartition() exists to prevent.
  const tailDigit = shed.name.match(/(\d{1,3})$/);
  if (tailDigit && /^\d{1,3}$/.test(restFolded) && restFolded === tailDigit[1]) {
    return { pattern: "P-pen-number-doubled", shed: shed.name, text: t, detail: `pen "${t}" repeats the "${tailDigit[1]}" already in the shed name` };
  }

  // Wrong separator for this shed's convention.
  const dashed = /-/.test(sep);
  if (shed.worded && !dashed && /^part\s+\d{1,3}$/.test(restFolded)) {
    return { pattern: "P-pen-separator-wrong", shed: shed.name, text: t, detail: `pen "${t}" needs a dash: "${composePenLabel(shed.name, rest)}"` };
  }
  if (!shed.worded && dashed && /^\d{1,3}$/.test(restFolded)) {
    return { pattern: "P-pen-separator-wrong", shed: shed.name, text: t, detail: `pen "${t}" needs a space: "${composePenLabel(shed.name, rest)}"` };
  }

  // Well-formed but unknown to the vocabulary (a newly added pen), or prose. Stay quiet.
  return null;
}

/**
 * Judges every candidate collected from one screen.
 * Candidates are { index, text, group } — group is a table column or list identity, so
 * "this shed is bare HERE while its siblings HERE show partitions" is answerable.
 */
export function penLabelFindings(candidates, manifest) {
  const vocab = buildPenVocabulary(manifest);
  const findings = [];
  const pendingBare = [];
  // group -> shed -> set of distinct partitioned renderings seen in that group
  const partitionedByGroup = new Map();

  for (const candidate of candidates) {
    const verdict = classifyPenLabel(candidate.text, vocab);
    const group = candidate.group ?? "";
    // Record every WELL-FORMED partitioned label, so a bare sibling can be judged against it.
    const hit = matchShed(candidate.text, vocab);
    if (hit && hit.rest !== "" && hit.shed.valid.has(fold(norm(candidate.text)))) {
      const key = `${group}|${hit.shed.name}`;
      if (!partitionedByGroup.has(key)) partitionedByGroup.set(key, new Set());
      partitionedByGroup.get(key).add(fold(norm(candidate.text)));
    }
    if (!verdict) continue;
    if (verdict.pending) {
      pendingBare.push({ ...verdict, index: candidate.index, group });
      continue;
    }
    findings.push({ pattern: verdict.pattern, index: candidate.index, text: verdict.text, detail: verdict.detail });
  }

  for (const bare of pendingBare) {
    const siblings = partitionedByGroup.get(`${bare.group}|${bare.shed}`);
    // Two distinct partitioned siblings, so the column is unambiguously rendering pens,
    // not sheds. One is not enough: a park can hold a building whose name is another
    // building's name plus a numeral.
    if (!siblings || siblings.size < 2) continue;
    const examples = [...siblings].slice(0, 2).join('", "');
    findings.push({
      pattern: "P-pen-partition-missing",
      index: bare.index,
      text: bare.text,
      detail: `pen "${bare.text}" is shown with no part number beside "${examples}"`,
    });
  }

  return findings;
}

/**
 * Runs INSIDE the browser (serialised by page.evaluate). Must stay self-contained.
 * Collects short leaf strings and the column/list each one sits in, and tags every
 * candidate so the classified findings can be outlined for the screenshot.
 */
export function collectPenLabelCandidates() {
  const out = [];
  const txt = (el) => (el.textContent ?? "").replace(/\s+/g, " ").trim();
  const hidden = (el) => {
    for (let p = el; p && p !== document.documentElement; p = p.parentElement) {
      if (p.hasAttribute("data-smoke-ignore") || p.getAttribute("aria-hidden") === "true" || p.hasAttribute("hidden")) return true;
      const s = getComputedStyle(p);
      if (s.display === "none" || s.visibility === "hidden" || Number(s.opacity) === 0) return true;
      if (p.matches(".sr-only, .visually-hidden, .visuallyhidden")) return true;
    }
    return false;
  };
  const root = document.querySelector("main") ?? document.body;
  const tableIds = new Map();
  const parentIds = new Map();
  const idFor = (map, el, prefix) => {
    if (!map.has(el)) map.set(el, `${prefix}${map.size}`);
    return map.get(el);
  };

  let index = 0;
  for (const el of root.querySelectorAll("td, th, li, span, b, strong, p, h3, h4, small, a, button, option, div, .wbl-text, .gclab, .mclab, .hblab")) {
    const t = txt(el);
    if (!t || t.length > 64) continue;
    // Leaf text only: skip containers whose text is assembled from children, or the same
    // string is collected once per ancestor and one bad pen becomes six findings.
    if (Array.from(el.children).some((c) => txt(c))) continue;
    if (hidden(el)) continue;
    const r = el.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) continue;
    // Group = the column this label sits in. A table column is the strongest grouping the
    // farm's screens offer; otherwise fall back to the immediate list parent.
    const td = el.closest("td, th");
    let group;
    if (td) {
      const table = td.closest("table");
      group = `${idFor(tableIds, table ?? td, "t")}|c${td.cellIndex ?? 0}`;
    } else {
      group = idFor(parentIds, el.parentElement ?? root, "p");
    }
    el.setAttribute("data-pen-candidate", String(index));
    out.push({ index, text: t, group });
    index += 1;
  }
  return out;
}
