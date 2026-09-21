package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/health/domain"
)

var (
	// ErrDraftExists is returned when a second author tries to open a draft on a protocol that
	// already has one. Backed by health_protocol_versions_one_draft_uq, so it is a real
	// serialization point and not an advisory check two concurrent requests could both pass.
	ErrDraftExists = errors.New("health: a draft is already open for this protocol")

	// ErrDiseaseExists is returned when creating a disease whose key is already in use, in either
	// age band and in any status. The key is the join identity for every case ever opened, so
	// reusing it for a different disease would merge two clinical histories.
	ErrDiseaseExists = errors.New("health: disease already exists")

	// ErrNotADraft is returned when publish or discard addresses a published or retired version.
	// A published version is immutable by design: goats are being treated from it.
	ErrNotADraft = errors.New("health: version is not a draft")

	// ErrProtocolInUse is returned when discarding would remove the only remaining protocol a live
	// case depends on. Guards the create-then-discard path; a published version is never deletable.
	ErrProtocolInUse = errors.New("health: protocol is in use by an open case")

	// ErrImportAfterAuthoring is returned by the sheet importer once the web has taken authorship.
	// See ProtocolImporter's doc comment.
	ErrImportAfterAuthoring = errors.New("health: protocols have been authored in the app; the sheet import would discard those edits")
)

// ProtocolAuthoring is the /health-config/* persistence boundary: the read side of the Health
// Config screen and the four authored writes behind it.
//
// Every write carries the idempotency contract end to end. The client key and the request
// fingerprint are persisted in health_config_write_log in the SAME transaction as the side
// effects; an exact replay returns the original result without re-running them; a
// same-key/different-payload replay is ErrConflict.
type ProtocolAuthoring interface {
	// ListProtocolCatalog returns one bounded keyset page of disease/age-band rows, each carrying
	// its published version and its open draft side by side.
	ListProtocolCatalog(ctx context.Context, q domain.ProtocolCatalogQuery) (domain.ProtocolCatalogPage, error)

	// GetProtocolDetail returns one version with its ordered steps, the disease's version history,
	// and the number of open cases pinned to this version.
	GetProtocolDetail(ctx context.Context, tenantID, protocolVersionID string) (domain.ProtocolDetail, error)

	// GetDraftForEdit returns the open draft for a disease/age band, CREATING it as a copy of the
	// published version if none is open.
	//
	// Copy-on-edit rather than edit-in-place is the whole safety property: the live protocol keeps
	// serving diagnoses untouched while an author works, and nothing an author types reaches a
	// treating operator until publish.
	GetDraftForEdit(ctx context.Context, cmd domain.ProtocolVersionCommand, diseaseKey, ageBand string) (domain.ProtocolDetail, error)

	// CreateDisease opens drafts for BOTH age bands of a disease that does not exist yet.
	CreateDisease(ctx context.Context, cmd domain.CreateDiseaseCommand) (domain.AuthoringResult, error)

	// SaveDraft replaces a draft's entire content -- name, duration, and the whole ordered step
	// list. Identical content returns outcome 'unchanged' and writes no step rows.
	SaveDraft(ctx context.Context, cmd domain.SaveDraftCommand) (domain.AuthoringResult, error)

	// PublishDraft promotes a draft to published and retires the version it replaces, in one
	// transaction. Open cases keep running on the version they pinned.
	PublishDraft(ctx context.Context, cmd domain.ProtocolVersionCommand) (domain.AuthoringResult, error)

	// DiscardDraft deletes a draft and its steps. Published versions are never deletable.
	DiscardDraft(ctx context.Context, cmd domain.ProtocolVersionCommand) (domain.AuthoringResult, error)
}

// MedicineCatalog is the authoring picker's source: the medicines a protocol step may name.
//
// It exists because a treatment step names a medicine FROM THE CATALOG and never free text
// (maintainer instruction 2026-09-21). Authoring by typing produced two spellings of one
// medicine, a medicine nobody stocks, and a dosage attached to something the store has
// never heard of.
//
// It is a HEALTH-owned read over the shared item registry rather than a filter threaded
// through the configuration register interface: every register there shares one options
// signature, and widening it for one consumer's kind filter would change a contract five
// other screens depend on.
type MedicineCatalog interface {
	// ListMedicines returns the tenant's ACTIVE medicines, by name.
	//
	// Active only, and that is the enforcement point rather than a convenience: retiring a
	// medicine on /configuration/items is how a farm stops new courses being written
	// against it, and an archived row still showing in the picker would make that a
	// suggestion instead of a decision.
	ListMedicines(ctx context.Context, tenantID string) ([]domain.CatalogItem, error)
}
