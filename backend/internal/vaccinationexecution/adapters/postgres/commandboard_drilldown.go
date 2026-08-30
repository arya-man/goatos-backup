package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	oploc "github.com/vgoats/goatos/backend/internal/platform/oploc"
	vaccinatdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// Command-board drilldowns: the per-cell evidence lists that used to be computed eagerly on every
// board render. See commandboard_drilldown_sql.go for why each statement is shaped the way it is.
//
// All four share one contract:
//   - the page is asked for as limit+1 rows and truncated to limit, so "is there a next page" is
//     answered without a second COUNT and without an OFFSET scan;
//   - the cursor is minted from the LAST RETURNED ROW's sort key, so the next page resumes exactly
//     where this one stopped even if rows were inserted in between;
//   - an empty NextCursor means the list is exhausted, which is a different fact from an empty
//     page and is why the cursor is only minted when the probe row existed.

// nullableString renders an optional keyset component for SQL. pgx maps a nil *string to NULL,
// which is what the "$5::text IS NULL" first-page branch tests.
func nullableString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// CommandBoardClosedWithoutDoseAnimals returns one page of the animals behind the
// ClosedWithoutDose KPI tile.
//
// The tile's COUNT stays whole-scope on the board; this is the evidence behind it, and it ranges
// over the byte-identical residual predicate so the number and the list cannot describe different
// animals.
func (r *Repository) CommandBoardClosedWithoutDoseAnimals(ctx context.Context, q domain.CommandBoardDrilldownQuery) (domain.CommandBoardClosedWithoutDosePage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	q = q.Normalized()
	page := domain.CommandBoardClosedWithoutDosePage{Animals: []domain.CommandBoardClosedWithoutDoseAnimal{}}

	var cursorDisplay, cursorGoat *string
	if q.Cursor != "" {
		cursor, err := domain.DecodeCommandBoardAnimalCursor(q.Cursor)
		if err != nil {
			return page, fmt.Errorf("vaccination command board: closed-without-dose cursor: %w", err)
		}
		cursorDisplay = nullableString(cursor.DisplayID)
		// A blank display_id is a legitimate value and must still page. Encode it as the empty
		// string rather than NULL, or the keyset silently restarts at the top of the list.
		if cursorDisplay == nil {
			empty := ""
			cursorDisplay = &empty
		}
		cursorGoat = &cursor.GoatID
	}

	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}

	rows, err := r.pool.Query(ctx, commandBoardClosedWithoutDoseSQL,
		q.TenantID, asOf, q.DriveBatchID, q.ParkID, cursorDisplay, cursorGoat, q.Limit+1)
	if err != nil {
		return page, fmt.Errorf("vaccination command board: closed-without-dose query: %w", err)
	}
	defer rows.Close()

	overflow := false
	for rows.Next() {
		if len(page.Animals) >= q.Limit {
			overflow = true
			break
		}
		var animal domain.CommandBoardClosedWithoutDoseAnimal
		var reasonStatus, doseCode string
		if err := rows.Scan(&animal.GoatID, &animal.DisplayID, &animal.Tag1, &animal.Tag2,
			&animal.ParkName, &animal.ShedName, &animal.PartitionLabel, &reasonStatus, &doseCode); err != nil {
			return page, fmt.Errorf("vaccination command board: closed-without-dose scan: %w", err)
		}
		// Ground location is park + physical shed + partition. shed.name alone would print
		// "Godel 1" for an animal standing in "Godel 1 - Part 3".
		animal.LocationDisplay = oploc.OperationalLocation{
			ParkName:       animal.ParkName,
			ShedName:       animal.ShedName,
			PartitionLabel: animal.PartitionLabel,
		}.Display()
		// NormalizePartition collapses every non-partitioned encoding to the "whole" MATCHING
		// sentinel, which is a grouping key and never copy. A non-partitioned shed carries no
		// partition label on the wire at all.
		if normalized := oploc.NormalizePartition(animal.PartitionLabel); oploc.IsPartitioned(normalized) {
			animal.PartitionLabel = strings.TrimSpace(animal.PartitionLabel)
		} else {
			animal.PartitionLabel = ""
		}
		animal.Reason = closureReasonLabel(reasonStatus)
		animal.VaccineLabel = vaccinatdomain.DoseQualifiedDisplayLabel("", doseCode)
		page.Animals = append(page.Animals, animal)
	}
	if err := rows.Err(); err != nil {
		return page, fmt.Errorf("vaccination command board: closed-without-dose rows: %w", err)
	}

	if overflow && len(page.Animals) > 0 {
		last := page.Animals[len(page.Animals)-1]
		cursor, err := domain.EncodeCommandBoardAnimalCursor(domain.CommandBoardAnimalCursor{
			DisplayID: last.DisplayID,
			GoatID:    last.GoatID,
		})
		if err != nil {
			return page, fmt.Errorf("vaccination command board: closed-without-dose cursor: %w", err)
		}
		page.NextCursor = cursor
	}
	return page, nil
}

