// Command herd-signals-udp-bridge ingests raw BLE scan reports from a UDP gateway
// and calls app.Service.IngestPackets -- never a second write path. This bridge
// follows the same batching, backpressure, and ingestion contract as the MQTT bridge.
//
// UDP is unauthenticated and unordered (no delivery guarantee, no source authentication).
// Payloads are decoded from the known JSON envelope (same as MQTT) until the real shape
// is confirmed; any unparseable datagram is logged as a hex sample and counted, rather
// than crashing or silently dropping.
//
// Pipeline: tag -> gateway -> UDP -> this bridge -> app.Service.IngestPackets -> DB.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	herdsignalspg "github.com/vgoats/goatos/backend/internal/herdsignals/adapters/postgres"
	herdsignalsapp "github.com/vgoats/goatos/backend/internal/herdsignals/app"
	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/gateway"
	"github.com/vgoats/goatos/backend/internal/platform/buildinfo"
	"github.com/vgoats/goatos/backend/internal/platform/migrationguard"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "herd-signals-udp-bridge: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	log := observability.New(observability.Config{Service: "herd-signals-udp-bridge"})

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	// Fail fast on migration drift: if the database has migrations this binary
	// doesn't know about, refuse to start and don't bind the socket. This prevents
	// the bridge from silently starting and then crashing on the first packet when
	// it encounters a missing schema element (e.g. column pkt_sn does not exist).
	binaryMigrationVersion, err := migrationguard.BinaryVersion()
	if err != nil {
		return err
	}
	dbMigrationVersion, err := migrationguard.AppliedVersion(ctx, pool)
	if err != nil {
		return err
	}
	status, err := migrationguard.Check(dbMigrationVersion, binaryMigrationVersion)
	if err != nil {
		if status.DBAhead {
			log.Error("migration_drift_dbahead_fatal",
				slog.String("db_migration_version", dbMigrationVersion),
				slog.String("binary_migration_version", binaryMigrationVersion),
				slog.String("binary_version", buildinfo.Current()),
				slog.String("error", err.Error()))
		}
		return err
	}

	repo := herdsignalspg.NewRepository(pool)
	svc := herdsignalsapp.NewService(repo, log)

	// Machine actor (see MQTT bridge for actor contract)
	actor := domain.Actor{TenantID: cfg.TenantID, UserID: cfg.ServiceActorID}

	b := &bridge{
		cfg:   cfg,
		log:   log,
		repo:  repo,
		svc:   svc,
		actor: actor,
		queue: make(chan decodedPacket, cfg.QueueMax),
	}

	// Start drain loop before listening
	go b.drainLoop(ctx)

	// Start UDP listener
	addr := net.UDPAddr{
		Port: cfg.Port,
		IP:   net.ParseIP(cfg.Host),
	}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		return fmt.Errorf("udp listen: %w", err)
	}
	defer conn.Close()

	go b.recvLoop(ctx, conn)

	log.Info("herd_signals_udp_bridge_running", "addr", addr.String(), "tenant_id", cfg.TenantID)
	<-ctx.Done()
	log.Info("herd_signals_udp_bridge_shutting_down")
	b.flush(context.Background())
	return nil
}

type config struct {
	Host           string
	Port           int
	TenantID       string
	ServiceActorID string
	DefaultGateway string // used when a datagram's own gw_addr cannot be trusted/derived
	BatchSize      int
	BatchInterval  time.Duration
	QueueMax       int
}

