// Command herd-signals-mqtt-bridge consumes raw BLE scan reports from a Mosquitto broker
// (the gateway's new transport, replacing the old HTTP-forwarding path) and calls the existing
// herdsignals app.Service.IngestPackets -- never a second write path. That "never a second
// write path" rule is not cosmetic: the OCI seed script invented a parallel ingest shape once
// already and produced state that never matched the real one.
//
// Pipeline: tag -> gateway -> Mosquitto broker -> this bridge -> app.Service.IngestPackets -> DB.
//
// THE PAYLOAD (captured live, /Users/ravi/goatos-work/herd-signals-proof/mqtt-sample.json):
// the gateway publishes one scan_report per second to topic GwData, each carrying dev_infos: an
// array of EVERY BLE device the gateway's radio heard, most of them not ours (phones, earbuds,
// Apple continuity beacons, randomized MACs -- the payload audit measured 178,209 non-HoneyComm
// rows across 414 devices). Only devices whose adv_raw carries the HoneyComm signature (a
// Service Data AD structure, type 0x16, 16-bit UUID 0xAB4C) are ear tags; everything else is
// counted and discarded, never stored.
//
// It also publishes pkt_type:"state" heartbeats (data.state == "sta_gw_hb") roughly every 5
// minutes with no dev_infos at all -- those drive herd_signal_gateways.last_seen_at directly, so
// "gateway up but hearing nothing" (heartbeats arriving, zero tags decoded) is distinguishable
// from "gateway down" (no messages of either kind).
//
// TIMESTAMPS (security decision already made elsewhere in this module, restated here because
// this is where it becomes load-bearing): received_at is stamped by app.Service.IngestPackets
// from the SERVER clock at consume time -- this bridge never sets it. The gateway's own per-
// DEVICE time+msec (not the envelope's, which the audit found differs from the device rows by
// 0-1s) is carried through uncorrected as gateway_seen_at/device_seen_at, diagnostic only. A
// live re-measurement during this build found the gateway clock ~+2h33m ahead of the laptop's
// real time -- never used for ordering, staleness, or gap detection.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	herdsignalspg "github.com/vgoats/goatos/backend/internal/herdsignals/adapters/postgres"
	herdsignalsapp "github.com/vgoats/goatos/backend/internal/herdsignals/app"
	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "herd-signals-mqtt-bridge: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

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

	repo := herdsignalspg.NewRepository(pool)
	svc := herdsignalsapp.NewService(repo, log)

	// Machine actor (security review): this bridge authenticates as a service principal, never
	// a human user -- it calls app.Service in-process, so there is no HTTP/RBAC layer to hold a
	// scoped token here, but the actor identity is still distinct from any human UserID and is
	// never one that also carries admin/oversight permissions. When this bridge moves out of
	// process (e.g. behind the HTTP ingest endpoint instead), it must carry a real token scoped
	// to exactly herd_signals.ingest, not this in-process actor.
	actor := domain.Actor{TenantID: cfg.TenantID, UserID: cfg.ServiceActorID}

	b := &bridge{cfg: cfg, log: log, repo: repo, svc: svc, actor: actor, queue: make(chan decodedPacket, cfg.QueueMax)}

	client, err := b.connect()
	if err != nil {
		return fmt.Errorf("mqtt connect: %w", err)
	}
	defer client.Disconnect(250)

	go b.drainLoop(ctx)

	log.Info("herd_signals_mqtt_bridge_running", "broker", cfg.BrokerURL(), "topic", cfg.Topic, "tenant_id", cfg.TenantID)
	<-ctx.Done()
	log.Info("herd_signals_mqtt_bridge_shutting_down")
	b.flush(context.Background())
	return nil
}

// config is entirely env-driven -- no hardcoded broker host/port. Required vars fail fast rather
// than silently defaulting to a broker that isn't the one the operator meant.
type config struct {
	Host           string
	Port           string
	TLS            bool
	Username       string
	Password       string
	ClientID       string
	Topic          string
	TenantID       string
	ServiceActorID string
	DefaultGateway string // used only when a message's own gw_addr cannot be trusted/derived
	BatchSize      int
	BatchInterval  time.Duration
	QueueMax       int
}

