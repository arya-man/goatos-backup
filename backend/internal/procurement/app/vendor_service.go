package app

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// VendorService serves the procurement vendor register.
//
// It is deliberately thin. The register is a contact book: there is no state machine, no clock, no
// obligation and no proof, so there is nothing here for a service layer to orchestrate beyond
// validating a write and deciding whether the caller may see payment instruments. Inventing
// workflow around it would be inventing rules the business does not have.
type VendorService struct {
	repo       ports.VendorRepository
	voiceNotes ports.VoiceNoteValidator
	form       ports.VendorFormSource
}

func NewVendorService(repo ports.VendorRepository) *VendorService {
	return &VendorService{repo: repo}
}

// WithVoiceNoteValidator attaches the proof-store check a voice-note ref must pass before it is
// stored on a vendor. Without one, a write carrying a voice note is REFUSED rather than stored
// unchecked: a proof id nobody verified is a link to nothing.
func (s *VendorService) WithVoiceNoteValidator(v ports.VoiceNoteValidator) *VendorService {
	s.voiceNotes = v
	return s
}

// WithVendorFormSource attaches the authored vendor form (VENDOR FORM IS AUTHORED, 2026-09-19).
// Without one a write carrying answers is REFUSED: answers nobody checked against a form are not
// answers to anything.
func (s *VendorService) WithVendorFormSource(f ports.VendorFormSource) *VendorService {
	s.form = f
	return s
}

// VendorForm is the published form a screen renders now. side narrows the record types offered
// to the register half the screen is on, exactly as ListVendorCatalog does; "" offers all.
func (s *VendorService) VendorForm(ctx context.Context, tenantID, side string) (domain.VendorForm, error) {
	if s.form == nil {
		return domain.VendorForm{}, ErrVendorFormUnavailable
	}
	catalog, err := s.ListVendorCatalog(ctx, tenantID, side)
	if err != nil {
		return domain.VendorForm{}, err
	}
	// The SIDE decides the document (2026-09-20 split): the supply register renders
	// `procurement.vendor`, the sales one `sales.vendor`. A screen that names no side gets the
	// sales document, which is what the whole register rendered before the split.
	return s.form.PublishedVendorForm(ctx, tenantID, domain.VendorFormSOPCode(side), catalog)
}

// formVersion reads one form version with the WHOLE catalog, unnarrowed: a write is checked
// against every catalog value the register accepts, and a detail row must label a value from
// either side.
func (s *VendorService) formVersion(ctx context.Context, tenantID, recordType string, version int) (domain.VendorForm, error) {
	catalog, err := s.repo.ListVendorCatalog(ctx, tenantID, false)
	if err != nil {
		return domain.VendorForm{}, err
	}
	// WHICH document this row was answered on is derived from the register's own data -- the side
	// its record type belongs to -- never from anything the client sends. A record type the
	// catalog does not carry resolves to the sales document, the pre-split behaviour; the write's
	// record-type check is what refuses an unknown type.
	side := domain.VendorSideForRecordType(catalog, recordType)
	return s.form.VendorFormVersion(ctx, tenantID, domain.VendorFormSOPCode(side), version, catalog)
}

// VendorAnswerRows labels a vendor's extra answers by the form version they were answered on. A
// vendor written before the form existed has none; a form the library no longer carries lists the
// answers under their ids rather than dropping them.
func (s *VendorService) VendorAnswerRows(ctx context.Context, tenantID string, v domain.Vendor) []domain.VendorAnswerRow {
	if s.form == nil || v.QuestionnaireVersion == nil || len(v.SOPAnswers) == 0 {
		return domain.VendorAnswerRows(domain.VendorForm{}, v.SOPAnswers)
	}
	form, err := s.formVersion(ctx, tenantID, v.RecordType, *v.QuestionnaireVersion)
	if err != nil {
		return domain.VendorAnswerRows(domain.VendorForm{}, v.SOPAnswers)
	}
	return domain.VendorAnswerRows(form, v.SOPAnswers)
}

