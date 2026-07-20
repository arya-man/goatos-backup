package domain

import (
	"errors"
	"testing"
)

// These are pure-logic tests: no Docker, no database, no clock. They exist because the rules they
// cover are the ones that decide whether a real animal is fed the right amount, and they must be
// runnable in the default suite rather than behind an opt-in Postgres gate.

// TestNormalizeDecimalRejectsRatherThanRepairs is the central "validate or reject" proof.
//
// The two cases that matter most, and would both be invisible without an assertion:
//
//	an ABSENT value must not become 0 -- absence means "not configured", a BLOCKING state, while 0
//	means "feed nothing", a correct instruction for milk-fed kids. Collapsing them silently feeds
//	an unconfigured shed nothing while showing the operator a clean, complete feed sheet.
//
//	a PRESENT but negative value must not be clamped to 0 -- clamping writes a real "feed nothing"
//	instruction the author never gave.
func TestNormalizeDecimalRejectsRatherThanRepairs(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		scale     int
		allowZero bool
		want      string
		wantErr   error
	}{
		{name: "plain integer canonicalizes to scale", raw: "150", scale: 3, allowZero: true, want: "150.000"},
		{name: "already scaled passes through", raw: "150.500", scale: 3, allowZero: true, want: "150.500"},
		{name: "short fraction is padded", raw: "150.5", scale: 3, allowZero: true, want: "150.500"},
		{name: "leading zeros are stripped", raw: "0150.5", scale: 3, allowZero: true, want: "150.500"},
		{name: "surrounding whitespace is trimmed", raw: "  12.25  ", scale: 3, allowZero: true, want: "12.250"},
		{name: "explicit plus sign is accepted", raw: "+7", scale: 3, allowZero: true, want: "7.000"},
		// AUTHORED ZERO. This must succeed: K0/K1 kids are on milk and are correctly fed 0 g of every
		// solid feed item.
		{name: "authored zero is a legal rate", raw: "0", scale: 3, allowZero: true, want: "0.000"},
		{name: "authored zero with decimals is legal", raw: "0.000", scale: 3, allowZero: true, want: "0.000"},
		{name: "negative zero normalizes to zero", raw: "-0.000", scale: 3, allowZero: true, want: "0.000"},
		// ABSENT. Rejected, never defaulted to zero.
		{name: "empty string is missing not zero", raw: "", scale: 3, allowZero: true, wantErr: ErrMissingField},
		{name: "whitespace only is missing not zero", raw: "   ", scale: 3, allowZero: true, wantErr: ErrMissingField},
		// PRESENT BUT OUT OF RANGE. Rejected, never clamped.
		{name: "negative is rejected not clamped", raw: "-5", scale: 3, allowZero: true, wantErr: ErrNegativeValue},
		{name: "negative fraction is rejected not clamped", raw: "-0.001", scale: 3, allowZero: true, wantErr: ErrNegativeValue},
		{name: "zero rejected when zero is disallowed", raw: "0", scale: 4, allowZero: false, wantErr: ErrNegativeValue},
		// PRESENT BUT MALFORMED / OVER PRECISE. Rejected, never rounded -- rounding changes the
		// authored number.
		{name: "over-precise value is rejected not rounded", raw: "12.3456", scale: 3, allowZero: true, wantErr: ErrInvalidDecimal},
		{name: "non-numeric is rejected", raw: "abc", scale: 3, allowZero: true, wantErr: ErrInvalidDecimal},
		{name: "trailing dot is rejected", raw: "12.", scale: 3, allowZero: true, wantErr: ErrInvalidDecimal},
		{name: "embedded space is rejected", raw: "1 2", scale: 3, allowZero: true, wantErr: ErrInvalidDecimal},
		{name: "scientific notation is rejected", raw: "1e3", scale: 3, allowZero: true, wantErr: ErrInvalidDecimal},
		{name: "bare sign is rejected", raw: "-", scale: 3, allowZero: true, wantErr: ErrInvalidDecimal},
		// Multiplier scale (numeric(8,4)).
		{name: "multiplier canonicalizes to four places", raw: "1.5", scale: 4, allowZero: true, want: "1.5000"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeDecimal("grams_per_head", tc.raw, tc.scale, tc.allowZero)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("NormalizeDecimal(%q) error = %v, want %v", tc.raw, err, tc.wantErr)
				}
				// The failure must name the offending field so the UI can attach it to the right input
				// instead of showing a generic banner.
				var fe *FieldError
				if !errors.As(err, &fe) || fe.Field != "grams_per_head" {
					t.Fatalf("NormalizeDecimal(%q) error = %v, want a FieldError naming grams_per_head", tc.raw, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeDecimal(%q) unexpected error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeDecimal(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestNormalizeLocalTimeRejectsOffsets pins the time semantics from migration 000004.
//
// These values are recurring Asia/Kolkata wall-clock business rules, NOT instants. Accepting an
// offset suffix would imply the stored value is tied to one, which is exactly the UTC-defines-the-
// business-day mistake the schema is built to avoid.
func TestNormalizeLocalTimeRejectsOffsets(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr error
	}{
		{name: "HH:MM is expanded to HH:MM:SS", raw: "07:00", want: "07:00:00"},
		{name: "HH:MM:SS passes through", raw: "15:45:00", want: "15:45:00"},
		{name: "whitespace is trimmed", raw: " 14:00 ", want: "14:00:00"},
		{name: "midnight is valid", raw: "00:00", want: "00:00:00"},
		{name: "last minute of the day is valid", raw: "23:59:59", want: "23:59:59"},
		{name: "empty is missing", raw: "", wantErr: ErrMissingField},
		// An offset must be REJECTED, not stripped: stripping it would silently reinterpret the
		// author's instant as a wall-clock rule.
		{name: "positive offset suffix is rejected", raw: "07:00+05:30", wantErr: ErrInvalidTime},
		{name: "zulu suffix is rejected", raw: "07:00:00Z", wantErr: ErrInvalidTime},
		{name: "hour out of range is rejected", raw: "24:00", wantErr: ErrInvalidTime},
		{name: "minute out of range is rejected", raw: "07:60", wantErr: ErrInvalidTime},
		{name: "single-digit hour is rejected", raw: "7:00", wantErr: ErrInvalidTime},
		{name: "non-numeric is rejected", raw: "ab:cd", wantErr: ErrInvalidTime},
		{name: "too many components is rejected", raw: "07:00:00:00", wantErr: ErrInvalidTime},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeLocalTime("direction_time", tc.raw)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("NormalizeLocalTime(%q) error = %v, want %v", tc.raw, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeLocalTime(%q) unexpected error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeLocalTime(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestValidateScheduleOrder covers the ordering invariant, including the equality case that the
// LIVE experiment workflow depends on (direction 14:00, correction 14:00). A naive strict-greater
// check would reject the real configuration.
func TestValidateScheduleOrder(t *testing.T) {
	ptr := func(s string) *string { return &s }
	tests := []struct {
		name       string
		direction  string
		correction string
		transport  *string
		wantErr    bool
	}{
		{name: "live normal workflow", direction: "07:00:00", correction: "14:00:00", transport: ptr("15:45:00")},
		// The equality case. Experiment issues its direction at 14:00 and batches corrections at the
		// same time: the first direction of the day already carries that day's approved corrections.
		{name: "live experiment workflow with equal times", direction: "14:00:00", correction: "14:00:00", transport: ptr("15:45:00")},
		{name: "transport equal to correction is allowed", direction: "07:00:00", correction: "14:00:00", transport: ptr("14:00:00")},
		{name: "absent transport is allowed", direction: "07:00:00", correction: "14:00:00", transport: nil},
		{name: "correction before direction is rejected", direction: "14:00:00", correction: "07:00:00", transport: nil, wantErr: true},
		{name: "transport before correction is rejected", direction: "07:00:00", correction: "14:00:00", transport: ptr("13:00:00"), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateScheduleOrder(tc.direction, tc.correction, tc.transport)
			if tc.wantErr {
				if !errors.Is(err, ErrTimeOrder) {
					t.Fatalf("ValidateScheduleOrder = %v, want ErrTimeOrder", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateScheduleOrder unexpected error: %v", err)
			}
		})
	}
}

func TestValidateWorkflow(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr error
	}{
		{name: "normal", raw: "normal", want: WorkflowNormal},
		{name: "experiment", raw: "experiment", want: WorkflowExperiment},
		{name: "case insensitive", raw: "Experiment", want: WorkflowExperiment},
		{name: "whitespace trimmed", raw: " normal ", want: WorkflowNormal},
		{name: "empty is missing", raw: "", wantErr: ErrMissingField},
		// Rejected rather than ignored: an unrecognised workflow must not quietly fall through to a
		// default clock, because the two workflows run at deliberately different hours.
		{name: "unknown workflow is rejected", raw: "trial", wantErr: ErrInvalidWorkflow},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateWorkflow("workflow", tc.raw)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ValidateWorkflow(%q) error = %v, want %v", tc.raw, err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("ValidateWorkflow(%q) = %q, %v; want %q, nil", tc.raw, got, err, tc.want)
			}
		})
	}
}

// TestValidateAppliesTo proves an EMPTY filter means "no filter" while an unrecognised one is an
// error. Silently dropping a bad filter would hand a caller who asked for kid tags the full
// vocabulary.
func TestValidateAppliesTo(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "empty means no filter", raw: "", want: ""},
		{name: "adult", raw: "adult", want: "adult"},
		{name: "kid case insensitive", raw: "Kid", want: "kid"},
		{name: "unknown is rejected not ignored", raw: "juvenile", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateAppliesTo("applies_to", tc.raw)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidAppliesTo) {
					t.Fatalf("ValidateAppliesTo(%q) error = %v, want ErrInvalidAppliesTo", tc.raw, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("ValidateAppliesTo(%q) = %q, %v; want %q, nil", tc.raw, got, err, tc.want)
			}
		})
	}
}

func TestValidateBusinessDate(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{name: "valid date", raw: "2026-07-19"},
		{name: "empty is missing", raw: "", wantErr: true},
		// A timestamp is rejected: an effective date is a business-calendar DAY, and accepting an
		// instant would reintroduce the UTC-vs-Asia/Kolkata boundary bug the date form avoids.
		{name: "timestamp is rejected", raw: "2026-07-19T10:00:00Z", wantErr: true},
		{name: "month out of range", raw: "2026-13-01", wantErr: true},
		{name: "day out of range", raw: "2026-07-32", wantErr: true},
		{name: "slash separators rejected", raw: "2026/07/19", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateBusinessDate("effective_from", tc.raw)
			if tc.wantErr && err == nil {
				t.Fatalf("ValidateBusinessDate(%q) = nil error, want failure", tc.raw)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("ValidateBusinessDate(%q) unexpected error: %v", tc.raw, err)
			}
		})
	}
}

func TestRequireNonBlank(t *testing.T) {
	if _, err := RequireNonBlank("ration_group", "   "); !errors.Is(err, ErrMissingField) {
		t.Fatalf("blank label error = %v, want ErrMissingField", err)
	}
	got, err := RequireNonBlank("ration_group", "  Boer  ")
	if err != nil || got != "Boer" {
		t.Fatalf("RequireNonBlank = %q, %v; want \"Boer\", nil", got, err)
	}
}