func (c config) BrokerURL() string {
	scheme := "tcp"
	if c.TLS {
		scheme = "ssl"
	}
	return fmt.Sprintf("%s://%s:%s", scheme, c.Host, c.Port)
}

func loadConfig() (config, error) {
	c := config{
		Topic:          getenvDefault("HERD_SIGNALS_MQTT_TOPIC", "GwData"),
		ClientID:       getenvDefault("HERD_SIGNALS_MQTT_CLIENT_ID", "herd-signals-mqtt-bridge"),
		ServiceActorID: getenvDefault("HERD_SIGNALS_MQTT_SERVICE_ACTOR_ID", "svc-herd-signals-mqtt-bridge"),
		Username:       os.Getenv("HERD_SIGNALS_MQTT_USERNAME"),
		Password:       os.Getenv("HERD_SIGNALS_MQTT_PASSWORD"),
		BatchSize:      envInt("HERD_SIGNALS_MQTT_BATCH_SIZE", 50),
		BatchInterval:  envDuration("HERD_SIGNALS_MQTT_BATCH_INTERVAL", 2*time.Second),
		QueueMax:       envInt("HERD_SIGNALS_MQTT_QUEUE_MAX", 5000),
		TLS:            os.Getenv("HERD_SIGNALS_MQTT_TLS") == "true",
	}
	c.Host = os.Getenv("HERD_SIGNALS_MQTT_HOST")
	c.Port = getenvDefault("HERD_SIGNALS_MQTT_PORT", "1883")
	c.TenantID = os.Getenv("HERD_SIGNALS_TENANT_ID")
	c.DefaultGateway = os.Getenv("HERD_SIGNALS_DEFAULT_GATEWAY_ID")

	var missing []string
	if c.Host == "" {
		missing = append(missing, "HERD_SIGNALS_MQTT_HOST")
	}
	if c.TenantID == "" {
		missing = append(missing, "HERD_SIGNALS_TENANT_ID")
	}
	if len(missing) > 0 {
		return config{}, fmt.Errorf("required env vars not set: %s", strings.Join(missing, ", "))
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

// decodedPacket is one HoneyComm tag reading plus which gateway it arrived on, queued for
// batched ingest.
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

	// Counters, logged periodically rather than per-message (this is a firehose: a single
	// gateway's scan_report carries ~18 devices/second, nearly all foreign).
	decodedTotal   atomic.Int64
	discardedTotal atomic.Int64
	droppedTotal   atomic.Int64 // queue was full: backpressure, not data loss we pretend didn't happen
	heartbeats     atomic.Int64
}

func (b *bridge) connect() (mqtt.Client, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(b.cfg.BrokerURL()).
		SetClientID(b.cfg.ClientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(2 * time.Second).
		SetMaxReconnectInterval(30 * time.Second).
		SetKeepAlive(30 * time.Second).
		SetOnConnectHandler(func(c mqtt.Client) {
			b.log.Info("mqtt_connected", "broker", b.cfg.BrokerURL())
			token := c.Subscribe(b.cfg.Topic, 1, b.onMessage) // QoS 1: at-least-once, duplicates expected
			token.Wait()
			if err := token.Error(); err != nil {
				b.log.Error("mqtt_subscribe_failed", "topic", b.cfg.Topic, "error", err)
				return
			}
			b.log.Info("mqtt_subscribed", "topic", b.cfg.Topic, "qos", 1)
		}).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			b.log.Warn("mqtt_connection_lost", "error", err)
		}).
		SetReconnectingHandler(func(_ mqtt.Client, _ *mqtt.ClientOptions) {
			b.log.Info("mqtt_reconnecting")
		})

	if b.cfg.Username != "" {
		opts.SetUsername(b.cfg.Username)
		opts.SetPassword(b.cfg.Password)
	}

	client := mqtt.NewClient(opts)
	token := client.Connect()
	token.Wait()
	if err := token.Error(); err != nil {
		return nil, err
	}
	return client, nil
}