// applyForm checks a form-driven write's answers against the form version the client rendered
// and maps the typed answers onto the register's columns. A write with no answers (an older
// client, the importer) passes through untouched.
func (s *VendorService) applyForm(ctx context.Context, tenantID string, write domain.VendorWrite) (domain.VendorWrite, error) {
	if write.SOPAnswers == nil {
		return write, nil
	}
	if s.form == nil {
		return write, ErrVendorFormUnavailable
	}
	if write.QuestionnaireVersion <= 0 {
		return write, ErrVendorFormVersionRequired
	}
	form, err := s.formVersion(ctx, tenantID, write.RecordType, write.QuestionnaireVersion)
	if err != nil {
		return write, err
	}
	if err := domain.ValidateVendorAnswers(form, write.SOPAnswers); err != nil {
		return write, err
	}
	applied, extras := domain.ApplyVendorAnswers(write, write.SOPAnswers)
	applied.SOPAnswers = extras
	return applied, nil
}

// validateVoiceNote checks an optional voice-note ref. Blank means "no note" and passes.
func (s *VendorService) validateVoiceNote(ctx context.Context, tenantID, proofRef string) error {
	if proofRef == "" {
		return nil
	}
	if s.voiceNotes == nil {
		return ports.ErrInvalidVoiceNote
	}
	return s.voiceNotes.ValidateVendorVoiceNote(ctx, tenantID, proofRef)
}

// VendorListQuery is one page request against the register.
type VendorListQuery struct {
	Filter domain.VendorFilter
	Limit  int
	Offset int
	// IncludeFinance reflects the CALLER's VendorFinanceRead permission, resolved by the HTTP layer
	// from the request's grants. It is never a client-supplied parameter -- a caller must not be
	// able to ask for payment instruments they do not hold the permission for.
	IncludeFinance bool
}

// ListVendors returns one page plus the whole-filter total.
func (s *VendorService) ListVendors(ctx context.Context, tenantID string, q VendorListQuery) (ports.VendorPage, error) {
	if q.Offset < 0 || q.Offset > domain.MaxVendorOffset {
		// REJECTED rather than clamped. Clamping would serve page 1's rows under page 400's number,
		// which is a lie the operator cannot detect from the screen.
		return ports.VendorPage{}, ErrVendorOffsetOutOfRange
	}
	if _, ok := domain.NormalizeVendorSide(q.Filter.Side); !ok {
		return ports.VendorPage{}, ErrVendorSideUnknown
	}
	return s.repo.ListVendors(ctx, tenantID, q.Filter, q.Limit, q.Offset, q.IncludeFinance)
}

// GetVendor returns one vendor.
func (s *VendorService) GetVendor(ctx context.Context, tenantID, vendorID string, includeFinance bool) (domain.Vendor, error) {
	return s.repo.GetVendor(ctx, tenantID, vendorID, includeFinance)
}

// CreateVendor validates and inserts a vendor.
//
// Normalize runs BEFORE Validate so the rules apply to the values that will actually be stored: a
// business name of "   " must fail the required check, not pass it because it was non-empty before
// trimming.
func (s *VendorService) CreateVendor(ctx context.Context, tenantID string, write domain.VendorWrite, actorID string, includeFinance bool) (domain.Vendor, error) {
	write, err := s.applyForm(ctx, tenantID, write)
	if err != nil {
		return domain.Vendor{}, err
	}
	normalized := write.Normalize()
	// Create is held to the stricter bar: contact person, phone and city too, matching the Slack
	// intake questionnaire. Update is not -- see ValidateForCreate for why.
	if err := normalized.ValidateForCreate(); err != nil {
		return domain.Vendor{}, err
	}
	if err := s.validateVoiceNote(ctx, tenantID, normalized.VoiceNoteProofRef); err != nil {
		return domain.Vendor{}, err
	}
	created, err := s.repo.CreateVendor(ctx, tenantID, normalized, actorID)
	if err != nil {
		return domain.Vendor{}, err
	}
	// The write path accepted finance values from a caller who holds VendorWrite; the READ back is
	// still governed by VendorFinanceRead. A caller who may add a bank account but not read one
	// gets the redacted row, which keeps one rule in one place instead of two that can disagree.
	if !includeFinance {
		created = created.RedactFinance()
	}
	return created, nil
}

