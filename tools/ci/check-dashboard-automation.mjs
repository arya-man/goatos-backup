#!/usr/bin/env node
import { spawnSync } from "node:child_process";

const args = process.argv.slice(2);
const child = spawnSync(process.execPath, ["tools/dashboard-automation/check-static-inventory.mjs", ...args], {
  cwd: new URL("../..", import.meta.url),
  stdio: "inherit"
});

process.exit(child.status ?? 1);
