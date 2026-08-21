package postgres

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// ProofURLResolver turns a proof artifact id into a clickable, playable URL: a time-limited
// signed HTTPS URL for GCS-backed proofs, an absolute HTTP URL against the local media server
// for local_object proofs. It is the SAME resolver the mobile app uses to open proof media
// (backend/internal/proof/app.Service.DownloadURL) -- the export must not invent a second
// signing scheme. Optional: a Repository with no resolver configured falls back to emitting the
// raw storage reference with an explanatory note instead of a URL.
type ProofURLResolver interface {
	ResolveProofDownloadURL(ctx context.Context, tenantID, proofID string) (string, error)
}

// ExportCampaignCSVRow represents one row in the export.
type ExportCampaignCSVRow struct {
	ShedName           string
	ObservationType    string // "individual" or "lumpsum"
	ScannedIdentifier  string // For individual only
	WeightKg           float64
	AverageWeightKg    float64 // For lumpsum only
	AnimalCount        int     // For lumpsum only
	VerificationStatus string  // "pending", "verified", "rework"
	ProofReferenceType string  // "gcs" or "local" or empty
	ProofReference     string  // Bucket path for GCS, local path or empty
	AcceptedAt         time.Time
}

// ExportCampaignCSV streams CSV rows for all observations in a campaign to the writer.
// Rows are STREAMED: each shed's observations are read with rows.Next() and handed straight to
// the CSV writer, so no result set is collected into a slice first. The only thing held in memory
// is the shed list, which is bounded by the sheds in the task, not by how many animals were
// weighed. (It is not cursor-paginated -- an earlier version of this comment claimed keyset
// pagination that was never in the query.)
//
// The export is CURRENT STATE per shed and per animal, not an audit trail: a re-weigh after a
// rejection replaces the earlier capture, so the file carries what the weight IS, along with the
// verification status that says whether it was accepted.
func (r *Repository) ExportCampaignCSV(ctx context.Context, tenantID, campaignID string, writer io.Writer) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	csvWriter := csv.NewWriter(writer)
	defer csvWriter.Flush()

	// Write CSV header
	// Column shape follows the stg import sheet the maintainer shared (park / shed / split
	// IST date+time / identifier / weight), so an operations reader can line the two up.
	//
	// Deliberately NOT carried over from that sheet: old_id and old_id_suffix. Free-flow
	// weighing records a RAW scanned identifier and never resolves it to a goat, so this
	// export cannot honestly print an old tag -- it would have to invent the lookup the
	// capture path is banned from doing. The scanned identifier is what was actually read.
	header := []string{
		"Park",
		"Shed Name",
		"Shed Status",
		"Type",
		"Scanned Identifier",
		"Weight (kg)",
		"Average Weight (kg)",
		"Animal Count",
		"Verification Status",
		"Proof Reference Type",
		"Proof Reference",
		"Proof Video URL",
		"Proof URL Note",
		"Recorded At",
		"Date (IST)",
		"Time (IST)",
	}
	if err := csvWriter.Write(header); err != nil {
		return err
	}

	// Query all sheds for this campaign
	sheds, err := r.queryCampaignSheds(ctx, tenantID, campaignID)
	if err != nil {
		return err
	}

	// For each shed, write observations
	for _, shed := range sheds {
		// Write individual observations
		individualRows, err := r.writeIndividualObservationsToCSV(ctx, tenantID, shed, csvWriter)
		if err != nil {
			return err
		}

		// Write lumpsum observation if it exists
		lumpSumRows, err := r.writeLumpSumObservationToCSV(ctx, tenantID, shed, csvWriter)
		if err != nil {
			return err
		}

		// EVERY shed of the task appears, including one with nothing weighed yet. An export that
		// silently omits the untouched sheds reads as if the task were smaller than it is -- the
		// reader cannot tell "no animals here" from "this shed was never exported", which is the
		// opposite of what an export is for. The row says plainly that nothing was captured.
		if individualRows == 0 && lumpSumRows == 0 {
			if err := csvWriter.Write([]string{
				csvText(shed.ParkName), csvText(shed.DisplayName), shed.Status, "not weighed",
				"", "", "", "", "", "", "", "", "", "", "", "",
			}); err != nil {
				return err
			}
		}
	}

	return nil
}

