// Package postgres implements the identity repository port with explicit pgx
// queries. The methods are intentionally sqlc-style typed query functions, but
// hand-written for this first backend foundation because the repo does not yet
// have a sqlc config, migration-managed schema dump, or generated-code placement
// convention. Replace these methods with generated sqlc queries in the next DB
// access pass before write-heavy import/reconciliation code depends on them.
package postgres
