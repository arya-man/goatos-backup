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
const commandBoardViewSource = readFileSync(
  new URL("./command-board-view.tsx", import.meta.url),
  "utf8",
);

test("vaccination command board forwards top-bar park scope to the backend read", () => {
  assert.match(
    commandBoardSource,
    /vaccinationCurrentViewScope\(parseScope\(searchParams \?\? \{\}\)\)/,
  );
  assert.match(commandBoardSource, /getVaccinationCommandBoard\(\{ parkId \}\)/);
  assert.match(commandBoardSource, /driveOptions\.some\(\(drive\) => drive\.driveBatchId === selectedDriveBatchId\)/);
  assert.doesNotMatch(commandBoardSource, /selectedDriveBatchId = driveOptions\[0\]\?\.driveBatchId/);
  assert.match(commandBoardSource, /selectedDriveBatchId && !requestedDriveIsInScope/);
  assert.match(commandBoardSource, /getVaccinationCommandBoard\(\{ driveBatchId: selectedDriveBatchId, parkId \}\)/);
});

test("command board defaults to all drives and renders the complete future programme", () => {
  assert.match(commandBoardViewSource, /params\.delete\("cb_drive"\)/);
  assert.match(commandBoardViewSource, /command_board\.filter\.all_common_drives/);
  assert.match(commandBoardViewSource, /scheduledDriveRows\(driveOptions\)/);
  assert.match(commandBoardViewSource, /command_board\.future_drives\.column\.dates/);
  assert.match(commandBoardViewSource, /command_board\.future_drives\.column\.animals/);
  assert.match(commandBoardViewSource, /command_board\.future_drives\.column\.doses/);
  assert.match(commandBoardViewSource, /scheduledDriveCampaigns\(futureDrives\)/);
  assert.match(commandBoardViewSource, /command_board\.filter\.operator_day/);
  assert.match(commandBoardViewSource, /formatDateSpan\(cell\.minAdministeredDate, cell\.maxAdministeredDate\)/);
  assert.match(commandBoardViewSource, /formatDateSpan\(administered\?\.min, administered\?\.max\)/);
  assert.match(commandBoardViewSource, /pending === 0 && done === 0/);
  assert.match(commandBoardViewSource, /hasPending \? "cbm-pending" : "cbm-verified"/);
  assert.ok(
    commandBoardViewSource.indexOf("command_board.shed_matrix.title")
      < commandBoardViewSource.indexOf("command_board.future_drives.title"),
    "future vaccination drives should render below Vaccine × Shed Status",
  );
});

test("vaccination operations passes URL search params into the command board", () => {
  assert.match(
    operationsSource,
    /<VaccinationCommandBoard pageContract=\{pageContract\} searchParams=\{sp\} driveBatchId=\{one\(sp, "cb_drive"\)\} \/>/,
  );
});