// CommandBoardShedVaccineAnimals returns one page of the animals behind ONE shed-vaccine cell,
// together with that shed's proof videos for the days the page's animals were recorded.
//
// The videos are correlated by SHED and DAY, not by animal. Vaccination proof is filmed per shed
// for the operator day -- one clip covers 76 goats -- so hanging a video off each animal row would
// repeat one link 76 times and imply per-goat footage that does not exist.
func (r *Repository) CommandBoardShedVaccineAnimals(ctx context.Context, q domain.CommandBoardShedVaccineAnimalsQuery) (domain.CommandBoardShedVaccineAnimalsPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	q.CommandBoardDrilldownQuery = q.CommandBoardDrilldownQuery.Normalized()
	page := domain.CommandBoardShedVaccineAnimalsPage{
		Animals:     []domain.CommandBoardShedVaccineAnimal{},
		ProofVideos: []domain.CommandBoardShedVideo{},
	}

	var cursorDue *time.Time
	var cursorGoat *string
	if q.Cursor != "" {
		cursor, err := domain.DecodeCommandBoardDueCursor(q.Cursor)
		if err != nil {
			return page, fmt.Errorf("vaccination command board: shed vaccine animals cursor: %w", err)
		}
		// A nil DueAt is passed through as NULL: the SQL COALESCEs it to 'infinity', which is where
		// the NULLS LAST tail sits, and the resume is gated on goat_id rather than on this value so
		// a NULL here still resumes instead of restarting.
		cursorDue = cursor.DueAt
		cursorGoat = &cursor.GoatID
	}

	asOf := q.AsOf
	if asOf.IsZero() {
		asOf = time.Now().In(biztime.DefaultLocation())
	}

	rows, err := r.pool.Query(ctx, commandBoardShedVaccineAnimalSQL,
		q.TenantID, asOf, q.DriveBatchID, q.ParkID,
		q.ShedID, q.VaccineCode, q.PartitionLabel,
		cursorDue, cursorGoat, q.Limit+1)
	if err != nil {
		return page, fmt.Errorf("vaccination command board: shed vaccine animals query: %w", err)
	}
	defer rows.Close()

	overflow := false
	dueByGoat := map[string]*time.Time{}
	recordedDays := map[string]time.Time{}
	for rows.Next() {
		if len(page.Animals) >= q.Limit {
			overflow = true
			break
		}
		var scopePartitionLabel, vaccineCode, goatID, displayID, tag1, tag2, status string
		var parkName, shedName, partitionLabel string
		var dueAt, recordedAt pgtype.Timestamptz
		var awaitingVerification bool
		if err := rows.Scan(&scopePartitionLabel, &vaccineCode, &goatID, &displayID, &tag1, &tag2, &status, &dueAt,
			&parkName, &shedName, &partitionLabel, &awaitingVerification, &recordedAt); err != nil {
			return page, fmt.Errorf("vaccination command board: shed vaccine animals scan: %w", err)
		}
		animal := domain.CommandBoardShedVaccineAnimal{
			GoatID:               goatID,
			DisplayID:            displayID,
			Tag:                  tag1,
			Tag2:                 tag2,
			Status:               status,
			AwaitingVerification: awaitingVerification,
			LocationDisplay: oploc.OperationalLocation{
				ParkName:       parkName,
				ShedName:       shedName,
				PartitionLabel: partitionLabel,
			}.Display(),
		}
		// NormalizePartition collapses every non-partitioned encoding onto the "whole" MATCHING
		// sentinel, which is a grouping key and never copy, so an unpartitioned shed puts no
		// partition on the wire at all.
		if normalized := oploc.NormalizePartition(partitionLabel); oploc.IsPartitioned(normalized) {
			animal.PartitionLabel = strings.TrimSpace(partitionLabel)
		}
		if recordedAt.Valid {
			rec := recordedAt.Time
			animal.RecordedAt = &rec
			day := rec.In(biztime.DefaultLocation()).Format("2006-01-02")
			if _, seen := recordedDays[day]; !seen {
				if parsed, err := time.ParseInLocation("2006-01-02", day, biztime.DefaultLocation()); err == nil {
					recordedDays[day] = parsed
				}
			}
		}
		if dueAt.Valid {
			due := dueAt.Time
			animal.DueAt = &due
			dueByGoat[goatID] = &due
		} else {
			dueByGoat[goatID] = nil
		}
		page.Animals = append(page.Animals, animal)
	}
	if err := rows.Err(); err != nil {
		return page, fmt.Errorf("vaccination command board: shed vaccine animals rows: %w", err)
	}

	if overflow && len(page.Animals) > 0 {
		last := page.Animals[len(page.Animals)-1]
		cursor, err := domain.EncodeCommandBoardDueCursor(domain.CommandBoardDueCursor{
			DueAt:  dueByGoat[last.GoatID],
			GoatID: last.GoatID,
		})
		if err != nil {
			return page, fmt.Errorf("vaccination command board: shed vaccine animals cursor: %w", err)
		}
		page.NextCursor = cursor
	}

	// Videos for the days this PAGE's animals were recorded. Asked for only when there is a day to
	// ask about, so an all-overdue cell (no completions, therefore no footage) costs no query.
	if len(recordedDays) > 0 {
		days := make([]time.Time, 0, len(recordedDays))
		for _, day := range recordedDays {
			days = append(days, day)
		}
		videos, err := r.commandBoardShedVideos(ctx, q.TenantID, []string{q.ShedID}, days, q.ParkID)
		if err != nil {
			return page, err
		}
		page.ProofVideos = videos
	}
	return page, nil
}