// ExportCSV streams the leadership-visible weighing window across authorized parks,
// optionally narrowed to selected shed locations.
//
// Column shape follows the operations "Weight check" sheet the maintainer reconciles
// against (maintainer request 2026-08-21), minus its video-link column: date | rfid |
// rfid_2 | old_id | old_id_suffix | breed | gender | shed | type | count | operator |
// approval | verified_weight_kg. `old_id_suffix` is structurally blank — the sheet
// keeps the column and leaves it empty, and the file must line up cell-for-cell.
//
// Pending verification is exported as data, not filtered out; the verdict travels in
// the `approval` column (approved / rejected / pending, the sheet's own words) so CEO
// can see today's unverified work instead of receiving an empty file. The weight
// column is the CURRENT weight: a verifier correction REPLACES weight_kg (migration
// 000172), so this is the verified figure whenever one exists.
//
// The rfid_2 / old_id / breed / gender cells come from exportGoatIdentities in
// weight_demographics.go — the ONE weighing file the free-flow guard permits to
// resolve a scanned tag — fetched as ONE bounded map before streaming, never per row.
// A tag that resolves to nothing exports blank cells and is still COUNTED, never
// dropped: free-flow capture stays untouched.
func (r *Repository) ExportCSV(ctx context.Context, tenantID string, parkIDs []string, shedLocationIDs []string, periodStart, periodEnd time.Time, writer io.Writer) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	csvWriter := csv.NewWriter(writer)
	defer csvWriter.Flush()

	if err := csvWriter.Write([]string{
		"date",
		"rfid",
		"rfid_2",
		"old_id",
		"old_id_suffix",
		"breed",
		"gender",
		"shed",
		"type",
		"count",
		"operator",
		"approval",
		"verified_weight_kg",
	}); err != nil {
		return err
	}
	if len(parkIDs) == 0 {
		return nil
	}
	if shedLocationIDs == nil {
		shedLocationIDs = []string{}
	}

	identities, err := r.exportGoatIdentities(ctx, tenantID, parkIDs, shedLocationIDs, periodStart, periodEnd)
	if err != nil {
		return err
	}

	rows, err := r.pool.Query(ctx, `
WITH individual AS (
  SELECT
    (o.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS business_date,
    'individual'::text AS weighing_type,
    o.scanned_identifier AS rfid,
    cs.display_name AS shed_label,
    COALESCE(cs.partition_label, '') AS partition_label,
    NULL::int AS animal_count,
    COALESCE(op.display_name, '') AS operator,
    COALESCE(NULLIF(o.verification_status, ''), 'pending') AS verification_status,
    o.weight_kg::float8 AS weight_kg
  FROM weighing_observations o
  JOIN weighing_campaign_sheds cs ON cs.tenant_id=o.tenant_id AND cs.campaign_shed_id=o.campaign_shed_id
  JOIN weighing_campaigns c ON c.tenant_id=o.tenant_id AND c.campaign_id=o.campaign_id
  -- LATERAL LIMIT 1 so a person with more than one roster row can never multiply an
  -- observation row; newest row wins deterministically. workforce_members is one of
  -- the allowlisted ORG tables — this names the person, it gates nothing.
  LEFT JOIN LATERAL (
    SELECT wm.display_name
    FROM workforce_members wm
    WHERE wm.tenant_id = o.tenant_id AND wm.user_id = o.recorded_by
    ORDER BY wm.updated_at DESC
    LIMIT 1
  ) op ON true
  WHERE o.tenant_id=$1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND (cardinality($5::uuid[]) = 0 OR cs.location_id = ANY($5::uuid[]))
    AND o.accepted_at >= $3::timestamptz
    AND o.accepted_at < $4::timestamptz
),
lumpsum AS (
  SELECT
    (so.accepted_at AT TIME ZONE 'Asia/Kolkata')::date AS business_date,
    'lumpsum'::text AS weighing_type,
    ''::text AS rfid,
    cs.display_name AS shed_label,
    COALESCE(cs.partition_label, '') AS partition_label,
    so.animal_count::int AS animal_count,
    COALESCE(op.display_name, '') AS operator,
    COALESCE(NULLIF(so.verification_status, ''), 'pending') AS verification_status,
    so.weight_kg::float8 AS weight_kg
  FROM weighing_shed_observations so
  JOIN weighing_campaign_sheds cs ON cs.tenant_id=so.tenant_id AND cs.campaign_shed_id=so.campaign_shed_id
  JOIN weighing_campaigns c ON c.tenant_id=so.tenant_id AND c.campaign_id=so.campaign_id
  LEFT JOIN LATERAL (
    SELECT wm.display_name
    FROM workforce_members wm
    WHERE wm.tenant_id = so.tenant_id AND wm.user_id = so.recorded_by
    ORDER BY wm.updated_at DESC
    LIMIT 1
  ) op ON true
  WHERE so.tenant_id=$1::uuid
    AND c.park_id = ANY($2::uuid[])
    AND (cardinality($5::uuid[]) = 0 OR cs.location_id = ANY($5::uuid[]))
    AND so.accepted_at >= $3::timestamptz
    AND so.accepted_at < $4::timestamptz
    AND so.withdrawn_at IS NULL
)
SELECT business_date::text, weighing_type, rfid, shed_label, partition_label,
       animal_count, operator, verification_status, weight_kg
FROM (
  SELECT * FROM individual
  UNION ALL
  SELECT * FROM lumpsum
) exported
ORDER BY business_date DESC, shed_label ASC, partition_label ASC, weighing_type ASC, rfid ASC
`, tenantID, parkIDs, periodStart, periodEnd, shedLocationIDs)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var date, kind, rfid, shedLabel, partitionLabel, operator, status string
		var weightKg *float64
		var animalCount *int
		if err := rows.Scan(
			&date, &kind, &rfid, &shedLabel, &partitionLabel,
			&animalCount, &operator, &status, &weightKg,
		); err != nil {
			return err
		}
		identity := identities[strings.ToLower(strings.TrimSpace(rfid))]
		if err := csvWriter.Write([]string{
			date,
			csvText(rfid),
			csvText(identity.SecondTag),
			csvText(identity.DisplayID),
			"", // old_id_suffix — kept blank by sheet convention
			csvText(identity.Breed),
			csvText(identity.Sex),
			csvText(exportShedDisplay(shedLabel, partitionLabel)),
			kind,
			formatOptionalInt(animalCount),
			csvText(operator),
			exportApprovalLabel(status),
			formatOptionalFloat(weightKg),
		}); err != nil {
			return err
		}
	}
	return rows.Err()
}

