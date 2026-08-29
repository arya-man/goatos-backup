package app

import (
	"context"
	"math"
	"strings"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// THE APPROVE CARRIES THE NUMBER (maintainer decision 2026-08-20).
//
// Weighing (2026-08-17) and feed wastage (2026-08-18) each gave the verifier a value field with its
// own save button, and each of those saves relabels the verification item -- which bumps
// row_version. The approve that followed still carried the version she had on screen when the page
// loaded, so the version-fenced verdict UPDATE matched nothing and the approve silently did not
// happen. Two acts for one judgement, and the second one broken by the first.
//
// So the number now rides the approve. She types it (or does not, where it is optional) and presses
// Approve once.
//
// Verification still does not know what the number MEANS. It holds the copy and the decision; the
// producing module owns the write, reached through this seam -- the same shape as the enqueue,
// withdraw and relabel seams the producers already register.

// MeasurementApplier is the producing module's half of an approve that carries a number.
//
// Registered per category at composition time. A category with a MeasurementCorrection spec and no
// applier can still be approved; the number is simply refused, because accepting one we cannot
// write would tell the verifier her reading landed when nothing stored it.
type MeasurementApplier interface {
	// ApplyMeasurement writes the verifier's number onto the producer's own record.
	//
	// It runs BEFORE the verdict, so a producer that refuses (out of range, bucket already closed,
	// record immutable) stops the approve entirely rather than leaving an approved item next to a
	// number that never landed.
	ApplyMeasurement(ctx context.Context, in MeasurementApply) error
	// HasRecordedMeasurement reports whether the producer's record already carries a number.
	//
	// Only consulted for a RequiredForApprove category when the approve carries none, so that an
	// item measured earlier -- by an installed APK still using the old save button -- can still be
	// approved. A producer that cannot answer returns an error and the approve is refused rather
	// than let through on an assumption.
	HasRecordedMeasurement(ctx context.Context, tenantID string, source domain.SourceRef) (bool, error)
}

// MeasurementApply is one verifier reading, addressed by the ITEM's own source.
//
// The client never names the target: it sends a number, and the target is read off the item the
// verdict is being recorded against. A client that could name its own target could aim one item's
// approve at another item's record.
type MeasurementApply struct {
	TenantID   string
	VerifierID string
	Source     domain.SourceRef
	Value      float64
	Count      *int
	Reason     string
	// Entries is the per-field readings for items that declare MeasurementFields (feed packing:
	// one packed weight per feed item). Keys are the item's own field keys, already validated
	// against them -- complete, no unknowns, no duplicates. Empty for single-value categories.
	Entries []domain.MeasurementEntry
	// Fields is the item's own declared field list (key + display label), handed along so the
	// producer can store display labels without re-reading its frozen source.
	Fields []domain.MeasurementField
	// IdempotencyKey is derived from the verdict's own key, so a replayed approve re-applies the
	// same reading instead of writing a second one.
	IdempotencyKey string
	TraceID        string
}

// RegisterMeasurementApplier wires the producer that owns the write for one category's number.
//
// Composition-time, like RegisterCategory, and deliberately separate from it: the registry is
// display copy that the wire layer reads, while this is behaviour the service calls. Keeping them
// apart is what lets a category declare the control before its write path exists.
func (s *Service) RegisterMeasurementApplier(category string, applier MeasurementApplier) error {
	category = strings.TrimSpace(category)
	if category == "" {
		return BadRequest("invalid_category", "category is required")
	}
	if applier == nil {
		return BadRequest("invalid_applier", "measurement applier is required")
	}
	if _, ok := s.registry.Get(category); !ok {
		return BadRequest("unknown_category", "category "+category+" is not registered")
	}
	if s.measurementAppliers == nil {
		s.measurementAppliers = map[string]MeasurementApplier{}
	}
	if _, exists := s.measurementAppliers[category]; exists {
		return Conflict("applier_exists", "category "+category+" already has a measurement applier")
	}
	s.measurementAppliers[category] = applier
	return nil
}

// measurementApplierFor returns the applier registered for a category, if any.
func (s *Service) measurementApplierFor(category string) (MeasurementApplier, bool) {
	applier, ok := s.measurementAppliers[strings.TrimSpace(category)]
	return applier, ok
}

// maxMeasurementValue is a shared sanity ceiling. Each producer enforces its OWN range underneath
// this -- weighing's plausible goat weight and wastage's 10000kg typo ceiling are different numbers
// and stay where they are. This only stops a value that cannot be a measurement at all from
// reaching a producer.
const maxMeasurementValue = 1_000_000

// validateMeasurement refuses a value that is not a number before any producer sees it.
func validateMeasurement(m *domain.VerdictMeasurement) *Error {
	if m == nil {
		return nil
	}
	if math.IsNaN(m.Value) || math.IsInf(m.Value, 0) || m.Value < 0 || m.Value > maxMeasurementValue {
		return UnprocessableField(
			"invalid_measurement", "the measurement is not a usable number",
			"measurement.value", "value_out_of_range", "enter the number you can read in the video",
		)
	}
	if m.Count != nil && *m.Count < 1 {
		return UnprocessableField(
			"invalid_measurement", "the count must be a whole number of one or more",
			"measurement.count", "count_out_of_range", "enter how many animals were on the scale",
		)
	}
	seen := map[string]bool{}
	for _, entry := range m.Entries {
		key := strings.TrimSpace(entry.Key)
		if key == "" {
			return UnprocessableField(
				"invalid_measurement", "a measurement entry is missing its field key",
				"measurement.entries", "key_required", "",
			)
		}
		if seen[key] {
			return UnprocessableField(
				"invalid_measurement", "a measurement entry names the same field twice",
				"measurement.entries", "duplicate_key", "",
			)
		}
		seen[key] = true
		if math.IsNaN(entry.Value) || math.IsInf(entry.Value, 0) || entry.Value < 0 || entry.Value > maxMeasurementValue {
			return UnprocessableField(
				"invalid_measurement", "a measurement entry is not a usable number",
				"measurement.entries", "value_out_of_range", "enter the number you can read in the video",
			)
		}
	}
	return nil
}

// validateMeasurementEntriesAgainstFields checks a per-field approve against the ITEM's own
// declared entry boxes: every field filled (the client keeps Approve disabled until they are, and
// the backend refuses the same state rather than trust it), and no entry naming a field the item
// never declared -- an unknown key would silently vanish inside the producer instead of landing
// anywhere the verifier could see.
func validateMeasurementEntriesAgainstFields(entries []domain.MeasurementEntry, fields []domain.MeasurementField) *Error {
	byKey := map[string]bool{}
	for _, entry := range entries {
		byKey[strings.TrimSpace(entry.Key)] = true
	}
	for _, field := range fields {
		if !byKey[field.Key] {
			return UnprocessableField(
				"measurement_required", "record every value before approving",
				"measurement.entries", "required", field.Label,
			)
		}
	}
	declared := map[string]bool{}
	for _, field := range fields {
		declared[field.Key] = true
	}
	for _, entry := range entries {
		if !declared[strings.TrimSpace(entry.Key)] {
			return UnprocessableField(
				"invalid_measurement", "a measurement entry names a field this proof does not carry",
				"measurement.entries", "unknown_key", strings.TrimSpace(entry.Key),
			)
		}
	}
	return nil
}

// applyVerdictMeasurement writes the verifier's number through the producing module, and enforces
// the categories that cannot be approved without one.
//
// Returns whether a producer write actually happened, which tells RecordVerdict whether the item's
// row_version moved underneath it.
func (s *Service) applyVerdictMeasurement(ctx context.Context, in domain.Verdict, item domain.Item) (bool, error) {
	def, registered := s.registry.Get(item.Category)
	spec := def.MeasurementCorrection
	if !registered || spec == nil {
		// The category declares no number. Accepting one would mean storing it nowhere while
		// telling the verifier it landed, so it is refused rather than dropped.
		if in.Measurement != nil {
			return false, UnprocessableField(
				"measurement_not_supported", "this kind of proof does not carry a number",
				"measurement", "not_supported", "",
			)
		}
		return false, nil
	}
	applier, hasApplier := s.measurementApplierFor(item.Category)
	if in.Measurement != nil && !hasApplier {
		return false, UnprocessableField(
			"measurement_not_supported", "this kind of proof does not carry a number",
			"measurement", "not_supported", "",
		)
	}
	if in.Measurement != nil && in.Measurement.Count != nil && !spec.HasCountField(item.Source.RefType) {
		// The count field is not offered on this ref type, and the producer refuses one, so a
		// stray count would fail the whole approve. Refuse it by name instead.
		return false, UnprocessableField(
			"measurement_count_not_supported", "this proof carries no animal count",
			"measurement.count", "not_supported", "",
		)
	}
	// Per-field vs single-value is decided by the ITEM (its enqueued MeasurementFields), not the
	// category spec: the field list varies per item (a pen-session's own feed items), and an item
	// with none keeps today's single-value contract byte for byte.
	if in.Measurement != nil && len(in.Measurement.Entries) > 0 && len(item.MeasurementFields) == 0 {
		return false, UnprocessableField(
			"measurement_not_supported", "this kind of proof does not carry per-item values",
			"measurement.entries", "not_supported", "",
		)
	}
	if in.Measurement != nil && len(item.MeasurementFields) > 0 {
		if len(in.Measurement.Entries) == 0 {
			// A bare single value on a per-field item has no field to land on; name the real ask.
			return false, UnprocessableField(
				"measurement_required", "record every value before approving",
				"measurement.entries", "required", spec.ValueLabel,
			)
		}
		if err := validateMeasurementEntriesAgainstFields(in.Measurement.Entries, item.MeasurementFields); err != nil {
			return false, err
		}
	}
	if in.Measurement == nil {
		if !spec.RequiredForApprove {
			// The normal weighing case: blank means the operator's recorded number is right.
			return false, nil
		}
		if spec.PerItemFields && len(item.MeasurementFields) == 0 {
			// A per-field category whose item advertises NO boxes: the producer's frozen sheet was
			// unreadable at enqueue (a deliberate fail-open on the operator's submit), so there is
			// nothing to require -- refusing would strand the item unapprovable with no field the
			// verifier could fill.
			return false, nil
		}
		// Born-on-the-verifier's-screen categories still let through an item that was measured
		// earlier, so an APK using the old save button is not stranded with an unapprovable item.
		if !hasApplier {
			return false, UnprocessableField(
				"measurement_required", "record the value before approving",
				"measurement.value", "required", spec.ValueLabel,
			)
		}
		recorded, err := applier.HasRecordedMeasurement(ctx, in.TenantID, item.Source)
		if err != nil {
			return false, mapRepoErr(err)
		}
		if !recorded {
			return false, UnprocessableField(
				"measurement_required", "record the value before approving",
				"measurement.value", "required", spec.ValueLabel,
			)
		}
		return false, nil
	}
	if err := applier.ApplyMeasurement(ctx, MeasurementApply{
		TenantID:   in.TenantID,
		VerifierID: in.VerifierID,
		Source:     item.Source,
		Value:      in.Measurement.Value,
		Count:      in.Measurement.Count,
		Reason:     in.Measurement.Reason,
		Entries:    in.Measurement.Entries,
		Fields:     item.MeasurementFields,
		// Derived from the verdict's own key so a replayed approve re-applies the SAME reading
		// rather than writing a second one. An approve with no key of its own gets none here
		// either, and each producer's own idempotency rules take over.
		IdempotencyKey: measurementIdempotencyKey(in.IdempotencyKey),
	}); err != nil {
		return false, mapRepoErr(err)
	}
	return true, nil
}

// measurementIdempotencyKey derives the producer write's key from the verdict's.
//
// Suffixed rather than reused: the two writes land in different modules' idempotency tables, and a
// key that is byte-identical across them reads to a later author as one shared record when it is
// two. Blank stays blank -- inventing a key for a caller that sent none would make an unkeyed
// approve silently idempotent in a way its caller never asked for.
func measurementIdempotencyKey(verdictKey string) string {
	verdictKey = strings.TrimSpace(verdictKey)
	if verdictKey == "" {
		return ""
	}
	return verdictKey + ":measurement"
}