func (r *Repository) commandBoardShedVideos(ctx context.Context, tenantID string, shedIDs []string, days []time.Time, parkID *string) ([]domain.CommandBoardShedVideo, error) {
	rows, err := r.pool.Query(ctx, commandBoardShedVideoSQL, tenantID, shedIDs, days, parkID)
	if err != nil {
		return nil, fmt.Errorf("vaccination command board: shed video query: %w", err)
	}
	defer rows.Close()
	videos := []domain.CommandBoardShedVideo{}
	for rows.Next() {
		var shedID, proofID string
		var uploadedAt pgtype.Timestamptz
		var durationMS int64
		if err := rows.Scan(&shedID, &proofID, &uploadedAt, &durationMS); err != nil {
			return nil, fmt.Errorf("vaccination command board: shed video scan: %w", err)
		}
		video := domain.CommandBoardShedVideo{
			// Playback PATH, not a bare id: the client must not have to know how proof URLs are
			// built, and the signed GCS URL is minted per request by the proof service.
			Path:       "/app/proofs/" + proofID + "/download",
			DurationMS: durationMS,
		}
		if uploadedAt.Valid {
			at := uploadedAt.Time
			video.UploadedAt = &at
		}
		videos = append(videos, video)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vaccination command board: shed video rows: %w", err)
	}
	return videos, nil
}

