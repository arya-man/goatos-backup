import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

// TanStack's flexRender renders a column's `cell` function AS A COMPONENT, so a rebuilt column model
// is a new component type and React remounts every cell. Every server action re-renders the Feed
// Config route with fresh props, so a column model rebuilt from `pageContract` / the save action
// reset any open editor two seconds after its "Rate rejected" message appeared (found clicking the
// page, 2026-09-25). The column model must depend on CONTENT only; the live copy and action reach
// the stateful cells through context.
const tables = {
  "ration-grid-table.tsx": { deps: "[contractKey, editHeader]", stateful: ["EditRateCell", "RateValueCell"] },
  "feed-items-table.tsx": { deps: "[contractKey, placeholder]", stateful: ["StatusCellFromContext"] },
};

for (const [file, want] of Object.entries(tables)) {
  const source = readFileSync(new URL(`./${file}`, import.meta.url), "utf8");
  test(`${file}: the column model is keyed on content, never on the props' identity`, () => {
    assert.match(source, /const contractKey = JSON\.stringify\(contract\);/);
    assert.ok(source.includes(want.deps), `useMemo deps must be ${want.deps}`);
    const memo = source.slice(source.indexOf("const columns = useMemo("), source.indexOf(want.deps));
    assert.doesNotMatch(memo, /pageContract=\{pageContract\}|action=\{action\}|action=\{statusAction\}/,
      "a stateful cell must read the live copy and action from context, not capture them");
    for (const cell of want.stateful) assert.match(memo, new RegExp(`<${cell} row=\\{`));
  });
}

// A refused save left Apply on "Loading" and both buttons disabled forever: the form keyed its
// disabled state on useTransition's isPending, which waits for the route re-render every write now
// triggers. The form must wait only for the action's own answer.
test("feed-config-editor.tsx: the form's saving state ends when the action answers, not with the page redraw", () => {
  const source = readFileSync(new URL("./feed-config-editor.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(source, /import \{[^}]*useTransition[^}]*\} from "react"/);
  assert.doesNotMatch(source, /startTransition\(/);
  assert.match(source, /const \[pending, setPending\] = useState\(false\);/);
  assert.match(source, /\} finally \{\s*inFlight\.current = false;\s*setPending\(false\);/);
  assert.match(source, /outcome = \{ ok: false, messageKey: "action\.save_unreachable" \};/);
  // Cancel clears a refusal too, or "Rate rejected" lingers under the closed button.
  assert.match(source, /setIdem\(CLOSED_STATE\);\s*setResult\(null\);/);
});
