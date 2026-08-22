package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/herdsignals/app"
	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	tenantID   = "00000000-0000-4000-8000-000000000001"
	gatewayID  = "honeycomm-gateway-001"
	gatewayMAC = "f130d402dcb4"
	csvPath    = "/Users/ravi/mesha/local-data/honeycomm-gateway-capture/decoded_ear_tags.csv"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "read CSV and validate but do not ingest")
	flag.Parse()

	log := slog.Default()

	ctx := context.Background()

	// Connect to OCI database
	pgCfg := platformpg.ConfigFromEnv()
	if pgCfg.DatabaseURL == "" {
		log.Error("DATABASE_URL not set")
		os.Exit(1)
	}

	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		log.Error("failed to connect", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Create repository and service
	repo := postgres.NewRepository(pool, log)
	service := app.NewService(repo, log)

	// Load CSV
	packets, err := loadCSV(csvPath)
	if err != nil {
		log.Error("failed to load CSV", "error", err)
		os.Exit(1)
	}

	log.Info("loaded CSV packets", "count", len(packets))

	if *dryRun {
		log.Info("dry-run mode: CSV loaded successfully")
		os.Exit(0)
	}

	// Ingest through the service (which handles:
	// - proper deduplication via (tenant_id, tag_id, received_at, motion_count) natural key
	// - tag_latest materialization with proper state classification
	// - activity window rollup for all three tiers (60s, 300s, 3600s)
	// - motion_delta computation
	actor := domain.Actor{
		TenantID: tenantID,
		UserID:   "00000000-0000-0000-0000-000000000001", // seed tool
	}

	// Batch packets in groups to avoid overwhelming the service
	batchSize := 500
	for i := 0; i < len(packets); i += batchSize {
		end := i + batchSize
		if end > len(packets) {
			end = len(packets)
		}

		batch := packets[i:end]
		req := domain.IngestRequest{
			GatewayID:   gatewayID,
			GatewaySeen: time.Now().UTC().Format(time.RFC3339),
			Packets:     batch,
		}

		resp, err := service.IngestPackets(ctx, actor, req)
		if err != nil {
			log.Error("ingest failed", "batch_start", i, "batch_end", end, "error", err)
			os.Exit(1)
		}

		log.Info("batch ingested", "batch_start", i, "batch_end", end,
			"stored", resp.Stored, "latest_updated", resp.LatestUpdated)
	}

	log.Info("seed completed successfully", "total_packets", len(packets))
}

// Packet represents a CSV row from decoded_ear_tags.csv
type Packet struct {
	ReceivedAt  string
	GatewayIP   string
	GatewayAddr string
	TagAddr     string
	PrintedID   string
	RSSI        int16
	PacketTime  string
	BatteryV    float32
	TemperatureC float32
	MotionCount int64
	TempSensorOK  bool
	AccelSensorOK bool
	SensorState   int16
	AdvRaw      string
}

func loadCSV(path string) ([]domain.Packet, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open CSV: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read CSV: %w", err)
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("CSV has no data rows (only header)")
	}

	result := make([]domain.Packet, 0, len(records)-1)

	for i, record := range records[1:] {
		if len(record) < 14 {
			continue // skip malformed rows
		}

		rssi, err := strconv.ParseInt(record[5], 10, 16)
		if err != nil {
			rssi = 0
		}

		batteryV, err := strconv.ParseFloat(record[7], 32)
		if err != nil {
			batteryV = 0
		}
		batteryMV := int(batteryV * 1000)

		tempC, err := strconv.ParseFloat(record[8], 32)
		if err != nil {
			tempC = 0
		}

		motionCount, err := strconv.ParseInt(record[9], 10, 64)
		if err != nil {
			motionCount = 0
		}

		tempOK := strings.ToLower(strings.TrimSpace(record[10])) == "true"
		accelOK := strings.ToLower(strings.TrimSpace(record[11])) == "true"

		sensorState, err := strconv.ParseInt(record[12], 10, 16)
		if err != nil {
			sensorState = 0
		}

		// Parse timestamps
		receivedAt, err := time.Parse(time.RFC3339, record[0])
		if err != nil {
			// Try alternative format
			receivedAt, err = time.Parse("2006-01-02T15:04:05", record[0])
			if err != nil {
				continue // skip unparseable timestamps
			}
		}

		gatewaySeen, err := time.Parse("2006-01-02 15:04:05.000000", record[6])
		if err != nil {
			// Use received_at if gateway_seen_at is unparseable
			gatewaySeen = receivedAt
		}

		dp := domain.Packet{
			TenantID:              tenantID,
			GatewayID:             &gatewayID,
			Source:                "gateway",
			TagID:                 record[4], // printed_id (e.g., "A0002A")
			TagMAC:                &record[3], // tag_addr (e.g., "f0c990a0002a")
			ReceivedAt:            receivedAt,
			GatewaySeenAt:         &gatewaySeen,
			RSSIdbm:               int16(rssi),
			BatteryMV:             batteryMV,
			TagTemperatureC:       float32(tempC),
			MotionCount:           motionCount,
			SensorState:           int16(sensorState),
			TemperatureSensorOK:   tempOK,
			AccelerometerSensorOK: accelOK,
			RawAdv:                record[13],
			RawPayload:            map[string]interface{}{},
		}

		result = append(result, dp)

		if (i + 1) % 1000 == 0 {
			slog.Info("loaded CSV rows", "count", i+1)
		}
	}

	return result, nil
}
