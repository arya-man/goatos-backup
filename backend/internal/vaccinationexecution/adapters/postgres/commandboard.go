package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	oploc "github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	vaccinatdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// commandBoardConcurrencyBudget bounds the connections the command board's endpoints may hold at
// once on this instance, across the board, cohort matrix, shed-dose grid and drive picker.
//
// ONE CONNECTION PER ENDPOINT. Each endpoint now sends all of its statements as one pgx.Batch on
// one connection (commandBoardBatch) and holds ONE slot for it. The budget used to be spent per
// STATEMENT: admin-web fires the board (6 sections), cohort matrix (2), shed-dose grid (1) and
// drive picker (1) together, so one first paint queued ~10 statements on 6 slots and serialised
// the slow ones behind each other -- stg p50 1.8 s against ~220 ms of clone DB time
// (docs/perf/2026-09-24-stg-latency/audit/part-vaccination.md root 3). Four slots admit a whole
// first paint in one wave and leave six of the default GOATOS_PG_MAX_CONNS=10 for every other
// caller. Only CACHE MISSES reach it: the read cache (vaccinationCached) single-flights identical
// keys, so concurrent readers of one board share one load and one slot.
const commandBoardConcurrencyBudget = 4

// commandBoardSection holds one slot from the SHARED command-board budget for exactly as long as
// fn runs. Acquire honours ctx, so a cancelled request stops waiting rather than piling up behind
// the pool.
func (r *Repository) commandBoardSection(ctx context.Context, fn func() error) func() error {
	return func() error {
		if r.commandBoardSlots != nil {
			if err := r.commandBoardSlots.Acquire(ctx, 1); err != nil {
				return err
			}
			defer r.commandBoardSlots.Release(1)
		}
		return fn()
	}
}

// queueBound queues a statement whose placeholders were checked against its arguments
// (sqlbind), so a batch cannot silently shift a parameter.
func queueBound(batch *pgx.Batch, sql string, args ...any) {
	bound := sqlbind.MustBind(sql, args...)
	batch.Queue(bound.SQL(), bound.Args()...)
}

// commandBoardBatch sends batch as ONE round trip on ONE pooled connection under ONE budget slot,
// and hands the results to read in queue order. Statements run back to back server-side; the
// batch is one implicit transaction, so a failed statement aborts the ones queued after it.
func (r *Repository) commandBoardBatch(ctx context.Context, batch *pgx.Batch, read func(pgx.BatchResults) error) error {
	return r.commandBoardSection(ctx, func() error {
		br := r.pool.SendBatch(ctx, batch)
		readErr := read(br)
		closeErr := br.Close()
		if readErr != nil {
			return readErr
		}
		return closeErr
	})()
}

