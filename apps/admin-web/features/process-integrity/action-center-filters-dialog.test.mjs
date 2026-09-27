import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

// guard: action-center-filters-dialog (TR1-#7). The /action-center "My tasks" / Filters panel was a
// hand-made `div.modal.on.card` over a hand-made button scrim: no template backdrop, no focus trap.
// It is the template MUI Dialog now, and no feature or component renders the legacy `.modal` shell.
const here = fileURLToPath(new URL(".", import.meta.url));
const root = join(here, "..", "..");

test("action-center filters render the template Dialog", () => {
  const src = readFileSync(join(here, "action-center-filters.tsx"), "utf8");
  assert.match(src, /import Dialog from "@mui\/material\/Dialog"/);
  assert.match(src, /<Dialog [^>]*open=\{open\}[^>]*onClose=/);
  assert.doesNotMatch(src, /role="dialog"|className="modal|position: "fixed"/);
});

test("no legacy .modal shell in features or components", () => {
  const offenders = [];
  const walk = (dir) => {
    for (const name of readdirSync(dir)) {
      const p = join(dir, name);
      if (statSync(p).isDirectory()) walk(p);
      else if (/\.tsx$/.test(name) && /className="modal[ "]/.test(readFileSync(p, "utf8"))) offenders.push(p.slice(root.length + 1));
    }
  };
  walk(join(root, "features"));
  walk(join(root, "components"));
  assert.deepEqual(offenders, []);
});