// gwEnvelope is the top-level MQTT message shape. Handles both pkt_type "scan_report" (tag
// data) and "state" (heartbeats) -- see the file doc comment.
type gwEnvelope struct {
	PktType string          `json:"pkt_type"`
	GwAddr  string          `json:"gw_addr"`
	Time    string          `json:"time"`
	Msec    string          `json:"msec"`
	Data    json.RawMessage `json:"data"`
}

type scanReportData struct {
	Flags      int             `json:"flags"`
	PktSN      int64           `json:"pkt_sn"`
	ReportType string          `json:"report_type"`
	PktTotal   int             `json:"pkt_total"`
	PktIndex   int             `json:"pkt_index"`
	DevTotal   int             `json:"dev_total"`
	DevNum     int             `json:"dev_num"`
	DevInfos   []devInfo       `json:"dev_infos"`
	State      json.RawMessage `json:"state,omitempty"`
}

type devInfo struct {
	Addr   string `json:"addr"`
	RSSI   int16  `json:"rssi"`
	Time   string `json:"time"`
	Msec   string `json:"msec"`
	Name   string `json:"name"`
	AdvRaw string `json:"adv_raw"`
}

type gatewayHeartbeat struct {
	State    string `json:"state"`
	TicksCnt int64  `json:"ticks_cnt"`
}

// lastPktSN tracks the last pkt_sn seen per gateway so a decrease (gateway reboot -- the only
// packet-loss instrument this protocol gives us) is detected and logged, the same counter-reset
// discipline already applied to a tag's own motion_count.
var lastPktSN = map[string]int64{}

func (b *bridge) onMessage(_ mqtt.Client, msg mqtt.Message) {
	var env gwEnvelope
	if err := json.Unmarshal(msg.Payload(), &env); err != nil {
		b.log.Warn("mqtt_message_unparseable", "error", err, "topic", msg.Topic())
		return
	}

	switch env.PktType {
	case "scan_report":
		b.handleScanReport(env)
	case "state":
		b.handleHeartbeat(env)
	default:
		b.log.Debug("mqtt_message_unknown_pkt_type", "pkt_type", env.PktType)
	}
}

func (b *bridge) handleScanReport(env gwEnvelope) {
	var data scanReportData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		b.log.Warn("scan_report_data_unparseable", "error", err)
		return
	}

	if prev, ok := lastPktSN[env.GwAddr]; ok && data.PktSN < prev {
		b.log.Warn("gateway_pkt_sn_decreased_likely_reboot", "gw_addr", env.GwAddr, "previous_pkt_sn", prev, "new_pkt_sn", data.PktSN)
	}
	lastPktSN[env.GwAddr] = data.PktSN

	gatewayID := env.GwAddr
	if gatewayID == "" {
		gatewayID = b.cfg.DefaultGateway
	}

	for _, dev := range data.DevInfos {
		rawBytes, err := hex.DecodeString(dev.AdvRaw)
		if err != nil {
			b.discardedTotal.Add(1)
			continue
		}
		if !isHoneyCombAdv(rawBytes) {
			b.discardedTotal.Add(1)
			continue
		}
		pkt, ok := decodeHoneyCombPacket(dev, rawBytes, env, data.PktSN)
		if !ok {
			b.discardedTotal.Add(1)
			continue
		}
		b.decodedTotal.Add(1)

		select {
		case b.queue <- decodedPacket{gatewayID: gatewayID, packet: pkt}:
		default:
			// Bounded queue, drop-with-a-counter rather than growing unboundedly if the DB
			// stalls (requirement 5: backpressure, not an unbounded buffer).
			b.droppedTotal.Add(1)
		}
	}
}

