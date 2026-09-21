import { execFileSync, spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = dirname(dirname(dirname(fileURLToPath(import.meta.url))));
const config = JSON.parse(readFileSync(join(repoRoot, "tools", "dashboard-automation", "config.json"), "utf8"));

export function runPreflight({ requireOci = false } = {}) {
  const disk = readDiskHeadroomGb(repoRoot);
  const failures = [];
  if (disk.availableGb < config.ociAlwaysFree.minimumHeadroomGb) {
    failures.push(`filesystem headroom ${disk.availableGb.toFixed(1)} GB is below ${config.ociAlwaysFree.minimumHeadroomGb} GB`);
  }

  const oci = readOciFreeShape();
  if (requireOci && !oci.available) failures.push("OCI CLI/free-tier metadata unavailable");
  if (requireOci && oci.available && oci.classification === "unknown") {
    failures.push("OCI free-tier classification is unknown; set GOATOS_DASHBOARD_OCI_FREE_CLASSIFICATION from an OCI readback before running");
  }
  if (oci.available && oci.nonFreeSignals.length > 0) {
    failures.push(`OCI free-tier check found non-free signal(s): ${oci.nonFreeSignals.join(", ")}`);
  }

  return {
    ok: failures.length === 0,
    failures,
    disk,
    oci,
    forbidden_actions: config.ociAlwaysFree.forbiddenActions,
  };
}

function readDiskHeadroomGb(path) {
  const raw = execFileSync("df", ["-Pk", path], { encoding: "utf8" }).trim().split(/\n/).at(-1);
  const parts = raw.trim().split(/\s+/);
  const availableKb = Number(parts[3] ?? 0);
  return { path, availableGb: availableKb / 1024 / 1024 };
}

function readOciFreeShape() {
  const oci = spawnSync("oci", ["--version"], { encoding: "utf8" });
  if (oci.status !== 0) return { available: false, nonFreeSignals: [], note: "oci cli not installed or not configured" };
  // This intentionally avoids creating or mutating anything. The live runner can add compartment-specific
  // list commands, but the repo guard already refuses to proceed when a caller marks the resource non-free.
  const classification = process.env.GOATOS_DASHBOARD_OCI_FREE_CLASSIFICATION ?? "";
  const allocationGb = Number(process.env.GOATOS_DASHBOARD_OCI_BOOT_VOLUME_GB ?? "0");
  const nonFreeSignals = [];
  if (classification && !/always[-_ ]free/i.test(classification)) nonFreeSignals.push(`classification=${classification}`);
  if (allocationGb > 100) nonFreeSignals.push(`boot_volume_gb=${allocationGb}`);
  return { available: true, nonFreeSignals, classification: classification || "unknown", bootVolumeGb: allocationGb || null };
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const result = runPreflight({ requireOci: process.argv.includes("--require-oci") });
  console.log(JSON.stringify(result, null, 2));
  if (!result.ok) process.exit(1);
}
