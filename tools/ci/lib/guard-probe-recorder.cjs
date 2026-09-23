// Records every repo file a guard actually READS, by wrapping the read calls themselves.
//
// This exists because a guard can be gutted to `print ok; exit 0` and keep its file, its manifest
// row, its build target and its CI step. Nothing that looks at the SHAPE of the script can tell
// the difference - and neither can the guard's own self-test, which is inside the part that was
// removed. What a gutted guard cannot do is READ anything. So the probe watches for the work, not
// for the text: no line counts, no hashes, no "the file is long enough".
const fs = require("node:fs");
const path = require("node:path");

const log = process.env.GOATOS_GUARD_PROBE_LOG;
const root = process.env.GOATOS_GUARD_PROBE_ROOT || process.cwd();
if (log) {
  const record = (file) => {
    try {
      if (typeof file !== "string") return;
      const full = path.resolve(file);
      if (!full.startsWith(root)) return;
      fs.appendFileSync(log, `${path.relative(root, full)}\n`);
    } catch {
      // A probe must never be able to break the guard it is watching.
    }
  };
  // Reading content is not the only way to examine a file: the large-file guard only ever asks
  // how big each one is. Statting counts as work, so the probe watches for it too.
  for (const name of [
    "readFileSync", "openSync", "createReadStream", "readdirSync", "readFile", "readdir", "open",
    "statSync", "lstatSync", "stat", "lstat", "accessSync", "access", "realpathSync"
  ]) {
    const original = fs[name];
    if (typeof original !== "function") continue;
    fs[name] = function wrapped(file, ...rest) {
      record(file);
      return original.call(this, file, ...rest);
    };
  }
  if (fs.promises) {
    for (const name of ["readFile", "readdir", "open", "stat", "lstat", "access", "realpath"]) {
      const original = fs.promises[name];
      if (typeof original !== "function") continue;
      fs.promises[name] = function wrapped(file, ...rest) {
        record(file);
        return original.call(this, file, ...rest);
      };
    }
  }
}
