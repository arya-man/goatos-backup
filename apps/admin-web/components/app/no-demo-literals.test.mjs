// guard: no-template-demo-literals. Template sections ship demo figures and copy (fCurrency 6789 /
// 1234 / 1012, the '2023' / 'Yearly' default select, 'Order total' / 'Earning' / 'Refunded' rows,
// 'Request' / 'Transfer' buttons). Pages feed the template-derived copies (components/app/sections)
// our data; none of those demo literals may appear in admin-web page source.
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import test from "node:test";

const appDir = new URL("../..", import.meta.url).pathname;
const DEMO = [
  /\b(?:fCurrency|fShortenNumber|fNumber)\(\s*(?:6789|1234|1012)\s*\)/,
  /useState\(\s*['"](?:2023|Yearly)['"]\s*\)/,
  /['"`>]\s*(?:Order total|Earning|Refunded)\s*['"`<]/,
  />\s*(?:Request|Transfer)\s*</,
];

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name.startsWith(".")) continue;
    const abs = join(dir, name);
    if (statSync(abs).isDirectory()) walk(abs, out);
    else if (/\.tsx?$/.test(name) && !/\.(test|stories)\.tsx?$/.test(name)) out.push(abs);
  }
  return out;
}

test("no template demo literal renders on an admin-web page", () => {
  const offenders = [];
  for (const root of ["app", "features", "components/app", "layouts/app"]) {
    for (const abs of walk(join(appDir, root))) {
      const src = readFileSync(abs, "utf8");
      for (const re of DEMO) if (re.test(src)) offenders.push(`${relative(appDir, abs)}: ${re}`);
    }
  }
  assert.deepEqual(offenders, []);
});

test("the pattern list catches the demo wiring it names", () => {
  const sample = "const [s] = useState('Yearly'); values={[fCurrency(6789)]}; renderRow('Order total', x); <Button>Request</Button>";
  assert.equal(DEMO.filter((re) => re.test(sample)).length, 4);
});

// The auth-split section is the template's verbatim, so its demo defaults ('Manage the job', 'More
// effectively with optimized workflows.') render unless the caller passes Mesha copy.
test("every AuthSplitLayout passes its own section title and subtitle", () => {
  const offenders = [];
  for (const abs of walk(join(appDir, "app"))) {
    const src = readFileSync(abs, "utf8");
    for (const m of src.matchAll(/<AuthSplitLayout\b([\s\S]*?)>/g)) {
      const section = /section:\s*\{[\s\S]*?\btitle:[\s\S]*?\bsubtitle:/.test(m[1]);
      if (!section) offenders.push(relative(appDir, abs));
    }
  }
  assert.deepEqual(offenders, []);
});
