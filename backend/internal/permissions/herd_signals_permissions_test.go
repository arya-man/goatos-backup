package permissions

import "testing"

func TestHerdSignalsStreamRouteUsesReadPermission(t *testing.T) {
	route, ok := Match("GET", "/herd-signals/live/stream")
	if !ok {
		t.Fatal("herd signals live stream route is not registered")
	}
	if route.OperationID != "streamHerdSignalsLive" {
		t.Fatalf("operation_id=%q, want streamHerdSignalsLive", route.OperationID)
	}
	if len(route.Permissions) != 1 || route.Permissions[0] != HerdSignalsRead {
		t.Fatalf("permissions=%v, want [%s]", route.Permissions, HerdSignalsRead)
	}
}
