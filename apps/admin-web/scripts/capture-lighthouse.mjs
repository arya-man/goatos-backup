import { mkdirSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { spawn } from "node:child_process";

function readOption(name, fallback) {
  const prefixed = `--${name}=`;
  const index = process.argv.indexOf(`--${name}`);
  if (index !== -1 && process.argv[index + 1]) return process.argv[index + 1];
  const inline = process.argv.find((arg) => arg.startsWith(prefixed));
  return inline ? inline.slice(prefixed.length) : fallback;
}

const url = readOption("url", process.env.ADMIN_WEB_LIGHTHOUSE_URL || process.env.ADMIN_WEB_URL || "http://127.0.0.1:3300/?scope_mode=company");
const out = resolve(readOption("output", process.env.ADMIN_WEB_LIGHTHOUSE_REPORT || ".codex-goatos-render/lighthouse/admin-web-lighthouse.json"));
const chromeFlags = readOption("chrome-flags", process.env.ADMIN_WEB_LIGHTHOUSE_CHROME_FLAGS || "--headless=new --no-sandbox");
const categories = readOption("categories", process.env.ADMIN_WEB_LIGHTHOUSE_CATEGORIES || "performance,accessibility,best-practices,seo");
const extraHeaders = readOption("extra-headers", process.env.ADMIN_WEB_LIGHTHOUSE_EXTRA_HEADERS || "");
const expectFinalUrlContains = readOption("expect-final-url-contains", process.env.ADMIN_WEB_LIGHTHOUSE_EXPECT_FINAL_URL_CONTAINS || expectedUrlFragment(url));
const minimumScores = parseMinimumScores(readOption("min-scores", process.env.ADMIN_WEB_LIGHTHOUSE_MIN_SCORES || "performance=70,accessibility=90,best-practices=90,seo=80"));

mkdirSync(dirname(out), { recursive: true });

const args = [
  "lighthouse",
  url,
  `--chrome-flags=${chromeFlags}`,
  "--output=json",
  `--output-path=${out}`,
  `--only-categories=${categories}`,
  "--quiet",
];
if (extraHeaders) args.push(`--extra-headers=${extraHeaders}`);

const child = spawn("npm", ["exec", "--", ...args], { stdio: "inherit", shell: false });
child.on("exit", (code, signal) => {
  if (signal) {
    console.error(`lighthouse terminated by ${signal}`);
    process.exit(1);
  }
  if (code !== 0) process.exit(code ?? 1);
  const report = JSON.parse(readFileSync(out, "utf8"));
  const finalUrl = report.finalDisplayedUrl || report.finalUrl || report.requestedUrl || "";
  if (expectFinalUrlContains && !finalUrl.includes(expectFinalUrlContains)) {
    console.error(`Lighthouse audited the wrong page. final_url=${finalUrl} expected_fragment=${expectFinalUrlContains}`);
    process.exit(1);
  }
  if (expectFinalUrlContains !== "/login" && /\/login(?:$|[?#])/.test(finalUrl)) {
    console.error(`Lighthouse reached login instead of the authenticated admin page. final_url=${finalUrl}`);
    process.exit(1);
  }
  const scores = categoryScores(report);
  const failures = scoreFailures(scores, minimumScores);
  console.log(`lighthouse_report=${out}`);
  console.log(`lighthouse_scores=${JSON.stringify(scores)}`);
  if (failures.length) {
    console.error(`Lighthouse scores below budget: ${failures.join(", ")}`);
    process.exit(1);
  }
});

function expectedUrlFragment(value) {
  try {
    const parsed = new URL(value);
    return parsed.pathname === "/" ? parsed.host : parsed.pathname;
  } catch {
    return "";
  }
}

function parseMinimumScores(value) {
  return Object.fromEntries(
    String(value || "")
      .split(",")
      .map((part) => part.trim())
      .filter(Boolean)
      .map((part) => {
        const [key, raw] = part.split("=");
        const score = Number(raw);
        if (!key || !Number.isFinite(score)) {
          throw new Error(`Invalid score budget entry: ${part}`);
        }
        return [key.trim(), score];
      }),
  );
}

function categoryScores(report) {
  return Object.fromEntries(
    Object.entries(report?.categories ?? {}).map(([key, value]) => [key, Math.round((value.score ?? 0) * 100)]),
  );
}

function scoreFailures(scores, minimums) {
  return Object.entries(minimums)
    .filter(([key, minimum]) => (scores[key] ?? 0) < minimum)
    .map(([key, minimum]) => `${key}=${scores[key] ?? 0}<${minimum}`);
}
