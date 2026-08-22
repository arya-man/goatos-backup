package main

import "testing"

// TestDecodeRealHoneyCombSample proves the decode against the ACTUAL captured sample
// (/Users/ravi/goatos-work/herd-signals-proof/mqtt-sample.json, addr f0c990a00036, name "mTnA"):
// adv_raw "02010615164CAB011F6400F0C990A000360A19000000002BA305096D546E41" must decode to
// tag_id A00036 (a real row confirmed in herd_signal_tag_latest during this build), battery
// 3100mV (0x1F decivolts), sensor_state 10, temperature 25.0C, and motion_count 11171 (0x2BA3),
// which matched the live database's stored motion_count for A00036 at capture time.
func TestDecodeRealHoneyCombSample(t *testing.T) {
	const advRawHex = "02010615164CAB011F6400F0C990A000360A19000000002BA305096D546E41"
	raw, err := hexDecode(advRawHex)
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}

	if !isHoneyCombAdv(raw) {
		t.Fatal("isHoneyCombAdv = false, want true for the real sample")
	}

	dev := devInfo{Addr: "f0c990a00036", RSSI: -65, Time: "2026-08-22 22:30:28", Msec: "203", Name: "mTnA", AdvRaw: advRawHex}
	env := gwEnvelope{PktType: "scan_report", GwAddr: "f130d402dcb4", Time: "2026-08-22 22:30:28", Msec: "714"}

	pkt, ok := decodeHoneyCombPacket(dev, raw, env, 318)
	if !ok {
		t.Fatal("decodeHoneyCombPacket returned ok=false")
	}

	if pkt.TagID != "A00036" {
		t.Errorf("TagID = %q, want A00036", pkt.TagID)
	}
	if pkt.Battery == nil || *pkt.Battery != 3100 {
		t.Errorf("Battery = %v, want 3100 (0x1F decivolts = 3.1V)", pkt.Battery)
	}
	if pkt.SensorState == nil || *pkt.SensorState != 10 {
		t.Errorf("SensorState = %v, want 10", pkt.SensorState)
	}
	if pkt.TagTemperature == nil || *pkt.TagTemperature != 25.0 {
		t.Errorf("TagTemperature = %v, want 25.0", pkt.TagTemperature)
	}
	if pkt.MotionCount == nil || *pkt.MotionCount != 11171 {
		t.Errorf("MotionCount = %v, want 11171 (0x2BA3, matched the live DB row for A00036 at capture time)", pkt.MotionCount)
	}
	if pkt.TagMAC != "f0c990a00036" {
		t.Errorf("TagMAC = %q, want f0c990a00036", pkt.TagMAC)
	}
}

// TestNonHoneyCombDevicesAreFiltered proves the filter discards the OTHER devices in the same
// real sample (an Apple device and a phone), which is the load-bearing requirement -- the audit
// measured 178,209 non-HoneyComm rows across 414 devices in the CSV path this replaces.
func TestNonHoneyCombDevicesAreFiltered(t *testing.T) {
	samples := []string{
		"02011A020A0C11FF4C000F08C00A2A141400040D10020F04", // Apple continuity beacon
		"020108", // bare flags-only advertisement
		"02011A17FF4C0009081370C0A800051B581608008548CDF9510B4C", // another Apple device
	}
	for _, hexStr := range samples {
		raw, err := hexDecode(hexStr)
		if err != nil {
			t.Fatalf("hex decode %q: %v", hexStr, err)
		}
		if isHoneyCombAdv(raw) {
			t.Errorf("isHoneyCombAdv(%q) = true, want false (not a HoneyComm signature)", hexStr)
		}
	}
}

func TestPrintedTagID(t *testing.T) {
	got, err := printedTagID("f0c990a00036")
	if err != nil {
		t.Fatalf("printedTagID: %v", err)
	}
	if got != "A00036" {
		t.Errorf("got %q, want A00036", got)
	}

	if _, err := printedTagID("not-a-mac"); err == nil {
		t.Error("printedTagID(invalid) = nil error, want an error")
	}
}

func hexDecode(s string) ([]byte, error) {
	b := make([]byte, len(s)/2)
	for i := 0; i < len(b); i++ {
		hi, err := hexNibble(s[i*2])
		if err != nil {
			return nil, err
		}
		lo, err := hexNibble(s[i*2+1])
		if err != nil {
			return nil, err
		}
		b[i] = hi<<4 | lo
	}
	return b, nil
}

func hexNibble(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	default:
		return 0, errInvalidHex
	}
}

var errInvalidHex = errInvalidHexT{}

type errInvalidHexT struct{}

func (errInvalidHexT) Error() string { return "invalid hex character" }