func loadConfig() (config, error) {
	c := config{
		ServiceActorID: getenvDefault("HERD_SIGNALS_UDP_SERVICE_ACTOR_ID", "svc-herd-signals-udp-bridge"),
		BatchSize:      envInt("HERD_SIGNALS_UDP_BATCH_SIZE", 50),
		BatchInterval:  envDuration("HERD_SIGNALS_UDP_BATCH_INTERVAL", 2*time.Second),
		QueueMax:       envInt("HERD_SIGNALS_UDP_QUEUE_MAX", 5000),
	}
	c.Host = getenvDefault("HERD_SIGNALS_UDP_HOST", "0.0.0.0")
	c.Port = envInt("HERD_SIGNALS_UDP_PORT", 5555)
	c.TenantID = os.Getenv("HERD_SIGNALS_TENANT_ID")
	c.DefaultGateway = os.Getenv("HERD_SIGNALS_DEFAULT_GATEWAY_ID")

	if c.TenantID == "" {
		return config{}, fmt.Errorf("required env var not set: HERD_SIGNALS_TENANT_ID")
	}
	return c, nil
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

type decodedPacket struct {
	gatewayID string
	packet    domain.IngestPacket
}

type bridge struct {
	cfg   config
	log   *slog.Logger
	repo  *herdsignalspg.Repository
	svc   *herdsignalsapp.Service
	actor domain.Actor
	queue chan decodedPacket

	// Counters, logged periodically
	decodedTotal      atomic.Int64
	discardedTotal    atomic.Int64
	droppedTotal      atomic.Int64 // queue full
	unparseableTotal  atomic.Int64
	unparseableSample atomic.Value // []byte, last unparse sample
}

// recvLoop reads UDP datagrams and queues decoded packets. It does not block on
// unparseable data; instead it logs samples and counts them.
func (b *bridge) recvLoop(ctx context.Context, conn *net.UDPConn) {
	statsTicker := time.NewTicker(30 * time.Second)
	defer statsTicker.Stop()

	buffer := make([]byte, 4096) // enough for a reasonable UDP datagram

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Non-blocking deadline so context cancellation can interrupt
		conn.SetReadDeadline(time.Now().Add(time.Second))
		n, remoteAddr, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue // deadline exceeded; check context
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, net.ErrClosed) {
				return
			}
			b.log.Warn("udp_read_error", "error", err, "remote", remoteAddr)
			continue
		}

		// Copy the datagram (buffer is reused)
		data := make([]byte, n)
		copy(data, buffer[:n])

		// Parse and queue
		b.handleDatagram(data, remoteAddr)

		// Log stats periodically
		select {
		case <-statsTicker.C:
			sample := ""
			if v := b.unparseableSample.Load(); v != nil {
				sample = v.(string)
			}
			b.log.Info("herd_signals_udp_bridge_stats",
				"decoded_total", b.decodedTotal.Load(),
				"discarded_non_honeycomb_total", b.discardedTotal.Load(),
				"dropped_queue_full_total", b.droppedTotal.Load(),
				"unparseable_total", b.unparseableTotal.Load(),
				"unparseable_sample_first_bytes", sample,
			)
		default:
		}
	}
}

// gwEnvelope (from MQTT bridge) is the expected top-level UDP payload shape.
type gwEnvelope struct {
	PktType string          `json:"pkt_type"`
	GwAddr  string          `json:"gw_addr"`
	Time    string          `json:"time"`
	Msec    string          `json:"msec"`
	Data    json.RawMessage `json:"data"`
}

type scanReportData struct {
	Flags    int       `json:"flags"`
	PktSN    int64     `json:"pkt_sn"`
	DevInfos []devInfo `json:"dev_infos"`
}

type devInfo struct {
	Addr   string `json:"addr"`
	RSSI   int16  `json:"rssi"`
	Time   string `json:"time"`
	Msec   string `json:"msec"`
	Name   string `json:"name"`
	AdvRaw string `json:"adv_raw"`
}

