package gateway

import (
	"encoding/hex"
	"testing"
)

// TestIsHoneyCombAdvertisement tests the HoneyComm signature detection using the live capture
// sample from /Users/ravi/goatos-work/herd-signals-proof/mqtt-sample.json.
func TestIsHoneyCombAdvertisement(t *testing.T) {
	tests := []struct {
		name    string
		advHex  string // hex-encoded advertisement
		wantOK  bool
		comment string
	}{
		{
			name:    "honeycomb_tag_f0c990a00036",
			advHex:  "02010615164CAB011F6400F0C990A000360A19000000002BA305096D546E41",
			wantOK:  true,
			comment: "real tag from mqtt-sample.json with Service Data type 0x16, UUID 0xAB4C",
		},
		{
			name:    "non_honeycomb_4d29460f8306",
			advHex:  "02011A17FF4C0009081370C0A800051B581608008548CDF9510B4C",
			wantOK:  false,
			comment: "device with 0x16 but NOT UUID 0xAB4C (type 0x17, not 0x16)",
		},
		{
			name:    "non_honeycomb_6d3c432abbb1",
			advHex:  "02011A1BFF4C000C0E08444C73DAD646283FFD78A5DEFF10064E1D12893948",
			wantOK:  false,
			comment: "foreign device from sample with manufacturer data",
		},
		{
			name:    "empty_advertisement",
			advHex:  "",
			wantOK:  false,
			comment: "empty bytes",
		},
		{
			name:    "malformed_truncated",
			advHex:  "020108",
			wantOK:  false,
			comment: "advertisement cut short",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := hex.DecodeString(tt.advHex)
			if err != nil {
				t.Fatalf("failed to decode hex %q: %v", tt.advHex, err)
			}

			got := IsHoneyCombAdvertisement(raw)
			if got != tt.wantOK {
				t.Errorf("IsHoneyCombAdvertisement(%q) = %v, want %v (%s)",
					tt.advHex, got, tt.wantOK, tt.comment)
			}
		})
	}
}

// TestDecodeHoneyCombPacket tests packet decoding using the real tag from mqtt-sample.json.
func TestDecodeHoneyCombPacket(t *testing.T) {
	// Real device from mqtt-sample.json:
	// {"addr":"f0c990a00036","rssi":-65,"time":"2026-08-22 22:30:28","msec":"203",
	//  "name":"mTnA","adv_raw":"02010615164CAB011F6400F0C990A000360A19000000002BA305096D546E41"}

	raw, err := hex.DecodeString("02010615164CAB011F6400F0C990A000360A19000000002BA305096D546E41")
	if err != nil {
		t.Fatalf("failed to decode advertisement hex: %v", err)
	}

	dev := DeviceAdvertisement{
		Addr:   "f0c990a00036",
		RSSI:   -65,
		Time:   "2026-08-22 22:30:28",
		Msec:   "203",
		Name:   "mTnA",
		AdvRaw: "02010615164CAB011F6400F0C990A000360A19000000002BA305096D546E41",
	}

	env := EnvelopeMetadata{
		GatewayAddr: "f130d402dcb4",
		Time:        "2026-08-22 22:30:28",
		Msec:        "714",
		PktSN:       318,
	}

	pkt, ok := DecodeHoneyCombPacket(dev, raw, env)
	if !ok {
		t.Fatal("DecodeHoneyCombPacket returned false, expected true")
	}

	// Verify the tag ID is the last 3 MAC bytes, uppercased
	if pkt.TagID != "A00036" {
		t.Errorf("TagID = %q, want %q", pkt.TagID, "A00036")
	}

	// Verify battery (byte 8: 0x1F = 31 decivolts = 3100 mV)
	if pkt.Battery == nil || *pkt.Battery != 3100 {
		t.Errorf("Battery = %v, want 3100 (mV)", pkt.Battery)
	}

	// Verify temperature parsing (byte 18: 0x19 = 25 int, byte 19: 0x00 frac)
	// 25 + 0/100 = 25.0C
	if pkt.TagTemperature == nil || *pkt.TagTemperature != 25.0 {
		t.Errorf("TagTemperature = %v, want 25.0", pkt.TagTemperature)
	}

	// Verify motion count (bytes 21-24: big-endian, reads as 0x00002BA3 = 11171)
	if pkt.MotionCount == nil || *pkt.MotionCount != 11171 {
		t.Errorf("MotionCount = %v, want 11171", pkt.MotionCount)
	}

	// Verify RSSI
	if pkt.RSSI == nil || *pkt.RSSI != -65 {
		t.Errorf("RSSI = %v, want -65", pkt.RSSI)
	}

	// Verify PktSN is propagated
	if pkt.PktSN == nil || *pkt.PktSN != 318 {
		t.Errorf("PktSN = %v, want 318", pkt.PktSN)
	}

	// Verify gateway metadata is in RawPayload
	if pkt.RawPayload == nil {
		t.Fatal("RawPayload is nil")
	}
	if gwAddr, ok := pkt.RawPayload["gw_addr"]; !ok || gwAddr != "f130d402dcb4" {
		t.Errorf("RawPayload[gw_addr] = %v, want f130d402dcb4", gwAddr)
	}
}

// TestDecodeHoneyCombPacket_TooShort verifies that packets shorter than the minimum
// required length are rejected.
func TestDecodeHoneyCombPacket_TooShort(t *testing.T) {
	raw := make([]byte, 24) // minLen is 25

	dev := DeviceAdvertisement{
		Addr:   "f0c990a00036",
		RSSI:   -65,
		Time:   "2026-08-22 22:30:28",
		Msec:   "203",
		Name:   "mTnA",
		AdvRaw: "xxx",
	}

	env := EnvelopeMetadata{
		GatewayAddr: "f130d402dcb4",
		PktSN:       318,
	}

	_, ok := DecodeHoneyCombPacket(dev, raw, env)
	if ok {
		t.Error("DecodeHoneyCombPacket should reject packets < 25 bytes, got ok=true")
	}
}

// TestPrintedTagID tests MAC-to-tag-ID conversion.
func TestPrintedTagID(t *testing.T) {
	tests := []struct {
		mac     string
		wantID  string
		wantErr bool
	}{
		{"f0c990a00036", "A00036", false},
		{"F0:C9:90:A0:00:36", "A00036", false}, // colon-separated
		{"f0c990a00036", "A00036", false},      // already lowercase
		{"abc", "", true},                      // too short
		{"xxxxxxxxxxxxxxxx", "", true},         // invalid hex
	}

	for _, tt := range tests {
		t.Run(tt.mac, func(t *testing.T) {
			got, err := printedTagID(tt.mac)
			if (err != nil) != tt.wantErr {
				t.Errorf("printedTagID(%q) error = %v, wantErr %v", tt.mac, err, tt.wantErr)
				return
			}
			if err == nil && got != tt.wantID {
				t.Errorf("printedTagID(%q) = %q, want %q", tt.mac, got, tt.wantID)
			}
		})
	}
}
