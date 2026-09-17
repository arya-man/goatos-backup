import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const layoutSource = readFileSync(new URL("../app/(admin)/layout.tsx", import.meta.url), "utf8");

assert.match(
  layoutSource,
  /<Suspense fallback=\{<AdminShellFallback \/>\}>[\s\S]*<AdminShell>\{children\}<\/AdminShell>[\s\S]*<\/Suspense>/,
  "admin layout must catch AdminShell bootstrap suspension locally",
);

assert.match(
  layoutSource,
  /function AdminShellFallback\(\)/,
  "admin layout needs a local shell fallback before route-level loading starts",
);

assert.doesNotMatch(
  layoutSource,
  /Loading Mesha admin data/,
  "admin shell bootstrap must not show the root app/loading.tsx copy",
);
