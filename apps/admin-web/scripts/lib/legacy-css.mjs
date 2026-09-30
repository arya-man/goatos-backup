// Tests that once read the legacy stylesheets (app/frame.css, app/minimal-theme.css,
// app/mesha-theme.css, layouts/mesha-layout.css). FIXJ6 deleted them; `legacy-css-ceiling`
// (scripts/legacy-css-ceiling.test.mjs) keeps them absent. A "this legacy rule stays deleted"
// assertion reads "" for a missing file and so still holds; an assertion that NEEDED a legacy rule
// was rewritten against the template part / theme sx that replaced it.
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const appRoot = join(fileURLToPath(new URL(".", import.meta.url)), "..", "..");
const FILES = { "frame": "app/frame.css", "minimal-theme": "app/minimal-theme.css", "mesha-theme": "app/mesha-theme.css", "mesha-layout": "layouts/mesha-layout.css" };

/** The named legacy stylesheets joined ("" for each deleted one). */
export function legacyCss(...names) {
  return names.map((n) => {
    const abs = join(appRoot, FILES[n] ?? n);
    return existsSync(abs) ? readFileSync(abs, "utf8") : "";
  }).join("\n");
}
