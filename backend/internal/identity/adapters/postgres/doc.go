// Package postgres implements the identity repository port with pgx and sqlc.
// Static read queries live in the generated sqlc subpackage. Dynamic reads that
// intentionally compose optional filters stay handwritten in this adapter until
// they have stable query shapes worth generating.
package postgres
