// Records any attempt to LAUNCH A BROWSER, however the test reaches one.
//
// There are at least three routes and no grep sees all of them: a test can import Playwright and
// launch directly, reach a browser through a library it imports (compositing-checks), or shell out
// to a separate fixture script that does it (procurement-answer-accessibility, which greps zero for
// either word). What they have in common is the last step: a browser EXECUTABLE is spawned. So the
// probe watches the spawn, not the source.
//
// It records the ATTEMPT, not the success, so a machine with no browser installed still produces
// the same answer about which tests need one. Whether such a test PASSES is a separate question
// this probe deliberately cannot answer, and says so rather than guessing.
const child_process = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const log = process.env.GOATOS_BROWSER_PROBE_LOG;
// Deliberately loose. A browser binary's path is not a tidy word: Playwright's is
// ".../ms-playwright/chromium-1228/chrome-mac-arm64/Google Chrome for Testing.app/...", where the
// name is followed by a dash, a digit and a space in turn. Requiring a clean boundary matched none
// of it. Over-matching costs a test being called browser-needing when it is not, which the drift
// check then shows; under-matching costs the opposite, silently.
const BROWSER = /(chrome|chromium|headless_shell|firefox|webkit|msedge|ms-playwright)/i;
// A BROWSER is an executable, not a source file. Without this, `sales-chrome.tsx` - a React
// component about sales page chrome - made check-ui-contract-literals read as a browser test.
// That is a check firing on correct work, which is worse than not having the check.
const SOURCE_FILE = /\.(tsx?|jsx?|mjs|cjs|css|scss|json|md|ya?ml|html|svg|png|jpe?g|go|kt|java|py|sh|sql|txt|lock|map)$/i;

if (log) {
  const note = (kind, value) => {
    try {
      fs.appendFileSync(log, `${kind}\t${value}\n`);
    } catch {
      // A probe must never be able to break what it is watching.
    }
  };
  const look = (command, args) => {
    try {
      const text = [String(command ?? ""), ...(Array.isArray(args) ? args.map(String) : [])].join(" ");
      if (!BROWSER.test(text) || SOURCE_FILE.test(String(command ?? ""))) return;
      const binary = String(command ?? "");
      note("browser", binary);
      // A browser that is named but not installed is the "not checked" case, never a defect in
      // the test. Distinguish it here rather than letting the test's own error message decide.
      if (BROWSER.test(binary) && path.isAbsolute(binary) && !fs.existsSync(binary)) note("missing", binary);
    } catch {
      /* never throw out of a probe */
    }
  };
  // Watching the spawn alone is not enough: Playwright CHECKS FOR the browser binary and throws
  // before spawning anything when it is absent. On a machine with no browser installed that would
  // read as "this test needs no browser", which is the wrong answer in the dangerous direction -
  // the test would then be expected to pass without one. So the look at the binary counts as
  // intent, whether or not the launch follows.
  for (const name of ["existsSync", "statSync", "accessSync", "realpathSync"]) {
    const original = fs[name];
    if (typeof original !== "function") continue;
    fs[name] = function wrapped(file, ...rest) {
      try {
        const text = String(file ?? "");
        if (BROWSER.test(text) && !SOURCE_FILE.test(text)) {
          note("browser", text);
          if (!original.call(fs, text)) note("missing", text);
        }
      } catch {
        // `existsSync` returning false lands here for stat/access; that IS the missing case.
        try { note("missing", String(file ?? "")); } catch { /* never throw out of a probe */ }
      }
      return original.call(this, file, ...rest);
    };
  }

  for (const name of ["spawn", "spawnSync", "execFile", "execFileSync", "exec", "execSync"]) {
    const original = child_process[name];
    if (typeof original !== "function") continue;
    child_process[name] = function wrapped(command, args, ...rest) {
      look(command, args);
      return original.call(this, command, args, ...rest);
    };
  }
}
