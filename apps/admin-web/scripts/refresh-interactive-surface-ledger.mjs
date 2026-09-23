#!/usr/bin/env node
// Regenerates apps/admin-web/scripts/interactive-surface-ledger.json from the source tree.
//
// It never invents a verdict. For each surface it can anchor on, it writes a
// `stated-not-executed` entry whose expected value is the set of control labels the surface's own
// source declares -- a reference that goes red on a blank screen, an empty payload, or a renamed
// control, but that still has NEVER been run against the product, so it counts zero until a sweep
// receipt names it. Surfaces whose labels come from data get `not-checked` with that reason.
//
// Hand-written entries are preserved verbatim: this only fills in surfaces the ledger has no
// decision about, and drops entries whose surface has left the tree.
import { readFileSync, writeFileSync, existsSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { blankNonMarkup, scanInteractiveSurfaces } from "./lib/interactive-surfaces.mjs";
import { readSourceFiles } from "./check-interactive-surfaces.mjs";

const adminWeb = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const LEDGER = path.join(adminWeb, "scripts/interactive-surface-ledger.json");

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
    /\bt\(\s*"([A-Za-z0-9_.-]{2,60})"\s*\)/g,
    /\bcopy\(\s*[A-Za-z0-9_$.]+\s*,\s*"([A-Za-z0-9_.-]{2,60})"\s*\)/g,
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

function entryFor(surface, sourceText) {
  const labels = labelsNear(sourceText, surface.offset);
  const where = `${surface.path}:${surface.line}`;
  if (labels.length < 2) {
    return {
      key: surface.key,
      kind: surface.kind,
      where,
      status: "not-checked",
      notCheckedReason:
        `every control on this ${surface.kind} is labelled from data, not from its own source, so no ` +
        `expected value can be derived offline; it needs one run against a seeded throwaway dataset ` +
        `to state a reference that could fail`,
    };
  }
  return {
    key: surface.key,
    kind: surface.kind,
    where,
    status: "stated-not-executed",
    viewports: ["1440", "390"],
    notExecutedReason:
      "no sweep has opened this surface yet; the browser lane is disabled after the 2026-09-23 incident",
    assertions: [
      {
        subject:
          `the labelled controls, copy slots and named fields a person meets on the ${surface.kind} ` +
          `"${surface.anchor}" (${where})`,
        operator: "field-set-equals",
        expected: labels,
        // A blank screen, a failed payload, or a panel parked off-screen all read as no controls.
        blankScreenValue: [],
      },
    ],
  };
}

function main() {
  const files = readSourceFiles();
  const byPath = new Map(files.map((f) => [f.path, f.text]));
  const surfaces = scanInteractiveSurfaces(files).map((surface) => {
    const text = byPath.get(surface.path) ?? "";
    const offset = blankNonMarkup(text).split("\n").slice(0, surface.line - 1).join("\n").length;
    return { ...surface, offset };
  });

  const existing = existsSync(LEDGER) ? JSON.parse(readFileSync(LEDGER, "utf8")) : { entries: [] };
  const kept = new Map((existing.entries ?? []).map((e) => [e.key, e]));

  const entries = surfaces.map((surface) => kept.get(surface.key) ?? entryFor(surface, byPath.get(surface.path) ?? ""));
  const ledger = {
    note:
      "Decisions about every edit form, inline editor, row action and modal in admin-web. " +
      "Statuses: covered (assertion stated AND run, with a receipt), stated-not-executed (assertion " +
      "stated and proven discriminating, never run -- counts zero), not-checked (with a reason). " +
      "Regenerate with scripts/refresh-interactive-surface-ledger.mjs; the gate is " +
      "scripts/check-interactive-surfaces.mjs.",
    generated: "2026-09-23",
    entries,
  };
  writeFileSync(LEDGER, `${JSON.stringify(ledger, null, 2)}\n`);
  console.log(`interactive-surface ledger: ${entries.length} entries written to ${path.relative(adminWeb, LEDGER)}`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main();
