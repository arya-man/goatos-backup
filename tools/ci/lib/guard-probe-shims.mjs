// A guard's reads are not all made by the guard's own process.
//
// The first version of this probe watched `fs` inside one node process and parsed a `bash -x`
// trace. Both miss the common cases: a shell guard's work happens in CHILD commands the trace does
// not follow, and `execFileSync("grep", ...)` from a node guard is invisible for the same reason.
// Measured, 17 of the 20 shell guards read "zero" files that way - and a guard that plainly reads
// the repo was then reported as checking nothing, which is a check firing on correct work.
//
// So the probe follows the reading wherever it happens, by two routes that both propagate to
// children on their own: NODE_OPTIONS for node, and a PATH of thin wrappers for the ordinary
// reading commands a shell guard uses. Nothing is exempted and no bar is lowered.
import { chmodSync, mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

// The commands a guard reads the repo with. A wrapper logs the arguments and then execs the real
// tool, so behaviour is unchanged and only the observation is added.
export const SHIMMED = ["grep", "rg", "egrep", "fgrep", "cat", "head", "tail", "sed", "awk", "find", "wc", "sort", "comm", "diff", "jq", "git", "stat", "file", "xargs"];

export function writeShims(dir, { log, realPath }) {
  mkdirSync(dir, { recursive: true });
  for (const tool of SHIMMED) {
    const script = [
      "#!/bin/sh",
      // Log every argument; the reader keeps the ones that name a file really in the repo, so a
      // flag or a pattern costs nothing and an unanticipated path is still counted.
      `printf '%s\\n' "$@" >> ${JSON.stringify(log)} 2>/dev/null || true`,
      `PATH=${JSON.stringify(realPath)} exec ${tool} "$@"`,
      ""
    ].join("\n");
    const file = path.join(dir, tool);
    writeFileSync(file, script);
    chmodSync(file, 0o755);
  }
  return dir;
}
