// Package domain: the VIDEO LOG is the per-shed arrival record for ONE business day -- for each
// shed, every proof that was captured for it that day and the time the server received the upload.
// Maintainer decision 2026-08-14.
//
// It answers a different question from the queue and from oversight analytics, and the distinction
// is what keeps all three honest:
//
//	queue      -> "what do I have to decide"      (open work, oldest first, verdict entry point)
//	analytics  -> "how is the backlog trending"   (tenant aggregates, no individual rows)
//	video log  -> "what arrived from this shed today, and when"
//
// Gated on permissions.VerificationEvidenceTimeline, which the VERIFIER holds -- unlike
// VerificationOversee. See that constant for why the two are separate capabilities.
//
// TWO LEVELS, both bounded, because one flat list of a day's proofs is not bounded: a vaccination
// drive raises ONE item per animal, so a single park-day can carry several hundred items before any
// feed work is counted. Level 1 is one row per shed (counts + first/last arrival). Level 2 is one
// shed's proofs in full, requested by shed_id.
package domain

import "time"

// VideoLogGrain says whether a row describes work on ONE ANIMAL or on a whole SHED. It is derived
// by the service from the item's source_ref_type -- the producer's own declaration of what its
// ref_id points at -- never guessed from the label text.
//
// This exists because the two grains genuinely differ in the field and the maintainer asked for
// both on one page: feed (distribution, packing, transport) is packed and filmed per pen, while
// vaccination, weighing and the herd operations (birth, death, shifting) are about animals. The
// client uses it to decide which column an identity belongs in -- it must NOT try to re-derive the
// grain, and must NOT compose its own label from it.
type VideoLogGrain string

const (
	// VideoLogGrainAnimal: the item is about a specific animal or animals -- vaccination's
	// per-goat clip, weighing's individual observation, a birth/death workflow, a shed move.
	VideoLogGrainAnimal VideoLogGrain = "animal"
	// VideoLogGrainShed: the item is about a location's work -- a feed session's distribution,
	// a pen's packing bag, a shed's transport load, a lump-sum weigh.
	VideoLogGrainShed VideoLogGrain = "shed"
)

// VideoLogProof is ONE uploaded artifact and the time it landed.
type VideoLogProof struct {
	ProofID string
	// Ordinal is this proof's 1-based position in the producing item's media_refs array. It is the
	// producer's DECLARED order, and it is what the category registry's MediaLabels are positional
	// against -- so the label for a proof is resolved from Ordinal, never from its index in a
	// filtered slice. A ref whose artifact is missing is dropped from Proofs, which would shift
	// every later index by one and silently rename the remaining proofs.
	Ordinal int
	// Label is the backend-owned header for this proof ("Water distribution video"). Resolved from
	// the artifact's own verification_label metadata first, then from the category registry's
	// positional MediaLabels. Never a raw code, never composed by the client.
	Label string
	// MediaKind is "video" or "photo", from proof_artifacts.proof_type. Feed distribution is the
	// one category that mixes them (a weight PHOTO leads its two videos), so a screen that says
	// "video" for all three would be lying about one of them.
	MediaKind string
	// UploadedAt is when the SERVER accepted the bytes (proof_artifacts.uploaded_at). It is nil
	// for a proof that was registered but never finished uploading -- which is a real and
	// interesting state, so the row is kept and the time is left empty rather than the proof being
	// dropped from the log.
	//
	// This is deliberately NOT a device capture time: no such column exists on proof_artifacts,
	// and inventing one from created_at would be a guess presented as a fact. See RegisteredAt.
	UploadedAt *time.Time
	// RegisteredAt is when the client first registered the artifact and took an upload URL
	// (proof_artifacts.created_at). On mobile the outbox registers at capture and retries the byte
	// upload later, so RegisteredAt is the closest thing the schema has to a capture instant and
	// the gap to UploadedAt is real upload lag. Exposed so a reader can see a proof that was shot
	// in the morning and only landed in the evening, instead of reading the evening time as when
	// the work happened.
	RegisteredAt time.Time
}