// VaccinationCommandBoard returns the CEO closure view's FIRST PAINT: KPIs, shed vaccine matrix,
// weekly given, verification queue and a small first page of the drive picker.
//
// The cohort matrix and the shed x dose grid are NOT here. They are their own sections
// (CommandBoardCohortMatrix, CommandBoardShedDoseMatrix), because each alone held this endpoint
// over its non-relaxable 300ms budget.
//
// SUMMARY-FIRST. This endpoint used to stitch FOURTEEN sequential live reads into one SSR
// response, five of which computed per-animal DRILLDOWN lists tenant-wide and eagerly. On the
// staging-scale tenant (~71k obligation_instances, 5.8k completions, 1.6k live goats) that cost
// ~8.6s of server-side SQL against a 15s pool timeout, and in staging it did not merely run slow:
// the closed-without-dose statement exhausted the deadline outright
// ("closed-without-dose rows: timeout: context deadline exceeded"), the endpoint returned 500, and
// the board rendered "Unable to load command board".
//
// Two changes, in the order that matters:
//
//  1. THE DRILLDOWNS LEFT. The five per-animal/per-day lists (closed-without-dose animals,
//     shed-vaccine flagged animals, cohort exceptions, cohort administered days, proof videos)
//     were ~5.3s of the ~8.6s -- 62% of the endpoint spent computing evidence for cells nobody had
//     opened. They are now their own keyset-paginated endpoints, each REQUIRING the cell it
//     explains, so the database does a drawer's work instead of the tenant's. See
//     commandboard_drilldown_sql.go. The board keeps every COUNT those lists sat under: the
//     numbers are the board, the lists were never first paint.
//
//  2. WHAT REMAINS IS ONE BATCH. The six summary sections are sent as one pgx.Batch on one
//     connection (commandBoardBatch) and assembled deterministically after. Once the canceled
//     obligations left the folds (migration 000410's partial index) each statement is tens of
//     milliseconds, so one connection beats six that queue on the shared budget.
//
// SECTION-LEVEL DEGRADATION. KPIs and the drive picker are REQUIRED: a board with no numbers is
// the blank failure this change exists to remove, and a board whose picker is missing strands a
// reader on whichever drive they last chose. Every other section is OPTIONAL -- if it fails, the
// section is named in UnavailableSections and the rest of the board still renders. A verification
// queue that times out must not delete the KPI row from the CEO's screen; that is precisely the
// all-or-nothing coupling that turned one slow statement into a blank page.
func (r *Repository) VaccinationCommandBoard(ctx context.Context, q domain.CommandBoardQuery) (domain.CommandBoardResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}

	resp := domain.CommandBoardResponse{
		Source:            domain.SourceAPI,
		WeeklyGiven:       []domain.WeeklyGivenRow{},
		VerificationQueue: []domain.VerificationQueueRow{},
	}

	// TWO park scopes, because the board and its drive picker answer different questions.
	//
	// catalogParkID scopes the PICKER and is the caller's own park scope: the list of drives that
	// can be chosen must not shrink to the park of the drive already chosen, or selecting one park's
	// drive deletes every other park's drive from the dropdown and strands the reader there.
	//
	// parkID scopes the BOARD SECTIONS and additionally honours DriveParkID: a batch can span parks,
	// so "this drive" means one park's operator day, and its numbers must be that park's.
	var catalogParkID *string
	if q.ParkID != nil && strings.TrimSpace(*q.ParkID) != "" {
		catalogParkID = q.ParkID
	}
	parkID := catalogParkID
	if q.DriveParkID != nil && strings.TrimSpace(*q.DriveParkID) != "" {
		parkID = q.DriveParkID
	}
	boardParkID := ""
	if parkID != nil {
		boardParkID = strings.TrimSpace(*parkID)
	}
	catalogScopeID := ""
	if catalogParkID != nil {
		catalogScopeID = strings.TrimSpace(*catalogParkID)
	}
	driveBatchID := ""
	if q.DriveBatchID != nil {
		driveBatchID = strings.TrimSpace(*q.DriveBatchID)
	}
	cacheKey := strings.Join([]string{"command_board", strings.TrimSpace(q.TenantID), vaccinationCacheAsOfKey(asOf), catalogScopeID, boardParkID, driveBatchID}, "|")
	return vaccinationCached(ctx, r, q.TenantID, cacheKey, func(ctx context.Context) (domain.CommandBoardResponse, error) {
		var (
			unavailable  []string
			shedVaccine  commandBoardShedVaccineResult
			vaccineCodes []string
			weekly       []domain.WeeklyGivenRow
			verifyQueue  []domain.VerificationQueueRow
			driveOptions []domain.CommandBoardDriveOption
			driveTrunc   bool
		)
		driveQuery := domain.CommandBoardDriveOptionsQuery{TenantID: q.TenantID, ParkID: catalogParkID, Limit: r.driveOptionsLimit}
		driveArgs, err := commandBoardDriveOptionsArgs(driveQuery)
		if err != nil {
			return resp, err
		}

		// ONE pgx.Batch on ONE connection. REQUIRED sections are queued first; see
		// commandBoardConcurrencyBudget for why the board no longer fans out over the pool.
		// projection-review: membership=unchanged per section (each statement is the same SQL it ran
		// on its own connection); group_key=unchanged per section; join_cardinality=no join across
		// sections, each result is scanned into its own field; pagination=only the drive picker pages
		// (limit+1 probe), unchanged; scope=tenant plus the same park/drive arguments per section.
		batch := &pgx.Batch{}
		queueBound(batch, commandBoardKPISQL, q.TenantID, asOf, q.DriveBatchID, parkID)
		queueBound(batch, driveOptionsSQL, driveArgs...)
		queueBound(batch, commandBoardShedVaccineSQL, q.TenantID, asOf, q.DriveBatchID, parkID)
		queueBound(batch, commandBoardVaccineCodeSQL, q.TenantID)
		queueBound(batch, commandBoardWeeklySQL, q.TenantID, asOf, q.DriveBatchID, parkID)
		queueBound(batch, commandBoardVerifyQueueSQL, q.TenantID, asOf, q.DriveBatchID, parkID)

		err = r.commandBoardBatch(ctx, batch, func(br pgx.BatchResults) error {
			if err := br.QueryRow().Scan(&resp.KPIs.Targets, &resp.KPIs.MissedNotGiven, &resp.KPIs.DosesVerified,
				&resp.KPIs.AwaitingVerification, &resp.KPIs.ReworkNeeded, &resp.KPIs.OverdueNotGiven, &resp.KPIs.ScheduledAhead,
				&resp.KPIs.ClosedWithoutDose); err != nil {
				return fmt.Errorf("vaccination command board: kpi query: %w", err)
			}
			// The BOARD's page only. The full catalogue is CommandBoardDriveOptions, fetched lazily --
			// see domain.CommandBoardDriveOptionsPageSize for why the picker stopped shipping eagerly.
			driveRows, err := br.Query()
			if err != nil {
				return fmt.Errorf("vaccination command board: drive options query: %w", err)
			}
			page, truncated, err := scanCommandBoardDriveOptions(driveRows, driveQuery.Limit)
			if err != nil {
				return err
			}
			driveOptions, driveTrunc = page.Options, truncated

			// OPTIONAL sections. A batch runs as one implicit transaction, so once a statement fails
			// every later one reports the aborted transaction: each is still named in
			// UnavailableSections (never a silent hole) and the required sections above are intact.
			optional := func(name string, scan func(pgx.Rows) error) {
				rows, err := br.Query()
				if err == nil {
					err = scan(rows)
				}
				if err != nil {
					if r.log != nil {
						// LOGGED before it is swallowed: a section failing for a week must not be
						// visible only as a string in a JSON field nobody alerts on.
						r.log.WarnContext(ctx, "vaccination command board: optional section unavailable",
							"section", name, "tenant_id", q.TenantID, "error", err)
					}
					unavailable = append(unavailable, name)
				}
			}
			optional("shedVaccineMatrix", func(rows pgx.Rows) (err error) {
				shedVaccine, err = scanCommandBoardShedVaccineCells(rows)
				return err
			})
			optional("shedVaccineColumns", func(rows pgx.Rows) (err error) {
				vaccineCodes, err = scanCommandBoardVaccineCodes(rows)
				return err
			})
			optional("weeklyGiven", func(rows pgx.Rows) (err error) {
				weekly, err = scanCommandBoardWeeklyGiven(rows)
				return err
			})
			optional("verificationQueue", func(rows pgx.Rows) (err error) {
				verifyQueue, err = scanCommandBoardVerificationQueue(rows, asOf)
				return err
			})
			return nil
		})
		if err != nil {
			return resp, err
		}

		resp.DriveOptions = driveOptions
		resp.DriveOptionsTruncated = driveTrunc
		resp.WeeklyGiven = append(resp.WeeklyGiven, weekly...)
		resp.VerificationQueue = append(resp.VerificationQueue, verifyQueue...)
		for _, code := range vaccineCodes {
			// Labelled HERE, from the one canonical vaccine labeller, so the column header is server
			// copy like every other visible string on this board.
			resp.ShedVaccineColumns = append(resp.ShedVaccineColumns, domain.CommandBoardVaccineColumn{
				Code:  code,
				Label: vaccinatdomain.VaccineAntigenLabel(code),
			})
		}
		resp.ShedVaccineMatrix = commandBoardDensifyShedVaccine(shedVaccine, resp.ShedVaccineColumns)

		// Sorted so a section list is stable across renders: it is rendered as copy ("verification
		// queue unavailable"), and copy that reorders itself between two identical requests reads as
		// two different failures.
		sort.Strings(unavailable)
		resp.UnavailableSections = unavailable

		return resp, nil
	})
}

