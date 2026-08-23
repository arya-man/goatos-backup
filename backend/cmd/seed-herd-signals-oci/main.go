// Command seed-herd-signals-oci replays a decoded HoneyComm BLE gateway packet
// capture CSV through the herd-signals ingest service so movement_state and
// activity_windows get computed by the real backend logic (not hand-inserted).
//
// Usage:
//
//	DATABASE_URL=postgres://... go run ./backend/cmd/seed-herd-signals-oci \
//	    -csv /Users/ravi/mesha/local-data/honeycomm-gateway-capture/decoded_ear_tags.csv \
//	    -tenant-id <tenant-uuid> -gateway-id <gateway-uuid> -batch-size 200
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/herdsignals/app"
	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/platform/migrationguard"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

func main() {
	csvPath := flag.String("csv", "/Users/ravi/mesha/local-data/honeycomm-gateway-capture/decoded_ear_tags.csv", "path to decoded HoneyComm CSV capture")
	tenantID := flag.String("tenant-id", "", "tenant ID to ingest packets under (required)")
	userID := flag.String("user-id", "seed-herd-signals-oci", "actor user ID recorded for the ingest")
	gatewayID := flag.String("gateway-id", "", "gateway ID to attribute packets to (required)")
	batchSize := flag.Int("batch-size", 200, "number of packets per IngestPackets call")
	dryRun := flag.Bool("dry-run", false, "parse the CSV and report counts without writing to the database")
	flag.Parse()

	if *tenantID == "" {
		log.Fatal("seed-herd-signals-oci: -tenant-id is required")
	}
	if *gatewayID == "" {
		log.Fatal("seed-herd-signals-oci: -gateway-id is required")
	}

	rows, err := readCSV(*csvPath)
	if err != nil {
		log.Fatalf("seed-herd-signals-oci: read csv: %v", err)
	}
	log.Printf("seed-herd-signals-oci: parsed %d packet rows from %s", len(rows), *csvPath)

	if *dryRun {
		log.Printf("seed-herd-signals-oci: dry-run requested, exiting without database writes")
		return
	}

	ctx := context.Background()

	cfg := platformpg.ConfigFromEnv()
	if cfg.DatabaseURL == "" {
		log.Fatal("seed-herd-signals-oci: DATABASE_URL is required")
	}

	pool, err := platformpg.Connect(ctx, cfg)
	if err != nil {
		log.Fatalf("seed-herd-signals-oci: connect to postgres: %v", err)
	}
	defer pool.Close()

	// Fail fast on migration drift: if the database has migrations this binary
	// doesn't know about, refuse to write to the database.
	binaryMigrationVersion, err := migrationguard.BinaryVersion()
	if err != nil {
		log.Fatalf("seed-herd-signals-oci: migration guard: %v", err)
	}
	dbMigrationVersion, err := migrationguard.AppliedVersion(ctx, pool)
	if err != nil {
		log.Fatalf("seed-herd-signals-oci: migration guard: %v", err)
	}
	status, err := migrationguard.Check(dbMigrationVersion, binaryMigrationVersion)
	if err != nil {
		if status.DBAhead {
			log.Printf("seed-herd-signals-oci: ERROR migration_drift_dbahead_fatal: db=%s binary=%s", dbMigrationVersion, binaryMigrationVersion)
		}
		log.Fatalf("seed-herd-signals-oci: migration guard: %v", err)
	}

	repo := postgres.NewRepository(pool)
	svc := app.NewService(repo)

	actor := domain.Actor{
		TenantID: *tenantID,
		UserID:   *userID,
	}

	var (
		totalAccepted int
		totalStored   int
		totalLatest   int
	)

	for start := 0; start < len(rows); start += *batchSize {
		end := start + *batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[start:end]

		// gateway_seen_at is the GATEWAY's own clock (CSV packet_time), kept separate
		// and UNCORRECTED. The observed HoneyComm gateway runs ~2h30m ahead of real
		// IST, so it is recorded for diagnostics only -- server received_at is truth
		// for ordering, staleness and every rendered time. Never reconcile the two by
		// shifting one onto the other: the skew is the signal that a gateway's NTP is
		// wrong, and averaging it away hides that.
		gatewaySeen := batch[0].gatewayTime
		for _, r := range batch {
			if r.gatewayTime.After(gatewaySeen) {
				gatewaySeen = r.gatewayTime
			}
		}
		if gatewaySeen.IsZero() {
			gatewaySeen = batch[len(batch)-1].receivedAt
		}

		req := domain.IngestRequest{
			GatewayID:   *gatewayID,
			GatewaySeen: gatewaySeen.Format(time.RFC3339),
			Packets:     make([]domain.IngestPacket, 0, len(batch)),
		}

		for _, r := range batch {
			req.Packets = append(req.Packets, r.toIngestPacket())
		}

		// scale-guard:ignore: dev-only seed tool, one IngestPackets call per *batchSize chunk (not per packet); not a serving path
		resp, err := svc.IngestPackets(ctx, actor, req)
		if err != nil {
			log.Fatalf("seed-herd-signals-oci: ingest batch [%d:%d] failed: %v", start, end, err)
		}

		totalAccepted += resp.Accepted
		totalStored += resp.Stored
		totalLatest += resp.LatestUpdated

		log.Printf("seed-herd-signals-oci: batch [%d:%d] accepted=%d stored=%d latest_updated=%d trace_id=%s",
			start, end, resp.Accepted, resp.Stored, resp.LatestUpdated, resp.TraceID)
	}

	log.Printf("seed-herd-signals-oci: done. total_accepted=%d total_stored=%d total_latest_updated=%d",
		totalAccepted, totalStored, totalLatest)
}