// CommandBoardCohortExceptions returns one page of a cohort cell's dose-sequence exceptions: the
// animals behind the cell's MissingPriorDoseCount.
func (r *Repository) CommandBoardCohortExceptions(ctx context.Context, q domain.CommandBoardCohortCellQuery) (domain.CommandBoardCohortExceptionsPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	q.CommandBoardDrilldownQuery = q.CommandBoardDrilldownQuery.Normalized()
	page := domain.CommandBoardCohortExceptionsPage{Animals: []domain.CommandBoardCohortAnimal{}}

	var cursorDisplay, cursorGoat *string
	if q.Cursor != "" {
		cursor, err := domain.DecodeCommandBoardAnimalCursor(q.Cursor)
		if err != nil {
			return page, fmt.Errorf("vaccination command board: cohort exception cursor: %w", err)
		}
		display := cursor.DisplayID
		cursorDisplay = &display
		cursorGoat = &cursor.GoatID
	}

	rows, err := r.pool.Query(ctx, commandBoardCohortExceptionListSQL,
		q.TenantID, q.DriveBatchID, q.ParkID,
		q.CohortParkID, q.ManagementStage, q.Sex, q.DoseCodes,
		cursorDisplay, cursorGoat, q.Limit+1)
	if err != nil {
		return page, fmt.Errorf("vaccination command board: cohort exception query: %w", err)
	}
	defer rows.Close()

	overflow := false
	for rows.Next() {
		if len(page.Animals) >= q.Limit {
			overflow = true
			break
		}
		var animal domain.CommandBoardCohortAnimal
		if err := rows.Scan(&animal.GoatID, &animal.DisplayID, &animal.Tag); err != nil {
			return page, fmt.Errorf("vaccination command board: cohort exception scan: %w", err)
		}
		page.Animals = append(page.Animals, animal)
	}
	if err := rows.Err(); err != nil {
		return page, fmt.Errorf("vaccination command board: cohort exception rows: %w", err)
	}

	if overflow && len(page.Animals) > 0 {
		last := page.Animals[len(page.Animals)-1]
		cursor, err := domain.EncodeCommandBoardAnimalCursor(domain.CommandBoardAnimalCursor{
			DisplayID: last.DisplayID,
			GoatID:    last.GoatID,
		})
		if err != nil {
			return page, fmt.Errorf("vaccination command board: cohort exception cursor: %w", err)
		}
		page.NextCursor = cursor
	}
	return page, nil
}

// CommandBoardCohortDays returns a cohort cell's administered-day split.
//
// No cursor: the row count is bounded by the days in the drive window, which is a drawer-sized
// list by construction. Paginating a bar chart would only let a caller render half of one.
func (r *Repository) CommandBoardCohortDays(ctx context.Context, q domain.CommandBoardCohortCellQuery) (domain.CommandBoardCohortDaysPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	page := domain.CommandBoardCohortDaysPage{Days: []domain.CommandBoardCohortDay{}}
	rows, err := r.pool.Query(ctx, commandBoardCohortDaySQL,
		q.TenantID, q.DriveBatchID, q.ParkID,
		q.CohortParkID, q.ManagementStage, q.Sex, q.DoseCodes)
	if err != nil {
		return page, fmt.Errorf("vaccination command board: cohort day query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var administeredDate pgtype.Date
		var animalCount int
		if err := rows.Scan(&administeredDate, &animalCount); err != nil {
			return page, fmt.Errorf("vaccination command board: cohort day scan: %w", err)
		}
		if !administeredDate.Valid {
			continue
		}
		page.Days = append(page.Days, domain.CommandBoardCohortDay{
			Date:        administeredDate.Time.Format("2006-01-02"),
			AnimalCount: animalCount,
		})
	}
	if err := rows.Err(); err != nil {
		return page, fmt.Errorf("vaccination command board: cohort day rows: %w", err)
	}
	return page, nil
}