// commandBoardCohortRow is one (park x stage x sex x dose_code) row of the cohort matrix query,
// before dose codes are folded onto their displayed vaccine label.
type commandBoardCohortRow struct {
	parkID, parkName, stage, sex, doseCode                    string
	animalCount, pending, submitted, rejectedRework, verified int
	minAdministeredAt, maxAdministeredAt                      pgtype.Timestamptz
}

type commandBoardHeadKey struct{ parkID, stage, sex string }

type commandBoardCohortKey struct{ parkID, stage, sex, vaccine string }

func scanCommandBoardCohortRows(rows pgx.Rows) ([]commandBoardCohortRow, error) {
	defer rows.Close()
	var out []commandBoardCohortRow
	for rows.Next() {
		var row commandBoardCohortRow
		if err := rows.Scan(&row.parkID, &row.parkName, &row.stage, &row.sex, &row.doseCode,
			&row.animalCount, &row.pending, &row.submitted, &row.rejectedRework, &row.verified,
			&row.minAdministeredAt, &row.maxAdministeredAt); err != nil {
			return nil, fmt.Errorf("vaccination command board: cohort scan: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination command board: cohort rows: %w", err)
	}
	return out, nil
}

func scanCommandBoardCohortHeadCounts(rows pgx.Rows) (map[commandBoardHeadKey]int, error) {
	defer rows.Close()
	counts := map[commandBoardHeadKey]int{}
	for rows.Next() {
		var parkIDValue, stage, sex string
		var headCount int
		if err := rows.Scan(&parkIDValue, &stage, &sex, &headCount); err != nil {
			return nil, fmt.Errorf("vaccination command board: cohort head count scan: %w", err)
		}
		counts[commandBoardHeadKey{parkIDValue, stage, sex}] = headCount
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination command board: cohort head count rows: %w", err)
	}
	return counts, nil
}

func (r *Repository) commandBoardCohortExceptionCounts(ctx context.Context, tenantID string, batchID, parkID *string) (map[commandBoardCohortKey]int, error) {
	bound := sqlbind.MustBind(commandBoardCohortExceptionCountSQL, tenantID, batchID, parkID, nil, nil, nil, nil)
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, fmt.Errorf("vaccination command board: cohort exception query: %w", err)
	}
	defer rows.Close()
	// De-duplicated at LABEL grain, which is the grain the board renders and the grain the drawer
	// pages. The statement returns one row per (cell, dose_code, animal); several dose codes collapse
	// onto one displayed label (et_tt_kid_4w and et_tt_kid_7w are both "ET+TT"), so summing per-code
	// counts would report an animal once per code while the drawer -- which pages DISTINCT goat_id
	// across the cell's whole dose-code set -- lists it once. A cell would read "2 exceptions" over a
	// drawer naming one animal, which is the reconciliation break this pairing exists to prevent.
	seen := map[commandBoardCohortKey]map[string]struct{}{}
	for rows.Next() {
		var parkIDValue, stage, sex, doseCode, goatID string
		if err := rows.Scan(&parkIDValue, &stage, &sex, &doseCode, &goatID); err != nil {
			return nil, fmt.Errorf("vaccination command board: cohort exception scan: %w", err)
		}
		key := commandBoardCohortKey{parkIDValue, stage, sex, vaccinatdomain.DoseQualifiedDisplayLabel("", doseCode)}
		animals, ok := seen[key]
		if !ok {
			animals = map[string]struct{}{}
			seen[key] = animals
		}
		animals[goatID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination command board: cohort exception rows: %w", err)
	}
	counts := make(map[commandBoardCohortKey]int, len(seen))
	for key, animals := range seen {
		counts[key] = len(animals)
	}
	return counts, nil
}

// commandBoardFoldCohortCells folds dose-code rows onto their displayed vaccine label and applies
// the true herd head count.
//
// Head count is a HERD fact, not an obligation fact. The cohort query can only see animals that
// carry an obligation for that dose, so the "Animals" column reported 229 for a cohort of 324 live
// adults -- every animal whose Dose 1 obligation had been closed out of the window vanished from
// its own head count.
func commandBoardFoldCohortCells(rows []commandBoardCohortRow, headCounts map[commandBoardHeadKey]int) []domain.CommandBoardCohortCell {
	agg := map[commandBoardCohortKey]*domain.CommandBoardCohortCell{}
	order := []commandBoardCohortKey{}
	for _, row := range rows {
		// Dose-QUALIFIED, not vaccine-collapsed. Collapsing ET+TT's three doses into one column
		// summed Dose 1 + Dose 2 + Revaccination into a single number, which buried the figure
		// leadership actually asks for (ET+TT Dose 2: 210 pending, 114 verified) behind a total
		// that also exceeded the cohort head count.
		key := commandBoardCohortKey{row.parkID, row.stage, row.sex, vaccinatdomain.DoseQualifiedDisplayLabel("", row.doseCode)}
		cell, ok := agg[key]
		if !ok {
			cell = &domain.CommandBoardCohortCell{
				Cohort: domain.CommandBoardCohort{
					ParkID:          row.parkID,
					ParkName:        row.parkName,
					ManagementStage: row.stage,
					Sex:             row.sex,
					AnimalCount:     row.animalCount,
				},
				VaccineLabel: key.vaccine,
			}
			agg[key] = cell
			order = append(order, key)
		}
		if !slices.Contains(cell.DoseCodes, row.doseCode) {
			cell.DoseCodes = append(cell.DoseCodes, row.doseCode)
		}
		cell.PendingCount += row.pending
		cell.SubmittedCount += row.submitted
		cell.RejectedReworkCount += row.rejectedRework
		cell.VerifiedCount += row.verified
		if row.minAdministeredAt.Valid && (cell.MinAdministeredDate == nil || row.minAdministeredAt.Time.Before(*cell.MinAdministeredDate)) {
			administeredAt := row.minAdministeredAt.Time
			cell.MinAdministeredDate = &administeredAt
		}
		if row.maxAdministeredAt.Valid && (cell.MaxAdministeredDate == nil || row.maxAdministeredAt.Time.After(*cell.MaxAdministeredDate)) {
			administeredAt := row.maxAdministeredAt.Time
			cell.MaxAdministeredDate = &administeredAt
		}
	}
	cells := make([]domain.CommandBoardCohortCell, 0, len(order))
	for _, key := range order {
		cell := agg[key]
		if head, ok := headCounts[commandBoardHeadKey{cell.Cohort.ParkID, cell.Cohort.ManagementStage, cell.Cohort.Sex}]; ok {
			cell.Cohort.AnimalCount = head
		}
		// Sorted so one cell renders one request shape every time.
		slices.Sort(cell.DoseCodes)
		cells = append(cells, *cell)
	}
	return cells
}

func (r *Repository) commandBoardShedDoseCells(ctx context.Context, tenantID string, asOf time.Time, batchID, parkID *string) (domain.ShedDoseMatrix, error) {
	matrix := domain.ShedDoseMatrix{
		Sheds:     []domain.ShedDoseMatrixShed{},
		DoseRules: []string{},
		Cells:     []domain.ShedDoseMatrixCell{},
	}
	bound := sqlbind.MustBind(commandBoardShedDoseSQL, tenantID, asOf, batchID, parkID)
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return matrix, fmt.Errorf("vaccination command board: shed dose query: %w", err)
	}
	defer rows.Close()

	// Interning tables. A shed identity is (shedID, partitionLabel): two partitions of one shed are
	// two rows on the board and must not collapse, which is the same key buildShedGrid uses.
	shedIndex := map[commandBoardShedDoseIdentity]int{}
	doseIndex := map[string]int{}

	for rows.Next() {
		var shedID, shedName, parkName, partitionLabel, doseCode, state string
		var animalCount int
		var minAdministeredAt, maxAdministeredAt, minDueAt, maxDueAt pgtype.Timestamptz
		if err := rows.Scan(&shedID, &shedName, &parkName, &partitionLabel, &doseCode, &state, &animalCount,
			&minAdministeredAt, &maxAdministeredAt, &minDueAt, &maxDueAt); err != nil {
			return matrix, fmt.Errorf("vaccination command board: shed dose scan: %w", err)
		}

		identity := commandBoardShedDoseIdentity{shedID: shedID, partitionLabel: partitionLabel}
		shed, ok := shedIndex[identity]
		if !ok {
			shed = len(matrix.Sheds)
			shedIndex[identity] = shed
			matrix.Sheds = append(matrix.Sheds, domain.ShedDoseMatrixShed{
				ShedID:          shedID,
				ShedName:        shedName,
				PartitionLabel:  partitionLabel,
				LocationDisplay: oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}.Display(),
				ParkName:        parkName,
			})
		}

		// Interned on the raw dose_code, NOT on its display label.
		//
		// DoseQualifiedDisplayLabel only qualifies _W1/_W2/_BOOSTER/_REVAC/_REPEAT/_FIRST, so
		// et_tt_kid_4w and et_tt_kid_7w BOTH render "ET+TT" -- the same collision this file already
		// de-duplicates for cohort exceptions. Keying the interner on the label merged two distinct
		// dose codes onto one index and emitted two cells sharing a (shed, dose, state) triple,
		// whose counts a grid consumer would silently drop one of. Keying on the code keeps the
		// statement's own GROUP BY grain, so nothing is lost on the wire. Two entries in DoseRules
		// may therefore carry the SAME label string; that is honest -- they are two real doses --
		// and consumers must key on the INDEX, never on the label.
		dose, ok := doseIndex[doseCode]
		if !ok {
			dose = len(matrix.DoseRules)
			doseIndex[doseCode] = dose
			matrix.DoseRules = append(matrix.DoseRules, vaccinatdomain.DoseQualifiedDisplayLabel("", doseCode))
		}

		matrix.Cells = append(matrix.Cells, domain.ShedDoseMatrixCell{
			Shed:                shed,
			Dose:                dose,
			State:               state,
			AnimalCount:         animalCount,
			MinAdministeredDate: commandBoardBusinessDate(minAdministeredAt),
			MaxAdministeredDate: commandBoardBusinessDate(maxAdministeredAt),
			MinDueDate:          commandBoardBusinessDate(minDueAt),
			MaxDueDate:          commandBoardBusinessDate(maxDueAt),
		})
	}
	if err := rows.Err(); err != nil {
		return matrix, fmt.Errorf("vaccination command board: shed dose rows: %w", err)
	}
	return matrix, nil
}

