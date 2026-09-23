// Records every module a guard LOADS, which the fs probe cannot see.
//
// Node's module loader reads through internal bindings, not the public `fs`, so a guard that
// reaches its inputs with `import()` or `require()` looks like a guard that read nothing. That
// would be a false accusation - the exact failure this whole check was corrected for once
// already - so the accusation is never made without this second look.
import { appendFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const log = process.env.GOATOS_GUARD_PROBE_LOG;
const root = process.env.GOATOS_GUARD_PROBE_ROOT || process.cwd();

export async function resolve(specifier, context, nextResolve) {
  const resolved = await nextResolve(specifier, context);
  if (log && resolved?.url?.startsWith("file:")) {
    try {
      const full = fileURLToPath(resolved.url);
      if (full.startsWith(root)) appendFileSync(log, `${path.relative(root, full)}\n`);
    } catch {
      // A probe must never be able to break what it is watching.
    }
  }
  return resolved;
}
