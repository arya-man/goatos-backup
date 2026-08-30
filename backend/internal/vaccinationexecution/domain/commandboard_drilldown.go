package domain

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// Command-board drilldowns.
//
// These four lists used to be computed EAGERLY inside GET /vaccination/command and shipped on
// first paint, nested inside the tiles and matrix cells they explain. That is what made the
// endpoint fail: on the staging-scale tenant the four drilldown statements cost ~5.3s of the
// endpoint's ~8.6s, and the closed-without-dose statement alone exhausted the 15s pool timeout
// (`closed-without-dose rows: timeout: context deadline exceeded`), which returned a 500 and left
// the CEO board reading "Unable to load command board".
//
// The cost was never necessary. A drilldown is what a reader asks for AFTER reading a tile, for
// ONE cell — "which animals are behind on ET+TT in Sumathi 1" — so computing every cell's list on
// every render pays for hundreds of answers to fetch one. Worse, each list was computed
// TENANT-WIDE and then capped in Go, so the database sorted an estimated 57k rows to return 500,
// and on the live tenant shedVaccineAnimalSQL spent 2.4s to return zero rows.
//
// Each drilldown is now its own endpoint, REQUIRES the cell it explains, and is keyset-paginated:
// the scope predicate is applied before the work, not after it. The counts stay on the board,
// whole-scope and authoritative; only the evidence lists moved.
//
// The tile COUNT and the drilldown LIST must keep describing the same animals. Every drilldown
// statement therefore reuses the SAME predicate its board aggregate uses, byte for byte, and
// commandboard_query_plan_test.go asserts the two agree on live data rather than trusting the
// comment.

// CommandBoardDrilldownDefaultLimit is the page size used when a caller names none. It is the size
// of a drawer, not of a report: the reader is looking at one cell.
const CommandBoardDrilldownDefaultLimit = 50

// CommandBoardDrilldownMaxLimit bounds what a caller may ask for in one page. Paging is the
// contract; a caller wanting more takes another page rather than turning a drawer read back into
// the tenant-wide scan this split exists to remove.
const CommandBoardDrilldownMaxLimit = 200

// CommandBoardDrilldownQuery is the shared scope of every command-board drilldown: the same
// tenant/drive/park filter the board itself was rendered under, plus one page.
//
// AsOf is carried because the overdue/behind predicates are business-DATE comparisons against it.
// A drilldown resolved against a different as-of than the tile that raised it would list a
// different animal set than the number the reader clicked.
type CommandBoardDrilldownQuery struct {
	TenantID     string
	DriveBatchID *string
	ParkID       *string
	AsOf         time.Time
	Limit        int
	Cursor       string
}

// Normalized clamps the page size into the published bounds. A caller naming 0 gets the default
// rather than an unbounded read.
func (q CommandBoardDrilldownQuery) Normalized() CommandBoardDrilldownQuery {
	if q.Limit <= 0 {
		q.Limit = CommandBoardDrilldownDefaultLimit
	}
	if q.Limit > CommandBoardDrilldownMaxLimit {
		q.Limit = CommandBoardDrilldownMaxLimit
	}
	return q
}

// CommandBoardShedVaccineAnimalsQuery names ONE shed-vaccine cell. The cell keys are required, not
// optional filters: without them this is the tenant-wide scan that took 2.4s to return nothing.
type CommandBoardShedVaccineAnimalsQuery struct {
	CommandBoardDrilldownQuery
	ShedID         string
	PartitionLabel string
	VaccineCode    string
}

// CommandBoardCohortCellQuery names ONE cohort matrix cell (park x stage x sex x dose). DoseCodes
// is the set of dose codes that fold into the cell's single displayed vaccine label — the board
// collapses several dose codes onto one column, so the drilldown must ask for all of them or it
// will under-report the cell it was opened from.
type CommandBoardCohortCellQuery struct {
	CommandBoardDrilldownQuery
	CohortParkID    string
	ManagementStage string
	Sex             string
	DoseCodes       []string
}

// CommandBoardClosedWithoutDosePage is one page of the ClosedWithoutDose tile's animals.
type CommandBoardClosedWithoutDosePage struct {
	Animals    []CommandBoardClosedWithoutDoseAnimal `json:"animals"`
	NextCursor string                                `json:"nextCursor,omitempty"`
}

