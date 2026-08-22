package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/herdsignals/ports"
)

// fakeRepo implements only what IngestPackets needs; every other method panics if called, so a
// test that exercises an unexpected path fails loudly instead of silently returning zero values.
type fakeRepo struct {
	ports.Repository
	ingestGotPackets []domain.Packet
}

func (f *fakeRepo) IngestPackets(_ context.Context, _ string, _ domain.Gateway, packets []domain.Packet) (int, int, error) {
	f.ingestGotPackets = packets
	return len(packets), len(packets), nil
}

// TestIngestPacketsUsesPerPacketGatewaySeenAt is the direct proof for the gateway-payload audit's
// top item: a batch's packets must carry their OWN gateway timestamps, not all be collapsed to
// the single envelope-level relay time.
func TestIngestPacketsUsesPerPacketGatewaySeenAt(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)

	actor := domain.Actor{TenantID: "tenant-1", UserID: "user-1"}
	t1 := "2026-01-01T00:00:00Z" // packet's own gateway clock
	t2 := "2026-01-01T00:00:05Z"
	req := domain.IngestRequest{
		GatewayID:   "gw-1",
		GatewaySeen: "2026-01-01T00:00:10Z", // envelope/relay time: later than either packet's own time
		Packets: []domain.IngestPacket{
			{TagID: "tag-a", TagMAC: "aa:aa:aa:aa:aa:aa", SeenAt: "2026-01-01T00:00:00Z", GatewaySeenAt: &t1},
			{TagID: "tag-b", TagMAC: "bb:bb:bb:bb:bb:bb", SeenAt: "2026-01-01T00:00:05Z", GatewaySeenAt: &t2},
			{TagID: "tag-c", TagMAC: "cc:cc:cc:cc:cc:cc", SeenAt: "2026-01-01T00:00:08Z"}, // no per-packet value: must fall back
		},
	}

	if _, err := svc.IngestPackets(context.Background(), actor, req); err != nil {
		t.Fatalf("IngestPackets: %v", err)
	}

	if len(repo.ingestGotPackets) != 3 {
		t.Fatalf("got %d packets, want 3", len(repo.ingestGotPackets))
	}

	got := make(map[string]string, 3)
	for _, p := range repo.ingestGotPackets {
		if p.GatewaySeenAt == nil {
			t.Fatalf("packet %s has nil GatewaySeenAt, want a value", p.TagID)
		}
		got[p.TagID] = p.GatewaySeenAt.UTC().Format("2006-01-02T15:04:05Z")
	}

	if got["tag-a"] != t1 {
		t.Errorf("tag-a gateway_seen_at = %s, want %s (its own value, not collapsed to the envelope)", got["tag-a"], t1)
	}
	if got["tag-b"] != t2 {
		t.Errorf("tag-b gateway_seen_at = %s, want %s (its own value)", got["tag-b"], t2)
	}
	if got["tag-c"] != req.GatewaySeen {
		t.Errorf("tag-c (no per-packet value) gateway_seen_at = %s, want fallback %s", got["tag-c"], req.GatewaySeen)
	}
	if got["tag-a"] == got["tag-b"] {
		t.Error("tag-a and tag-b were collapsed to the same gateway_seen_at -- the bug this test exists to catch")
	}
}