// UpdateVendor validates and replaces a vendor's fields, fenced on rowVersion.
func (s *VendorService) UpdateVendor(ctx context.Context, tenantID, vendorID string, write domain.VendorWrite, rowVersion int64, actorID, idempotencyKey string, includeFinance bool) (domain.Vendor, error) {
	if rowVersion <= 0 {
		// A missing or zero row_version means the client never read the row it is trying to replace,
		// so the optimistic fence cannot protect anyone. Refuse rather than defaulting to "overwrite
		// whatever is there".
		return domain.Vendor{}, ErrVendorRowVersionRequired
	}
	if idempotencyKey == "" {
		return domain.Vendor{}, ErrVendorIdempotencyRequired
	}
	write, err := s.applyForm(ctx, tenantID, write)
	if err != nil {
		return domain.Vendor{}, err
	}
	normalized := write.Normalize()
	if err := normalized.Validate(); err != nil {
		return domain.Vendor{}, err
	}
	if err := s.validateVoiceNote(ctx, tenantID, normalized.VoiceNoteProofRef); err != nil {
		return domain.Vendor{}, err
	}
	// preserveFinance is the INVERSE of includeFinance. A caller who cannot READ the payment
	// instruments was never shown them, so their form submits blanks -- and a replace would delete a
	// bank account they had no way to know existed. You cannot clear what you cannot see.
	updated, err := s.repo.UpdateVendor(ctx, tenantID, vendorID, normalized, rowVersion, actorID, idempotencyKey, !includeFinance)
	if err != nil {
		return domain.Vendor{}, err
	}
	if !includeFinance {
		updated = updated.RedactFinance()
	}
	return updated, nil
}

// UpdateVendorStatus flips only the trading status.
//
// Separate from UpdateVendor because the update is a REPLACE: routing a status change through it
// would make the caller resend every other field, and anything their screen did not render would be
// cleared as a side effect.
func (s *VendorService) UpdateVendorStatus(ctx context.Context, tenantID, vendorID, status string, rowVersion int64, actorID string) (domain.Vendor, error) {
	if rowVersion <= 0 {
		return domain.Vendor{}, ErrVendorRowVersionRequired
	}
	normalized, ok := domain.NormalizeStatus(status)
	if !ok {
		return domain.Vendor{}, domain.ErrVendorValidation{Field: "status", Reason: "must be one of active, inactive, negotiating, banned"}
	}
	return s.repo.UpdateVendorStatus(ctx, tenantID, vendorID, normalized, rowVersion, actorID)
}

// ListVendorCatalog returns the dropdown vocabularies.
//
// activeOnly is false here on purpose: the screen needs retired entries too, because an existing
// vendor may still carry one and the edit form must be able to render (and re-save) the value it
// already has. The client marks inactive entries so they are shown but not offered for new rows.
func (s *VendorService) ListVendorCatalog(ctx context.Context, tenantID string, side string) ([]domain.VendorCatalogEntry, error) {
	normalizedSide, ok := domain.NormalizeVendorSide(side)
	if !ok {
		return nil, ErrVendorSideUnknown
	}
	entries, err := s.repo.ListVendorCatalog(ctx, tenantID, false)
	if err != nil {
		return nil, err
	}
	if normalizedSide == "" {
		return entries, nil
	}
	// Narrow ONLY the record types. Every other vocabulary -- breed, state, city, status, feed,
	// capacity unit, supply frequency -- is shared by both registers: a butcher and a feed stockist
	// sit in the same states and are reached in the same towns, and duplicating those lists per side
	// would be two things to keep in step for no gain.
	//
	// The narrowing happens HERE rather than in SQL so the whole vocabulary is read once and the two
	// sides cannot drift into two different queries. It also keeps the side out of the repository's
	// catalog read, which the picklist and the importer share.
	narrowed := make([]domain.VendorCatalogEntry, 0, len(entries))
	for _, e := range entries {
		if e.Kind == domain.CatalogKindRecordType && e.RegisterSide != normalizedSide {
			continue
		}
		narrowed = append(narrowed, e)
	}
	return narrowed, nil
}

// ListVendorOptions returns the ACTIVE register as a bounded picklist for a counterparty dropdown.
//
// Read by the Sales record-sale drawer, which must map every deal to a vendor (maintainer decision
// 2026-08-27). It stays a procurement read served under VendorRead: the SALES module reads no
// procurement table (the 000173 lock), so the vendor is chosen in admin-web and only the chosen id
// travels onto the sale.
func (s *VendorService) ListVendorOptions(ctx context.Context, tenantID string) (domain.VendorOptions, error) {
	return s.repo.ListVendorOptions(ctx, tenantID)
}