// exportApprovalLabel maps the stored verification status to the sheet's own approval
// words: verified → approved, rework → rejected, anything else (including blank and
// the stored literal 'pending') → pending.
func exportApprovalLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "verified":
		return "approved"
	case "rework", "rejected":
		return "rejected"
	default:
		return "pending"
	}
}

// exportShedDisplay composes the shed cell exactly the way the Weights screen does
// (shed_weights.go): the bucket's planning label plus its partition through the
// canonical oploc composer, with the same doubling guard — a partitioned bucket is
// routinely NAMED for the pen it covers ("Castro 2"), and appending the partition
// again would print "Castro 2 2".
func exportShedDisplay(shedLabel, partitionLabel string) string {
	if partitionLabel != "" && strings.HasSuffix(shedLabel, partitionLabel) {
		return shedLabel
	}
	return (oploc.OperationalLocation{
		ShedName:       shedLabel,
		PartitionLabel: partitionLabel,
	}).Display()
}

type shedInfo struct {
	CampaignShedID string
	DisplayName    string
	ParkName       string
	Status         string
}

// queryCampaignSheds fetches all sheds for a campaign.
func (r *Repository) queryCampaignSheds(ctx context.Context, tenantID, campaignID string) ([]shedInfo, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT cs.campaign_shed_id::text,
		       cs.display_name,
		       COALESCE(p.name, ''),
		       cs.status
		FROM weighing_campaign_sheds cs
		LEFT JOIN locations p ON p.location_id = cs.park_id AND p.tenant_id = cs.tenant_id
		WHERE cs.tenant_id=$1::uuid AND cs.campaign_id=$2::uuid
		ORDER BY COALESCE(p.name, '') ASC, cs.display_name ASC
	`, tenantID, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sheds []shedInfo
	for rows.Next() {
		var shed shedInfo
		if err := rows.Scan(&shed.CampaignShedID, &shed.DisplayName, &shed.ParkName, &shed.Status); err != nil {
			return nil, err
		}
		sheds = append(sheds, shed)
	}
	return sheds, rows.Err()
}

// writeIndividualObservationsToCSV writes all individual observations for a shed to CSV.
func (r *Repository) writeIndividualObservationsToCSV(ctx context.Context, tenantID string, shed shedInfo, csvWriter *csv.Writer) (int, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT
			o.scanned_identifier,
			o.weight_kg,
			o.verification_status,
			-- The DURABLE storage reference, not the artifact id. An id is useless outside this
			-- database; the maintainer asked for the actual link so a row can be opened from the
			-- exported sheet. gs://bucket/object for GCS, the object path for local storage.
			COALESCE(pa.storage_provider, ''),
			COALESCE(pa.object_key, ''),
			COALESCE(pa.proof_id::text, ''),
			o.accepted_at
		FROM weighing_observations o
		LEFT JOIN proof_artifacts pa
		  ON pa.tenant_id = o.tenant_id
		 AND pa.proof_id = o.proof_artifact_id
		WHERE o.tenant_id=$1::uuid AND o.campaign_shed_id=$2::uuid
		ORDER BY o.accepted_at ASC, o.observation_id ASC
	`, tenantID, shed.CampaignShedID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	written := 0
	for rows.Next() {
		var scannedID, provider, objectKey, proofID string
		var weight float64
		var status string
		var acceptedAt time.Time

		if err := rows.Scan(&scannedID, &weight, &status, &provider, &objectKey, &proofID, &acceptedAt); err != nil {
			return 0, err
		}
		proofRefType, proofRef := proofStorageReference(provider, objectKey)
		proofURL, proofURLNote := r.resolveProofVideoURL(ctx, tenantID, proofID, proofRef)

		row := []string{
			csvText(shed.ParkName),
			csvText(shed.DisplayName),
			shed.Status,
			"individual",
			csvText(scannedID),
			formatFloat(weight),
			"", // no average weight for individual
			"", // no animal count for individual
			status,
			proofRefType,
			csvText(proofRef),
			csvText(proofURL),
			csvText(proofURLNote),
			acceptedAt.Format("2006-01-02T15:04:05Z07:00"),
			istDate(acceptedAt),
			istTime(acceptedAt),
		}
		if err := csvWriter.Write(row); err != nil {
			return written, err
		}
		written++
	}
	return written, rows.Err()
}

