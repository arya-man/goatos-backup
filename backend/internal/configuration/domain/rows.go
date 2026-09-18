package domain

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Row statuses. Archive is the ordinary way a value leaves the farm's vocabulary: the row stays
// so everything that already names it keeps rendering; only new rows stop offering it. Delete
// is for a row nothing ever used.
const (
	StatusActive   = "active"
	StatusArchived = "archived"
)

// Row is one record of a register as the screen sees it.
type Row struct {
	ID         string         `json:"id"`
	Register   string         `json:"register"`
	Display    string         `json:"display"`
	Status     string         `json:"status"`
	RowVersion int            `json:"row_version"`
	IsBuiltin  bool           `json:"is_builtin"`
	Fields     map[string]any `json:"fields"`
	// Labels carries the display name of every ref column's target, keyed by column.
	Labels map[string]string `json:"labels"`
	// Counts carries what the row holds (a park's pens, a category's items), keyed by noun.
	Counts map[string]int `json:"counts,omitempty"`
}

// Usage answers "what would break if this row went away": every dependent noun with its count.
// Blocked is true when any count is non-zero.
type Usage struct {
	Blocked bool         `json:"blocked"`
	Uses    []UsageCount `json:"uses"`
}

// UsageCount is one dependent noun.
type UsageCount struct {
	Noun  string `json:"noun"`
	Count int    `json:"count"`
}

// Sentence renders the usage as a farm sentence: "In use by 12 animals, 3 partitions".
func (u Usage) Sentence() string {
	parts := make([]string, 0, len(u.Uses))
	for _, c := range u.Uses {
		if c.Count == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d %s", c.Count, c.Noun))
	}
	if len(parts) == 0 {
		return ""
	}
	return "In use by " + strings.Join(parts, ", ")
}

// FieldError is one refused field, in farm wording the drawer shows beside the input.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ValidationError carries every field refusal of one write.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	msgs := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		msgs = append(msgs, f.Field+": "+f.Message)
	}
	return strings.Join(msgs, "; ")
}

var (
	// ErrUnknownRegister is a register key the catalog does not carry.
	ErrUnknownRegister = errors.New("unknown register")
	// ErrReadOnlyRegister is a write to a register edited elsewhere.
	ErrReadOnlyRegister = errors.New("register is read-only here")
	// ErrBuiltin is an archive or delete of a built-in row.
	ErrBuiltin = errors.New("built-in row")
)

var codePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// NormalizeCode turns a typed label into the lower_snake key a code column stores: "Boer Goat"
// -> "boer_goat". It is what the screen offers when the person leaves the code blank.
func NormalizeCode(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out != "" && (out[0] < 'a' || out[0] > 'z') {
		out = "x_" + out
	}
	if len(out) > 40 {
		out = strings.Trim(out[:40], "_")
	}
	return out
}

