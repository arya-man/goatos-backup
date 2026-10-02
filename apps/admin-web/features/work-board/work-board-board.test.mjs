import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./work-board-board.tsx", import.meta.url), "utf8");

test("work-board filter changes clear per-lane per-park cursors", () => {
  assert.match(source, /function setParam\(params: URLSearchParams, pageContract: AdminUiPageContract/);
  assert.match(source, /laneCursorParams\(laneKeys\)/);
  assert.match(source, /laneParkResetParams\(laneKeys, parkKeys\)/);
  assert.match(source, /setParam\(p, pageContract, PARAM_OWNER, next\.find\(\(id\) => id !== selectedOwner\)\)/);
  assert.match(source, /setParam\(p, pageContract, PARAM_MODULE/);
});

test("work-board opened subtasks preserve the selected owner filter", () => {
  const pageSource = readFileSync(new URL("./work-board-page.tsx", import.meta.url), "utf8");
  const modalSource = readFileSync(new URL("./work-board-modal.tsx", import.meta.url), "utf8");
  const subtasksSource = readFileSync(new URL("./work-board-subtasks.tsx", import.meta.url), "utf8");
  const actionsSource = readFileSync(new URL("./actions.ts", import.meta.url), "utf8");
  const apiSource = readFileSync(new URL("../../lib/api/work-board-server.ts", import.meta.url), "utf8");

  assert.match(pageSource, /selectedOwner=\{owner\}/);
  assert.match(modalSource, /selectedOwner\?: string/);
  assert.match(modalSource, /key=\{`\$\{row\.row_key\}:\$\{selectedOwner \?\? ""\}`\}/);
  assert.match(modalSource, /<WorkBoardSubtasks[^>]+selectedOwner=\{selectedOwner\}/);
  assert.match(subtasksSource, /owner: selectedOwner/);
  assert.match(actionsSource, /owner\?: string/);
  assert.match(actionsSource, /owner: input\.owner/);
  assert.match(apiSource, /owner\?: string/);
  assert.match(apiSource, /owner: scope\.owner/);
});

test("work-board owner picker options are not narrowed by the selected owner page rows", () => {
  const pageSource = readFileSync(new URL("./work-board-page.tsx", import.meta.url), "utf8");
  const apiSource = readFileSync(new URL("../../lib/api/work-board-server.ts", import.meta.url), "utf8");

  assert.doesNotMatch(pageSource, /listLeadershipTaskAssignees/);
  assert.match(apiSource, /include_owner_vocabulary: scope\.owner \? "1" : undefined/);
  assert.match(pageSource, /ownerOptionsFromVocabulary\(pageResults\.flatMap/);
  assert.match(pageSource, /result\.data\.owner_vocabulary/);
  assert.match(pageSource, /owners=\{ownerOptions\}/);
  assert.doesNotMatch(pageSource, /owners=\{ownersOnPage\(rows\)\}/);
});

test("work-board page requests the optimized vocabulary shape only for module filters", () => {
  const pageSource = readFileSync(new URL("./work-board-page.tsx", import.meta.url), "utf8");
  const apiSource = readFileSync(new URL("../../lib/api/work-board-server.ts", import.meta.url), "utf8");

  assert.match(apiSource, /client\.request<WorkBoardPageData>\("\/work-board\/page"/);
  assert.match(apiSource, /page_lane: page\.lanes \? \(page\.lanes\.length \? page\.lanes\.join\(","\) : "__none__"\) : undefined/);
  assert.match(apiSource, /include_vocabulary: scope\.modules && scope\.modules\.length \? "1" : undefined/);
  // "Clear all" still reads a SUMMARY-ONLY page per park (no lanes -> page_lane=__none__): the
  // Module menu lists only modules with work and needs the counts to know which.
  assert.match(pageSource, /const pageResults = await runBounded\(pagePlans/);
  assert.match(pageSource, /if \(!noneSelected\) \{\s*\n\s*for \(const lane of laneKeys\)/);
  assert.match(pageSource, /modulesWithWork\(/);
  assert.match(pageSource, /if \(!noneSelected\) \{\s*\n\s*pagePlans\.forEach/);
  assert.match(pageSource, /result\.data\.vocabulary_summary \?\? result\.data\.summary/);
  assert.match(pageSource, /const vocabularySummary = mergeSummaries\(okVocabulary\) \?\? summary/);
});

test("work-board keeps reading summary when every lane cursor is exhausted", () => {
  const pageSource = readFileSync(new URL("./work-board-page.tsx", import.meta.url), "utf8");
  assert.match(pageSource, /if \(raw === LANE_PARK_END\) continue/);
  assert.match(pageSource, /pagePlans\.push\(\{ parkKey: park\.key, openLanes, cursors \}\)/);
  assert.doesNotMatch(pageSource, /if \(noneSelected \|\| openLanes\.length\) pagePlans\.push/);
});

// guard: select-value-sentence-case (J2 P2-12): the Module select shows "All" like every other
// select, not the lowercase remainder of "Module · all".
test("guard: select-value-sentence-case - the module select value starts upper case", async () => {
  const { readFileSync } = await import("node:fs");
  const src = readFileSync(new URL("./work-board-board.tsx", import.meta.url), "utf8");
  assert.match(src, /return rest\.charAt\(0\)\.toUpperCase\(\) \+ rest\.slice\(1\);/);
});

// pr294 L-A10: under "All parks" every park read the full column page and the lanes stacked, so
// To-do held 30 cards (~6,000px) beside short columns. A column page is `limit` cards in total.
test("work board column page is split across parks, and opens at 10 cards", async () => {
  const { readFileSync } = await import("node:fs");
  const page = readFileSync(new URL("./work-board-page.tsx", import.meta.url), "utf8");
  assert.match(page, /const perParkLimit = Math\.max\(1, Math\.ceil\(limit \/ activeParks\.length\)\)/);
  assert.match(page, /getWorkBoardPage\([^)]*\{ limit: perParkLimit,/);
  const service = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");
  assert.match(service, /tableP\("work-board", "Board", [^\n]*"row_key", \[\]int\{10, 25, 50\}\)/);
});
