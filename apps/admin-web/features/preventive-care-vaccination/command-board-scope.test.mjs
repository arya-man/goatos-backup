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
  // The top-bar park reaches the backend read. Asserted over EVERY read below rather than against
  // one exact call shape, because the read now also carries the selected drive and its park.
  assert.doesNotMatch(commandBoardSource, /selectedDriveBatchId = driveOptions\[0\]\?\.driveBatchId/);
  // Selection identity is (batch, park), matching the API's drive-option row grain, and the
  // narrowed read is scoped to the SELECTED DRIVE's park rather than only the top-bar scope.
  assert.match(commandBoardSource, /resolveSelectedDrive\(driveOptions, driveBatchId, driveParkId\)/);
  assert.match(commandBoardSource, /driveBatchId: selectedDrive\.driveBatchId,/);
  // The park the sections are narrowed to is the one the CATALOGUE resolved for the selected batch,
  // falling back to the URL and then the top bar. A board narrowed to the wrong park is a wrong
  // board, so this is asserted exactly.
  assert.match(commandBoardSource, /const resolvedParkId = selectedDrive\.parkId \|\| driveParkId \|\| parkId;/);
  // ONE request serves the narrowed sections AND the full picker: the drive's park rides as its own
  // parameter while parkId keeps scoping the catalogue. The previous two-request shape awaited a
  // wide read purely to keep its picker and rebuilt the endpoint's most expensive query twice.
  assert.match(commandBoardSource, /driveParkId: driveBatchId \? driveParkId : undefined,/);
  assert.doesNotMatch(commandBoardSource, /Promise\.all/);
  // The picker's scope is the TOP BAR's park on every call, never the selected drive's — otherwise
  // choosing one park's drive deletes the other parks' drives from the dropdown.
  const reads = commandBoardSource.match(/getVaccinationCommandBoard\(\{[\s\S]*?\}\)/g) ?? [];
  assert.ok(reads.length >= 1, "the board must read the command endpoint");
  for (const read of reads) {
    assert.match(read, /parkId,/, `every command-board read keeps the top-bar park scope: ${read}`);
  }
  assert.match(commandBoardSource, /driveParkId=\{resolvedParkId\}/);
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
  assert.match(commandBoardViewSource, /const selectedDrive = optimisticDrive\?\.from === currentSearch/);
  assert.match(commandBoardViewSource, /driveSelectionValue\(driveBatchId, driveParkId\)/);
  assert.match(commandBoardViewSource, /value=\{selectedDrive\}/);
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