// CommandBoardShedVaccineAnimalsPage is one page of a shed-vaccine cell's flagged animals, plus
// the shed's proof videos for the days those animals were recorded.
//
// The videos ride along rather than getting a fifth endpoint because they are the SAME drawer: a
// verifier reading "12 animals awaiting verification" reaches for the footage in the same glance.
// They are cheap once the shed is known — the expensive version was the eager one, which had to
// derive the shed set from a tenant-wide behind-animal scan first.
type CommandBoardShedVaccineAnimalsPage struct {
	Animals     []CommandBoardShedVaccineAnimal `json:"animals"`
	ProofVideos []CommandBoardShedVideo         `json:"proofVideos"`
	NextCursor  string                          `json:"nextCursor,omitempty"`
}

// CommandBoardCohortExceptionsPage is one page of a cohort cell's missing-prior-dose animals.
type CommandBoardCohortExceptionsPage struct {
	Animals    []CommandBoardCohortAnimal `json:"animals"`
	NextCursor string                     `json:"nextCursor,omitempty"`
}

// CommandBoardCohortDaysPage is a cohort cell's administered-day split. It carries no cursor: the
// row count is bounded by the days in the drive window, which is a drawer-sized list by
// construction, and paginating a bar chart would only let a caller render half of one.
type CommandBoardCohortDaysPage struct {
	Days []CommandBoardCohortDay `json:"days"`
}

// maxCommandBoardCursorBytes caps the decoded cursor payload so a hostile client cannot force an
// oversized JSON parse. A legitimate cursor is a timestamp or a display id plus a UUID.
const maxCommandBoardCursorBytes = 256

// CommandBoardAnimalCursor is the keyset position for the display-id-ordered drilldowns
// (closed-without-dose, cohort exceptions). display_id is not unique on its own, so goat_id breaks
// ties and makes the ordering total — a keyset on a non-total order silently skips or repeats rows
// at every page boundary.
type CommandBoardAnimalCursor struct {
	DisplayID string `json:"displayId"`
	GoatID    string `json:"goatId"`
}

func EncodeCommandBoardAnimalCursor(cursor CommandBoardAnimalCursor) (string, error) {
	if cursor.GoatID == "" {
		return "", errors.New("command board cursor requires goat_id")
	}
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeCommandBoardAnimalCursor(raw string) (CommandBoardAnimalCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return CommandBoardAnimalCursor{}, err
	}
	if len(decoded) > maxCommandBoardCursorBytes {
		return CommandBoardAnimalCursor{}, errors.New("invalid command board cursor: payload too large")
	}
	var cursor CommandBoardAnimalCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return CommandBoardAnimalCursor{}, err
	}
	// goat_id is cast to ::uuid in the keyset SQL — validate here so a malformed cursor is a 400
	// (invalid_cursor) rather than a DB 500 (invalid input syntax for type uuid).
	if !uuidutil.IsUUIDString(cursor.GoatID) {
		return CommandBoardAnimalCursor{}, errors.New("invalid command board cursor")
	}
	return cursor, nil
}

// CommandBoardDueCursor is the keyset position for the due-date-ordered shed-vaccine drilldown.
//
// DueAt is a pointer because due_at is nullable and the list is ordered NULLS LAST. The SQL
// compares COALESCE(due_at, 'infinity'), so a nil DueAt here means "past every dated row", which
// is exactly where the NULLS LAST tail sits.
type CommandBoardDueCursor struct {
	DueAt  *time.Time `json:"dueAt,omitempty"`
	GoatID string     `json:"goatId"`
}

func EncodeCommandBoardDueCursor(cursor CommandBoardDueCursor) (string, error) {
	if cursor.GoatID == "" {
		return "", errors.New("command board cursor requires goat_id")
	}
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeCommandBoardDueCursor(raw string) (CommandBoardDueCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return CommandBoardDueCursor{}, err
	}
	if len(decoded) > maxCommandBoardCursorBytes {
		return CommandBoardDueCursor{}, errors.New("invalid command board cursor: payload too large")
	}
	var cursor CommandBoardDueCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return CommandBoardDueCursor{}, err
	}
	if !uuidutil.IsUUIDString(cursor.GoatID) {
		return CommandBoardDueCursor{}, errors.New("invalid command board cursor")
	}
	return cursor, nil
}

