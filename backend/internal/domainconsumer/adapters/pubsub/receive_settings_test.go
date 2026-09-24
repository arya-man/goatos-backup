package pubsub

import (
	"testing"

	cloudpubsub "cloud.google.com/go/pubsub"
)

func TestReceiveSettingsCapInFlightMessages(t *testing.T) {
	t.Setenv("GOATOS_PUBSUB_MAX_OUTSTANDING", "")
	got := boundedReceiveSettings(cloudpubsub.DefaultReceiveSettings)
	if got.MaxOutstandingMessages != DefaultMaxOutstandingMessages || got.NumGoroutines != 1 {
		t.Fatalf("settings = outstanding %d goroutines %d", got.MaxOutstandingMessages, got.NumGoroutines)
	}
	t.Setenv("GOATOS_PUBSUB_MAX_OUTSTANDING", "2")
	if got := boundedReceiveSettings(cloudpubsub.DefaultReceiveSettings); got.MaxOutstandingMessages != 2 {
		t.Fatalf("env override ignored: %d", got.MaxOutstandingMessages)
	}
}
