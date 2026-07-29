#!/usr/bin/env node

// check-android-row-action-scope.mjs — Android row-action concurrency guard.
//
// Incident class: free-flow individual weighing rendered one card per animal,
// but the row Save path used the screen-wide `actionInFlight` gate. A slow save
// on animal A disabled/ignored animal B's Save, which felt random in the field.
//
// Rule: actions attached to a repeated animal row must be scoped by row/animal id.
// Screen-wide busy gates are only for screen-wide actions such as final submit.
//
// Current static coverage is intentionally narrow and fail-closed for the known
// production path:
//   1. WeighingViewModel.recordIndividual(animalId, rawWeight) must call
//      recordIndividualRow(..., useGlobalBusyGate = false).
//   2. Weighing row canSaveWeight must not depend on global `busy`/
//      `actionInFlight`; it must depend on updatingAnimalIds/animal id.
//   3. A regression test for overlapping free-flow saves must exist.

import { mkdirSync, mkdtempSync, rmSync, writeFileSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import process from "node:process";

const VIEW_MODEL = "apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/WeighingViewModel.kt";
const TEST = "apps/goatos-android/app/src/test/kotlin/sg/mesha/goatos/viewmodel/WeighingViewModelTest.kt";

function read(root, rel) {
  return readFileSync(resolve(root, rel), "utf8");
}

function write(root, rel, text) {
  const path = resolve(root, rel);
  mkdirSync(resolve(path, ".."), { recursive: true });
  writeFileSync(path, text, "utf8");
}

function findFunctionBody(source, signatureRe) {
  const match = signatureRe.exec(source);
  if (!match) return null;
  const open = source.indexOf("{", match.index);
  if (open < 0) return null;
  let depth = 0;
  let inStr = null;
  for (let i = open; i < source.length; i++) {
    const c = source[i];
    if (inStr) {
      if (c === "\\") {
        i++;
        continue;
      }
      if (c === inStr) inStr = null;
      continue;
    }
    if (c === '"' || c === "'") {
      inStr = c;
      continue;
    }
    if (c === "{") depth++;
    if (c === "}") {
      depth--;
      if (depth === 0) return source.slice(open + 1, i);
    }
  }
  return null;
}

function validate(root) {
  const errors = [];
  let vm = "";
  let test = "";
  try {
    vm = read(root, VIEW_MODEL);
  } catch {
    return [`missing ${VIEW_MODEL}`];
  }
  try {
    test = read(root, TEST);
  } catch {
    errors.push(`missing ${TEST}`);
  }

  const rowEntry = findFunctionBody(
    vm,
    /fun\s+recordIndividual\s*\(\s*animalId\s*:\s*String\s*,\s*rawWeight\s*:\s*String\s*\)/,
  );
  if (!rowEntry) {
    errors.push(`${VIEW_MODEL}: missing row-scoped recordIndividual(animalId, rawWeight) entrypoint`);
  } else {
    if (/if\s*\(\s*actionInFlight\.value\s*\)\s*return/.test(rowEntry)) {
      errors.push(`${VIEW_MODEL}: row-scoped recordIndividual(animalId, rawWeight) must not gate on actionInFlight`);
    }
    if (!/recordIndividualRow\s*\([^)]*useGlobalBusyGate\s*=\s*false/s.test(rowEntry)) {
      errors.push(`${VIEW_MODEL}: row-scoped recordIndividual(animalId, rawWeight) must call recordIndividualRow with useGlobalBusyGate = false`);
    }
    if (!/animalId\s+in\s+updatingWeightAnimalIds\.value/.test(rowEntry)) {
      errors.push(`${VIEW_MODEL}: row-scoped recordIndividual(animalId, rawWeight) must guard duplicate saves per animal id`);
    }
  }

  const rowWorker = findFunctionBody(
    vm,
    /private\s+fun\s+recordIndividualRow\s*\([^)]*useGlobalBusyGate\s*:\s*Boolean\s*=\s*true/s,
  );
  if (!rowWorker) {
    errors.push(`${VIEW_MODEL}: recordIndividualRow must keep an explicit useGlobalBusyGate parameter`);
  } else {
    if (!/if\s*\(\s*useGlobalBusyGate\s*&&\s*actionInFlight\.value\s*\)\s*return/.test(rowWorker)) {
      errors.push(`${VIEW_MODEL}: recordIndividualRow global busy check must be conditional on useGlobalBusyGate`);
    }
    if (!/if\s*\(\s*useGlobalBusyGate\s*\)\s*actionInFlight\.value\s*=\s*true/.test(rowWorker)) {
      errors.push(`${VIEW_MODEL}: recordIndividualRow must set actionInFlight only when useGlobalBusyGate is true`);
    }
    if (!/if\s*\(\s*useGlobalBusyGate\s*\)\s*actionInFlight\.value\s*=\s*false/.test(rowWorker)) {
      errors.push(`${VIEW_MODEL}: recordIndividualRow must clear actionInFlight only when useGlobalBusyGate is true`);
    }
    if (!/updatingWeightAnimalIds\.value\s*=\s*updatingWeightAnimalIds\.value\s*\+\s*row\.animalId/.test(rowWorker)) {
      errors.push(`${VIEW_MODEL}: recordIndividualRow must mark the active animal id as updating`);
    }
  }

  const canSaveMatch = /canSaveWeight\s*=\s*([\s\S]*?)\n\s*weightSaved\s*=/.exec(vm);
  if (!canSaveMatch) {
    errors.push(`${VIEW_MODEL}: missing canSaveWeight assignment for weighing rows`);
  } else {
    const expr = canSaveMatch[1];
    if (/\bbusy\b|actionInFlight/.test(expr)) {
      errors.push(`${VIEW_MODEL}: row canSaveWeight must not depend on global busy/actionInFlight`);
    }
    if (!/updatingAnimalIds/.test(expr) || !/row\.animalId/.test(expr)) {
      errors.push(`${VIEW_MODEL}: row canSaveWeight must be scoped by row.animalId in updatingAnimalIds`);
    }
  }

  if (test) {
    const requiredTestName = "free flow saving one animal does not block saving the next animal";
    for (const token of [
      requiredTestName,
      "CompletableDeferred<AppResult<IndividualWeighingDraft>>",
      "assertTrue(firstPendingState.getValue(SECOND_TAG).canSaveWeight)",
      "assertEquals(listOf(TEST_TAG, SECOND_TAG), repository.captures.map { it.animalId })",
    ]) {
      if (!test.includes(token)) errors.push(`${TEST}: missing overlapping row-save regression token: ${token}`);
    }
  }

  return errors;
}

function selfTest() {
  const root = mkdtempSync(resolve(tmpdir(), "android-row-action-scope-"));
  try {
    const goodVm = `
class WeighingViewModel {
  fun recordIndividual(animalId: String, rawWeight: String) {
    if (animalId in updatingWeightAnimalIds.value) return
    recordIndividualRow(key, row, useGlobalBusyGate = false)
  }
  private fun recordIndividualRow(
    key: String,
    row: WeighingRosterRowEntity,
    useGlobalBusyGate: Boolean = true,
  ) {
    if (useGlobalBusyGate && actionInFlight.value) return
    if (row.animalId in updatingWeightAnimalIds.value) return
    if (useGlobalBusyGate) actionInFlight.value = true
    updatingWeightAnimalIds.value = updatingWeightAnimalIds.value + row.animalId
    try {} finally { if (useGlobalBusyGate) actionInFlight.value = false }
  }
  fun rows() = WeighingRosterUiRow(
    canSaveWeight = row.animalId !in updatingAnimalIds &&
      weight.toDoubleOrNull()?.let { it > 0.0 } == true,
    weightSaved = false,
  )
}`;
    const goodTest = `
class WeighingViewModelTest {
  fun \`free flow saving one animal does not block saving the next animal\`() {
    val firstGate = CompletableDeferred<AppResult<IndividualWeighingDraft>>()
    assertTrue(firstPendingState.getValue(SECOND_TAG).canSaveWeight)
    assertEquals(listOf(TEST_TAG, SECOND_TAG), repository.captures.map { it.animalId })
  }
}`;
    write(root, VIEW_MODEL, goodVm);
    write(root, TEST, goodTest);
    const passing = validate(root);
    if (passing.length) throw new Error(`self-test complete fixture should pass: ${passing.join("; ")}`);

    write(root, VIEW_MODEL, goodVm.replace("recordIndividualRow(key, row, useGlobalBusyGate = false)", "recordIndividualRow(key, row)"));
    const missingFalse = validate(root);
    if (!missingFalse.some((x) => x.includes("useGlobalBusyGate = false"))) {
      throw new Error("self-test did not catch row entrypoint using the default global gate");
    }

    write(root, VIEW_MODEL, goodVm.replace("row.animalId !in updatingAnimalIds &&", "!busy &&"));
    const globalBusyCanSave = validate(root);
    if (!globalBusyCanSave.some((x) => x.includes("row canSaveWeight must not depend on global busy"))) {
      throw new Error("self-test did not catch canSaveWeight depending on global busy");
    }
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
  console.log("check-android-row-action-scope self-test: ok");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const errors = validate(process.cwd());
if (errors.length) {
  console.error("check-android-row-action-scope failed:");
  for (const error of errors) console.error(`- ${error}`);
  process.exit(1);
}
console.log("check-android-row-action-scope: ok");