// writeLumpSumObservationToCSV writes the lumpsum observation for a shed to CSV, if it exists.
func (r *Repository) writeLumpSumObservationToCSV(ctx context.Context, tenantID string, shed shedInfo, csvWriter *csv.Writer) (int, error) {
	var totalWeight, avgWeight float64
	var animalCount int
	var status string
	var provider, objectKey, proofID string
	var acceptedAt time.Time

	err := r.pool.QueryRow(ctx, `
		SELECT
			so.weight_kg,
			so.average_weight_kg,
			so.animal_count,
			so.verification_status,
			COALESCE(pa.storage_provider, ''),
			COALESCE(pa.object_key, ''),
			COALESCE(pa.proof_id::text, ''),
			so.accepted_at
		FROM weighing_shed_observations so
		LEFT JOIN proof_artifacts pa
		  ON pa.tenant_id = so.tenant_id
		 AND pa.proof_id = so.proof_artifact_id
		WHERE so.tenant_id=$1::uuid AND so.campaign_shed_id=$2::uuid
		ORDER BY so.created_at DESC
		LIMIT 1
	`, tenantID, shed.CampaignShedID).Scan(&totalWeight, &avgWeight, &animalCount, &status, &provider, &objectKey, &proofID, &acceptedAt)

	if err != nil {
		// No lumpsum observation for this shed. Compared with errors.Is(pgx.ErrNoRows) rather
		// than by message text: a string match silently turns any future wording change into a
		// swallowed error.
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}

	proofRefType, proofRef := proofStorageReference(provider, objectKey)
	proofURL, proofURLNote := r.resolveProofVideoURL(ctx, tenantID, proofID, proofRef)

	row := []string{
		csvText(shed.ParkName),
		csvText(shed.DisplayName),
		shed.Status,
		"lumpsum",
		"", // no scanned identifier for lumpsum
		formatFloat(totalWeight),
		formatFloat(avgWeight),
		formatInt(animalCount),
		status,
		proofRefType,
		csvText(proofRef),
		csvText(proofURL),
		csvText(proofURLNote),
		acceptedAt.Format("2006-01-02T15:04:05Z07:00"),
		istDate(acceptedAt),
		istTime(acceptedAt),
	}
	if err := csvWriter.Write(row); err != nil {
		return 0, err
	}
	return 1, nil
}

