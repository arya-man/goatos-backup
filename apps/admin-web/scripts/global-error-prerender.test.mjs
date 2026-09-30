// guard: global-error-prerender-no-providers
//
// Next 16 prerenders `/_global-error` from a synthetic loader tree that has NO root layout (a global
// error replaces app/layout.tsx), but that DOES carry the root-segment special files: app/loading.tsx
// becomes the Suspense fallback around the built-in error page, app/global-error.tsx the boundary.
// Whether React emits that loading fallback depends on whether the lazily required page chunk has
// resolved yet, so a loading.tsx that needs the app theme (theme.vars) failed `next build`
// intermittently with "Cannot read properties of undefined (reading 'palette')" (FIXJ-BUILD, 7e181ce32:
// the ShellSkeleton -> PageHeaderSkeleton -> template breadcrumb separator reads
// theme.vars.palette.text.disabled). These tests render the REAL root special files with the real MUI,
// the real theme and no provider around them, and force the fallback deterministically.
import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
import vm from "node:vm";
import test from "node:test";
import ts from "typescript";
import React from "react";
import { renderToString } from "react-dom/server";
import { prerender } from "react-dom/static";

const root = fileURLToPath(new URL("../", import.meta.url));
const require = createRequire(import.meta.url);
const modules = new Map();

function resolveFile(filename) {
  if (path.extname(filename) && existsSync(filename)) return filename;
  for (const ext of [".tsx", ".ts", ".mjs", ".js"]) if (existsSync(`${filename}${ext}`)) return `${filename}${ext}`;
  for (const ext of ["index.tsx", "index.ts"]) if (existsSync(path.join(filename, ext))) return path.join(filename, ext);
  throw new Error(`cannot resolve ${filename}`);
}

// Minimal CommonJS loader for the app's TS/TSX sources: `@/` alias, relative imports, CSS stubbed,
// everything else from node_modules (the real React, MUI, emotion, next).
function load(request) {
  const filename = resolveFile(request);
  if (filename.endsWith(".css")) return {};
  if (modules.has(filename)) return modules.get(filename).exports;
  const loaded = { exports: {} };
  modules.set(filename, loaded);
  const source = ts.transpileModule(readFileSync(filename, "utf8"), {
    compilerOptions: { jsx: ts.JsxEmit.ReactJSX, module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, esModuleInterop: true },
  }).outputText;
  const localRequire = (name) =>
    name.endsWith(".css") ? {}
      : name.startsWith("@/") ? load(path.join(root, name.slice(2)))
        : name.startsWith(".") ? load(path.resolve(path.dirname(filename), name))
          : require(name);
  vm.runInThisContext(`(function(require,module,exports){${source}\n})`, { filename })(localRequire, loaded, loaded.exports);
  return loaded.exports;
}

const Loading = load(path.join(root, "app/loading.tsx")).default;
const GlobalError = load(path.join(root, "app/global-error.tsx")).default;

// A page that suspends past the first render pass, like the `Promise.resolve().then(require(...))`
// page entry of the prerendered /_global-error tree when its chunk is not warm yet.
function slowPage() {
  return React.lazy(() => new Promise((resolve) => setTimeout(() => resolve({ default: () => React.createElement("p", null, "page") }), 20)));
}

async function streamToString(stream) {
  let html = "";
  const decoder = new TextDecoder();
  for await (const chunk of stream) html += decoder.decode(chunk, { stream: true });
  return html + decoder.decode();
}

test("app/loading.tsx renders with no root layout or theme provider around it", () => {
  const html = renderToString(React.createElement(Loading));
  assert.match(html, /data-shell-skeleton/);
});

test("the /_global-error tree (Suspense fallback = app/loading.tsx) prerenders when the page suspends", async () => {
  const Page = slowPage();
  const fallbacks = [];
  function TrackedLoading() {
    fallbacks.push(1);
    return React.createElement(Loading);
  }
  const tree = React.createElement(React.Suspense, { fallback: React.createElement(TrackedLoading) }, React.createElement(Page));
  const errors = [];
  const { prelude } = await prerender(tree, { onError: (error) => { errors.push(error); } });
  const html = await streamToString(prelude);
  assert.deepEqual(errors.map((error) => String(error?.message ?? error)), []);
  assert.ok(fallbacks.length > 0, "the fallback must actually render, or this test proves nothing");
  assert.match(html, /page/);
});

test("app/global-error.tsx server-renders on its own (it replaces the root layout)", () => {
  const error = Object.assign(new Error("boom"), { digest: "123" });
  const html = renderToString(React.createElement(GlobalError, { error, reset: () => {} }));
  assert.match(html, /<html/);
  assert.match(html, /123/);
});

test("every root-segment special file Next mounts in /_global-error is covered here", () => {
  // layout.tsx is replaced by global-error; not-found.tsx only renders on notFound(), which the
  // built-in error page never calls. Anything else at the app root (error, template, forbidden,
  // unauthorized, default) joins the provider-less tree and must be added to this file.
  const covered = new Set(["layout.tsx", "loading.tsx", "global-error.tsx", "not-found.tsx"]);
  const special = /^(layout|loading|global-error|not-found|error|template|forbidden|unauthorized|default)\.(tsx|ts|jsx|js)$/;
  const extra = readdirSync(path.join(root, "app")).filter((name) => special.test(name) && !covered.has(name));
  assert.deepEqual(extra, [], "render the new root special file without providers in this test");
});
