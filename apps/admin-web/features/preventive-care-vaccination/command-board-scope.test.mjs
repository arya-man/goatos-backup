import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const commandBoardSource = readFileSync(
  new URL("./command-board.tsx", import.meta.url),
  "utf8",
);
const operationsSource = readFileSync(
  new URL("./operations.tsx", import.meta.url),
  "utf8",
);

test("vaccination command board forwards top-bar park scope to the backend read", () => {
  assert.match(
    commandBoardSource,
    /vaccinationCurrentViewScope\(parseScope\(searchParams \?\? \{\}\)\)/,
  );
  assert.match(commandBoardSource, /getVaccinationCommandBoard\(\{ parkId \}\)/);
  assert.match(commandBoardSource, /driveOptions\.some\(\(drive\) => drive\.driveBatchId === selectedDriveBatchId\)/);
  assert.match(commandBoardSource, /selectedDriveBatchId = driveOptions\[0\]\?\.driveBatchId/);
  assert.match(commandBoardSource, /getVaccinationCommandBoard\(\{ driveBatchId: selectedDriveBatchId, parkId \}\)/);
});

test("vaccination operations passes URL search params into the command board", () => {
  assert.match(
    operationsSource,
    /<VaccinationCommandBoard pageContract=\{pageContract\} searchParams=\{sp\} driveBatchId=\{one\(sp, "cb_drive"\)\} \/>/,
  );
});
