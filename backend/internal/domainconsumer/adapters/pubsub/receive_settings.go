package pubsub

import (
	"os"
	"strconv"

	cloudpubsub "cloud.google.com/go/pubsub"
)

// DefaultMaxOutstandingMessages caps how many domain events one consumer
// handles at once. The library default (1000 outstanding, 10 goroutines) lets a
// burst fan out into as many concurrent handler transactions as the process's
// pgx pool allows, starving every other stage on the kernel worker (8
// connections under the stg connection budget). Override with
// GOATOS_PUBSUB_MAX_OUTSTANDING.
const DefaultMaxOutstandingMessages = 4

func boundedReceiveSettings(s cloudpubsub.ReceiveSettings) cloudpubsub.ReceiveSettings {
	limit := DefaultMaxOutstandingMessages
	if raw := os.Getenv("GOATOS_PUBSUB_MAX_OUTSTANDING"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	s.MaxOutstandingMessages = limit
	s.NumGoroutines = 1
	return s
}
