import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

const root = new URL("../src", import.meta.url);
const forbidden = ["firebase", "firestore", "bigquery", "gcs", "slack"];
const files = [];
walk(root.pathname);

for (const file of files) {
  const text = readFileSync(file, "utf8");
  for (const marker of forbidden) {
    if (text.toLowerCase().includes(marker)) {
      throw new Error(`${file} contains forbidden direct backend dependency marker: ${marker}`);
    }
  }
}

console.log(`operator-mobile lint checked ${files.length} TypeScript files`);

function walk(dir) {
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    const stat = statSync(path);
    if (stat.isDirectory()) {
      walk(path);
    } else if (entry.endsWith(".ts")) {
      files.push(path);
    }
  }
}
