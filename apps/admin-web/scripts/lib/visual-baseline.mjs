// visual-baseline.mjs — baseline contract shared by the story and route visual lanes.
//
// Storage decision (2026-09-19): the repo's large-file guard allows 5MB per file but the
// sweep is ~1,000 full-page PNGs (~100MB, churning on every intended change), so PNGs are
// NOT committed. Instead:
//   - apps/admin-web/visual-baselines/<lane>/manifest.json  (COMMITTED) holds, per capture,
//     the size, a sha256 of the pixels and a 256-bit perceptual hash — small, diffable, and
//     enough to fail a run whose picture moved.
//   - <pngDir> (default .codex-goatos-render/admin-web-<lane>-baselines, gitignored) holds
//     the PNGs for pixelmatch, which gives the per-capture DIFF IMAGE a reviewer opens.
//   - apps/admin-web/visual-baselines/<lane>/waivers.json (COMMITTED) lists render-integrity
//     finding keys that are accepted debt.
// Compare order: PNG present -> pixelmatch (precise, diff image). Only the manifest present ->
// perceptual-hash distance (coarser, no diff image, says so). Neither -> failure when
// --require-baseline, otherwise a logged skip. `--update-baseline` rewrites all three.
import { copyFileSync, existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import pixelmatch from "pixelmatch";
import { PNG } from "pngjs";
import { fingerprintPng, hashDistance } from "./render-integrity.mjs";

export class VisualBaseline {
  constructor({ lane, manifestDir, pngDir, diffDir, updateBaseline, requireBaseline, waive = null, maxDiffRatio = 0.01, maxHashDistance = 0.04, relativeToRepo = (p) => p }) {
    // `waive` = { reason } from an explicit --waive "<reason>": the ONLY way a render-integrity
    // finding enters waivers.json. --update-baseline refreshes pixels/hashes and prunes waivers
    // that no longer reproduce; it never adds one (a baseline refresh silently absorbing 18
    // double-skeleton findings is exactly the failure this guards against).
    this.waive = waive && typeof waive.reason === "string" && waive.reason.trim() ? { reason: waive.reason.trim() } : null;
    this.lane = lane;
    this.manifestDir = manifestDir;
    this.manifestPath = join(manifestDir, "manifest.json");
    this.waiversPath = join(manifestDir, "waivers.json");
    this.pngDir = pngDir;
    this.diffDir = diffDir;
    this.updateBaseline = updateBaseline;
    this.requireBaseline = requireBaseline;
    this.maxDiffRatio = maxDiffRatio;
    this.maxHashDistance = maxHashDistance;
    this.rel = relativeToRepo;
    this.manifest = existsSync(this.manifestPath) ? JSON.parse(readFileSync(this.manifestPath, "utf8")) : { lane, updated_at: null, captures: {} };
    this.waived = new Set(existsSync(this.waiversPath) ? JSON.parse(readFileSync(this.waiversPath, "utf8")).waived ?? [] : []);
    this.nextManifest = { lane, updated_at: new Date().toISOString(), captures: {} };
    this.seenWaiverKeys = new Set();
    this.unwaivedKeys = new Set();
    this.visitedContexts = new Set();
    this.stats = { compared_pixels: 0, compared_hash: 0, updated: 0, missing: 0 };
  }

  /** Compare a fresh capture; throws with a diff path when it moved. */
  compare(name, screenshotPath) {
    const actual = PNG.sync.read(readFileSync(screenshotPath));
    const fp = fingerprintPng(actual);
    if (this.updateBaseline) {
      if (this.pngDir) {
        mkdirSync(this.pngDir, { recursive: true });
        copyFileSync(screenshotPath, join(this.pngDir, name));
      }
      this.nextManifest.captures[name] = fp;
      this.stats.updated += 1;
      return { status: "updated" };
    }
    const pngBaseline = this.pngDir ? join(this.pngDir, name) : null;
    if (pngBaseline && existsSync(pngBaseline)) {
      const expected = PNG.sync.read(readFileSync(pngBaseline));
      if (actual.width !== expected.width || actual.height !== expected.height) {
        throw new Error(`dimensions changed: actual ${actual.width}x${actual.height}, baseline ${expected.width}x${expected.height} (${this.rel(pngBaseline)})`);
      }
      const diff = new PNG({ width: actual.width, height: actual.height });
      const diffPixels = pixelmatch(expected.data, actual.data, diff.data, actual.width, actual.height, { threshold: 0.1 });
      const ratio = diffPixels / (actual.width * actual.height);
      this.stats.compared_pixels += 1;
      if (ratio > this.maxDiffRatio) {
        mkdirSync(this.diffDir, { recursive: true });
        const diffPath = join(this.diffDir, name);
        writeFileSync(diffPath, PNG.sync.write(diff));
        throw new Error(`visual diff ${ratio.toFixed(4)} exceeds max ${this.maxDiffRatio}; diff=${this.rel(diffPath)} baseline=${this.rel(pngBaseline)}`);
      }
      return { status: "pixels", ratio };
    }
    const entry = this.manifest.captures?.[name];
    if (entry) {
      this.stats.compared_hash += 1;
      if (entry.width !== fp.width || entry.height !== fp.height) {
        throw new Error(`dimensions changed: actual ${fp.width}x${fp.height}, manifest ${entry.width}x${entry.height} (no local PNG baseline; run --update-baseline on a good build to get pixel diffs)`);
      }
      if (entry.sha256 === fp.sha256) return { status: "hash", distance: 0 };
      const distance = hashDistance(entry.hash, fp.hash);
      if (distance > this.maxHashDistance) {
        throw new Error(`perceptual hash moved ${distance.toFixed(3)} (> ${this.maxHashDistance}); no local PNG baseline so no diff image — actual=${this.rel(screenshotPath)}`);
      }
      return { status: "hash", distance };
    }
    this.stats.missing += 1;
    if (this.requireBaseline) {
      throw new Error(`missing baseline for ${name} in ${this.rel(this.manifestPath)} (run the lane's :update-baseline script if this capture is new/intended)`);
    }
    return { status: "missing" };
  }

  /** Filter integrity findings through the waiver list; returns the unwaived ones. */
  unwaived(context, findings) {
    const out = [];
    this.visitedContexts.add(context);
    for (const finding of findings) {
      const key = `${finding.check}|${context}|${finding.target}`;
      this.seenWaiverKeys.add(key);
      if (this.waived.has(key)) continue;
      this.unwaivedKeys.add(key);
      out.push({ ...finding, key });
    }
    return out;
  }

  /**
   * The waiver list this run would persist. Pure, so it is unit-testable:
   *  - keys for contexts NOT visited by this (possibly --only filtered) run are kept as they are;
   *  - visited contexts keep only the waivers that still reproduce (a fixed finding drops off);
   *  - NEW findings are added ONLY under an explicit --waive reason — never by --update-baseline.
   */
  nextWaivers() {
    const kept = [...this.waived].filter((key) => {
      const context = key.split("|").slice(1, -1).join("|");
      if (!this.visitedContexts.has(context)) return true;
      return this.seenWaiverKeys.has(key);
    });
    const added = this.waive ? [...this.unwaivedKeys] : [];
    return { waived: [...new Set([...kept, ...added])].sort(), added, reason: this.waive?.reason ?? null };
  }

  /** Persist manifest (+ waivers) after an --update-baseline / --waive run. */
  writeUpdated({ waiverNote }) {
    if (!this.updateBaseline && !this.waive) return;
    mkdirSync(this.manifestDir, { recursive: true });
    if (this.updateBaseline) {
      // Keep manifest entries for captures not in this (possibly --only filtered) run.
      const merged = { ...this.manifest.captures, ...this.nextManifest.captures };
      writeFileSync(this.manifestPath, `${JSON.stringify({ ...this.nextManifest, captures: sortKeys(merged) }, null, 2)}\n`);
    }
    const next = this.nextWaivers();
    const changed = next.waived.length !== this.waived.size || next.waived.some((key) => !this.waived.has(key));
    if (!changed) return;
    const reasons = existsSync(this.waiversPath) ? (JSON.parse(readFileSync(this.waiversPath, "utf8")).reasons ?? {}) : {};
    for (const key of next.added) reasons[key] = next.reason;
    for (const key of Object.keys(reasons)) if (!next.waived.includes(key)) delete reasons[key];
    writeFileSync(
      this.waiversPath,
      `${JSON.stringify({ note: waiverNote, updated_at: new Date().toISOString(), waived: next.waived, reasons: sortKeys(reasons) }, null, 2)}\n`,
    );
  }

  summary() {
    return {
      manifest: this.rel(this.manifestPath),
      png_dir: this.pngDir ? this.rel(this.pngDir) : null,
      max_diff_ratio: this.maxDiffRatio,
      max_hash_distance: this.maxHashDistance,
      ...this.stats,
      waived_integrity_keys: this.waived.size,
    };
  }
}

function sortKeys(object) {
  return Object.fromEntries(Object.entries(object).sort(([a], [b]) => a.localeCompare(b)));
}

export function ensureDir(path) {
  mkdirSync(dirname(path), { recursive: true });
}
