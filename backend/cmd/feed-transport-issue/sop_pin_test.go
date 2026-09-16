package main

import (
	"testing"
	"time"
)

// TestTransportMaterializerPinsThePublishedFeedSOP: a transport task is pinned to the transport
// card in force when it is MATERIALIZED. This command wrote sop_version NULL (the seed) on every
// task because it called the repository directly with no card version (2026-09-17 E2E).
func TestTransportMaterializerPinsThePublishedFeedSOP(t *testing.T) {
	if !newService(nil, time.Second).PinsPublishedFeedSOP() {
		t.Fatal("feed-transport-issue materializes tasks without the published transport card; every task would be pinned to the seed")
	}
}
