import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

const token = process.env.GOATOS_BEARER_TOKEN;

if (!token) {
  console.log("token leak guard skipped: GOATOS_BEARER_TOKEN is not set");
  process.exit(0);
}

const roots = [".next/static", ".next/server/app"];
const textExtensions = new Set([
  ".html",
  ".js",
  ".json",
  ".mjs",
  ".rsc",
  ".txt",
  ".map",
  ".css",
]);
const leaks = [];

for (const root of roots) {
  scan(root);
}

if (leaks.length > 0) {
  console.error("GOATOS_BEARER_TOKEN leaked into admin-web client/static build output:");
  for (const file of leaks) {
    console.error(`  ${file}`);
  }
  process.exit(1);
}

console.log("token leak guard passed");

function scan(path) {
  if (!existsSync(path)) return;
  const stat = statSync(path);
  if (stat.isDirectory()) {
    for (const entry of readdirSync(path)) {
      scan(join(path, entry));
    }
    return;
  }
  if (!textExtensions.has(extension(path))) return;
  const body = readFileSync(path, "utf8");
  if (body.includes(token)) {
    leaks.push(path);
  }
}

function extension(path) {
  const index = path.lastIndexOf(".");
  return index >= 0 ? path.slice(index) : "";
}