// resolveProofVideoURL resolves the CLICKABLE, PLAYABLE URL for a proof, using the same signed-URL
// path the mobile app uses to open proof media (proof.app.Service.DownloadURL, injected via
// WithProofURLResolver) -- never a second signing scheme invented here.
//
// It never writes a half-path that only looks like a link: any failure to resolve returns an
// empty URL plus a plain-English reason in the neighbouring column, so the raw reference column
// stays the only thing a reader can act on for that row.
func (r *Repository) resolveProofVideoURL(ctx context.Context, tenantID, proofID, proofRef string) (url, note string) {
	if proofRef == "" {
		return "", ""
	}
	if strings.TrimSpace(proofID) == "" {
		return "", "no proof artifact linked to this observation"
	}
	if r.proofURLs == nil {
		return "", "proof URL resolver not configured"
	}
	resolved, err := r.proofURLs.ResolveProofDownloadURL(ctx, tenantID, proofID)
	if err != nil {
		return "", "proof link unavailable: " + err.Error()
	}
	resolved = strings.TrimSpace(resolved)
	if resolved == "" || !strings.HasPrefix(resolved, "http") {
		// Anything that isn't an absolute http(s) URL is not clickable -- refuse to emit it
		// rather than write something that looks like a link but is not.
		return "", "proof link unavailable: resolver did not return an absolute URL"
	}
	return resolved, ""
}

func (r *Repository) proofVideoColumns(ctx context.Context, tenantID, proofIDs, proofProviders, proofObjectKeys string) (types, refs, urls, notes string) {
	ids := splitProofField(proofIDs)
	providers := splitProofField(proofProviders)
	objectKeys := splitProofField(proofObjectKeys)

	var refTypes, proofRefs, proofURLs, proofURLNotes []string
	for i, objectKey := range objectKeys {
		provider := fieldAt(providers, i)
		proofID := fieldAt(ids, i)
		refType, proofRef := proofStorageReference(provider, objectKey)
		if proofRef == "" {
			continue
		}
		proofURL, proofURLNote := r.resolveProofVideoURL(ctx, tenantID, proofID, proofRef)
		refTypes = append(refTypes, refType)
		proofRefs = append(proofRefs, proofRef)
		if proofURL != "" {
			proofURLs = append(proofURLs, proofURL)
		}
		if proofURLNote != "" {
			proofURLNotes = append(proofURLNotes, proofURLNote)
		}
	}
	return strings.Join(refTypes, " | "), strings.Join(proofRefs, " | "), strings.Join(proofURLs, " | "), strings.Join(proofURLNotes, " | ")
}

