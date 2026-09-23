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
import { blankNonMarkup, labelsNear, routesOwningFiles, scanInteractiveSurfaces } from "./lib/interactive-surfaces.mjs";
import { readSourceFiles } from "./check-interactive-surfaces.mjs";

const adminWeb = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const LEDGER = path.join(adminWeb, "scripts/interactive-surface-ledger.json");

function entryFor(surface, sourceText) {
  const labels = labelsNear(sourceText, surface.offset);
  const where = `${surface.path}:${surface.line}`;
  // ONE label is already discriminating: a blank panel renders none of them, so [] never equals
  // ["action.retag.cancel"]. Requiring two was an arbitrary threshold that pushed real references
  // into the gap list.
  if (labels.length < 1) {
    // Say WHICH gap this is. A shell whose copy arrives as props and a table whose copy arrives
    // as data are both unanchorable offline, but they are closed by different work, and one
    // sentence covering both would hide that.
    const window = blankNonMarkup(sourceText).slice(surface.offset, surface.offset + 4000);
    const prop =
      /(?:aria-label|title|label|ariaLabel|closeLabel)=\{[A-Za-z_$][A-Za-z0-9_$.]*\}/.test(window) ||
      /\{\s*children\s*\}/.test(window);
    // A surface whose only reference is a value created at run time -- the id of the row that
    // opened it, a sentence the server composed for this one refusal. There IS a discriminating
    // reference; it just cannot be written as a literal here.
    const runtimeIdentity = /data-[a-z-]+=\{[A-Za-z_$][A-Za-z0-9_$.]*\}|role="(?:status|alert)"/.test(window);
    return {
      key: surface.key,
      kind: surface.kind,
      where,
      status: "not-checked",
      notCheckedReason: runtimeIdentity && !prop
        ? `the only reference this ${surface.kind} owns is a value made at run time -- the id of the ` +
          `row that opened it, or the sentence the server composed for this one refusal -- so no ` +
          `literal expected value can be written here; the sweep has to compare it against what it ` +
          `clicked, which nothing runs today`
        : prop
        ? `this ${surface.kind} is a shell: every word on it arrives as a prop from whichever screen ` +
          `renders it, so no expected value belongs here -- the reference belongs on each call site, ` +
          `and those call sites are inventoried separately`
        : `every control on this ${surface.kind} is labelled from data, not from its own source, so no ` +
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
        // The gate re-derives this from the same file and line and refuses the entry if it does
        // not land on the same set, so the value cannot be an author's invention (judge B1).
        provenance: { kind: "source", path: surface.path, line: surface.line, extractor: "labels-near" },
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

  const routesByPath = routesOwningFiles(files);
  const existing = existsSync(LEDGER) ? JSON.parse(readFileSync(LEDGER, "utf8")) : { entries: [] };
  const kept = new Map((existing.entries ?? []).map((e) => [e.key, e]));

  const entries = surfaces.map((surface) => {
    const entry = kept.get(surface.key) ?? entryFor(surface, byPath.get(surface.path) ?? "");
    const routes = routesByPath.get(surface.path) ?? [];
    // Routes are DERIVED every run, never hand-kept: a page that stops importing a component
    // must drop off its list on the next refresh rather than leave a stale claim behind.
    return routes.length
      ? { ...entry, routes }
      : {
          ...entry,
          routes: [],
          routeGapReason:
            /\/(loading|error|not-found)\.[jt]sx$/.test(surface.path)
              ? "Next.js reaches this file by convention rather than by an import, so the import graph cannot name its route; it is the loading state of the page it sits beside"
              : "no page in the app imports this file, directly or through a barrel -- nothing can reach this surface today, which is a finding of its own and not something an assertion can cover",
        };
  });
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
