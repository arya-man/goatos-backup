package audit

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type captureExec struct {
	args []any
}

func (e *captureExec) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	e.args = args
	return pgconn.CommandTag{}, nil
}

func TestRecordAddsClientMetadataFromContext(t *testing.T) {
	ctx := httpmiddleware.WithClientInfo(context.Background(), httpmiddleware.ClientInfo{
		AppVersion:     "0.1.17",
		AppVersionCode: "18",
		BuildType:      "stgRelease",
		DeviceID:       "install-123",
		Platform:       "android",
		OSVersion:      "Android 14",
		SDKVersion:     "34",
		DeviceModel:    "Infinix X",
	})
	exec := &captureExec{}

	err := record(ctx, exec, Event{
		ActorType:    "operator",
		Action:       "weighing.observation_accepted",
		ResourceType: "weighing_observation",
		Metadata:     map[string]any{"result": "pending"},
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	if len(exec.args) < 12 {
		t.Fatalf("captured args=%d, want metadata arg", len(exec.args))
	}
	var metadata map[string]any
	if err := json.Unmarshal(exec.args[11].([]byte), &metadata); err != nil {
		t.Fatalf("metadata json: %v", err)
	}
	client, ok := metadata["client"].(map[string]any)
	if !ok {
		t.Fatalf("metadata missing client block: %#v", metadata)
	}
	if client["app_version"] != "0.1.17" || client["app_version_code"] != "18" || client["device_id"] != "install-123" || client["os_version"] != "Android 14" {
		t.Fatalf("client metadata = %#v", client)
	}
	if metadata["result"] != "pending" {
		t.Fatalf("existing metadata lost: %#v", metadata)
	}
}
