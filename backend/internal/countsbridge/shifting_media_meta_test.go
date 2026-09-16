package countsbridge

import (
	"context"
	"testing"
	"time"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsports "github.com/vgoats/goatos/backend/internal/counts/ports"
)

// SHIFTING SOP (2026-09-16): the bridge carries each capture's slot title and kind (positional
// against media_refs) and the answers' groups across the module boundary, through the one shared
// MediaMeta builder.
func TestShiftingBridgeMapsMediaMetaAndContextRowGroups(t *testing.T) {
	capture := &capturingVerificationCreator{}
	bridge := NewShiftingVerificationEnqueuer(capture)
	err := bridge.EnqueueShiftingMoveVerification(context.Background(), countsapp.ShiftingVerificationEnqueueRequest{
		TenantID: "tenant", ShiftingEventID: "event", OperatorID: "operator", ParkID: "park", ShedID: "shed",
		MediaRefs: []string{"v1", "r1"},
		MediaMeta: []countsports.ProofMeta{{Label: " Shifting video ", Kind: "video"}, {Label: "At raise · Pen photo", Kind: "photo"}},
		ContextRows: []countsapp.VerificationContextRow{
			{Label: "Moved to", Value: "Castro 2"},
			{Label: "Why move", Value: "Overcrowded", Group: "At raise"},
		},
		CapturedAt: time.Now(), IdempotencyKey: "k",
	})
	if err != nil {
		t.Fatal(err)
	}
	meta := capture.item.MediaMeta
	if len(meta) != 2 || meta[0].Label != "Shifting video" || meta[0].Kind != "video" || meta[1].Label != "At raise · Pen photo" || meta[1].Kind != "photo" {
		t.Fatalf("media meta = %+v", meta)
	}
	rows := capture.item.ContextRows
	if len(rows) != 2 || rows[0].Group != "" || rows[1].Group != "At raise" {
		t.Fatalf("context rows = %+v", rows)
	}
}
