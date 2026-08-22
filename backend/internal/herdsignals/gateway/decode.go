// Package gateway provides shared payload decoding for HoneyComm tag advertisements
// received over multiple transports (MQTT, UDP, etc.). This ensures both transports
// decode identically: same signature validation, same advertisement parsing, same
// ingest data structure. See cmd/herd-signals-mqtt-bridge/main.go for the contract.
package gateway

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
)

// DeviceAdvertisement is a single device's BLE advertisement as received from a gateway.
type DeviceAdvertisement struct {
	Addr   string // colon-separated or colon-less MAC address
	RSSI   int16
	Time   string // gateway time "YYYY-MM-DD HH:MM:SS"
	Msec   string // milliseconds as string
	Name   string
	AdvRaw string // hex-encoded advertisement bytes
}

// EnvelopeMetadata is the envelope-level information from a gateway message.
type EnvelopeMetadata struct {
	GatewayAddr string // gateway MAC address
	Time        string // "YYYY-MM-DD HH:MM:SS"
	Msec        string // milliseconds
	PktSN       int64  // packet sequence number for loss detection
}

// IsHoneyCombAdvertisement reports whether the advertisement carries the HoneyComm
// signature: a Service Data (16-bit UUID) AD structure, AD type 0x16, UUID 0xAB4C
// little-endian. Walks the AD structures generically (length-prefixed TLV) to remain
// robust against firmware reordering.
func IsHoneyCombAdvertisement(rawBytes []byte) bool {
	i := 0
	for i+1 < len(rawBytes) {
		adLen := int(rawBytes[i])
		if adLen == 0 || i+1+adLen > len(rawBytes) {
			return false
		}
		adType := rawBytes[i+1]
		adData := rawBytes[i+2 : i+1+adLen]
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

// DecodeHoneyCombPacket decodes a HoneyComm advertisement from a device.
// Returns the IngestPacket ready for app.Service.IngestPackets, or (zero, false)
// if the advertisement is malformed or too short.
//
// The HoneyComm advertisement layout (confirmed against live capture):
//
//	byte 8:     battery, DECIVOLTS (0x1F = 31 = 3.1V)
//	byte 9:     battery percent (observed constant 100)
//	byte 17:    sensor_state
//	byte 18:    temperature, integer part
//	byte 19:    temperature, fractional part (0.1C resolution, e.g. 10 -> 0.1C, stored as fraction/100)
//	bytes 21-24: motion_count, 32-bit BIG-ENDIAN cumulative counter
//
// The printed tag ID is derived from the device's MAC address (last 3 bytes, uppercased).
// The gateway_seen_at timestamp is uncorrected (diagnostic only, never used for ordering).
func DecodeHoneyCombPacket(dev DeviceAdvertisement, rawBytes []byte, env EnvelopeMetadata) (domain.IngestPacket, bool) {
	const minLen = 25 // need index 24 inclusive
	if len(rawBytes) < minLen {
		return domain.IngestPacket{}, false
	}

	tagID, err := printedTagID(dev.Addr)
	if err != nil {
		return domain.IngestPacket{}, false
	}

	batteryMV := int(rawBytes[8]) * 100 // decivolts -> millivolts
	sensorState := int16(rawBytes[17])
	tempInt := int(rawBytes[18])
	tempFrac := int(rawBytes[19])
	tempC := float64(tempInt) + float64(tempFrac)/100.0
	motionCount := int64(uint32(rawBytes[21])<<24 | uint32(rawBytes[22])<<16 | uint32(rawBytes[23])<<8 | uint32(rawBytes[24]))

	// Per-DEVICE gateway time (uncorrected, diagnostic-only, never used for ordering/staleness).
	// app.Service.IngestPackets stamps received_at from the server clock regardless of what
	// is sent here.
	gatewayTimeStr := formatGatewayTime(dev.Time, dev.Msec)

	sensorOK := true // no fault bit is currently decoded; raw value is preserved in RawPayload
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
		PktSN:                 &env.PktSN,
		RawAdv:                &advRaw,
		SeenAt:                gatewayTimeStr,
		GatewaySeenAt:         &gatewayTimeStr,
		RawPayload: map[string]interface{}{
			"gw_addr": env.GatewayAddr,
			"pkt_sn":  env.PktSN,
		},
	}
	return pkt, true
}

// printedTagID takes the last 3 MAC bytes (6 hex chars) of a colon-less, lowercase
// MAC string and uppercases them (e.g. "f0c990a00036" -> "A00036").
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

// formatGatewayTime combines the gateway's "YYYY-MM-DD HH:MM:SS" + millisecond string
// into RFC3339, treating it as-is (no timezone correction) since it is diagnostic-only
// and its offset from real time drifts (observed +2h33m in testing, variable across reboots).
func formatGatewayTime(t, msec string) string {
	base, err := time.Parse("2006-01-02 15:04:05", t)
	if err != nil {
		return ""
	}
	ms, _ := strconv.Atoi(msec)
	return base.Add(time.Duration(ms) * time.Millisecond).UTC().Format(time.RFC3339Nano)
}
