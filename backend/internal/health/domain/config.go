package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Authoring contracts for the Health Config screen (/health/config -> /health-config/*).
//
// A protocol is EDITED BY VERSION, never in place. Saving builds a draft; publishing promotes that
// draft and retires the version it replaces. `health_cases` pins the version a goat was diagnosed
// under, so an animal mid-treatment finishes on the dosages it started on and the version it was
// actually treated from stays readable forever. Everything in this file exists to make that safe:
// the validation refuses content the schema would accept but a treating operator could not follow,
// and the content hash is what lets a re-save of identical content be recognised as a no-op rather
// than churn out an indistinguishable new version.

const (
	// MaxStepsPerProtocol matches the sheet importer's cap. The two paths write the same table, so
	// a bound the importer refuses to exceed must not be reachable from the web.
	MaxStepsPerProtocol = 2000
	// MaxDisplayNameLen bounds the disease name. It is rendered in a mobile work-item row and in a
	// push notification; past this it is truncated everywhere and stops being a name.
	MaxDisplayNameLen = 80
	// MaxDosageTextLen / MaxInstructionLen bound the free-text an operator must read off a phone
	// mid-treatment. These are legibility limits, not storage limits.
	MaxDosageTextLen  = 120
	MaxInstructionLen = 2000

	RecordTypeAction         = "action"
	RecordTypeMedication     = "medication"
	RecordTypeCriticalAction = "critical_action"

	CriticalActionLifecycleExit    = "lifecycle_exit"
	CriticalActionQuarantineOrMove = "quarantine_or_movement"

	ProtocolStatusDraft     = "draft"
	ProtocolStatusPublished = "published"
	ProtocolStatusRetired   = "retired"

	OutcomeCreated   = "created"
	OutcomeSaved     = "saved"
	OutcomeUnchanged = "unchanged"
	OutcomePublished = "published"
	OutcomeDiscarded = "discarded"

	WriteKindDiseaseCreate = "disease_create"
	WriteKindDraftSave     = "draft_save"
	WriteKindDraftPublish  = "draft_publish"
	WriteKindDraftDiscard  = "draft_discard"
)

// AgeBands is the authoring order for the two bands. Creating a disease creates BOTH, because
// every one of the 27 imported diseases exists in both bands and a disease that treats adults but
// not kids has no representable meaning on the phone: the operator picks a goat, and the goat's
// own age band decides which protocol is loaded. A missing band would fail at diagnosis time with
// "protocol not published", which reads as a bug rather than as a deliberate gap.
var AgeBands = []string{AgeBandAdult, AgeBandKid}

// Sessions is the authoring vocabulary for when in the day a step happens. 'unscheduled' is not a
// placeholder for "not decided yet" -- it is the real bucket the imported sheet uses for a step
// with no fixed time (a once-daily injection given whenever the operator reaches the animal).
var Sessions = []string{SessionMorning, SessionAfternoon, SessionEvening, SessionUnscheduled}

// MedicineRoutes is the closed vocabulary of administration routes.
//
// It is validate-or-reject rather than free text because the route is a CLINICAL instruction: the
// difference between IM and IV is not a labelling preference, and a typo that stores "1M" would
// render on the operator's phone as an instruction nobody can follow. The set is exactly what the
// maintainer's imported sheet uses.
var MedicineRoutes = []string{"IM", "SQ", "IV", "Oral", "Topical", "Intra Mammary"}

// DosageDenominators is the closed vocabulary for what the dosage number is counted in. 'none'
// is a real authored value in the imported data (a whole-unit dose such as one bolus), and is
// deliberately distinct from an ABSENT denominator, which means the author has not said.
var DosageDenominators = []string{"ml", "kg", "none"}

// CriticalActionTypes is the closed vocabulary of guarded handoffs. Both values hand the animal to
// a policy-pack transition that Health does not itself perform (AGENTS.md: critical animal actions
// fail closed until a policy pack owns them), which is why an author may name one but the Health
// module never executes it.
var CriticalActionTypes = []string{CriticalActionQuarantineOrMove, CriticalActionLifecycleExit}

// ---------------------------------------------------------------------------
// Read contracts
// ---------------------------------------------------------------------------

// ProtocolCatalogItem is one row of the Health Config list: one disease in one age band, with the
// live version and the open draft (if any) side by side.
//
// Published and draft are separate fields rather than one "current" object because the screen has
// to show both at once: an author needs to see that the live protocol still says 5 ml while their
// unpublished draft says 3 ml. Collapsing them would hide exactly the state that most needs review.
type ProtocolCatalogItem struct {
	DiseaseKey  string `json:"disease_key"`
	DisplayName string `json:"display_name"`
	AgeBand     string `json:"age_band"`

	PublishedVersionID  string     `json:"published_version_id"`
	PublishedVersion    int        `json:"published_version"`
	DurationDays        int        `json:"duration_days"`
	StepCount           int        `json:"step_count"`
	MedicationCount     int        `json:"medication_count"`
	CriticalActionCount int        `json:"critical_action_count"`
	PublishedAt         *time.Time `json:"published_at"`
	SourceRef           string     `json:"source_ref"`

	HasDraft             bool       `json:"has_draft"`
	DraftVersionID       string     `json:"draft_version_id"`
	DraftVersion         int        `json:"draft_version"`
	DraftDurationDays    int        `json:"draft_duration_days"`
	DraftStepCount       int        `json:"draft_step_count"`
	DraftMedicationCount int        `json:"draft_medication_count"`
	DraftUpdatedAt       *time.Time `json:"draft_updated_at"`
}

// ProtocolCatalogPage is a bounded keyset page. There is no total: counting the filtered catalog
// on every request is compute-on-read, and a page subtotal shown as a catalog total is a false
// statement (docs/decisions/scale-anti-patterns.md).
type ProtocolCatalogPage struct {
	Items      []ProtocolCatalogItem `json:"items"`
	NextCursor *string               `json:"next_cursor"`
}

// ProtocolCatalogQuery filters the catalog. Cursor is the opaque keyset position.
type ProtocolCatalogQuery struct {
	TenantID  string
	AgeBand   string
	Search    string
	DraftOnly bool
	Cursor    string
	Limit     int
}

// ProtocolVersionSummary is one entry of a disease's version history.
type ProtocolVersionSummary struct {
	ProtocolVersionID string     `json:"protocol_version_id"`
	Version           int        `json:"version"`
	Status            string     `json:"status"`
	DurationDays      int        `json:"duration_days"`
	StepCount         int        `json:"step_count"`
	SourceRef         string     `json:"source_ref"`
	PublishedAt       *time.Time `json:"published_at"`
	CreatedAt         time.Time  `json:"created_at"`
}

// ProtocolDetail is one version with its ordered steps and the disease's version history.
type ProtocolDetail struct {
	ProtocolVersionID string                   `json:"protocol_version_id"`
	DiseaseKey        string                   `json:"disease_key"`
	DisplayName       string                   `json:"display_name"`
	AgeBand           string                   `json:"age_band"`
	Version           int                      `json:"version"`
	Status            string                   `json:"status"`
	DurationDays      int                      `json:"duration_days"`
	SourceRef         string                   `json:"source_ref"`
	ContentHash       string                   `json:"content_hash"`
	PublishedAt       *time.Time               `json:"published_at"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
	Steps             []ProtocolStep           `json:"steps"`
	History           []ProtocolVersionSummary `json:"history"`
	// OpenCaseCount is how many goats are currently being treated under THIS version. It is shown
	// next to the publish control so an author can see that retiring this version does not stop
	// those treatments -- the cases keep running on the version they pinned.
	OpenCaseCount int `json:"open_case_count"`
}

// ---------------------------------------------------------------------------
// Write contracts
// ---------------------------------------------------------------------------

// AuthoredStep is one step as the CLIENT sends it. Note the absence of Seq: order is positional,
// and the adapter assigns seq 1..N server-side.
//
// That is not a convenience. `health_protocol_steps` has UNIQUE (version_id, seq), so letting a
// client send seq means any reorder arrives as a set of UPDATEs that collide with the constraint
// halfway through. Positional order makes a reorder a whole-list replacement that either commits
// or does not.
type AuthoredStep struct {
	DayNo              int    `json:"day_no"`
	Session            string `json:"session"`
	RecordType         string `json:"record_type"`
	MedicineName       string `json:"medicine_name"`
	DosageText         string `json:"dosage_text"`
	DosageDenominator  string `json:"dosage_denominator"`
	MedicineRoute      string `json:"medicine_route"`
	Instruction        string `json:"instruction"`
	CriticalActionType string `json:"critical_action_type"`
}

// AuthoredProtocol is the whole content of one draft. Saves are whole-document, never per-field:
// a protocol is read by an operator as one sequence, and a partial save that left step 7 pointing
// at a day the duration no longer covers would be a protocol nobody can execute.
type AuthoredProtocol struct {
	DisplayName  string         `json:"display_name"`
	DurationDays int            `json:"duration_days"`
	Steps        []AuthoredStep `json:"steps"`
}

type SaveDraftCommand struct {
	TenantID           string
	ActorID            string
	DiseaseKey         string
	AgeBand            string
	Protocol           AuthoredProtocol
	IdempotencyKey     string
	RequestFingerprint string
}

type CreateDiseaseCommand struct {
	TenantID           string
	ActorID            string
	DiseaseKey         string
	DisplayName        string
	DurationDays       int
	IdempotencyKey     string
	RequestFingerprint string
}

type ProtocolVersionCommand struct {
	TenantID           string
	ActorID            string
	ProtocolVersionID  string
	IdempotencyKey     string
	RequestFingerprint string
}

// AuthoringResult is what every write returns. Outcome uses the ledger vocabulary so the API, the
// audit trail and the UI all say the same word for the same act.
type AuthoringResult struct {
	Outcome           string `json:"outcome"`
	DiseaseKey        string `json:"disease_key"`
	AgeBand           string `json:"age_band,omitempty"`
	ProtocolVersionID string `json:"protocol_version_id,omitempty"`
	Version           int    `json:"version,omitempty"`
	RetiredVersionID  string `json:"retired_version_id,omitempty"`
	// DraftVersionIDs carries both age bands for a disease create, keyed by age band.
	DraftVersionIDs map[string]string `json:"draft_version_ids,omitempty"`
	// IdempotentReplay is true when this response was read back from the ledger rather than
	// produced by re-running the write.
	IdempotentReplay bool `json:"idempotent_replay"`
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// FieldError names the exact field that was rejected so the editor can mark the offending row
// rather than showing one message for the whole form.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e FieldError) Error() string { return e.Field + ": " + e.Message }

// ValidationError is the ordered set of field errors from one validation pass. All errors are
// returned together: an author fixing a 20-step protocol one rejected field per round trip would
// give up, and the editor can only highlight what it is told about.
type ValidationError struct {
	Errors []FieldError `json:"errors"`
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Errors))
	for _, fe := range e.Errors {
		parts = append(parts, fe.Error())
	}
	return "health: invalid authored protocol: " + strings.Join(parts, "; ")
}

// NormalizeDiseaseKey derives the stable machine key from a display name.
//
// The key is the identity a case, an obligation and every historical version join on, so it is
// derived ONCE at creation and never re-derived from a later rename. Renaming "Foot rot" to
// "Footrot" must not orphan the cases treated under it.
func NormalizeDiseaseKey(displayName string) string {
	lower := strings.ToLower(strings.TrimSpace(displayName))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range lower {
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
	key := strings.Trim(b.String(), "_")
	// The column CHECK requires a leading letter; a name starting with a digit ("3 day scours")
	// would otherwise produce a key the database refuses at insert time with an opaque error.
	if key != "" && key[0] >= '0' && key[0] <= '9' {
		key = "d_" + key
	}
	return key
}

// ValidDiseaseKey mirrors the column CHECK (disease_key ~ '^[a-z][a-z0-9_]*$'), so a bad key is
// rejected with a field error naming the input rather than as a constraint violation.
func ValidDiseaseKey(key string) bool {
	if key == "" {
		return false
	}
	if key[0] < 'a' || key[0] > 'z' {
		return false
	}
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func inSet(v string, set []string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// NormalizeAuthoredProtocol trims every field and drops steps that are entirely blank.
//
// Blank-row dropping is what makes an "add step" button safe: the editor appends an empty row the
// moment it is clicked, and an author who clicks it and then saves without filling it in means
// "I changed my mind", not "store an empty instruction". A row with ANY content is kept and
// validated, so a half-filled row is still reported rather than silently discarded.
func NormalizeAuthoredProtocol(p AuthoredProtocol) AuthoredProtocol {
	out := AuthoredProtocol{
		DisplayName:  strings.Join(strings.Fields(p.DisplayName), " "),
		DurationDays: p.DurationDays,
		Steps:        make([]AuthoredStep, 0, len(p.Steps)),
	}
	for _, s := range p.Steps {
		n := AuthoredStep{
			DayNo:              s.DayNo,
			Session:            strings.ToLower(strings.TrimSpace(s.Session)),
			RecordType:         strings.ToLower(strings.TrimSpace(s.RecordType)),
			MedicineName:       strings.TrimSpace(s.MedicineName),
			DosageText:         strings.TrimSpace(s.DosageText),
			DosageDenominator:  strings.TrimSpace(s.DosageDenominator),
			MedicineRoute:      strings.TrimSpace(s.MedicineRoute),
			Instruction:        strings.TrimRight(strings.TrimLeft(s.Instruction, " \t"), " \t"),
			CriticalActionType: strings.TrimSpace(s.CriticalActionType),
		}
		if n.DayNo == 0 && n.Session == "" && n.RecordType == "" && n.MedicineName == "" &&
			n.DosageText == "" && n.DosageDenominator == "" && n.MedicineRoute == "" &&
			strings.TrimSpace(n.Instruction) == "" && n.CriticalActionType == "" {
			continue
		}
		out.Steps = append(out.Steps, n)
	}
	return out
}

// ValidateAuthoredProtocol is the whole authoring rulebook. It returns every field error at once.
//
// forPublish tightens two rules that a DRAFT is allowed to break, because a draft is
// work-in-progress an author saves and returns to, while a published protocol is what an operator
// treats an animal from:
//
//   - a draft may have zero steps; a published protocol may not (OpenCase would substitute a
//     generic "follow the configured protocol" placeholder for every day, which is not a protocol).
//   - a draft may be saved while a step sits on a day beyond the duration; publishing it cannot,
//     because those steps would never be scheduled and the author would never be told.
//
// Both are reported as field errors at publish time rather than being auto-fixed. Silently
// deleting the steps past the duration, or silently extending the duration to cover them, are each
// a guess about a medical document.
func ValidateAuthoredProtocol(p AuthoredProtocol, forPublish bool) error {
	var errs []FieldError
	add := func(field, msg string) { errs = append(errs, FieldError{Field: field, Message: msg}) }

	if p.DisplayName == "" {
		add("display_name", "Enter a disease name.")
	} else if len([]rune(p.DisplayName)) > MaxDisplayNameLen {
		add("display_name", fmt.Sprintf("Disease name must be %d characters or fewer.", MaxDisplayNameLen))
	}

	// Validate-or-reject: a present but out-of-range duration is an error, never rewritten to the
	// default. Only a genuinely absent value gets DefaultDurationDays, and that substitution
	// happens at the API edge where absent is still distinguishable from zero.
	if p.DurationDays < 1 || p.DurationDays > MaxDurationDays {
		add("duration_days", fmt.Sprintf("Number of days must be between 1 and %d.", MaxDurationDays))
	}

	if len(p.Steps) > MaxStepsPerProtocol {
		add("steps", fmt.Sprintf("A protocol cannot have more than %d steps.", MaxStepsPerProtocol))
	}
	if forPublish && len(p.Steps) == 0 {
		add("steps", "Add at least one step before publishing.")
	}

	// A day/session pair an author never filled in reads as a gap in the course; report the days
	// that carry no step at all, but only at publish time.
	covered := map[int]bool{}

	for i, s := range p.Steps {
		field := func(name string) string { return fmt.Sprintf("steps[%d].%s", i, name) }

		if s.DayNo < 1 || s.DayNo > MaxDurationDays {
			add(field("day_no"), fmt.Sprintf("Day must be between 1 and %d.", MaxDurationDays))
		} else {
			covered[s.DayNo] = true
			if s.DayNo > p.DurationDays && p.DurationDays >= 1 {
				msg := fmt.Sprintf("Day %d is beyond this protocol's %d day(s).", s.DayNo, p.DurationDays)
				if forPublish {
					add(field("day_no"), msg+" Move the step or increase the number of days.")
				}
			}
		}
		if !inSet(s.Session, Sessions) {
			add(field("session"), "Choose when in the day this step happens.")
		}

		switch s.RecordType {
		case RecordTypeMedication:
			if s.MedicineName == "" {
				add(field("medicine_name"), "Enter the medicine.")
			}
			if s.CriticalActionType != "" {
				add(field("critical_action_type"), "Only a critical action carries a handoff type.")
			}
			if s.DosageText != "" && len([]rune(s.DosageText)) > MaxDosageTextLen {
				add(field("dosage_text"), fmt.Sprintf("Dosage must be %d characters or fewer.", MaxDosageTextLen))
			}
			if s.MedicineRoute != "" && !inSet(s.MedicineRoute, MedicineRoutes) {
				add(field("medicine_route"), "Choose a route from the list.")
			}
			if s.DosageDenominator != "" && !inSet(s.DosageDenominator, DosageDenominators) {
				add(field("dosage_denominator"), "Choose a unit from the list.")
			}
			// A dose with a unit but no number, or a number with no unit, is half an instruction.
			// Neither half is safe to infer, so both are reported at publish time.
			if forPublish && s.DosageText == "" && s.DosageDenominator != "" {
				add(field("dosage_text"), "Enter the dosage amount, or clear the unit.")
			}
			if forPublish && s.DosageText != "" && s.DosageDenominator == "" {
				add(field("dosage_denominator"), "Choose the unit for this dosage.")
			}
			if forPublish && s.MedicineRoute == "" {
				add(field("medicine_route"), "Choose how the medicine is given.")
			}
		case RecordTypeAction:
			if strings.TrimSpace(s.Instruction) == "" {
				add(field("instruction"), "Enter what the operator should do.")
			} else if len([]rune(s.Instruction)) > MaxInstructionLen {
				add(field("instruction"), fmt.Sprintf("Instruction must be %d characters or fewer.", MaxInstructionLen))
			}
			if s.CriticalActionType != "" {
				add(field("critical_action_type"), "Only a critical action carries a handoff type.")
			}
			if s.MedicineName != "" {
				add(field("medicine_name"), "Change this step to a medicine step, or clear the medicine.")
			}
		case RecordTypeCriticalAction:
			if strings.TrimSpace(s.Instruction) == "" {
				add(field("instruction"), "Enter what the operator should do.")
			} else if len([]rune(s.Instruction)) > MaxInstructionLen {
				add(field("instruction"), fmt.Sprintf("Instruction must be %d characters or fewer.", MaxInstructionLen))
			}
			if !inSet(s.CriticalActionType, CriticalActionTypes) {
				add(field("critical_action_type"), "Choose what this critical action hands off to.")
			}
			if s.MedicineName != "" {
				add(field("medicine_name"), "A critical action cannot also give a medicine. Add a separate medicine step.")
			}
		default:
			add(field("record_type"), "Choose whether this step is an action, a medicine or a critical action.")
		}
	}

	if forPublish && p.DurationDays >= 1 && p.DurationDays <= MaxDurationDays && len(p.Steps) > 0 {
		var empty []string
		for day := 1; day <= p.DurationDays; day++ {
			if !covered[day] {
				empty = append(empty, fmt.Sprintf("%d", day))
			}
		}
		if len(empty) > 0 {
			add("steps", "These days have no steps: "+strings.Join(empty, ", ")+".")
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return &ValidationError{Errors: errs}
}

// ContentHash is the per-protocol fingerprint stored on an authored version row.
//
// It covers exactly what an author can change: the display name, the duration, and the ordered
// steps with every field. It deliberately excludes the actor, the timestamp and the version
// number, so re-saving identical content hashes identically and the adapter can answer "unchanged"
// instead of writing a version that differs from its predecessor in nothing but its id.
//
// This is also why an authored row's hash means something the IMPORTER's does not: the importer
// stores one hash for a whole sheet snapshot, shared by every protocol in it.
func ContentHash(p AuthoredProtocol) string {
	h := sha256.New()
	fmt.Fprintf(h, "display_name=%s\nduration_days=%d\nsteps=%d\n", p.DisplayName, p.DurationDays, len(p.Steps))
	for i, s := range p.Steps {
		// Fields are written QUOTED (%q) so the encoding is unambiguous on its face.
		//
		// Being precise about what this does and does not buy, because the obvious story is wrong:
		// a raw %s here would ALSO be collision-free, because each line carries a fixed number of
		// separators and the step COUNT is hashed in the header above — so a "|" or a newline typed
		// into an instruction cannot forge a field or a line boundary without changing one of those
		// two. The quoting is defence in depth against a future edit to this format (adding an
		// optional field, dropping the count from the header) silently reintroducing that risk. It
		// is hardening, not a fix for a defect this ever had.
		//
		// The leading index is likewise a readability aid, not the thing that makes order matter:
		// order is already covered because the steps are hashed as a stream.
		fmt.Fprintf(h, "%d|%d|%q|%q|%q|%q|%q|%q|%q|%q\n",
			i, s.DayNo, s.Session, s.RecordType, s.MedicineName, s.DosageText,
			s.DosageDenominator, s.MedicineRoute, s.Instruction, s.CriticalActionType)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SortAuthoredSteps puts the steps into the order an operator works through them: day, then
// session in day order, then the author's own order within that session.
//
// Sorting is applied at SAVE time so the stored seq already reads correctly, rather than at read
// time on every request. Session order is explicit rather than alphabetical -- alphabetically
// "afternoon" precedes "evening" precedes "morning", which is the wrong order for a day.
func SortAuthoredSteps(steps []AuthoredStep) []AuthoredStep {
	rank := map[string]int{SessionMorning: 0, SessionAfternoon: 1, SessionEvening: 2, SessionUnscheduled: 3}
	out := make([]AuthoredStep, len(steps))
	copy(out, steps)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].DayNo != out[j].DayNo {
			return out[i].DayNo < out[j].DayNo
		}
		return rank[out[i].Session] < rank[out[j].Session]
	})
	return out
}

// ToProtocolSteps converts authored steps into the persisted representation, assigning seq 1..N in
// the given order and turning empty strings into NULLs.
//
// The empty-to-NULL mapping matters beyond tidiness: `health_protocol_steps_critical_check`
// asserts (record_type = 'critical_action') = (critical_action_type IS NOT NULL), so storing ”
// for a non-critical step's handoff type would violate the constraint.
func ToProtocolSteps(steps []AuthoredStep) []ProtocolStep {
	out := make([]ProtocolStep, 0, len(steps))
	for i, s := range steps {
		out = append(out, ProtocolStep{
			DayNo:              s.DayNo,
			Session:            s.Session,
			Seq:                i + 1,
			RecordType:         s.RecordType,
			MedicineName:       nilIfEmpty(s.MedicineName),
			DosageText:         nilIfEmpty(s.DosageText),
			DosageDenominator:  nilIfEmpty(s.DosageDenominator),
			MedicineRoute:      nilIfEmpty(s.MedicineRoute),
			Instruction:        nilIfEmpty(s.Instruction),
			CriticalActionType: nilIfEmpty(s.CriticalActionType),
		})
	}
	return out
}

// FromProtocolSteps is the inverse, used to seed a draft from the published version an author
// clicked "edit" on.
func FromProtocolSteps(steps []ProtocolStep) []AuthoredStep {
	out := make([]AuthoredStep, 0, len(steps))
	for _, s := range steps {
		out = append(out, AuthoredStep{
			DayNo:              s.DayNo,
			Session:            s.Session,
			RecordType:         s.RecordType,
			MedicineName:       deref(s.MedicineName),
			DosageText:         deref(s.DosageText),
			DosageDenominator:  deref(s.DosageDenominator),
			MedicineRoute:      deref(s.MedicineRoute),
			Instruction:        deref(s.Instruction),
			CriticalActionType: deref(s.CriticalActionType),
		})
	}
	return out
}

func nilIfEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