func (b *bridge) handleHeartbeat(env gwEnvelope) {
	var data scanReportData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return
	}
	var hb gatewayHeartbeat
	if len(data.State) > 0 {
		_ = json.Unmarshal(data.State, &hb)
	}
	if hb.State != "" && hb.State != "sta_gw_hb" {
		return
	}
	b.heartbeats.Add(1)

	gatewayID := env.GwAddr
	if gatewayID == "" {
		gatewayID = b.cfg.DefaultGateway
	}
	// Persisted through the module's own heartbeat write path (000197) rather than a bare
	// gateway upsert: that path also records last_heartbeat_at (distinct from last_seen_at, so
	// "up but hearing no tags" is distinguishable from "down") and ticks_cnt, and counts a reboot
	// when ticks_cnt goes BACKWARDS -- never a negative, same discipline as motion_count and
	// pkt_sn. The timestamp is stamped inside that path from the server clock.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := domain.GatewayHeartbeatRequest{GatewayID: gatewayID, State: hb.State, TicksCnt: &hb.TicksCnt}
	if _, err := b.repo.RecordGatewayHeartbeat(ctx, b.cfg.TenantID, req, time.Now().UTC()); err != nil {
		b.log.Error("heartbeat_record_failed", "gateway_id", gatewayID, "error", err)
	}
}

// isHoneyCombAdv reports whether the decoded advertisement carries the HoneyComm signature: a
// Service Data (16-bit UUID) AD structure, AD type 0x16, UUID 0xAB4C little-endian -- confirmed
// against the live sample (payload audit + this build's own decode of addr f0c990a00036, name
// "mTnA": adv_raw bytes 3-6 are 15 16 4C AB, i.e. AD length 0x15, type 0x16, UUID bytes 4C AB).
// Walks the AD structures generically (length-prefixed TLV) rather than a fixed-offset check, so
// it still matches if a future firmware reorders the flags/other AD entries before this one.
func isHoneyCombAdv(raw []byte) bool {
	i := 0
	for i+1 < len(raw) {
		adLen := int(raw[i])
		if adLen == 0 || i+1+adLen > len(raw) {
			return false
		}
		adType := raw[i+1]
		adData := raw[i+2 : i+1+adLen]
		if adType == 0x16 && len(adData) >= 2 {
			uuid := uint16(adData[0]) | uint16(adData[1])<<8 // little-endian
			if uuid == 0xAB4C {
				return true
			}
		}
		i += 1 + adLen
	}
	return false
}

// decodeHoneyCombPacket decodes the fixed HoneyComm advertisement layout (payload audit,
// confirmed by this build against the live sample):
//
//	byte 8:     battery, DECIVOLTS (0x1F = 31 = 3.1V)
//	byte 9:     battery percent (observed constant 100)
//	byte 17:    sensor_state
//	byte 18:    temperature, integer part
//	byte 19:    temperature, fractional part -- always a multiple of 10; native resolution 0.1C,
//	            so actual fraction = byte/100 (e.g. 10 -> 0.1C)
//	bytes 21-24: motion_count, 32-bit BIG-ENDIAN cumulative counter
//
// The printed tag id is the device's last 3 MAC bytes, uppercased (addr f0c990a00036 -> A00036 --
// confirmed against a real row in herd_signal_tag_latest during this build).
func decodeHoneyCombPacket(dev devInfo, raw []byte, env gwEnvelope, pktSN int64) (domain.IngestPacket, bool) {
	const minLen = 25 // need index 24 inclusive
	if len(raw) < minLen {
		return domain.IngestPacket{}, false
	}

	tagID, err := printedTagID(dev.Addr)
	if err != nil {
		return domain.IngestPacket{}, false
	}

	batteryMV := int(raw[8]) * 100 // decivolts -> millivolts
	sensorState := int16(raw[17])
	tempInt := int(raw[18])
	tempFrac := int(raw[19])
	tempC := float64(tempInt) + float64(tempFrac)/100.0
	motionCount := int64(uint32(raw[21])<<24 | uint32(raw[22])<<16 | uint32(raw[23])<<8 | uint32(raw[24]))

	// Per-DEVICE gateway time (not the envelope's -- the audit found the per-device row differs
	// from the envelope by 0-1s). Uncorrected: this is the gateway's own clock, stored verbatim,
	// never used for ordering/staleness/gap decisions -- app.Service.IngestPackets stamps
	// received_at from the server clock regardless of what is sent here.
	gatewayTimeStr := formatGatewayTime(dev.Time, dev.Msec)

	sensorOK := true // no fault bit is currently decoded from sensor_state; the raw value is preserved in raw_payload/sensor_state
	advRaw := dev.AdvRaw
	pkt := domain.IngestPacket{
		TagID:                 tagID,
		TagMAC:                dev.Addr,
		RSSI:                  &dev.RSSI,
		Battery:               &batteryMV,
		TagTemperature:        &tempC,
		MotionCount:           &motionCount,
		SensorState:           &sensorState,
		TemperatureSensorOK:   &sensorOK,
		AccelerometerSensorOK: &sensorOK,
		// pkt_sn is now a real COLUMN (migration 000197), not only a jsonb crumb in RawPayload
		// below: it is the only packet-loss instrument this protocol gives us, and inside jsonb
		// it could neither be aggregated nor compared across a bridge restart. It stays in
		// RawPayload too so the stored raw diagnostic record remains complete.
		PktSN:         &pktSN,
		RawAdv:        &advRaw,
		SeenAt:        gatewayTimeStr, // diagnostic only (DeviceSeenAt) -- see file doc comment
		GatewaySeenAt: &gatewayTimeStr,
		RawPayload: map[string]interface{}{
			"gw_addr":          env.GwAddr,
			"gw_envelope_time": env.Time,
			"pkt_sn":           pktSN,
			"dev_addr":         dev.Addr,
			"dev_time":         dev.Time,
			"dev_msec":         dev.Msec,
			"dev_name":         dev.Name,
		},
	}
	return pkt, true
}

