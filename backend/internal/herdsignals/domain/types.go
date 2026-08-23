package domain

import (
	"errors"
	"time"
)

// Thresholds for herd signals analysis. All values are configurable and documented
// as provisional placeholders pending vendor confirmation or operational tuning.
type Thresholds struct {
	// Signal quality thresholds (RSSI in dBm).
	// rssi >= -65 is strong/ok. rssi <= -75 is weak. Average <= -80 is weak.
	SignalStrong  int16 // -65 dBm
	SignalWeak    int16 // -75 dBm
	SignalAvgWeak int16 // -80 dBm for average

	// Battery thresholds (millivolts). ALL PROVISIONAL, pending vendor confirmation of the
	// discharge curve. Replaces a removed remaining-life estimate ("~90 days") that computed
	// fake precision against an assumed discharge curve and advertising interval, and presented
	// it next to real device readings (maintainer decision). The tag reports VOLTAGE only, so the
	// model here is a voltage TREND: an absolute band plus a relative fall against the tag's own
	// history -- the same per-animal-baseline discipline used for motion (compare a tag to
	// itself, not the fleet). See BatteryStateFromVoltage / BatteryTrendFromHistory.
	//
	//   healthy  >= 3.00 V
	//   watch     2.80-2.99 V, OR voltage falling against the tag's own history
	//   low      <  2.80 V
	//   critical <  2.60 V, or the tag went quiet (missing) after voltage was falling
	BatteryHealthyMV  int // >= this is healthy
	BatteryWatchMV    int // >= this (and < BatteryHealthyMV) is watch by absolute band
	BatteryCriticalMV int // < this is critical
	// BatteryFallMV is the minimum drop (mV) over the trend window to call a tag "falling"
	// against its own history -- the RELATIVE half of watch, and the point of this model: a tag
	// drifting 3.18V -> 3.10V is informative long before it crosses BatteryWatchMV. PROVISIONAL:
	// coin-cell voltage is noisy and temperature-sensitive, so this must be large enough that
	// ordinary sensor noise across a day does not read as "falling".
	BatteryFallMV int
	// BatteryTrendWindowDays is the window BatteryTrendFromHistory reads over. Below
	// BatteryTrendMinSpanHours of actual reading span within that window, the trend is
	// null/absent rather than invented from two adjacent, noisy packets.
	BatteryTrendWindowDays   int
	BatteryTrendMinSpanHours float64

	// Motion state thresholds (cumulative motion_count delta over window). Not-moving is
	// hardcoded as delta==0 in MovementStateFromDelta -- there is no lower threshold to
	// configure, so no MotionNotMovingDelta field exists (a config knob with no reader is worse
	// than none, maintainer correctness review defect 6).
	MotionActiveDelta int64 // >= 100 => moving
	MotionLowDelta    int64 // 10-99 => low activity
	MotionQuietDelta  int64 // 1-9 => quiet

	// Stale packet threshold (minutes).
	StalePacketMinutes int // >= 30min with no packet => stale

	// ReceptionGapMinutes is the threshold above which a delta between two consecutive packets
	// for the same tag is a GAP DELTA (maintainer decision on offline behaviour): the gateway
	// does not buffer scan reports through a WAN outage and a tag only broadcasts its CURRENT
	// cumulative motion_count, so a gap this long means the backend received NOTHING for that
	// period and the eventual reconnect delta is a TOTAL with an unknown time distribution, not
	// ordinary in-window movement. Configurable and reused from a single call site
	// (domain.IsGapDelta) rather than hardcoded -- kept as its OWN field, separate from
	// StalePacketMinutes, even though both default to 30, because they answer different
	// questions (is this row too old to trust vs is this delta smeared across a hole) and a
	// future change to one must not silently change the other.
	ReceptionGapMinutes int

	// Motion calculation window (seconds).
	MotionWindowSeconds int // default 60

	// Pattern state thresholds (duration-based).
	// All provisional; pending operational validation.
	QuietWatchDurationMinutes int // 1-2 hours of low delta => quiet_watch
	InactiveDurationMinutes   int // 3+ hours of low delta with packets => inactive
	MissingSignalMinutes      int // 30+ minutes with no packets => missing_signal
	// The baseline percentile (p75) is hardcoded in percentile75/Baseline75 -- AGENTS.md is
	// explicit that p75, not the median, is the required statistic (a resting animal's median
	// bucket is 0), so there is no BaselinePercentile field to configure it away from p75.
	SpikeThresholdMultiplier float64 // how many x baseline = spike
}