// CommandBoardDriveOptionsPageSize is how many drives the BOARD itself carries.
//
// The picker used to ship its whole catalogue on first paint -- 200 fully decorated drives, which
// on the staging-scale tenant was 448ms and 753 KB, more than the endpoint's entire 512 KB budget
// and the single largest thing left on its critical path. A dropdown needs enough rows to open
// against, not the catalogue; the rest is fetched from CommandBoardDriveOptions as the reader
// scrolls or searches.
//
// DriveOptionsTruncated on the response still says the bound was hit, so the UI keeps saying "more
// drives exist" instead of lying by omission -- that flag is now the normal case rather than the
// rare one.
const CommandBoardDriveOptionsPageSize = 20

// CommandBoardDriveOptionsMaxLimit bounds what the lazy picker endpoint will serve in one page.
const CommandBoardDriveOptionsMaxLimit = 100

// CommandBoardDriveOptionsQuery is one page of the drive picker.
type CommandBoardDriveOptionsQuery struct {
	TenantID string
	ParkID   *string
	Limit    int
	Cursor   string
}

// CommandBoardDriveOptionsPage is one page of drives plus the position to resume from.
type CommandBoardDriveOptionsPage struct {
	Options    []CommandBoardDriveOption `json:"options"`
	NextCursor string                    `json:"nextCursor,omitempty"`
}

// CommandBoardDriveCursor is the keyset position in the picker's mixed-direction sort
// (status rank ascending, planned date and window descending, then batch and park ascending).
//
// PlannedDate and WindowStart are carried as TEXT, and may be the literal "-infinity". The sort
// COALESCEs a missing date to -infinity so undated drives sort last within their status rank; a
// cursor that could not express that value would skip the whole undated tail. They are bound
// straight into the statement's ::date and ::timestamptz casts, so Postgres parses both spellings.
//
// ParkIsNull is a separate key from ParkName because the sort puts park-less rows after named ones
// and an empty name is not the same fact as no park.
type CommandBoardDriveCursor struct {
	StatusRank  int    `json:"statusRank"`
	PlannedDate string `json:"plannedDate"`
	WindowStart string `json:"windowStart"`
	BatchID     string `json:"batchId"`
	ParkIsNull  bool   `json:"parkIsNull"`
	ParkName    string `json:"parkName"`
}

// maxCommandBoardDriveCursorBytes caps the decoded payload. A legitimate cursor is a UUID, two
// timestamps and a park name.
const maxCommandBoardDriveCursorBytes = 512

func EncodeCommandBoardDriveCursor(cursor CommandBoardDriveCursor) (string, error) {
	if cursor.BatchID == "" {
		return "", errors.New("command board drive cursor requires batch_id")
	}
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DecodeCommandBoardDriveCursor(raw string) (CommandBoardDriveCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return CommandBoardDriveCursor{}, err
	}
	if len(decoded) > maxCommandBoardDriveCursorBytes {
		return CommandBoardDriveCursor{}, errors.New("invalid command board drive cursor: payload too large")
	}
	var cursor CommandBoardDriveCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return CommandBoardDriveCursor{}, err
	}
	// batch_id is cast to ::uuid in the keyset SQL — validate here so a malformed cursor is a 400
	// (invalid_cursor) rather than a DB 500 (invalid input syntax for type uuid).
	if !uuidutil.IsUUIDString(cursor.BatchID) {
		return CommandBoardDriveCursor{}, errors.New("invalid command board drive cursor")
	}
	// The date keys are bound into ::date / ::timestamptz casts. Refuse anything that is not a date
	// Postgres will accept, so a hostile cursor cannot reach the planner as a cast error.
	if !isCommandBoardCursorDate(cursor.PlannedDate) || !isCommandBoardCursorTimestamp(cursor.WindowStart) {
		return CommandBoardDriveCursor{}, errors.New("invalid command board drive cursor")
	}
	return cursor, nil
}

func isCommandBoardCursorDate(v string) bool {
	if v == "-infinity" {
		return true
	}
	_, err := time.Parse("2006-01-02", v)
	return err == nil
}

func isCommandBoardCursorTimestamp(v string) bool {
	if v == "-infinity" {
		return true
	}
	_, err := time.Parse(time.RFC3339, v)
	return err == nil
}