// packetRow is the parsed representation of one decoded_ear_tags.csv row.
type packetRow struct {
	receivedAt    time.Time
	tagAddr       string
	printedID     string
	gatewayTime   time.Time
	rssi          *int16
	batteryMV     *int
	temperatureC  *float64
	motionCount   *int64
	tempSensorOK  *bool
	accelSensorOK *bool
	sensorState   *int16
	rawAdv        *string
}

func (r packetRow) toIngestPacket() domain.IngestPacket {
	var rawAdv *string
	if r.rawAdv != nil && *r.rawAdv != "" {
		rawAdv = r.rawAdv
	}
	return domain.IngestPacket{
		TagID:                 r.printedID,
		TagMAC:                r.tagAddr,
		RSSI:                  r.rssi,
		Battery:               r.batteryMV,
		TagTemperature:        r.temperatureC,
		MotionCount:           r.motionCount,
		SensorState:           r.sensorState,
		TemperatureSensorOK:   r.tempSensorOK,
		AccelerometerSensorOK: r.accelSensorOK,
		RawAdv:                rawAdv,
		SeenAt:                r.receivedAt.Format(time.RFC3339),
	}
}

// readCSV parses decoded_ear_tags.csv into packetRow entries. Rows with an
// unparsable received_at timestamp or missing tag_addr are skipped with a
// warning, matching the ingest service's own tolerant-skip behavior for
// malformed packets.
func readCSV(path string) ([]packetRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	col := make(map[string]int, len(header))
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}

	required := []string{"received_at", "tag_addr"}
	for _, c := range required {
		if _, ok := col[c]; !ok {
			return nil, fmt.Errorf("csv missing required column %q", c)
		}
	}

	var rows []packetRow
	lineNum := 1
	skipped := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read record at line %d: %w", lineNum+1, err)
		}
		lineNum++

		tagAddr := field(record, col, "tag_addr")
		if tagAddr == "" {
			skipped++
			continue
		}

		receivedAt, err := parseTimestamp(field(record, col, "received_at"))
		if err != nil {
			log.Printf("seed-herd-signals-oci: skipping line %d: invalid received_at: %v", lineNum, err)
			skipped++
			continue
		}

		// The tag's PRINTED id (A00033) is its identity everywhere else in the
		// system — tag_latest, goat_identifiers.normalized_value, the UI. The BLE
		// MAC is a separate field. Keying both off the MAC would build a parallel
		// MAC-keyed universe that maps to no animal.
		printedID := strings.ToUpper(strings.TrimSpace(field(record, col, "printed_id")))
		if printedID == "" {
			skipped++
			continue
		}
		// packet_time is the gateway's own clock and is stored verbatim as
		// gateway_seen_at; it is NOT a fallback for received_at.
		gatewayTime, gwErr := parseTimestamp(field(record, col, "packet_time"))
		if gwErr != nil {
			gatewayTime = time.Time{}
		}
		row := packetRow{
			receivedAt:  receivedAt,
			tagAddr:     strings.ToLower(tagAddr),
			printedID:   printedID,
			gatewayTime: gatewayTime,
		}

		if v, ok := parseInt16(field(record, col, "rssi")); ok {
			row.rssi = &v
		}
		if v, ok := parseBatteryMV(field(record, col, "battery_v")); ok {
			row.batteryMV = &v
		}
		if v, ok := parseFloat64(field(record, col, "temperature_c")); ok {
			row.temperatureC = &v
		}
		if v, ok := parseInt64(field(record, col, "motion_count")); ok {
			row.motionCount = &v
		}
		if v, ok := parseBool(field(record, col, "temp_sensor_ok")); ok {
			row.tempSensorOK = &v
		}
		if v, ok := parseBool(field(record, col, "accel_sensor_ok")); ok {
			row.accelSensorOK = &v
		}
		if v, ok := parseInt16(field(record, col, "sensor_state")); ok {
			row.sensorState = &v
		}
		if v := field(record, col, "adv_raw"); v != "" {
			row.rawAdv = &v
		}

		rows = append(rows, row)
	}

	if skipped > 0 {
		log.Printf("seed-herd-signals-oci: skipped %d malformed rows", skipped)
	}

	return rows, nil
}

func field(record []string, col map[string]int, name string) string {
	idx, ok := col[name]
	if !ok || idx >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[idx])
}

// captureZone is the wall clock the gateway capture files are written in.
// A zone-less timestamp in decoded_ear_tags.csv is Asia/Kolkata local time;
// parsing it as UTC once put every packet 5h30m into the future, which made
// staleness impossible to evaluate.
var captureZone = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return time.FixedZone("IST", 5*60*60+30*60)
	}
	return loc
}()

// parseTimestamp accepts either RFC3339 (zone explicit) or the CSV's zone-less
// "YYYY-MM-DDTHH:MM:SS" form, which is interpreted in captureZone.
func parseTimestamp(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", s, captureZone); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05.999999999", s, captureZone); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp format %q", s)
}

func parseInt16(s string) (int16, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 16)
	if err != nil {
		return 0, false
	}
	return int16(v), true
}

func parseInt64(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parseFloat64(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parseBool(s string) (bool, bool) {
	if s == "" {
		return false, false
	}
	v, err := strconv.ParseBool(strings.ToLower(s))
	if err != nil {
		return false, false
	}
	return v, true
}

// parseBatteryMV converts the CSV's battery_v (volts, e.g. "3.1") into
// millivolts as expected by domain.IngestPacket.Battery.
func parseBatteryMV(s string) (int, bool) {
	v, ok := parseFloat64(s)
	if !ok {
		return 0, false
	}
	return int(v * 1000), true
}