// ValidateWrite coerces the raw fields of a create or update against the register's columns and
// returns the clean map. `existing` is nil on create. Every refusal is collected, not just the
// first, so the drawer can mark every wrong input at once.
//
// Rules, each of which the screen can also apply locally but which are decided HERE:
//   - unknown keys are refused (a dropped tick reads as saved while saving nothing);
//   - required columns must be present and non-blank on create, and non-blank when sent on update;
//   - an immutable column may not change once written;
//   - a code column must match ^[a-z][a-z0-9_]{0,39}$;
//   - a number column is a finite number, >= Min, and whole when Integer;
//   - an enum column is one of its options;
//   - a ref column is a non-blank id (existence is the store's check, inside the transaction);
//   - a Kinds-scoped column may only be sent when the row's kind is one of them.
func ValidateWrite(reg Register, raw map[string]any, existing *Row, kind string) (map[string]any, error) {
	var errs []FieldError
	clean := make(map[string]any, len(raw))
	known := make(map[string]Column, len(reg.Columns))
	for _, c := range reg.Columns {
		known[c.Key] = c
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, ok := known[k]; !ok {
			errs = append(errs, FieldError{Field: k, Code: "unknown", Message: "This field is not part of " + strings.ToLower(reg.Label) + "."})
		}
	}
	for _, c := range reg.Columns {
		v, sent := raw[c.Key]
		if len(c.Kinds) > 0 && sent && kind != "" && !containsString(c.Kinds, kind) {
			if !isBlank(v) {
				errs = append(errs, FieldError{Field: c.Key, Code: "not_for_kind", Message: c.Label + " does not apply to this kind of item."})
			}
			continue
		}
		if !sent {
			if c.Required && existing == nil {
				errs = append(errs, FieldError{Field: c.Key, Code: "required", Message: c.Label + " is required."})
			}
			continue
		}
		if c.Immutable && existing != nil {
			prev := fmt.Sprint(existing.Fields[c.Key])
			if !isBlank(v) && strings.TrimSpace(fmt.Sprint(v)) != prev {
				errs = append(errs, FieldError{Field: c.Key, Code: "immutable", Message: c.Label + " cannot change once saved."})
			}
			continue
		}
		if isBlank(v) {
			if c.Required {
				errs = append(errs, FieldError{Field: c.Key, Code: "required", Message: c.Label + " is required."})
				continue
			}
			clean[c.Key] = nil
			continue
		}
		val, err := coerce(c, v)
		if err != nil {
			errs = append(errs, FieldError{Field: c.Key, Code: "invalid", Message: err.Error()})
			continue
		}
		clean[c.Key] = val
	}
	if len(errs) > 0 {
		return nil, &ValidationError{Fields: errs}
	}
	return clean, nil
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func isBlank(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	}
	return false
}

func coerce(c Column, v any) (any, error) {
	switch c.Type {
	case TypeText, TypeNotes:
		s := strings.TrimSpace(fmt.Sprint(v))
		if len(s) > 500 {
			return nil, errors.New(c.Label + " is too long (500 characters at most).")
		}
		return s, nil
	case TypeCode:
		s := strings.TrimSpace(fmt.Sprint(v))
		if !codePattern.MatchString(s) {
			return nil, errors.New(c.Label + " must be lowercase letters, digits and underscores, starting with a letter.")
		}
		return s, nil
	case TypeBool:
		switch t := v.(type) {
		case bool:
			return t, nil
		case string:
			switch strings.ToLower(strings.TrimSpace(t)) {
			case "true", "yes", "1", "on":
				return true, nil
			case "false", "no", "0", "off":
				return false, nil
			}
		}
		return nil, errors.New(c.Label + " must be yes or no.")
	case TypeNumber:
		f, err := toFloat(v)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, errors.New(c.Label + " must be a number.")
		}
		if c.Min != nil && f < *c.Min {
			return nil, fmt.Errorf("%s must be at least %v.", c.Label, *c.Min)
		}
		if c.Integer {
			if f != math.Trunc(f) {
				return nil, errors.New(c.Label + " must be a whole number.")
			}
			return int64(f), nil
		}
		return f, nil
	case TypeEnum:
		s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		for _, o := range c.Options {
			if o.Value == s {
				return s, nil
			}
		}
		return nil, errors.New(c.Label + " must be one of the offered choices.")
	case TypeRef:
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" {
			return nil, errors.New(c.Label + " is required.")
		}
		return s, nil
	}
	return nil, errors.New(c.Label + " has an unknown type.")
}

func toFloat(v any) (float64, error) {
	switch t := v.(type) {
	case float64:
		return t, nil
	case float32:
		return float64(t), nil
	case int:
		return float64(t), nil
	case int64:
		return float64(t), nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(t), 64)
	}
	return 0, fmt.Errorf("not a number")
}

// FieldString reads a clean field as a string ("" when absent or nil).
func FieldString(fields map[string]any, key string) string {
	v, ok := fields[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// FieldInt reads a clean integer field; ok is false when absent or nil.
func FieldInt(fields map[string]any, key string) (int64, bool) {
	v, ok := fields[key]
	if !ok || v == nil {
		return 0, false
	}
	switch t := v.(type) {
	case int64:
		return t, true
	case int:
		return int64(t), true
	case float64:
		return int64(t), true
	}
	return 0, false
}

// FieldBool reads a clean bool field (false when absent).
func FieldBool(fields map[string]any, key string) bool {
	v, ok := fields[key]
	if !ok || v == nil {
		return false
	}
	b, _ := v.(bool)
	return b
}
