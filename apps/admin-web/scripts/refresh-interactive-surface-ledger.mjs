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
import { blankNonMarkup, rootLabels, routesOwningFiles, scanInteractiveSurfaces } from "./lib/interactive-surfaces.mjs";
import { readSourceFiles } from "./check-interactive-surfaces.mjs";

const adminWeb = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const LEDGER = path.join(adminWeb, "scripts/interactive-surface-ledger.json");

function entryFor(surface, sourceText) {
  const labels = rootLabels(sourceText, surface.offset);
  const where = `${surface.path}:${surface.line}`;
  if (labels.length === 0) {
    // Judge B-1. Everything else this surface renders sits on a CHILD control, and admin-web
    // compiles which children a person meets from a backend contract against their permissions.
    // Scraping the component source lists every control it COULD render -- a superset of what
    // most principals see -- so an exact comparison fails on a correct page in both directions.
    // The expectation belongs per principal, and nothing offline knows the principals.
    const window = blankNonMarkup(sourceText).slice(surface.offset, surface.offset + 4000);
    const prop =
      /(?:aria-label|title|label|ariaLabel|closeLabel)=\{[A-Za-z_$][A-Za-z0-9_$.]*\}/.test(window) ||
      /\{\s*children\s*\}/.test(window);
    return {
      key: surface.key,
      kind: surface.kind,
      where,
      status: "not-checked",
      notCheckedReason: prop
        ? `this ${surface.kind} puts no label of its own on screen -- its heading arrives as a prop from ` +
          `whichever screen renders it -- so the only expectation that could be written here belongs ` +
          `to each call site, and what its child controls show depends on the permissions of whoever ` +
          `is signed in`
        : `this ${surface.kind} carries no label on its own element, and every control inside it is ` +
          `compiled per principal from the page contract, so no expected value here would be true of ` +
          `more than one person; it needs a run signed in as a named principal to state one`,
    };
  }
  return {
    key: surface.key,
    kind: surface.kind,
    where,
    status: "stated-not-executed",
    viewports: ["1440", "390"],
    // The one principal this expectation IS true of, and why it is safe for all of them: the
    // label sits on the surface's own element, so it is painted whenever the surface is mounted,
    // whatever the contract compiled for that person's child controls.
    principal:
      "any principal whose page contract lets them open this surface -- the expectation is only " +
      "about the surface's own label, which does not vary with permissions",
    notExecutedReason:
      "no sweep has opened this surface yet; the browser lane is disabled after the 2026-09-23 incident",
    assertions: [
      {
        subject:
          `the words on the ${surface.kind}'s own heading when a person opens it (${where}) -- a panel ` +
          `that opened empty, or whose copy did not resolve, shows none of them`,
        operator: "field-set-contains-all",
        expected: labels,
        provenance: { kind: "source", path: surface.path, line: surface.line, extractor: "root-label" },
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
            "no page, layout or router-convention file in the app reaches this file -- not through " +
            "an import, not through a barrel, not through a dynamic import. Nothing can render this " +
            "surface today, which is a finding of its own rather than something an assertion covers",
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
