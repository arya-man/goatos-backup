// Package pgconv holds small, shared conversions between Go domain values and pgx/pgtype
// values used by generated sqlc code. Keeping them here avoids duplicating the same helpers
// in every domain's postgres adapter.
package pgconv

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// UUID parses a UUID string into pgtype.UUID. An empty string yields an invalid (NULL) UUID.
func UUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if s == "" {
		return u, nil
	}
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

// UUIDs parses UUID strings into pgtype.UUID values for pgx uuid[] parameters.
func UUIDs(values []string) ([]pgtype.UUID, error) {
	ids := make([]pgtype.UUID, 0, len(values))
	for _, value := range values {
		id, err := UUID(value)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// MustUUID is UUID without the error (invalid string yields a NULL UUID). Use only when the
// caller has already validated the input.
func MustUUID(s string) pgtype.UUID {
	u, _ := UUID(s)
	return u
}

// NullableUUID converts an optional UUID string pointer to pgtype.UUID (nil/empty -> NULL).
func NullableUUID(s *string) pgtype.UUID {
	if s == nil || *s == "" {
		return pgtype.UUID{}
	}
	return MustUUID(*s)
}

// UUIDString renders a pgtype.UUID as its canonical string (empty when NULL/invalid).
func UUIDString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	v, err := u.Value()
	if err != nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// Text wraps a string into pgtype.Text; empty string becomes NULL.
func Text(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// TextValue unwraps pgtype.Text (NULL -> empty string).
func TextValue(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

// Numeric parses a decimal string into pgtype.Numeric; empty string becomes NULL.
func Numeric(s string) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	if s == "" {
		return n, nil
	}
	if err := n.Scan(s); err != nil {
		return pgtype.Numeric{}, err
	}
	return n, nil
}

// NumericString renders pgtype.Numeric as a decimal string (NULL/invalid -> empty string).
func NumericString(n pgtype.Numeric) string {
	if !n.Valid {
		return ""
	}
	v, err := n.Value()
	if err != nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// Date wraps an optional time pointer into pgtype.Date (nil -> NULL).
func Date(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *t, Valid: true}
}

// DateValue unwraps pgtype.Date to an optional time pointer (NULL -> nil).
func DateValue(d pgtype.Date) *time.Time {
	if !d.Valid {
		return nil
	}
	t := d.Time
	return &t
}

// Timestamptz wraps a time into pgtype.Timestamptz (always valid).
func Timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// NullableTimestamptz wraps an optional time pointer (nil -> NULL).
func NullableTimestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// Int4 wraps an optional int32 pointer into pgtype.Int4 (nil -> NULL).
func Int4(v *int32) pgtype.Int4 {
	if v == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *v, Valid: true}
}

// JSONB returns a non-nil jsonb payload; empty/nil becomes an empty object.
func JSONB(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}
