import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";

function readOption(name, fallback) {
  const prefixed = `--${name}=`;
  const index = process.argv.indexOf(`--${name}`);
  if (index !== -1 && process.argv[index + 1]) return process.argv[index + 1];
  const inline = process.argv.find((arg) => arg.startsWith(prefixed));
  return inline ? inline.slice(prefixed.length) : fallback;
}

const url = readOption("url", process.env.PAGESPEED_URL || process.env.ADMIN_WEB_PUBLIC_URL);
if (!url) {
  console.error("Missing --url, PAGESPEED_URL, or ADMIN_WEB_PUBLIC_URL. PageSpeed Insights only works for URLs Google can reach.");
  process.exit(2);
}
const parsedUrl = new URL(url);
if (/^localhost$|^127\./.test(parsedUrl.hostname)) {
  console.error("PageSpeed Insights cannot audit local authenticated pages. Use perf:lighthouse for local Chrome, or pass a public STG/prod URL.");
  process.exit(2);
}

const out = resolve(readOption("output", process.env.PAGESPEED_REPORT || ".codex-goatos-render/pagespeed/admin-web-pagespeed.json"));
const strategy = readOption("strategy", process.env.PAGESPEED_STRATEGY || "mobile");
const minimumScores = parseMinimumScores(readOption("min-scores", process.env.PAGESPEED_MIN_SCORES || "performance=70,accessibility=90,best-practices=90,seo=80"));
const api = new URL("https://www.googleapis.com/pagespeedonline/v5/runPagespeed");
api.searchParams.set("url", url);
api.searchParams.set("strategy", strategy);
for (const category of readOption("categories", process.env.PAGESPEED_CATEGORIES || "PERFORMANCE,ACCESSIBILITY,BEST_PRACTICES,SEO").split(",")) {
  api.searchParams.append("category", category.trim());
}
const apiKey = readOption("api-key", process.env.PAGESPEED_API_KEY);
if (apiKey) {
  api.searchParams.set("key", apiKey);
}

const response = await fetch(api, { cache: "no-store" });
const body = await response.text();
mkdirSync(dirname(out), { recursive: true });
writeFileSync(out, body);
if (!response.ok) {
  console.error(`PageSpeed Insights failed with HTTP ${response.status}; report=${out}`);
  process.exit(1);
}

const json = JSON.parse(body);
const categories = json?.lighthouseResult?.categories ?? {};
const scores = Object.fromEntries(
  Object.entries(categories).map(([key, value]) => [key, Math.round((value.score ?? 0) * 100)]),
);
console.log(`pagespeed_report=${out}`);
console.log(`pagespeed_scores=${JSON.stringify(scores)}`);
const failures = scoreFailures(scores, minimumScores);
if (failures.length) {
  console.error(`PageSpeed scores below budget: ${failures.join(", ")}`);
  process.exit(1);
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

function scoreFailures(scores, minimums) {
  return Object.entries(minimums)
    .filter(([key, minimum]) => (scores[key] ?? 0) < minimum)
    .map(([key, minimum]) => `${key}=${scores[key] ?? 0}<${minimum}`);
}