// commandBoardShedDoseIdentity is the interning key for a shed row on the board.
type commandBoardShedDoseIdentity struct{ shedID, partitionLabel string }

// commandBoardBusinessDate renders a timestamptz as its IST business date, or "" when absent.
// Vaccination's grain is the IST business DAY, so the board must not ship an instant that a reader
// could compare against a wall clock in another zone.
func commandBoardBusinessDate(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}
	return ts.Time.In(biztime.DefaultLocation()).Format("2006-01-02")
}

type commandBoardShedVaccineKey struct{ shedID, partitionLabel, vaccine string }

type commandBoardShedIdentity struct{ id, name, partitionLabel, display, park string }

type commandBoardShedVaccineResult struct {
	cells map[commandBoardShedVaccineKey]domain.CommandBoardShedVaccineCell
	sheds map[string]commandBoardShedIdentity
	order []string
}

func scanCommandBoardShedVaccineCells(rows pgx.Rows) (commandBoardShedVaccineResult, error) {
	result := commandBoardShedVaccineResult{
		cells: map[commandBoardShedVaccineKey]domain.CommandBoardShedVaccineCell{},
		sheds: map[string]commandBoardShedIdentity{},
	}
	defer rows.Close()
	for rows.Next() {
		var shedID, shedName, partitionLabel, parkName, vaccineCode string
		var behind, verifying, rework, total int64
		if err := rows.Scan(&shedID, &shedName, &partitionLabel, &parkName, &vaccineCode, &behind, &verifying, &rework, &total); err != nil {
			return result, fmt.Errorf("vaccination command board: shed vaccine scan: %w", err)
		}
		shedKey := shedID + "|" + partitionLabel
		if _, seen := result.sheds[shedKey]; !seen {
			display := oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}.Display()
			result.sheds[shedKey] = commandBoardShedIdentity{id: shedID, name: shedName, partitionLabel: partitionLabel, display: display, park: parkName}
			result.order = append(result.order, shedKey)
		}
		// BEHIND outranks VERIFYING: an animal nobody dosed is a bigger problem than one whose proof
		// is queued, so a shed holding both reads red. Verifying is amber on its own -- the work is
		// done and the wait is on a person at a desk, not on the herd.
		state := "ok"
		switch {
		case behind > 0:
			state = "behind"
		case rework > 0:
			state = "rework"
		case verifying > 0:
			state = "verifying"
		}
		result.cells[commandBoardShedVaccineKey{shedID, partitionLabel, vaccineCode}] = domain.CommandBoardShedVaccineCell{
			ShedID:                     shedID,
			ShedName:                   shedName,
			PartitionLabel:             partitionLabel,
			OperationalLocationDisplay: oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}.Display(),
			ParkName:                   parkName,
			VaccineCode:                vaccineCode,
			State:                      state,
			BehindAnimals:              int(behind),
			VerifyingAnimals:           int(verifying),
			ReworkAnimals:              int(rework),
			TotalAnimals:               int(total),
		}
	}
	if err := rows.Err(); err != nil {
		return result, fmt.Errorf("vaccination command board: shed vaccine rows: %w", err)
	}
	return result, nil
}