// VideoLogRow is ONE verification item -- one piece of work that was filmed -- with its proofs.
type VideoLogRow struct {
	ItemID string
	// ShedID/ShedLabel/PartitionLabel identify WHERE this work happened.
	//
	// Redundant when a single shed's detail is on screen (the header already names it), and
	// essential for the WHOLE-DAY export, which spans every shed: without them a row in that file
	// says a video arrived at 06:58 and cannot say from where. Composed into
	// operational_location_display at the wire boundary by the same oploc helper the summary uses,
	// so one animal never reads two ways across the two levels.
	ShedID         string
	ShedLabel      string
	PartitionLabel string
	// ParkLabel disambiguates the location in the whole-day export. Shed NAMES repeat across parks
	// -- this tenant has 74 shed rows resolving to only 69 distinct display strings -- so a file
	// without the park renders two different sheds identically and a reader cannot tell them apart.
	// That is the OL-1 name-collision case the operational-location convention exists to prevent.
	ParkLabel string
	// Module/Category are the raw codes ("feed", "feed_packing"). They are carried for filtering
	// and links only; ModuleLabel/CategoryLabel are what a screen renders. The copy firewall bans
	// the raw codes in visible UI.
	Module        string
	ModuleLabel   string
	Category      string
	CategoryLabel string
	// NavModule is the queue's own module-filter key for Module ("feed_direction" where Module is
	// "feed"), so a row can link back to the queue filtered to its module.
	NavModule string
	Grain     VideoLogGrain
	// SubjectLabel is the producing module's own composed description of the work
	// ("Session 1 · Castro - 2", "Godel 1 · Goat 4821 · PPR", "Shed move · Godel 1 - Part 3 · 12
	// animals"). Rendered VERBATIM. It is legitimately EMPTY for feed transport, whose bridge
	// deliberately writes no label because the card already names the shed -- a client must handle
	// that rather than printing a placeholder that looks like missing data.
	SubjectLabel string
	// Status is the item's verdict state, carried so the log can show that an arrival was later
	// rejected. The video log NEVER offers a verdict control -- it is a read.
	Status string
	// OperatorName is the backend-resolved display name of whoever captured the work, empty when
	// the id resolves to no active roster member (automation and backfill writes). An unresolved
	// id is dropped rather than rendered raw, per the operational-location/id-display rule.
	OperatorName string
	// CapturedAt is the item's own anchor -- the instant the producing module stamped when it
	// enqueued. It is what the business DAY is cut on, and it is NOT the upload time. For feed,
	// vaccination and weighing it closely tracks when the work happened; for shifting, birth and
	// death the producer passes the enqueue instant, so it is approximate there.
	CapturedAt time.Time
	Proofs     []VideoLogProof
}

// VideoLogShed is ONE shed's line in the day summary. It is also the header for that shed's detail.
type VideoLogShed struct {
	ShedID string
	// ShedLabel/PartitionLabel are the raw parts; OperationalLocationDisplay is the ONLY one a
	// screen renders, composed by oploc.Display() at the wire boundary. PartitionLabel is NULL for
	// weighing, shifting, birth and death items, whose producers never set it -- so a shed whose
	// day is only those modules shows its bare shed name, which is correct rather than missing.
	ShedLabel                  string
	PartitionLabel             string
	OperationalLocationDisplay string
	ParkID                     string
	ParkLabel                  string
	// ProofCount is every proof that arrived for this shed on the day, across every module.
	ProofCount int
	// ItemCount is how many pieces of work those proofs belong to. ProofCount >= ItemCount, and the
	// gap is exactly the multi-proof categories (feed distribution's three, death's two).
	ItemCount int
	// AwaitingUploadCount is proofs registered but not yet received. It is the honest "filmed but
	// not landed" number and is included in ProofCount, not counted beside it.
	AwaitingUploadCount int
	// FirstUploadAt/LastUploadAt bracket the day's arrivals for this shed, over RECEIVED proofs
	// only. Both nil when nothing has landed yet.
	FirstUploadAt *time.Time
	LastUploadAt  *time.Time
	// Modules is the distinct set of module labels that contributed, so the summary line can say
	// WHAT arrived without opening the shed.
	Modules []string
}

// VideoLog is the full GET /verification/video-log payload.
type VideoLog struct {
	// BusinessDate is the Asia/Kolkata day the log covers, YYYY-MM-DD.
	BusinessDate string
	// Sheds is the day's per-shed summary, ordered by operational location display. Always present.
	Sheds []VideoLogShed
	// SelectedShedID echoes the requested shed, empty when none was asked for.
	SelectedShedID string
	// Rows is work in full, ordered by shed then first arrival then the producer's own proof order.
	//
	// Populated in TWO cases and no others: a selected shed (the panel's detail level), or an
	// explicit whole-day EXPORT (AllSheds). It is deliberately EMPTY for the ordinary summary read,
	// because a vaccination drive raises one item per animal and rendering every row for a park-day
	// would be the unbounded read this two-level design exists to avoid. The export pays that cost
	// once, on demand, into a file -- never into the screen.
	Rows []VideoLogRow
	// RowsTruncated reports that the selected shed had more work than the read returns, so a
	// reader is never shown a partial list that looks complete. No silent caps.
	RowsTruncated bool
}