// printedTagID takes the last 3 MAC bytes (6 hex chars) of a colon-less lowercase MAC string
// and uppercases them, e.g. "f0c990a00036" -> "A00036".
func printedTagID(addr string) (string, error) {
	addr = strings.ToLower(strings.ReplaceAll(addr, ":", ""))
	if len(addr) != 12 {
		return "", fmt.Errorf("unexpected MAC length %d for %q", len(addr), addr)
	}
	if _, err := hex.DecodeString(addr); err != nil {
		return "", fmt.Errorf("not a valid MAC hex string: %q", addr)
	}
	return strings.ToUpper(addr[6:]), nil
}

// formatGatewayTime combines the gateway's "YYYY-MM-DD HH:MM:SS" + millisecond string into
// RFC3339, treating it as-is (no timezone correction) since it is diagnostic-only and its
// absolute offset from real time is unknown and drifts (payload audit: seen anywhere from
// +2h33m to other values across gateway reboots).
func formatGatewayTime(t, msec string) string {
	base, err := time.Parse("2006-01-02 15:04:05", t)
	if err != nil {
		return ""
	}
	ms, _ := strconv.Atoi(msec)
	return base.Add(time.Duration(ms) * time.Millisecond).UTC().Format(time.RFC3339Nano)
}

// drainLoop batches queued packets by gateway and flushes on size or interval -- never a
// transaction per message (requirement 5).
func (b *bridge) drainLoop(ctx context.Context) {
	ticker := time.NewTicker(b.cfg.BatchInterval)
	defer ticker.Stop()

	statsTicker := time.NewTicker(30 * time.Second)
	defer statsTicker.Stop()

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
		case <-statsTicker.C:
			b.log.Info("herd_signals_mqtt_bridge_stats",
				"decoded_total", b.decodedTotal.Load(),
				"discarded_non_honeycomb_total", b.discardedTotal.Load(),
				"dropped_queue_full_total", b.droppedTotal.Load(),
				"heartbeats_total", b.heartbeats.Load(),
			)
		}
	}
}

func (b *bridge) flush(_ context.Context) {
	// Drain whatever is left in the queue into one final batch per gateway, best-effort.
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
		GatewaySeen: time.Now().UTC().Format(time.RFC3339), // envelope-level relay time fallback; per-packet GatewaySeenAt above is what actually matters
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