// commandBoardDensifyShedVaccine gives every shed in view a cell for every vaccine in the
// catalogue. A missing cell and a clean cell are different facts and the UI must not have to guess
// which a gap means.
//
// Ordered by shed NAME for the reader, but keyed throughout by shed id: the live tenant has 175
// sheds under only 99 distinct names ("Godel 1" exists in two parks), so name is a label, never an
// identity. Grouping by it merges two parks' sheds into one row and attributes one park's red cell
// to the other's shed.
func commandBoardDensifyShedVaccine(result commandBoardShedVaccineResult, columns []domain.CommandBoardVaccineColumn) []domain.CommandBoardShedVaccineCell {
	order := append([]string(nil), result.order...)
	sort.SliceStable(order, func(i, j int) bool {
		a, b := result.sheds[order[i]], result.sheds[order[j]]
		if a.name != b.name {
			return a.name < b.name
		}
		if a.partitionLabel != b.partitionLabel {
			return a.partitionLabel < b.partitionLabel
		}
		return a.park < b.park
	})
	matrix := make([]domain.CommandBoardShedVaccineCell, 0, len(order)*len(columns))
	for _, shedKey := range order {
		identity := result.sheds[shedKey]
		for _, column := range columns {
			key := commandBoardShedVaccineKey{identity.id, identity.partitionLabel, column.Code}
			if cell, ok := result.cells[key]; ok {
				matrix = append(matrix, cell)
				continue
			}
			matrix = append(matrix, domain.CommandBoardShedVaccineCell{
				ShedID:                     identity.id,
				ShedName:                   identity.name,
				PartitionLabel:             identity.partitionLabel,
				OperationalLocationDisplay: identity.display,
				ParkName:                   identity.park,
				VaccineCode:                column.Code,
				State:                      "not_planned",
			})
		}
	}
	return matrix
}

