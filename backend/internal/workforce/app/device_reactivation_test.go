package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// These tests cover the device-lockout P0 self-heal contract on the HeartbeatDevice path
// (mirrors the Bootstrap coverage in service_test.go): a device left non-active purely as a push
// delivery side effect must recover without requiring app reinstall, while a device an admin
// deliberately revoked must stay locked out forever.

func TestHeartbeatDeviceDeniesAdministrativelyRevokedDevice(t *testing.T) {
	revoked := device("revoked")
	revoked.Metadata = map[string]any{"revocation_reason": "lost phone"}
	repo := &fakeRepo{
		profile: profile("active"),
		grants:  []domain.GrantSummary{grant()},
		device:  revoked,
	}
	svc := NewService(repo)
	_, err := svc.HeartbeatDevice(context.Background(), ports.HeartbeatDeviceCommand{
		TenantID: testTenant,
		ActorID:  testActor,
		DeviceID: testDevice,
	}, "trace-1")
	assertAppCode(t, err, "device_revoked")
	if len(repo.registerDeviceCalls) != 0 {
		t.Fatalf("RegisterDevice called %d times, want 0 (administrative revocation must never self-heal)", len(repo.registerDeviceCalls))
	}
}

func TestHeartbeatDeviceSelfHealsPushSuppressedDevice(t *testing.T) {
	suppressed := device("revoked")
	suppressed.Metadata = map[string]any{"fcm_invalidated_reason": "FCM: UNREGISTERED"}
	reactivated := device("active")
	repo := &fakeRepo{
		profile:              profile("active"),
		grants:               []domain.GrantSummary{grant()},
		device:               suppressed,
		registerDeviceResult: reactivated,
	}
	svc := NewService(repo)
	got, err := svc.HeartbeatDevice(context.Background(), ports.HeartbeatDeviceCommand{
		TenantID: testTenant,
		ActorID:  testActor,
		DeviceID: testDevice,
		Body: domain.HeartbeatDeviceRequest{
			AppVersion: "1.2.0",
			OSVersion:  "15",
		},
	}, "trace-1")
	if err != nil {
		t.Fatalf("HeartbeatDevice() error=%v, want self-heal to succeed", err)
	}
	if got.Device.Status != "active" {
		t.Fatalf("device status=%q, want active after self-heal", got.Device.Status)
	}
	if len(repo.registerDeviceCalls) != 1 {
		t.Fatalf("RegisterDevice called %d times, want 1 (self-heal must reactivate via the register path)", len(repo.registerDeviceCalls))
	}
	call := repo.registerDeviceCalls[0]
	if call.TenantID != testTenant || call.ActorID != testActor {
		t.Fatalf("RegisterDevice scoped to tenant=%q actor=%q, want tenant=%q actor=%q (must reactivate only the authenticated owner's device)", call.TenantID, call.ActorID, testTenant, testActor)
	}
	if call.Body.AppInstallID != suppressed.AppInstallID {
		t.Fatalf("RegisterDevice app_install_id=%q, want %q (must re-key onto the same device row)", call.Body.AppInstallID, suppressed.AppInstallID)
	}
	if call.Body.AppVersion != "1.2.0" || call.Body.OSVersion != "15" {
		t.Fatalf("RegisterDevice app_version/os_version=%q/%q, want the heartbeat-reported values", call.Body.AppVersion, call.Body.OSVersion)
	}
}

// TestHeartbeatDeviceSelfHealFallsBackToKnownVersionsWhenBodyOmitsThem covers an older client
// build that heartbeats without app_version/os_version: reactivation must still succeed, falling
// back to the device row's last known values rather than erroring or overwriting them with blanks.
func TestHeartbeatDeviceSelfHealFallsBackToKnownVersionsWhenBodyOmitsThem(t *testing.T) {
	suppressed := device("revoked")
	suppressed.Metadata = map[string]any{"fcm_invalidated_reason": "FCM: UNREGISTERED"}
	suppressed.AppVersion = "1.0.0"
	suppressed.OSVersion = "14"
	reactivated := device("active")
	repo := &fakeRepo{
		profile:              profile("active"),
		grants:               []domain.GrantSummary{grant()},
		device:               suppressed,
		registerDeviceResult: reactivated,
	}
	svc := NewService(repo)
	_, err := svc.HeartbeatDevice(context.Background(), ports.HeartbeatDeviceCommand{
		TenantID: testTenant,
		ActorID:  testActor,
		DeviceID: testDevice,
	}, "trace-1")
	if err != nil {
		t.Fatalf("HeartbeatDevice() error=%v, want self-heal to succeed", err)
	}
	call := repo.registerDeviceCalls[0]
	if call.Body.AppVersion != "1.0.0" || call.Body.OSVersion != "14" {
		t.Fatalf("RegisterDevice app_version/os_version=%q/%q, want fallback to device row values 1.0.0/14", call.Body.AppVersion, call.Body.OSVersion)
	}
}

func TestIsAdministrativelyRevokedDistinguishesPushSuppressionFromAdminRevocation(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]any
		want     bool
	}{
		{name: "nil metadata is recoverable", metadata: nil, want: false},
		{name: "fcm_invalidated_reason only is recoverable", metadata: map[string]any{"fcm_invalidated_reason": "FCM: UNREGISTERED"}, want: false},
		{name: "revocation_reason present is terminal", metadata: map[string]any{"revocation_reason": "lost phone"}, want: true},
		{name: "both present is terminal (admin action wins)", metadata: map[string]any{"fcm_invalidated_reason": "x", "revocation_reason": "y"}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := domain.DeviceSummary{Metadata: tc.metadata}
			if got := isAdministrativelyRevoked(item); got != tc.want {
				t.Errorf("isAdministrativelyRevoked(%#v) = %v, want %v", tc.metadata, got, tc.want)
			}
		})
	}
}
