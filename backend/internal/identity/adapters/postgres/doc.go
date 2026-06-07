// Package postgres implements the identity repository port with explicit pgx
// queries. The methods are intentionally sqlc-style typed query functions, but
// hand-written for this first backend foundation because the repo does not yet
// have a sqlc config or generated-code placement convention.
package postgres