func scanCommandBoardVaccineCodes(rows pgx.Rows) ([]string, error) {
	defer rows.Close()
	var codes []string
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, fmt.Errorf("vaccination command board: vaccine catalogue scan: %w", err)
		}
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination command board: vaccine catalogue rows: %w", err)
	}
	return codes, nil
}

func scanCommandBoardWeeklyGiven(rows pgx.Rows) ([]domain.WeeklyGivenRow, error) {
	defer rows.Close()
	var out []domain.WeeklyGivenRow
	for rows.Next() {
		var isoYear, isoWeek, count int
		var doseCode, status string
		var minAt, maxAt time.Time
		if err := rows.Scan(&isoYear, &isoWeek, &doseCode, &status, &count, &minAt, &maxAt); err != nil {
			return nil, fmt.Errorf("vaccination command board: weekly scan: %w", err)
		}
		out = append(out, domain.WeeklyGivenRow{
			ISOYear:           isoYear,
			ISOWeek:           isoWeek,
			VaccineLabel:      vaccinatdomain.DoseDisplayLabel("", doseCode),
			CompletionStatus:  status,
			Count:             count,
			MinAdministeredAt: minAt,
			MaxAdministeredAt: maxAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination command board: weekly rows: %w", err)
	}
	return out, nil
}

func scanCommandBoardVerificationQueue(rows pgx.Rows, asOf time.Time) ([]domain.VerificationQueueRow, error) {
	defer rows.Close()
	var out []domain.VerificationQueueRow
	for rows.Next() {
		var shedID, shedName, partitionLabel, doseCode string
		var awaitingCount, totalCount int
		var lastGivenDate, firstGivenDate pgtype.Timestamptz
		if err := rows.Scan(&shedID, &shedName, &partitionLabel, &doseCode, &awaitingCount, &totalCount, &lastGivenDate, &firstGivenDate); err != nil {
			return nil, fmt.Errorf("vaccination command board: verification queue scan: %w", err)
		}
		row := domain.VerificationQueueRow{
			ShedID:                     shedID,
			ShedName:                   shedName,
			PartitionLabel:             partitionLabel,
			OperationalLocationDisplay: oploc.OperationalLocation{ShedName: shedName, PartitionLabel: partitionLabel}.Display(),
			DoseRule:                   vaccinatdomain.DoseQualifiedDisplayLabel("", doseCode),
			AwaitingCount:              awaitingCount,
			TotalCount:                 totalCount,
		}
		if lastGivenDate.Valid {
			row.LastGivenOnDate = &lastGivenDate.Time
		}
		// Farm operations run 7 days a week: queue age is whole business-day difference in
		// Asia/Kolkata, no weekend subtraction.
		if firstGivenDate.Valid && awaitingCount > 0 {
			firstDay := biztime.BusinessDayStart(firstGivenDate.Time)
			asOfDay := biztime.BusinessDayStart(asOf)
			days := int(asOfDay.Sub(firstDay).Hours() / 24)
			if days < 0 {
				days = 0
			}
			row.DaysInQueue = &days
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination command board: verification queue rows: %w", err)
	}
	return out, nil
}

// CommandBoardDriveOptions serves the drive picker's full catalogue, keyset-paginated.
//
// The picker moved off the board for the reason the whole endpoint was rewritten: it was 448ms and
// 753 KB of a response budgeted at 512 KB, for a dropdown. Paging it is not merely a payload
// saving -- the statement now applies the page BEFORE it computes per-drive counts, shed-location
// JSON and day history, so the database does one page's decoration instead of the tenant's.
func (r *Repository) CommandBoardDriveOptions(ctx context.Context, q domain.CommandBoardDriveOptionsQuery) (domain.CommandBoardDriveOptionsPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if q.Limit <= 0 {
		q.Limit = domain.CommandBoardDriveOptionsPageSize
	}
	if q.Limit > domain.CommandBoardDriveOptionsMaxLimit {
		q.Limit = domain.CommandBoardDriveOptionsMaxLimit
	}
	parkID := ""
	if q.ParkID != nil {
		parkID = strings.TrimSpace(*q.ParkID)
	}
	cacheKey := strings.Join([]string{"command_board_drive_options", strings.TrimSpace(q.TenantID), parkID, q.Cursor, fmt.Sprintf("%d", q.Limit)}, "|")
	return vaccinationCached(ctx, r, q.TenantID, cacheKey, func(ctx context.Context) (domain.CommandBoardDriveOptionsPage, error) {
		page, _, err := r.commandBoardDriveOptionsPage(ctx, q)
		return page, err
	})
}

// commandBoardDriveOptionsPage reads one page and reports whether more exist.
//
// TRUNCATION IS REPORTED, NOT SWALLOWED. A bound that silently drops rows makes the picker LIE: the
// drive is scheduled, the board just does not offer it, and the reader's only available conclusion
// is that it was never planned. The statement asks for limit+1 rows and keeps limit -- the extra
// row is never rendered, it exists only to answer "was there more?", which is the cheapest honest
// overflow probe on an ordered bounded read (no second COUNT, no OFFSET scan). It doubles as the
// keyset cursor's proof that a next page exists.
func (r *Repository) commandBoardDriveOptionsPage(ctx context.Context, q domain.CommandBoardDriveOptionsQuery) (domain.CommandBoardDriveOptionsPage, bool, error) {
	args, err := commandBoardDriveOptionsArgs(q)
	if err != nil {
		return domain.CommandBoardDriveOptionsPage{Options: []domain.CommandBoardDriveOption{}}, false, err
	}
	bound := sqlbind.MustBind(driveOptionsSQL, args...)
	rows, err := r.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return domain.CommandBoardDriveOptionsPage{Options: []domain.CommandBoardDriveOption{}}, false, fmt.Errorf("vaccination command board: drive options query: %w", err)
	}
	return scanCommandBoardDriveOptions(rows, q.Limit)
}

// commandBoardDriveOptionsArgs binds driveOptionsSQL for one page (limit+1 rows, see above).
func commandBoardDriveOptionsArgs(q domain.CommandBoardDriveOptionsQuery) ([]any, error) {
	var (
		cursorRank    *int
		cursorPlanned *string
		cursorWindow  *string
		cursorBatch   *string
		cursorParkNil *bool
		cursorPark    *string
		cursorParkID  *string
	)
	if q.Cursor != "" {
		cursor, err := domain.DecodeCommandBoardDriveCursor(q.Cursor)
		if err != nil {
			return nil, fmt.Errorf("vaccination command board: drive options cursor: %w", err)
		}
		rank, planned, window := cursor.StatusRank, cursor.PlannedDate, cursor.WindowStart
		batch, parkNil, park := cursor.BatchID, cursor.ParkIsNull, cursor.ParkName
		parkID := cursor.ParkID
		if parkID == "" {
			parkID = zeroUUID
		}
		cursorRank, cursorPlanned, cursorWindow = &rank, &planned, &window
		cursorBatch, cursorParkNil, cursorPark = &batch, &parkNil, &park
		cursorParkID = &parkID
	}

	return []any{q.TenantID, q.ParkID, q.Limit + 1,
		cursorRank, cursorPlanned, cursorWindow, cursorBatch, cursorParkNil, cursorPark, cursorParkID}, nil
}

// scanCommandBoardDriveOptions reads one limit+1 page of driveOptionsSQL.
func scanCommandBoardDriveOptions(rows pgx.Rows, limit int) (domain.CommandBoardDriveOptionsPage, bool, error) {
	page := domain.CommandBoardDriveOptionsPage{Options: []domain.CommandBoardDriveOption{}}
	defer rows.Close()

	truncated := false
	var lastCursor domain.CommandBoardDriveCursor
	for rows.Next() {
		var batchID, parkOptionID, parkOptionName, status string
		var plannedDate pgtype.Date
		var windowStart, windowEnd pgtype.Timestamptz
		var doseCodes []string
		var targetCount, doseCount int
		var operatorDaysJSON []byte
		var shedLocationsJSON []byte
		var shedNames []string
		var statusRank int
		var sortPlanned pgtype.Date
		var sortWindow pgtype.Timestamptz
		var parkIsNull bool
		var sortPark string
		var sortParkID string
		if err := rows.Scan(&batchID, &parkOptionID, &parkOptionName, &status, &plannedDate, &windowStart, &windowEnd,
			&doseCodes, &targetCount, &doseCount, &operatorDaysJSON, &shedNames, &shedLocationsJSON,
			&statusRank, &sortPlanned, &sortWindow, &parkIsNull, &sortPark, &sortParkID); err != nil {
			return page, false, fmt.Errorf("vaccination command board: drive options scan: %w", err)
		}
		if len(page.Options) >= limit {
			// The limit+1'th row proves more drives exist. Stop before rendering it: it is a probe,
			// not a choice the caller may act on, and admitting it would put the page one row over
			// its own published bound.
			truncated = true
			break
		}
		var operatorDays []domain.CommandBoardDriveDay
		if len(operatorDaysJSON) > 0 {
			if err := json.Unmarshal(operatorDaysJSON, &operatorDays); err != nil {
				return page, false, fmt.Errorf("vaccination command board: drive option operator days: %w", err)
			}
		}
		var shedLocations []domain.CommandBoardDriveShedLocation
		if len(shedLocationsJSON) > 0 {
			if err := json.Unmarshal(shedLocationsJSON, &shedLocations); err != nil {
				return page, false, fmt.Errorf("vaccination command board: drive option shed locations: %w", err)
			}
		}
		for i := range shedLocations {
			shedLocations[i].OperationalLocationDisplay = oploc.OperationalLocation{
				ShedName:       shedLocations[i].ShedName,
				PartitionLabel: shedLocations[i].PartitionLabel,
			}.Display()
		}
		shedIDSet := make(map[string]struct{}, len(shedLocations))
		shedIDs := make([]string, 0, len(shedLocations))
		for _, location := range shedLocations {
			if location.ShedID == "" {
				continue
			}
			if _, ok := shedIDSet[location.ShedID]; ok {
				continue
			}
			shedIDSet[location.ShedID] = struct{}{}
			shedIDs = append(shedIDs, location.ShedID)
		}
		sort.Strings(shedIDs)

		driveName := commandBoardDriveName(doseCodes)
		option := domain.CommandBoardDriveOption{
			DriveBatchID:  batchID,
			ParkID:        parkOptionID,
			ParkName:      parkOptionName,
			DriveName:     driveName,
			Status:        status,
			Label:         commandBoardDriveLabel(driveName, plannedDate, windowStart, status, targetCount),
			TargetCount:   targetCount,
			DoseCount:     doseCount,
			OperatorDays:  operatorDays,
			ShedNames:     shedNames,
			ShedIDs:       shedIDs,
			ShedLocations: shedLocations,
		}
		if plannedDate.Valid {
			planned := biztime.BusinessDayStart(plannedDate.Time)
			option.PlannedDate = &planned
		}
		if windowStart.Valid {
			option.WindowStart = &windowStart.Time
		}
		if windowEnd.Valid {
			option.WindowEnd = &windowEnd.Time
		}
		page.Options = append(page.Options, option)
		lastCursor = domain.CommandBoardDriveCursor{
			StatusRank:  statusRank,
			PlannedDate: commandBoardCursorDate(sortPlanned),
			WindowStart: commandBoardCursorTimestamp(sortWindow),
			BatchID:     batchID,
			ParkIsNull:  parkIsNull,
			ParkName:    sortPark,
			ParkID:      sortParkID,
		}
	}
	if err := rows.Err(); err != nil {
		return page, false, fmt.Errorf("vaccination command board: drive options rows: %w", err)
	}

	if truncated && len(page.Options) > 0 {
		cursor, err := domain.EncodeCommandBoardDriveCursor(lastCursor)
		if err != nil {
			return page, truncated, fmt.Errorf("vaccination command board: drive options cursor: %w", err)
		}
		page.NextCursor = cursor
	}
	return page, truncated, nil
}

// commandBoardCursorDate and commandBoardCursorTimestamp render a sort key back into the exact
// spelling the statement's ::date / ::timestamptz casts will parse.
//
// The sort COALESCEs a missing date to -infinity so undated drives sort last within their status
// rank. A cursor that dropped that value -- or rendered it as a zero time -- would skip the whole
// undated tail at the page boundary, so the sentinel is carried literally.
func commandBoardCursorDate(v pgtype.Date) string {
	if v.InfinityModifier == pgtype.NegativeInfinity {
		return "-infinity"
	}
	if !v.Valid {
		return "-infinity"
	}
	return v.Time.Format("2006-01-02")
}

func commandBoardCursorTimestamp(v pgtype.Timestamptz) string {
	if v.InfinityModifier == pgtype.NegativeInfinity {
		return "-infinity"
	}
	if !v.Valid {
		return "-infinity"
	}
	return v.Time.Format(time.RFC3339Nano)
}

// CommandBoardCohortMatrix serves the cohort matrix as its own section.
//
// Two statements, sent as ONE pgx.Batch on one connection: the cell aggregate and the TRUE herd
// head count (the dose-sequence exception count moved to its drawer). With the
// exception count they were ~420ms of the board's ~850ms of SQL and were
// what held GET /vaccination/command at p90 416ms against a 300ms budget that cannot be relaxed.
// Splitting them out is a product change (the grid arrives a moment after the rest of the board),
// not a data change: every number here is the same whole-scope figure it was on the board.
//
// The head count is a HERD fact and is deliberately NOT drive-scoped -- a cohort's head count does
// not shrink because a drive covers part of it.
func (r *Repository) CommandBoardCohortMatrix(ctx context.Context, q domain.CommandBoardDrilldownQuery) (domain.CommandBoardCohortMatrixPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}

	page := domain.CommandBoardCohortMatrixPage{Cells: []domain.CommandBoardCohortCell{}}
	parkID := ""
	if q.ParkID != nil {
		parkID = strings.TrimSpace(*q.ParkID)
	}
	driveBatchID := ""
	if q.DriveBatchID != nil {
		driveBatchID = strings.TrimSpace(*q.DriveBatchID)
	}
	cacheKey := strings.Join([]string{"command_board_cohort_matrix", strings.TrimSpace(q.TenantID), vaccinationCacheAsOfKey(asOf), parkID, driveBatchID}, "|")
	return vaccinationCached(ctx, r, q.TenantID, cacheKey, func(ctx context.Context) (domain.CommandBoardCohortMatrixPage, error) {
		var (
			rows       []commandBoardCohortRow
			headCounts = map[commandBoardHeadKey]int{}
		)

		// projection-review: membership=unchanged (the same two statements, now one batch);
		// group_key=(park, stage, sex, dose_code) cells and (park, stage, sex) heads, unchanged;
		// join_cardinality=folded in Go by commandBoardFoldCohortCells, unchanged; pagination=none,
		// whole-scope grid; scope=tenant plus park/drive arguments, unchanged.
		batch := &pgx.Batch{}
		queueBound(batch, commandBoardCohortSQL, q.TenantID, asOf, q.DriveBatchID, q.ParkID)
		queueBound(batch, commandBoardCohortHeadSQL, q.TenantID, q.ParkID)
		err := r.commandBoardBatch(ctx, batch, func(br pgx.BatchResults) error {
			cohortRows, err := br.Query()
			if err != nil {
				return fmt.Errorf("vaccination command board: cohort query: %w", err)
			}
			if rows, err = scanCommandBoardCohortRows(cohortRows); err != nil {
				return err
			}
			headRows, err := br.Query()
			if err != nil {
				return fmt.Errorf("vaccination command board: cohort head count query: %w", err)
			}
			headCounts, err = scanCommandBoardCohortHeadCounts(headRows)
			return err
		})
		if err != nil {
			return page, err
		}

		page.Cells = commandBoardFoldCohortCells(rows, headCounts)
		return page, nil
	})
}
