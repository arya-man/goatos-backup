import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const sectionSource = readFileSync(new URL("./inventory-vaccine-progress.tsx", import.meta.url), "utf8");
const operationsSource = readFileSync(new URL("./operations.tsx", import.meta.url), "utf8");
const serverSource = readFileSync(new URL("../../lib/api/server.ts", import.meta.url), "utf8");

test("vaccination page mounts the PC Care inventory progress section", () => {
  assert.match(operationsSource, /InventoryVaccineProgressSection/);
  assert.match(operationsSource, /pc-care-inventory-progress/);
});

test("inventory progress uses the existing PC Care monitor endpoint", () => {
  assert.match(serverSource, /export async function listPCCareTasks/);
  assert.match(serverSource, /client\.request<PCCareTaskPage>\("\/app\/pc-care\/tasks"/);
  assert.match(sectionSource, /listAllInventoryTasks/);
  assert.match(sectionSource, /category: "inventory_vaccine"/);
});

test("inventory progress fetches all pages and asks for current carry-over work", () => {
  assert.match(sectionSource, /while|for \(let page = 0; page < 20; page \+= 1\)/);
  assert.match(sectionSource, /cursor = result\.data\.next_cursor \?\? ""/);
  assert.match(sectionSource, /items\.push\(\.\.\.result\.data\.items\)/);
  assert.match(sectionSource, /currentOrCarry: true/);
  assert.match(serverSource, /current_or_carry: params\.currentOrCarry \? "true" : undefined/);
});

test("inventory progress filters out old closed and canceled work", () => {
  assert.match(sectionSource, /const ACTIVE_STATES = new Set<InventoryWorkState>\(\["scheduled", "delayed"\]\)/);
  assert.match(sectionSource, /task\.work_state === "completed" && task\.due_business_date === asOf/);
  assert.match(sectionSource, /visibleInventoryRows\(result\.data\.items, asOf\)/);
});

test("inventory progress renders director, verifier, and vaccine dose evidence", () => {
  assert.match(sectionSource, /assignee_names/);
  assert.match(sectionSource, /pending_verification/);
  assert.match(sectionSource, /inventory_requirements/);
  assert.match(sectionSource, /required_doses/);
  assert.match(sectionSource, /vaccine_label/);
});