// DefaultThresholds returns provisional default thresholds.
// These are operational placeholders and should be tuned based on real data.
func DefaultThresholds() Thresholds {
	return Thresholds{
		SignalStrong:              -65,
		SignalWeak:                -75,
		SignalAvgWeak:             -80,
		BatteryHealthyMV:          3000, // provisional
		BatteryWatchMV:            2800, // provisional
		BatteryCriticalMV:         2600, // provisional
		BatteryFallMV:             80,   // provisional: minimum drop over the trend window to call it "falling"
		BatteryTrendWindowDays:    30,   // provisional
		BatteryTrendMinSpanHours:  24,   // provisional: require at least a day of real spread between first/last reading
		MotionActiveDelta:         100,
		MotionLowDelta:            10,
		MotionQuietDelta:          1,
		StalePacketMinutes:        30,
		ReceptionGapMinutes:       30,
		MotionWindowSeconds:       60,
		QuietWatchDurationMinutes: 90,  // 1.5 hours
		InactiveDurationMinutes:   180, // 3 hours
		MissingSignalMinutes:      30,  // 30 minutes
		SpikeThresholdMultiplier:  2.5, // 2.5x baseline
	}
}

// Gateway represents a BLE gateway registration and status.
type Gateway struct {
	TenantID    string
	GatewayID   string
	Label       string
	ParkID      *string
	ShedID      *string
	LocationID  *string
	WifiMAC     string
	BLEMAC      string
	NetworkMode string
	Status      string // "active" (default), "inactive", "error"
	LastSeenAt  *time.Time
	// LastPktSN is the highest scan-report sequence number carried by the batch being ingested.
	// The repository compares it to the stored value to accrue packet loss (forward jump) or
	// count a reboot (decrease) -- see migration 000198.
	LastPktSN *int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Packet represents a raw BLE advertisement packet.
// Write-once, immutable. Never modify after ingest.
type Packet struct {
	PacketID  string
	TenantID  string
	GatewayID *string
	Source    string // "gateway" (default)
	TagID     string
	TagMAC    *string
	// ReceivedAt is stamped from the SERVER clock at ingest time (security review, HIGH): never
	// caller-supplied. It is the only input to staleness, gap detection, ordering, and the
	// packet dedup identity in this module.
	ReceivedAt time.Time
	// DeviceSeenAt is the caller's own claimed capture timestamp (request field `seen_at`),
	// preserved verbatim for diagnostics ONLY. Never read by any decision -- see ReceivedAt.
	DeviceSeenAt          *time.Time
	GatewaySeenAt         *time.Time
	RSSIdbm               *int16
	BatteryMV             *int
	TagTemperatureC       *float64
	MotionCount           *int64
	SensorState           *int16
	TemperatureSensorOK   *bool
	AccelerometerSensorOK *bool
	// PktSN is the GATEWAY's per-report sequence number for the scan report this packet arrived
	// in -- the ONLY packet-loss instrument this protocol gives us (nothing else says "there was
	// a report between these two you never received"). Previously only stashed inside RawPayload
	// jsonb, where it can neither be aggregated nor compared across process restarts. A DECREASE
	// means the gateway rebooted, handled like the motion counter reset: re-anchor, never a
	// negative loss.
	PktSN      *int64
	RawAdv     *string
	RawPayload map[string]interface{}
	CreatedAt  time.Time
}

// TagLatest represents the per-tag snapshot (latest state).
type TagLatest struct {
	TenantID              string
	TagID                 string
	TagMAC                *string
	GatewayID             *string
	Source                *string
	LastSeenAt            time.Time
	LastRSSIdbm           *int16
	SignalState           string // "strong", "ok", "weak", "unknown"
	BatteryMV             *int
	BatteryState          string // "ok", "low", "unknown"
	TagTemperatureC       *float64
	MotionCount           *int64
	MotionDelta           *int64 // 15-minute window delta (motion_window_seconds=900)
	MotionDelta1h         *int64 // real 1-hour (3600s-tier) delta -- distinct from MotionDelta, see 000194
	PreviousMotionCount   *int64
	PreviousSeenAt        *time.Time
	MotionWindowSeconds   *int
	MovementState         string // "moving", "low", "quiet", "not_moving", "stale", "unknown"
	PatternState          string // "no_movement", "quiet_watch", "inactive", "missing_signal", "spike", "recovered", "unknown"
	TemperatureSensorOK   *bool
	AccelerometerSensorOK *bool
	MappingState          string // "mapped", "unmapped", "conflict"
	// GapDelta is true when MotionDelta (the 15-minute windowed sum) includes a reconnect lump:
	// this ingest's received_at was more than Thresholds.ReceptionGapMinutes after the tag's
	// previous_seen_at, so the delta is a TOTAL over an unknown span, not this window's own
	// movement.
	GapDelta  bool
	UpdatedAt time.Time
}

// ActivityWindow represents a bucketed motion aggregate.
type ActivityWindow struct {
	TenantID         string
	TagID            string
	BucketStart      time.Time
	BucketSeconds    int
	FirstMotionCount *int64
	LastMotionCount  *int64
	MotionDelta      int64
	PacketCount      int
	AvgRSSIdbm       *float64
	MinRSSIdbm       *int16
	MaxRSSIdbm       *int16
	FirstSeenAt      *time.Time
	LastSeenAt       *time.Time
	IsGap            bool // true if PacketCount = 0 (no data in window)
	// GapDelta is true when this bucket carries a RECONNECT delta: the first packet after a
	// reception gap longer than Thresholds.ReceptionGapMinutes. MotionDelta on such a bucket is
	// a TOTAL over the whole gap with unknown time distribution -- it must never feed the p75
	// baseline (see Baseline75) or the spike comparison (see PatternStateFromHistory), and it is
	// never smeared across the buckets the gap spans (those buckets simply have no row: IsGap).
	GapDelta bool
}

// PatternState describes movement pattern over history.
// no_movement: delta 0 in current 15m window
// quiet_watch: low delta sustained for 1-2 hours
// inactive: zero/low delta for 3+ hours WITH packets still arriving (duration-based, not missing)
// missing_signal: no packets for 30+ minutes
// spike: current delta far above animal's own baseline (p75 of 24h)
// recovered: activity resumed after quiet period
type PatternState string

const (
	PatternNoMovement PatternState = "no_movement"
	PatternQuietWatch PatternState = "quiet_watch"
	PatternInactive   PatternState = "inactive"
	// PatternMissingSignal serializes as "missing" (not "missing_signal") to match the
	// admin-web contract fixed at dispatch (apps/admin-web/lib/api/herd-signals.ts
	// HerdSignalPatternState). The Go const name stays descriptive; only the wire value moved.
	PatternMissingSignal PatternState = "missing"
	PatternSpike         PatternState = "spike"
	PatternRecovered     PatternState = "recovered"
	// PatternUnknown serializes as "normal" (not "unknown"): the frontend contract's resting/
	// default state is called "normal", not "unknown" -- this is the state of a tag with no
	// watch condition, which is the common case, not an error condition.
	PatternUnknown PatternState = "normal"
)

// IngestRequest is the payload for POST /herd-signals/packets.
type IngestRequest struct {
	GatewayID   string         `json:"gateway_id"`
	GatewaySeen string         `json:"gateway_seen_at"` // RFC3339 timestamp
	Packets     []IngestPacket `json:"packets"`
}

// IngestPacket is a single packet within an IngestRequest.
type IngestPacket struct {
	TagID                 string   `json:"tag_id"`
	TagMAC                string   `json:"tag_mac"`
	RSSI                  *int16   `json:"rssi_dbm"`
	Battery               *int     `json:"battery_mv"`
	TagTemperature        *float64 `json:"tag_temperature_c"`
	MotionCount           *int64   `json:"motion_count"`
	SensorState           *int16   `json:"sensor_state"`
	TemperatureSensorOK   *bool    `json:"temperature_sensor_ok"`
	AccelerometerSensorOK *bool    `json:"accelerometer_sensor_ok"`
	// PktSN is the gateway scan report's sequence number (see domain.Packet.PktSN). Optional:
	// older firmware and the CSV replay path do not carry one, and a packet with no sequence
	// number is still real sensor data -- it simply cannot contribute to loss accounting.
	PktSN  *int64  `json:"pkt_sn"`
	RawAdv *string `json:"raw_adv"`
	SeenAt string  `json:"seen_at"` // RFC3339 timestamp (server-relevant capture time; received_at is derived from server processing, this is what the caller asserts as when the tag was heard)
	// GatewaySeenAt is this PACKET's own gateway-clock timestamp (RFC3339, uncorrected -- the
	// gateway payload audit found one gateway running a constant +02:30:00 ahead of real IST).
	// Optional: older firmware may only send the envelope-level batch timestamp
	// (IngestRequest.GatewaySeen), in which case that is used as a fallback. Never used to decide
	// gap detection or ordering -- received_at (server clock) is truth for both; this is stored
	// purely as diagnostic/audit data. Previously ABSENT from this struct, which was the bug: the
	// loader collapsed every packet in a batch to the batch's single relay timestamp.
	GatewaySeenAt *string `json:"gateway_seen_at"`
	// RawPayload is optional free-form diagnostic context for this packet (e.g. the MQTT bridge
	// stores gw_addr, pkt_sn, and the raw dev_info fields here). Additive/optional: a caller that
	// omits it gets the previous behaviour (an empty object stored on the row).
	RawPayload map[string]interface{} `json:"raw_payload,omitempty"`
}

// IngestResponse is the response to POST /herd-signals/packets.
type IngestResponse struct {
	Accepted      int    `json:"accepted"`
	Stored        int    `json:"stored"`
	LatestUpdated int    `json:"latest_updated"`
	TraceID       string `json:"trace_id"`
}

// LiveItem is a single tag in the live view response.
type LiveItem struct {
	TagID                      string  `json:"tag_id"`
	TagMAC                     string  `json:"tag_mac"`
	GoatID                     *string `json:"goat_id"`
	DisplayID                  *string `json:"display_id"`
	AnimalIdentifier1          *string `json:"animal_identifier_1"`
	AnimalIdentifier2          *string `json:"animal_identifier_2"`
	ParkID                     *string `json:"park_id"`
	ParkName                   *string `json:"park_name"`
	ShedID                     *string `json:"shed_id"`
	ShedName                   *string `json:"shed_name"`
	PartitionLabel             *string `json:"partition_label"`
	OperationalLocationDisplay *string `json:"operational_location_display"`
	GatewayID                  *string `json:"gateway_id"`
	LastSeenAt                 string  `json:"last_seen_at"` // RFC3339
	RSSIdbm                    *int16  `json:"rssi_dbm"`
	// SignalState/BatteryState/MovementState/PatternState/SensorState are
	// all nullable per the admin-web contract fixed at dispatch (HerdSignalItem in
	// apps/admin-web/lib/api/herd-signals.ts): "unknown"/unset must serialize as JSON null, not
	// as a string value the frontend's enum types do not declare.
	SignalState  *string `json:"signal_state"`
	BatteryMV    *int    `json:"battery_mv"` // Direct reading. Must stay first among the battery fields.
	BatteryState *string `json:"battery_state"`
	// BatteryTrendResponse is the compact voltage trend (direction + the two endpoint readings
	// that justify it), null when there is not enough history to say anything. Never a
	// remaining-life estimate in any unit -- see domain.BatteryTrendFromHistory's doc comment.
	BatteryTrend        *BatteryTrendResponse `json:"battery_trend"`
	TagTemperatureC     *float64              `json:"tag_temperature_c"`
	MotionCount         *int64                `json:"motion_count"`
	MotionDelta         *int64                `json:"motion_delta"`
	MotionDelta1h       *int64                `json:"motion_delta_1h"`
	MotionWindowSeconds *int                  `json:"motion_window_seconds"`
	MovementState       *string               `json:"movement_state"`
	PatternState        *string               `json:"pattern_state"`
	BaselineDelta       *int64                `json:"baseline_delta"`
	// SensorState is a COMPUTED "ok"/"abnormal" summary (contract type HerdSignalSensorState),
	// never the raw device sensor_state int -- that raw value is stored but intentionally not
	// exposed on this endpoint; see herd_signal_tag_latest / herd_signal_packets for the raw bits.
	SensorState           *string `json:"sensor_state"`
	TemperatureSensorOK   *bool   `json:"temperature_sensor_ok"`
	AccelerometerSensorOK *bool   `json:"accelerometer_sensor_ok"`
	MappingState          string  `json:"mapping_state"`
	// GapDelta: true when motion_delta is a reconnect TOTAL across a reception gap (maintainer
	// decision on offline behaviour), not this window's own movement. A client must render this
	// distinctly (e.g. "+239 since reconnect, timing unknown"), never as a normal delta.
	GapDelta bool    `json:"gap_delta"`
	MappedBy *string `json:"mapped_by"` // User ID of the operator who bound this tag
	MappedAt *string `json:"mapped_at"` // RFC3339 timestamp when this tag was bound
}

// BatteryTrendResponse is the wire form of domain.BatteryTrend: a direction plus the two
// endpoint readings that justify it, and the window they were read over. No remaining-life
// estimate in any unit belongs here or anywhere else on this response (maintainer decision).
type BatteryTrendResponse struct {
	Direction  string    `json:"direction"` // "stable" | "falling"
	WindowDays int       `json:"window_days"`
	FirstMV    int       `json:"first_mv"`
	FirstAt    time.Time `json:"first_at"`
	LastMV     int       `json:"last_mv"`
	LastAt     time.Time `json:"last_at"`
}

// LiveResponse is the response to GET /herd-signals/live.
type LiveResponse struct {
	Summary    Summary    `json:"summary"`
	Items      []LiveItem `json:"items"`
	NextCursor *string    `json:"next_cursor"`
}

// Summary is the high-level counts in a live response.
type Summary struct {
	TagsSeen       int `json:"tags_seen"`
	MappedAnimals  int `json:"mapped_animals"`
	UnmappedTags   int `json:"unmapped_tags"`
	Moving         int `json:"moving"`
	Quiet          int `json:"quiet"`
	NotMoving      int `json:"not_moving"`
	Stale          int `json:"stale"`
	WeakSignal     int `json:"weak_signal"`
	LowBattery     int `json:"low_battery"`
	SensorAbnormal int `json:"sensor_abnormal"`
}

// TimelineResponse is the response to GET /herd-signals/tags/{tag_id}/timeline.
// TimelineResponse is the response to GET /herd-signals/tags/{tag_id}/timeline. The wire shape
// is {"buckets": [...]} with no tag_id envelope field -- the admin-web contract fixed at
// dispatch (HerdSignalTimelineResponse in apps/admin-web/lib/api/herd-signals.ts) reads the tag
// from the request path, not the response body.
type TimelineResponse struct {
	TagID   string           `json:"-"`
	Buckets []TimelineWindow `json:"buckets"`
}

// TimelineWindow is a bucketed motion aggregate in a timeline.
type TimelineWindow struct {
	BucketStart      time.Time  `json:"bucket_start"`
	BucketSeconds    int        `json:"bucket_seconds"`
	FirstMotionCount *int64     `json:"first_motion_count"`
	LastMotionCount  *int64     `json:"last_motion_count"`
	MotionDelta      int64      `json:"motion_delta"`
	PacketCount      int        `json:"packet_count"`
	AvgRSSIdbm       *float64   `json:"avg_rssi_dbm"`
	MinRSSIdbm       *int16     `json:"min_rssi_dbm"`
	MaxRSSIdbm       *int16     `json:"max_rssi_dbm"`
	FirstSeenAt      *time.Time `json:"first_seen_at"`
	LastSeenAt       *time.Time `json:"last_seen_at"`
	IsGap            bool       `json:"is_gap"` // true if PacketCount = 0: NO packets received.
	// GapDelta is true only on a RECONNECT bucket: packets WERE received, and motion_delta is a
	// TOTAL across a prior reception gap with unknown time distribution -- distinct from both
	// IsGap (no packets) and an ordinary zero delta (packets received, no movement). A client
	// must be able to render all three as different facts.
	GapDelta bool `json:"gap_delta"`
}

// GatewayItem is a single gateway in the gateways response.
type GatewayItem struct {
	GatewayID               string     `json:"gateway_id"`
	Label                   *string    `json:"label"`
	ParkID                  *string    `json:"park_id"`
	ParkName                *string    `json:"park_name"`
	ShedID                  *string    `json:"shed_id"`
	ShedName                *string    `json:"shed_name"`
	PartitionLabel          *string    `json:"partition_label"`
	LocationDisplay         *string    `json:"operational_location_display"`
	NetworkMode             *string    `json:"network_mode"`
	WifiMAC                 *string    `json:"wifi_mac"`
	BLEMAC                  *string    `json:"ble_mac"`
	LastSeenAt              *time.Time `json:"last_seen_at"`
	Status                  string     `json:"status"`
	TagsSeenRecently        *int       `json:"tags_seen_recently"`
	WeakTags                *int       `json:"weak_tags"`
	UnmappedTags            *int       `json:"unmapped_tags"`
	TagsSeenInWindow        *int       `json:"tags_seen_in_window"`        // 15-minute window aggregate
	DistinctMotionDeltas    *int       `json:"distinct_motion_deltas"`     // 15-minute window aggregate
	PacketsReceivedInWindow *int       `json:"packets_received_in_window"` // 15-minute window aggregate
}

// InsightCard is one card in the GET /herd-signals/insights response. Label, formula, and
// caveat text are backend-owned copy (AGENTS.md backend-owns-labels rule) -- admin-web
// renders exactly what this struct carries and must not hardcode card copy.
type InsightCard struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Value      string `json:"value"`
	Unit       string `json:"unit"`
	SignalType string `json:"signal_type"` // direct|derived|correlated|inferred
	Formula    string `json:"formula"`
	Caveat     string `json:"caveat"`
}

// InsightsResponse is the response to GET /herd-signals/insights.
type InsightsResponse struct {
	Cards []InsightCard `json:"cards"`
}

// GatewaysResponse is the response to GET /herd-signals/gateways.
type GatewaysResponse struct {
	Gateways []GatewayItem `json:"gateways"`
}

// Actor represents the authenticated principal making the request.
type Actor struct {
	TenantID string
	UserID   string
}

// ErrValidation wraps a caller-input validation failure so the HTTP layer can distinguish "bad
// request" from "server error" without string-matching error text (maintainer correctness
// review, defect 7: invalid bucket_seconds/range previously mapped to 500 like a real failure).
// Wrap with fmt.Errorf("...: %w", ErrValidation) and check with errors.Is at the handler.
var ErrValidation = errors.New("herdsignals: validation error")
