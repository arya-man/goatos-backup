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
  assert.doesNotMatch(commandBoardSource, /selectedDriveBatchId = driveOptions\[0\]\?\.driveBatchId/);
  // Selection identity is (batch, park), matching the API's drive-option row grain, and the
  // narrowed read is scoped to the SELECTED DRIVE's park rather than only the top-bar scope.
  assert.match(commandBoardSource, /resolveSelectedDrive\(driveOptions, driveBatchId, driveParkId\)/);
  assert.match(commandBoardSource, /driveBatchId: selectedDrive\.driveBatchId,/);
  assert.match(commandBoardSource, /const selectedParkId = selectedDrive\.parkId \|\| driveParkId \|\| parkId;/);
  assert.match(commandBoardSource, /parkId: selectedParkId,/);
  assert.match(commandBoardSource, /driveParkId=\{selectedParkId\}/);
});

test("a FAILED narrowed drive read never renders as a successful narrow one", () => {
  // The board used to keep the already-loaded ALL-DRIVES payload when the narrowed request failed,
  // while still handing the selected drive to the view: the heading named one operator day and
  // every number under it was the whole programme's.
  assert.doesNotMatch(commandBoardSource, /if \(driveResult\.ok\) result = driveResult;/);
  assert.match(commandBoardSource, /if \(!driveResult\.ok\) return <CommandBoardUnavailable pageContract=\{pageContract\} \/>;/);
  // The unavailable state is the same contract-driven copy the whole-board failure already uses,
  // so the failure is visible rather than silent.
  assert.match(commandBoardSource, /section\.command_board\.unavailable/);

  // The narrowed (park-scoped) response's own driveOptions must not replace the selector catalogue,
  // or choosing one park's drive would erase every other park's drive from the dropdown.
  assert.match(commandBoardSource, /board=\{\{ \.\.\.driveResult\.data, driveOptions \}\}/);
});

test("the drive selector round-trips (batch, park) through the URL", () => {
  assert.match(commandBoardViewSource, /params\.set\("cb_drive", selection\.driveBatchId\)/);
  assert.match(commandBoardViewSource, /params\.set\("cb_drive_park", selection\.parkId\)/);
  assert.match(commandBoardViewSource, /params\.delete\("cb_drive_park"\)/);
  // Both the option value and the React key carry the park, so two parks sharing one batch render
  // as two distinct, separately-selectable rows.
  assert.match(commandBoardViewSource, /value=\{driveBatchId \? driveSelectionValue\(driveBatchId, driveParkId\) : ""\}/);
  assert.match(commandBoardViewSource, /key=\{`\$\{driveSelectionValue\(drive\.batchIds\[0\] \?\? drive\.key, drive\.parkId\)\}\|\$\{drive\.dateKeys\.join\(","\)\}`\}/);
  assert.doesNotMatch(commandBoardViewSource, /key=\{drive\.driveBatchId\} value=\{drive\.driveBatchId\}/);
  // Operator-day option rows come from the already park-specific campaign treatments, not the raw API rows.
  assert.match(commandBoardViewSource, /campaign\.treatments\.map/);
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
  // Three-state cell: the empty guard must also spare a submitted-but-unverified cell, and the
  // colour is pending -> submitted -> clear. Pinning the old two-state literals here would
  // re-assert the defect where a fully submitted park rendered as untouched.
  assert.match(commandBoardViewSource, /pending === 0 && awaiting === 0 && done === 0/);
  assert.match(
    commandBoardViewSource,
    /pending > 0 \? "cbm-pending" : awaiting > 0 \? "cbm-awaiting" : "cbm-clear"/,
  );
  assert.ok(
    commandBoardViewSource.indexOf("command_board.shed_matrix.title")
      < commandBoardViewSource.indexOf("command_board.future_drives.title"),
    "future vaccination drives should render below Vaccine × Shed Status",
  );
});

test("vaccination operations passes URL search params into the command board", () => {
  assert.match(
    operationsSource,
    /<VaccinationCommandBoard pageContract=\{pageContract\} searchParams=\{sp\} driveBatchId=\{one\(sp, "cb_drive"\)\} driveParkId=\{one\(sp, "cb_drive_park"\)\} \/>/,
  );
});