func splitProofField(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return strings.Split(value, "\x1f")
}

func fieldAt(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}

// proofStorageReference renders the DURABLE location of a proof clip for the export.
//
// The export is read outside this system -- in a sheet, by someone reconciling weights -- so a
// proof_artifacts UUID is worthless there. GCS objects render as a gs:// URI that can be opened
// or handed to gsutil; local-storage objects render as their object path, labelled as such so
// nobody mistakes a dev path for a bucket link. A signed HTTPS URL is deliberately NOT emitted:
// it expires, and an expired link pasted into a report is worse than an honest path.
func proofStorageReference(provider, objectKey string) (string, string) {
	objectKey = strings.TrimSpace(objectKey)
	if objectKey == "" {
		return "", ""
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "gcs":
		bucket := strings.TrimSpace(os.Getenv("GOATOS_PROOF_GCS_BUCKET"))
		if bucket == "" {
			// No bucket configured: emit the object path rather than a malformed gs:// URI.
			return "gcs_object", objectKey
		}
		return "gcs", "gs://" + bucket + "/" + objectKey
	case "local":
		return "local_object", objectKey
	default:
		return "object", objectKey
	}
}

func formatFloat(f float64) string {
	if f == 0 {
		return ""
	}
	// -1 precision prints the shortest form that round-trips: 35.2, not 35.200, and 21.15 keeps
	// both digits. Scales are read to one decimal, and a column of "35.200" invites a reader to
	// think the third digit was measured.
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func formatInt(i int) string {
	if i == 0 {
		return ""
	}
	return strconv.Itoa(i)
}

func formatOptionalFloat(value *float64) string {
	if value == nil {
		return ""
	}
	return formatFloat(*value)
}

func formatOptionalInt(value *int) string {
	if value == nil {
		return ""
	}
	return formatInt(*value)
}

// The farm reads times in IST. The RFC3339 column keeps the exact instant with its offset for
// anything machine-read; these two are the human columns, split the same way the operations
// sheet splits them.
//
// A FIXED zone, not time.LoadLocation("Asia/Kolkata"): a container without tzdata would make
// LoadLocation fail and silently fall back to UTC, printing 04:00 as the weigh time for a
// 09:30 weighing. India has no DST, so the offset is a constant and there is nothing to lose.
var istZone = time.FixedZone("IST", 5*3600+30*60)

func istDate(t time.Time) string { return t.In(istZone).Format("2006-01-02") }

func istTime(t time.Time) string { return t.In(istZone).Format("15:04:05") }

// csvText neutralises a spreadsheet FORMULA before it is written to a cell.
//
// This file is opened in Excel or Google Sheets -- the Android app shares it straight to Drive
// and Gmail. A cell whose text begins with = + - @ (or a leading tab/CR) is evaluated as a
// FORMULA by both, so a shed named `=HYPERLINK("http://evil","click")`, or a free-flow scanned
// identifier typed as `+cmd|'/c calc'!A1`, executes on the reader's machine when they open the
// export. Free-flow weighing deliberately accepts ANY scanned string without validation, so the
// tag column is genuinely attacker-reachable and cannot be assumed numeric.
//
// A leading apostrophe is the standard neutraliser: both Excel and Sheets treat the rest of the
// cell as literal text and do not display the quote.
func csvText(value string) string {
	if value == "" {
		return ""
	}
	switch value[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + value
	}
	return value
}