// handleDatagram parses a UDP datagram as JSON and queues any HoneyComm packets.
// Unparseable datagrams are logged with a hex sample and counted, never crashing.
func (b *bridge) handleDatagram(data []byte, remoteAddr *net.UDPAddr) {
	var env gwEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		b.unparseableTotal.Add(1)

		// Log first 32 bytes of the unparseable datagram as hex for diagnosis
		sampleLen := 32
		if len(data) < sampleLen {
			sampleLen = len(data)
		}
		sample := hex.EncodeToString(data[:sampleLen])
		b.unparseableSample.Store(sample)

		b.log.Warn("udp_datagram_unparseable",
			"error", err,
			"datagram_len", len(data),
			"first_bytes_hex", sample,
			"remote", remoteAddr,
		)
		return
	}

	// Only handle scan_report pkt_type (not heartbeats, which UDP may not send)
	if env.PktType != "scan_report" {
		b.log.Debug("udp_datagram_unknown_pkt_type", "pkt_type", env.PktType)
		return
	}

	var data_ scanReportData
	if err := json.Unmarshal(env.Data, &data_); err != nil {
		b.log.Warn("udp_scan_report_data_unparseable", "error", err)
		return
	}

	gatewayID := env.GwAddr
	if gatewayID == "" {
		gatewayID = b.cfg.DefaultGateway
	}

	for _, dev := range data_.DevInfos {
		rawBytes, err := hex.DecodeString(dev.AdvRaw)
		if err != nil {
			b.discardedTotal.Add(1)
			continue
		}
		if !gateway.IsHoneyCombAdvertisement(rawBytes) {
			b.discardedTotal.Add(1)
			continue
		}

		// Use shared gateway decoder
		pkt, ok := gateway.DecodeHoneyCombPacket(
			gateway.DeviceAdvertisement{
				Addr:   dev.Addr,
				RSSI:   dev.RSSI,
				Time:   dev.Time,
				Msec:   dev.Msec,
				Name:   dev.Name,
				AdvRaw: dev.AdvRaw,
			},
			rawBytes,
			gateway.EnvelopeMetadata{
				GatewayAddr: env.GwAddr,
				Time:        env.Time,
				Msec:        env.Msec,
				PktSN:       data_.PktSN,
			},
		)
		if !ok {
			b.discardedTotal.Add(1)
			continue
		}
		b.decodedTotal.Add(1)

		select {
		case b.queue <- decodedPacket{gatewayID: gatewayID, packet: pkt}:
		default:
			b.droppedTotal.Add(1)
		}
	}
}

// drainLoop batches queued packets by gateway and flushes on size or interval.
func (b *bridge) drainLoop(ctx context.Context) {
	ticker := time.NewTicker(b.cfg.BatchInterval)
	defer ticker.Stop()

	batches := map[string][]domain.IngestPacket{}
	flushAll := func() {
		for gatewayID, pkts := range batches {
			if len(pkts) == 0 {
				continue
			}
			b.ingestBatch(gatewayID, pkts)
			delete(batches, gatewayID)
		}
	}

	for {
		select {
		case <-ctx.Done():
			flushAll()
			return
		case dp := <-b.queue:
			batches[dp.gatewayID] = append(batches[dp.gatewayID], dp.packet)
			if len(batches[dp.gatewayID]) >= b.cfg.BatchSize {
				b.ingestBatch(dp.gatewayID, batches[dp.gatewayID])
				delete(batches, dp.gatewayID)
			}
		case <-ticker.C:
			flushAll()
		}
	}
}

func (b *bridge) flush(_ context.Context) {
	batches := map[string][]domain.IngestPacket{}
	for {
		select {
		case dp := <-b.queue:
			batches[dp.gatewayID] = append(batches[dp.gatewayID], dp.packet)
		default:
			for gatewayID, pkts := range batches {
				if len(pkts) > 0 {
					b.ingestBatch(gatewayID, pkts)
				}
			}
			return
		}
	}
}

func (b *bridge) ingestBatch(gatewayID string, pkts []domain.IngestPacket) {
	if len(pkts) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req := domain.IngestRequest{
		GatewayID:   gatewayID,
		GatewaySeen: time.Now().UTC().Format(time.RFC3339),
		Packets:     pkts,
	}

	resp, err := b.svc.IngestPackets(ctx, b.actor, req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			b.log.Error("ingest_batch_timeout", "gateway_id", gatewayID, "packet_count", len(pkts))
		} else {
			b.log.Error("ingest_batch_failed", "gateway_id", gatewayID, "packet_count", len(pkts), "error", err)
		}
		return
	}
	b.log.Info("ingest_batch_ok", "gateway_id", gatewayID, "accepted", resp.Accepted, "stored", resp.Stored, "latest_updated", resp.LatestUpdated, "trace_id", resp.TraceID)
}
