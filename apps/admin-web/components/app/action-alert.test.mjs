import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

// guard: alert-actions-stack (J3 P1-3). MUI Alert lays `action` out as a no-wrap column beside the
// message, so an Alert whose action groups two or more buttons (a Stack / Box / fragment of them)
// squeezed them at 390 until every label broke one word per line ("Open / the / draft" on
// /vaccination/plan). Grouped actions go through components/app/action-alert (ActionAlert), which
// drops them under the message on phones and never wraps a label.
const appRoot = new URL("../../", import.meta.url).pathname;
const walk = (dir) =>
  readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (name === "node_modules" || name.startsWith(".")) return [];
    return statSync(path).isDirectory() ? walk(path) : name.endsWith(".tsx") ? [path] : [];
  });

/** Opening `<Alert …>` tags whose `action={…}` groups several controls. */
export function groupedAlertActions(text) {
  const out = [];
  for (let i = text.indexOf("<Alert"); i >= 0; i = text.indexOf("<Alert", i + 1)) {
    if (!/[\s>]/.test(text[i + 6] ?? "")) continue;
    let j = i;
    let depth = 0;
    for (; j < text.length; j++) {
      const c = text[j];
      if (c === "{") depth++;
      else if (c === "}") depth--;
      else if (c === ">" && depth === 0) break;
    }
    const tag = text.slice(i, j);
    const at = tag.indexOf("action={");
    if (at < 0) continue;
    const action = tag.slice(at);
    const grouped = /<(Stack|Box|>)|<>/.test(action) && (action.match(/<(Button|LinkButton|IconButton)\b/g) ?? []).length >= 2;
    if (grouped) out.push(text.slice(0, i).split("\n").length);
  }
  return out;
}

test("guard: alert-actions-stack - self-test", () => {
  assert.deepEqual(groupedAlertActions('<Alert severity="warning" action={<Stack direction="row"><Button>A</Button><LinkButton href="/x">B</LinkButton></Stack>}>x</Alert>'), [1]);
  // alternatives (one button per branch) are fine
  assert.deepEqual(groupedAlertActions('<Alert action={f ? <LinkButton href="/a">A</LinkButton> : <LinkButton href="/b">B</LinkButton>}>x</Alert>'), []);
  assert.deepEqual(groupedAlertActions('<ActionAlert actions={<><Button>A</Button><Button>B</Button></>}>x</ActionAlert>'), []);
});

test("guard: alert-actions-stack - an Alert with grouped actions uses ActionAlert", () => {
  const bad = [];
  for (const dir of ["features", "components/app", "app"]) {
    for (const path of walk(join(appRoot, dir))) {
      for (const line of groupedAlertActions(readFileSync(path, "utf8"))) bad.push(`${path.slice(appRoot.length)}:${line}`);
    }
  }
  assert.deepEqual(bad, [], "use ActionAlert (components/app/action-alert) for an Alert with more than one action button");
});
