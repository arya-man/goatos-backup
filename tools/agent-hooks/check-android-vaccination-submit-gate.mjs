#!/usr/bin/env node
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const repo = path.resolve(new URL("../..", import.meta.url).pathname);
const target = path.join(
  repo,
  "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/ScanViewModel.kt",
);

function checkSource(source, label) {
  const reason = source.indexOf('"proof_rescan"');
  if (reason < 0) {
    throw new Error(`${label}: missing proof_rescan branch in ScanViewModel`);
  }
  const nextProof = source.indexOf("requestGoatProof(row)", reason);
  if (nextProof < 0) {
    throw new Error(`${label}: proof_rescan branch no longer requests goat proof`);
  }
  const repair = source.indexOf("recordRosterScan(row, tag, capturedAtMs)", reason);
  if (repair < 0 || repair > nextProof) {
    throw new Error(
      `${label}: proof_rescan must repair the durable roster scan before proof capture; ` +
        "otherwise submit summary can show proof-ready > scanned and disable Submit",
    );
  }

  const testFile = path.join(
    repo,
    "apps/goatos-android/app/src/test/kotlin/sg/mesha/goatos/viewmodel/ScanViewModelTest.kt",
  );
  const testSource = fs.readFileSync(testFile, "utf8");
  if (
    !testSource.includes("rescan of proof missing goat repairs durable roster capture") ||
    !testSource.includes("proof rescan must repair the durable scan capture idempotently")
  ) {
    throw new Error(`${label}: missing regression test for proof_rescan durable scan repair`);
  }
}

function selfTest() {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "goatos-vax-submit-gate-"));
  const bad = `
    recordScanAttempt(tag, row, RfidScanAttemptOutcome.ACCEPTED, tagRole, "proof_rescan", capturedAtMs)
    requestGoatProof(row)
  `;
  try {
    checkSource(bad, "self-test bad fixture");
    throw new Error("self-test failed: bad fixture passed");
  } catch (err) {
    if (!String(err.message).includes("must repair the durable roster scan")) throw err;
  }
  const good = `
    recordScanAttempt(tag, row, RfidScanAttemptOutcome.ACCEPTED, tagRole, "proof_rescan", capturedAtMs)
    recordRosterScan(row, tag, capturedAtMs)
    requestGoatProof(row)
  `;
  const testFile = path.join(
    repo,
    "apps/goatos-android/app/src/test/kotlin/sg/mesha/goatos/viewmodel/ScanViewModelTest.kt",
  );
  const original = fs.readFileSync(testFile, "utf8");
  try {
    fs.writeFileSync(testFile, original);
    checkSource(good, "self-test good fixture");
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
}

if (process.argv.includes("--self-test")) {
  selfTest();
  console.log("android vaccination submit gate guard self-test: PASS");
} else {
  checkSource(fs.readFileSync(target, "utf8"), target);
  console.log("android vaccination submit gate guard: PASS");
}
