package domain

import "time"

// Thresholds for herd signals analysis. All values are configurable and documented
// as provisional placeholders pending vendor confirmation or operational tuning.
type Thresholds struct {
	// Signal quality thresholds (RSSI in dBm).
	// rssi >= -65 is strong/ok. rssi <= -75 is weak. Average <= -80 is weak.
	SignalStrong  int16 // -65 dBm
	SignalWeak    int16 // -75 dBm
	SignalAvgWeak int16 // -80 dBm for average

	// Battery thresholds (millivolts).
	// < 2800 mV is low battery. Provisional; pending vendor confirmation.
	BatteryLowMV int
	// BatteryNominalFullMV / BatteryNominalLifeDays anchor the linear, PROVISIONAL
	// battery_life_estimate shown on the live view. Pending vendor discharge-curve data.
	BatteryNominalFullMV   int
	BatteryNominalLifeDays int

	// Motion state thresholds (cumulative motion_count delta over window).
	MotionActiveDelta    int64 // >= 100 => moving
	MotionLowDelta       int64 // 10-99 => low activity
	MotionQuietDelta     int64 // 1-9 => quiet
	MotionNotMovingDelta int64 // 0 => not moving

	// Stale packet threshold (minutes).
	StalePacketMinutes int // >= 30min with no packet => stale

	// Motion calculation window (seconds).
	MotionWindowSeconds int // default 60

	// Pattern state thresholds (duration-based).
	// All provisional; pending operational validation.
	QuietWatchDurationMinutes int     // 1-2 hours of low delta => quiet_watch
	InactiveDurationMinutes   int     // 3+ hours of low delta with packets => inactive
	MissingSignalMinutes      int     // 30+ minutes with no packets => missing_signal
	BaselinePercentile        int     // p75 of 24h non-gap buckets (not median)
	SpikeThresholdMultiplier  float64 // how many x baseline = spike
}

// DefaultThresholds returns provisional default thresholds.
// These are operational placeholders and should be tuned based on real data.
func DefaultThresholds() Thresholds {
	return Thresholds{
		SignalStrong:              -65,
		SignalWeak:                -75,
		SignalAvgWeak:             -80,
		BatteryLowMV:              2800,
		BatteryNominalFullMV:      3000, // provisional: fresh CR2032-class coin cell
		BatteryNominalLifeDays:    90,   // provisional: pending vendor discharge-curve data
		MotionActiveDelta:         100,
		MotionLowDelta:            10,
		MotionQuietDelta:          1,
		MotionNotMovingDelta:      0,
		StalePacketMinutes:        30,
		MotionWindowSeconds:       60,
		QuietWatchDurationMinutes: 90,  // 1.5 hours
		InactiveDurationMinutes:   180, // 3 hours
		MissingSignalMinutes:      30,  // 30 minutes
		BaselinePercentile:        75,  // p75 of 24h non-gap buckets
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
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Packet represents a raw BLE advertisement packet.
// Write-once, immutable. Never modify after ingest.
type Packet struct {
	PacketID              string
	TenantID              string
	GatewayID             *string
	Source                string // "gateway" (default)
	TagID                 string
	TagMAC                *string
	ReceivedAt            time.Time
	GatewaySeenAt         *time.Time
	RSSIdbm               *int16
	BatteryMV             *int
	TagTemperatureC       *float64
	MotionCount           *int64
	SensorState           *int16
	TemperatureSensorOK   *bool
	AccelerometerSensorOK *bool
	RawAdv                *string
	RawPayload            map[string]interface{}
	CreatedAt             time.Time
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
	MotionDelta           *int64
	PreviousMotionCount   *int64
	PreviousSeenAt        *time.Time
	MotionWindowSeconds   *int
	MovementState         string // "moving", "low", "quiet", "not_moving", "stale", "unknown"
	PatternState          string // "no_movement", "quiet_watch", "inactive", "missing_signal", "spike", "recovered", "unknown"
	TemperatureSensorOK   *bool
	AccelerometerSensorOK *bool
	MappingState          string // "mapped", "unmapped", "conflict"
	UpdatedAt             time.Time
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
	PatternNoMovement    PatternState = "no_movement"
	PatternQuietWatch    PatternState = "quiet_watch"
	PatternInactive      PatternState = "inactive"
	PatternMissingSignal PatternState = "missing_signal"
	PatternSpike         PatternState = "spike"
	PatternRecovered     PatternState = "recovered"
	PatternUnknown       PatternState = "unknown"
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
	RawAdv                *string  `json:"raw_adv"`
	SeenAt                string   `json:"seen_at"` // RFC3339 timestamp
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
	TagID                      string   `json:"tag_id"`
	TagMAC                     string   `json:"tag_mac"`
	GoatID                     *string  `json:"goat_id"`
	DisplayID                  *string  `json:"display_id"`
	ParkID                     *string  `json:"park_id"`
	ParkName                   *string  `json:"park_name"`
	ShedID                     *string  `json:"shed_id"`
	ShedName                   *string  `json:"shed_name"`
	PartitionLabel             *string  `json:"partition_label"`
	OperationalLocationDisplay *string  `json:"operational_location_display"`
	GatewayID                  *string  `json:"gateway_id"`
	LastSeenAt                 string   `json:"last_seen_at"` // RFC3339
	RSSIdbm                    *int16   `json:"rssi_dbm"`
	SignalState                string   `json:"signal_state"`
	BatteryMV                  *int     `json:"battery_mv"`
	BatteryState               string   `json:"battery_state"`
	BatteryLifeEstimate        string   `json:"battery_life_estimate"`
	TagTemperatureC            *float64 `json:"tag_temperature_c"`
	MotionCount                *int64   `json:"motion_count"`
	MotionDelta                *int64   `json:"motion_delta"`
	MotionDelta1h              *int64   `json:"motion_delta_1h"`
	MotionWindowSeconds        *int     `json:"motion_window_seconds"`
	MovementState              string   `json:"movement_state"`
	PatternState               string   `json:"pattern_state"`
	BaselineDelta              *int64   `json:"baseline_delta"`
	SensorState                *int16   `json:"sensor_state"`
	TemperatureSensorOK        *bool    `json:"temperature_sensor_ok"`
	AccelerometerSensorOK      *bool    `json:"accelerometer_sensor_ok"`
	MappingState               string   `json:"mapping_state"`
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
type TimelineResponse struct {
	TagID   string           `json:"tag_id"`
	Windows []TimelineWindow `json:"windows"`
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
	IsGap            bool       `json:"is_gap"` // true if PacketCount = 0
}

// GatewayItem is a single gateway in the gateways response.
type GatewayItem struct {
	GatewayID        string     `json:"gateway_id"`
	Label            *string    `json:"label"`
	ParkID           *string    `json:"park_id"`
	ParkName         *string    `json:"park_name"`
	ShedID           *string    `json:"shed_id"`
	ShedName         *string    `json:"shed_name"`
	PartitionLabel   *string    `json:"partition_label"`
	LocationDisplay  *string    `json:"operational_location_display"`
	NetworkMode      *string    `json:"network_mode"`
	WifiMAC          *string    `json:"wifi_mac"`
	BLEMAC           *string    `json:"ble_mac"`
	LastSeenAt       *time.Time `json:"last_seen_at"`
	Status           string     `json:"status"`
	TagsSeenRecently int        `json:"tags_seen_recently"`
	WeakTags         int        `json:"weak_tags"`
	UnmappedTags     int        `json:"unmapped_tags"`
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
