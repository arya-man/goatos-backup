package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// The phone reports its OS notification switch on device register and on every heartbeat. Three
// states must survive the wire: on, off, and "this app build never said".
func TestDeviceRequestsCarryTheReportedNotificationSwitch(t *testing.T) {
	t.Run("register reports muted", func(t *testing.T) {
		var body RegisterDeviceRequest
		if err := json.Unmarshal([]byte(`{"app_install_id":"i-1","app_version":"1.0","notifications_enabled":false}`), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.NotificationsEnabled == nil {
			t.Fatal("register dropped the reported notification switch; the backend cannot tell a muted phone from a reachable one")
		}
		if *body.NotificationsEnabled {
			t.Fatal("register decoded a muted phone as reachable")
		}
	})

	t.Run("heartbeat re-reports after the person switches notifications back on", func(t *testing.T) {
		var body HeartbeatDeviceRequest
		if err := json.Unmarshal([]byte(`{"app_version":"1.0","notifications_enabled":true}`), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.NotificationsEnabled == nil || !*body.NotificationsEnabled {
			t.Fatal("heartbeat dropped the reported notification switch, so a phone stays marked muted until reinstall")
		}
	})

	t.Run("older app build omits it and stays unknown, not muted", func(t *testing.T) {
		var body HeartbeatDeviceRequest
		if err := json.Unmarshal([]byte(`{"app_version":"0.9"}`), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.NotificationsEnabled != nil {
			t.Fatal("an omitted switch must stay unknown (nil), never decode as a value")
		}
	})
}

func TestDeviceSummaryOmitsAnUnreportedSwitch(t *testing.T) {
	encoded, err := json.Marshal(DeviceSummary{DeviceID: "d-1"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(string(encoded), "notifications_enabled") {
		t.Errorf("a device that never reported its switch must not claim one: %s", encoded)
	}

	muted := false
	encoded, err = json.Marshal(DeviceSummary{DeviceID: "d-1", NotificationsEnabled: &muted})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !strings.Contains(string(encoded), `"notifications_enabled":false`) {
		t.Errorf("a push-muted device must be visible as muted: %s", encoded)
	}
}
