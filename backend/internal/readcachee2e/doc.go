// Package readcachee2e holds the Postgres-gated read-your-writes regression test for the shared
// analytics read cache (platform/readcache): two API-instance stacks, each with its own cache and
// its own LISTEN connection, on one throwaway database. A write through a real module repository
// on instance A must be visible to a page read on instance A immediately and on instance B within
// one second. See ryw_integration_test.go.
package readcachee2e
